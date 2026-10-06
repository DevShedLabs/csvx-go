package csvx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type xlsxWorkbook struct {
	WorkbookPr struct {
		Date1904 string `xml:"date1904,attr"`
	} `xml:"workbookPr"`
	DefinedNames []xlsxDefinedName `xml:"definedNames>definedName"`
	Sheets       []struct {
		Name string `xml:"name,attr"`
		RID  string `xml:"id,attr"`
	} `xml:"sheets>sheet"`
}
type xlsxRelationships struct {
	Relationships []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}
type xlsxSharedStrings struct {
	Items []struct {
		Text []string `xml:"t"`
	} `xml:"si"`
}
type xlsxWorksheet struct {
	Cols []struct {
		Min         string `xml:"min,attr"`
		Max         string `xml:"max,attr"`
		Width       string `xml:"width,attr"`
		CustomWidth string `xml:"customWidth,attr"`
	} `xml:"cols>col"`
	Rows []struct {
		Number       string     `xml:"r,attr"`
		Height       string     `xml:"ht,attr"`
		CustomHeight string     `xml:"customHeight,attr"`
		Cells        []xlsxCell `xml:"c"`
	} `xml:"sheetData>row"`
	xlsxPrint
}
type xlsxCell struct {
	Ref     string `xml:"r,attr"`
	Type    string `xml:"t,attr"`
	Style   string `xml:"s,attr"`
	Formula string `xml:"f"`
	Value   string `xml:"v"`
	Inline  struct {
		Text []string `xml:"t"`
	} `xml:"is"`
}

func importXLSXWorkbook(filename string, inspection *XLSXInspection) (*Workbook, error) {
	files, err := readXLSXFiles(filename)
	if err != nil {
		return nil, err
	}
	shared, err := parseSharedStrings(files["xl/sharedStrings.xml"])
	if err != nil {
		return nil, err
	}
	styles, err := parseXLSXStyles(files["xl/styles.xml"])
	if err != nil {
		return nil, err
	}
	names, paths, err := xlsxSheetPaths(files)
	if err != nil {
		return nil, err
	}
	var book xlsxWorkbook
	if err := xml.Unmarshal(files["xl/workbook.xml"], &book); err != nil {
		return nil, fmt.Errorf("decode workbook: %w", err)
	}
	workbook := &Workbook{ID: strings.TrimSuffix(path.Base(filename), path.Ext(filename)), Version: "1.0", Styles: exportStyles(styles)}
	var formulaDiagnostics []XLSXDiagnostic
	for index, sheetPath := range paths {
		sheet, formulaWarnings, err := importXLSXSheet(files[sheetPath], names[index], index, shared, styles, xlsxBool(book.WorkbookPr.Date1904))
		if err != nil {
			return nil, fmt.Errorf("import sheet %q: %w", names[index], err)
		}
		applyXLSXPrintNames(sheet, index, book.DefinedNames)
		workbook.Sheets = append(workbook.Sheets, sheet)
		formulaDiagnostics = append(formulaDiagnostics, formulaWarnings...)
	}
	if len(workbook.Sheets) == 0 {
		return nil, fmt.Errorf("XLSX contains no worksheets")
	}
	workbook.NamedRanges, workbook.importWarnings = importXLSXDefinedNames(book.DefinedNames)
	workbook.importWarnings = append(workbook.importWarnings, formulaDiagnostics...)
	return workbook, nil
}

func readXLSXFiles(filename string) (map[string][]byte, error) {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open XLSX ZIP: %w", err)
	}
	defer archive.Close()
	files := make(map[string][]byte, len(archive.File))
	for _, entry := range archive.File {
		if entry.Name == "" || path.IsAbs(entry.Name) || strings.HasPrefix(path.Clean(entry.Name), "..") {
			return nil, fmt.Errorf("unsafe XLSX entry %q", entry.Name)
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("open XLSX entry %q: %w", entry.Name, err)
		}
		body, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read XLSX entry %q: %w", entry.Name, readErr)
		}
		files[entry.Name] = body
	}
	return files, nil
}

func parseSharedStrings(body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}
	var resource xlsxSharedStrings
	if err := xml.Unmarshal(body, &resource); err != nil {
		return nil, fmt.Errorf("decode shared strings: %w", err)
	}
	values := make([]string, len(resource.Items))
	for index, item := range resource.Items {
		values[index] = strings.Join(item.Text, "")
	}
	return values, nil
}

