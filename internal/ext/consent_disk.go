package ext

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DiskConsentStore is the persisted ConsentStore the consent gate's "remember"
// promise (Q15) actually needs: a grant the user chose to remember must outlive
// the process, not just a re-mount within one session as MemoryConsentStore
// gives. It is the implementation the consent.go seam comment reserved for "the
// config layer" — kept path-agnostic on purpose, because the config layer's only
// job here is to decide *where* the file lives, and hard-coding an OS path in the
// store would fold that decision into the wrong layer.
//
// The store is keyed by the identity string (Identity), so persistence inherits
// the whole identity contract for free: a version bump, a new digest or a changed
// arg is a different key and re-asks, and a remembered grant can never transfer to
// bytes the user never saw. The store persists only the granted subset the gate
// already vetted, never the manifest — the file is an allow-list of identities,
// not a plugin registry.

// diskConsentFile is the on-disk shape. It is a named wrapper around the grants
// map rather than a bare top-level map so the format can grow a field later (a
// schema marker, a written-at timestamp) without a bare map forcing every future
// reader to special-case the old shape. json.Marshal emits map keys sorted, and
// the gate hands Remember an already-sorted granted set, so the file is
// deterministic — a re-grant that changes nothing produces byte-identical output.
type diskConsentFile struct {
	Grants map[string][]string `json:"grants"`
}

// DiskConsentStore holds the grants in memory for fast Lookup and flushes the
// whole set to disk on every Remember. The mutex guards both the map and the
// file: Grant may run on a mount goroutine while the loop reads a Lookup, and a
// consent allow-list is authority — a torn read or a lost write is a security
// event, not a display glitch.
type DiskConsentStore struct {
	path       string
	mu         sync.Mutex
	byIdentity map[string][]string
	writeErr   error
}

// OpenDiskConsentStore loads the store at path. A missing file is the first run,
// not an error: an empty store re-asks for everything, which is the correct
// behaviour before any grant has been remembered. A file that exists but does not
// parse IS an error, deliberately not swallowed into an empty store: silently
// starting empty on a corrupt file would forget every remembered grant and re-ask
// for all of them, a state indistinguishable from first run — so a corrupted or
// truncated consent file would look like a fresh install rather than the data-loss
// event it is. The caller (the config layer) decides whether to surface the error
// or reset the file; the store refuses to make that call by discarding authority.
func OpenDiskConsentStore(path string) (*DiskConsentStore, error) {
	s := &DiskConsentStore{path: path, byIdentity: map[string][]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("%s: reading the consent store: %w", path, err)
	}
	var file diskConsentFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: the consent store is malformed and was not loaded; refusing to start empty because that would silently forget every remembered grant and re-ask as if this were a fresh install: %w", path, err)
	}
	for id, granted := range file.Grants {
		s.byIdentity[id] = append([]string(nil), granted...)
	}
	return s, nil
}

// Lookup returns a copy of the remembered granted set for the same reason the
// memory store does: the granted set is authority, and handing out the live
// backing array would let one caller widen a remembered grant for the next.
func (s *DiskConsentStore) Lookup(identity string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.byIdentity[identity]
	if !ok {
		return nil, false
	}
	return append([]string(nil), g...), true
}

// Remember records the granted set against the identity and flushes the whole
// store to disk, replacing any prior grant for the same identity (a re-grant is
// not two entries). The write is atomic — a temp file in the same directory then
// a rename — so a crash mid-write leaves either the old complete set or the new
// complete set, never a truncated file that OpenDiskConsentStore would then refuse
// to load. A write failure is retained on writeErr rather than surfaced here:
// Remember cannot return an error without changing the ConsentStore contract for
// the memory store too, and — more importantly — the *session* grant is valid
// regardless of whether it reached disk, so a full disk must not block mounting a
// plugin the user just approved. The owner checks Err() to warn that the decision
// will not persist across a restart.
func (s *DiskConsentStore) Remember(identity string, granted []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byIdentity[identity] = append([]string(nil), granted...)
	s.writeErr = s.flushLocked()
}

// Err reports the last write failure, if any, so the config-layer owner can tell
// the user a remembered grant did not reach disk. It is separate from Remember
// because a persistence failure is not a consent failure: the grant holds for the
// session either way.
func (s *DiskConsentStore) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeErr
}

// flushLocked writes the whole store atomically. The caller holds s.mu. The temp
// file is created in the target's own directory so the rename stays on one
// filesystem (a cross-device rename is not atomic and Go returns an error for it),
// and the parent directory is created because the store owns its own file — a
// config layer handing us ~/.config/arxi/consent.json should not also have to
// pre-create the directory for the store to work.
func (s *DiskConsentStore) flushLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%s: creating the consent store directory: %w", dir, err)
	}
	data, err := json.MarshalIndent(diskConsentFile{Grants: s.byIdentity}, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: encoding the consent store: %w", s.path, err)
	}
	tmp, err := os.CreateTemp(dir, ".consent-*.tmp")
	if err != nil {
		return fmt.Errorf("%s: creating a temp file for the atomic consent write: %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("%s: writing the consent store: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%s: closing the consent store temp file: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("%s: renaming the consent store into place: %w", s.path, err)
	}
	return nil
}
