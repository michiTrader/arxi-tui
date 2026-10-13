package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi/internal/turn"
)

// This file is for models that cannot use the provider's tool channel. Some gateways
// and open models answer a request that offered tools with the call written as TEXT, in
// the shape they were trained on:
//
//	<tool_call>
//	<function=read>
//	<parameter=path>
//	main.go
//	</parameter>
//	</function>
//	</tool_call>
//
// or the JSON variant, <tool_call>{"name":"read","arguments":{...}}</tool_call>. The
// provider sees plain text and a finish reason of "stop", so the tool loop took it for
// the final answer: the user read a tool call on the screen, nothing ran, and the turn
// ended. That is the failure this fixes.
//
// The rule is narrow on purpose. A block becomes a call only when it names a tool that
// was OFFERED this turn (an unknown name stays text, so a model cannot make the core
// run something nobody lent it) and every argument is read against that tool's schema
// (an integer arrives as an integer). The call then goes through the same policy,
// approval and reporting as a native one: choosing another syntax buys no permission.

var (
	textCallBlock = regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)
	textCallFunc  = regexp.MustCompile(`(?s)<function=([^>\s]+)>(.*?)</function>`)
	textCallParam = regexp.MustCompile(`(?s)<parameter=([^>\s]+)>(.*?)</parameter>`)
)

// maxTextCalls bounds how many calls one reply may carry, like a native batch.
const maxTextCalls = 8

// textCalls finds the tool calls written as text in a reply. It returns the calls, in
// order, and the reply with those blocks taken out. Nothing is returned for a reply
// with no call, or whose calls name tools that were not offered.
func textCalls(text string, offered []turn.ToolDefinition, idPrefix string) (calls []*turn.ToolCall, rest string) {
	if !strings.Contains(text, "<tool_call>") || len(offered) == 0 {
		return nil, text
	}
	schemas := make(map[string]json.RawMessage, len(offered))
	for _, d := range offered {
		schemas[d.Name] = d.InputSchema
	}
	rest = textCallBlock.ReplaceAllStringFunc(text, func(block string) string {
		if len(calls) >= maxTextCalls {
			return block
		}
		inner := textCallBlock.FindStringSubmatch(block)[1]
		name, args, ok := parseTextCall(inner, schemas)
		if !ok {
			return block // not a call we can honour: leave the text as the model wrote it
		}
		canon, err := turn.NewToolCall(fmt.Sprintf("%s%d", idPrefix, len(calls)+1), name, args)
		if err != nil {
			return block
		}
		calls = append(calls, &canon)
		return ""
	})
	if len(calls) == 0 {
		return nil, text
	}
	return calls, strings.TrimSpace(rest)
}

// parseTextCall reads the inside of one <tool_call> block.
func parseTextCall(inner string, schemas map[string]json.RawMessage) (name string, args []byte, ok bool) {
	inner = strings.TrimSpace(inner)
	if strings.HasPrefix(inner, "{") {
		var j struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal([]byte(inner), &j) != nil || j.Name == "" {
			return "", nil, false
		}
		if _, offered := schemas[j.Name]; !offered {
			return "", nil, false
		}
		a := j.Arguments
		if len(a) == 0 || string(a) == "null" {
			a = json.RawMessage(`{}`)
		}
		var obj map[string]any
		if json.Unmarshal(a, &obj) != nil {
			return "", nil, false
		}
		return j.Name, a, true
	}
	m := textCallFunc.FindStringSubmatch(inner)
	if m == nil {
		return "", nil, false
	}
	name = m[1]
	schema, offered := schemas[name]
	if !offered {
		return "", nil, false
	}
	types := schemaTypes(schema)
	obj := map[string]any{}
	for _, p := range textCallParam.FindAllStringSubmatch(m[2], -1) {
		// A parameter's text is delimited by newlines the model put around it; the
		// value itself may hold more (a command, a block of code), so only one leading
		// and one trailing newline are the markup's.
		v := strings.TrimPrefix(p[2], "\n")
		v = strings.TrimSuffix(v, "\n")
		obj[p[1]] = typedValue(v, types[p[1]])
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return "", nil, false
	}
	return name, b, true
}

// schemaTypes is the declared type of each property of a tool's argument schema.
func schemaTypes(schema json.RawMessage) map[string]string {
	var s struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	out := map[string]string{}
	if json.Unmarshal(schema, &s) == nil {
		for k, v := range s.Properties {
			out[k] = v.Type
		}
	}
	return out
}

// typedValue reads a parameter's text as the type the schema declares. A value that
// does not parse as that type stays a string, and the tool reports it as it would a
// wrong argument from a native call.
func typedValue(v, typ string) any {
	switch typ {
	case "integer":
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	case "number":
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return n
		}
	case "boolean":
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	case "array", "object":
		var x any
		if json.Unmarshal([]byte(v), &x) == nil {
			return x
		}
	}
	return v
}

// withTextCalls turns a reply that wrote its tool calls as text into a reply that made
// them, so the loop runs them. The text around the blocks stays as the model's words.
func withTextCalls(resp turn.Response, offered []turn.ToolDefinition, round int) turn.Response {
	for _, b := range resp.Content {
		if b.Type == turn.BlockToolCall && b.ToolCall != nil {
			return resp // it used the channel: nothing to translate
		}
	}
	var out []turn.ContentBlock
	var found []*turn.ToolCall
	for _, b := range resp.Content {
		if b.Type != turn.BlockText {
			out = append(out, b)
			continue
		}
		calls, rest := textCalls(b.Text, offered, fmt.Sprintf("text_%d_%d_", round, len(found)))
		found = append(found, calls...)
		if strings.TrimSpace(rest) != "" || len(calls) == 0 {
			nb := b
			nb.Text = rest
			out = append(out, nb)
		}
	}
	if len(found) == 0 {
		return resp
	}
	for _, c := range found {
		out = append(out, turn.ContentBlock{Type: turn.BlockToolCall, ToolCall: c})
	}
	resp.Content = out
	resp.FinishReason = turn.FinishToolCalls
	return resp
}
