package scene

import (
	"sort"
	"testing"
)

// TestRowSchemasExportIsTheValidatorsInventory holds the exported inventory to
// the map the validator actually consults. It is the drift guard SignedBinds
// has: the prompt is built from this export, so a copy that fell behind the
// validator would tell the model a signed field does not exist.
func TestRowSchemasExportIsTheValidatorsInventory(t *testing.T) {
	got := RowSchemas()
	if len(got) != len(rowSchemas) {
		t.Fatalf("RowSchemas() has %d list binds, the validator signs %d\nconsequence: the prompt omits a list's row vocabulary.\nremedy: build the export from rowSchemas, not a copy.", len(got), len(rowSchemas))
	}
	for bind, fields := range rowSchemas {
		names := got[bind]
		if len(names) != len(fields) {
			t.Errorf("%s: export has %d fields, validator signs %d", bind, len(names), len(fields))
		}
		if !sort.StringsAreSorted(names) {
			t.Errorf("%s: fields are not sorted: %v\nconsequence: the prompt would vary between runs.", bind, names)
		}
		for _, n := range names {
			if !fields[n] {
				t.Errorf("%s: export names %q, which the validator does not sign", bind, n)
			}
		}
	}

	// The caller owns the result: mutating it must not edit the validator's map.
	got["team.members"][0] = "row.corrupted"
	delete(got, "agent.todos")
	if !rowSchemas["team.members"]["row.id"] || rowSchemas["agent.todos"] == nil {
		t.Fatal("mutating RowSchemas() result changed the validator's inventory\nconsequence: a caller could widen what the validator accepts.\nremedy: return fresh slices and a fresh map.")
	}
}
