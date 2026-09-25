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
type xlsxWorksheet struct {
	Cols []struct { Min string `xml:"min,attr"`; Max string `xml:"max,attr"`; Width string `xml:"width,attr"`; CustomWidth string `xml:"customWidth,attr"` } `xml:"cols>col"`
	Rows []struct { Number string `xml:"r,attr"`; Height string `xml:"ht,attr"`; CustomHeight string `xml:"customHeight,attr"`; Cells []xlsxCell `xml:"c"` } `xml:"sheetData>row"`
}
type xlsxCell struct { Ref string `xml:"r,attr"`; Type string `xml:"t,attr"`; Style string `xml:"s,attr"`; Formula string `xml:"f"`; Value string `xml:"v"`; Inline struct { Text []string `xml:"t"` } `xml:"is"` }

func importXLSXWorkbook(filename string, inspection *XLSXInspection) (*Workbook, error) {
	files, err := readXLSXFiles(filename)
	if err != nil { return nil, err }
	shared, err := parseSharedStrings(files["xl/sharedStrings.xml"])
	if err != nil { return nil, err }
	styles, err := parseXLSXStyles(files["xl/styles.xml"])
	if err != nil { return nil, err }
	names, paths, err := xlsxSheetPaths(files)
	if err != nil { return nil, err }
	workbook := &Workbook{ID: strings.TrimSuffix(path.Base(filename), path.Ext(filename)), Version: "1.0", Styles: styles}
	for index, sheetPath := range paths {
		sheet, err := importXLSXSheet(files[sheetPath], names[index], index, shared, styles)
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

func importXLSXSheet(body []byte, name string, index int, shared []string, styles map[string]map[string]any) (*Sheet, error) {
	var worksheet xlsxWorksheet
	if err := xml.Unmarshal(body, &worksheet); err != nil { return nil, err }
	maxColumn, maxRow := 0, 0
	values := make(map[string]string)
	metadata := make(map[string]CellMetadata)
	rowHeights := make(map[int]float64)
	columnWidths := make(map[int]float64)
	for _, column := range worksheet.Cols { min, _ := strconv.Atoi(column.Min); max, _ := strconv.Atoi(column.Max); width, _ := strconv.ParseFloat(column.Width, 64); for index := min; index <= max; index++ { columnWidths[index-1] = width } }
	for _, row := range worksheet.Rows { rowNumber, _ := strconv.Atoi(row.Number); if rowNumber == 0 { rowNumber = maxRow + 1 }; height, _ := strconv.ParseFloat(row.Height, 64); if rowNumber > 0 && height > 0 { rowHeights[rowNumber] = height }; if rowNumber > maxRow { maxRow = rowNumber }
		for cellIndex, cell := range row.Cells { ref := cell.Ref; if ref == "" { ref = cellReference(cellIndex, rowNumber-1) }; column, _ := splitCellReference(ref); if column >= maxColumn { maxColumn = column + 1 }; value := xlsxCellValue(cell, shared); values[ref] = value; cellMetadata := CellMetadata{Type: xlsxValueType(cell, styles)}; if cell.Formula != "" || cell.Style != "" { cellMetadata.Formula, cellMetadata.Style = formulaValue(cell.Formula), cell.Style }; if cell.Formula != "" { cached := typedValue(cell.Type, value); cellMetadata.Cached = &cached }; if cellMetadata.Type != "" || cellMetadata.Formula != "" || cellMetadata.Style != "" { metadata[ref] = cellMetadata } }
	}
	if maxColumn == 0 { maxColumn = 1 }
	columns := make([]Column, maxColumn)
	for column := range columns { columns[column] = Column{ID: columnID(column), Name: columnID(column), Width: columnWidths[column]} }
	// XLSX row 1 is a real worksheet row. CSVX requires a header row, so use
	// generated stable headers and preserve all worksheet rows as records.
	records := make([][]string, 0, maxRow)
	for row := 0; row < maxRow; row++ { record := make([]string, maxColumn); for column := range record { record[column] = values[cellReference(column, row)] }; records = append(records, record) }
	return &Sheet{ID: fmt.Sprintf("sheet-%d", index+1), Name: name, Columns: columns, Records: records, RowHeights: rowHeights, Cells: metadata}, nil
}

func xlsxCellValue(cell xlsxCell, shared []string) string { if cell.Type == "s" { index, _ := strconv.Atoi(cell.Value); if index >= 0 && index < len(shared) { return shared[index] } }; if cell.Type == "inlineStr" { return strings.Join(cell.Inline.Text, "") }; if cell.Type == "b" && cell.Value == "1" { return "TRUE" }; return cell.Value }
func xlsxValueType(cell xlsxCell, styles map[string]map[string]any) string { if cell.Type == "b" { return "boolean" }; if cell.Type == "e" { return "error" }; if cell.Type == "s" || cell.Type == "inlineStr" || cell.Type == "str" { return "string" }; if cell.Value == "" && cell.Formula == "" { return "blank" }; if style, ok := styles[cell.Style]; ok { format, _ := style["numberFormat"].(string); lower := strings.ToLower(format); if strings.Contains(lower, "h") || strings.Contains(lower, "s") { return "time" }; if strings.Contains(lower, "d") || strings.Contains(lower, "y") { return "date" }; if strings.Contains(format, "$") || strings.Contains(format, "€") || strings.Contains(format, "£") || strings.Contains(lower, "%") || strings.Contains(format, ".") { return "decimal" } }; if strings.Contains(cell.Value, ".") { return "decimal" }; return "integer" }
func formulaValue(value string) string { if value == "" { return "" }; if strings.HasPrefix(value, "=") { return value }; return "=" + value }
func typedValue(kind, value string) Value { switch kind { case "b": return Value{Type: "boolean", Value: value == "TRUE" || value == "1"}; case "e": return Value{Type: "error", Code: strings.TrimPrefix(value, "#")}; default: return Value{Type: "string", Value: value} } }
func cellReference(column, row int) string { return columnID(column) + strconv.Itoa(row+1) }
func splitCellReference(ref string) (int, int) { split := 0; for split < len(ref) && ref[split] >= 'A' && ref[split] <= 'Z' { split++ }; column := 0; for _, value := range ref[:split] { column = column*26 + int(value-'A'+1) }; row, _ := strconv.Atoi(ref[split:]); return column - 1, row }
