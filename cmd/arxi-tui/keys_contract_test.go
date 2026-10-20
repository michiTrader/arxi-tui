package main

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docs/KEYS.md is the contract for the factory keys. These tests read it the way
// internal/scene/binds_audit_test.go reads docs/BINDS.md: the document and the code must
// say the same thing, so a key cannot move by accident.

// contractSection returns the lines between two markers of docs/KEYS.md.
func contractSection(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile("../../docs/KEYS.md")
	if err != nil {
		t.Fatalf("docs/KEYS.md must exist (it is the keys contract): %v", err)
	}
	s := string(raw)
	begin, end := "<!-- "+name+":begin -->", "<!-- "+name+":end -->"
	i, j := strings.Index(s, begin), strings.Index(s, end)
	if i < 0 || j < i {
		t.Fatalf("docs/KEYS.md lost its %s markers; put them back around the table", name)
	}
	return strings.Split(strings.TrimSpace(s[i+len(begin):j]), "\n")
}

var backticked = regexp.MustCompile("`([^`]+)`")

func cellKeys(cell string) []string {
	var out []string
	for _, m := range backticked.FindAllStringSubmatch(cell, -1) {
		out = append(out, m[1])
	}
	return out
}

// A factory key that changes without the contract changing fails here. The remedy is
// never to edit this test: decide the change in docs/KEYS.md (section 4), then the code.
func TestFactoryKeysMatchTheContractInDocsKeys(t *testing.T) {
	var doc [][]string
	for _, line := range contractSection(t, "keys-contract") {
		if !strings.HasPrefix(line, "| `") {
			continue // header and separator rows
		}
		cells := strings.Split(line, "|")
		doc = append(doc, cellKeys(cells[1]))
	}
	var code [][]string
	for _, f := range factoryKeys {
		code = append(code, f.Keys)
	}
	if len(doc) != len(code) {
		t.Fatalf("docs/KEYS.md lists %d factory rows and /keys shows %d: a behaviour was added or removed without the contract; edit docs/KEYS.md first (section 4)", len(doc), len(code))
	}
	for i := range code {
		if !reflect.DeepEqual(doc[i], code[i]) {
			t.Errorf("factory row %d changed: the contract says %v, the program has %v. If that is intended, change docs/KEYS.md first (section 4); if not, put the keys back in keys_menu.go",
				i+1, doc[i], code[i])
		}
	}
}

// The default menu keys are part of the contract too.
func TestMenuKeyDefaultsMatchTheContractInDocsKeys(t *testing.T) {
	doc := map[string][]string{}
	for _, line := range contractSection(t, "menu-keys-contract") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		act := cellKeys(cells[1])
		if len(act) != 1 {
			t.Fatalf("menu keys row %q must name one action", line)
		}
		doc[act[0]] = cellKeys(cells[2])
	}
	for _, act := range menuNavActions {
		got := append([]string(nil), navDefaults[act]...)
		want := doc[act]
		sort.Strings(got)
		sort.Strings(want)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("menu action %q: the contract says %v, navDefaults has %v. If that is intended, change docs/KEYS.md first (section 4); if not, put navDefaults back",
				act, want, got)
		}
	}
	for act := range doc {
		if !contains(menuNavActions, act) {
			t.Errorf("docs/KEYS.md names a menu action %q that does not exist; remove the row", act)
		}
	}
}
