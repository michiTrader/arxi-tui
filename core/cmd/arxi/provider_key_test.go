package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/secretstore"
)

// These tests exist for one property: a key that is handed to arxi is never
// handed back. Every function that receives one is driven with a recognisable
// value, and everything that function produces -- results, errors, stdout,
// stderr, the provider record, the protocol response -- is searched for it.
// The only place the value may appear is the secrets file.

const leakyKey = "sk-LEAKCHECK-7f3a9c1e5b2d4a60"

// isolate points providers and secrets at fresh temporary directories.
func isolate(t *testing.T) (providers, secrets string) {
	t.Helper()
	savedDir, savedOpen, savedLookup := providerDir, secretsOpen, secretsLookup
	providers, secrets = t.TempDir(), t.TempDir()
	providerDir = providers
	secretsOpen = func() (*secretstore.Store, error) { return secretstore.Open(secrets) }
	secretsLookup = func(name string) (string, bool, error) {
		s, err := secretstore.Open(secrets)
		if err != nil {
			return "", false, err
		}
		return s.Get(name)
	}
	t.Cleanup(func() { providerDir, secretsOpen, secretsLookup = savedDir, savedOpen, savedLookup })
	return providers, secrets
}

// assertNoKey fails if the key is anywhere in what a caller was shown.
func assertNoKey(t *testing.T, what string, shown any) {
	t.Helper()
	var s string
	switch v := shown.(type) {
	case string:
		s = v
	case error:
		if v != nil {
			s = v.Error()
		}
	default:
		b, _ := json.Marshal(v)
		s = string(b)
	}
	if strings.Contains(s, leakyKey) {
		t.Errorf("%s contains the API key.\n  consequence: a key shown once is in a terminal scrollback, a log or a screenshot for good.\n  shown: %s", what, s)
	}
}

// treeContains reports every file under root whose bytes contain the key.
func treeContains(t *testing.T, root string) []string {
	t.Helper()
	var hits []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), leakyKey) {
			hits = append(hits, p)
		}
		return nil
	})
	return hits
}

func TestRegisteringWithAKeyStoresItOnlyInTheSecretsFolder(t *testing.T) {
	providers, secrets := isolate(t)
	res, err := registerProvider("acme", "https://api.acme.test/v1", "ACME_KEY", leakyKey)
	if err != nil {
		t.Fatal(err)
	}
	if !res.KeyStored {
		t.Fatal("key_stored is false after storing a key")
	}
	assertNoKey(t, "the result", res)
	if hits := treeContains(t, providers); len(hits) != 0 {
		t.Errorf("the key was written into the provider record(s): %v", hits)
	}
	if hits := treeContains(t, secrets); len(hits) != 1 {
		t.Errorf("expected the key in exactly one file of the secrets folder, found %v", hits)
	}
}

func TestADuplicateNameNeverOverwritesTheExistingKey(t *testing.T) {
	_, secrets := isolate(t)
	if _, err := registerProvider("acme", "https://api.acme.test/v1", "", "first-key-value"); err != nil {
		t.Fatal(err)
	}
	_, err := registerProvider("acme", "https://other.test/v1", "", leakyKey)
	if err == nil {
		t.Fatal("registering the same name twice succeeded")
	}
	assertNoKey(t, "the duplicate-name error", err)
	s, _ := secretstore.Open(secrets)
	got, _, gerr := s.Get("acme")
	if gerr != nil || got != "first-key-value" {
		t.Errorf("the existing key was changed by a refused registration: %q, %v", got, gerr)
	}
}

func TestAKeyThatIsNotAKeyIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	providers, secrets := isolate(t)
	for _, bad := range []string{"has\nnewline" + leakyKey, "ctrl\x07" + leakyKey, strings.Repeat("k", 5000) + leakyKey} {
		_, err := registerProvider("acme", "https://api.acme.test/v1", "", bad)
		if err == nil {
			t.Fatalf("a malformed key was accepted: %q", bad[:12])
		}
		assertNoKey(t, "the refusal", err)
		var inv badInvocation
		if !errors.As(err, &inv) {
			t.Errorf("a malformed key should be the caller's error (exit 2), got %T", err)
		}
	}
	for _, d := range []string{providers, secrets} {
		ents, _ := os.ReadDir(d)
		if len(ents) != 0 {
			t.Errorf("a refused registration left files in %s: %v", d, ents)
		}
	}
}

func TestAFailedKeyWriteRollsTheRecordBack(t *testing.T) {
	providers, _ := isolate(t)
	secretsOpen = func() (*secretstore.Store, error) { return nil, fmt.Errorf("disk is full") }
	_, err := registerProvider("acme", "https://api.acme.test/v1", "", leakyKey)
	if err == nil {
		t.Fatal("registration reported success although the key could not be stored")
	}
	assertNoKey(t, "the rollback error", err)
	ents, _ := os.ReadDir(providers)
	if len(ents) != 0 {
		t.Errorf("a provider without its key was left behind: %v\n  consequence: the user sees the provider as registered and every run fails for want of a key they believe they entered", ents)
	}
}

