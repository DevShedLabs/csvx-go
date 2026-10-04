package csvx

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"
)

// Runners for csvx-spec/tests/formulas, tests/calculations and the parse-formula vectors in
// tests/invalid, consuming the vectors verbatim (csvx-spec/AGENTS.md rule 3.4).

func specDir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{"..", "csvx-spec"}, parts...)...)
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("csvx-spec not found (expected a sibling checkout): %v", err)
	}
	return dir
}

func readVectors(t *testing.T, dir string) map[string]map[string]any {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(paths)
	out := map[string]map[string]any{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var vector map[string]any
		if err := json.Unmarshal(raw, &vector); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out[filepath.Base(path)] = vector
	}
	return out
}

// normalize round-trips a value through JSON so typed and decoded forms compare equal.
func normalize(t *testing.T, value any) any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func toValue(t *testing.T, raw any) Value {
	t.Helper()
	body, _ := json.Marshal(raw)
	var v Value
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func vectorCases(vector map[string]any) []map[string]any {
	if cases, ok := vector["cases"].([]any); ok {
		var out []map[string]any
		for _, c := range cases {
			out = append(out, c.(map[string]any))
		}
		return out
	}
	return []map[string]any{vector}
}

func TestFormulaVectors(t *testing.T) {
	for file, vector := range readVectors(t, specDir(t, "tests", "formulas")) {
		if vector["operation"] != "evaluate" {
			t.Errorf("%s: unexpected operation %v", file, vector["operation"])
			continue
		}
		for i, c := range vectorCases(vector) {
			input := c["input"].(map[string]any)
			cells, _ := input["cells"].(map[string]any)
			got := EvaluateFormula(input["formula"].(string), func(ref ReferenceRequest) Value {
				if cell, ok := cells[ref.Column+strconv.Itoa(ref.Row+1)]; ok {
					return toValue(t, cell)
				}
				return Value{Type: "blank"}
			})
			if want := c["expected"]; !reflect.DeepEqual(normalize(t, got), want) {
				t.Errorf("%s#%d %s: got %v; want %v", file, i+1, input["formula"], normalize(t, got), want)
			}
		}
	}
}

func TestCalculationVectors(t *testing.T) {
	for file, vector := range readVectors(t, specDir(t, "tests", "calculations")) {
		cells := CellMap{}
		for coordinate, raw := range vector["input"].(map[string]any)["cells"].(map[string]any) {
			cell := raw.(map[string]any)
			if formula, ok := cell["formula"].(string); ok {
				cells[coordinate] = FormulaCellInput{Formula: formula}
			} else if wrapped, ok := cell["value"].(map[string]any); ok {
				// Older vectors wrap the typed value: {"value": {"type": ..., "value": ...}}.
				cells[coordinate] = FormulaCellInput{Value: toValue(t, wrapped)}
			} else {
				cells[coordinate] = FormulaCellInput{Value: toValue(t, cell)}
			}
		}
		results := RecalculateCells(cells, RecalculateOptions{})
		for coordinate, want := range vector["expected"].(map[string]any) {
			if got := normalize(t, results[coordinate]); !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s: got %v; want %v", file, coordinate, got, want)
			}
		}
	}
}

func TestInvalidParseFormulaVectors(t *testing.T) {
	for file, vector := range readVectors(t, specDir(t, "tests", "invalid")) {
		if vector["operation"] != "parse-formula" {
			continue
		}
		formula := vector["input"].(string)
		expected := vector["expected"].(map[string]any)
		if errs, ok := expected["errors"].([]any); ok && len(errs) > 0 && errs[0].(map[string]any)["code"] == "NAME" {
			if got := EvaluateFormula(formula, func(ReferenceRequest) Value { return Value{Type: "blank"} }); got.Type != "error" || got.Code != "NAME" {
				t.Errorf("%s: %s should evaluate to NAME, got %+v", file, formula, got)
			}
			continue
		}
		var parseErr *FormulaParseError
		if _, err := ParseFormula(formula); !errors.As(err, &parseErr) {
			t.Errorf("%s: %s should fail to parse", file, formula)
		}
	}
}
