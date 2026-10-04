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

// Runs csvx-spec/tests/interop/csvx-to-xlsx.json (operation "csvx-to-xlsx"): each case exports a
// real fixture (optionally after edits and added names), imports the XLSX again, and compares
// cells, styles, print settings, names, sheet names and export warnings — then validates the
// re-imported package with csvx-spec/validator (csvx-spec/AGENTS.md rules 3.2, 3.7, 4.5).

func walk(value any, path string) (any, bool) {
	for _, part := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		if value, ok = object[part]; !ok {
			return nil, false
		}
	}
	return value, true
}

func findSheetByName(workbook *Workbook, name string) *Sheet {
	for _, sheet := range workbook.Sheets {
		if sheet.Name == name {
			return sheet
		}
	}
	return nil
}

func TestCSVXToXLSXVectors(t *testing.T) {
	spec := specDir(t)
	raw, err := os.ReadFile(filepath.Join(spec, "tests", "interop", "csvx-to-xlsx.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Cases []struct {
			Note        string `json:"note"`
			Input       string `json:"input"`
			NamedRanges []NamedRange
			Edits       []struct {
				Operation string         `json:"operation"`
				Args      map[string]any `json:"args"`
			} `json:"edits"`
			Expected struct {
				SchemaValid bool                             `json:"schemaValid"`
				Samples     []map[string]any                 `json:"samples"`
				NamedRanges []NamedRange                     `json:"namedRanges"`
				SheetNames  []string                         `json:"sheetNames"`
				Warnings    []struct{ Feature, Name string } `json:"warnings"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	for i, c := range vector.Cases {
		name := c.Input + " #" + string(rune('1'+i)) + ": " + c.Note
		workbook, err := OpenDirectory(filepath.Join(spec, "tests", "interop", c.Input))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.NamedRanges != nil {
			workbook.NamedRanges = c.NamedRanges
		}
		for _, edit := range c.Edits {
			if workbook, err = runEditOperation(edit.Operation, workbook, edit.Args); err != nil {
				t.Fatalf("%s: %s: %v", name, edit.Operation, err)
			}
		}
		xlsx := filepath.Join(t.TempDir(), "out.xlsx")
		warnings, err := ExportXLSX(workbook, xlsx)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		inspection, err := InspectXLSX(xlsx)
		if err != nil {
			t.Fatalf("%s: exported XLSX does not inspect: %v", name, err)
		}
		back, err := importXLSXWorkbook(xlsx, inspection)
		if err != nil {
			t.Fatalf("%s: exported XLSX does not import: %v", name, err)
		}

		for _, sample := range c.Expected.Samples {
			sheet := findSheetByName(back, sample["sheet"].(string))
			if sheet == nil {
				t.Errorf("%s: no sheet %v after round trip", name, sample["sheet"])
				continue
			}
			for key, want := range sample {
				switch {
				case key == "sheet" || key == "cell":
				case strings.HasPrefix(key, "print"):
					got, _ := walk(map[string]any{"print": printMap(sheet)}, key)
					if !reflect.DeepEqual(normalize(t, got), want) {
						t.Errorf("%s: %v %s = %v; want %v", name, sample["sheet"], key, normalize(t, got), want)
					}
				default:
					coordinate := sample["cell"].(string)
					column, row, _ := IndicesForCoordinate(coordinate)
					metadata := sheet.Cells[coordinate]
					cell := map[string]any{"type": metadata.Type, "value": RawCellText(sheet, row, column), "formula": metadata.Formula}
					if metadata.Cached != nil {
						cell["cached"] = normalize(t, metadata.Cached)
					}
					for _, style := range back.Styles {
						if style.Id == metadata.Style {
							cell["style"] = styleToMap(style)
						}
					}
					got, _ := walk(cell, key)
					if !reflect.DeepEqual(normalize(t, got), want) {
						t.Errorf("%s: %v %s %s = %v; want %v", name, sample["sheet"], coordinate, key, normalize(t, got), want)
					}
				}
			}
		}
		if c.Expected.NamedRanges != nil && !reflect.DeepEqual(normalize(t, back.NamedRanges), normalize(t, c.Expected.NamedRanges)) {
			t.Errorf("%s: namedRanges = %v; want %v", name, back.NamedRanges, c.Expected.NamedRanges)
		}
		if c.Expected.SheetNames != nil {
			var names []string
			for _, s := range back.Sheets {
				names = append(names, s.Name)
			}
			if !reflect.DeepEqual(names, c.Expected.SheetNames) {
				t.Errorf("%s: sheet names = %v; want %v", name, names, c.Expected.SheetNames)
			}
		}
		var got []struct{ Feature, Name string }
		for _, w := range warnings {
			got = append(got, struct{ Feature, Name string }{w.Feature, w.Path})
		}
		if !reflect.DeepEqual(got, c.Expected.Warnings) && !(len(got) == 0 && len(c.Expected.Warnings) == 0) {
			t.Errorf("%s: warnings = %v; want %v", name, got, c.Expected.Warnings)
		}
		if c.Expected.SchemaValid {
			node, err := exec.LookPath("node")
			if err != nil {
				continue
			}
			output := filepath.Join(t.TempDir(), "back.csvx")
			if err := WritePackage(back, output); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(node, filepath.Join(spec, "validator", "bin", "csvx-validate.mjs"), output).CombinedOutput(); err != nil {
				t.Errorf("%s: validator rejected the re-imported package: %v\n%s", name, err, out)
			}
		}
	}
}

func exportedParts(t *testing.T, workbook *Workbook) (map[string]string, []XLSXDiagnostic) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.xlsx")
	warnings, err := ExportXLSX(workbook, path)
	if err != nil {
		t.Fatal(err)
	}
	files, err := readXLSXFiles(path)
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for name, body := range files {
		parts[name] = string(body)
	}
	return parts, warnings
}

// Values, errors, dates, validations, widths and heights land in the worksheet XML as spec 14.9
// says; the round-trip vectors above cannot see some of these because the importer does not read
// them back.
func TestExportXLSXWorksheetDetails(t *testing.T) {
	validation := json.RawMessage(`{"type":"list","formula1":"a,b","allowBlank":true,"message":"pick"}`)
	workbook := &Workbook{ID: "book", Version: "1.0", Sheets: []*Sheet{{
		ID: "sheet-1", Name: "Sheet1", Path: "sheets/sheet-1.csv",
		Columns:    []Column{{ID: "A", Name: "When", Type: "date", Width: 18}, {ID: "B", Name: "Kind"}, {ID: "C", Name: "Err", Type: "error"}},
		Records:    [][]string{{"2026-09-22", "x", "DIV0"}, {"", "y", "CYCLE"}},
		Cells:      map[string]CellMetadata{"B2": {Validation: validation}, "A3": {Cached: &Value{Type: "error", Code: "CYCLE"}, Formula: "=A2"}},
		RowHeights: map[int]float64{2: 30},
	}}}
	parts, warnings := exportedParts(t, workbook)
	sheet := parts["xl/worksheets/sheet1.xml"]
	for _, want := range []string{
		`<v>46287</v>`,         // 2026-09-22 as a serial number
		`t="e"><v>#DIV/0!</v>`, // DIV0 -> #DIV/0!
		`<col min="1" max="1" width="18" customWidth="1"/>`, // width in its own unit
		`ht="30" customHeight="1"`,                          // height in points
		`<dataValidation type="list" allowBlank="1" showErrorMessage="1" error="pick" sqref="B2"><formula1>&#34;a,b&#34;</formula1>`,
		`<f>A2</f>`, // formula without its "="
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("worksheet missing %s\n%s", want, sheet)
		}
	}
	if !strings.Contains(parts["xl/styles.xml"], `formatCode="yyyy-mm-dd"`) {
		t.Errorf("date cells need a date number format:\n%s", parts["xl/styles.xml"])
	}
	cycles := 0
	for _, w := range warnings {
		if w.Feature == "value" && strings.Contains(w.Message, "CYCLE") {
			cycles++
		}
	}
	if cycles != 2 {
		t.Errorf("CYCLE has no XLSX equivalent and must warn each time (cell value and cached value), got %d: %v", cycles, warnings)
	}
}

// An edited package is exported from its CSVX content even though it embeds an original XLSX.
func TestExportOfEditedPackageDoesNotReturnTheStaleOriginal(t *testing.T) {
	spec := specDir(t)
	source := filepath.Join(spec, "examples", "example.xlsx")
	packaged := filepath.Join(t.TempDir(), "book.csvx")
	if err := Convert(source, packaged); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	// Unmodified: exact recovery.
	recovered := filepath.Join(t.TempDir(), "recovered.xlsx")
	if err := Convert(packaged, recovered); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(recovered); !reflect.DeepEqual(body, original) {
		t.Fatal("unmodified package must recover the original bytes")
	}
	// Edited: authority becomes csvx, so the export reflects the edit.
	workbook, err := Open(packaged)
	if err != nil {
		t.Fatal(err)
	}
	if workbook, err = SetCell(workbook, workbook.Sheets[0].ID, "A1", "Edited title", EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if workbook.Source.Authority != "csvx" {
		t.Fatalf("authority = %q; want csvx", workbook.Source.Authority)
	}
	editedPackage := filepath.Join(t.TempDir(), "edited.csvx")
	if err := WritePackage(workbook, editedPackage); err != nil {
		t.Fatal(err)
	}
	exported := filepath.Join(t.TempDir(), "edited.xlsx")
	if err := Convert(editedPackage, exported); err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectXLSX(exported)
	if err != nil {
		t.Fatal(err)
	}
	back, err := importXLSXWorkbook(exported, inspection)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Sheets[0].Columns[0].Name; got != "Edited title" {
		t.Fatalf("exported header = %q; want the edit", got)
	}
}

func TestImportDateTimeAndErrorConversions(t *testing.T) {
	for format, want := range map[string]string{
		"yyyy-mm-dd": "date", "m/d/yy": "date", "hh:mm:ss": "time", "h:mm AM/PM": "time",
		"yyyy-mm-dd hh:mm:ss": "datetime", `"Sales" 0.00`: "", `[$-409]0.00`: "", "0.00": "", "General": "",
	} {
		if got := dateTimeKind(format); got != want {
			t.Errorf("dateTimeKind(%q) = %q; want %q", format, got, want)
		}
	}
	for _, c := range []struct {
		kind, serial string
		date1904     bool
		want         string
		ok           bool
	}{
		{"date", "46287", false, "2026-09-22", true},
		{"time", "0.604166666667", false, "14:30:00", true},
		{"datetime", "46287.5", false, "2026-09-22T12:00:00", true},
		{"date", "43465", true, "2023-01-01", true}, // the 1904 system counts from 1904-01-01
		{"date", "46287.5", false, "", false},       // a fraction is not a date
		{"time", "1.5", false, "", false},           // a day or more is not a time of day
		{"date", "-1", false, "", false},
	} {
		got, ok := isoFromSerial(c.kind, c.serial, c.date1904)
		if got != c.want || ok != c.ok {
			t.Errorf("isoFromSerial(%s, %s, %v) = %q, %v; want %q, %v", c.kind, c.serial, c.date1904, got, ok, c.want, c.ok)
		}
	}
	for literal, code := range map[string]string{"#DIV/0!": "DIV0", "#N/A": "N/A", "#NAME?": "NAME", "#GETTING_DATA": "GETTING_DATA"} {
		if got := csvxErrorCode(literal); got != code {
			t.Errorf("csvxErrorCode(%q) = %q; want %q", literal, got, code)
		}
	}
}
