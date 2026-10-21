package expr

import (
	"fmt"
	"unicode/utf8"
)

func eval(n node, get Lookup) (Value, error) {
	switch n := n.(type) {
	case litNode:
		return n.v, nil
	case nameNode:
		v, ok := get(n.name)
		if !ok {
			return Value{}, fmt.Errorf("%s has no value right now, so the expression was not decided", n.name)
		}
		if v.K == Int && abs(v.I) > MaxInt {
			return Value{}, fmt.Errorf("%s is %d, past the largest number allowed (%d)", n.name, v.I, int64(MaxInt))
		}
		return v, nil
	case unNode:
		x, err := eval(n.x, get)
		if err != nil {
			return Value{}, err
		}
		if n.op == "!" {
			if x.K != Bool {
				return Value{}, kindErr(n.x, Bool, x)
			}
			return B(!x.B), nil
		}
		if x.K != Int {
			return Value{}, kindErr(n.x, Int, x)
		}
		return N(-x.I), nil
	case binNode:
		l, err := eval(n.l, get)
		if err != nil {
			return Value{}, err
		}
		// && and || stop early, so a name on the right is not read when the left decides.
		if n.op == "&&" || n.op == "||" {
			if l.K != Bool {
				return Value{}, kindErr(n.l, Bool, l)
			}
			if (n.op == "&&" && !l.B) || (n.op == "||" && l.B) {
				return l, nil
			}
			r, err := eval(n.r, get)
			if err != nil {
				return Value{}, err
			}
			if r.K != Bool {
				return Value{}, kindErr(n.r, Bool, r)
			}
			return r, nil
		}
		r, err := eval(n.r, get)
		if err != nil {
			return Value{}, err
		}
		return binEval(n.op, l, r)
	}
	return Value{}, fmt.Errorf("internal: unknown node")
}

// kindErr reports a host that gave a value of another kind than its schema promised.
func kindErr(n node, want Kind, got Value) error {
	who := "a value"
	if nn, ok := n.(nameNode); ok {
		who = nn.name
	}
	return fmt.Errorf("%s is a %s, and the schema said %s; the host and its schema disagree", who, got.K, want)
}

func binEval(op string, l, r Value) (Value, error) {
	if l.K != r.K {
		return Value{}, fmt.Errorf("%s got a %s and a %s; the host and its schema disagree", op, l.K, r.K)
	}
	switch op {
	case "==":
		return B(l == r), nil
	case "!=":
		return B(l != r), nil
	}
	if l.K == String {
		if op != "+" {
			return Value{}, fmt.Errorf("%s does not work on texts", op)
		}
		if utf8.RuneCountInString(l.S)+utf8.RuneCountInString(r.S) > MaxStringRunes {
			return Value{}, fmt.Errorf("the joined text is longer than %d characters", MaxStringRunes)
		}
		return S(l.S + r.S), nil
	}
	if l.K != Int {
		return Value{}, fmt.Errorf("%s does not work on yes/no values", op)
	}
	a, b := l.I, r.I
	switch op {
	case "<":
		return B(a < b), nil
	case "<=":
		return B(a <= b), nil
	case ">":
		return B(a > b), nil
	case ">=":
		return B(a >= b), nil
	}
	var v int64
	switch op {
	case "+":
		v = a + b
	case "-":
		v = a - b
	case "*":
		if a != 0 && b != 0 && abs(b) > MaxInt/abs(a) {
			return Value{}, fmt.Errorf("%d * %d is past the largest number allowed (%d)", a, b, int64(MaxInt))
		}
		v = a * b
	case "/", "%":
		if b == 0 {
			return Value{}, fmt.Errorf("%d %s 0: dividing by zero has no answer; check the divisor first", a, op)
		}
		if op == "/" {
			v = a / b
		} else {
			v = a % b
		}
	}
	if abs(v) > MaxInt {
		return Value{}, fmt.Errorf("the result is past the largest number allowed (%d)", int64(MaxInt))
	}
	return N(v), nil
}

func abs(a int64) int64 {
	if a < 0 {
		return -a
	}
	return a
}
