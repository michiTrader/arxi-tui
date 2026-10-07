package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

func toolEv(seq int64, typ, actor, tool string, extra map[string]any) fold.Event {
	p := map[string]any{"tool": tool, "call_id": fmt.Sprintf("c%d", seq)}
	for k, v := range extra {
		p[k] = v
	}
	return flowEv(seq, typ, actor, p)
}

// A member's detail names the tools it used and how each ended.
func TestFlowDetailListsTheHighlightedMembersTools(t *testing.T) {
	log := append(teamLog()[:5],
		toolEv(6, "tool.call", "backend", "read", nil),
		toolEv(7, "tool.call_completed", "backend", "read", nil),
		toolEv(8, "tool.call", "backend", "bash", nil),
		toolEv(9, "tool.call_denied", "backend", "bash", map[string]any{"policy": "ask"}),
		toolEv(10, "tool.call", "backend", "write", nil),
		toolEv(11, "tool.call_denied", "backend", "write", map[string]any{"policy": "deny"}),
		toolEv(12, "tool.call", "frontend", "edit", nil),
	)
	st := fold.Fold(log)
	f := flowScreen{}
	f.publish(&st)
	for _, want := range []string{"▸ backend · implementer", "✓ read", "… bash — waits for your approval", "✗ write — not allowed"} {
		if !strings.Contains(st.HubDetail, want) {
			t.Errorf("backend detail lacks %q:\n%s", want, st.HubDetail)
		}
	}
	if strings.Contains(st.HubDetail, "edit") {
		t.Errorf("another member's tool leaked into backend's detail:\n%s", st.HubDetail)
	}

	// Moving the highlight changes whose story is told.
	f.key(term.Key{Type: term.KeyDown})
	st = fold.Fold(log)
	f.publish(&st)
	if !strings.Contains(st.HubDetail, "▸ frontend") || !strings.Contains(st.HubDetail, "● edit — running") || strings.Contains(st.HubDetail, "✓ read") {
		t.Errorf("frontend detail:\n%s", st.HubDetail)
	}
}

func TestFlowDetailSaysAMemberHasUsedNoTool(t *testing.T) {
	st := fold.Fold(teamLog())
	f := flowScreen{}
	f.publish(&st)
	if !strings.Contains(st.HubDetail, "has not used any tool yet") {
		t.Fatalf("detail:\n%s", st.HubDetail)
	}
}

func TestFlowDetailKeepsOnlyTheLatestTools(t *testing.T) {
	log := teamLog()[:5]
	for i := 0; i < 8; i++ {
		seq := int64(10 + 2*i)
		log = append(log, toolEv(seq, "tool.call", "backend", fmt.Sprintf("t%d", i), nil), toolEv(seq+1, "tool.call_completed", "backend", fmt.Sprintf("t%d", i), nil))
	}
	st := fold.Fold(log)
	f := flowScreen{}
	f.publish(&st)
	if !strings.Contains(st.HubDetail, "last 5 of 8 tool calls") || strings.Contains(st.HubDetail, "✓ t2") || !strings.Contains(st.HubDetail, "✓ t3") || !strings.Contains(st.HubDetail, "✓ t7") {
		t.Fatalf("detail:\n%s", st.HubDetail)
	}
}

func TestFlowDetailForAChatHasNoMemberStory(t *testing.T) {
	st := fold.Fold([]fold.Event{flowEv(1, "run.prompt", "", map[string]any{"text": "hi"})})
	f := flowScreen{}
	f.publish(&st)
	if strings.Contains(st.HubDetail, "▸") {
		t.Fatalf("detail:\n%s", st.HubDetail)
	}
}
