package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

const testSearchKey = "brv-secret-0123456789"

// searchDir points the settings at a private folder for one test.
func searchDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	t.Setenv(envSearchBackend, "")
	return dir
}

func pressEnter(h *providerHub) hubKeyResult {
	return routeHubKey(h, term.Key{Type: term.KeyEnter})
}

func TestSearchConfigRoundTripAndPrivacy(t *testing.T) {
	dir := searchDir(t)
	path := searchConfigPath()
	if path != filepath.Join(dir, "search.json") {
		t.Fatalf("path = %q", path)
	}
	if err := (searchConfig{Backend: "brave", Key: testSearchKey}).save(path); err != nil {
		t.Fatal(err)
	}
	got := loadSearchConfig(path)
	if got.Backend != "brave" || got.Key != testSearchKey {
		t.Errorf("loaded %+v", got)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("the file holds a key and must be 0600, got %v", st.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temp file was left behind: %s", e.Name())
		}
	}
	// Off removes the file, key included.
	if err := (searchConfig{}).save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("turning search off must delete the saved key: %v", err)
	}
	if err := (searchConfig{}).save(path); err != nil {
		t.Errorf("turning off twice must not fail: %v", err)
	}
}

func TestSearchConfigDamagedFileIsOff(t *testing.T) {
	searchDir(t)
	path := searchConfigPath()
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := loadSearchConfig(path); c.Backend != "" {
		t.Errorf("a damaged file must read as off, got %+v", c)
	}
	if c := loadSearchConfig(""); c.Backend != "" {
		t.Errorf("no path must read as off, got %+v", c)
	}
}

func TestSearchEnvIsAddedOnlyWhenTheShellHasNoChoice(t *testing.T) {
	none := func(string) string { return "" }
	cfg := searchConfig{Backend: "brave", Key: testSearchKey}
	got := strings.Join(searchEnv(none, cfg), "|")
	if got != envSearchBackend+"=brave|"+envSearchKey+"="+testSearchKey {
		t.Errorf("env = %q", got)
	}
	sx := searchEnv(none, searchConfig{Backend: "searxng", URL: "http://localhost:8080"})
	if len(sx) != 2 || sx[1] != envSearchURL+"=http://localhost:8080" {
		t.Errorf("searxng env = %v", sx)
	}
	shell := func(k string) string {
		if k == envSearchBackend {
			return "exa"
		}
		return ""
	}
	if e := searchEnv(shell, cfg); e != nil {
		t.Errorf("a backend set in the shell wins whole, got %v", e)
	}
	if e := searchEnv(none, searchConfig{}); e != nil {
		t.Errorf("no choice adds nothing, got %v", e)
	}
}

func TestChatCoreEnvCarriesTheSavedChoice(t *testing.T) {
	searchDir(t)
	if chatCoreEnv() != nil {
		t.Error("with nothing saved the core inherits the host environment unchanged")
	}
	if err := (searchConfig{Backend: "brave", Key: testSearchKey}).save(searchConfigPath()); err != nil {
		t.Fatal(err)
	}
	var have bool
	for _, kv := range chatCoreEnv() {
		if kv == envSearchBackend+"=brave" {
			have = true
		}
	}
	if !have {
		t.Error("the saved backend must reach the core process")
	}
}

