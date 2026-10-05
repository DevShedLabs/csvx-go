package csvx

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CSVImportOptions are the options of csvx-spec/spec/11-import-export.md §11.1. The zero value is
// the spec default except Header, which is inverted (NoHeader) so the zero value is correct.
type CSVImportOptions struct {
	// Delimiter is the field delimiter; 0 means ','.
	Delimiter rune
	// NoHeader means the first record is data, not a header (spec option header=false).
	NoHeader bool
	// Infer declares column types from the data.
	Infer bool
	// Name is the sheet name; empty means "Sheet 1".
	Name string
}

// ImportWarning is a lossy or adjusted step during import, with a location and reason.
type ImportWarning struct {
	Location string `json:"location"`
	Reason   string `json:"reason"`
}

// CSVSyntaxError reports malformed CSV with the 1-based line of the fault.
type CSVSyntaxError struct {
	Line int
	Err  error
}

func (e *CSVSyntaxError) Error() string {
	return fmt.Sprintf("malformed CSV at line %d: %v", e.Line, e.Err)
}
func (e *CSVSyntaxError) Unwrap() error { return e.Err }

// ImportCSV converts plain CSV bytes into a single-sheet workbook per spec §11.1. The workbook is
// in memory; write it with WritePackage.
func ImportCSV(data []byte, options CSVImportOptions) (*Workbook, []ImportWarning, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(data) {
		return nil, nil, fmt.Errorf("CSV input is not valid UTF-8")
	}
	name := options.Name
	if name == "" {
		name = "Sheet 1"
	}
	if err := validateSheetName(name); err != nil {
		return nil, nil, err
	}
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	if options.Delimiter != 0 {
		reader.Comma = options.Delimiter
	}
	var records [][]string
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				line := parseErr.StartLine
				if line == 0 {
					line = parseErr.Line
				}
				return nil, nil, &CSVSyntaxError{Line: line, Err: parseErr.Err}
			}
			return nil, nil, fmt.Errorf("read CSV: %w", err)
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("CSV input has no records; a sheet needs a header row")
	}

	var warnings []ImportWarning
	var header []string
	if options.NoHeader {
		warnings = append(warnings, ImportWarning{Location: "record 1", Reason: "no header row: synthesized a header, so data rows are shifted down by one"})
	} else {
		header, records = records[0], records[1:]
	}

	firstData := 1
	if !options.NoHeader {
		firstData = 2
	}
	headerWidth := len(header)
	width := headerWidth
	for _, record := range records {
		if len(record) > width {
			width = len(record)
		}
	}
	for len(header) < width {
		header = append(header, "")
	}
	// An empty header field is legal (spec 03-sheets.md): the column simply has the empty name, and
	// the importer invents nothing.
	for index, record := range records {
		location := fmt.Sprintf("record %d", firstData+index)
		if !options.NoHeader && len(record) > headerWidth {
			warnings = append(warnings, ImportWarning{Location: location, Reason: fmt.Sprintf("has %d fields; header has %d, added columns with the empty name", len(record), headerWidth)})
		}
		if len(record) < width {
			warnings = append(warnings, ImportWarning{Location: location, Reason: fmt.Sprintf("has %d fields; padded to %d", len(record), width)})
			records[index] = append(record, make([]string, width-len(record))...)
		}
	}

	columns := make([]Column, width)
	for i := range columns {
		columns[i] = Column{ID: columnID(i), Name: header[i]}
	}
	if options.Infer {
		for i := range columns {
			columns[i].Type = inferColumnType(records, i)
		}
	}

	id := sheetIDFromName(name)
	sheet := &Sheet{ID: id, Name: name, Columns: columns, Records: records}
	for _, column := range columns {
		if column.Type != "" {
			sheet.MetadataPath = "sheets/" + id + ".meta.json"
			break
		}
	}
	workbook := &Workbook{ID: id, Version: "1.0", Sheets: []*Sheet{sheet}, Calculation: Calculation{Mode: "automatic"}}
	return workbook, warnings, nil
}

// ImportCSVFile reads a CSV file and imports it; an empty options.Name defaults to the file stem.
func ImportCSVFile(filename string, options CSVImportOptions) (*Workbook, []ImportWarning, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("read CSV: %w", err)
	}
	if options.Name == "" {
		base := filepath.Base(filename)
		if stem := strings.TrimSuffix(base, filepath.Ext(base)); stem != "" {
			options.Name = stem
		}
	}
	return ImportCSV(data, options)
}

// inferColumnType implements the spec §11.1 "Type inference" rules for one column.
func inferColumnType(records [][]string, column int) string {
	seen := map[string]bool{}
	for _, record := range records {
		if kind := literalType(record[column]); kind != "blank" {
			seen[kind] = true
		}
	}
	switch {
	case len(seen) == 0:
		return ""
	case len(seen) == 1:
		for kind := range seen {
			return kind
		}
	case len(seen) == 2 && seen["integer"] && seen["decimal"]:
		return "decimal"
	}
	return "string"
}

func validateSheetName(name string) error {
	if utf8.RuneCountInString(name) > 255 {
		return fmt.Errorf("sheet name is longer than 255 characters")
	}
	for _, r := range name {
		if r == ':' || unicode.IsControl(r) {
			return fmt.Errorf("sheet name must not contain ':' or control characters")
		}
	}
	return nil
}

// sheetIDFromName derives the stable sheet/workbook id per spec §11.1.
func sheetIDFromName(name string) string {
	var out []byte
	pendingDash := false
	for _, r := range strings.ToLower(name) {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			if pendingDash && len(out) > 0 {
				out = append(out, '-')
			}
			pendingDash = false
			out = append(out, byte(r))
		} else {
			pendingDash = true
		}
	}
	id := strings.Trim(string(out), "-")
	if id == "" {
		return "sheet-1"
	}
	if id[0] < 'a' || id[0] > 'z' {
		id = "s" + id
	}
	if len(id) > 64 {
		id = id[:64]
	}
	return id
}
