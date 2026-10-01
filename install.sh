#!/usr/bin/env sh
# install.sh — fetch the right arxi-tui static binary for this machine, verify
# it, and drop it on PATH. One command, Termux included.
#
# This is the user-facing half of the install rule (docs/PLAN.md): "the user
# installs arxi, not arxi's dependencies." scripts/build-release.sh produces one
# CGO_ENABLED=0 static binary per platform plus a SHA256SUMS file and publishes
# them to a GitHub Release; this script picks the artifact that matches `uname`
# (and detects Termux by its own environment, not by "android"), downloads it
# beside SHA256SUMS, verifies the checksum, and installs it. No compiler, no Go
# toolchain, no runtime dependency on the user's box — that is the whole point
# of the static-binary rule.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/michitrader/arxi-tui/master/install.sh | sh
#   # or, pinning a version / install dir:
#   ARXI_VERSION=v0.1.0 ARXI_BIN_DIR=$HOME/.local/bin sh install.sh
#
# This script is POSIX sh (not bash): it has to run under Termux's shell and a
# stock macOS /bin/sh alike. No arrays, no [[ ]], no `local`.
set -eu

REPO="michitrader/arxi-tui"
NAME="arxi-tui"
# ARXI_VERSION pins a tag; empty means "latest", resolved from the redirect of
# the releases/latest URL so we never hardcode a version this script outlives.
VERSION="${ARXI_VERSION:-latest}"

# ---------------------------------------------------------------------------
# Platform detection. The label set here must match build-release.sh exactly:
# linux / darwin / windows / termux, with amd64 / arm64. A mismatch here means a
# 404 at download time, so the two files are a single contract.
# ---------------------------------------------------------------------------
detect_label() {
	# Termux is detected by its own environment, not by `uname` (which prints
	# "Linux"): the build the user needs is the one labelled "termux", and only
	# Termux knows it is Termux.
	if [ -n "${TERMUX_VERSION:-}" ] || [ -n "${PREFIX:-}" ] && [ -d "${PREFIX:-/nonexistent}/bin" ] && command -v termux-info >/dev/null 2>&1; then
		echo "termux"
		return
	fi
	case "$(uname -s)" in
		Linux) echo "linux" ;;
		Darwin) echo "darwin" ;;
		MINGW* | MSYS* | CYGWIN* | Windows_NT) echo "windows" ;;
		*) echo "unsupported" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64 | amd64) echo "amd64" ;;
		arm64 | aarch64) echo "arm64" ;;
		*) echo "unsupported" ;;
	esac
}

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

LABEL="$(detect_label)"
ARCH="$(detect_arch)"
[ "$LABEL" = "unsupported" ] && fail "unsupported OS: $(uname -s). Build from source with: go install ./cmd/$NAME"
[ "$ARCH" = "unsupported" ] && fail "unsupported CPU: $(uname -m). Build from source with: go install ./cmd/$NAME"
# Termux is android-arm64 only; refuse an amd64 Termux rather than 404 silently.
[ "$LABEL" = "termux" ] && [ "$ARCH" != "arm64" ] && fail "Termux build is android-arm64 only, got $ARCH"

EXT=""
[ "$LABEL" = "windows" ] && EXT=".exe"

# ---------------------------------------------------------------------------
# Resolve the version. For "latest" we follow the releases/latest redirect,
# which lands on .../tag/<version>, and read the tag off the end — no API token,
# no jq, works offline-of-GitHub only in that it needs GitHub (which is where the
# artifacts live anyway).
# ---------------------------------------------------------------------------
have() { command -v "$1" >/dev/null 2>&1; }

# dl URL OUTFILE — download with whatever is on the box. curl first (it is on
# macOS and most Termux installs), wget as the Linux fallback.
dl() {
	if have curl; then
		curl -fsSL "$1" -o "$2"
	elif have wget; then
		wget -qO "$2" "$1"
	else
		fail "need curl or wget to download"
	fi
}

