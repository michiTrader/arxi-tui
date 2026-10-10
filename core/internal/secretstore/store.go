// Package secretstore keeps the API keys a user typed into arxi.
//
// # Why this is a package of its own
//
// internal/modelstore writes providers/<name>.json into the WORKING DIRECTORY,
// next to runs/ and triggers/, and refuses a record that carries a key. That
// refusal is right for that file: the directory is a repository, and a key that
// lands in it reaches a commit. A key typed into a menu still has to live
// somewhere, and "somewhere" must be a place no `git add .` can reach. So the
// key goes here, in the user's own configuration directory, and the provider
// record keeps naming only the variable.
//
// # What this does and does not protect
//
// Files are 0600 inside a 0700 directory: another account on the machine cannot
// read them. They are NOT encrypted. That is the position ~/.aws/credentials and
// the GitHub CLI's hosts file take, and it is stated here because a store that
// implies more than it does is the worse failure. Anything running as the same
// user can read the file, exactly as it can read the environment.
//
// # Reading order
//
// An environment variable, when set, still wins (see provider.Client). This
// store is the fallback, so an operator who exports a key in CI keeps full
// control and a person at a keyboard does not have to touch a shell profile.
package secretstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvDir overrides where keys live. Tests use it so they never touch the real
// configuration directory, and an operator can point it at a mounted secret.
const EnvDir = "ARXI_SECRETS_DIR"

const ext = ".key"

// maxKeyBytes bounds one key. Real keys are well under 1 KiB; the ceiling only
// exists so a pasted file cannot turn into a multi-megabyte "credential".
const maxKeyBytes = 4096

// Store is a directory of keys, one file per provider.
type Store struct{ dir string }

// legacyMarker, inside the new folder, records that the old one was already read. After
// it exists the old folder is never read again, so a key the user deletes does not come
// back from the folder this program no longer writes to.
const legacyMarker = ".legacy-imported"

// DefaultDir is the directory keys live in unless EnvDir says otherwise: ~/.arxi/secrets.
func DefaultDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv(EnvDir)); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("secretstore: cannot find your home directory (%v).\n"+
			"  set %s to a private folder and try again", err, EnvDir)
	}
	return filepath.Join(home, ".arxi", "secrets"), nil
}

// legacyDir is where keys lived before ~/.arxi: <user config dir>/arxi/secrets.
func legacyDir() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		return ""
	}
	return filepath.Join(base, "arxi", "secrets")
}

// importLegacy copies the keys of the old folder into dir, once. It never overwrites a
// key already in dir and never touches the old folder: the user may still run an older
// build, and deleting a credential is not this program's call. A missing old folder
// copies nothing and creates nothing.
func importLegacy(dir, old string) error {
	if old == "" || filepath.Clean(old) == filepath.Clean(dir) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, legacyMarker)); err == nil {
		return nil
	}
	entries, err := os.ReadDir(old)
	if err != nil {
		return nil // nothing to bring along
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("secretstore: create %s: %w", dir, err)
	}
	st := &Store{dir: dir}
	for _, n := range names {
		name := strings.TrimSuffix(n, ext)
		if validName(name) != nil {
			continue
		}
		if _, err := os.Stat(st.Path(name)); err == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(old, n))
		if err != nil {
			return fmt.Errorf("secretstore: read %s: %w", n, err)
		}
		if err := st.Set(name, string(b)); err != nil {
			continue // a file that is not a key is left where it is
		}
	}
	return os.WriteFile(filepath.Join(dir, legacyMarker), []byte("old folder: "+old+"\n"), 0o600)
}

