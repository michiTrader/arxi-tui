// Package verb is the catalogue of what the interface can be told to do, by name.
//
// A verb is a capability of the program with a name, typed parameters and a protection
// level (docs/DESIGN-OPEN-TUI.md section 5.2). The scene's `do:` action, the user's own
// keys and commands, and (later) the keymap and the interpreter all name verbs from this
// one list, so what a button may do is decided in one place and checked at load.
//
// The package holds only the description and the reader of a call. What a verb does lives
// in cmd/arxi-tui (it needs the shells, the conversation, the settings), and a test there
// holds the two lists together: a verb with no implementation, or an implementation with
// no verb, fails the build.
package verb

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Kind is the type of a parameter.
type Kind int

const (
	// Int is a whole number inside [Min, Max].
	Int Kind = iota
	// Text is free text. Only the last parameter of a verb may be Text: it takes the rest of
	// the line.
	Text
)

// Param is one parameter of a verb, in the order it is written.
type Param struct {
	Name     string
	Kind     Kind
	Required bool
	// Min and Max bound an Int. Max 0 means no upper bound.
	Min, Max int
	// Words are the keywords an Int parameter also accepts, such as "all".
	Words []string
	Doc   string
}

// Protection says who may cause a verb to run.
type Protection int

const (
	// Free verbs may run from anything the user set up: a key, a button, a command of their
	// own, a hook.
	Free Protection = iota
	// Typed verbs run only from a line the user typed themselves. A button, a shortcut, a
	// command or a hook cannot start them, because what they do outlives the press and the
	// label on a button can say anything.
	Typed
)

func (p Protection) String() string {
	if p == Typed {
		return "typed"
	}
	return "free"
}

// Verb is one entry of the catalogue.
type Verb struct {
	Name       string
	Doc        string
	Params     []Param
	Protection Protection
	// Example is a call that must parse; the catalogue test runs every one.
	Example string
}

// Call is a verb and its arguments, one string per parameter that was written.
type Call struct {
	Verb Verb
	Args []string
}

// MaxLine bounds a call, like every other action.
const MaxLine = 400

var catalogue = []Verb{
	{
		Name: "shell.start", Protection: Typed,
		Doc:     "start a command in the background; the shells list shows it",
		Params:  []Param{{Name: "command", Kind: Text, Required: true, Doc: "the command line to run"}},
		Example: "shell.start npm run dev",
	},
	{
		Name: "shell.stop", Protection: Free,
		Doc:     "end one background shell, or all of them",
		Params:  []Param{{Name: "id", Kind: Int, Required: true, Min: 1, Words: []string{"all"}, Doc: "the shell number, or all"}},
		Example: "shell.stop 2",
	},
	{
		Name: "shell.read", Protection: Free,
		Doc: "show the last lines a background shell printed, as a note in the conversation",
		Params: []Param{
			{Name: "id", Kind: Int, Required: true, Min: 1, Doc: "the shell number"},
			{Name: "lines", Kind: Int, Min: 1, Max: 200, Doc: "how many lines, 30 when left out"},
		},
		Example: "shell.read 1 50",
	},
	{
		Name: "view.expand", Protection: Free,
		Doc:     "show the cut part of a result or a change in full, or cut it again",
		Example: "view.expand",
	},
	{
		Name: "find.toggle", Protection: Free,
		Doc:     "open the find bar on the input row, or close it when it is open",
		Example: "find.toggle",
	},
	{
		Name: "queue.push", Protection: Free,
		Doc:     "put the line being typed in the queue of messages, to be sent when the answer being written ends",
		Example: "queue.push",
	},
	{
		Name: "script.run", Protection: Free,
		Doc:     "run the function `run(text, facts)` of one of the user's scripts (behaviour.json, scripts); what it returns is shown as a note",
		Params:  []Param{{Name: "script", Kind: Text, Required: true, Doc: "the name of the script, then optionally one line of text the script receives"}},
		Example: "script.run counter add 3",
	},
	{
		Name: "queue.steer", Protection: Free,
		Doc:     "stop the answer being written and send the line being typed at once (what the answer did stays in the conversation)",
		Example: "queue.steer",
	},
}

// All returns the catalogue, in a stable order. It is a copy: a caller cannot change it.
func All() []Verb {
	out := make([]Verb, len(catalogue))
	for i, v := range catalogue {
		v.Params = append([]Param(nil), v.Params...)
		out[i] = v
	}
	return out
}

