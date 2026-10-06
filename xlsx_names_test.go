package csvx

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

// Runs csvx-spec/tests/interop/xlsx-paper-size.json (operation "xlsx-to-csvx"): a paper size with no
// CSVX equivalent is kept as xlsxPaperSize rather than mapped or dropped (spec 14.6).
func TestXLSXImportKeepsUnmappedPaperSize(t *testing.T) {
	spec := specDir(t)
	raw, err := os.ReadFile(filepath.Join(spec, "tests", "interop", "xlsx-paper-size.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Input    string `json:"input"`
		Expected struct {
			Samples []map[string]any `json:"samples"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectXLSX(filepath.Join(spec, "tests", "interop", vector.Input))
	if err != nil {
		t.Fatal(err)
	}
	workbook, err := importXLSXWorkbook(filepath.Join(spec, "tests", "interop", vector.Input), inspection)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range vector.Expected.Samples {
		sheet := findSheetByName(workbook, sample["sheet"].(string))
		for key, want := range sample {
			if key == "sheet" {
				continue
			}
			got, _ := walk(map[string]any{"print": printMap(sheet)}, key)
			if !reflect.DeepEqual(normalize(t, got), want) {
				t.Errorf("%v %s = %v; want %v", sample["sheet"], key, normalize(t, got), want)
			}
		}
	}
}

// A corrupted embedded XLSX source must never be offered for exact recovery (spec 14.1: a reader
// MUST verify the hash first).
func TestExportRefusesACorruptedEmbeddedSource(t *testing.T) {
	spec := specDir(t)
	packaged := filepath.Join(t.TempDir(), "book.csvx")
	if err := Convert(filepath.Join(spec, "examples", "example.xlsx"), packaged); err != nil {
		t.Fatal(err)
	}
	workbook, err := Open(packaged)
	if err != nil {
		t.Fatal(err)
	}
	workbook.SourceBytes[len(workbook.SourceBytes)/2] ^= 0xFF // one flipped byte; the recorded hash is unchanged
	corrupted := filepath.Join(t.TempDir(), "corrupted.csvx")
	if err := WritePackage(workbook, corrupted); err != nil {
		t.Fatal(err)
	}
	err = Convert(corrupted, filepath.Join(t.TempDir(), "out.xlsx"))
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("export of a corrupted source = %v; want a SHA-256 verification failure", err)
	}
}

// Runs csvx-spec/tests/interop/xlsx-cross-sheet.json (operation "xlsx-to-csvx", spec 14.11) against
// examples/cross-sheet.xlsx: qualifiers are written as spec 06 requires, a three-dimensional
// reference and a reference into another workbook are dropped with a warning and keep their cached
// result, and the written package validates against the schemas.
func TestXLSXImportCrossSheetVector(t *testing.T) {
	spec := specDir(t)
	raw, err := os.ReadFile(filepath.Join(spec, "tests", "interop", "xlsx-cross-sheet.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Input    string `json:"input"`
		Expected struct {
			SchemaValid bool `json:"schemaValid"`
			Formulas    []struct{ Sheet, Cell, Formula string }
			Values      []struct {
				Sheet, Cell, Type string
				Value             any
			}
			Warnings []struct{ Feature, Path string }
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "cross.csvx")
	if err := Convert(filepath.Join(spec, "tests", "interop", vector.Input), output); err != nil {
		t.Fatal(err)
	}
	workbook, err := Open(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range vector.Expected.Formulas {
		if got := findSheetByName(workbook, want.Sheet).Cells[want.Cell].Formula; got != want.Formula {
			t.Errorf("%s!%s formula = %q; want %q", want.Sheet, want.Cell, got, want.Formula)
		}
	}
	for _, want := range vector.Expected.Values {
		sheet := findSheetByName(workbook, want.Sheet)
		if formula := sheet.Cells[want.Cell].Formula; formula != "" {
			t.Errorf("%s!%s kept formula %q; want none", want.Sheet, want.Cell, formula)
		}
		got := normalize(t, BuildCellMap(sheet, workbook.Styles)[want.Cell].Value)
		if !reflect.DeepEqual(got, map[string]any{"type": want.Type, "value": want.Value}) {
			t.Errorf("%s!%s = %v; want %s %v", want.Sheet, want.Cell, got, want.Type, want.Value)
		}
	}
	var warnings []struct{ Feature, Path string }
	for _, w := range workbook.Source.Warnings {
		if w.Feature == "formula" {
			warnings = append(warnings, struct{ Feature, Path string }{w.Feature, w.Path})
		}
	}
	if !reflect.DeepEqual(warnings, vector.Expected.Warnings) {
		t.Errorf("warnings = %v; want %v", warnings, vector.Expected.Warnings)
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
