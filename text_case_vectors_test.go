package csvx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Runs csvx-spec/tests/edit/text-case.json verbatim (operation "change-case").
func TestTextCaseVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "csvx-spec", "tests", "edit", "text-case.json"))
	if err != nil {
		t.Skipf("text-case vector not found (expected a sibling csvx-spec checkout): %v", err)
	}
	var vector struct {
		Cases []struct {
			Input struct {
				Mode         TextCaseMode `json:"mode"`
				Text         string       `json:"text"`
				DeclaredType string       `json:"declaredType"`
				Formula      string       `json:"formula"`
				Header       bool         `json:"header"`
			} `json:"input"`
			Expected string `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil || len(vector.Cases) == 0 {
		t.Fatalf("bad vector: %v", err)
	}
	for _, c := range vector.Cases {
		in := c.Input
		if got := ChangeCase(in.Text, in.Mode, in.DeclaredType, in.Formula, in.Header); got != c.Expected {
			t.Errorf("ChangeCase(%+v) = %q; want %q", in, got, c.Expected)
		}
	}
}