// Names returns the verb names, in the order of the catalogue.
func Names() []string {
	out := make([]string, len(catalogue))
	for i, v := range catalogue {
		out[i] = v.Name
	}
	return out
}

// Find returns the verb with that name.
func Find(name string) (Verb, bool) {
	for _, v := range All() {
		if v.Name == name {
			return v, true
		}
	}
	return Verb{}, false
}

// Usage is the written form of a verb: the name and its parameters, optional ones in
// brackets.
func (v Verb) Usage() string {
	var b strings.Builder
	b.WriteString(v.Name)
	for _, p := range v.Params {
		if p.Required {
			b.WriteString(" <" + p.Name + ">")
		} else {
			b.WriteString(" [" + p.Name + "]")
		}
	}
	return b.String()
}

// Check reads a call the way the scene validator does, at load: the verb must exist and
// the arguments must fit its parameters, except that a value still holding a {row.<field>}
// token is checked only after the token is replaced (the host reads it again with Parse at
// press time, when the row is known).
func Check(line string) error {
	_, err := parse(line, false)
	return err
}

// Parse reads a call with every value checked. It is the reader the host runs before it
// does anything.
func Parse(line string) (Call, error) { return parse(line, true) }

func parse(line string, strict bool) (Call, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Call{}, fmt.Errorf("a do: action names a verb; write do:<verb> <arguments>, one of %s", strings.Join(Names(), ", "))
	}
	if len([]rune(line)) > MaxLine {
		return Call{}, fmt.Errorf("do:%s is longer than %d characters", short(line), MaxLine)
	}
	for _, r := range line {
		if unicode.IsControl(r) {
			return Call{}, fmt.Errorf("do:%s holds a control character; one action is one line of plain text", short(line))
		}
	}
	name, rest, _ := strings.Cut(line, " ")
	v, ok := Find(name)
	if !ok {
		return Call{}, fmt.Errorf("do:%s names a verb that does not exist; the verbs are %s", name, strings.Join(Names(), ", "))
	}
	rest = strings.TrimSpace(rest)
	var args []string
	for i, p := range v.Params {
		if rest == "" {
			if p.Required {
				return Call{}, fmt.Errorf("do:%s lacks <%s> (%s); the form is do:%s", name, p.Name, p.Doc, v.Usage())
			}
			break
		}
		var val string
		if p.Kind == Text {
			if i != len(v.Params)-1 {
				return Call{}, fmt.Errorf("verb %s declares a text parameter before the last one; the catalogue is wrong", name)
			}
			val, rest = rest, ""
		} else {
			val, rest, _ = strings.Cut(rest, " ")
			rest = strings.TrimSpace(rest)
		}
		if strict || !strings.Contains(val, "{row.") {
			if err := checkValue(v, p, val); err != nil {
				return Call{}, err
			}
		}
		args = append(args, val)
	}
	if rest != "" {
		return Call{}, fmt.Errorf("do:%s has extra text %q after its last parameter; the form is do:%s", name, short(rest), v.Usage())
	}
	return Call{Verb: v, Args: args}, nil
}

func checkValue(v Verb, p Param, val string) error {
	switch p.Kind {
	case Text:
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("do:%s has an empty <%s>; the form is do:%s", v.Name, p.Name, v.Usage())
		}
	case Int:
		for _, w := range p.Words {
			if val == w {
				return nil
			}
		}
		n, err := strconv.Atoi(val)
		if err != nil {
			want := "a whole number"
			if len(p.Words) > 0 {
				want += " or " + strings.Join(p.Words, ", ")
			}
			return fmt.Errorf("do:%s: <%s> is %q, which is not %s; the form is do:%s", v.Name, p.Name, short(val), want, v.Usage())
		}
		if n < p.Min || (p.Max > 0 && n > p.Max) {
			rng := fmt.Sprintf("at least %d", p.Min)
			if p.Max > 0 {
				rng = fmt.Sprintf("from %d to %d", p.Min, p.Max)
			}
			return fmt.Errorf("do:%s: <%s> is %d; it must be %s", v.Name, p.Name, n, rng)
		}
	}
	return nil
}

func short(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:39]) + "…"
	}
	return s
}
