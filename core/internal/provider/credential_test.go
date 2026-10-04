package provider

import (
	"errors"
	"strings"
	"testing"
)

func lookupOf(m map[string]string) func(string) (string, bool, error) {
	return func(name string) (string, bool, error) {
		k, ok := m[name]
		return k, ok, nil
	}
}

func TestTheEnvironmentWinsOverAStoredKey(t *testing.T) {
	c := &Client{APIKeyEnv: "X_KEY", Provider: "x",
		Getenv: func(string) string { return "from-env" },
		Secret: lookupOf(map[string]string{"x": "from-store"})}
	if key, err := c.credential(); err != nil || key != "from-env" {
		t.Fatalf("credential = %q, %v; an exported variable must keep winning", key, err)
	}
}

func TestAStoredKeyIsUsedWhenTheVariableIsEmpty(t *testing.T) {
	c := &Client{APIKeyEnv: "X_KEY", Provider: "x",
		Getenv: func(string) string { return "" },
		Secret: lookupOf(map[string]string{"x": " from-store \n"})}
	if key, err := c.credential(); err != nil || key != "from-store" {
		t.Fatalf("credential = %q, %v; want the trimmed stored key", key, err)
	}
}

func TestNeitherSourceNamesTheFixAndNeverALeakedKey(t *testing.T) {
	c := &Client{APIKeyEnv: "X_KEY", Provider: "x", BaseURL: "https://api.x.test/v1",
		Getenv: func(string) string { return "" },
		Secret: lookupOf(nil)}
	_, err := c.credential()
	var nc *ErrNoCredential
	if !errors.As(err, &nc) {
		t.Fatalf("err = %v; want *ErrNoCredential", err)
	}
	if !strings.Contains(err.Error(), "/provider") {
		t.Errorf("the message does not point at /provider: %v", err)
	}
}

func TestAClientWithNoProviderNameNeverTouchesTheStore(t *testing.T) {
	called := false
	c := &Client{APIKeyEnv: "X_KEY",
		Getenv: func(string) string { return "" },
		Secret: func(string) (string, bool, error) { called = true; return "leak", true, nil }}
	if _, err := c.credential(); err == nil {
		t.Fatal("want ErrNoCredential")
	}
	if called {
		t.Error("an anonymous client consulted the key store; tests would read a developer's real keys")
	}
}

func TestAKeylessLocalProviderStillWorksAndStillUsesAStoredToken(t *testing.T) {
	c := &Client{Provider: "local", Secret: lookupOf(nil)}
	if key, err := c.credential(); err != nil || key != "" {
		t.Fatalf("credential = %q, %v; loopback needs no key", key, err)
	}
	c.Secret = lookupOf(map[string]string{"local": "token"})
	if key, _ := c.credential(); key != "token" {
		t.Errorf("a token stored for a keyless provider was ignored: %q", key)
	}
}

func TestAnUnreadableStoreIsReportedWithoutTheKeyAndNotAsMissing(t *testing.T) {
	boom := errors.New("permission denied")
	c := &Client{APIKeyEnv: "X_KEY", Provider: "x",
		Getenv: func(string) string { return "" },
		Secret: func(string) (string, bool, error) { return "", false, boom }}
	_, err := c.credential()
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want the read failure, not a misleading 'key missing'", err)
	}
}
