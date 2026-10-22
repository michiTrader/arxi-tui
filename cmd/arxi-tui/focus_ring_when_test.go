package main

import (
	"reflect"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// A node whose `when` is false is not drawn, so Tab must skip it.
// Consequence: the focus cursor lands on a button nobody can see and Enter presses it blind.
// Remedy: focusRing asks engine.NodeHidden, the predicate the renderer asks.
func TestFocusRingSkipsANodeWhoseWhenIsFalse(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"chip","type":"text","text":"[ shells ]","when":"host.shell.list","on_press":"cmd:/shell"},
	  {"id":"other","type":"text","text":"[ go ]","on_press":"cmd:/help"}
	]}}`)

	got, err := focusRing(doc, fold.State{}, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"other"}; !reflect.DeepEqual(got, want) {
		t.Errorf("with no shell running the ring is %q, want %q. Consequence: Tab stops on a chip that is not drawn. Remedy: skip nodes engine.NodeHidden reports in focusRing.", got, want)
	}

	got, err = focusRing(doc, fold.State{HostShellList: "shell 1"}, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"chip", "other"}; !reflect.DeepEqual(got, want) {
		t.Errorf("with a shell running the ring is %q, want %q. Consequence: the chip is drawn but Tab never reaches it. Remedy: only hide nodes whose when is false.", got, want)
	}
}
