package script

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// run loads src and calls main(); a test of the language is a main() that returns a value.
func run(t *testing.T, src string) any {
	t.Helper()
	v, err := Eval(src, Config{Name: "t.js"})
	if err != nil {
		t.Fatalf("the script failed: %v\n--- script ---\n%s", err, src)
	}
	return v
}

func runCfg(src string, cfg Config) (any, error) {
	cfg.Name = "t.js"
	return Eval(src, cfg)
}

func want(t *testing.T, src string, exp any) {
	t.Helper()
	got := run(t, src)
	if !reflect.DeepEqual(got, exp) {
		t.Errorf("got %#v, want %#v for:\n%s", got, exp, src)
	}
}

// failsWith checks the message of a refusal: it must say what is wrong AND what to write
// instead, which is the repair loop of the open interface.
func failsWith(t *testing.T, src string, kind Kind, parts ...string) *Error {
	t.Helper()
	return failsCfg(t, src, Config{}, kind, parts...)
}

func failsCfg(t *testing.T, src string, cfg Config, kind Kind, parts ...string) *Error {
	t.Helper()
	_, err := runCfg(src, cfg)
	if err == nil {
		t.Fatalf("the script was accepted, want a %s error with %q:\n%s", kind, parts, src)
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error is %T (%v), want *Error", err, err)
	}
	if e.Kind != kind {
		t.Errorf("kind = %s, want %s: %v", e.Kind, kind, e)
	}
	for _, p := range parts {
		if !strings.Contains(e.Error(), p) {
			t.Errorf("the message lacks %q.\nConsequence: the model cannot repair what it cannot read.\nRemedy: say it in the message.\nmessage: %v", p, e)
		}
	}
	return e
}

func TestArithmeticAndTexts(t *testing.T) {
	want(t, `function main() { return 1 + 2 * 3 - 4 / 2 }`, 5.0)
	want(t, `function main() { return 2 ** 3 ** 2 }`, 512.0)
	want(t, `function main() { return 7 % 3 }`, 1.0)
	want(t, `function main() { return -(-3) }`, 3.0)
	want(t, "function main() { const n = 3; return `n=${n + 1} ${'x'}` }", "n=4 x")
	want(t, `function main() { return 'a' + 'b' }`, "ab")
	want(t, `function main() { return 'héllo'.length }`, 5.0)
	want(t, `function main() { return 'héllo'.slice(1, 3) }`, "él")
	want(t, `function main() { return 'a,b,c'.split(',').join('-') }`, "a-b-c")
	want(t, `function main() { return (5).toFixed(2) }`, "5.00")
	want(t, `function main() { return 'ab'.padStart(5, '*') + 'ab'.padEnd(4, '.') }`, "***abab..")
	want(t, `function main() { return 0x1F + 1_000 }`, 1031.0)
}

func TestVariablesScopesAndClosures(t *testing.T) {
	want(t, `
function main() {
  let n = 0
  const inc = () => { n += 1; return n }
  inc(); inc()
  return n
}`, 2.0)
	// a closure made in a for loop keeps the value of its own round
	want(t, `
function main() {
  const fs = []
  for (let i = 0; i < 3; i++) { fs.push(() => i) }
  return fs.map(f => f())
}`, []any{0.0, 1.0, 2.0})
	want(t, `
function main() {
  let x = 1
  { let x = 2 }
  return x
}`, 1.0)
	want(t, `
const base = 10
function add(n) { return n + base }
function main() { return add(5) }`, 15.0)
	// a function may be used before the line that declares it
	want(t, `function main() { return later() }
function later() { return 'ok' }`, "ok")
}

func TestControlFlow(t *testing.T) {
	want(t, `
function main() {
  let s = 0
  for (let i = 0; i < 10; i++) {
    if (i % 2 === 0) { continue }
    if (i > 7) { break }
    s += i
  }
  return s
}`, 16.0)
	want(t, `
function main() {
  let n = 0
  while (n < 5) { n++ }
  return n
}`, 5.0)
	want(t, `
function main() {
  const out = []
  for (const [k, v] of Object.entries({ a: 1, b: 2 })) { out.push(k + String(v)) }
  return out.join('')
}`, "a1b2")
	want(t, `function main() { return 3 > 2 ? 'big' : 'small' }`, "big")
	want(t, `function main() { for (const c of 'ab') { if (c === 'b') { return c } } return null }`, "b")
}

