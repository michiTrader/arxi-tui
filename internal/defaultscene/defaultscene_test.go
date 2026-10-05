package defaultscene

import (
	"bytes"
	"os"
	"testing"
)

// The embedded copy and the testdata file are the same document. If this fails,
// copy testdata/SOBRIA.json over internal/defaultscene/SOBRIA.json (or the other way).
func TestEmbeddedSceneMatchesTestdata(t *testing.T) {
	want, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(JSON, want) {
		t.Fatal("internal/defaultscene/SOBRIA.json differs from testdata/SOBRIA.json")
	}
}
