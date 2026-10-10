package secretstore

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAKeyRoundTripsAndIsTrimmed(t *testing.T) {
	s := open(t)
	if err := s.Set("openai", "  sk-test-123\n"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("openai")
	if err != nil || !ok || got != "sk-test-123" {
		t.Fatalf("Get = %q, %v, %v; want the trimmed key", got, ok, err)
	}
}

func TestNoKeyIsNotAnError(t *testing.T) {
	s := open(t)
	if _, ok, err := s.Get("nobody"); ok || err != nil {
		t.Fatalf("Get on a missing key = ok %v, err %v; want ok=false and no error", ok, err)
	}
}

func TestSetReplacesAnEarlierKey(t *testing.T) {
	s := open(t)
	_ = s.Set("openai", "old-key")
	_ = s.Set("openai", "new-key")
	if got, _, _ := s.Get("openai"); got != "new-key" {
		t.Fatalf("key = %q; a second Set must replace the first", got)
	}
}

// The permissions are the whole point of this package. POSIX only: Windows has
// no group/other mode bits to assert and relies on the profile's ACL.
func TestTheKeyFileAndItsDirectoryArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are not meaningful on Windows")
	}
	s := open(t)
	if err := s.Set("openai", "sk-secret"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.Path("openai"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("key file mode = %o; want 600 so no other account can read it", mode)
	}
	di, _ := os.Stat(s.Dir())
	if mode := di.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("secrets directory mode = %o; want no group/other access", mode)
	}
}

func TestNoTempFileIsLeftBehind(t *testing.T) {
	s := open(t)
	_ = s.Set("openai", "k")
	entries, _ := os.ReadDir(s.Dir())
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file %s: a key was written outside the atomic path", e.Name())
		}
	}
}

func TestEvidentlyNotAKeyIsRefusedWithoutEchoingIt(t *testing.T) {
	s := open(t)
	cases := map[string]string{
		"empty":        "   ",
		"line break":   "sk-abc\ndef",
		"control char": "sk-abc\x07def",
		"huge":         strings.Repeat("a", maxKeyBytes+1),
	}
	for name, key := range cases {
		err := s.Set("openai", key)
		if err == nil {
			t.Errorf("%s: Set accepted it", name)
			continue
		}
		if key = strings.TrimSpace(key); key != "" && strings.Contains(err.Error(), key) {
			t.Errorf("%s: the refusal echoes the value: %v", name, err)
		}
	}
	if ok, _ := s.Has("openai"); ok {
		t.Error("a refused key was stored anyway")
	}
}

func TestANameThatEscapesTheDirectoryIsRefused(t *testing.T) {
	s := open(t)
	for _, name := range []string{"", "../x", "a/b", `a\b`, "A", "a.b"} {
		if err := s.Set(name, "k"); err == nil {
			t.Errorf("Set accepted provider name %q", name)
		}
	}
}

func TestDeleteForgetsAndIsIdempotent(t *testing.T) {
	s := open(t)
	_ = s.Set("openai", "k")
	if err := s.Delete("openai"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("openai"); err != nil {
		t.Fatalf("deleting twice must not fail: %v", err)
	}
	if ok, _ := s.Has("openai"); ok {
		t.Error("key still present after Delete")
	}
}

func TestEnvDirOverridesTheDefault(t *testing.T) {
	want := filepath.Join(t.TempDir(), "elsewhere")
	t.Setenv(EnvDir, want)
	got, err := DefaultDir()
	if err != nil || got != want {
		t.Fatalf("DefaultDir = %q, %v; want %q", got, err, want)
	}
}

func TestLookupDoesNotCreateTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never")
	t.Setenv(EnvDir, dir)
	if _, ok, err := Lookup("openai"); ok || err != nil {
		t.Fatalf("Lookup = ok %v, err %v", ok, err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("Lookup created the secrets directory just for having looked")
	}
}

func TestDefaultDirIsDotArxiInTheHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvDir, "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".arxi", "secrets"); got != want {
		t.Errorf("DefaultDir = %q, want %q", got, want)
	}
}

func seed(t *testing.T, dir, name, key string) {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(name, key); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyKeysAreCopiedOnceAndTheOldFolderIsKept(t *testing.T) {
	root := t.TempDir()
	old, dir := filepath.Join(root, "old"), filepath.Join(root, "new", "secrets")
	seed(t, old, "openai", "sk-old")
	seed(t, old, "vyceai", "vy-old")
	seed(t, dir, "vyceai", "vy-new") // already configured here: must win

	if err := importLegacy(dir, old); err != nil {
		t.Fatal(err)
	}
	st := &Store{dir: dir}
	if k, _, _ := st.Get("openai"); k != "sk-old" {
		t.Errorf("openai = %q, want the imported key", k)
	}
	if k, _, _ := st.Get("vyceai"); k != "vy-new" {
		t.Errorf("vyceai = %q, an existing key was overwritten", k)
	}
	if _, err := os.Stat(filepath.Join(old, "openai.key")); err != nil {
		t.Error("the old folder must be left untouched")
	}

	// Deleted here, it must not come back from the old folder.
	_ = st.Delete("openai")
	if err := importLegacy(dir, old); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.Has("openai"); ok {
		t.Error("a deleted key came back from the old folder")
	}
}

func TestNoLegacyFolderCreatesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "secrets")
	if err := importLegacy(dir, filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the new folder was created with nothing to import")
	}
}
