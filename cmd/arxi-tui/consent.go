package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// This file is the config layer the I5 consent gate reserved: the one place that
// decides *where* the persisted allow-list lives and hands the gate a store
// pointed at it. ext.DiskConsentStore is kept path-agnostic on purpose (its own
// doc argues folding an OS path into the store would put that decision in the
// wrong layer), so the path choice belongs here, in the host, beside the run-log
// path openServeDriver already resolves the same way.

// consentStorePath resolves the on-disk location of the remembered-grant
// allow-list. ARXI_CONSENT_FILE overrides it outright — a test points it at a
// temp file, and a user with a non-default config home can too — otherwise it is
// ~/.arxi/consent.json, the same ~/.arxi tree openServeDriver puts the run log
// under. It is a single file rather than a directory because the store owns one
// file and creates its own parent (DiskConsentStore.flushLocked MkdirAll's the
// dir), so the config layer only has to name the path, not provision it.
func consentStorePath() (string, error) {
	if p := os.Getenv("ARXI_CONSENT_FILE"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir for the consent store path: %w", err)
	}
	return filepath.Join(home, ".arxi", "consent.json"), nil
}

// openConsentGate builds the gate the loop consults before spawning any
// behavioral plugin. It returns the gate and a human-facing warning (empty when
// clean) rather than an error, because a consent-store problem must never brick
// the whole interface: the TUI has to come up so the user can read the warning
// and every non-plugin surface keeps working.
//
// A malformed or unreadable consent file is the case that decides the fallback.
// OpenDiskConsentStore deliberately refuses it rather than starting empty (that
// would silently forget every remembered grant and look identical to a fresh
// install). The config layer's answer here is to fall back to a *session* store
// and leave the file on disk untouched: re-asking for one session preserves the
// remembered grants for the user to recover, while resetting the file — or
// refusing to boot — would either destroy them or make an allow-list problem
// take down the entire interface. The warning names the file so the user knows
// which one to inspect.
func openConsentGate() (*ext.Gate, string) {
	path, err := consentStorePath()
	if err != nil {
		return ext.NewGate(ext.NewMemoryConsentStore()),
			fmt.Sprintf("consent store path could not be resolved, so remembered grants will not persist this session: %v", err)
	}
	store, err := ext.OpenDiskConsentStore(path)
	if err != nil {
		return ext.NewGate(ext.NewMemoryConsentStore()),
			fmt.Sprintf("%s: the consent store did not load, so remembered grants are unavailable this session and you will be re-asked; the file is left untouched for you to inspect: %v", path, err)
	}
	return ext.NewGate(store), ""
}
