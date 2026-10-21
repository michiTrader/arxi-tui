package script

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
)

// builtinNames is what the language gives without the host. Each is read-only.
var builtinNames = map[string]declKind{
	"Math": dConst, "Number": dConst, "String": dConst, "Array": dConst, "Object": dConst, "JSON": dConst,
}

func native(name string, fn func(it *interp, args []any) any) *Native {
	return &Native{name: name, fn: fn}
}

func namespace(name string, fn func(it *interp, args []any) any, props map[string]any) *Native {
	o := newObject()
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o.set(k, props[k])
	}
	return &Native{name: name, fn: fn, props: o}
}

func installBuiltins(root *env) {
	num1 := func(name string, f func(float64) float64) *Native {
		return native(name, func(it *interp, args []any) any {
			return it.finiteResult(pos{}, f(it.argNum(name, args, 0)), "Math."+name)
		})
	}
	math_ := newObject()
	for _, p := range []struct {
		n string
		f func(float64) float64
	}{
		{"floor", math.Floor}, {"ceil", math.Ceil}, {"trunc", math.Trunc}, {"abs", math.Abs}, {"sqrt", math.Sqrt},
		{"round", func(x float64) float64 { return math.Floor(x + 0.5) }},
		{"sign", func(x float64) float64 {
			switch {
			case x > 0:
				return 1
			case x < 0:
				return -1
			}
			return 0
		}},
		{"sin", math.Sin}, {"cos", math.Cos},
	} {
		math_.set(p.n, num1(p.n, p.f))
	}
	math_.set("min", native("min", func(it *interp, args []any) any {
		return it.extreme("min", args, func(a, b float64) bool { return a < b })
	}))
	math_.set("max", native("max", func(it *interp, args []any) any {
		return it.extreme("max", args, func(a, b float64) bool { return a > b })
	}))
	math_.set("pow", native("pow", func(it *interp, args []any) any {
		return it.finiteResult(pos{}, math.Pow(it.argNum("pow", args, 0), it.argNum("pow", args, 1)), "Math.pow")
	}))
	math_.set("PI", math.Pi)
	root.declare("Math", math_, true)

	root.declare("Number", namespace("Number", func(it *interp, args []any) any {
		// Number(x): a number, or null when the text is not one.
		switch v := it.arg(args, 0).(type) {
		case float64:
			return v
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil
			}
			return f
		case bool:
			if v {
				return 1.0
			}
			return 0.0
		}
		return nil
	}, map[string]any{
		"isInteger": native("isInteger", func(it *interp, args []any) any {
			f, ok := it.arg(args, 0).(float64)
			return ok && f == math.Trunc(f)
		}),
		"MAX_SAFE_INTEGER": 9007199254740991.0,
	}), true)

	root.declare("String", native("String", func(it *interp, args []any) any {
		s := text(it.arg(args, 0))
		it.checkString(pos{}, len(s))
		return s
	}), true)

	root.declare("Array", namespace("Array", func(it *interp, args []any) any {
		it.fail(pos{}, "`Array(...)` is not available. Remedy: write a list `[...]`; to make n items use a `for` loop and `push`.")
		return nil
	}, map[string]any{
		"isArray": native("isArray", func(it *interp, args []any) any { _, ok := it.arg(args, 0).(*Array); return ok }),
	}), true)

	root.declare("Object", namespace("Object", func(it *interp, args []any) any {
		it.fail(pos{}, "`Object(...)` is not available. Remedy: write an object `{...}`.")
		return nil
	}, map[string]any{
		"keys": native("keys", func(it *interp, args []any) any {
			o := it.argObj("Object.keys", args, 0)
			a := &Array{}
			for _, k := range o.keys {
				a.elems = append(a.elems, k)
			}
			it.charge(pos{}, 16*len(a.elems)+24)
			return a
		}),
		"values": native("values", func(it *interp, args []any) any {
			o := it.argObj("Object.values", args, 0)
			a := &Array{}
			for _, k := range o.keys {
				a.elems = append(a.elems, o.m[k])
			}
			it.charge(pos{}, 8*len(a.elems)+24)
			return a
		}),
		"entries": native("entries", func(it *interp, args []any) any {
			o := it.argObj("Object.entries", args, 0)
			a := &Array{}
			for _, k := range o.keys {
				a.elems = append(a.elems, &Array{elems: []any{k, o.m[k]}})
			}
			it.charge(pos{}, 64*len(a.elems)+24)
			return a
		}),
		"assign": native("assign", func(it *interp, args []any) any {
			dst := it.argObj("Object.assign", args, 0)
			for i := 1; i < len(args); i++ {
				src := it.argObj("Object.assign", args, i)
				for _, k := range src.keys {
					it.setKey(dst, k, src.m[k], pos{})
				}
			}
			return dst
		}),
	}), true)

	root.declare("JSON", func() *Object {
		o := newObject()
		o.set("stringify", native("stringify", func(it *interp, args []any) any {
			s, ok := jsonText(it.arg(args, 0), 0)
			if !ok {
				it.fail(pos{}, "`JSON.stringify` cannot write a value nested more than %d levels deep.", jsonDepth)
			}
			it.checkString(pos{}, len(s))
			it.charge(pos{}, len(s))
			return s
		}))
		o.set("parse", native("parse", func(it *interp, args []any) any {
			s := it.argStr("JSON.parse", args, 0)
			var v any
			if err := json.Unmarshal([]byte(s), &v); err != nil {
				return nil // a text that is not JSON is null: check with `=== null`
			}
			iv, err := importValue(v, 0)
			if err != nil {
				return nil
			}
			it.charge(pos{}, 2*len(s))
			return iv
		}))
		return o
	}(), true)
}

