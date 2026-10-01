#!/usr/bin/env bash
# build-release.sh — cross-compile arxi-tui into per-platform static binaries.
#
# This is Block L2 of the install rule (docs/PLAN.md): "the user installs arxi,
# not arxi's dependencies." Every artifact is CGO_ENABLED=0 so it is a single
# static binary with no runtime requirement on the user's machine — the property
# that lets one `install.sh` work on a stock box and lets Termux get a real
# android-arm64 build rather than a "compile it yourself" apology.
#
# Usage:
#   scripts/build-release.sh [version]
#
# version defaults to `git describe --tags --always --dirty`, so a local run is
# labelled honestly (e.g. v0.1.0-3-g1a2b3c4-dirty) and a tagged CI run is the
# clean tag. The value is stamped into the binary through
# -ldflags "-X main.version=…", which `arxi-tui -version` prints.
#
# Output: dist/<artifact> for each target, plus dist/SHA256SUMS. install.sh
# fetches the artifact and verifies it against that file, so the checksums are
# part of the deliverable, not a convenience.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="dist"
PKG="./cmd/arxi-tui"
NAME="arxi-tui"

# The target matrix is the install rule's named set: linux, macos (darwin),
# windows, and android-arm64 for Termux, with arm64 alongside amd64 where the
# hardware is common (Apple Silicon, ARM servers, phones). Each entry is
# GOOS/GOARCH; the loop derives the artifact name and the .exe suffix from it.
TARGETS=(
	"linux/amd64"
	"linux/arm64"
	"darwin/amd64"
	"darwin/arm64"
	"windows/amd64"
	"android/arm64"
)

rm -rf "$OUT"
mkdir -p "$OUT"

echo "building $NAME $VERSION (CGO_ENABLED=0, static) for ${#TARGETS[@]} targets"
for t in "${TARGETS[@]}"; do
	os="${t%/*}"
	arch="${t#*/}"
	# android artifacts are labelled "termux" in the filename: that is the name
	# the user who needs this build searches for, not "android", and install.sh
	# detects Termux by its own environment to pick it.
	label="$os"
	if [ "$os" = "android" ]; then
		label="termux"
	fi
	artifact="${NAME}_${VERSION}_${label}_${arch}"
	ext=""
	if [ "$os" = "windows" ]; then
		ext=".exe"
	fi
	# -s -w strip the symbol table and DWARF: a smaller download, and the version
	# a user needs is carried by -X main.version, not by debug symbols.
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
		go build -trimpath \
		-ldflags "-s -w -X main.version=${VERSION}" \
		-o "${OUT}/${artifact}${ext}" "$PKG"
	echo "  ok  ${artifact}${ext}"
done

# One checksum file over every artifact, computed in dist/ so the names in it are
# bare and install.sh can verify against the file it downloads beside the binary.
# The artifacts are listed explicitly (arxi-tui_*) rather than with a bare glob so
# SHA256SUMS can never hash itself on a re-run. sha256sum is coreutils; the macOS
# fallback (shasum -a 256) prints the same two-column format.
(
	cd "$OUT"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum arxi-tui_* >SHA256SUMS
	else
		shasum -a 256 arxi-tui_* >SHA256SUMS
	fi
)
echo "wrote ${OUT}/SHA256SUMS"
echo "done: $VERSION"
