package modelstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michiTrader/arxi/internal/fsdurability"
	"github.com/michiTrader/arxi/internal/model"
)

// defaultFile holds the model chat uses when no model is named.
//
// It lives inside the provider directory because it is a statement about those
// providers (which of them to talk to), so it moves with them between projects
// and is removed with them. Its name does not end in ext, which is what keeps
// names() from ever listing it as a provider: the same property the temp files
// rely on.
const defaultFile = "default-model"

// Default returns the stored default model reference ("provider/id"), or ""
// when none is chosen. A missing file is not an error: nobody has chosen yet.
func (s *Store) Default() (string, error) {
	body, err := os.ReadFile(filepath.Join(s.dir, defaultFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("modelstore: read default model: %w", err)
	}
	return strings.TrimSpace(string(body)), nil
}

// SetDefault records a model as the default. The reference must be the full
// "provider/id" form and must name a model that exists: a default that points
// at nothing would make every later chat fail with an error about a choice the
// user does not remember making.
func (s *Store) SetDefault(ref string) (provider, id string, err error) {
	p, id, err := s.Owner(ref)
	if err != nil {
		return "", "", err
	}
	if err := s.writeDefault(p.Name + "/" + id); err != nil {
		return "", "", err
	}
	return p.Name, id, nil
}

// ClearDefault forgets the default. Clearing when none is set is not an error.
func (s *Store) ClearDefault() error {
	if err := os.Remove(filepath.Join(s.dir, defaultFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("modelstore: clear default model: %w", err)
	}
	return fsdurability.SyncDirectory(s.dir)
}

// ResolveDefault returns the default as a resolution-ready reference, but only
// while it still points at a model that exists and may be used. A stale
// default (its provider was removed, its model disabled) reads as "none"
// rather than as an error, because the remedy is the same as for a fresh
// install: choose one.
func (s *Store) ResolveDefault() (string, error) {
	ref, err := s.Default()
	if err != nil || ref == "" {
		return "", err
	}
	ps, err := s.List()
	if err != nil {
		return "", err
	}
	if _, err := model.Resolve(ps, ref); err != nil {
		return "", nil
	}
	return ref, nil
}

func (s *Store) writeDefault(ref string) error {
	tmp, err := os.CreateTemp(s.dir, defaultFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("modelstore: create temp file for the default model: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(ref + "\n"); err != nil {
		tmp.Close()
		return fmt.Errorf("modelstore: write default model: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("modelstore: fsync default model: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("modelstore: close default model: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("modelstore: chmod default model: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(s.dir, defaultFile)); err != nil {
		return fmt.Errorf("modelstore: publish default model: %w", err)
	}
	return fsdurability.SyncDirectory(s.dir)
}
