package csvx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Convert imports or exports a workbook based on the input and output extensions.
func Convert(input, output string) error {
	inputExt := strings.ToLower(filepath.Ext(input))
	outputExt := strings.ToLower(filepath.Ext(output))
	if inputExt == ".xlsx" && outputExt == ".csvx" {
		return importXLSXSource(input, output)
	}
	if inputExt == ".csvx" && outputExt == ".xlsx" {
		return exportXLSXSource(input, output)
	}
	return fmt.Errorf("unsupported conversion %q to %q; supported conversions are .xlsx to .csvx and .csvx to .xlsx", inputExt, outputExt)
}

func importXLSXSource(input, output string) error {
	inspection, err := InspectXLSX(input)
	if err != nil {
		return err
	}
	source, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read XLSX source: %w", err)
	}
	workbook, err := importXLSXWorkbook(input, inspection)
	if err != nil { return err }
	workbook.Source = &SourceMetadata{
		Format: inspection.Format, Filename: inspection.Filename, SHA256: inspection.SHA256,
		Authority: "original", ImportedAt: time.Now().UTC().Format(time.RFC3339),
		Importer: "csvx-go", Features: inspection.Features, Warnings: inspection.Warnings,
	}
	workbook.SourceBytes = source
	return WritePackage(workbook, output)
}

func placeholderSheets(inspection *XLSXInspection) []*Sheet {
	sheets := make([]*Sheet, 0, len(inspection.Sheets))
	for index, sourceSheet := range inspection.Sheets {
		id := fmt.Sprintf("sheet-%d", index+1)
		name := sourceSheet.Name
		if name == "" { name = id }
		sheets = append(sheets, &Sheet{ID: id, Name: name, Columns: []Column{{ID: "A", Name: "Imported XLSX"}}, Records: [][]string{{"Source preserved; canonical cell import pending"}}})
	}
	if len(sheets) == 0 { sheets = append(sheets, &Sheet{ID: "sheet-1", Name: "Imported XLSX", Columns: []Column{{ID: "A", Name: "Source"}}, Records: [][]string{{"Source preserved"}}}) }
	return sheets
}

func exportXLSXSource(input, output string) error {
	workbook, err := Open(input)
	if err != nil { return err }
	if workbook.Source == nil || len(workbook.SourceBytes) == 0 || workbook.Source.Authority != "original" {
		return fmt.Errorf("CSVX package has no authoritative embedded XLSX source; edited XLSX export is not implemented")
	}
	if hashBytes(workbook.SourceBytes) != workbook.Source.SHA256 { return fmt.Errorf("embedded XLSX source failed SHA-256 verification") }
	if err := os.WriteFile(output, workbook.SourceBytes, 0o644); err != nil { return fmt.Errorf("write XLSX source: %w", err) }
	return nil
}
