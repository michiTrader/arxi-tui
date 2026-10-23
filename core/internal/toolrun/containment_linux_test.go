//go:build linux

package toolrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandEnvironmentAllowlistDropsGenericAndProviderSecrets(t *testing.T) {
	t.Setenv("ARXI_SENTINEL_SECRET", "generic-secret")
	t.Setenv("OPENAI_API_KEY", "provider-secret")
	t.Setenv("PATH", os.Getenv("PATH"))
	w := commandWorkspace(t)
	res, err := w.Bash(context.Background(), `printf 'path=%s\ngeneric=%s\nprovider=%s\nhome=%s\n' "$PATH" "$ARXI_SENTINEL_SECRET" "$OPENAI_API_KEY" "$HOME"`, time.Second)
	if err != nil {
		t.Fatalf("run environment probe: %v", err)
	}
	if !strings.Contains(res.Output, "path=") || !strings.Contains(res.Output, "home="+w.Root) {
		t.Fatalf("allowlisted minimum = %q: shell and toolchain variables must remain usable without inheriting the host environment", res.Output)
	}
	if strings.Contains(res.Output, "generic-secret") || strings.Contains(res.Output, "provider-secret") {
		t.Fatalf("child environment = %q: undeclared and provider credentials must be absent by default", res.Output)
	}
}

func TestProcessGroupProfileDoesNotClaimSetsidContainment(t *testing.T) {
	w := commandWorkspace(t)
	marker := filepath.Join(w.Root, "setsid-survived.txt")
	script := `(setsid sh -c 'sleep 0.4; echo survived > setsid-survived.txt') & echo started`
	res, err := w.Bash(context.Background(), script, 100*time.Millisecond)
	if err != nil || !res.TimedOut {
		t.Fatalf("setsid probe = %#v / %v: the operator process-group profile must still enforce its own deadline", res, err)
	}
	// The escaped child writes after 0.4s of its own. A fixed wait of 0.7s after the call
	// returned was enough on an idle machine and not on a loaded CI runner (the file was
	// missing when looked for, with the behaviour unchanged), so look for it until it
	// shows or a generous limit passes. Only a child that was really killed never writes.
	var statErr error
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if _, statErr = os.Stat(marker); statErr == nil {
			break
		}
	}
	if statErr != nil {
		t.Fatalf("setsid child did not demonstrate the documented escape within 10s: %v; never relabel process-group termination as whole-tree containment", statErr)
	}
}

func TestBashWithoutProfileNeverStartsACommand(t *testing.T) {
	w := ws(t)
	marker := filepath.Join(w.Root, "must-not-exist")
	if _, err := w.Bash(context.Background(), "touch must-not-exist", time.Second); err == nil {
		t.Fatal("bash without a frozen profile ran: None and unsupported profiles must fail before process creation")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker after refused bash = %v: unsupported execution must have no child-side effects", err)
	}
}
