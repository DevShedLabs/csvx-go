package csvx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Runs csvx-spec/tests/export-csv/*.json verbatim (csvx-spec/AGENTS.md rule 3.4): operations
// "csvx-to-csv", "csv-round-trip" and "csv-via-xlsx". Expected CSV is compared byte for byte.

type exportOptionsJSON struct {
	Sheet     string   `json:"sheet"`
	Header    *bool    `json:"header"`
	Delimiter string   `json:"delimiter"`
	Formulas  string   `json:"formulas"`
	Display   bool     `json:"display"`
	_         struct{} // keep the struct unkeyed-literal safe
}

func (o exportOptionsJSON) options() CSVExportOptions {
	options := CSVExportOptions{Sheet: o.Sheet, Formulas: o.Formulas, Display: o.Display}
	if o.Header != nil && !*o.Header {
		options.NoHeader = true
	}
	if o.Delimiter != "" {
		options.Delimiter = []rune(o.Delimiter)[0]
	}
	return options
}

func readExportVectors(t *testing.T, file string) []json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(specDir(t, "tests", "export-csv"), file))
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Cases []json.RawMessage `json:"cases"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil || len(vector.Cases) == 0 {
		t.Fatalf("%s: bad vector: %v", file, err)
	}
	return vector.Cases
}

func exportWarnings(w []ExportWarning) []struct{ Feature, Location string } {
	var out []struct{ Feature, Location string }
	for _, x := range w {
		out = append(out, struct{ Feature, Location string }{x.Feature, x.Location})
	}
	return out
}

func TestCSVExportVectors(t *testing.T) {
	spec := specDir(t)
	for _, raw := range readExportVectors(t, "export.json") {
		var c struct {
			Note  string `json:"note"`
			Input struct {
				Package  string          `json:"package"`
				Workbook json.RawMessage `json:"workbook"`
				Options  exportOptionsJSON
			} `json:"input"`
			Expected struct {
				CSV      string                               `json:"csv"`
				Warnings []struct{ Feature, Location string } `json:"warnings"`
				Error    bool                                 `json:"error"`
			} `json:"expected"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		var workbook *Workbook
		var err error
		if c.Input.Package != "" {
			workbook, err = OpenDirectory(filepath.Join(spec, c.Input.Package))
		} else {
			workbook = &Workbook{ID: "book", Version: "1.0"}
			err = json.Unmarshal(c.Input.Workbook, workbook)
			for _, sheet := range workbook.Sheets {
				sheet.Path = "sheets/" + sheet.ID + ".csv"
			}
		}
		if err != nil {
			t.Fatalf("%s: %v", c.Note, err)
		}
		// The Go option is a single rune, so a multi-character delimiter is rejected before the call.
		if d := c.Input.Options.Delimiter; len([]rune(d)) > 1 {
			if !c.Expected.Error {
				t.Errorf("%s: unexpected multi-character delimiter", c.Note)
			}
			continue
		}
		got, warnings, err := ExportCSV(workbook, c.Input.Options.options())
		if c.Expected.Error {
			if err == nil {
				t.Errorf("%s: expected an error", c.Note)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.Note, err)
			continue
		}
		if got != c.Expected.CSV {
			t.Errorf("%s:\n got %q\nwant %q", c.Note, got, c.Expected.CSV)
		}
		if gotW := exportWarnings(warnings); !reflect.DeepEqual(gotW, c.Expected.Warnings) && !(len(gotW) == 0 && len(c.Expected.Warnings) == 0) {
			t.Errorf("%s: warnings = %v; want %v", c.Note, gotW, c.Expected.Warnings)
		}
	}
}

func TestCSVRoundTripVectors(t *testing.T) {
	spec := specDir(t)
	for _, raw := range readExportVectors(t, "round-trip.json") {
		var c struct {
			Note  string `json:"note"`
			Input struct {
				File          string `json:"file"`
				ImportOptions struct {
					Header    *bool  `json:"header"`
					Infer     bool   `json:"infer"`
					Delimiter string `json:"delimiter"`
					Name      string `json:"name"`
				} `json:"importOptions"`
			} `json:"input"`
			Expected struct {
				CSV string `json:"csv"`
			} `json:"expected"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		o := CSVImportOptions{Infer: c.Input.ImportOptions.Infer, Name: c.Input.ImportOptions.Name}
		if h := c.Input.ImportOptions.Header; h != nil && !*h {
			o.NoHeader = true
		}
		if d := c.Input.ImportOptions.Delimiter; d != "" {
			o.Delimiter = []rune(d)[0]
		}
		workbook, _, err := ImportCSVFile(filepath.Join(spec, c.Input.File), o)
		if err != nil {
			t.Fatalf("%s: %v", c.Note, err)
		}
		got, _, err := ExportCSV(workbook, CSVExportOptions{})
		if err != nil {
			t.Fatalf("%s: %v", c.Note, err)
		}
		if got != c.Expected.CSV {
			t.Errorf("%s:\n got %q\nwant %q", c.Note, got, c.Expected.CSV)
		}
	}
}

// Exporting a fixture to CSV directly, and via XLSX (export to XLSX, import it back, export to CSV),
// must give the same bytes: the check for corruption across conversions.
func TestCSVViaXLSXVectors(t *testing.T) {
	spec := specDir(t)
	for _, raw := range readExportVectors(t, "via-xlsx.json") {
		var c struct {
			Note  string `json:"note"`
			Input struct {
				Package string `json:"package"`
				Options exportOptionsJSON
			} `json:"input"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		workbook, err := OpenDirectory(filepath.Join(spec, c.Input.Package))
		if err != nil {
			t.Fatalf("%s: %v", c.Note, err)
		}
		options := c.Input.Options.options()
		direct, _, err := ExportCSV(workbook, options)
		if err != nil {
			t.Fatalf("%s: %v", c.Note, err)
		}
		xlsx := filepath.Join(t.TempDir(), "via.xlsx")
		if _, err := ExportXLSX(workbook, xlsx); err != nil {
			t.Fatalf("%s: %v", c.Note, err)
		}
		inspection, err := InspectXLSX(xlsx)
		if err != nil {
			t.Fatal(err)
		}
		back, err := importXLSXWorkbook(xlsx, inspection)
		if err != nil {
			t.Fatalf("%s: %v", c.Note, err)
		}
		// The XLSX has its own sheet ids; select the same sheet by name.
		if options.Sheet != "" {
			for _, s := range workbook.Sheets {
				if s.ID == options.Sheet || s.Name == options.Sheet {
					options.Sheet = s.Name
				}
			}
		}
		via, _, err := ExportCSV(back, options)
		if err != nil {
			t.Fatalf("%s: %v", c.Note, err)
		}
		if via != direct {
			t.Errorf("%s: CSV changed across CSVX -> XLSX -> CSVX:\n direct %q\n via    %q", c.Note, direct, via)
		}
	}
}
