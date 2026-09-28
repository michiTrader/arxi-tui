package main

import (
	"net/http"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// This file is the cmd-edge orchestration for opening the community installer:
// the browse analogue of plugin_install.go. Opening the installer means fetching
// a registry index over the network, and a network fetch must never run on the
// loop goroutine — a hung download would freeze the loop and, with it, the panic
// gesture (invariant 6). So the fetch runs on a worker goroutine and its result
// arrives back on a channel the loop's select reads, exactly the shape the
// install worker uses. The keystroke routing over the opened browse is the pure
// routeBrowseKey; this half is only the fetch that produces the browse.

// browseOutcome is the worker→loop message carrying a registry fetch's result.
// url is echoed so a message can name what the user typed; reg is the parsed,
// validated index on success (nil on any failure); err distinguishes a fetch,
// HTTPS-scheme, status or malformed-index failure so the loop can name it. On
// success the loop opens an installerBrowse over reg; on failure it reports err
// and stays on the normal scene.
type browseOutcome struct {
	url string
	reg *ext.Registry
	err error
}

// newRegistryFetchClient builds the bounded HTTP client the browse fetch uses. It
// mirrors the manifest fetcher's 15s timeout for the same reason: the index is a
// small static JSON file, so the timeout only has to outlast a slow-but-honest
// server, and a hostile one that never finishes is cut off rather than pinning a
// worker goroutine for the session. Built once by the loop so a change to the
// bound is one edit a reviewer sees, like the other fetchers' constructors.
func newRegistryFetchClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// startBrowseFetch launches the worker that fetches one `/ui plugin browse <url>`
// registry index and reports the outcome on done. The whole fetch→parse→validate
// runs on the goroutine so the loop is never blocked on the network; the loop only
// records that a fetch is in flight and reacts to the outcome in its select. done
// is buffered (size 1) so the send never blocks even if the loop has moved on, and
// only one fetch is ever in flight (the loop's browseBusy guard), so a single slot
// is enough. FetchRegistryWithClient keeps the HTTPS-only rule and every index
// refusal; the client only bounds the transport.
func startBrowseFetch(url string, client *http.Client, done chan<- browseOutcome) {
	go func() {
		reg, err := ext.FetchRegistryWithClient(client, url)
		done <- browseOutcome{url: url, reg: reg, err: err}
	}()
}
