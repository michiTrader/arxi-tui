package scene

import (
	"os"
	"testing"
)

func TestRawParsing(t *testing.T) {
	// Golden file path: project root /testdata
	data, err := os.ReadFile("../../testdata/RAW.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if doc.Root == nil {
		t.Fatal("document has no root")
	}
	t.Logf("parsed root.type=%s", doc.Root.Type)
}