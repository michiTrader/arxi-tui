package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// This file is where /search keeps its choice. The web_search tool lives in the arxi core
// and reads its backend from three environment variables; asking a user to set those by
// hand before every session is the reason search was never used. The host stores the
// choice once and hands it to every core process it starts, so the core stays as it was.
//
// The file holds an API key, so it is private to the user (0600, in a 0700 folder) and the
// key is never printed, logged or put in an error: the same rule the provider hub keeps.

// configDirEnv overrides the folder the host keeps its settings in. A test points it at a
// temp dir, and a user with a non-default config home can too.
const configDirEnv = "ARXI_CONFIG_DIR"

// The core's own names for the three variables (core/internal/webtools/search.go).
const (
	envSearchBackend = "ARXI_SEARCH_BACKEND"
	envSearchKey     = "ARXI_SEARCH_KEY"
	envSearchURL     = "ARXI_SEARCH_URL"
)

// searchConfig is the saved choice. The zero value means web search is off.
type searchConfig struct {
	Backend string `json:"backend,omitempty"` // brave, exa or searxng
	Key     string `json:"key,omitempty"`     // brave, exa
	URL     string `json:"url,omitempty"`     // searxng
}

var errNoConfigDir = errors.New("this system has no settings folder to keep it in; set " + configDirEnv + " to a folder")

// configDir is the folder for host settings, "" when the system has none.
func configDir() string {
	if d := os.Getenv(configDirEnv); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return ""
	}
	return filepath.Join(base, "arxi")
}

// searchConfigPath is the settings file, "" when there is nowhere to keep it.
func searchConfigPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "search.json")
}

// loadSearchConfig reads the saved choice. A missing or damaged file is "off", never an
// error: a convenience must not stop the program (or a chat turn) from starting.
func loadSearchConfig(path string) searchConfig {
	var c searchConfig
	if path == "" {
		return c
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return searchConfig{}
	}
	c.Backend = strings.ToLower(strings.TrimSpace(c.Backend))
	return c
}

// save writes the choice; an empty backend removes the file, key included.
func (c searchConfig) save(path string) error {
	if path == "" {
		return errNoConfigDir
	}
	if c.Backend == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	// Write beside the file and rename over it, so a crash never leaves half a key.
	// CreateTemp makes the file readable by its owner only.
	tmp, err := os.CreateTemp(filepath.Dir(path), "search-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(name)
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// searchEnv is what to add to a core process's environment for the saved choice. A
// backend the user set in the environment wins outright, whole: mixing one variable from
// the shell with another from the file would build a search nobody asked for.
func searchEnv(getenv func(string) string, c searchConfig) []string {
	if c.Backend == "" || strings.TrimSpace(getenv(envSearchBackend)) != "" {
		return nil
	}
	env := []string{envSearchBackend + "=" + c.Backend}
	if c.Key != "" {
		env = append(env, envSearchKey+"="+c.Key)
	}
	if c.URL != "" {
		env = append(env, envSearchURL+"="+c.URL)
	}
	return env
}

// searchFromShell reports whether the environment already chooses the backend, in which
// case the saved choice is not in use and /search says so instead of pretending.
func searchFromShell() bool {
	return strings.TrimSpace(os.Getenv(envSearchBackend)) != ""
}

// chatCoreEnv is the environment for the core process that runs one chat turn: nil (the
// host's own) unless a saved search choice has to be added. It is read per turn, so a
// change made in /search applies from the next question.
func chatCoreEnv() []string {
	extra := searchEnv(os.Getenv, loadSearchConfig(searchConfigPath()))
	if len(extra) == 0 {
		return nil
	}
	return append(os.Environ(), extra...)
}
