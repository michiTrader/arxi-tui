package ext

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// ConsentScene authors the screen the host shows when the gate returns
// DecisionNeedsConsent for a behavioral plugin (I5). It is built the way the
// change-diff view is (PLAN.md ADR-0003, internal/patch/diff.go): a
// host-generated scene of node types the engine already renders, not a bespoke
// engine capability. The consent screen is chrome the user must trust, so it is
// authored through exactly the parse path a user's own scene takes — the
// dogfooding thesis of the whole project, applied to the one screen whose job
// is to earn the user's trust.
//
// # Why the view is a pure function separate from the prompt
//
// supervisor.Mount takes a Prompt callback (supervisor/mount.go) so the mount
// orchestration carries no UI. This is the visual half of that seam: it renders
// what the user reads, and the loop layer that reads a Y/N/remember keypress
// against it lands separately, exactly as the diff view (B4) was authored and
// golden-pinned before it met the live agent (B5). Keeping the view a pure
// Manifest -> *scene.Document function lets a golden pin the screen without a
// running subprocess or a live gate.
//
// # What it must show, and why every field earns its place
//
// The grant the user is about to make is bound to the identity tuple (I-H):
// name + version + protocol + executable + args + capability-set + digest. The
// screen shows exactly that tuple, because consent to a grant the user cannot
// see the terms of is not consent — a version the screen hid could re-run under
// a remembered grant, and an executable or arg the screen hid is a different
// program running under the same yes. The capabilities are the crux: they are
// the powers the plugin will receive, so they are listed one per row and
// emphasised, and a manifest that declares none says so rather than showing an
// empty space the user reads as "nothing to grant" versus "the list failed to
// render".
//
// digest is passed in rather than read from the manifest because it is the
// loader's PackageDigest over the fetched bytes (identity.go), not a manifest
// field — the screen must show the digest the grant will bind to, which only
// the caller that fetched the package can supply.
func ConsentScene(m *Manifest, digest string) (*scene.Document, error) {
	if m == nil {
		return nil, fmt.Errorf("ConsentScene: cannot render a consent screen for a nil manifest")
	}

	rows := make([]any, 0, 12)

	// Identity block. name+version lead as the human handle; id/protocol are the
	// machine identity below it; the executable+args line is what will actually
	// run, so it is plain (not dimmed) — it is not secondary. The digest is dimmed
	// like the other machine metadata but shown in full: a truncated digest is a
	// weaker identity than the grant is bound to, and the screen must not claim a
	// shorter check than the gate performs.
	rows = append(rows,
		textRow(fmt.Sprintf("%s  v%s", m.Name, m.Version), "header"),
		textRow(fmt.Sprintf("id: %s   protocol: %s", m.ID, m.Protocol), "dim"),
		textRow("runs: "+runLine(m), ""),
		textRow("package digest: "+digest, "dim"),
		textRow("", ""), // a blank spacer row, kept so the rows keep a readable rhythm
	)

	// Capability block. This is the decision the screen exists to inform, so the
	// heading is emphasised and each requested power is its own row. A manifest
	// with no capabilities is spelled out ("no host powers") rather than left
	// blank, so "runs powerless" and "the list did not render" cannot look the
	// same.
	rows = append(rows, textRow("Capabilities requested:", "header"))
	if len(m.Capabilities) == 0 {
		rows = append(rows, textRow("  (none — this plugin runs with no host powers)", "dim"))
	} else {
		for _, c := range m.Capabilities {
			rows = append(rows, textRow("  • "+c, ""))
		}
	}
	rows = append(rows, textRow("", ""))

	// The prompt. The keys named here are the ones the loop's consent-reading
	// layer binds (a later increment); the view states them so the golden pins
	// the contract the reader must honour, the same way the slash menu draws the
	// keys before the loop dispatches them.
	rows = append(rows,
		textRow("Grant these powers?  [y] grant   [n] reject", "banner"),
		textRow("[r] grant and remember for this exact plugin", "banner"),
	)

	doc := map[string]any{
		"root": map[string]any{
			"type":     "box",
			"border":   "single",
			"title":    "A plugin is requesting consent",
			"children": []any{map[string]any{"type": "stack", "children": rows}},
		},
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not serialise the consent scene: %w", err)
	}
	return scene.ParseNamed("consent", out)
}

// runLine is the executable-and-args line the identity binds to. A declarative
// manifest has no executable and never reaches this screen, but the function is
// total rather than assuming that: an empty executable renders as "(none)" so a
// misused call produces a visible screen rather than a blank "runs: " the reader
// cannot interpret. Args are space-joined after the executable, which is how the
// grant remembers them (identity.go sorts capabilities but keeps args ordered,
// so the order shown is the order that matters).
func runLine(m *Manifest) string {
	if m.Executable == "" {
		return "(none)"
	}
	if len(m.Args) == 0 {
		return m.Executable
	}
	return m.Executable + " " + strings.Join(m.Args, " ")
}

// textRow is one line of the consent screen: a text node carrying the literal
// content, optionally under a token that emphasises it. It mirrors diff.go's
// lineNode so the two host-generated views author a styled line the same way
// rather than each inventing its own node shape.
//
// An empty token emits a node with NO style key rather than a node styled
// "text": the Factory backstop theme signs dim/header/banner (and the diff
// tokens) but not "text", so a plain row must reference no token at all to
// render under both themes — and a node with no style is exactly a plain,
// full-brightness line, which is what the runs line and the capability rows
// want. Only the tokens both themes sign (header/dim/banner) are used for the
// emphasised rows.
func textRow(text, token string) map[string]any {
	row := map[string]any{
		"type": "text",
		"text": text,
	}
	if token != "" {
		row["style"] = map[string]any{"style": token}
	}
	return row
}
