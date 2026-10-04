package csvx

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// Runs csvx-spec/tests/interop/xlsx-named-ranges.json (operation "xlsx-to-csvx") against the real
// fixture examples/named-ranges.xlsx: the imported names and the warnings for names that are not
// imported, then validates the written package with csvx-spec/validator.
func TestXLSXImportDefinedNamesVector(t *testing.T) {
	spec := specDir(t)
	raw, err := os.ReadFile(filepath.Join(spec, "tests", "interop", "xlsx-named-ranges.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Input    string `json:"input"`
		Expected struct {
			SchemaValid bool `json:"schemaValid"`
			NamedRanges []struct{ Name, RefersTo string }
			Warnings    []struct{ Feature, Name string }
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "named.csvx")
	if err := Convert(filepath.Join(spec, "tests", "interop", vector.Input), output); err != nil {
		t.Fatal(err)
	}
	workbook, err := Open(output)
	if err != nil {
		t.Fatal(err)
	}
	var got []struct{ Name, RefersTo string }
	for _, n := range workbook.NamedRanges {
		got = append(got, struct{ Name, RefersTo string }{n.Name, n.RefersTo})
	}
	if !reflect.DeepEqual(got, vector.Expected.NamedRanges) {
		t.Errorf("namedRanges = %v; want %v", got, vector.Expected.NamedRanges)
	}
	var warnings []struct{ Feature, Name string }
	for _, w := range workbook.Source.Warnings {
		if w.Feature == "definedName" {
			warnings = append(warnings, struct{ Feature, Name string }{w.Feature, w.Path})
		}
	}
	if !reflect.DeepEqual(warnings, vector.Expected.Warnings) {
		t.Errorf("warnings = %v; want %v", warnings, vector.Expected.Warnings)
	}
	// The existing print names on the fixture still map to print settings, not to names.
	if workbook.Sheets[0].Print == nil || workbook.Sheets[0].Print.Area == nil || *workbook.Sheets[0].Print.Area != "A1:H20" {
		t.Errorf("print area lost: %+v", workbook.Sheets[0].Print)
	}
	if vector.Expected.SchemaValid {
		node, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node not available; cannot run csvx-spec/validator")
		}
		if out, err := exec.Command(node, filepath.Join(spec, "validator", "bin", "csvx-validate.mjs"), output).CombinedOutput(); err != nil {
			t.Fatalf("validator rejected package: %v\n%s", err, out)
		}
	}
}

// A package that declares an invalid name is rejected on load with INVALID_NAMED_RANGE.
func TestOpenRejectsInvalidNamedRanges(t *testing.T) {
	workbook := &Workbook{ID: "book", Version: "1.0", Sheets: []*Sheet{{ID: "sheet-1", Name: "Sheet1", Path: "sheets/sheet-1.csv", Columns: []Column{{ID: "A", Name: "A"}}, Records: [][]string{}}},
		NamedRanges: []NamedRange{{Name: "A1", RefersTo: "=Sheet1!$A$1"}}}
	output := filepath.Join(t.TempDir(), "bad.csvx")
	if err := WritePackage(workbook, output); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(output); err == nil {
		t.Fatal("expected Open to reject a cell-like name")
	}
	if result := Validate(output); result.Valid || result.Errors[0].Code != "INVALID_NAMED_RANGE" {
		t.Fatalf("Validate = %+v; want INVALID_NAMED_RANGE", result)
	}
}
