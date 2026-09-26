package patch

import (
	"errors"
	"strings"
	"testing"
)

// This file covers H6: the `/ui plugin add <url>` / `/ui plugin remove <id>`
// command surface — the parse grammar, the Fetcher seam, and the routing into
// the H3 composer (Mount/Unmount). The composition itself (id prefixing,
// uniqueness, the behavioral refusal, invariant-3 re-validation) is proven in
// mount_test.go; these tests prove the command reaches it, that the network
// stays behind an injected Fetcher, and that a fetched manifest gets the same
// gate a local one does.

// sweepManifest is a minimal valid declarative manifest a fake fetcher returns.
// It mounts one overlay fragment at an overlay anchor, so it composes into any
// host with a root without naming a host id — the mount cannot fail for a reason
// unrelated to the command path under test.
const sweepManifest = `{
  "id": "probe",
  "name": "Fetch Probe",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "mounts": [ { "where": "top-right", "fragment":
    { "id": "panel", "type": "overlay", "anchor": "top-right",
      "children": [ { "type": "text", "text": "hi" } ] } } ]
}`

// behavioralManifest carries an executable, so it is behavioral: the H2 split
// says a fetched one must be refused by the same gate a local one is, which is
// the counterfactual that fetching does not buy a manifest past the mounter.
const behavioralManifest = `{
  "id": "probe",
  "name": "Fetch Probe",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "executable": "./probe",
  "consent_required": true,
  "mounts": [ { "where": "top-right", "fragment":
    { "id": "panel", "type": "text", "text": "hi" } } ]
}`

// fakeFetcher is a patch.Fetcher that returns fixed bytes (or an error) instead
// of touching the network, so every plugin-add test runs offline. It records the
// URL it was asked for, so a test can assert the parsed URL reached the fetch.
type fakeFetcher struct {
	data []byte
	err  error
	name string
	got  string
}

func (f *fakeFetcher) Fetch(rawURL string) (string, []byte, error) {
	f.got = rawURL
	if f.err != nil {
		return "", nil, f.err
	}
	name := f.name
	if name == "" {
		name = rawURL
	}
	return name, f.data, nil
}

// TestPluginAddFetchesValidatesAndMounts is the positive control: a fetched
// declarative manifest composes into the host, the URL reaches the fetcher, the
// mounted id arrives prefixed, and the document validates. If this fails the
// refusal tests below are measuring a path that mounts nothing.
func TestPluginAddFetchesValidatesAndMounts(t *testing.T) {
	name, src := sobria(t)
	fetch := &fakeFetcher{data: []byte(sweepManifest), name: "probe.json"}

	res, err := ApplyWithFetch(name, src, "/ui plugin add https://example.test/probe.json", fetch)
	if err != nil {
		t.Fatalf("`/ui plugin add` refused a well-formed declarative manifest: %v; H6's promise is that a fetched declarative plugin mounts", err)
	}
	if fetch.got != "https://example.test/probe.json" {
		t.Errorf("the fetcher was asked for %q, want the parsed URL.\nConsequence: the command would fetch the wrong bytes, or none.\nRemedy: pass Command.Value (the URL) to Fetch.", fetch.got)
	}
	if res.Doc == nil || res.Source == nil {
		t.Fatalf("a successful mount must return a document and its source, got Doc=%v Source=%v", res.Doc, res.Source)
	}
	if !idsIn(t, res.Source)["probe/panel"] {
		t.Fatalf("composed ids = %v; the mounted node must arrive as \"probe/panel\" (H-B.3 prefixes every mounted id), so `plugin add` routes through the same Mount the H3 tests cover", keys(idsIn(t, res.Source)))
	}
	if res.Summary == "" {
		t.Error("`/ui plugin add` produced no summary; the change-diff view needs one before the mount is trusted.")
	}
}

// TestPluginAddWithNoFetcherIsRefused proves the nil-Fetcher path is a reasoned
// refusal, not a panic. Apply (the fetch-free spelling) hands applyPlugin a nil
// Fetcher, and `plugin add` is the one verb that needs one — so it must say so,
// not dereference nil.
func TestPluginAddWithNoFetcherIsRefused(t *testing.T) {
	name, src := sobria(t)
	_, err := Apply(name, src, "/ui plugin add https://example.test/probe.json")
	if err == nil {
		t.Fatal("`/ui plugin add` with no fetcher was accepted; without a network fetcher there are no manifest bytes, so it must be refused rather than silently doing nothing")
	}
	if !strings.Contains(err.Error(), "fetcher") {
		t.Errorf("the no-fetcher refusal must say a fetcher is missing so the caller knows the context, not the command, is at fault.\n  got: %v", err)
	}
}

// TestPluginAddPropagatesFetchError proves a fetch failure reaches the user
// unchanged rather than being swallowed into a generic refusal: the endpoint's
// own error (a 404, a timeout, a bad scheme) is the one thing that tells the user
// why the URL did not load.
func TestPluginAddPropagatesFetchError(t *testing.T) {
	name, src := sobria(t)
	sentinel := errors.New("connection refused by test")
	fetch := &fakeFetcher{err: sentinel}

	_, err := ApplyWithFetch(name, src, "/ui plugin add https://example.test/probe.json", fetch)
	if err == nil {
		t.Fatal("a fetch that failed produced no error; the command must surface why the URL did not load")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("the fetch error was not propagated.\n  got: %v\nConsequence: the user sees a generic failure instead of the endpoint's reason.\nRemedy: return the fetcher's error unchanged.", err)
	}
}