// prepareDefault resolves the default directory and, unless EnvDir chose it, brings the
// old folder's keys along once.
func prepareDefault() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(os.Getenv(EnvDir)) == "" {
		if err := importLegacy(dir, legacyDir()); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// Open prepares dir, creating it private if necessary.
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("secretstore: no directory given")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secretstore: create %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// OpenDefault opens the store at DefaultDir.
func OpenDefault() (*Store, error) {
	dir, err := prepareDefault()
	if err != nil {
		return nil, err
	}
	return Open(dir)
}

// Lookup reads a provider's key from the default location WITHOUT creating
// anything. A run that has no stored key must not leave an empty private
// directory behind in the user's configuration folder just for having looked.
func Lookup(name string) (key string, ok bool, err error) {
	dir, err := prepareDefault()
	if err != nil {
		return "", false, err
	}
	return (&Store{dir: dir}).Get(name)
}

// Dir is the directory this store reads and writes.
func (s *Store) Dir() string { return s.dir }

// Path is the file a provider's key occupies.
func (s *Store) Path(name string) string { return filepath.Join(s.dir, name+ext) }

// validName keeps a provider name usable as a filename. It mirrors the rule in
// internal/model on purpose without importing it: this package is on the other
// side of the line that keeps key handling away from the pure model.
func validName(name string) error {
	if name == "" {
		return errors.New("secretstore: a key belongs to a provider, and no provider was named")
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			return fmt.Errorf("secretstore: provider name %q is not usable as a filename", name)
		}
	}
	return nil
}

// CheckKey reports whether a key is storable, without storing it. A caller that
// must do two writes (a provider record and its key) calls this FIRST so the
// common refusal happens before anything touches the disk.
func CheckKey(key string) error {
	_, err := cleanKey(key)
	return err
}

// cleanKey trims the key and refuses what is evidently not one. The refusal
// message never contains the value: it is shown to a person and logged.
func cleanKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("the API key is empty")
	}
	if len(key) > maxKeyBytes {
		return "", fmt.Errorf("the API key is %d bytes long, more than the %d any real key needs; "+
			"something other than a key was pasted", len(key), maxKeyBytes)
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("the API key contains a control character or a line break; " +
				"paste only the key itself")
		}
	}
	return key, nil
}

// Set stores a provider's key, replacing any earlier one. The write is atomic:
// a crash leaves the old key or the new one, never half of either.
func (s *Store) Set(name, key string) error {
	if err := validName(name); err != nil {
		return err
	}
	key, err := cleanKey(key)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, name+ext+".tmp-*")
	if err != nil {
		return fmt.Errorf("secretstore: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // a no-op once the rename succeeded

	// Permissions are set BEFORE the key is written, not after. Writing first
	// and tightening later leaves a window in which the file holds a key and is
	// readable by everyone the umask allows.
	if err := os.Chmod(tmpName, 0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secretstore: restrict temp file: %w", err)
	}
	if _, err := tmp.WriteString(key + "\n"); err != nil {
		tmp.Close()
		return fmt.Errorf("secretstore: write key: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("secretstore: fsync key: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("secretstore: close key file: %w", err)
	}
	if err := os.Rename(tmpName, s.Path(name)); err != nil {
		return fmt.Errorf("secretstore: publish key for %q: %w", name, err)
	}
	return nil
}

// Get returns a provider's stored key. ok is false, with a nil error, when none
// is stored: that is the ordinary state of a provider whose key lives in the
// environment, not a failure.
func (s *Store) Get(name string) (key string, ok bool, err error) {
	if err := validName(name); err != nil {
		return "", false, err
	}
	b, err := os.ReadFile(s.Path(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("secretstore: read key for %q: %w", name, err)
	}
	key = strings.TrimSpace(string(b))
	if key == "" {
		return "", false, nil
	}
	return key, true, nil
}

// Has reports whether a key is stored, without returning it.
func (s *Store) Has(name string) (bool, error) {
	_, ok, err := s.Get(name)
	return ok, err
}

// Delete removes a provider's stored key. Deleting one that is not there is not
// an error: the caller wanted it gone and it is.
func (s *Store) Delete(name string) error {
	if err := validName(name); err != nil {
		return err
	}
	if err := os.Remove(s.Path(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("secretstore: remove key for %q: %w", name, err)
	}
	return nil
}
