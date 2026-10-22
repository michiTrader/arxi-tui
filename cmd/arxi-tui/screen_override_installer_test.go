package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

const installerRegistry = `{"version":"reg/v1","entries":[{"id":"tick","name":"Ticker","version":"0.2.0",
"manifest_url":"https://example.com/tick/manifest.json","description":"A ticker.","preview":"# Ticker"}]}`

// installerLoop opens the community installer over a local TLS registry and returns what
// the screen drew once want appeared.
func installerLoop(t *testing.T, want string) string {
	t.Helper()
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(installerRegistry))
	}))
	defer ts.Close()
	old := newRegistryFetchClient
	newRegistryFetchClient = func() *http.Client { return ts.Client() }
	defer func() { newRegistryFetchClient = old }()
	drv := &testDriver{evCh: make(chan fold.Event, 64)}
	return screenLoopWith(t, builtinDoc(t), drv, drv.evCh, "/ui plugin browse "+ts.URL, want)
}

// The installer is drawn from the profile's installer.json when there is one.
// Consequence: the installer stays the one screen a profile cannot reshape, so a factory
// that wants its own look must ship a Go change.
// Remedy: load the installer through screenFor like the other screens.
func TestLoopShowsTheProfilesInstallerScreen(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	writeScreen(t, "installer", strings.Replace(replacementScreen, "PROFILE HUB", "PROFILE INSTALLER", 1))
	out := installerLoop(t, "PROFILE INSTALLER")
	if !strings.Contains(out, "PROFILE INSTALLER") {
		t.Errorf("the installer did not show the profile's screen:\n%s", out)
	}
}

// Positive control: without a file the real installer lists the registry entry, so the
// replacement test above is not passing because every installer draws something.
func TestLoopShowsTheBuiltInInstallerWithoutAProfile(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	out := installerLoop(t, "Ticker")
	if !strings.Contains(out, "Ticker") || strings.Contains(out, "PROFILE INSTALLER") {
		t.Errorf("the built-in installer did not list the registry entry:\n%s", out)
	}
}