func TestListsObjectsAndDestructuring(t *testing.T) {
	want(t, `
function main() {
  const q = [3, 1, 2]
  q.push(5)
  const first = q.shift()
  return [first, q.length, q.sort((a, b) => a - b).join('')]
}`, []any{3.0, 3.0, "125"})
	want(t, `
function main() {
  const o = { a: 1, b: { c: 2 }, ['k' + String(1)]: 3 }
  const { a, b: { c }, k1 } = o
  const [x, , z = 9, ...rest] = [1, 2, null, 4, 5]
  return [a, c, k1, x, z, rest]
}`, []any{1.0, 2.0, 3.0, 1.0, 9.0, []any{4.0, 5.0}})
	want(t, `
function main() {
  const base = { a: 1, b: 2 }
  const o = { ...base, b: 3 }
  const l = [...[1, 2], 3]
  return [o.a, o.b, l.length]
}`, []any{1.0, 3.0, 3.0})
	want(t, `
function main() {
  const o = { n: 1, bump() { o.n += 1; return o.n } }
  o.bump()
  return o.bump()
}`, 3.0)
	want(t, `
function main() {
  const xs = [1, 2, 3, 4]
  return [xs.filter(x => x > 2).length, xs.reduce((a, x) => a + x, 0), xs.some(x => x === 3), xs.find(x => x > 1), xs.indexOf(4), xs.includes(9)]
}`, []any{2.0, 10.0, true, 2.0, 3.0, false})
	want(t, `function main() { const o = { a: null }; return [o.a ?? 'd', o?.b?.c ?? 'none', o.zz === null] }`, []any{"d", "none", true})
	want(t, `function main() { const f = (a, b = 2, ...r) => a + b + r.length; return f(1) + f(1, 1, 7, 8) }`, 7.0)
}

func TestKeysComeBackInTheOrderTheyWereSet(t *testing.T) {
	want(t, `function main() { return Object.keys({ z: 1, a: 2, m: 3 }).join('') }`, "zam")
}

func TestJSONAndNumber(t *testing.T) {
	want(t, `function main() { return JSON.stringify({ a: [1, 'x', null, true] }) }`, `{"a":[1,"x",null,true]}`)
	want(t, `function main() { const o = JSON.parse('{"n": 3, "l": [1,2]}'); return o.n + o.l.length }`, 5.0)
	want(t, `function main() { return JSON.parse('not json') === null }`, true)
	want(t, `function main() { return [Number('12.5'), Number('x') === null, Number.isInteger(3), Math.max(1, 9, 4), Math.round(2.5), Math.floor(-1.5)] }`,
		[]any{12.5, true, true, 9.0, 3.0, -2.0})
}

// ---- the host side

