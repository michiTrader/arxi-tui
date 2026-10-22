# Guardrails - what must not get worse while the interface opens up

Status: **in force (2026-10-20)**. These are the limits that keep arxi-tui what it is today
while `docs/DESIGN-OPEN-TUI.md` is built underneath it: one static binary, no C, installs on
Termux with one command, and a factory interface that does not move by accident.

Each limit is a table in this file, and `internal/guard` reads the table and fails when the
repository disagrees with it. A limit is raised here first, with the reason in the commit;
never by editing the test. The tests follow the rule of `docs/KEYS.md`: the document and the
code must say the same thing.

## 1. Dependencies

The only modules `go.mod` may name, direct or indirect. A new module is a decision about the
install, the binary size and Termux; it needs a row here with the reason.

<!-- guard-deps:begin -->
| Module | Why it is here |
|---|---|
| `github.com/charmbracelet/x/ansi` | ANSI parsing and width of text |
| `github.com/charmbracelet/x/term` | raw mode of the terminal |
| `golang.org/x/sys` | console modes on Windows, ioctls elsewhere |
| `github.com/clipperhouse/displaywidth` | indirect: cell width of a rune |
| `github.com/clipperhouse/uax29/v2` | indirect: text segmentation |
| `github.com/lucasb-eyer/go-colorful` | indirect: colour maths |
| `github.com/mattn/go-runewidth` | indirect: cell width of a rune |
| `github.com/michiTrader/arxi` | indirect: the core, as a module of this repository |
<!-- guard-deps:end -->

`core/go.mod` has **no** third-party modules and must stay that way.

## 2. No C

Every release artifact is built with `CGO_ENABLED=0` (`scripts/build-release.sh`), which is what
makes it one static file that runs on Termux without anything else installed. A single
`import "C"` anywhere ends that. The test refuses it, and also builds the Termux target
(`android/arm64`) the way the release does.

## 3. Size of the Termux binary

Measured on 2026-10-20, Go 1.25.3, `CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath
-ldflags "-s -w" ./cmd/arxi-tui`: **9503073 bytes** (linux/amd64 9363640, windows/amd64 9693184).

The budget is a proposal of about 16 % over that. The owner sets the real number.

<!-- guard-size:begin -->
| Target | Measured (bytes) | Budget (bytes) |
|---|---|---|
| android/arm64 | 9503073 | 11000000 |
<!-- guard-size:end -->

Going over is not forbidden, it is a decision: raise the budget here and say what the bytes buy.

## 4. The factory interface does not move

The default scene and its goldens are pinned by hash. `default goldens never move` (AGENTS.md);
this makes a regenerated golden (`UPDATE_GOLDEN=1`) or an edit of the built-in scene a visible
two-file change in review instead of a quiet diff.

<!-- guard-pins:begin -->
| File | sha256 |
|---|---|
| `internal/defaultscene/SOBRIA.json` | `252abddca057216c325af8e148849305f0a1bc5e0233891b5dec032375fe58bc` |
| `testdata/SOBRIA.json` | `252abddca057216c325af8e148849305f0a1bc5e0233891b5dec032375fe58bc` |
| `testdata/SOBRIA.frame` | `4acc3b94b4a015f94e812b1f6744d02bfad8b8f6e531169b97b478610ad64a64` |
| `testdata/SOBRIA.styled` | `45f79a04346baf49aaea9b6a6b5945aaa9d2748e0b258189ac560451094eb102` |
| `testdata/RAW.frame` | `4b4115dcd78f23b2d1fe49d90f6609fec3cfee965c3218d9a112a3a62078777a` |
| `testdata/RAW.json` | `232c6af3f1f87153543ced199e5f94cbf11d81350e12f0f909438f7b9a19fde8` |
| `testdata/RAW.styled` | `c2d8056f2b2c805874bd71a63b4f3603b15d1df20bf62c3d876a7cd1b30cef37` |
<!-- guard-pins:end -->

To change the factory interface on purpose: change the file, regenerate the golden, put the new
hash here, and say in the commit what the owner will see differently.

## What is not guarded yet

- **Start-up time and memory.** They need a baseline measured on the CI runner and on a phone;
  a number invented here would be noise. Do it with the first real measurement.
- **A safe mode flag.** `-raw` boots the factory raw scene today and double Ctrl-C is
  immovable. A `--safe` that also ignores the user's profile belongs to the design, not here.
