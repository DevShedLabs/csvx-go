package csvx

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// Open reads a CSVX ZIP package from disk.
func Open(filename string) (*Workbook, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open CSVX file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat CSVX file: %w", err)
	}
	return Load(file, info.Size())
}

// Load reads a CSVX ZIP package from an io.ReaderAt with the supplied size.
func Load(reader io.ReaderAt, size int64) (*Workbook, error) {
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return nil, fmt.Errorf("read CSVX ZIP: %w", err)
	}
	entries, err := packageEntries(archive)
	if err != nil {
		return nil, err
	}
	manifest, err := decodeEntry[Manifest](entries, "manifest.json")
	if err != nil {
		return nil, err
	}
	if manifest.Format != "csvx" || manifest.Version != "1.0" || manifest.Workbook != "workbook.json" {
		return nil, fmt.Errorf("unsupported CSVX manifest")
	}
	workbookDoc, err := decodeEntry[WorkbookDocument](entries, manifest.Workbook)
	if err != nil {
		return nil, err
	}
	if workbookDoc.Version != "1.0" || len(workbookDoc.Sheets) == 0 {
		return nil, fmt.Errorf("invalid workbook resource")
	}

	workbook := &Workbook{ID: workbookDoc.ID, Version: workbookDoc.Version, Calculation: workbookDoc.Calculation}
	for _, entry := range workbookDoc.Sheets {
		sheet, err := loadSheet(entries, entry)
		if err != nil {
			return nil, err
		}
		workbook.Sheets = append(workbook.Sheets, sheet)
	}
	return workbook, nil
}

func loadSheet(entries map[string]*zip.File, entry SheetEntry) (*Sheet, error) {
	file, ok := entries[entry.Path]
	if !ok {
		return nil, fmt.Errorf("missing sheet CSV %q", entry.Path)
	}
	body, err := readEntry(file)
	if err != nil {
		return nil, err
	}
	header, records, err := readCSV(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("sheet %q: %w", entry.Name, err)
	}
	sheet := &Sheet{ID: entry.ID, Name: entry.Name, Path: entry.Path, MetadataPath: entry.Metadata, Records: records, Cells: map[string]CellMetadata{}}
	for index, name := range header {
		sheet.Columns = append(sheet.Columns, Column{ID: columnID(index), Name: name})
	}
	if entry.Metadata == "" {
		return sheet, nil
	}
	metadata, ok := entries[entry.Metadata]
	if !ok {
		return nil, fmt.Errorf("missing sheet metadata %q", entry.Metadata)
	}
	metadataBody, err := readEntry(metadata)
	if err != nil {
		return nil, err
	}
	var resource struct {
		ID      string                   `json:"id"`
		Name    string                   `json:"name"`
		Columns []Column                 `json:"columns"`
		Cells   map[string]CellMetadata  `json:"cells"`
	}
	if err := json.Unmarshal(metadataBody, &resource); err != nil {
		return nil, fmt.Errorf("decode sheet metadata %q: %w", entry.Metadata, err)
	}
	if resource.ID != "" && resource.ID != sheet.ID || resource.Name != "" && resource.Name != sheet.Name {
		return nil, fmt.Errorf("sheet metadata identity mismatch for %q", entry.Name)
	}
	if len(resource.Columns) > 0 {
		if len(resource.Columns) != len(sheet.Columns) {
			return nil, fmt.Errorf("sheet %q metadata column count does not match CSV", sheet.Name)
		}
		sheet.Columns = resource.Columns
	}
	sheet.Cells = resource.Cells
	return sheet, nil
}

func packageEntries(archive *zip.Reader) (map[string]*zip.File, error) {
	entries := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		if file.Name == "" || path.IsAbs(file.Name) || strings.HasPrefix(path.Clean(file.Name), "..") || strings.ContainsRune(file.Name, '\\x00') {
			return nil, fmt.Errorf("invalid ZIP entry path %q", file.Name)
		}
		if _, exists := entries[file.Name]; exists {
			return nil, fmt.Errorf("duplicate ZIP entry %q", file.Name)
		}
		entries[file.Name] = file
	}
	return entries, nil
}

func decodeEntry[T any](entries map[string]*zip.File, name string) (T, error) {
	var value T
	file, ok := entries[name]
	if !ok {
		return value, fmt.Errorf("missing package entry %q", name)
	}
	body, err := readEntry(file)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return value, fmt.Errorf("decode %q: %w", name, err)
	}
	return value, nil
}

func readEntry(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open ZIP entry %q: %w", file.Name, err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read ZIP entry %q: %w", file.Name, err)
	}
	return body, nil
}

func columnID(index int) string {
	var result []byte
	for index >= 0 {
		result = append([]byte{byte('A' + index%26)}, result...)
		index = index/26 - 1
	}
	return string(result)
}