# dl_effective_url URL — print the URL after redirects, without downloading the
# body. Used only to turn releases/latest into a concrete tag.
dl_effective_url() {
	if have curl; then
		curl -fsSLI -o /dev/null -w '%{url_effective}' "$1"
	elif have wget; then
		# wget prints "Location:" lines on redirect; take the last one.
		wget --spider -S "$1" 2>&1 | awk '/^  Location: /{u=$2} END{print u}'
	else
		fail "need curl or wget to download"
	fi
}

if [ "$VERSION" = "latest" ]; then
	eff="$(dl_effective_url "https://github.com/$REPO/releases/latest")"
	VERSION="${eff##*/tag/}"
	case "$VERSION" in
		"" | *github.com*) fail "could not resolve latest release — set ARXI_VERSION explicitly" ;;
	esac
fi

ARTIFACT="${NAME}_${VERSION}_${LABEL}_${ARCH}${EXT}"
BASE="https://github.com/$REPO/releases/download/$VERSION"

echo "arxi-tui installer"
echo "  version:  $VERSION"
echo "  platform: $LABEL/$ARCH"
echo "  artifact: $ARTIFACT"

TMP="$(mktemp -d 2>/dev/null || mktemp -d -t arxi)"
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "downloading…"
dl "$BASE/$ARTIFACT" "$TMP/$ARTIFACT"
dl "$BASE/SHA256SUMS" "$TMP/SHA256SUMS"

# ---------------------------------------------------------------------------
# Verify. The checksum is part of the deliverable, not a nicety: a tampered or
# truncated download must not be installed. We verify just our one artifact by
# filtering SHA256SUMS to its line, so a checksum file covering six platforms
# does not fail on the five we did not download.
# ---------------------------------------------------------------------------
echo "verifying checksum…"
line="$(grep " [*]\{0,1\}${ARTIFACT}\$" "$TMP/SHA256SUMS" || true)"
[ -n "$line" ] || fail "no checksum for $ARTIFACT in SHA256SUMS"
want="${line%% *}"
if have sha256sum; then
	got="$(sha256sum "$TMP/$ARTIFACT" | cut -d' ' -f1)"
elif have shasum; then
	got="$(shasum -a 256 "$TMP/$ARTIFACT" | cut -d' ' -f1)"
else
	fail "need sha256sum or shasum to verify"
fi
[ "$want" = "$got" ] || fail "checksum mismatch for $ARTIFACT (want $want, got $got)"

# ---------------------------------------------------------------------------
# Install. Default dir prefers a writable user bin so the common path needs no
# sudo; on Termux that is $PREFIX/bin (already on PATH). The binary is installed
# under the plain name "arxi-tui" regardless of the artifact's platform suffix.
# ---------------------------------------------------------------------------
if [ -n "${ARXI_BIN_DIR:-}" ]; then
	BIN_DIR="$ARXI_BIN_DIR"
elif [ -n "${PREFIX:-}" ] && [ -d "${PREFIX}/bin" ]; then
	BIN_DIR="$PREFIX/bin"
elif [ -d "$HOME/.local/bin" ]; then
	BIN_DIR="$HOME/.local/bin"
else
	BIN_DIR="$HOME/.local/bin"
	mkdir -p "$BIN_DIR"
fi

DEST="$BIN_DIR/${NAME}${EXT}"
install -m 0755 "$TMP/$ARTIFACT" "$DEST" 2>/dev/null || {
	# install(1) is absent on some minimal boxes; fall back to cp + chmod.
	cp "$TMP/$ARTIFACT" "$DEST"
	chmod 0755 "$DEST"
}

echo "installed $DEST"
case ":$PATH:" in
	*":$BIN_DIR:"*) ;;
	*) echo "note: $BIN_DIR is not on your PATH — add it, e.g. export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac
echo "run: $NAME -version"
