// Package defaultscene holds the scene the program boots with, inside the binary.
//
// It used to be read from testdata/SOBRIA.json, a path relative to wherever the
// program was started, so it only worked from the repository: opened in any other
// folder the file was missing and the interface silently fell back to the bare raw
// scene. Embedding it makes "the normal interface" independent of the folder.
//
// SOBRIA.json here is a copy of testdata/SOBRIA.json (the goldens and many tests read
// that one); TestEmbeddedSceneMatchesTestdata keeps the two identical.
package defaultscene

import _ "embed"

// JSON is the sobria scene document.
//
//go:embed SOBRIA.json
var JSON []byte

// Name is how the scene is addressed in errors, since it has no path on disk.
const Name = "SOBRIA.json (built in)"