func xlsxSheetPaths(files map[string][]byte) ([]string, []string, error) {
	var workbook xlsxWorkbook
	if err := xml.Unmarshal(files["xl/workbook.xml"], &workbook); err != nil {
		return nil, nil, fmt.Errorf("decode workbook: %w", err)
	}
	var relationships xlsxRelationships
	if err := xml.Unmarshal(files["xl/_rels/workbook.xml.rels"], &relationships); err != nil {
		return nil, nil, fmt.Errorf("decode workbook relationships: %w", err)
	}
	byID := make(map[string]string, len(relationships.Relationships))
	for _, relationship := range relationships.Relationships {
		// OPC allows absolute targets ("/xl/worksheets/sheet1.xml", as openpyxl writes them) as well
		// as ones relative to the part's directory.
		if strings.HasPrefix(relationship.Target, "/") {
			byID[relationship.ID] = path.Clean(strings.TrimPrefix(relationship.Target, "/"))
		} else {
			byID[relationship.ID] = path.Clean(path.Join("xl", relationship.Target))
		}
	}
	names, paths := make([]string, 0, len(workbook.Sheets)), make([]string, 0, len(workbook.Sheets))
	for _, sheet := range workbook.Sheets {
		sheetPath, ok := byID[sheet.RID]
		if !ok {
			return nil, nil, fmt.Errorf("missing worksheet relationship %q", sheet.RID)
		}
		names, paths = append(names, sheet.Name), append(paths, sheetPath)
	}
	return names, paths, nil
}

func importXLSXSheet(body []byte, name string, index int, shared []string, styles map[string]map[string]any, date1904 bool) (*Sheet, []XLSXDiagnostic, error) {
	var worksheet xlsxWorksheet
	if err := xml.Unmarshal(body, &worksheet); err != nil {
		return nil, nil, err
	}
	var warnings []XLSXDiagnostic
	maxColumn, maxRow := 0, 0
	values := make(map[string]string)
	metadata := make(map[string]CellMetadata)
	rowHeights := make(map[int]float64)
	columnWidths := make(map[int]float64)
	for _, column := range worksheet.Cols {
		min, _ := strconv.Atoi(column.Min)
		max, _ := strconv.Atoi(column.Max)
		width, _ := strconv.ParseFloat(column.Width, 64)
		for index := min; index <= max; index++ {
			columnWidths[index-1] = width
		}
	}
	for _, row := range worksheet.Rows {
		rowNumber, _ := strconv.Atoi(row.Number)
		if rowNumber == 0 {
			rowNumber = maxRow + 1
		}
		height, _ := strconv.ParseFloat(row.Height, 64)
		if rowNumber > 0 && height > 0 {
			rowHeights[rowNumber] = height
		}
		if rowNumber > maxRow {
			maxRow = rowNumber
		}
		for cellIndex, cell := range row.Cells {
			ref := cell.Ref
			if ref == "" {
				ref = cellReference(cellIndex, rowNumber-1)
			}
			column, _ := splitCellReference(ref)
			if column >= maxColumn {
				maxColumn = column + 1
			}
			value := xlsxCellValue(cell, shared)
			cellMetadata := CellMetadata{Type: xlsxValueType(cell, styles)}
			switch cellMetadata.Type {
			case "date", "time", "datetime":
				// Spec 14.10: the CSV text of a date or time is its ISO 8601 form, not the serial number.
				if iso, ok := isoFromSerial(cellMetadata.Type, value, date1904); ok && cell.Formula == "" {
					value = iso
				}
			case "error":
				value = "#" + csvxErrorCode(value)
			}
			values[ref] = value
			if cell.Formula != "" || cell.Style != "" {
				formula, warning := importXLSXFormula(name, ref, cell.Formula)
				if warning != nil {
					warnings = append(warnings, *warning)
				}
				cellMetadata.Formula, cellMetadata.Style = formula, xlsxStyleRef(cell.Style)
			}
			if cellMetadata.Formula != "" {
				cached := typedValue(cell.Type, value)
				cellMetadata.Cached = &cached
			}
			if cellMetadata.Type != "" || cellMetadata.Formula != "" || cellMetadata.Style != "" {
				metadata[ref] = cellMetadata
			}
		}
	}
	if maxColumn == 0 {
		maxColumn = 1
	}
	// Worksheet row N is CSVX row N (spec/03-sheets.md, 14.7): row 1 is the CSV header row, whose cell
	// text names the columns, and rows 2 and later are the data records. An empty header cell gives
	// the column the empty name (14.7); nothing is invented for it.
	columns := make([]Column, maxColumn)
	for column := range columns {
		columns[column] = Column{ID: columnID(column), Name: values[cellReference(column, 0)], Width: columnWidths[column]}
	}
	records := make([][]string, 0, maxRow)
	for row := 1; row < maxRow; row++ {
		record := make([]string, maxColumn)
		for column := range record {
			record[column] = values[cellReference(column, row)]
		}
		records = append(records, record)
	}
	return &Sheet{ID: fmt.Sprintf("sheet-%d", index+1), Name: name, Columns: columns, Records: records, RowHeights: rowHeights, Print: printFromWorksheet(worksheet.xlsxPrint), Cells: metadata}, warnings, nil
}

