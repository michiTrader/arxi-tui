package scene

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

func TestDebugSOBRIATokens(t *testing.T) {
	thm := theme.SOBRIA()
	t.Logf("SOBRIA tokens: %v", thm.Tokens())
	t.Logf("Has 'dim': %v", thm.Has("dim"))
	t.Logf("Has 'bright': %v", thm.Has("bright"))
}