func TestAKeyForAProviderThatDoesNotExistIsRefused(t *testing.T) {
	_, secrets := isolate(t)
	err := setProviderKey("ghost", leakyKey)
	if err == nil {
		t.Fatal("stored a key for a provider nobody registered")
	}
	assertNoKey(t, "the error", err)
	if hits := treeContains(t, secrets); len(hits) != 0 {
		t.Errorf("the key was stored anyway: %v", hits)
	}
}

func TestListReportsWhereTheKeyComesFromAndNeverTheKey(t *testing.T) {
	isolate(t)
	t.Setenv("ACME_ENV_KEY", "from-env-"+leakyKey)
	t.Setenv("NOT_SET_VAR", "")
	for _, c := range [][4]string{
		{"viaenv", "https://a.test/v1", "ACME_ENV_KEY", ""},
		{"stored", "https://b.test/v1", "NOT_SET_VAR", leakyKey},
		{"missing", "https://c.test/v1", "NOT_SET_VAR", ""},
		{"local", "http://127.0.0.1:11434/v1", "", ""},
	} {
		if _, err := registerProvider(c[0], c[1], c[2], c[3]); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := listProviders()
	if err != nil {
		t.Fatal(err)
	}
	assertNoKey(t, "provider list", rows)
	want := map[string]string{"viaenv": "env", "stored": "stored", "missing": "missing", "local": "none"}
	for _, r := range rows {
		if want[r.Name] != r.Key {
			t.Errorf("provider %s: key state %q, want %q", r.Name, r.Key, want[r.Name])
		}
	}
	if len(rows) != len(want) {
		t.Errorf("got %d rows, want %d", len(rows), len(want))
	}
}

func TestModelAddNeedsBothPricesOrNeither(t *testing.T) {
	isolate(t)
	if _, err := registerProvider("local", "http://127.0.0.1:11434/v1", "", ""); err != nil {
		t.Fatal(err)
	}
	one := 1.0
	if _, err := addModel("local", "llama3", &one, nil); err == nil {
		t.Error("half a price was accepted")
	}
	if _, err := addModel("local", "llama3", nil, nil); err != nil {
		t.Errorf("an unpriced model was refused: %v", err)
	}
	zero := 0.0
	if _, err := addModel("local", "llama3b", &zero, &zero); err != nil {
		t.Errorf("a zero price is a real claim and was refused: %v", err)
	}
	neg := -1.0
	if _, err := addModel("local", "llama3c", &neg, &zero); err == nil {
		t.Error("a negative price was accepted")
	}
	if _, err := addModel("local", "llama3b", &zero, &zero); err == nil {
		t.Error("the same model was added twice")
	}
}

// ---- the protocol -----------------------------------------------------------

func TestTheProtocolVerbsNeverReturnTheKey(t *testing.T) {
	providers, secrets := isolate(t)
	rs := exchange(t,
		fmt.Sprintf(`{"id":"1","type":"provider.add","params":{"name":"acme","base_url":"https://api.acme.test/v1","api_key":%q}}`, leakyKey),
		`{"id":"2","type":"provider.list"}`,
		`{"id":"3","type":"model.add","params":{"provider":"acme","model":"acme-1","in":1.5,"out":6}}`,
		fmt.Sprintf(`{"id":"4","type":"provider.key","params":{"name":"acme","api_key":%q}}`, leakyKey+"-second"),
		fmt.Sprintf(`{"id":"5","type":"provider.key","params":{"name":"ghost","api_key":%q}}`, leakyKey),
		fmt.Sprintf(`{"id":"6","type":"provider.add","params":{"name":"acme","api_key":%q}}`, leakyKey),
		`{"id":"7","type":"provider.key","params":{"name":"acme"}}`,
	)
	if len(rs) != 7 {
		t.Fatalf("7 requests, %d responses", len(rs))
	}
	for i, r := range rs {
		assertNoKey(t, fmt.Sprintf("response %d", i+1), r)
	}
	for i := 0; i < 4; i++ {
		if !rs[i].OK {
			t.Errorf("request %d failed: %+v", i+1, rs[i].Error)
		}
	}
	for i := 4; i < 7; i++ {
		if rs[i].OK {
			t.Errorf("request %d should have been refused", i+1)
		}
	}
	if hits := treeContains(t, providers); len(hits) != 0 {
		t.Errorf("provider records contain the key: %v", hits)
	}
	s, _ := secretstore.Open(secrets)
	if got, _, _ := s.Get("acme"); got != leakyKey+"-second" {
		t.Errorf("provider.key did not replace the stored key")
	}
}

func TestTheProtocolRefusesKeysOfTheWrongType(t *testing.T) {
	isolate(t)
	r := one(t, `{"id":"1","type":"provider.add","params":{"name":"acme","api_key":12345}}`)
	if r.OK {
		t.Error("a numeric api_key was accepted")
	}
	r = one(t, `{"id":"1","type":"model.add","params":{"provider":"x","model":"y","in":"free"}}`)
	if r.OK {
		t.Error("a string price was accepted")
	}
}

func TestHelloAdvertisesTheNewVerbs(t *testing.T) {
	out := exchangeRaw(t, `{"id":"1","type":"schema"}`)
	for _, v := range []string{"provider.key", "provider.list", "model.add"} {
		if !strings.Contains(out, `"`+v+`"`) {
			t.Errorf("the hello does not list %s as implemented", v)
		}
	}
}

func exchangeRaw(t *testing.T, lines ...string) string {
	t.Helper()
	var out strings.Builder
	if err := serveConn(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(out.String(), "\n", 2)[0]
	return first
}

// ---- the command line -------------------------------------------------------

func cliRun(t *testing.T, dir, stdin string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(buildIash(t), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running arxi %v: %v", args, err)
	}
	return result{out: string(out), code: code}
}

func TestTheCommandLineRefusesAKeyOnTheCommandLine(t *testing.T) {
	dir, secrets := t.TempDir(), t.TempDir()
	env := []string{"ARXI_SECRETS_DIR=" + secrets}
	for _, args := range [][]string{
		{"provider", "add", "acme", "--base-url", "https://a.test/v1", "--api-key", leakyKey},
		{"provider", "key", "acme", "--api-key", leakyKey},
	} {
		r := cliRun(t, dir, "", env, args...)
		if r.code != 2 {
			t.Errorf("arxi %v: exit %d, want 2:\n%s", args[:2], r.code, r.out)
		}
		assertNoKey(t, "the refusal text", r.out)
		if !strings.Contains(r.out, "standard input") {
			t.Errorf("the refusal does not name the safe way:\n%s", r.out)
		}
	}
	if hits := treeContains(t, dir); len(hits) != 0 {
		t.Errorf("a refused command wrote the key: %v", hits)
	}
	if hits := treeContains(t, secrets); len(hits) != 0 {
		t.Errorf("a refused command stored the key: %v", hits)
	}
}

func TestTheCommandLineReadsTheKeyFromStandardInput(t *testing.T) {
	dir, secrets := t.TempDir(), t.TempDir()
	env := []string{"ARXI_SECRETS_DIR=" + secrets}
	if r := cliRun(t, dir, "", env, "provider", "add", "acme", "--base-url", "https://a.test/v1", "--api-key-env", "ACME_NOT_SET_XYZ"); r.code != 0 {
		t.Fatalf("add failed: %s", r.out)
	}
	r := cliRun(t, dir, leakyKey+"\n", env, "provider", "key", "acme")
	if r.code != 0 {
		t.Fatalf("provider key failed (%d): %s", r.code, r.out)
	}
	assertNoKey(t, "provider key output", r.out)
	list := cliRun(t, dir, "", env, "provider", "list")
	assertNoKey(t, "provider list output", list.out)
	if !strings.Contains(list.out, "stored") {
		t.Errorf("provider list does not say the key is stored:\n%s", list.out)
	}
	js := cliRun(t, dir, "", env, "provider", "list", "--json")
	assertNoKey(t, "provider list --json output", js.out)
	if hits := treeContains(t, dir); len(hits) != 0 {
		t.Errorf("the key reached the project folder: %v", hits)
	}
	s, _ := secretstore.Open(secrets)
	if got, _, err := s.Get("acme"); err != nil || got != leakyKey {
		t.Errorf("stored key is %q (%v), want the piped one without its newline", got, err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(s.Path("acme"))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("key file mode = %v (%v), want 0600", fi.Mode().Perm(), err)
		}
	}
	// empty stdin is refused rather than storing an empty key
	if r := cliRun(t, dir, "\n", env, "provider", "key", "acme"); r.code == 0 {
		t.Errorf("an empty key was accepted:\n%s", r.out)
	}
}

func TestTheCommandLineAddsAPricedModel(t *testing.T) {
	dir := t.TempDir()
	env := []string{"ARXI_SECRETS_DIR=" + t.TempDir()}
	cliRun(t, dir, "", env, "provider", "add", "local", "--base-url", "http://127.0.0.1:11434/v1")
	if r := cliRun(t, dir, "", env, "model", "add", "local", "llama3", "--in", "0", "--out", "0"); r.code != 0 {
		t.Fatalf("model add failed: %s", r.out)
	}
	if r := cliRun(t, dir, "", env, "model", "add", "local", "llama4", "--in", "1"); r.code != 2 {
		t.Errorf("half a price: exit %d, want 2:\n%s", r.code, r.out)
	}
	if r := cliRun(t, dir, "", env, "model", "add", "local", "llama5", "--in", "abc", "--out", "1"); r.code != 2 {
		t.Errorf("a non-number price: exit %d, want 2:\n%s", r.code, r.out)
	}
}