// ---- argument helpers

func (it *interp) arg(args []any, i int) any {
	if i < len(args) {
		return args[i]
	}
	return nil
}

func (it *interp) argNum(fn string, args []any, i int) float64 {
	v := it.arg(args, i)
	f, ok := v.(float64)
	if !ok {
		it.fail(pos{}, "`%s` needs a number as argument %d but got %s. Remedy: convert a text with `Number(text)`.", fn, i+1, article(v))
	}
	return f
}

func (it *interp) argInt(fn string, args []any, i int, def int) int {
	if i >= len(args) || args[i] == nil {
		return def
	}
	f := it.argNum(fn, args, i)
	if f != math.Trunc(f) {
		it.fail(pos{}, "`%s` needs a whole number as argument %d but got %s. Remedy: use `Math.floor(x)`.", fn, i+1, formatNumber(f))
	}
	if f > 1e9 {
		f = 1e9
	} else if f < -1e9 {
		f = -1e9
	}
	return int(f)
}

func (it *interp) argStr(fn string, args []any, i int) string {
	v := it.arg(args, i)
	s, ok := v.(string)
	if !ok {
		it.fail(pos{}, "`%s` needs a text as argument %d but got %s. Remedy: use `String(x)`.", fn, i+1, article(v))
	}
	return s
}

func (it *interp) argObj(fn string, args []any, i int) *Object {
	v := it.arg(args, i)
	o, ok := v.(*Object)
	if !ok {
		it.fail(pos{}, "`%s` needs an object as argument %d but got %s.", fn, i+1, article(v))
	}
	return o
}

func (it *interp) argFn(fn string, args []any, i int) any {
	v := it.arg(args, i)
	switch v.(type) {
	case *Closure, *Native:
		return v
	}
	it.fail(pos{}, "`%s` needs a function as argument %d but got %s. Remedy: write `(x) => ...`.", fn, i+1, article(v))
	return nil
}

func (it *interp) extreme(name string, args []any, better func(a, b float64) bool) any {
	if len(args) == 0 {
		it.fail(pos{}, "`Math.%s` needs at least one number.", name)
	}
	best := it.argNum("Math."+name, args, 0)
	for i := 1; i < len(args); i++ {
		if f := it.argNum("Math."+name, args, i); better(f, best) {
			best = f
		}
	}
	return best
}

func numberToFixed(it *interp, x float64, args []any, at pos) any {
	d := it.argInt("toFixed", args, 0, 0)
	if d < 0 || d > 20 {
		it.fail(at, "`toFixed` needs a number of digits from 0 to 20 but got %d.", d)
	}
	return strconv.FormatFloat(x, 'f', d, 64)
}

// ---- lists

type arrayMethod func(it *interp, a *Array, args []any, at pos) any

