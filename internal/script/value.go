package script

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// A value is one of: nil (null), bool, float64 (always finite), string, *Array, *Object,
// *Closure or *Native. Texts count characters, not bytes: "é".length is 1.

type Array struct{ elems []any }

// Object keeps its keys in the order they were first set, so what a script prints and what a
// test compares do not depend on a map's order.
type Object struct {
	keys   []string
	m      map[string]any
	frozen bool
}

func newObject() *Object { return &Object{m: map[string]any{}} }

func (o *Object) get(k string) (any, bool) { v, ok := o.m[k]; return v, ok }
func (o *Object) set(k string, v any) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

type Closure struct {
	fn  *funcLit
	env *env
}

// Native is a function written in Go: a part of the standard library, or one the host gave.
type Native struct {
	name  string
	fn    func(it *interp, args []any) any
	props *Object // for `Number.isInteger`: the function is also a namespace
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case *Array:
		return "array"
	case *Object:
		return "object"
	case *Closure, *Native:
		return "function"
	}
	return "unknown"
}

// the way a type is named in a message
func article(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a yes/no"
	case float64:
		return "a number"
	case string:
		return "a text"
	case *Array:
		return "a list"
	case *Object:
		return "an object"
	}
	return "a function"
}

func formatNumber(f float64) string {
	if f == 0 {
		return "0"
	}
	if math.Abs(f) < 1e21 && math.Abs(f) >= 1e-6 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	// 1e+21 -> 1e+21, 1e-07 -> 1e-7
	if i := strings.IndexByte(s, 'e'); i >= 0 {
		exp := strings.TrimLeft(s[i+2:], "0")
		if exp == "" {
			exp = "0"
		}
		s = s[:i+2] + exp
	}
	return s
}

// text is how a value reads inside a template text and in String(x): lists and objects as JSON.
func text(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return formatNumber(x)
	case string:
		return x
	case *Closure:
		return "[function " + x.fn.name + "]"
	case *Native:
		return "[function " + x.name + "]"
	}
	s, _ := jsonText(v, 0)
	return s
}

const jsonDepth = 24

func jsonText(v any, depth int) (string, bool) {
	if depth > jsonDepth {
		return "...", false
	}
	switch x := v.(type) {
	case nil:
		return "null", true
	case bool, float64:
		return text(v), true
	case string:
		return strconv.Quote(x), true
	case *Array:
		var sb strings.Builder
		sb.WriteByte('[')
		ok := true
		for i, e := range x.elems {
			if i > 0 {
				sb.WriteByte(',')
			}
			s, good := jsonText(e, depth+1)
			ok = ok && good
			sb.WriteString(s)
		}
		sb.WriteByte(']')
		return sb.String(), ok
	case *Object:
		var sb strings.Builder
		sb.WriteByte('{')
		ok := true
		for i, k := range x.keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(strconv.Quote(k))
			sb.WriteByte(':')
			s, good := jsonText(x.m[k], depth+1)
			ok = ok && good
			sb.WriteString(s)
		}
		sb.WriteByte('}')
		return sb.String(), ok
	}
	return "null", true // a function has no JSON form
}

func strictEqual(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	}
	return a == b // lists, objects and functions are equal when they are the same one
}

// ---- crossing to and from Go

// Import turns a Go value the host gives into a script value (copying lists and maps).
func importValue(v any, depth int) (any, error) {
	if depth > jsonDepth {
		return nil, fmt.Errorf("a value nested more than %d levels deep cannot enter a script", jsonDepth)
	}
	switch x := v.(type) {
	case nil:
		return nil, nil
	case bool, string:
		return x, nil
	case float64:
		return finite(x)
	case float32:
		return finite(float64(x))
	case int:
		return float64(x), nil
	case int8:
		return float64(x), nil
	case int16:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case uint:
		return float64(x), nil
	case uint8:
		return float64(x), nil
	case uint16:
		return float64(x), nil
	case uint32:
		return float64(x), nil
	case uint64:
		return float64(x), nil
	case []any:
		a := &Array{elems: make([]any, len(x))}
		for i, e := range x {
			iv, err := importValue(e, depth+1)
			if err != nil {
				return nil, err
			}
			a.elems[i] = iv
		}
		return a, nil
	case []string:
		a := &Array{elems: make([]any, len(x))}
		for i, e := range x {
			a.elems[i] = e
		}
		return a, nil
	case map[string]any:
		o := newObject()
		ks := make([]string, 0, len(x))
		for k := range x {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			iv, err := importValue(x[k], depth+1)
			if err != nil {
				return nil, err
			}
			o.set(k, iv)
		}
		return o, nil
	case map[string]string:
		o := newObject()
		ks := make([]string, 0, len(x))
		for k := range x {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			o.set(k, x[k])
		}
		return o, nil
	case HostFunc:
		return nil, fmt.Errorf("a HostFunc must be given as a global, not inside a value")
	}
	return nil, fmt.Errorf("a Go value of type %T cannot enter a script. Remedy: give nil, bool, a number, string, []any or map[string]any", v)
}

func finite(f float64) (any, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, fmt.Errorf("a number that is not finite cannot enter a script")
	}
	return f, nil
}

// exportValue turns a script value into plain Go: nil, bool, float64, string, []any,
// map[string]any. A function cannot leave the script.
func exportValue(v any, depth int) (any, error) {
	if depth > jsonDepth {
		return nil, fmt.Errorf("the value is nested more than %d levels deep, or it contains itself", jsonDepth)
	}
	switch x := v.(type) {
	case nil, bool, float64, string:
		return x, nil
	case *Array:
		out := make([]any, len(x.elems))
		for i, e := range x.elems {
			ev, err := exportValue(e, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = ev
		}
		return out, nil
	case *Object:
		out := make(map[string]any, len(x.keys))
		for _, k := range x.keys {
			ev, err := exportValue(x.m[k], depth+1)
			if err != nil {
				return nil, err
			}
			out[k] = ev
		}
		return out, nil
	}
	return nil, fmt.Errorf("a function cannot be returned to the host. Remedy: return its result, or a text naming it")
}

// sizeOf estimates the bytes a value keeps alive. It is an estimate used to bound the state a
// script keeps, not an accounting of the Go heap; the constants are chosen so that a script
// cannot hide a large structure behind a small estimate.
func sizeOf(v any, seen map[any]bool, budget int) int {
	n := 16
	switch x := v.(type) {
	case string:
		return n + len(x)
	case *Array:
		if seen[x] {
			return 8
		}
		seen[x] = true
		n += 24
		for _, e := range x.elems {
			n += sizeOf(e, seen, budget)
			if n > budget {
				return n
			}
		}
	case *Object:
		if seen[x] {
			return 8
		}
		seen[x] = true
		n += 48
		for _, k := range x.keys {
			n += 32 + len(k) + sizeOf(x.m[k], seen, budget)
			if n > budget {
				return n
			}
		}
	case *Closure:
		if seen[x] {
			return 8
		}
		seen[x] = true
		n += 48 + envSize(x.env, seen, budget)
	}
	return n
}

func envSize(e *env, seen map[any]bool, budget int) int {
	n := 0
	for ; e != nil && !e.root; e = e.parent {
		if seen[e] {
			return n
		}
		seen[e] = true
		for k, b := range e.vars {
			n += 32 + len(k) + sizeOf(b.v, seen, budget)
			if n > budget {
				return n
			}
		}
	}
	return n
}
