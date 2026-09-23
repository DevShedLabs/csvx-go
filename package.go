package csvx

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
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
	if info.IsDir() {
		return nil, fmt.Errorf("CSVX path is a directory: %s", filename)
	}
	return Load(file, info.Size())
}

// ExtractPackage extracts a CSVX ZIP package into an unpacked directory.
func ExtractPackage(filename, directory string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open CSVX package: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat CSVX package: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("CSVX package path is a directory: %s", filename)
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		return fmt.Errorf("read CSVX ZIP: %w", err)
	}
	entries, err := packageEntries(archive)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create extraction directory: %w", err)
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return fmt.Errorf("resolve extraction directory: %w", err)
	}
	for name, entry := range entries {
		target, err := safeExtractionPath(root, name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create extracted directory %q: %w", name, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create directory for %q: %w", name, err)
		}
		body, err := readEntry(entry)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			return fmt.Errorf("write extracted entry %q: %w", name, err)
		}
	}
	return nil
}

func safeExtractionPath(root, name string) (string, error) {
	cleanName := filepath.Clean(filepath.FromSlash(name))
	target := filepath.Join(root, cleanName)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("unsafe extraction path %q", name)
	}
	return target, nil
}

// PackageDirectory writes an unpacked CSVX package directory as a ZIP .csvx file.
func PackageDirectory(directory, output string) error {
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("stat CSVX directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("CSVX input is not a directory: %s", directory)
	}
	file, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create CSVX package: %w", err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	err = filepath.Walk(directory, func(filename string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(directory, filename)
		if err != nil {
			return err
		}
		zipPath := filepath.ToSlash(relative)
		archiveEntry, err := writer.Create(zipPath)
		if err != nil {
			return err
		}
		contents, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer contents.Close()
		_, err = io.Copy(archiveEntry, contents)
		return err
	})
	if err != nil {
		_ = writer.Close()
		return fmt.Errorf("package CSVX directory: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close CSVX package: %w", err)
	}
	return nil
}

// OpenDirectory reads an unpacked CSVX package directory.
func OpenDirectory(directory string) (*Workbook, error) {
	info, err := os.Stat(directory)
	if err != nil {
		return nil, fmt.Errorf("stat CSVX directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("CSVX path is not a directory: %s", directory)
	}
	manifest, err := readJSONFile[Manifest](filepath.Join(directory, "manifest.json"))
	if err != nil {
		return nil, err
	}
	if manifest.Format != "csvx" || manifest.Version != "1.0" || manifest.Workbook != "workbook.json" {
		return nil, fmt.Errorf("unsupported CSVX manifest")
	}
	workbookDoc, err := readJSONFile[WorkbookDocument](filepath.Join(directory, manifest.Workbook))
	if err != nil {
		return nil, err
	}
	if workbookDoc.Version != "1.0" || len(workbookDoc.Sheets) == 0 {
		return nil, fmt.Errorf("invalid workbook resource")
	}
	workbook := &Workbook{ID: workbookDoc.ID, Version: workbookDoc.Version, Calculation: workbookDoc.Calculation}
	for _, entry := range workbookDoc.Sheets {
		sheet, err := loadDirectorySheet(directory, entry)
		if err != nil {
			return nil, err
		}
		workbook.Sheets = append(workbook.Sheets, sheet)
	}
	return workbook, nil
}

func loadDirectorySheet(directory string, entry SheetEntry) (*Sheet, error) {
	body, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(entry.Path)))
	if err != nil {
		return nil, fmt.Errorf("read sheet CSV %q: %w", entry.Path, err)
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
	metadataBody, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(entry.Metadata)))
	if err != nil {
		return nil, fmt.Errorf("read sheet metadata %q: %w", entry.Metadata, err)
	}
	return applySheetMetadata(sheet, metadataBody, entry)
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
	return applySheetMetadata(sheet, metadataBody, entry)
}

func applySheetMetadata(sheet *Sheet, metadataBody []byte, entry SheetEntry) (*Sheet, error) {
	var resource struct {
		ID      string                  `json:"id"`
		Name    string                  `json:"name"`
		Columns []Column                `json:"columns"`
		Cells   map[string]CellMetadata `json:"cells"`
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
		if file.Name == "" || path.IsAbs(file.Name) || strings.HasPrefix(path.Clean(file.Name), "..") || strings.ContainsRune(file.Name, '\x00') {
			return nil, fmt.Errorf("invalid ZIP entry path %q", file.Name)
		}
		if _, exists := entries[file.Name]; exists {
			return nil, fmt.Errorf("duplicate ZIP entry %q", file.Name)
		}
		entries[file.Name] = file
	}
	return entries, nil
}

func readJSONFile[T any](filename string) (T, error) {
	var value T
	body, err := os.ReadFile(filename)
	if err != nil {
		return value, fmt.Errorf("read %q: %w", filename, err)
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return value, fmt.Errorf("decode %q: %w", filename, err)
	}
	return value, nil
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
