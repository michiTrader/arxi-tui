package main

import (
	"fmt"
	"strings"
)

// This file is the host-level grammar for `/ui plugin install <url>`. It lives
// in the host, not the patch surface, because install is not a source-to-source
// transform: it fetches a bundle, lays it out on disk, drives a consent modal and
// spawns a subprocess it must hold for `/ui plugin remove`. The patch package is
// a pure offline transform (patch.Parse's Command comment argues exactly this
// boundary), so its closed verb set owns `add`/`remove` — the manifest-only
// paths — and install is intercepted here, before the loop reaches
// patch.ApplyWithFetch.
//
// Keeping it a pure function of the line, with no loop state and no network, is
// the same discipline consentAnswerForKey follows: the recognition of an install
// command and the extraction of its one argument is the decidable part, and a
// pure function is the one shape a counterfactual pins exactly. The async
// orchestration (fetch, install, modal, spawn) reads a resolved URL from here and
// carries no grammar of its own.

// parsePluginInstall recognizes the `/ui plugin install <url>` command and
// returns the URL to hand the installer.
//
// matched reports whether the line IS a `plugin install` invocation — the verb
// is `plugin` and its subcommand is `install`. When matched is false the line is
// some other /ui verb (or not a /ui line at all) and the host falls through to
// patch.ApplyWithFetch, which owns every verb this one does not.
//
// A line that IS `plugin install` but malformed returns matched=true with a
// located refusal in err, so the host reports the missing or malformed URL
// itself rather than letting the line fall through to the patch surface. That
// fall-through is the failure this split exists to avoid: patch.parsePlugin
// knows only `add`/`remove`, so it would refuse `install` as an unknown
// subcommand and bury the real problem — a URL the user forgot — under a grammar
// error naming the wrong thing.
func parsePluginInstall(line string) (rawURL string, matched bool, err error) {
	body := strings.TrimSpace(line)
	// Strip a leading "/ui" or "ui" exactly as patch.Parse does, so the two
	// grammars agree on what the line looks like once the prefix is gone. The
	// host calls this with the raw buffer (prefix present); accepting both
	// spellings keeps a future call site that has already stripped it working.
	for _, p := range []string{"/ui", "ui"} {
		if body == p {
			body = ""
			break
		}
		if strings.HasPrefix(body, p+" ") {
			body = strings.TrimSpace(body[len(p):])
			break
		}
	}

	fields := strings.Fields(body)
	// Not `plugin install …`: leave it for the patch surface. This is the only
	// exit that returns matched=false, so every genuine install attempt — even a
	// malformed one — is reported here rather than downstream.
	if len(fields) < 2 || fields[0] != "plugin" || fields[1] != "install" {
		return "", false, nil
	}

	args := fields[2:]
	// The URL is a single token, the same rule `/ui plugin add` enforces: a URL
	// with a space in it is not a URL, and splitting on spaces would let a
	// fat-fingered second word be silently dropped rather than refused. Scheme and
	// reachability are the fetcher's to check (httpArchiveFetcher.Fetch); this
	// only guarantees the command carries exactly one argument to hand it.
	if len(args) != 1 {
		return "", true, fmt.Errorf("/ui plugin install needs exactly one bundle URL: /ui plugin install <url>")
	}
	return args[0], true, nil
}

// parsePluginRemoveID reads the plugin id from a `/ui plugin remove <id>` line, so
// the host can Close a behavioral plugin's supervisor before the patch surface
// handles the document/token half of the same command.
//
// matched reports only that the line IS a `plugin remove` invocation with one id;
// it is a peek, not a takeover. A behavioral plugin (one this host spawned and
// holds a supervisor for) must have that process stopped, which the patch
// surface — a pure document transform — cannot do. So the host peeks the id here,
// stops the process if it is one it holds, and still lets the command flow to the
// patch surface for the declarative half (a plugin that also mounted fragments).
// A malformed remove (wrong id count) returns matched=false: the host does not own
// the refusal, the patch surface already refuses it with the right message, and
// duplicating that here would be a second grammar to drift.
func parsePluginRemoveID(line string) (id string, matched bool) {
	body := strings.TrimSpace(line)
	for _, p := range []string{"/ui", "ui"} {
		if body == p {
			body = ""
			break
		}
		if strings.HasPrefix(body, p+" ") {
			body = strings.TrimSpace(body[len(p):])
			break
		}
	}
	fields := strings.Fields(body)
	if len(fields) != 3 || fields[0] != "plugin" || fields[1] != "remove" {
		return "", false
	}
	return fields[2], true
}