func TestHostFunctionsAndGlobalsAreTheOnlyDoor(t *testing.T) {
	var got []any
	cfg := Config{Globals: map[string]any{
		"emit":  HostFunc(func(args []any) (any, error) { got = append(got, args...); return float64(len(args)), nil }),
		"width": 80,
		"names": []string{"a", "b"},
	}}
	v, err := runCfg(`function main() { return emit('x', width, names) + width }`, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if v != 83.0 {
		t.Errorf("got %v, want 83", v)
	}
	if !reflect.DeepEqual(got, []any{"x", 80.0, []any{"a", "b"}}) {
		t.Errorf("the host got %#v", got)
	}
}

func TestAHostErrorIsARuntimeErrorAtTheLineOfTheCall(t *testing.T) {
	cfg := Config{Globals: map[string]any{
		"boom": HostFunc(func(args []any) (any, error) { return nil, errors.New("the file is gone") }),
	}}
	e := failsCfg(t, "function main() {\n  return boom()\n}", cfg, KindRuntime, "`boom` failed: the file is gone")
	if e.Line != 2 {
		t.Errorf("line = %d, want 2 (the line of the call). Consequence: the model edits the wrong line.", e.Line)
	}
}

func TestAHostGlobalIsReadOnly(t *testing.T) {
	cfg := Config{Globals: map[string]any{"width": 80}}
	failsCfg(t, `function main() { width = 3 }`, cfg, KindSyntax, "`width` is given by the host and read-only", "Remedy")
}

func TestStateSurvivesBetweenCallsAndAFailedCallKeepsWhatItDid(t *testing.T) {
	s, err := Load(`
let queue = []
function push(x) { queue.push(x); return queue.length }
function crash() { queue.push('half'); return 1 / 0 }
function size() { return queue.length }`, Config{Name: "q.js"})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []string{"a", "b"} {
		if _, err := s.Call("push", x); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Call("crash"); err == nil {
		t.Fatal("crash() succeeded")
	}
	// The call that failed stopped, and the script is still usable: one failing event must
	// not take the keymap down with it.
	n, err := s.Call("size")
	if err != nil || n != 3.0 {
		t.Errorf("size() = %v, %v; want 3, nil. Consequence: a script that fails once is dead for the rest of the session.", n, err)
	}
}

func TestCallingAFunctionThatIsNotThereNamesTheNearestOne(t *testing.T) {
	s, _ := Load(`function onKey() { return 1 }`, Config{Name: "k.js"})
	_, err := s.Call("onKye")
	if err == nil || !strings.Contains(err.Error(), "Did you mean `onKey`") || !strings.Contains(err.Error(), "Remedy") {
		t.Errorf("err = %v", err)
	}
	if !s.Has("onKey") || s.Has("onKye") {
		t.Error("Has is wrong")
	}
}

func TestAFunctionCannotLeaveTheScript(t *testing.T) {
	s, _ := Load(`function f() { return () => 1 }`, Config{Name: "k.js"})
	_, err := s.Call("f")
	if err == nil || !strings.Contains(err.Error(), "cannot be returned to the host") {
		t.Errorf("err = %v", err)
	}
}

// ---- what is refused, with the remedy

func TestWhatJavaScriptHasAndThisDoesNotIsRefusedWithWhatToUse(t *testing.T) {
	for _, c := range []struct{ src, word, remedy string }{
		{`class A {}`, "`class` is not available", "use an object with functions"},
		{`function main() { return new Map() }`, "`new` is not available", "call a function that returns the value"},
		{`function main() { return this.x }`, "`this` is not available", "name the object"},
		{`async function f() {}`, "`async` is not available", "write a plain function"},
		{`function main() { try { return 1 } catch (e) { return 2 } }`, "`try` is not available", "check the value before using it"},
		{`function main() { throw 1 }`, "`throw` is not available", "return a value that says what went wrong"},
		{`function main() { switch (1) { case 1: return 1 } }`, "`switch` is not available", "if (...) {...} else if"},
		{`var x = 1`, "`var` is not available", "use `let`"},
		{`function main() { do { } while (true) }`, "`do ... while` is not available", "use `while"},
		{`function main() { const o = {}; delete o.a }`, "`delete` is not available", "build the object again"},
		{`import x from 'y'`, "`import` is not available", "globals listed by the host"},
		{`function main() { return 1 in {} }`, "`in` is not available", "Object.keys"},
		{`function main() { return [] instanceof Array }`, "`instanceof` is not available", "Array.isArray"},
		{`function* g() {}`, "generators are not available", "one step per call"},
	} {
		t.Run(c.word, func(t *testing.T) {
			e := failsWith(t, c.src, KindSyntax, c.word, c.remedy)
			if e.Line != 1 || e.Col == 0 {
				t.Errorf("no position: %+v. Consequence: the model cannot find the line it must change.", e)
			}
		})
	}
}

func TestGlobalsThatDoNotExistAreRefusedWithWhatToUse(t *testing.T) {
	for name, remedy := range map[string]string{
		"console":    "return the text",
		"setTimeout": "have the host call your function",
		"Date":       "no clock",
		"Map":        "object with text keys",
		"parseInt":   "Number(text)",
		"RegExp":     "startsWith",
		"Promise":    "never waits",
	} {
		t.Run(name, func(t *testing.T) {
			failsWith(t, "function main() { return "+name+" }", KindSyntax, "`"+name+"` is not available", remedy)
		})
	}
}

func TestLooseEqualityAndBitwiseOperatorsAreRefused(t *testing.T) {
	failsWith(t, `function main() { return 1 == 1 }`, KindSyntax, "`==` is not available", "use `===`")
	failsWith(t, `function main() { return 1 != 2 }`, KindSyntax, "`!=` is not available", "use `!==`")
	failsWith(t, `function main() { return 1 | 2 }`, KindSyntax, "bitwise operator `|`", "use arithmetic")
	failsWith(t, `function main() { return 1 << 2 }`, KindSyntax, "bitwise operator `<<`", "arithmetic")
}

func TestATypeofComparisonThatCanNeverBeTrueIsRefused(t *testing.T) {
	failsWith(t, `function main() { return typeof 1 === 'undefined' }`, KindSyntax, "never gives 'undefined'", "x === null")
	failsWith(t, `function main() { return 'Number' === typeof 1 }`, KindSyntax, "never gives 'Number'", "one of")
	want(t, `function main() { return typeof 1 === 'number' && typeof [] === 'array' && typeof null === 'null' }`, true)
}

func TestAnUndefinedNameIsRefusedAtLoadWithTheNearestNames(t *testing.T) {
	cfg := Config{Globals: map[string]any{"emitLine": HostFunc(func([]any) (any, error) { return nil, nil })}}
	e := failsCfg(t, "function never() {\n  return emitLin('x')\n}\nfunction main() { return 1 }", cfg, KindSyntax,
		"`emitLin` is not defined", "Did you mean `emitLine`", "or use a name the host gives: emitLine")
	if e.Line != 2 {
		t.Errorf("line = %d, want 2. Consequence: a typo in a function nobody calls yet is found when a key is pressed.", e.Line)
	}
}

func TestAConstCannotBeAssignedAndTheMessageSaysWhatToUse(t *testing.T) {
	failsWith(t, `function main() { const n = 1; n = 2 }`, KindSyntax, "`n` is a const", "declare it with `let`")
	failsWith(t, `function f() {}  function main() { f = 1 }`, KindSyntax, "`f` is a function", "Remedy")
	failsWith(t, `function main() { let a = 1; let a = 2 }`, KindSyntax, "`a` is already declared", "use another name")
}

func TestBreakOutsideALoopIsRefusedAndAFunctionInALoopStartsOver(t *testing.T) {
	failsWith(t, `function main() { break }`, KindSyntax, "`break` is outside a loop", "return")
	failsWith(t, `function main() { for (let i = 0; i < 1; i++) { const f = () => { break } } }`, KindSyntax, "`break` is outside a loop")
}

func TestSyntaxErrorsSayWhatWasFoundAndWhatToAdd(t *testing.T) {
	failsWith(t, `function main() { return (1 + 2 }`, KindSyntax, "expected `)`", "to close the parenthesis", "add the `)`")
	failsWith(t, `function main() { return [1, 2 }`, KindSyntax, "expected `]`")
	failsWith(t, `function main() { if (true) { return 1 }`, KindSyntax, "this `{` is never closed", "matching `}`")
	failsWith(t, "function main() { return 'abc }", KindSyntax, "never closed")
	failsWith(t, "function main() { return `abc }", KindSyntax, "template text is never closed")
	failsWith(t, "function main() { return `a${}` }", KindSyntax, "this ${} is empty")
	failsWith(t, `function main() { return 3x }`, KindSyntax, "followed by letters")
	failsWith(t, `function main() { return @ }`, KindSyntax, "not part of the language")
	failsWith(t, `function main() { let x = 1 2 }`, KindSyntax, "expected `;` or a new line", "start the number 2 on a new line")
	failsWith(t, `const x`, KindSyntax, "a `const` needs a value")
}

func TestErrorsSayWhereAsScriptNameLineColumn(t *testing.T) {
	_, err := Eval("function main() {\n  return 1 +\n}", Config{Name: "ui/keys.js"})
	if err == nil || !strings.HasPrefix(err.Error(), "ui/keys.js:3:1: syntax error:") {
		t.Errorf("err = %v, want it to start with `ui/keys.js:3:1: syntax error:`", err)
	}
}

// ---- strict types: the silent mistakes of JavaScript are errors here

func TestThereIsNoTruthiness(t *testing.T) {
	failsWith(t, `function main() { if (1) { return 1 } }`, KindRuntime, "the condition of `if` is a number, not a yes/no", "n !== 0")
	failsWith(t, `function main() { if ('') { return 1 } }`, KindRuntime, "is a text, not a yes/no", "s !== ''")
	failsWith(t, `function main() { const x = null; if (x) { return 1 } }`, KindRuntime, "is null, not a yes/no", "x !== null")
	failsWith(t, `function main() { return !'a' }`, KindRuntime, "`!` needs a yes/no", "s === ''")
	failsWith(t, `function main() { return 1 && true }`, KindRuntime, "`&&` needs a yes/no on its left", "x ?? default")
	failsWith(t, `function main() { return true && 'a' }`, KindRuntime, "`&&` needs a yes/no on its right", "cond ? a : b")
	failsWith(t, `function main() { while (1) {} }`, KindRuntime, "condition of `while`")
	failsWith(t, `function main() { return 1 ? 2 : 3 }`, KindRuntime, "condition of `? :`")
}

func TestThereAreNoSilentConversions(t *testing.T) {
	failsWith(t, `function main() { return 'a' + 1 }`, KindRuntime, "`+` joins texts only with texts", "template text")
	failsWith(t, `function main() { return 1 + 'a' }`, KindRuntime, "`+` adds numbers only to numbers", "template text")
	failsWith(t, `function main() { return 1 - 'a' }`, KindRuntime, "`-` needs numbers", "Number(text)")
	failsWith(t, `function main() { return null + 1 }`, KindRuntime, "`+` needs numbers", "null")
	failsWith(t, `function main() { return 1 < 'a' }`, KindRuntime, "`<` needs numbers")
	failsWith(t, `function main() { return 'a' < 1 }`, KindRuntime, "compares a text only with a text")
	failsWith(t, `function main() { return -'a' }`, KindRuntime, "unary `-` needs a number")
	want(t, `function main() { return [1 === '1', null === 0, [] === [], 'a' === 'a'] }`, []any{false, false, false, true})
}

func TestANumberIsNeverNaNOrInfinity(t *testing.T) {
	failsWith(t, `function main() { return 1 / 0 }`, KindRuntime, "division by zero", "check the divisor")
	failsWith(t, `function main() { return 5 % 0 }`, KindRuntime, "division by zero")
	failsWith(t, `function main() { return 10 ** 1000 }`, KindRuntime, "not a finite number")
	failsWith(t, `function main() { return Math.sqrt(-1) }`, KindRuntime, "not a finite number")
}

func TestReadingThroughNullSaysHowToAskInstead(t *testing.T) {
	failsWith(t, `function main() { const o = null; return o.a }`, KindRuntime, "cannot read `.a` of `o` because it is null", "o?.a")
	failsWith(t, `function main() { const o = {}; return o.a.b }`, KindRuntime, "cannot read `.b` of `o.a`")
	failsWith(t, `function main() { const l = null; return l[0] }`, KindRuntime, "cannot read an element of `l`", "l?.[i]")
	failsWith(t, `function main() { const o = {}; return o.f() }`, KindRuntime, "`o.f` is not a function", "followed by")
	want(t, `function main() { const o = {}; return o.a?.b === null && o.f?.() === null }`, true)
}

func TestAMisspeltMethodNamesTheNearestOnes(t *testing.T) {
	failsWith(t, `function main() { return [1].pussh(2) }`, KindRuntime, "lists have no `.pussh`", "Did you mean `push`")
	failsWith(t, `function main() { return 'a'.lenght }`, KindRuntime, "texts have no `.lenght`", "Did you mean `length`")
	failsWith(t, `function main() { return 'a'.toUppercase() }`, KindRuntime, "Did you mean `toUpperCase`")
}

func TestListIndexesAreChecked(t *testing.T) {
	want(t, `function main() { return [1][5] === null }`, true) // reading past the end is null, like a missing key
	failsWith(t, `function main() { const l = [1]; l[5] = 2 }`, KindRuntime, "index 5 is outside `l`", "list.push(x)")
	failsWith(t, `function main() { return [1][0.5] }`, KindRuntime, "must be a whole number", "Math.floor")
	failsWith(t, `function main() { const l = [1]; l['a'] = 1 }`, KindRuntime, "must be a whole number")
}

func TestUsingALetBeforeItsLineHasRunIsAnError(t *testing.T) {
	failsWith(t, `function main() { const a = b + 1; const b = 2; return a }`, KindRuntime, "`b` is used before the line that declares it", "move the `let`/`const`")
	// an outer name of the same spelling must not be found instead
	failsWith(t, `const b = 1
function main() { const a = b; const b = 2; return a }`, KindRuntime, "`b` is used before")
}

func TestTemplateTextsPrintListsAndObjectsAsJSON(t *testing.T) {
	want(t, "function main() { return `${[1, 'a']} ${{ k: null }} ${true} ${1.5}` }", `[1,"a"] {"k":null} true 1.5`)
	want(t, `function main() { return String(1e21) + ' ' + String(0.000001) + ' ' + String(100) }`, "1e+21 0.000001 100")
}

// ---- limits: the reason this interpreter is written here

func TestAnEndlessLoopIsStoppedBySteps(t *testing.T) {
	start := time.Now()
	e := failsWith(t, `function main() { while (true) { } }`, KindLimit, "more than 200000 steps", "Consequence", "make the loop end")
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("stopping took %v. Consequence: a stuck script freezes the screen for that long. Remedy: lower the step limit or count cheaper.", d)
	}
	if e.Line != 1 {
		t.Errorf("line = %d", e.Line)
	}
	_, err := runCfg(`function main() { let n = 0; for (let i = 0; i < 100000; i++) { n += i } return n }`, Config{Limits: Limits{Steps: 1000}})
	if err == nil || !strings.Contains(err.Error(), "more than 1000 steps") {
		t.Errorf("a host limit was not used: %v", err)
	}
}

func TestEveryCallStartsWithFreshStepsSoALongSessionIsNotCut(t *testing.T) {
	s, _ := Load(`
let n = 0
function tick() { for (let i = 0; i < 1000; i++) { n += 1 } return n }`, Config{Name: "t.js", Limits: Limits{Steps: 20000}})
	for i := 0; i < 50; i++ {
		if _, err := s.Call("tick"); err != nil {
			t.Fatalf("call %d: %v. Consequence: the budget is spent over the session and the script dies of old age. Remedy: reset the counters in run().", i, err)
		}
	}
}

func TestRunawayRecursionIsStoppedByDepthBeforeTheGoStackIsHurt(t *testing.T) {
	failsWith(t, `function f(n) { return f(n + 1) }
function main() { return f(0) }`, KindLimit, "nested more than 64 deep", "use a loop")
}

func TestManyCallsOneAfterTheOtherAreNotNesting(t *testing.T) {
	// Consequence if the depth is never given back: the 65th call in a loop is refused as
	// "nested too deep" although none is inside another, so any loop over a helper dies.
	// Remedy: the depth counts calls that are open now, not calls made so far.
	want(t, `function inc(x) { return x + 1 }
function main() { let n = 0; for (let i = 0; i < 1000; i++) { n = inc(n) } return n }`, 1000.0)
}

func TestYesAndNoAreOnlyEqualToThemselves(t *testing.T) {
	// Consequence if `true === false` were true for some pair: a guard such as
	// `if (busy === false)` would run while the agent is busy and drop a queued line.
	// Remedy: compare booleans by value, all four pairs.
	want(t, `function main() { return [true === true, false === false, true === false, false === true, true !== false, true === 1, false === 0, false === null] }`,
		[]any{true, true, false, false, true, false, false, false})
}

func TestAListCannotGrowWithoutEnd(t *testing.T) {
	failsWith(t, `function main() { const l = []; while (true) { l.push('x'.repeat(5000)) } }`, KindLimit, "allocated more than", "build smaller")
}

func TestATextCannotGrowWithoutEnd(t *testing.T) {
	_, err := runCfg(`function main() { let s = 'x'; while (true) { s = s + s } }`, Config{})
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindLimit {
		t.Fatalf("err = %v, want a limit error. Consequence: doubling a text takes all the memory in 30 steps.", err)
	}
	failsWith(t, `function main() { return 'x'.repeat(100000000) }`, KindLimit, "longer than the")
}

func TestTheStateKeptBetweenCallsIsBounded(t *testing.T) {
	s, err := Load(`
let log = []
function add(x) { log.push(x); return log.length }`, Config{Name: "log.js", Limits: Limits{Live: 4096}})
	if err != nil {
		t.Fatal(err)
	}
	var last error
	for i := 0; i < 200 && last == nil; i++ {
		_, last = s.Call("add", "a line of text that is not tiny at all")
	}
	if last == nil {
		t.Fatal("200 calls kept adding to the state and nothing stopped them.\nConsequence: a script that logs forever takes the memory of the machine, one cheap call at a time.\nRemedy: bound the state kept between calls.")
	}
	var e *Error
	if !errors.As(last, &e) || e.Kind != KindLimit || !strings.Contains(e.Msg, "state the script keeps") || !strings.Contains(e.Msg, "drop old entries") {
		t.Errorf("err = %v", last)
	}
}

func TestAnInternalPanicIsAnErrorNotACrash(t *testing.T) {
	s, _ := Load(`function f() { return 1 }`, Config{Name: "p.js"})
	err := s.run(func(it *interp) { var m map[string]int; m["a"] = 1 })
	if err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Errorf("err = %v. Consequence: a bug in the interpreter takes the whole program down.", err)
	}
}

