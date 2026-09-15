package scene

import (
	"encoding/json"
	"fmt"
)

// Document is the root of a scene tree. The document is the UI, living as JSON
// on disk, reloaded on change, edited by agents or users.
type Document struct {
	Root *Node `json:"root"`
}

// ParseDocument parses a JSON scene document from bytes. It returns file:line
// on syntax errors, as required by the contract "every error carries loc."
func ParseDocument(data []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &doc, nil
}
