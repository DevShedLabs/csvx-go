package csvx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Runner for csvx-spec/tests/import-csv/*.json (operation "csv-to-csvx"). It consumes the vectors
// verbatim and validates the written package with csvx-spec/validator, per AGENTS.md rules 3.2/3.4.

type csvImportVector struct {
	ID    string `json:"id"`
	Input struct {
		File    string `json:"file"`
		Options struct {
			Delimiter string `json:"delimiter"`
			Header    *bool  `json:"header"`
			Infer     bool   `json:"infer"`
			Name      string `json:"name"`
		} `json:"options"`
	} `json:"input"`
	Expected struct {
		Error       bool                       `json:"error"`
		Line        int                        `json:"line"`
		SchemaValid bool                       `json:"schemaValid"`
		Sheet       *struct{ ID, Name string } `json:"sheet"`
		Columns     []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"columns"`
		CSV   *string `json:"csv"`
		Cells []struct {
			Cell  string `json:"cell"`
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"cells"`
		NoFormulas bool `json:"noFormulas"`
		Warnings   *[]struct {
			Location string `json:"location"`
		} `json:"warnings"`
	} `json:"expected"`
}

func TestCSVImportVectors(t *testing.T) {
	spec := filepath.Join("..", "csvx-spec")
	paths, _ := filepath.Glob(filepath.Join(spec, "tests", "import-csv", "*.json"))
	if len(paths) == 0 {
		t.Skip("no csvx-spec import-csv vectors found (expected a sibling csvx-spec checkout)")
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var v csvImportVector
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		t.Run(v.ID, func(t *testing.T) { runCSVImportVector(t, spec, v) })
	}
}

func runCSVImportVector(t *testing.T, spec string, v csvImportVector) {
	options := CSVImportOptions{Infer: v.Input.Options.Infer, Name: v.Input.Options.Name}
	if v.Input.Options.Header != nil && !*v.Input.Options.Header {
		options.NoHeader = true
	}
	if d := v.Input.Options.Delimiter; d != "" {
		options.Delimiter = []rune(d)[0]
	}
	workbook, warnings, err := ImportCSVFile(filepath.Join(spec, v.Input.File), options)
	if v.Expected.Error {
		if err == nil {
			t.Fatal("import succeeded; expected an error")
		}
		var syntax *CSVSyntaxError
		if v.Expected.Line != 0 && (!errors.As(err, &syntax) || syntax.Line != v.Expected.Line) {
			t.Fatalf("error = %v; want CSV syntax error at line %d", err, v.Expected.Line)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	sheet := workbook.Sheets[0]
	e := v.Expected
	if e.Sheet != nil && (sheet.ID != e.Sheet.ID || sheet.Name != e.Sheet.Name) {
		t.Errorf("sheet = {%s %s}; want %+v", sheet.ID, sheet.Name, *e.Sheet)
	}
	if e.Columns != nil {
		var got, want []string
		for _, c := range sheet.Columns {
			got = append(got, c.ID+"|"+c.Name+"|"+c.Type)
		}
		for _, c := range e.Columns {
			want = append(want, c.ID+"|"+c.Name+"|"+c.Type)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("columns = %v; want %v", got, want)
		}
	}
	output := filepath.Join(t.TempDir(), "out.csvx")
	if err := WritePackage(workbook, output); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(output)
	if err != nil {
		t.Fatalf("written package does not reload: %v", err)
	}
	if e.CSV != nil {
		var body bytes.Buffer
		if err := writeCSV(&body, loaded.Sheets[0]); err != nil || body.String() != *e.CSV {
			t.Errorf("csv = %q; want %q", body.String(), *e.CSV)
		}
	}
	for _, cell := range e.Cells {
		column, row := parseTestA1(t, cell.Cell)
		raw := ""
		if row >= 1 {
			raw = loaded.Sheets[0].Records[row-1][column]
		}
		got := ResolveCellValue(raw, loaded.Sheets[0].Columns[column].Type, "")
		if got.Type != cell.Type || (cell.Value != "" && fmt.Sprint(got.Value) != cell.Value) {
			t.Errorf("%s resolves to %+v; want %s %q", cell.Cell, got, cell.Type, cell.Value)
		}
	}
	if e.NoFormulas {
		for ref, meta := range loaded.Sheets[0].Cells {
			if meta.Formula != "" {
				t.Errorf("%s gained formula %q", ref, meta.Formula)
			}
		}
	}
	if e.Warnings != nil {
		var got, want []string
		for _, w := range warnings {
			got = append(got, w.Location)
		}
		for _, w := range *e.Warnings {
			want = append(want, w.Location)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("warning locations = %v; want %v", got, want)
		}
	}
	if e.SchemaValid {
		node, err := exec.LookPath("node")
		if err != nil {
			t.Log("node not available; skipping csvx-spec/validator")
			return
		}
		if out, err := exec.Command(node, filepath.Join(spec, "validator", "bin", "csvx-validate.mjs"), output).CombinedOutput(); err != nil {
			t.Fatalf("validator rejected package: %v\n%s", err, out)
		}
	}
}

// parseTestA1 returns a zero-based column and the 1-based row of an A1 ref.
func parseTestA1(t *testing.T, ref string) (int, int) {
	t.Helper()
	i := 0
	column := 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		column = column*26 + int(ref[i]-'A'+1)
		i++
	}
	var row int
	if _, err := fmt.Sscanf(ref[i:], "%d", &row); err != nil || i == 0 {
		t.Fatalf("bad A1 ref %q", ref)
	}
	return column - 1, row - 1 // row 1 is the header; records[row-2] — caller adjusts below
}

func TestLiteralType(t *testing.T) {
	cases := map[string]string{
		"": "blank", "true": "boolean", "TRUE": "string", " true": "string",
		"0": "integer", "-5": "integer", "007": "string", "+5": "string", "-0": "string", "1e3": "string",
		"19.95": "decimal", "0.5": "decimal", "-0.5": "decimal", "00.5": "string", ".5": "string", "5.": "string", "1,000": "string", "$5": "string",
		"2026-09-22": "date", "2026-02-30": "string", "13:45:00": "time", "25:00:00": "string",
		"2026-09-22T12:30:00Z": "datetime", "2026-09-22T12:30:00": "string", "1/2/2026": "string",
	}
	for text, want := range cases {
		if got := literalType(text); got != want {
			t.Errorf("literalType(%q) = %s; want %s", text, got, want)
		}
	}
}

func TestImportCSVRejectsBadInput(t *testing.T) {
	if _, _, err := ImportCSV([]byte("a\n\xff\n"), CSVImportOptions{}); err == nil {
		t.Error("invalid UTF-8 accepted")
	}
	if _, _, err := ImportCSV([]byte("a\n"), CSVImportOptions{Name: "a:b"}); err == nil {
		t.Error("sheet name with ':' accepted")
	}
	wb, _, err := ImportCSV([]byte("\xEF\xBB\xBFa;b\r\n1;2\r\n"), CSVImportOptions{Delimiter: ';'})
	if err != nil || wb.Sheets[0].Columns[0].Name != "a" || strings.Contains(wb.Sheets[0].Columns[1].Name, "\r") {
		t.Errorf("BOM/semicolon/CRLF handling wrong: %v %+v", err, wb)
	}
}
