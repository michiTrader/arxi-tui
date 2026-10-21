package expr

import (
	"fmt"
	"sort"
	"strings"
)

type checker struct {
	schema Schema
	src    string
	used   map[string]bool
}

func (c *checker) check(n node) (Kind, error) {
	switch n := n.(type) {
	case litNode:
		return n.v.K, nil
	case nameNode:
		k, ok := c.schema[n.name]
		if !ok {
			return 0, fmt.Errorf("at %d: %q is not a name this place knows%s", n.pos+1, n.name, nearest(n.name, c.schema))
		}
		c.used[n.name] = true
		return k, nil
	case unNode:
		k, err := c.check(n.x)
		if err != nil {
			return 0, err
		}
		switch {
		case n.op == "!" && k != Bool:
			return 0, fmt.Errorf("at %d: ! turns a yes/no around, and this is a %s; compare it first, for example !(x == 0)", n.pos+1, k)
		case n.op == "-" && k != Int:
			return 0, fmt.Errorf("at %d: - negates a number, and this is a %s", n.pos+1, k)
		}
		return k, nil
	case binNode:
		l, err := c.check(n.l)
		if err != nil {
			return 0, err
		}
		r, err := c.check(n.r)
		if err != nil {
			return 0, err
		}
		return binKind(n, l, r)
	}
	return 0, fmt.Errorf("internal: unknown node")
}

func binKind(n binNode, l, r Kind) (Kind, error) {
	at := n.pos + 1
	switch n.op {
	case "&&", "||":
		if l != Bool || r != Bool {
			return 0, fmt.Errorf("at %d: %s joins two yes/no conditions, and this joins a %s and a %s; compare the value first, for example `x > 0 %s y == 'a'`", at, n.op, l, r, n.op)
		}
		return Bool, nil
	case "==", "!=":
		if l != r {
			return 0, fmt.Errorf("at %d: %s compares two values of the same kind, and this compares a %s with a %s; a number is written 3 and a text 'three'", at, n.op, l, r)
		}
		return Bool, nil
	case "<", "<=", ">", ">=":
		if l != Int || r != Int {
			return 0, fmt.Errorf("at %d: %s compares numbers, and this compares a %s with a %s", at, n.op, l, r)
		}
		return Bool, nil
	case "+":
		if l == String && r == String {
			return String, nil
		}
		if l == Int && r == Int {
			return Int, nil
		}
		return 0, fmt.Errorf("at %d: + adds two numbers or joins two texts, and this has a %s and a %s", at, l, r)
	default: // - * / %
		if l != Int || r != Int {
			return 0, fmt.Errorf("at %d: %s works on numbers, and this has a %s and a %s", at, n.op, l, r)
		}
		return Int, nil
	}
}

// nearest names up to three known names that look like the one written.
func nearest(name string, schema Schema) string {
	type cand struct {
		n string
		d int
	}
	var cs []cand
	for k := range schema {
		d := lev(name, k)
		if d <= 3 || strings.Contains(k, name) || strings.Contains(name, k) {
			cs = append(cs, cand{k, d})
		}
	}
	if len(cs) == 0 {
		return "; the names are listed by the host"
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].d != cs[j].d {
			return cs[i].d < cs[j].d
		}
		return cs[i].n < cs[j].n
	})
	var out []string
	for i, c := range cs {
		if i == 3 {
			break
		}
		out = append(out, c.n)
	}
	return "; did you mean " + strings.Join(out, " or ") + "?"
}

func lev(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
