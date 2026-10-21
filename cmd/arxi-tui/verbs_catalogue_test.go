package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/verb"
)

// The catalogue (internal/verb) says what exists; verbImpl says what is done. Both lists
// are walked here so one cannot grow without the other.
func TestEveryVerbHasAnImplementationAndEveryImplementationAVerb(t *testing.T) {
	var noImpl, noVerb []string
	names := map[string]bool{}
	for _, v := range verb.All() {
		names[v.Name] = true
		if verbImpl[v.Name] == nil {
			noImpl = append(noImpl, v.Name)
		}
	}
	for n := range verbImpl {
		if !names[n] {
			noVerb = append(noVerb, n)
		}
	}
	sort.Strings(noVerb)
	if len(noImpl) > 0 {
		t.Errorf("verbs with no implementation: %v\nConsequence: a button calls a verb the validator accepts and nothing happens.\nRemedy: add it to verbImpl in verbs.go, or take it out of the catalogue.", noImpl)
	}
	if len(noVerb) > 0 {
		t.Errorf("implementations with no verb: %v\nConsequence: code that no scene can reach and no test checks.\nRemedy: describe it in internal/verb or delete it.", noVerb)
	}
}

func TestTheGuideTheModelReadsListsEveryVerbAndEveryParameter(t *testing.T) {
	g := verbsGuide()
	for _, v := range verb.All() {
		if !strings.Contains(g, "do:"+v.Usage()) {
			t.Errorf("the guide lacks %q\nConsequence: the model does not know the verb exists and builds the screen with a Go-sized workaround.\nRemedy: write the guide from verb.All().", "do:"+v.Usage())
		}
		if !strings.Contains(g, "("+v.Protection.String()+")") {
			t.Errorf("the guide does not say whether %s is %s", v.Name, v.Protection)
		}
		for _, p := range v.Params {
			if !strings.Contains(g, p.Doc) {
				t.Errorf("the guide lacks the description of <%s> of %s", p.Name, v.Name)
			}
		}
	}
}

func TestTheUIGuideCarriesTheVerbsChapter(t *testing.T) {
	r := newBridgeRig(t, "ask")
	if g := r.b.guideText(); !strings.Contains(g, "VERBS.") || !strings.Contains(g, "do:shell.stop") {
		t.Errorf("ui_guide has no VERBS chapter\nConsequence: the model never learns do:.\nRemedy: add verbsGuide() where the guide is joined (ui_bridge.go).")
	}
}
