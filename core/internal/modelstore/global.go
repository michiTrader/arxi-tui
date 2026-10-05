package modelstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvDir overrides where providers live. Tests and operators use it; it wins over
// everything else.
const EnvDir = "ARXI_PROVIDERS_DIR"

// GlobalDirIn is the providers folder under a user configuration directory. It sits
// next to the secrets folder (<config>/arxi/secrets), so one place holds everything
// the user configured and it is the same from any working directory.
func GlobalDirIn(configDir string) string {
	return filepath.Join(configDir, "arxi", "providers")
}

// migratedFile, inside the global folder, lists the local folders already copied (one
// absolute path per line), so each is brought along once and a provider the user later
// removes does not come back. Its name does not end in ext, so it is never a provider.
const migratedFile = ".migrated"

// Locate decides where providers live for this process and brings along the providers
// the user already has in ./providers.
//
//   - ARXI_PROVIDERS_DIR, when set, is used as it is and nothing is copied.
//   - Otherwise the global folder (see GlobalDirIn) is used, so the providers and the
//     default model are the same whichever folder the program is started in. Before
//     this, they were read from ./providers and a new folder had none.
//   - If ./providers (relative to cwd) holds providers and that folder has not been
//     brought along before, they are copied to the global folder. Files already there
//     are never overwritten, and the originals are left alone, so nothing is lost. The
//     global default model is only set from a local one when it has none.
//   - With no configuration directory to use, it falls back to ./providers.
//
// migrated is how many files were copied, so the caller can say so.
func Locate(env func(string) string, configDir func() (string, error), cwd string) (dir string, migrated int, err error) {
	if d := strings.TrimSpace(env(EnvDir)); d != "" {
		return d, 0, nil
	}
	base, cerr := configDir()
	if cerr != nil || strings.TrimSpace(base) == "" {
		return DefaultDir, 0, nil
	}
	global := GlobalDirIn(base)
	local := filepath.Join(cwd, DefaultDir)
	if filepath.Clean(local) == filepath.Clean(global) {
		return global, 0, nil
	}
	if done, err := alreadyMigrated(global, local); err != nil || done {
		return global, 0, err
	}
	n, err := copyProviders(local, global)
	if err != nil {
		return "", 0, err
	}
	return global, n, nil
}

// alreadyMigrated reports whether local was brought along before.
func alreadyMigrated(global, local string) (bool, error) {
	body, err := os.ReadFile(filepath.Join(global, migratedFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("modelstore: read %s: %w", migratedFile, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == filepath.Clean(local) {
			return true, nil
		}
	}
	return false, nil
}

// copyProviders copies every provider file, and the default-model file, from one
// folder to another. A source that is missing or holds no provider copies nothing and
// creates nothing, so the global folder is not made until there is something to put in
// it (or a provider is added).
func copyProviders(from, to string) (int, error) {
	entries, err := os.ReadDir(from)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("modelstore: read %s: %w", from, err)
	}
	var names []string
	providers := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch {
		case strings.HasSuffix(e.Name(), ext):
			providers++
			names = append(names, e.Name())
		case e.Name() == defaultFile:
			names = append(names, e.Name())
		}
	}
	if providers == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return 0, fmt.Errorf("modelstore: create %s: %w", to, err)
	}
	copied := 0
	for _, name := range names {
		dst := filepath.Join(to, name)
		if _, err := os.Stat(dst); err == nil {
			continue // never overwrite what the user already has here
		}
		body, err := os.ReadFile(filepath.Join(from, name))
		if err != nil {
			return copied, fmt.Errorf("modelstore: read %s: %w", name, err)
		}
		// Written 0600 like every provider file the store writes.
		if err := os.WriteFile(dst, body, 0o600); err != nil {
			return copied, fmt.Errorf("modelstore: copy %s: %w", name, err)
		}
		copied++
	}
	f, err := os.OpenFile(filepath.Join(to, migratedFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return copied, fmt.Errorf("modelstore: record %s: %w", from, err)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, filepath.Clean(from)); err != nil {
		return copied, fmt.Errorf("modelstore: record %s: %w", from, err)
	}
	return copied, nil
}
