package csvx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

type xlsxWorkbook struct { Sheets []struct { Name string `xml:"name,attr"`; RID string `xml:"id,attr"` } `xml:"sheets>sheet"` }
type xlsxRelationships struct { Relationships []struct { ID string `xml:"Id,attr"`; Target string `xml:"Target,attr"` } `xml:"Relationship"` }
type xlsxSharedStrings struct { Items []struct { Text []string `xml:"t"` } `xml:"si"` }
type xlsxWorksheet struct { Rows []struct { Cells []xlsxCell `xml:"c"` } `xml:"sheetData>row"` }
type xlsxCell struct { Ref string `xml:"r,attr"`; Type string `xml:"t,attr"`; Style string `xml:"s,attr"`; Formula string `xml:"f"`; Value string `xml:"v"`; Inline struct { Text []string `xml:"t"` } `xml:"is"` }

func importXLSXWorkbook(filename string, inspection *XLSXInspection) (*Workbook, error) {
	files, err := readXLSXFiles(filename)
	if err != nil { return nil, err }
	shared, err := parseSharedStrings(files["xl/sharedStrings.xml"])
	if err != nil { return nil, err }
	names, paths, err := xlsxSheetPaths(files)
	if err != nil { return nil, err }
	workbook := &Workbook{ID: strings.TrimSuffix(path.Base(filename), path.Ext(filename)), Version: "1.0"}
	for index, sheetPath := range paths {
		sheet, err := importXLSXSheet(files[sheetPath], names[index], index, shared)
		if err != nil { return nil, fmt.Errorf("import sheet %q: %w", names[index], err) }
		workbook.Sheets = append(workbook.Sheets, sheet)
	}
	if len(workbook.Sheets) == 0 { return nil, fmt.Errorf("XLSX contains no worksheets") }
	return workbook, nil
}

func readXLSXFiles(filename string) (map[string][]byte, error) {
	archive, err := zip.OpenReader(filename)
	if err != nil { return nil, fmt.Errorf("open XLSX ZIP: %w", err) }
	defer archive.Close()
	files := make(map[string][]byte, len(archive.File))
	for _, entry := range archive.File {
		if entry.Name == "" || path.IsAbs(entry.Name) || strings.HasPrefix(path.Clean(entry.Name), "..") { return nil, fmt.Errorf("unsafe XLSX entry %q", entry.Name) }
		reader, err := entry.Open(); if err != nil { return nil, fmt.Errorf("open XLSX entry %q: %w", entry.Name, err) }
		body, readErr := io.ReadAll(reader); _ = reader.Close(); if readErr != nil { return nil, fmt.Errorf("read XLSX entry %q: %w", entry.Name, readErr) }
		files[entry.Name] = body
	}
	return files, nil
}

func parseSharedStrings(body []byte) ([]string, error) {
	if len(body) == 0 { return nil, nil }
	var resource xlsxSharedStrings
	if err := xml.Unmarshal(body, &resource); err != nil { return nil, fmt.Errorf("decode shared strings: %w", err) }
	values := make([]string, len(resource.Items))
	for index, item := range resource.Items { values[index] = strings.Join(item.Text, "") }
	return values, nil
}

func xlsxSheetPaths(files map[string][]byte) ([]string, []string, error) {
	var workbook xlsxWorkbook
	if err := xml.Unmarshal(files["xl/workbook.xml"], &workbook); err != nil { return nil, nil, fmt.Errorf("decode workbook: %w", err) }
	var relationships xlsxRelationships
	if err := xml.Unmarshal(files["xl/_rels/workbook.xml.rels"], &relationships); err != nil { return nil, nil, fmt.Errorf("decode workbook relationships: %w", err) }
	byID := make(map[string]string, len(relationships.Relationships))
	for _, relationship := range relationships.Relationships { byID[relationship.ID] = path.Clean(path.Join("xl", relationship.Target)) }
	names, paths := make([]string, 0, len(workbook.Sheets)), make([]string, 0, len(workbook.Sheets))
	for _, sheet := range workbook.Sheets {
		sheetPath, ok := byID[sheet.RID]; if !ok { return nil, nil, fmt.Errorf("missing worksheet relationship %q", sheet.RID) }
		names, paths = append(names, sheet.Name), append(paths, sheetPath)
	}
	return names, paths, nil
}

func importXLSXSheet(body []byte, name string, index int, shared []string) (*Sheet, error) {
	var worksheet xlsxWorksheet
	if err := xml.Unmarshal(body, &worksheet); err != nil { return nil, err }
	maxColumn, maxRow := 0, 0
	values := make(map[string]string)
	metadata := make(map[string]CellMetadata)
	for rowIndex, row := range worksheet.Rows {
		for cellIndex, cell := range row.Cells {
			ref := cell.Ref; if ref == "" { ref = cellReference(cellIndex, rowIndex) }
			column, rowNumber := splitCellReference(ref); if column > maxColumn { maxColumn = column }; if rowNumber > maxRow { maxRow = rowNumber }
			value := xlsxCellValue(cell, shared); values[ref] = value
			if cell.Formula != "" || cell.Style != "" { metadata[ref] = CellMetadata{Formula: formulaValue(cell.Formula), Style: cell.Style} }
			if cell.Formula != "" { cached := typedValue(cell.Type, value); metadata[ref] = CellMetadata{Formula: formulaValue(cell.Formula), Cached: &cached, Style: cell.Style} }
		}
	}
	if maxColumn == 0 { maxColumn = 1 }
	columns := make([]Column, maxColumn)
	for column := range columns { columns[column] = Column{ID: columnID(column), Name: columnID(column)} }
	// XLSX row 1 is a real worksheet row. CSVX requires a header row, so use
	// generated stable headers and preserve all worksheet rows as records.
	records := make([][]string, 0, maxRow)
	for row := 0; row < maxRow; row++ { record := make([]string, maxColumn); for column := range record { record[column] = values[cellReference(column, row)] }; records = append(records, record) }
	return &Sheet{ID: fmt.Sprintf("sheet-%d", index+1), Name: name, Columns: columns, Records: records, Cells: metadata}, nil
}

func xlsxCellValue(cell xlsxCell, shared []string) string { if cell.Type == "s" { index, _ := strconv.Atoi(cell.Value); if index >= 0 && index < len(shared) { return shared[index] } }; if cell.Type == "inlineStr" { return strings.Join(cell.Inline.Text, "") }; if cell.Type == "b" && cell.Value == "1" { return "TRUE" }; return cell.Value }
func formulaValue(value string) string { if value == "" { return "" }; if strings.HasPrefix(value, "=") { return value }; return "=" + value }
func typedValue(kind, value string) Value { switch kind { case "b": return Value{Type: "boolean", Value: value == "TRUE" || value == "1"}; case "e": return Value{Type: "error", Code: strings.TrimPrefix(value, "#")}; default: return Value{Type: "string", Value: value} } }
func cellReference(column, row int) string { return columnID(column) + strconv.Itoa(row+1) }
func splitCellReference(ref string) (int, int) { split := 0; for split < len(ref) && ref[split] >= 'A' && ref[split] <= 'Z' { split++ }; column := 0; for _, value := range ref[:split] { column = column*26 + int(value-'A'+1) }; row, _ := strconv.Atoi(ref[split:]); return column - 1, row }
