package csvx

import (
	"os/exec"
	"path/filepath"
	"testing"
)

const exampleXLSX = "../csvx-spec/examples/example.xlsx"

// Worksheet row N is CSVX row N (spec/03-sheets.md, 14.7): row 1 is the CSV header and its cell
// text names the columns; rows 2+ are the records.
func TestXLSXImportRowOneIsTheHeader(t *testing.T) {
	inspection, err := InspectXLSX(exampleXLSX)
	if err != nil {
		t.Fatal(err)
	}
	workbook, err := importXLSXWorkbook(exampleXLSX, inspection)
	if err != nil {
		t.Fatal(err)
	}
	sheet := workbook.Sheets[0]

	if got, want := sheet.Columns[0].Name, "Average salary for a Senior Software Engineer"; got != want {
		t.Errorf("column A name = %q, want %q (XLSX A1 text)", got, want)
	}
	if got := sheet.Columns[1].Name; got != "" {
		t.Errorf("column B name = %q, want the empty name for an empty header cell (spec 14.7)", got)
	}
	if got := sheet.Columns[2].Name; got != "Minimum" {
		t.Errorf("column C name = %q, want %q (XLSX C1 text)", got, "Minimum")
	}
	if got, want := sheet.Records[0][0], "40 hours a week"; got != want {
		t.Errorf("first record column A = %q, want %q (XLSX A2)", got, want)
	}
	// XLSX D2 is CSVX D2: records[0] is row 2, so its column D value is the cell at coordinate D2.
	if got := sheet.Records[0][3]; got != "170000.0" {
		t.Errorf("records[0][D] = %q, want D2's value", got)
	}
	// Metadata keeps its worksheet coordinates: C11's formula is still keyed C11.
	if got := sheet.Cells["C11"].Formula; got != "=SUM(D11/B11)" {
		t.Errorf("C11 formula = %q", got)
	}
	if got := len(sheet.Records) + 1; got != 1002 {
		t.Errorf("rows (header + records) = %d, want the worksheet's 1002", got)
	}
}

func TestXLSXImportHeaderRowPackageIsSchemaValid(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; cannot run csvx-spec/validator")
	}
	inspection, err := InspectXLSX(exampleXLSX)
	if err != nil {
		t.Fatal(err)
	}
	workbook, err := importXLSXWorkbook(exampleXLSX, inspection)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "example.csvx")
	if err := WritePackage(workbook, output); err != nil {
		t.Fatal(err)
	}
	if result, err := exec.Command(node, "../csvx-spec/validator/bin/csvx-validate.mjs", output).CombinedOutput(); err != nil {
		t.Fatalf("validator rejected package: %v\n%s", err, result)
	}
}
