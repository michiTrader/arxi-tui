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

// The golden scenes' §4.5 audit lives in binds_audit_test.go
// (TestEveryGoldenBindIsSigned), which checks all three pinned scenes against
// the signed document itself rather than a hardcoded subset. It replaced the
// earlier test here, which listed only RAW and SOBRIA and so never validated
// MAXIMUM — the one scene that binds agent.todos and session.tokens_used.

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
