package engine

import (
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// NodeHidden says whether the renderer skips n for this state: its `when` is falsy, or its
// id is in the hidden set. It is the same predicate the draw and the measuring pass ask,
// exported so a tool that explains a screen cannot disagree with the screen.
func NodeHidden(n *scene.Node, state fold.State) bool {
	return hiddenByWhen(n, state)
}

// BindValue is what a bind resolves to for this state, as the renderer would read it.
func BindValue(bind string, state fold.State) string {
	return resolveBind(bind, state)
}
