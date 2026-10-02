package csvx

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

const printFixture = "../csvx-spec/examples/print-settings.xlsx"

func importPrintFixture(t *testing.T) *Workbook {
	t.Helper()
	inspection, err := InspectXLSX(printFixture)
	if err != nil {
		t.Fatal(err)
	}
	workbook, err := importXLSXWorkbook(printFixture, inspection)
	if err != nil {
		t.Fatal(err)
	}
	return workbook
}

func printAsMap(t *testing.T, settings *PrintSettings) map[string]any {
	t.Helper()
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestXLSXImportMapsPrintSettings(t *testing.T) {
	workbook := importPrintFixture(t)

	fit := printAsMap(t, workbook.Sheets[0].Print)
	want := map[string]any{
		"orientation": "landscape", "paperSize": "a4", "pageOrder": "overThenDown",
		"fitToWidth": float64(1), "fitToHeight": float64(0),
		"area": "A1:H20", "repeatRows": "1:2", "repeatColumns": "A:B",
		"gridlines": true, "centerHorizontally": true,
		"margins":      map[string]any{"top": 0.5, "right": 0.4, "bottom": 0.6, "left": 0.3},
		"columnBreaks": []any{float64(4)}, "rowBreaks": []any{float64(10)},
	}
	if !reflect.DeepEqual(fit, want) {
		t.Fatalf("Fit print settings:\n got %v\nwant %v", fit, want)
	}

	scaled := printAsMap(t, workbook.Sheets[1].Print)
	if scaled["scale"] != float64(80) || scaled["paperSize"] != "legal" {
		t.Fatalf("Scaled print settings wrong: %v", scaled)
	}
	if _, ok := scaled["fitToWidth"]; ok {
		t.Fatalf("Scaled must not declare fitToWidth: %v", scaled)
	}

	if workbook.Sheets[2].Print != nil {
		t.Fatalf("Plain sheet states nothing but defaults, got %v", printAsMap(t, workbook.Sheets[2].Print))
	}
}

// Print settings, including properties the schema does not define, survive write → read.
func TestPrintSettingsRoundTripThroughPackage(t *testing.T) {
	workbook := importPrintFixture(t)
	extra := map[string]any{"headerFooter": map[string]any{"oddFooter": "&P of &N"}}
	workbook.Sheets[0].Print.AdditionalProperties = extra

	output := filepath.Join(t.TempDir(), "print.csvx")
	if err := WritePackage(workbook, output); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(output)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := printAsMap(t, reopened.Sheets[0].Print), printAsMap(t, workbook.Sheets[0].Print); !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed print settings:\n got %v\nwant %v", got, want)
	}
	if printAsMap(t, reopened.Sheets[0].Print)["headerFooter"] == nil {
		t.Fatal("unknown print property was dropped")
	}
}

func TestPrintSettingsRejectInvalidValues(t *testing.T) {
	for _, body := range []string{`{"orientation":"sideways"}`, `{"scale":5}`, `{"paperSize":"b5"}`} {
		var settings PrintSettings
		if err := json.Unmarshal([]byte(body), &settings); err == nil {
			t.Errorf("expected %s to be rejected", body)
		}
	}
}

func TestXLSXImportPrintSettingsSchemaValid(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; cannot run csvx-spec/validator")
	}
	output := filepath.Join(t.TempDir(), "print.csvx")
	if err := WritePackage(importPrintFixture(t), output); err != nil {
		t.Fatal(err)
	}
	result, err := exec.Command(node, "../csvx-spec/validator/bin/csvx-validate.mjs", output).CombinedOutput()
	if err != nil {
		t.Fatalf("validator rejected package: %v\n%s", err, result)
	}
}
