package main

import "testing"

// These tests pin the host-level `/ui plugin install <url>` grammar. The
// security-relevant property is not the URL validation (the fetcher owns scheme
// and reachability) but the DISPATCH boundary: exactly the install lines are
// claimed here, and every other /ui line is left for the patch surface. A
// parser that over-claims would swallow `/ui plugin add` into the install path
// (spawning a subprocess for a manifest-only mount); one that under-claims would
// let `/ui plugin install` fall through to patch, which refuses it as an unknown
// subcommand and reports the wrong problem.

func TestParsePluginInstallExtractsTheURL(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"with the /ui prefix", "/ui plugin install https://example.com/p.tar.gz", "https://example.com/p.tar.gz"},
		{"without the /ui prefix", "plugin install https://example.com/p.tar.gz", "https://example.com/p.tar.gz"},
		{"extra surrounding spaces", "  /ui   plugin   install   https://x/p.tgz  ", "https://x/p.tgz"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			url, matched, err := parsePluginInstall(c.line)
			if !matched {
				t.Fatalf("%q was not recognized as a plugin install command.\nConsequence: the line falls through to the patch surface, which knows only add/remove and refuses install as an unknown subcommand — the user sees the wrong error.\nRemedy: parsePluginInstall must claim every `plugin install` line.", c.line)
			}
			if err != nil {
				t.Fatalf("%q is a well-formed install command but was refused: %v", c.line, err)
			}
			if url != c.want {
				t.Fatalf("install URL mismatch for %q: got %q, want %q.\nConsequence: the installer fetches the wrong bytes, or none.\nRemedy: return the single argument token verbatim.", c.line, url, c.want)
			}
		})
	}
}

func TestParsePluginInstallLeavesOtherLinesToThePatchSurface(t *testing.T) {
	// Each of these is a real /ui line the patch surface owns, or not a /ui line
	// at all. None may be claimed here: matched=true would route it into the
	// install orchestration, and `/ui plugin add` in particular would then spawn a
	// subprocess for what is a manifest-only mount.
	lines := []string{
		"/ui plugin add https://example.com/m.json",
		"/ui plugin remove weather",
		"/ui plugin",
		"/ui style status dim",
		"/ui hide status",
		"/ui add node below chat {}",
		"/uize the thing",
		"just some chat text",
		"",
	}
	for _, line := range lines {
		if _, matched, _ := parsePluginInstall(line); matched {
			t.Errorf("parsePluginInstall claimed %q, which it does not own.\nConsequence: a non-install line is routed into the fetch+spawn orchestration; `/ui plugin add` would spawn a subprocess for a manifest-only mount.\nRemedy: claim a line only when its verb is `plugin` and its subcommand is `install`.", line)
		}
	}
}

func TestParsePluginInstallRefusesAMissingURLItself(t *testing.T) {
	// `plugin install` with no URL is unmistakably an install attempt, so it is
	// claimed (matched) and refused HERE with a message naming the missing URL —
	// not left to fall through to patch, whose "unknown subcommand" names the
	// wrong problem. A second argument is refused for the same reason `plugin add`
	// refuses it: a URL is a single token, and a dropped second word is a silent
	// wrong install.
	for _, line := range []string{
		"/ui plugin install",
		"/ui plugin install https://x/a.tgz https://x/b.tgz",
	} {
		url, matched, err := parsePluginInstall(line)
		if !matched {
			t.Errorf("%q is a malformed install command but was not claimed; it would fall through to the patch surface and be refused as an unknown subcommand, naming the wrong problem.", line)
		}
		if err == nil {
			t.Errorf("%q is malformed (wrong argument count) but was accepted with url=%q.\nConsequence: the installer is handed no URL or a silently-dropped one.\nRemedy: refuse any argument count other than one.", line, url)
		}
	}
}