func clampIndex(i, n int) int {
	if i < 0 {
		i += n
		if i < 0 {
			i = 0
		}
	}
	if i > n {
		i = n
	}
	return i
}

func (it *interp) callback(fn any, at pos, args ...any) any { return it.call(fn, args, at) }

func (it *interp) mustBool(v any, at pos, fn string) bool {
	b, ok := v.(bool)
	if !ok {
		it.fail(at, "the function given to `%s` must return a yes/no but returned %s. Remedy: return a comparison such as `x > 0`.", fn, article(v))
	}
	return b
}

var arrayMethods map[string]arrayMethod

func arrayMethodNames() []string {
	out := make([]string, 0, len(arrayMethods))
	for n := range arrayMethods {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func init() {
	arrayMethods = map[string]arrayMethod{
		"push": func(it *interp, a *Array, args []any, at pos) any {
			it.charge(at, 8*len(args))
			a.elems = append(a.elems, args...)
			return float64(len(a.elems))
		},
		"pop": func(it *interp, a *Array, args []any, at pos) any {
			if len(a.elems) == 0 {
				return nil
			}
			v := a.elems[len(a.elems)-1]
			a.elems = a.elems[:len(a.elems)-1]
			return v
		},
		"shift": func(it *interp, a *Array, args []any, at pos) any {
			if len(a.elems) == 0 {
				return nil
			}
			v := a.elems[0]
			a.elems = a.elems[1:]
			return v
		},
		"unshift": func(it *interp, a *Array, args []any, at pos) any {
			it.charge(at, 8*(len(a.elems)+len(args)))
			a.elems = append(append([]any(nil), args...), a.elems...)
			return float64(len(a.elems))
		},
		"slice": func(it *interp, a *Array, args []any, at pos) any {
			n := len(a.elems)
			from := clampIndex(it.argInt("slice", args, 0, 0), n)
			to := clampIndex(it.argInt("slice", args, 1, n), n)
			out := &Array{}
			if from < to {
				out.elems = append(out.elems, a.elems[from:to]...)
			}
			it.charge(at, 8*len(out.elems)+24)
			return out
		},
		"splice": func(it *interp, a *Array, args []any, at pos) any {
			n := len(a.elems)
			from := clampIndex(it.argInt("splice", args, 0, 0), n)
			del := it.argInt("splice", args, 1, n-from)
			if del < 0 {
				del = 0
			}
			if from+del > n {
				del = n - from
			}
			removed := &Array{elems: append([]any(nil), a.elems[from:from+del]...)}
			var ins []any
			if len(args) > 2 {
				ins = args[2:]
			}
			it.charge(at, 8*(n+len(ins)))
			rest := append(append([]any(nil), ins...), a.elems[from+del:]...)
			a.elems = append(a.elems[:from:from], rest...)
			return removed
		},
		"concat": func(it *interp, a *Array, args []any, at pos) any {
			out := &Array{elems: append([]any(nil), a.elems...)}
			for _, x := range args {
				if o, ok := x.(*Array); ok {
					out.elems = append(out.elems, o.elems...)
				} else {
					out.elems = append(out.elems, x)
				}
			}
			it.charge(at, 8*len(out.elems)+24)
			return out
		},
		"join": func(it *interp, a *Array, args []any, at pos) any {
			sep := ","
			if len(args) > 0 {
				sep = it.argStr("join", args, 0)
			}
			parts := make([]string, len(a.elems))
			total := 0
			for i, e := range a.elems {
				parts[i] = text(e)
				total += len(parts[i]) + len(sep)
				it.checkString(at, total)
			}
			it.charge(at, total+16)
			return strings.Join(parts, sep)
		},
		"reverse": func(it *interp, a *Array, args []any, at pos) any {
			for i, j := 0, len(a.elems)-1; i < j; i, j = i+1, j-1 {
				a.elems[i], a.elems[j] = a.elems[j], a.elems[i]
			}
			return a
		},
		"indexOf": func(it *interp, a *Array, args []any, at pos) any {
			for i, e := range a.elems {
				if strictEqual(e, it.arg(args, 0)) {
					return float64(i)
				}
			}
			return -1.0
		},
		"includes": func(it *interp, a *Array, args []any, at pos) any {
			for _, e := range a.elems {
				if strictEqual(e, it.arg(args, 0)) {
					return true
				}
			}
			return false
		},
		"map": func(it *interp, a *Array, args []any, at pos) any {
			fn := it.argFn("map", args, 0)
			out := &Array{}
			snap := append([]any(nil), a.elems...)
			for i, e := range snap {
				out.elems = append(out.elems, it.callback(fn, at, e, float64(i)))
				it.charge(at, 8)
			}
			return out
		},
		"filter": func(it *interp, a *Array, args []any, at pos) any {
			fn := it.argFn("filter", args, 0)
			out := &Array{}
			snap := append([]any(nil), a.elems...)
			for i, e := range snap {
				if it.mustBool(it.callback(fn, at, e, float64(i)), at, "filter") {
					out.elems = append(out.elems, e)
					it.charge(at, 8)
				}
			}
			return out
		},
		"forEach": func(it *interp, a *Array, args []any, at pos) any {
			fn := it.argFn("forEach", args, 0)
			snap := append([]any(nil), a.elems...)
			for i, e := range snap {
				it.callback(fn, at, e, float64(i))
			}
			return nil
		},
		"find": func(it *interp, a *Array, args []any, at pos) any {
			fn := it.argFn("find", args, 0)
			for i, e := range append([]any(nil), a.elems...) {
				if it.mustBool(it.callback(fn, at, e, float64(i)), at, "find") {
					return e
				}
			}
			return nil
		},
		"findIndex": func(it *interp, a *Array, args []any, at pos) any {
			fn := it.argFn("findIndex", args, 0)
			for i, e := range append([]any(nil), a.elems...) {
				if it.mustBool(it.callback(fn, at, e, float64(i)), at, "findIndex") {
					return float64(i)
				}
			}
			return -1.0
		},
		"some": func(it *interp, a *Array, args []any, at pos) any {
			fn := it.argFn("some", args, 0)
			for i, e := range append([]any(nil), a.elems...) {
				if it.mustBool(it.callback(fn, at, e, float64(i)), at, "some") {
					return true
				}
			}
			return false
		},
		"every": func(it *interp, a *Array, args []any, at pos) any {
			fn := it.argFn("every", args, 0)
			for i, e := range append([]any(nil), a.elems...) {
				if !it.mustBool(it.callback(fn, at, e, float64(i)), at, "every") {
					return false
				}
			}
			return true
		},
		"reduce": func(it *interp, a *Array, args []any, at pos) any {
			fn := it.argFn("reduce", args, 0)
			snap := append([]any(nil), a.elems...)
			var acc any
			start := 0
			if len(args) > 1 {
				acc = args[1]
			} else {
				if len(snap) == 0 {
					it.fail(at, "`reduce` of an empty list needs a starting value. Remedy: write `list.reduce((acc, x) => ..., start)`.")
				}
				acc, start = snap[0], 1
			}
			for i := start; i < len(snap); i++ {
				acc = it.callback(fn, at, acc, snap[i], float64(i))
			}
			return acc
		},
		"sort": func(it *interp, a *Array, args []any, at pos) any {
			var less func(x, y any) bool
			if len(args) > 0 {
				fn := it.argFn("sort", args, 0)
				less = func(x, y any) bool {
					r := it.callback(fn, at, x, y)
					f, ok := r.(float64)
					if !ok {
						it.fail(at, "the function given to `sort` must return a number (negative, 0, positive) but returned %s. Remedy: write `(a, b) => a - b`, or `a.name < b.name ? -1 : 1` for texts.", article(r))
					}
					return f < 0
				}
			} else {
				allNum, allStr := true, true
				for _, e := range a.elems {
					_, isN := e.(float64)
					_, isS := e.(string)
					allNum = allNum && isN
					allStr = allStr && isS
				}
				switch {
				case allNum:
					less = func(x, y any) bool { return x.(float64) < y.(float64) }
				case allStr:
					less = func(x, y any) bool { return x.(string) < y.(string) }
				default:
					it.fail(at, "`sort` without a function needs a list of only numbers or only texts. Remedy: pass how to compare: `list.sort((a, b) => a.n - b.n)`.")
				}
			}
			sort.SliceStable(a.elems, func(i, j int) bool { return less(a.elems[i], a.elems[j]) })
			return a
		},
	}
}

// ---- texts

type stringMethod func(it *interp, s string, args []any, at pos) any

var stringMethods map[string]stringMethod

func stringMethodNames() []string {
	out := make([]string, 0, len(stringMethods))
	for n := range stringMethods {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func init() {
	stringMethods = map[string]stringMethod{
		"slice": func(it *interp, s string, args []any, at pos) any {
			rs := []rune(s)
			from := clampIndex(it.argInt("slice", args, 0, 0), len(rs))
			to := clampIndex(it.argInt("slice", args, 1, len(rs)), len(rs))
			if from >= to {
				return ""
			}
			it.charge(at, 4*(to-from)+16)
			return string(rs[from:to])
		},
		"indexOf": func(it *interp, s string, args []any, at pos) any {
			i := strings.Index(s, it.argStr("indexOf", args, 0))
			if i < 0 {
				return -1.0
			}
			return float64(len([]rune(s[:i])))
		},
		"includes": func(it *interp, s string, args []any, at pos) any {
			return strings.Contains(s, it.argStr("includes", args, 0))
		},
		"startsWith": func(it *interp, s string, args []any, at pos) any {
			return strings.HasPrefix(s, it.argStr("startsWith", args, 0))
		},
		"endsWith": func(it *interp, s string, args []any, at pos) any {
			return strings.HasSuffix(s, it.argStr("endsWith", args, 0))
		},
		"split": func(it *interp, s string, args []any, at pos) any {
			sep := it.argStr("split", args, 0)
			var parts []string
			if sep == "" {
				for _, r := range s {
					parts = append(parts, string(r))
				}
			} else {
				parts = strings.Split(s, sep)
			}
			out := &Array{}
			for _, p := range parts {
				out.elems = append(out.elems, p)
			}
			it.charge(at, 24*len(parts)+len(s)+24)
			return out
		},
		"trim":        func(it *interp, s string, args []any, at pos) any { return strings.TrimSpace(s) },
		"toUpperCase": func(it *interp, s string, args []any, at pos) any { return strings.ToUpper(s) },
		"toLowerCase": func(it *interp, s string, args []any, at pos) any { return strings.ToLower(s) },
		"repeat": func(it *interp, s string, args []any, at pos) any {
			n := it.argInt("repeat", args, 0, 0)
			if n < 0 {
				it.fail(at, "`repeat` needs a count of 0 or more but got %d.", n)
			}
			it.checkString(at, len(s)*n)
			it.charge(at, len(s)*n+16)
			return strings.Repeat(s, n)
		},
		"padStart": func(it *interp, s string, args []any, at pos) any { return pad(it, s, args, at, true) },
		"padEnd":   func(it *interp, s string, args []any, at pos) any { return pad(it, s, args, at, false) },
		"replace": func(it *interp, s string, args []any, at pos) any {
			it.checkString(at, len(s))
			return strings.Replace(s, it.argStr("replace", args, 0), it.argStr("replace", args, 1), 1)
		},
		"replaceAll": func(it *interp, s string, args []any, at pos) any {
			from := it.argStr("replaceAll", args, 0)
			if from == "" {
				it.fail(at, "`replaceAll` needs a non-empty text to look for.")
			}
			r := strings.ReplaceAll(s, from, it.argStr("replaceAll", args, 1))
			it.checkString(at, len(r))
			it.charge(at, len(r)+16)
			return r
		},
	}
}

func pad(it *interp, s string, args []any, at pos, start bool) any {
	name := "padEnd"
	if start {
		name = "padStart"
	}
	n := it.argInt(name, args, 0, 0)
	fill := " "
	if len(args) > 1 {
		fill = it.argStr(name, args, 1)
	}
	have := len([]rune(s))
	if n <= have || fill == "" {
		return s
	}
	it.checkString(at, len(s)+(n-have)*len(fill))
	var sb strings.Builder
	fr := []rune(fill)
	for i := 0; i < n-have; i++ {
		sb.WriteRune(fr[i%len(fr)])
	}
	it.charge(at, sb.Len()+len(s)+16)
	if start {
		return sb.String() + s
	}
	return s + sb.String()
}