func TestSearchScreenPicksAServiceAndSavesTheKey(t *testing.T) {
	searchDir(t)
	h, _ := newHub(hubData{}, hubOpenSearch)
	if h.level != lvSearch || len(h.items()) != 4 {
		t.Fatalf("level %v, %d rows", h.level, len(h.items()))
	}
	// Brave is the first row.
	if r := pressEnter(h); r.work != nil || h.level != lvForm {
		t.Fatalf("a service opens its form first: %+v level %v", r, h.level)
	}
	// An empty key is refused by name, never by value.
	if r := pressEnter(h); r.work != nil || !strings.Contains(r.notice, "API key is required") {
		t.Fatalf("empty key: %+v", r)
	}
	h.paste(testSearchKey)

	var st fold.State
	h.publish(&st)
	all := st.UserInput + st.HubTitle + st.HubHint + st.HubDetail
	for _, r := range st.HubRows {
		all += r.Label + r.Status
	}
	if strings.Contains(all, testSearchKey) {
		t.Fatalf("the key reached the screen: %q", all)
	}

	r := pressEnter(h)
	if r.work == nil || r.work.Op != opSearch || r.work.Name != "brave" || !r.work.Close {
		t.Fatalf("work = %+v", r.work)
	}
	out := runHubWork(nil, nil, *r.work)
	if !out.ok || !out.closeUI || strings.Contains(out.notice, testSearchKey) {
		t.Fatalf("outcome %+v", out)
	}
	if got := loadSearchConfig(searchConfigPath()); got.Backend != "brave" || got.Key != testSearchKey {
		t.Errorf("saved %+v", got)
	}
}

func TestSearchScreenSearxngAsksForAnAddressAndChecksIt(t *testing.T) {
	searchDir(t)
	h, _ := newHub(hubData{}, hubOpenSearch)
	h.sel = 2
	pressEnter(h)
	h.paste("localhost:8080")
	if r := pressEnter(h); r.work != nil || !strings.Contains(r.notice, "http") {
		t.Fatalf("an address without a scheme must be refused: %+v", r)
	}
	h.form.fields[0].value = ""
	h.paste("http://localhost:8080")
	r := pressEnter(h)
	if r.work == nil || r.work.Name != "searxng" || r.work.BaseURL != "http://localhost:8080" {
		t.Fatalf("work = %+v", r.work)
	}
	runHubWork(nil, nil, *r.work)
	if got := loadSearchConfig(searchConfigPath()); got.Backend != "searxng" || got.URL != "http://localhost:8080" {
		t.Errorf("saved %+v", got)
	}
}

func TestSearchScreenOffForgetsTheKey(t *testing.T) {
	searchDir(t)
	if err := (searchConfig{Backend: "exa", Key: testSearchKey}).save(searchConfigPath()); err != nil {
		t.Fatal(err)
	}
	h, _ := newHub(hubData{}, hubOpenSearch)
	var exa, off string
	for _, it := range h.items() {
		switch it.id {
		case "exa":
			exa = it.status
		case "off":
			off = it.status
		}
	}
	if !strings.Contains(exa, "in use") || strings.Contains(off, "in use") {
		t.Errorf("the service in use must be marked: exa %q, off %q", exa, off)
	}
	h.sel = 3
	r := pressEnter(h)
	if r.work == nil || r.work.Name != "" {
		t.Fatalf("work = %+v", r.work)
	}
	out := runHubWork(nil, nil, *r.work)
	if !out.ok || !strings.Contains(out.notice, "off") {
		t.Errorf("outcome %+v", out)
	}
	if _, err := os.Stat(searchConfigPath()); !os.IsNotExist(err) {
		t.Error("the saved key must be deleted")
	}
}

func TestSearchScreenSaysWhenTheShellOverrides(t *testing.T) {
	searchDir(t)
	t.Setenv(envSearchBackend, "exa")
	h, _ := newHub(hubData{}, hubOpenSearch)
	if d := h.detail(); !strings.Contains(d, envSearchBackend) {
		t.Errorf("the screen must say the environment decides: %q", d)
	}
	out := saveSearchChoice(hubWork{Op: opSearch, Name: "brave", Key: testSearchKey})
	if !strings.Contains(out.notice, "wins") || strings.Contains(out.notice, testSearchKey) {
		t.Errorf("notice = %q", out.notice)
	}
}

func TestSearchCommandOpensItsScreen(t *testing.T) {
	if got, ok := hubCommand("/search"); !ok || got != hubOpenSearch {
		t.Errorf("/search -> %v %v", got, ok)
	}
	var listed bool
	for _, m := range fold.FilterSlashCategory("search", fold.SlashAll) {
		listed = listed || m.Name == "search"
	}
	if !listed {
		t.Error("/search must be in the command menu")
	}
}
