package scene

import (
	"os"
	"path/filepath"
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

// TestValidateTokensChecksTheKeyTheScenesActuallyUse is the regression test for
// a gap that every other token test in this file walked straight past.
//
// collectTokenErrors read style["token"]. The shipped scenes, SCENES.md and
// TOKENS.md all write the style reference as style["style"] — SOBRIA's header
// row is `"style": {"style": "header"}` — and styleName() in the render path
// reads style["style"] too. So the validator was checking a key the format does
// not use, and the only reason no test noticed is that every token test here
// was written with the validator's key rather than the scenes' key.
//
// What that cost, measured rather than supposed: SOBRIA does not define
// "header", SOBRIA.json references it twice, and ValidateTokens(SOBRIA) returned
// zero errors. The default interface shipped with an undefined token reference
// that the token validator existed to catch.
//
// Why this is worth a test rather than a quiet fix. TOKENS.md signs the
// promise — "the validator walks the scene tree, collects every "style" value,
// and checks each against the theme's key set" — and LESSONS.md's rule, with
// arxi-sim's polarity inverted, is that the reference is checked, not the
// inventory. A validator that reads the wrong key keeps both promises on paper
// and neither in fact; worse, it is the exact failure mode the corpus cannot
// see, because a corpus case naming the "token" key gets a real refusal and
// concludes the machinery works.
func TestValidateTokensChecksTheKeyTheScenesActuallyUse(t *testing.T) {
	// The spelling used by the three golden scenes and by every example in
	// SCENES.md and TOKENS.md.
	src := []byte(`{
  "root": {
    "type": "text",
    "text": "hello",
    "style": { "style": "nonexistent.token" }
  }
}`)
	doc, err := ParseDocument(src)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	errs := ValidateTokens(doc, theme.SOBRIA())
	if len(errs) == 0 {
		t.Fatalf("ValidateTokens accepted the undefined token %q written as \"style\": {\"style\": ...}\n"+
			"consequence: this is the spelling the shipped scenes use, so the token validator is blind to the only form that occurs in practice — SOBRIA references the undefined token \"header\" twice and validates clean.\n"+
			"remedy: collect the style reference from both keys in collectTokenErrors, not just style[\"token\"].",
			"nonexistent.token")
	}
	if errs[0].Token != "nonexistent.token" {
		t.Errorf("error Token = %q, want %q", errs[0].Token, "nonexistent.token")
	}
	if errs[0].Loc.Line == 0 {
		t.Errorf("token refusal carries no address\n" +
			"consequence: invariant 4 requires file:line on every refusal, and the repair loop is exactly the case where it has to be read.\n" +
			"remedy: populate Loc from the offending node's path.")
	}
}

// TestTheShippedScenesReferenceOnlyDefinedTokens holds the factory scenes to
// the rule the validator enforces for everyone else.
//
// A downloaded scene naming an undefined token is refused at load; the scenes
// arxi ships must clear the same bar, or the product is enforcing on community
// content a standard its own default interface fails. This is the check that
// would have caught "header" the day it was written, independent of whichever
// key the validator happens to read.
func TestTheShippedScenesReferenceOnlyDefinedTokens(t *testing.T) {
	thm := theme.SOBRIA()
	for _, name := range []string{"RAW.json", "SOBRIA.json", "MAXIMUM.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
			if err != nil {
				t.Fatalf("read scene: %v", err)
			}
			doc, err := ParseNamed(name, raw)
			if err != nil {
				t.Fatalf("parse scene: %v", err)
			}
			for _, e := range ValidateTokens(doc, thm) {
				t.Errorf("shipped scene references undefined token %q (%s)\n"+
					"consequence: the factory interface fails the rule the validator applies to community content, and a theme swap can leave a node with no style at all.\n"+
					"remedy: define the token in the theme, or use one the theme already defines.",
					e.Token, e.Error())
			}
		})
	}
}