// TestPluginAddRefusesABehavioralManifest is H6's load-bearing counterfactual:
// fetching a manifest does not buy it past the H2 split. A fetched behavioral
// manifest (one with an executable) is refused by the same Mount gate a local one
// is — the fetch succeeds, the mount refuses — so `/ui plugin add` cannot become
// a back door that runs code the declarative path promised it would not.
func TestPluginAddRefusesABehavioralManifest(t *testing.T) {
	name, src := sobria(t)
	fetch := &fakeFetcher{data: []byte(behavioralManifest), name: "probe.json"}

	_, err := ApplyWithFetch(name, src, "/ui plugin add https://example.test/probe.json", fetch)
	if err == nil {
		t.Fatal("`/ui plugin add` mounted a behavioral manifest; mounting a process is Block I, so a fetched manifest with an executable must be refused exactly as a local one is")
	}
	if !strings.Contains(err.Error(), "behavioral") && !strings.Contains(err.Error(), "executable") {
		t.Errorf("the behavioral refusal must name the reason (an executable makes it behavioral), so the author knows which half to fix.\n  got: %v", err)
	}
}

// TestPluginRemoveUnmounts proves `remove` routes to Unmount and is a pure
// source edit needing no fetcher: mounting then removing the same plugin returns
// the original document, the round-trip fork 4 requires, driven through the
// command surface rather than the Mount/Unmount functions directly.
func TestPluginRemoveUnmounts(t *testing.T) {
	name, src := sobria(t)
	fetch := &fakeFetcher{data: []byte(sweepManifest), name: "probe.json"}

	mounted, err := ApplyWithFetch(name, src, "/ui plugin add https://example.test/probe.json", fetch)
	if err != nil {
		t.Fatalf("mounting the probe plugin failed: %v", err)
	}
	if !idsIn(t, mounted.Source)["probe/panel"] {
		t.Fatalf("the probe did not mount, so the remove below would prove nothing")
	}

	// remove needs no fetcher: Apply (nil Fetcher) must handle it.
	removed, err := Apply(name, mounted.Source, "/ui plugin remove probe")
	if err != nil {
		t.Fatalf("`/ui plugin remove probe` refused to remove a plugin just mounted: %v", err)
	}
	if idsIn(t, removed.Source)["probe/panel"] {
		t.Fatalf("composed ids = %v; remove must drop every probe/-prefixed node", keys(idsIn(t, removed.Source)))
	}

	want, err := canonical(src)
	if err != nil {
		t.Fatalf("canonical(sobria): %v", err)
	}
	if string(removed.Source) != string(want) {
		t.Fatalf("mount+remove did not restore the document.\n got:\n%s\nwant:\n%s\nremove must drop exactly the probe/-prefixed nodes and nothing else", removed.Source, want)
	}
}

// TestPluginParseRefusals covers the grammar refusals: a plugin command that
// names no subcommand, an unknown subcommand, and the wrong argument count for
// each. Each is refused at Parse, before any fetch or mount, because a
// malformed command should never reach the network.
func TestPluginParseRefusals(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"/ui plugin", "subcommand"},
		{"/ui plugin sideways foo", "subcommand"},
		{"/ui plugin add", "URL"},
		{"/ui plugin add a b", "URL"},
		{"/ui plugin remove", "plugin id"},
		{"/ui plugin remove a b", "plugin id"},
	}
	for _, c := range cases {
		_, err := Parse(c.line)
		if err == nil {
			t.Errorf("%q was accepted; a malformed plugin command must be refused at parse, before any fetch", c.line)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q refusal = %q; it must name %q so the user learns the shape", c.line, err.Error(), c.want)
		}
	}
}

// TestPluginAddAndRemoveParseIntoTheRightCommand pins that the subcommand lands
// in Key and its argument in Value, the fields applyPlugin reads. A drift here —
// the URL landing in Target, say — would route the fetch to the wrong string
// while every higher-level test that mocks the fetcher still passed.
func TestPluginAddAndRemoveParseIntoTheRightCommand(t *testing.T) {
	add, err := Parse("/ui plugin add https://example.test/x.json")
	if err != nil {
		t.Fatalf("plugin add parse: %v", err)
	}
	if add.Verb != "plugin" || add.Key != "add" || add.Value != "https://example.test/x.json" {
		t.Errorf("plugin add parsed to %+v; want Verb=plugin Key=add Value=<url>", add)
	}
	rm, err := Parse("/ui plugin remove probe")
	if err != nil {
		t.Fatalf("plugin remove parse: %v", err)
	}
	if rm.Verb != "plugin" || rm.Key != "remove" || rm.Value != "probe" {
		t.Errorf("plugin remove parsed to %+v; want Verb=plugin Key=remove Value=probe", rm)
	}
}
