package keymap

import _ "embed"

// FactoryJSON is the keymap the program starts with (see docs/KEYS.md).
//
//go:embed factory.json
var FactoryJSON []byte
