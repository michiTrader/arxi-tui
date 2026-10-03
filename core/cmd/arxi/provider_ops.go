package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/michiTrader/arxi/internal/model"
	"github.com/michiTrader/arxi/internal/modelstore"
	"github.com/michiTrader/arxi/internal/secretstore"
)

// This file holds what the CLI and the NDJSON protocol do to providers, once.
// cmdProviderAdd and handleProviderAdd each used to build the provider and call
// the store; adding a key would have made that two copies of a sequence whose
// ORDER matters (record first, key second, undo on failure).
//
// # The key's path through this file
//
// A key arrives as a string parameter, is checked, handed to secretstore and
// forgotten. It is never placed in a returned value, an error message, a log
// line or the provider record. provider_key_test.go drives every function here
// with a recognisable key and searches everything produced for it; that test,
// not this comment, is the guarantee.

// secretsOpen is a variable so tests point it at a temporary directory. The
// production value creates ARXI_SECRETS_DIR or the user's config folder.
var secretsOpen = secretstore.OpenDefault

// secretsLookup reports whether a key is stored for a provider, without
// creating the folder. A variable for the same reason as secretsOpen.
var secretsLookup = secretstore.Lookup

// badInvocation marks an error that is the caller's doing (a malformed name, a
// key that is not a key) as opposed to the machine's (a full disk). The CLI
// exits 2 for the first and 1 for the second.
type badInvocation struct{ error }

func (b badInvocation) Unwrap() error { return b.error }

// providerAdded is the result of registering a provider: the record exactly as
// stored, plus whether a key was stored alongside it. It embeds Provider so the
// frozen result shape {name, protocol, base_url, api_key_env, models} is intact
// and key_stored is purely additive.
type providerAdded struct {
	model.Provider
	KeyStored bool `json:"key_stored"`
}

// registerProvider registers a provider and, when apiKey is not empty, stores its key.
//
// Order: validate the key, write the record, write the key, and if the key
// write fails remove the record again. The reverse order would be wrong in the
// common failure: a name that is already taken would have its EXISTING key
// overwritten by the new one before the store refused the duplicate record.
func registerProvider(name, baseURL, keyEnv, apiKey string) (providerAdded, error) {
	p, err := model.New(name, baseURL, keyEnv, nowFunc().Format(time.RFC3339))
	if err != nil {
		return providerAdded{}, badInvocation{err}
	}
	if apiKey != "" {
		if err := secretstore.CheckKey(apiKey); err != nil {
			return providerAdded{}, badInvocation{fmt.Errorf("provider %q was not registered: %w", p.Name, err)}
		}
	}
	store, err := modelstore.Open(providerDir)
	if err != nil {
		return providerAdded{}, err
	}
	if err := store.Add(p); err != nil {
		return providerAdded{}, err
	}
	if apiKey == "" {
		return providerAdded{Provider: p}, nil
	}
	if err := storeKey(p.Name, apiKey); err != nil {
		if rmErr := store.Remove(p.Name); rmErr != nil {
			return providerAdded{}, fmt.Errorf("%w\n  and the half-registered provider could not be removed (%v); delete %s by hand",
				err, rmErr, store.Path(p.Name))
		}
		return providerAdded{}, fmt.Errorf("provider %q was not registered: %w", p.Name, err)
	}
	return providerAdded{Provider: p, KeyStored: true}, nil
}

// storeKey writes a key for a provider name.
func storeKey(name, apiKey string) error {
	secrets, err := secretsOpen()
	if err != nil {
		return err
	}
	return secrets.Set(name, apiKey)
}

// setProviderKey replaces the key of a provider that exists. A key for a name
// nobody registered is refused: it would sit in the secrets folder unreachable,
// and `provider list` would never show it.
func setProviderKey(name, apiKey string) error {
	store, err := modelstore.Open(providerDir)
	if err != nil {
		return err
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if _, err := store.Load(name); err != nil {
		return err
	}
	return storeKey(name, apiKey)
}

// providerRow is one line of `provider list`. Key says where the credential
// would come from RIGHT NOW and never what it is.
type providerRow struct {
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	APIKeyEnv string `json:"api_key_env"`
	// Key is one of: "env" (the variable is set), "stored" (a key was typed into
	// the TUI), "missing" (a credential is needed and neither exists), "none"
	// (no credential is configured, as for a local server), "unreadable" (the
	// key folder could not be read; the remedy differs from "missing").
	Key    string `json:"key"`
	Models int    `json:"models"`
}

// listProviders reports every registered provider and its credential state.
func listProviders() ([]providerRow, error) {
	store, err := modelstore.Open(providerDir)
	if err != nil {
		return nil, err
	}
	ps, err := store.List()
	if err != nil {
		return nil, err
	}
	rows := make([]providerRow, 0, len(ps))
	for _, p := range ps {
		row := providerRow{Name: p.Name, BaseURL: p.BaseURL, APIKeyEnv: p.APIKeyEnv, Models: len(p.Models)}
		if p.APIKeyEnv != "" && strings.TrimSpace(os.Getenv(p.APIKeyEnv)) != "" {
			row.Key = "env"
		} else {
			_, ok, lerr := secretsLookup(p.Name)
			switch {
			case lerr != nil:
				row.Key = "unreadable"
			case ok:
				row.Key = "stored"
			case p.APIKeyEnv == "":
				row.Key = "none"
			default:
				row.Key = "missing"
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// addModel registers a model by hand, with the price the operator pays.
//
// in and out are pointers because "not given" and "zero" are different answers:
// a zero price is a real claim (a local model costs nothing) that lets a run
// start, while an omitted one leaves the model unpriced and a run refuses it.
// They must be given together; half a price would price one direction at zero.
func addModel(providerName, modelID string, in, out *float64) (model.Provider, error) {
	if (in == nil) != (out == nil) {
		return model.Provider{}, errors.New("a price needs both --in and --out (USD per million tokens); " +
			"giving only one would price the other direction at zero and under-charge every run")
	}
	var price *model.Price
	if in != nil {
		price = &model.Price{InUSDPerMTok: *in, OutUSDPerMTok: *out}
	}
	store, err := modelstore.Open(providerDir)
	if err != nil {
		return model.Provider{}, err
	}
	p, err := store.Load(strings.ToLower(strings.TrimSpace(providerName)))
	if err != nil {
		return model.Provider{}, err
	}
	if err := p.AddModel(modelID, price); err != nil {
		return model.Provider{}, err
	}
	if err := store.Save(p); err != nil {
		return model.Provider{}, err
	}
	return p, nil
}
