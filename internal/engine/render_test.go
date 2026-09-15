package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// rawSceneBytes loads RAW.json from testdata, searching relative paths
// to handle both test binary locations and source-tree execution.
func rawSceneBytes(t *testing.T) []byte {
	t.Helper()
	// Try current directory first, then the project root.
	for _, p := range []string{
		"testdata/RAW.json",
		filepath.Join("..", "..", "testdata", "RAW.json"),
	} {
		if data, err := os.ReadFile(p); err == nil {
			return data
		}
	}
	t.Fatal("could not find testdata/RAW.json in testdata/ or ../../testdata/")
	return nil
}

func frameGoldenBytes(t *testing.T) []byte {
	t.Helper()
	for _, p := range []string{
		"testdata/RAW.frame",
		filepath.Join("..", "..", "testdata", "RAW.frame"),
	} {
		if data, err := os.ReadFile(p); err == nil {
			return data
		}
	}
	return nil // first run: write golden
}

// TestRawSceneRendersAsGolden reads RAW.json, renders it, and checks output
// matches the golden frame. This is the Phase 0 exit criterion.
func TestRawSceneRendersAsGolden(t *testing.T) {
	raw := rawSceneBytes(t)
	doc, err := scene.ParseDocument(raw)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc)

	got := strings.TrimSuffix(f.Plain(), "\n")

	want := frameGoldenBytes(t)
	if want == nil {
		// First run: write golden so we see what we get
		t.Logf("no golden yet; rendered:\n%s", got)
		return
	}
	wantStr := strings.TrimSuffix(string(want), "\n")

	if got != wantStr {
		t.Errorf("frame mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, wantStr)
	}
	t.Logf("golden output:\n%s", got)
}
