package csvx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritePackageRoundTrip(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "round-trip.csvx")
	original := &Workbook{
		ID:      "book-1",
		Version: "1.0",
		Sheets: []*Sheet{{
			ID:      "sales",
			Name:    "Sales",
			Columns: []Column{{ID: "A", Name: "Item"}, {ID: "B", Name: "Amount", Type: "decimal"}},
			Records: [][]string{{"Coffee", "19.95"}},
			Cells: map[string]CellMetadata{
				"B2": {Formula: "=1+2", Cached: &Value{Type: "decimal", Value: "3.00"}},
			},
		}},
	}
	if err := WritePackage(original, filename); err != nil {
		t.Fatalf("WritePackage() error = %v", err)
	}

	loaded, err := Open(filename)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if loaded.ID != original.ID || len(loaded.Sheets) != 1 {
		t.Fatalf("unexpected workbook: %#v", loaded)
	}
	if loaded.Sheets[0].Records[0][1] != "19.95" {
		t.Fatalf("unexpected CSV record: %#v", loaded.Sheets[0].Records)
	}
	if loaded.Sheets[0].Cells["B2"].Formula != "=1+2" {
		t.Fatalf("formula metadata was not preserved: %#v", loaded.Sheets[0].Cells)
	}
}

func TestWritePackageRejectsNilWorkbook(t *testing.T) {
	if err := WritePackage(nil, filepath.Join(t.TempDir(), "invalid.csvx")); err == nil {
		t.Fatal("WritePackage(nil) expected an error")
	}
}

func TestPackageDirectoryRoundTrip(t *testing.T) {
	input := t.TempDir()
	writeFixtureFile(t, filepath.Join(input, "manifest.json"), `{"format":"csvx","version":"1.0","workbook":"workbook.json","files":["manifest.json","workbook.json","sheets/sheet-1.csv"]}`)
	writeFixtureFile(t, filepath.Join(input, "workbook.json"), `{"id":"book-1","version":"1.0","sheets":[{"id":"sheet-1","name":"Sheet 1","path":"sheets/sheet-1.csv"}]}`)
	writeFixtureFile(t, filepath.Join(input, "sheets/sheet-1.csv"), "Value\n1\n2\n")

	packagePath := filepath.Join(t.TempDir(), "fixture.csvx")
	if err := PackageDirectory(input, packagePath); err != nil {
		t.Fatalf("PackageDirectory() error = %v", err)
	}
	extracted := filepath.Join(t.TempDir(), "extracted")
	if err := ExtractPackage(packagePath, extracted); err != nil {
		t.Fatalf("ExtractPackage() error = %v", err)
	}
	loaded, err := OpenDirectory(extracted)
	if err != nil {
		t.Fatalf("OpenDirectory() error = %v", err)
	}
	if len(loaded.Sheets) != 1 || loaded.Sheets[0].Records[1][0] != "2" {
		t.Fatalf("unexpected extracted workbook: %#v", loaded)
	}
}

func writeFixtureFile(t *testing.T, filename, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
