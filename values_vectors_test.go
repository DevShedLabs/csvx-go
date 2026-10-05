package csvx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Runs csvx-spec/tests/values/resolve.json verbatim (operation "resolve-cell-value") — csvx-spec/
// AGENTS.md rule 3.4: how a CSV field resolves to a typed value.
func TestResolveCellValueVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(specDir(t, "tests", "values"), "resolve.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Cases []struct {
			Note  string `json:"note"`
			Input struct {
				Raw          string `json:"raw"`
				DeclaredType string `json:"declaredType"`
				NumberFormat string `json:"numberFormat"`
			} `json:"input"`
			Expected any `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil || len(vector.Cases) == 0 {
		t.Fatalf("bad vector: %v", err)
	}
	for _, c := range vector.Cases {
		got := normalize(t, ResolveCellValue(c.Input.Raw, c.Input.DeclaredType, c.Input.NumberFormat))
		if !reflect.DeepEqual(got, c.Expected) {
			t.Errorf("%s: got %v; want %v", c.Note, got, c.Expected)
		}
	}
}
