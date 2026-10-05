package csvx

import (
	"fmt"
	"strings"
)

// CSV export, implementing csvx-spec/spec/11-import-export.md 11.2. The output is the sheet's data
// and nothing else, byte for byte predictable: UTF-8, LF terminators, minimal quoting, and no
// escaping or altering of field text.

// CSVExportOptions are the options of 11.2. The zero value is the spec default except NoHeader,
// which is inverted so the zero value is correct.
type CSVExportOptions struct {
	// Sheet is a sheet id or name; empty means the first sheet.
	Sheet string
	// NoHeader omits the header row (spec option header=false).
	NoHeader bool
	// Delimiter is the field delimiter; 0 means ','.
	Delimiter rune
	// Formulas is "values" (the default, also for "") or "text".
	Formulas string
	// Display writes numbers as their number format displays them.
	Display bool
}

// ExportWarning is something a CSV export leaves out, with a location and reason.
type ExportWarning struct {
	Feature  string `json:"feature"`
	Location string `json:"location"`
	Message  string `json:"message"`
}

// formatCSV writes rows as CSV: every record ends with LF, and a field is quoted if and only if it
// contains the delimiter, a double quote, CR, or LF. It is the one CSV writer, used for packages
// and for export (spec 11.1 and 11.2).
func formatCSV(rows [][]string, delimiter rune) string {
	if delimiter == 0 {
		delimiter = ','
	}
	var out strings.Builder
	for _, row := range rows {
		for i, field := range row {
			if i > 0 {
				out.WriteRune(delimiter)
			}
			if strings.ContainsAny(field, "\"\r\n") || strings.ContainsRune(field, delimiter) {
				out.WriteByte('"')
				out.WriteString(strings.ReplaceAll(field, `"`, `""`))
				out.WriteByte('"')
			} else {
				out.WriteString(field)
			}
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// literalText is a value in its literal form (spec 04-data-types.md): true or false, a number's or
// string's text as is, an error as # and its code, a blank as the empty string.
func literalText(v Value) string {
	switch v.Type {
	case "", "blank":
		return ""
	case "error":
		return "#" + v.Code
	case "boolean":
		if b, _ := v.Value.(bool); b {
			return "true"
		}
		return "false"
	}
	return stringOf(v)
}

// ExportCSV writes one sheet of a workbook as CSV text and reports what CSV cannot carry.
func ExportCSV(workbook *Workbook, options CSVExportOptions) (string, []ExportWarning, error) {
	if workbook == nil || len(workbook.Sheets) == 0 {
		return "", nil, fmt.Errorf("workbook requires at least one sheet")
	}
	if options.Delimiter == '"' || options.Delimiter == '\r' || options.Delimiter == '\n' {
		return "", nil, fmt.Errorf("delimiter must not be a double quote, CR or LF")
	}
	if options.Formulas != "" && options.Formulas != "values" && options.Formulas != "text" {
		return "", nil, fmt.Errorf("formulas must be \"values\" or \"text\", got %q", options.Formulas)
	}
	index := 0
	if options.Sheet != "" {
		var err error
		if index, err = sheetIndex(workbook, options.Sheet); err != nil {
			return "", nil, err
		}
	}
	// A stale or missing cache must never reach the file: recalculate first.
	calculated := RecalculateWorkbook(workbook)
	sheet := calculated.Sheets[index]

	rowCount := len(sheet.Records)
	formulaCells := 0
	for coordinate, metadata := range sheet.Cells {
		if metadata.Formula == "" {
			continue
		}
		formulaCells++
		if _, row, ok := IndicesForCoordinate(coordinate); ok && row+1 > rowCount {
			rowCount = row + 1
		}
	}

	var rows [][]string
	if !options.NoHeader {
		header := make([]string, len(sheet.Columns))
		for i, column := range sheet.Columns {
			header[i] = column.Name
		}
		rows = append(rows, header)
	}
	for r := 0; r < rowCount; r++ {
		fields := make([]string, len(sheet.Columns))
		for c := range sheet.Columns {
			metadata := sheet.Cells[CoordinateFor(c, r)]
			raw := ""
			if r < len(sheet.Records) && c < len(sheet.Records[r]) {
				raw = sheet.Records[r][c]
			}
			numberFormat := ""
			if options.Display {
				numberFormat = numberFormatFor(calculated.Styles, metadata.Style)
			}
			switch {
			case metadata.Formula != "" && options.Formulas == "text":
				fields[c] = metadata.Formula
			case metadata.Formula != "":
				value := Value{Type: "blank"}
				if metadata.Cached != nil {
					value = *metadata.Cached
				}
				fields[c] = displayOrLiteral(value, numberFormat)
			case numberFormat != "":
				declared := metadata.Type
				if declared == "" {
					declared = sheet.Columns[c].Type
				}
				fields[c] = displayOrLiteral(ResolveCellValue(raw, declared, numberFormat), numberFormat)
				if fields[c] == "" {
					fields[c] = raw
				}
			default:
				fields[c] = raw
			}
		}
		rows = append(rows, fields)
	}

	var warnings []ExportWarning
	if formulaCells > 0 && options.Formulas != "text" {
		warnings = append(warnings, ExportWarning{Feature: "formula", Location: sheet.Name, Message: fmt.Sprintf("%d formula cell(s) were exported as their calculated values; the formulas were omitted", formulaCells)})
	}
	if sheetHasExportOnlyMetadata(sheet) || len(workbook.NamedRanges) > 0 {
		warnings = append(warnings, ExportWarning{Feature: "metadata", Location: sheet.Name, Message: "CSV holds data only; styles, validation rules, widths, heights, print settings and names were not written"})
	}
	return formatCSV(rows, options.Delimiter), warnings, nil
}

// displayOrLiteral writes a number as its number format displays it, and anything else (or any
// value with no format) in its literal form.
func displayOrLiteral(v Value, numberFormat string) string {
	if numberFormat != "" && (v.Type == "integer" || v.Type == "decimal") {
		return FormatValue(v, numberFormat)
	}
	return literalText(v)
}

func sheetHasExportOnlyMetadata(sheet *Sheet) bool {
	if sheet.Print != nil || len(sheet.RowHeights) > 0 {
		return true
	}
	for _, column := range sheet.Columns {
		if column.Width != 0 {
			return true
		}
	}
	for _, metadata := range sheet.Cells {
		if metadata.Style != "" || len(metadata.Validation) > 0 {
			return true
		}
	}
	return false
}
