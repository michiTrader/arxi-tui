package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// buildCore compiles the vendored core (core/ is a nested module).
func buildCore(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the core; skipped with -short")
	}
	bin := filepath.Join(t.TempDir(), "arxi")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/arxi")
	cmd.Dir = "../../core"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the core: %v\n%s", err, out)
	}
	return bin
}

// TestLoginAgainstTheRealCore drives the whole path with the real core process:
// /login -> API key -> OpenRouter -> paste -> save. It then reads the disk, because
// "the screen said saved" is not the claim; the claim is that the key is in the key
// file with mode 0600, is NOT in the provider record, and never appeared on screen.
func TestLoginAgainstTheRealCore(t *testing.T) {
	bin := buildCore(t)
	work := t.TempDir()
	secrets := filepath.Join(work, "secrets")
	t.Setenv("ARXI_SECRETS_DIR", secrets)
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Chdir(work)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drv, evCh, err := openServeDriver(ctx, bin)
	if err != nil {
		t.Fatalf("openServeDriver: %v", err)
	}

	step := 400 * time.Millisecond // a real round trip per step
	script := typeLine("/login")
	script = append(script,
		scheduledEvent{20 * time.Millisecond, enterEvent()},
		scheduledEvent{step, arrowEvent(term.KeyDown)},
		scheduledEvent{40 * time.Millisecond, enterEvent()},
		scheduledEvent{40 * time.Millisecond, arrowEvent(term.KeyDown)},
		scheduledEvent{40 * time.Millisecond, arrowEvent(term.KeyDown)},
		scheduledEvent{40 * time.Millisecond, arrowEvent(term.KeyDown)}, // openrouter
		scheduledEvent{40 * time.Millisecond, enterEvent()},
		scheduledEvent{40 * time.Millisecond, pasteEvent(testKey + "\n")},
		scheduledEvent{40 * time.Millisecond, enterEvent()}, // -> Model id
		scheduledEvent{40 * time.Millisecond, keyEvent('m')},
		scheduledEvent{40 * time.Millisecond, enterEvent()}, // -> Price in
		scheduledEvent{40 * time.Millisecond, enterEvent()}, // -> Price out
		scheduledEvent{40 * time.Millisecond, enterEvent()}, // save
		scheduledEvent{2 * step, arrowEvent(term.KeyDown)},  // let the refresh land
	)
	script = append(script, quit()...)

	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 30, script)
	if err := loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	screen := tty.output()

	keyFile := filepath.Join(secrets, "openrouter.key")
	got, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("the key file was not written: %v", err)
	}
	if strings.TrimSpace(string(got)) != testKey {
		t.Errorf("the key file holds %q", got)
	}
	if fi, _ := os.Stat(keyFile); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v; want 0600", fi.Mode().Perm())
	}

	rec, err := os.ReadFile(filepath.Join(work, "providers", "openrouter.json"))
	if err != nil {
		t.Fatalf("provider record missing: %v", err)
	}
	if strings.Contains(string(rec), testKey) {
		t.Error("the API key is in the provider record")
	}
	for _, want := range []string{"openrouter.ai/api/v1", `"m"`} {
		if !strings.Contains(string(rec), want) {
			t.Errorf("provider record lacks %s:\n%s", want, rec)
		}
	}

	if strings.Contains(screen, testKey) || strings.Contains(screen, testKey[:12]) {
		t.Error("the key reached the screen")
	}
	for _, want := range []string{"key stored for openrouter", "✓ key stored"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen never showed %q", want)
		}
	}
}
