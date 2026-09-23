package csvx

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestLoadCSVBackedWorkbook(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	writeTestEntry(t, writer, "manifest.json", `{"format":"csvx","version":"1.0","workbook":"workbook.json","files":["manifest.json","workbook.json","sheets/sales.csv"]}`)
	writeTestEntry(t, writer, "workbook.json", `{"id":"book-1","version":"1.0","sheets":[{"id":"sales","name":"Sales","path":"sheets/sales.csv"}]}`)
	writeTestEntry(t, writer, "sheets/sales.csv", "Item,Amount\nCoffee,19.95\n")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	workbook, err := Load(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(workbook.Sheets) != 1 || workbook.Sheets[0].Records[0][1] != "19.95" {
		t.Fatalf("unexpected workbook: %#v", workbook)
	}
}

func writeTestEntry(t *testing.T, writer *zip.Writer, name, content string) {
	t.Helper()
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}