// ---- the script touches nothing it was not given

func TestAScriptCannotReachAnythingTheHostDidNotGive(t *testing.T) {
	for _, name := range []string{"process", "require", "fs", "os", "fetch", "window", "global", "Deno"} {
		t.Run(name, func(t *testing.T) {
			_, err := runCfg("function main() { return "+name+" }", Config{})
			if err == nil {
				t.Fatalf("`%s` resolved. Consequence: a script reaches something the host never decided to give.", name)
			}
		})
	}
}

func TestValuesAreCopiedInSoAScriptCannotChangeTheHostsData(t *testing.T) {
	data := []any{"a", "b"}
	cfg := Config{Globals: map[string]any{"names": data}}
	if _, err := runCfg(`function main() { names.push('c'); return names.length }`, cfg); err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 {
		t.Errorf("the host's list changed: %v. Consequence: a script edits the host's state behind its back.", data)
	}
}

func TestAGoValueThatCannotEnterIsRefusedWithWhatToGive(t *testing.T) {
	_, err := Load(`function f() {}`, Config{Name: "x.js", Globals: map[string]any{"ch": make(chan int)}})
	if err == nil || !strings.Contains(err.Error(), "cannot enter a script") || !strings.Contains(err.Error(), "Remedy") {
		t.Errorf("err = %v", err)
	}
	_, err = Load(`function f() {}`, Config{Name: "x.js", Globals: map[string]any{"Math": 1}})
	if err == nil || !strings.Contains(err.Error(), "name of something the language gives") {
		t.Errorf("err = %v", err)
	}
}

