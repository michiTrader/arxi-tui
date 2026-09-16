package scene

import (
	"os"
	"strings"
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

// TestAllGoldenScenesValidate verifies that every golden scene's bind/when
// strings resolve to a signed row in docs/BINDS.md Section 4 (the §4.5 exit
// criterion). A golden scene referencing an unsigned bind is a file:line
// validation error at load time.
func TestAllGoldenScenesValidate(t *testing.T) {
	scenes := []string{"RAW.json", "SOARIA.json"}
	for _, name := range scenes {
		data, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatalf("%s: read file: %v", name, err)
		}
		doc, err := ParseDocument(data)
		if err != nil {
			t.Fatalf("%s: ParseDocument: %v", name, err)
		}
		if err := doc.Validate(); err != nil {
			t.Errorf("%s: Validate: %v — every bind must resolve to a signed row in BINDS.md §4.5", name, err)
		}
	}
}

// TestUnsignedBindFailsValidation verifies that a scene referencing a bind
// not in the signed inventory fails validation. This is the §4.5 exit
// criterion: an unsigned bind is a load-time error.
func TestUnsignedBindFailsValidation(t *testing.T) {
	doc, err := ParseDocument([]byte(`{ "root": { "type": "markdown", "bind": "chat.history" }}`))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("valid scene should pass: %v", err)
	}

	bad, err := ParseDocument([]byte(`{ "root": { "type": "text", "bind": "agent.nonexistent" }}`))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	err = bad.Validate()
	if err == nil {
		t.Fatal("scene with unsigned bind should fail validation")
	}
	if !strings.Contains(err.Error(), "unsigned bind") {
		t.Errorf("validation error should mention 'unsigned bind', got: %v", err)
	}
}
