package main

import "testing"

// TestVersionDefaultsToDevNotEmpty pins the one thing the version stamping
// contract rests on: an un-stamped build names itself "dev", never the empty
// string. The release build overrides it through -ldflags "-X main.version=…"
// (scripts/build-release.sh); a plain `go build` leaves the default. If the
// default were ever "" a stock build would print a blank line for `-version` and
// a bug report could not say which binary it ran — the exact failure the default
// exists to prevent. This does not test the ldflags path (that is the build
// script's job, verified by running it); it guards the fallback the script
// depends on being present to override.
func TestVersionDefaultsToDevNotEmpty(t *testing.T) {
	if version == "" {
		t.Fatal("main.version is empty; an un-stamped build would print a blank -version line " +
			"and a bug report could not name the binary. Restore the \"dev\" default that the " +
			"release build overrides via -ldflags -X main.version.")
	}
}