// ---- a realistic script: the message queue the keymap uses today in Go

const queueScript = `
let waiting = []
const MAX = 3

function push(text) {
  if (text.trim() === '') { return { ok: false, why: 'empty' } }
  if (waiting.length >= MAX) { return { ok: false, why: ` + "`${MAX} lines are already waiting`" + ` } }
  waiting.push(text)
  return { ok: true, n: waiting.length }
}

function next() { return waiting.shift() ?? null }
function list() { return waiting.map((t, i) => ` + "`${i + 1}. ${t}`" + `) }
`

func TestTheMessageQueueAsAScript(t *testing.T) {
	s, err := Load(queueScript, Config{Name: "queue.js"})
	if err != nil {
		t.Fatal(err)
	}
	call := func(name string, a ...any) any {
		t.Helper()
		v, err := s.Call(name, a...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return v
	}
	if r := call("push", "  "); !reflect.DeepEqual(r, map[string]any{"ok": false, "why": "empty"}) {
		t.Errorf("empty: %v", r)
	}
	for _, x := range []string{"one", "two", "three"} {
		call("push", x)
	}
	if r := call("push", "four"); !reflect.DeepEqual(r, map[string]any{"ok": false, "why": "3 lines are already waiting"}) {
		t.Errorf("full: %v", r)
	}
	if got := call("list"); !reflect.DeepEqual(got, []any{"1. one", "2. two", "3. three"}) {
		t.Errorf("list: %v", got)
	}
	if got := call("next"); got != "one" {
		t.Errorf("next = %v, want one (first in, first out)", got)
	}
	call("next")
	call("next")
	if got := call("next"); got != nil {
		t.Errorf("next on an empty queue = %v, want null", got)
	}
}