func xlsxCellValue(cell xlsxCell, shared []string) string {
	if cell.Type == "s" {
		index, _ := strconv.Atoi(cell.Value)
		if index >= 0 && index < len(shared) {
			return shared[index]
		}
	}
	if cell.Type == "inlineStr" {
		return strings.Join(cell.Inline.Text, "")
	}
	if cell.Type == "b" {
		// Spec 04-data-types.md: the literal forms of a boolean are exactly true and false.
		if cell.Value == "1" || strings.EqualFold(cell.Value, "true") {
			return "true"
		}
		return "false"
	}
	return cell.Value
}
func xlsxValueType(cell xlsxCell, styles map[string]map[string]any) string {
	if cell.Type == "b" {
		return "boolean"
	}
	if cell.Type == "e" {
		return "error"
	}
	if cell.Type == "s" || cell.Type == "inlineStr" || cell.Type == "str" {
		return "string"
	}
	if cell.Value == "" && cell.Formula == "" {
		return "blank"
	}
	if style, ok := styles[cell.Style]; ok {
		format, _ := style["numberFormat"].(string)
		if kind := dateTimeKind(format); kind != "" {
			return kind
		}
		lower := strings.ToLower(format)
		if strings.Contains(format, "$") || strings.Contains(format, "€") || strings.Contains(format, "£") || strings.Contains(lower, "%") || strings.Contains(format, ".") {
			return "decimal"
		}
	}
	if strings.Contains(cell.Value, ".") {
		return "decimal"
	}
	return "integer"
}
func formulaValue(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "=") {
		return value
	}
	return "=" + value
}
func typedValue(kind, value string) Value {
	switch kind {
	case "b":
		return Value{Type: "boolean", Value: value == "true" || value == "TRUE" || value == "1"}
	case "e":
		return Value{Type: "error", Code: csvxErrorCode(value)}
	case "s", "str", "inlineStr":
		return Value{Type: "string", Value: value}
	default:
		// XLSX omits the cell type attribute for numeric cells, including numeric formula
		// results — that must not default to "string" (schema-legal, semantically wrong; see
		// csvx-spec/AGENTS.md). Mirrors the same heuristic xlsxValueType uses below.
		if value == "" {
			return Value{Type: "blank"}
		}
		if strings.Contains(value, ".") {
			return Value{Type: "decimal", Value: value}
		}
		return Value{Type: "integer", Value: value}
	}
}
func cellReference(column, row int) string { return columnID(column) + strconv.Itoa(row+1) }
func splitCellReference(ref string) (int, int) {
	split := 0
	for split < len(ref) && ref[split] >= 'A' && ref[split] <= 'Z' {
		split++
	}
	column := 0
	for _, value := range ref[:split] {
		column = column*26 + int(value-'A'+1)
	}
	row, _ := strconv.Atoi(ref[split:])
	return column - 1, row
}

var quotedOrBracketed = regexp.MustCompile(`"[^"]*"|\[[^\]]*\]|\\.`)

// dateTimeKind decides from a number format whether it formats dates, times, or both (spec 14.10),
// ignoring quoted text, bracketed sections, and escaped characters. It returns "" for any other
// format.
func dateTimeKind(format string) string {
	lower := strings.ToLower(quotedOrBracketed.ReplaceAllString(format, ""))
	date := strings.ContainsAny(lower, "dy")
	clock := strings.ContainsAny(lower, "hs")
	switch {
	case date && clock:
		return "datetime"
	case date:
		return "date"
	case clock:
		return "time"
	}
	return ""
}

// isoFromSerial converts an XLSX serial number to the ISO 8601 text of the given kind.
func isoFromSerial(kind, serial string, date1904 bool) (string, bool) {
	number, err := strconv.ParseFloat(strings.TrimSpace(serial), 64)
	if err != nil || number < 0 {
		return "", false
	}
	epoch := excelEpoch
	if date1904 {
		epoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	} else if number < 61 {
		// The 1900 date system counts a nonexistent 29 February 1900, so serials before March 1900
		// are one day further along than the calendar.
		epoch = time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
	}
	seconds := int64(math.Round(number * 86400))
	moment := epoch.Add(time.Duration(seconds) * time.Second)
	switch kind {
	case "date":
		if number != math.Floor(number) {
			return "", false
		}
		return moment.Format("2006-01-02"), true
	case "time":
		if number >= 1 {
			return "", false
		}
		return moment.Format("15:04:05"), true
	}
	return moment.Format("2006-01-02T15:04:05"), true
}
