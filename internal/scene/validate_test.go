package scene

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// TestValidateTokensReportsUndefined verifies that a scene referencing tokens
// the theme does not define is rejected with a clear diagnostic. The token
// vocabulary is open — a theme can define any name — so validation must run
// against the concrete theme the scene will be rendered with, not a fixed list.
func TestValidateTokensReportsUndefined(t *testing.T) {
	src := []byte(`{
  "root": {
    "type": "text",
    "text": "hello",
    "style": {
      "token": "nonexistent.token"
    }
  }
}`)
	doc, err := ParseDocument(src)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	thm := theme.SOBRIA()
	errs := ValidateTokens(doc, thm)
	if len(errs) == 0 {
		t.Fatal("ValidateTokens returned no errors for undefined token 'nonexistent.token'; validation must catch tokens the theme does not define")
	}
	if len(errs) != 1 {
		t.Fatalf("ValidateTokens returned %d errors, want 1", len(errs))
	}
	if errs[0].Token != "nonexistent.token" {
		t.Errorf("error Token = %q, want %q", errs[0].Token, "nonexistent.token")
	}
	if errs[0].NodeType != "text" {
		t.Errorf("error NodeType = %q, want %q", errs[0].NodeType, "text")
	}
}

// TestValidateTokensAcceptsDefinedTokens verifies that a scene using only
// tokens the theme defines passes validation. SOBRIA defines 'dim' and
// 'bright', so a scene using those tokens should validate cleanly.
func TestValidateTokensAcceptsDefinedTokens(t *testing.T) {
	src := []byte(`{
  "root": {
    "type": "stack",
    "children": [
      {
        "type": "text",
        "text": "dim text",
        "style": { "token": "dim" }
      },
      {
        "type": "text",
        "text": "bright text",
        "style": { "token": "bright" }
      }
    ]
  }
}`)
	doc, err := ParseDocument(src)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	thm := theme.SOBRIA()
	errs := ValidateTokens(doc, thm)
	if len(errs) != 0 {
		t.Fatalf("ValidateTokens returned %d errors for defined tokens 'dim' and 'bright'; validation must accept tokens the theme defines. errors: %v", len(errs), errs)
	}
}

// TestValidateTokensReportsMultipleErrors verifies that validation collects
// all undefined token references rather than stopping at the first one, so
// the author sees the full scope of the problem in one pass.
func TestValidateTokensReportsMultipleErrors(t *testing.T) {
	src := []byte(`{
  "root": {
    "type": "stack",
    "children": [
      {
        "type": "text",
        "text": "one",
        "style": { "token": "missing.one" }
      },
      {
        "type": "text",
        "text": "two",
        "style": { "token": "missing.two" }
      },
      {
        "type": "text",
        "text": "valid",
        "style": { "token": "dim" }
      },
      {
        "type": "text",
        "text": "three",
        "style": { "token": "missing.three" }
      }
    ]
  }
}`)
	doc, err := ParseDocument(src)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	thm := theme.SOBRIA()
	errs := ValidateTokens(doc, thm)
	if len(errs) != 3 {
		t.Fatalf("ValidateTokens returned %d errors, want 3 (missing.one, missing.two, missing.three)", len(errs))
	}

	// Verify each missing token is reported exactly once.
	for _, want := range []string{"missing.one", "missing.two", "missing.three"} {
		found := false
		for _, e := range errs {
			if e.Token == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ValidateTokens did not report undefined token %q", want)
		}
	}
}

// TestValidateTokensIgnoresEmptyToken verifies that nodes without a token
// attribute are skipped — not every node needs styling, and an absent token
// is not an error.
func TestValidateTokensIgnoresEmptyToken(t *testing.T) {
	src := []byte(`{
  "root": {
    "type": "stack",
    "children": [
      {
        "type": "text",
        "text": "unstyled"
      },
      {
        "type": "text",
        "text": "styled",
        "style": { "token": "dim" }
      }
    ]
  }
}`)
	doc, err := ParseDocument(src)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	thm := theme.SOBRIA()
	errs := ValidateTokens(doc, thm)
	if len(errs) != 0 {
		t.Fatalf("ValidateTokens returned %d errors for a scene with unstyled nodes; an absent token is not an error. errors: %v", len(errs), errs)
	}
}

// TestValidateTokensChecksBorderStyles verifies that border style tokens are
// validated when a node declares border: { shape: "single", style: "warn" }.
func TestValidateTokensChecksBorderStyles(t *testing.T) {
	src := []byte(`{
  "root": {
    "type": "box",
    "border": { "shape": "single", "style": "undefined.border" },
    "children": [
      {
        "type": "text",
        "text": "content"
      }
    ]
  }
}`)
	doc, err := ParseDocument(src)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	thm := theme.SOBRIA()
	errs := ValidateTokens(doc, thm)
	if len(errs) == 0 {
		t.Fatal("ValidateTokens returned no errors for undefined border style 'undefined.border'; border tokens must be validated")
	}
	if len(errs) != 1 {
		t.Fatalf("ValidateTokens returned %d errors, want 1", len(errs))
	}
	if errs[0].Token != "undefined.border" {
		t.Errorf("error Token = %q, want %q", errs[0].Token, "undefined.border")
	}
}
