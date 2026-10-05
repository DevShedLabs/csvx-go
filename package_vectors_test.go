package csvx

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Runs the csvx-spec vectors whose operations are "validate-package" and "round-trip" verbatim
// (csvx-spec/AGENTS.md rule 3.4): tests/parsing, tests/invalid/missing-manifest.json, tests/styles,
// and tests/print/print-settings-preserved.json. Nothing ran these before.

type packageVector struct {
	ID        string          `json:"id"`
	Operation string          `json:"operation"`
	Input     json.RawMessage `json:"input"`
	Expected  json.RawMessage `json:"expected"`
}

func vectorsWithOperation(t *testing.T, operation string) []packageVector {
	t.Helper()
	spec := specDir(t)
	var found []packageVector
	for _, dir := range []string{"parsing", "invalid", "styles", "print"} {
		paths, _ := filepath.Glob(filepath.Join(spec, "tests", dir, "*.json"))
		sort.Strings(paths)
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var vector packageVector
			if err := json.Unmarshal(raw, &vector); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			if vector.Operation == operation {
				found = append(found, vector)
			}
		}
	}
	return found
}

func TestValidatePackageVectors(t *testing.T) {
	vectors := vectorsWithOperation(t, "validate-package")
	if len(vectors) == 0 {
		t.Fatal("no validate-package vectors found")
	}
	for _, vector := range vectors {
		var expected struct {
			Valid  bool                    `json:"valid"`
			Errors []struct{ Code string } `json:"errors"`
		}
		if err := json.Unmarshal(vector.Expected, &expected); err != nil {
			t.Fatal(err)
		}
		var result ValidationResult
		var path string
		if json.Unmarshal(vector.Input, &path) == nil {
			result = Validate(filepath.Join(specDir(t), path))
		} else {
			var input struct {
				Files []string `json:"files"`
			}
			if err := json.Unmarshal(vector.Input, &input); err != nil {
				t.Fatal(err)
			}
			// A package containing only the listed (empty) files.
			var buffer bytes.Buffer
			zw := zip.NewWriter(&buffer)
			for _, name := range input.Files {
				_, _ = zw.Create(name)
			}
			_ = zw.Close()
			output := filepath.Join(t.TempDir(), "listed.csvx")
			if err := os.WriteFile(output, buffer.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			result = Validate(output)
		}
		if result.Valid != expected.Valid {
			t.Errorf("%s: valid = %v; want %v (%+v)", vector.ID, result.Valid, expected.Valid, result.Errors)
			continue
		}
		var got []struct{ Code string }
		for _, e := range result.Errors {
			got = append(got, struct{ Code string }{e.Code})
		}
		if len(got) != len(expected.Errors) || (len(got) > 0 && !reflect.DeepEqual(got, expected.Errors)) {
			t.Errorf("%s: errors = %v; want %v", vector.ID, got, expected.Errors)
		}
	}
}

func TestRoundTripVectors(t *testing.T) {
	vectors := vectorsWithOperation(t, "round-trip")
	if len(vectors) == 0 {
		t.Fatal("no round-trip vectors found")
	}
	base := func() *Workbook {
		return &Workbook{ID: "book", Version: "1.0", Sheets: []*Sheet{{ID: "sheet-1", Name: "Sheet1", Path: "sheets/sheet-1.csv", Columns: []Column{{ID: "A", Name: "A"}}, Records: [][]string{{"1"}}}}}
	}
	for _, vector := range vectors {
		var input struct {
			Style         json.RawMessage `json:"style"`
			SheetMetadata *struct {
				Name       string                  `json:"name"`
				Columns    []Column                `json:"columns"`
				RowHeights map[int]float64         `json:"rowHeights"`
				Print      *PrintSettings          `json:"print"`
				Cells      map[string]CellMetadata `json:"cells"`
			} `json:"sheetMetadata"`
		}
		if err := json.Unmarshal(vector.Input, &input); err != nil {
			t.Fatal(err)
		}
		var expected map[string]any
		if err := json.Unmarshal(vector.Expected, &expected); err != nil {
			t.Fatal(err)
		}
		workbook := base()
		if input.Style != nil {
			var style Style
			if err := json.Unmarshal(input.Style, &style); err != nil {
				t.Fatalf("%s: %v", vector.ID, err)
			}
			workbook.Styles = []Style{style}
		} else {
			meta := input.SheetMetadata
			sheet := workbook.Sheets[0]
			sheet.Name, sheet.Print, sheet.Cells, sheet.RowHeights = meta.Name, meta.Print, meta.Cells, meta.RowHeights
			if len(meta.Columns) > 0 {
				sheet.Columns = meta.Columns
				sheet.Records = [][]string{make([]string, len(meta.Columns))}
			}
		}
		output := filepath.Join(t.TempDir(), "rt.csvx")
		if err := WritePackage(workbook, output); err != nil {
			t.Fatalf("%s: %v", vector.ID, err)
		}
		loaded, err := Open(output)
		if err != nil {
			t.Fatalf("%s: written package does not load: %v", vector.ID, err)
		}
		if input.Style != nil {
			if got := normalize(t, loaded.Styles[0]); !reflect.DeepEqual(got, expected["style"]) {
				t.Errorf("%s: style = %v; want %v", vector.ID, got, expected["style"])
			}
			continue
		}
		first := loaded.Sheets[0]
		actual := map[string]any{"id": first.ID, "name": first.Name, "columns": first.Columns, "rowHeights": first.RowHeights}
		if first.Print != nil {
			actual["print"] = first.Print
		}
		for key, want := range expected["sheetMetadata"].(map[string]any) {
			if got := normalize(t, actual[key]); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: %s = %v; want %v", vector.ID, key, got, want)
			}
		}
	}
}
