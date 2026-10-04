package csvx

// CSVX to XLSX export, implementing csvx-spec/spec/14-xlsx-interoperability.md 14.9. The CSVX
// workbook is the authority: values, formulas (with cached results), styles, column widths, row
// heights, print settings, validations, and names are written from the model, never from an
// embedded source that an edit has made stale. Anything XLSX cannot represent is reported as a
// warning rather than dropped silently.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ExportXLSX writes a workbook to an XLSX file and returns the warnings for everything that could
// not be represented exactly.
func ExportXLSX(workbook *Workbook, output string) ([]XLSXDiagnostic, error) {
	var buffer bytes.Buffer
	warnings, err := ExportXLSXTo(workbook, &buffer)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(output, buffer.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("write XLSX: %w", err)
	}
	return warnings, nil
}

// ExportXLSXTo writes a workbook as XLSX to any writer; see ExportXLSX.
func ExportXLSXTo(workbook *Workbook, out io.Writer) ([]XLSXDiagnostic, error) {
	if workbook == nil || len(workbook.Sheets) == 0 {
		return nil, fmt.Errorf("workbook requires at least one sheet")
	}
	e := &xlsxExporter{shared: map[string]int{}, formats: map[string]int{}, fontIDs: map[string]int{}, fillIDs: map[string]int{}, borderIDs: map[string]int{}, xfIDs: map[string]int{}}
	e.workbook = e.withValidSheetNames(workbook)
	e.initStyles()

	files := map[string][]byte{}
	var sheetXML [][]byte
	for index, sheet := range e.workbook.Sheets {
		body, err := e.worksheet(sheet, index)
		if err != nil {
			return nil, err
		}
		sheetXML = append(sheetXML, body)
	}
	for index, body := range sheetXML {
		files[fmt.Sprintf("xl/worksheets/sheet%d.xml", index+1)] = body
	}
	files["[Content_Types].xml"] = e.contentTypes(len(sheetXML))
	files["_rels/.rels"] = []byte(xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	files["xl/workbook.xml"] = e.workbookXML()
	files["xl/_rels/workbook.xml.rels"] = e.workbookRels(len(sheetXML))
	files["xl/styles.xml"] = e.stylesXML()
	files["xl/sharedStrings.xml"] = e.sharedStringsXML()

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	writer := zip.NewWriter(out)
	for _, name := range names {
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return nil, fmt.Errorf("write XLSX entry %q: %w", name, err)
		}
		if _, err := entry.Write(files[name]); err != nil {
			return nil, fmt.Errorf("write XLSX entry %q: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close XLSX: %w", err)
	}
	return e.warnings, nil
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

type xlsxExporter struct {
	workbook *Workbook
	warnings []XLSXDiagnostic

	shared       map[string]int
	sharedList   []string
	formats      map[string]int // custom number format code -> id
	formatList   []string
	fontIDs      map[string]int
	fonts        []string
	fillIDs      map[string]int
	fills        []string
	borderIDs    map[string]int
	borders      []string
	xfIDs        map[string]int
	xfs          []string
	styleMaps    map[string]map[string]any
	nextFormatID int
}

func (e *xlsxExporter) warn(feature, location, message string) {
	e.warnings = append(e.warnings, XLSXDiagnostic{Severity: "warning", Feature: feature, Path: location, Message: message})
}

func escape(text string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(text))
	return out.String()
}

func attr(name, value string) string { return " " + name + `="` + escape(value) + `"` }

// ---------------------------------------------------------------------------------------------
// Sheet names.

var xlsxForbiddenNameChars = regexp.MustCompile(`[\[\]*?/\\]`)

// withValidSheetNames returns a copy of the workbook whose sheet names are legal in XLSX, with every
// reference to a renamed sheet rewritten, and warns for each change (spec 14.9).
func (e *xlsxExporter) withValidSheetNames(workbook *Workbook) *Workbook {
	copied := cloneWorkbook(workbook)
	used := map[string]bool{}
	for index, sheet := range copied.Sheets {
		name := xlsxForbiddenNameChars.ReplaceAllString(sheet.Name, "_")
		if runes := []rune(name); len(runes) > 31 {
			name = string(runes[:31])
		}
		unique := name
		for n := 2; used[strings.ToLower(unique)]; n++ {
			suffix := "_" + strconv.Itoa(n)
			base := []rune(name)
			if len(base)+len(suffix) > 31 {
				base = base[:31-len(suffix)]
			}
			unique = string(base) + suffix
		}
		used[strings.ToLower(unique)] = true
		if unique != sheet.Name {
			old := sheet.Name
			e.warn("sheetName", old, fmt.Sprintf("sheet name %q is not valid in XLSX and was exported as %q", old, unique))
			copied.Sheets[index].Name = unique
			rewriteAllFormulas(copied, func(f string) string { return RewriteFormulaForSheetChange(f, old, unique, false) })
		}
	}
	return copied
}

// ---------------------------------------------------------------------------------------------
// Styles.

var builtinFormatIDs = map[string]int{"General": 0, "0": 1, "0.00": 2, "#,##0": 3, "#,##0.00": 4, "0%": 9, "0.00%": 10}

func (e *xlsxExporter) initStyles() {
	e.styleMaps = map[string]map[string]any{}
	for _, style := range e.workbook.Styles {
		e.styleMaps[style.Id] = styleToMap(style)
	}
	e.nextFormatID = 164
	// Mandatory defaults: font 0, fills 0 and 1 (none, gray125), border 0, cell format 0.
	e.fontID(map[string]any{})
	e.fills = []string{`<fill><patternFill patternType="none"/></fill>`, `<fill><patternFill patternType="gray125"/></fill>`}
	e.fillIDs["none"], e.fillIDs["gray125"] = 0, 1
	e.borderID(nil)
	e.xfs = []string{`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`}
	e.xfIDs[""] = 0
}

func rgb(color string) string {
	color = strings.TrimPrefix(strings.TrimSpace(color), "#")
	if len(color) == 6 {
		return "FF" + strings.ToUpper(color)
	}
	return ""
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	}
	return ""
}

func (e *xlsxExporter) fontID(font map[string]any) int {
	var xmlText strings.Builder
	xmlText.WriteString("<font>")
	if font["bold"] == true {
		xmlText.WriteString("<b/>")
	}
	if font["italic"] == true {
		xmlText.WriteString("<i/>")
	}
	if font["underline"] == true {
		xmlText.WriteString("<u/>")
	}
	size := asString(font["size"])
	if size == "" {
		size = "11"
	}
	xmlText.WriteString(`<sz val="` + escape(size) + `"/>`)
	if c := rgb(asString(font["color"])); c != "" {
		xmlText.WriteString(`<color rgb="` + c + `"/>`)
	}
	name := asString(font["name"])
	if name == "" {
		name = "Calibri"
	}
	xmlText.WriteString(`<name val="` + escape(name) + `"/></font>`)
	key := xmlText.String()
	if id, ok := e.fontIDs[key]; ok {
		return id
	}
	e.fonts = append(e.fonts, key)
	e.fontIDs[key] = len(e.fonts) - 1
	return len(e.fonts) - 1
}

func (e *xlsxExporter) fillID(fill map[string]any) int {
	color := rgb(asString(fill["color"]))
	pattern := asString(fill["pattern"])
	if color == "" || pattern == "none" {
		return 0
	}
	if pattern == "" {
		pattern = "solid"
	}
	key := `<fill><patternFill patternType="` + escape(pattern) + `"><fgColor rgb="` + color + `"/><bgColor indexed="64"/></patternFill></fill>`
	if id, ok := e.fillIDs[key]; ok {
		return id
	}
	e.fills = append(e.fills, key)
	e.fillIDs[key] = len(e.fills) - 1
	return len(e.fills) - 1
}

func (e *xlsxExporter) borderID(border map[string]any) int {
	edgeXML := func(name string) string {
		style, color := asString(border["style"]), asString(border["color"])
		if over, ok := border[name].(map[string]any); ok {
			if v := asString(over["style"]); v != "" {
				style = v
			}
			if v := asString(over["color"]); v != "" {
				color = v
			}
			if exact := asString(over["xlsxStyle"]); exact != "" {
				style = exact
			}
		}
		if style == "none" || (style == "" && color == "") {
			return "<" + name + "/>"
		}
		if style == "" {
			style = "thin"
		}
		colorXML := ""
		if c := rgb(color); c != "" {
			colorXML = `<color rgb="` + c + `"/>`
		}
		return "<" + name + ` style="` + escape(style) + `">` + colorXML + "</" + name + ">"
	}
	key := "<border>" + edgeXML("left") + edgeXML("right") + edgeXML("top") + edgeXML("bottom") + "<diagonal/></border>"
	if id, ok := e.borderIDs[key]; ok {
		return id
	}
	e.borders = append(e.borders, key)
	e.borderIDs[key] = len(e.borders) - 1
	return len(e.borders) - 1
}

func (e *xlsxExporter) numberFormatID(code string) int {
	if code == "" {
		return 0
	}
	if id, ok := builtinFormatIDs[code]; ok {
		return id
	}
	if id, ok := e.formats[code]; ok {
		return id
	}
	id := e.nextFormatID
	e.nextFormatID++
	e.formats[code] = id
	e.formatList = append(e.formatList, `<numFmt numFmtId="`+strconv.Itoa(id)+`" formatCode="`+escape(code)+`"/>`)
	return id
}

var knownStyleKeys = map[string]bool{"id": true, "numberFormat": true, "font": true, "fill": true, "border": true, "alignment": true, "protection": true}

// xfIndex returns the XLSX cell format index for a style id, adding it on first use. forceFormat, if
// set, is the number format to use when the style declares none (dates and times).
func (e *xlsxExporter) xfIndex(styleID, forceFormat string) int {
	if styleID == "" && forceFormat == "" {
		return 0
	}
	key := styleID + "\x00" + forceFormat
	if id, ok := e.xfIDs[key]; ok {
		return id
	}
	style := e.styleMaps[styleID]
	if styleID != "" && style == nil {
		e.warn("style", styleID, fmt.Sprintf("style %q is referenced but not defined", styleID))
	}
	for k := range style {
		if !knownStyleKeys[k] {
			e.warn("style", styleID, fmt.Sprintf("style property %q cannot be represented in XLSX", k))
		}
	}
	format, _ := style["numberFormat"].(string)
	if format == "" {
		format = forceFormat
	}
	font, _ := style["font"].(map[string]any)
	fill, _ := style["fill"].(map[string]any)
	border, _ := style["border"].(map[string]any)
	alignment, _ := style["alignment"].(map[string]any)
	protection, _ := style["protection"].(map[string]any)
	for k := range font {
		switch k {
		case "name", "size", "bold", "italic", "underline", "color":
		default:
			e.warn("style", styleID, fmt.Sprintf("font property %q cannot be represented in XLSX", k))
		}
	}
	var xf strings.Builder
	xf.WriteString(fmt.Sprintf(`<xf numFmtId="%d" fontId="%d" fillId="%d" borderId="%d" xfId="0" applyNumberFormat="1" applyFont="1" applyFill="1" applyBorder="1"`,
		e.numberFormatID(format), e.fontID(font), e.fillID(fill), e.borderID(border)))
	var inner strings.Builder
	if len(alignment) > 0 {
		var a strings.Builder
		for _, name := range []string{"horizontal", "vertical", "textRotation", "indent"} {
			if v := asString(alignment[name]); v != "" {
				a.WriteString(attr(name, v))
			}
		}
		if alignment["wrapText"] == true {
			a.WriteString(` wrapText="1"`)
		}
		if a.Len() > 0 {
			inner.WriteString("<alignment" + a.String() + "/>")
			xf.WriteString(` applyAlignment="1"`)
		}
	}
	if len(protection) > 0 {
		inner.WriteString(fmt.Sprintf(`<protection locked="%d" hidden="%d"/>`, boolInt(protection["locked"] != false), boolInt(protection["hidden"] == true)))
		xf.WriteString(` applyProtection="1"`)
	}
	if inner.Len() > 0 {
		xf.WriteString(">" + inner.String() + "</xf>")
	} else {
		xf.WriteString("/>")
	}
	e.xfs = append(e.xfs, xf.String())
	e.xfIDs[key] = len(e.xfs) - 1
	return len(e.xfs) - 1
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (e *xlsxExporter) stylesXML() []byte {
	var out strings.Builder
	out.WriteString(xmlHeader + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	if len(e.formatList) > 0 {
		out.WriteString(fmt.Sprintf(`<numFmts count="%d">%s</numFmts>`, len(e.formatList), strings.Join(e.formatList, "")))
	}
	out.WriteString(fmt.Sprintf(`<fonts count="%d">%s</fonts>`, len(e.fonts), strings.Join(e.fonts, "")))
	out.WriteString(fmt.Sprintf(`<fills count="%d">%s</fills>`, len(e.fills), strings.Join(e.fills, "")))
	out.WriteString(fmt.Sprintf(`<borders count="%d">%s</borders>`, len(e.borders), strings.Join(e.borders, "")))
	out.WriteString(`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>`)
	out.WriteString(fmt.Sprintf(`<cellXfs count="%d">%s</cellXfs>`, len(e.xfs), strings.Join(e.xfs, "")))
	out.WriteString(`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`)
	return []byte(out.String())
}

// ---------------------------------------------------------------------------------------------
// Values.

var errorLiterals = map[string]string{"DIV0": "#DIV/0!", "VALUE": "#VALUE!", "REF": "#REF!", "NAME": "#NAME?", "NUM": "#NUM!", "NULL": "#NULL!", "N/A": "#N/A"}

// xlsxErrorLiteral maps a CSVX error code (with or without a leading '#') to its XLSX literal. ok is
// false for a code XLSX has no equivalent for.
func xlsxErrorLiteral(code string) (string, bool) {
	code = strings.TrimPrefix(code, "#")
	if literal, ok := errorLiterals[code]; ok {
		return literal, true
	}
	for _, literal := range errorLiterals {
		if literal == "#"+code {
			return literal, true
		}
	}
	return "#VALUE!", false
}

// csvxErrorCode is the inverse, used when importing an XLSX error literal.
func csvxErrorCode(literal string) string {
	for code, l := range errorLiterals {
		if l == literal {
			return code
		}
	}
	return strings.TrimPrefix(literal, "#")
}

var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// excelSerial converts a CSVX date, time, or datetime literal to an XLSX serial number.
func excelSerial(kind, text string) (string, bool) {
	var days, fraction float64
	switch kind {
	case "date":
		t, err := time.Parse("2006-01-02", text)
		if err != nil {
			return "", false
		}
		days = math.Floor(t.Sub(excelEpoch).Hours() / 24)
	case "time":
		t, err := time.Parse("15:04:05", text)
		if err != nil {
			return "", false
		}
		fraction = float64(t.Hour()*3600+t.Minute()*60+t.Second()) / 86400
	default:
		t, err := time.Parse(time.RFC3339, text)
		if err != nil {
			if t, err = time.Parse("2006-01-02T15:04:05", text); err != nil {
				return "", false
			}
		}
		t = t.UTC()
		days = math.Floor(t.Sub(excelEpoch).Hours() / 24)
		fraction = float64(t.Hour()*3600+t.Minute()*60+t.Second()) / 86400
	}
	return strconv.FormatFloat(days+fraction, 'f', -1, 64), true
}

var dateTimeFormats = map[string]string{"date": "yyyy-mm-dd", "time": "hh:mm:ss", "datetime": "yyyy-mm-dd hh:mm:ss"}

func (e *xlsxExporter) sharedIndex(text string) int {
	if index, ok := e.shared[text]; ok {
		return index
	}
	e.sharedList = append(e.sharedList, text)
	e.shared[text] = len(e.sharedList) - 1
	return len(e.sharedList) - 1
}

func (e *xlsxExporter) sharedStringsXML() []byte {
	var out strings.Builder
	out.WriteString(xmlHeader + fmt.Sprintf(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="%d" uniqueCount="%d">`, len(e.sharedList), len(e.sharedList)))
	for _, text := range e.sharedList {
		out.WriteString(`<si><t xml:space="preserve">` + escape(text) + `</t></si>`)
	}
	out.WriteString("</sst>")
	return []byte(out.String())
}

// cellXML renders one cell. numeric reports whether a value was written.
func (e *xlsxExporter) valueXML(location string, value Value, raw string, format string) (typeAttr string, body string, forceFormat string) {
	switch value.Type {
	case "boolean":
		if b, _ := value.Value.(bool); b {
			return ` t="b"`, "<v>1</v>", ""
		}
		return ` t="b"`, "<v>0</v>", ""
	case "integer", "decimal":
		text := strings.TrimSpace(raw)
		if text == "" {
			text = stringOf(value)
		}
		return "", "<v>" + escape(text) + "</v>", ""
	case "string":
		return ` t="s"`, "<v>" + strconv.Itoa(e.sharedIndex(stringOf(value))) + "</v>", ""
	case "date", "time", "datetime":
		serial, ok := excelSerial(value.Type, stringOf(value))
		if !ok {
			e.warn("value", location, fmt.Sprintf("%s value %q is not a valid %s literal and was written as text", value.Type, stringOf(value), value.Type))
			return ` t="s"`, "<v>" + strconv.Itoa(e.sharedIndex(stringOf(value))) + "</v>", ""
		}
		lower := strings.ToLower(format)
		if !strings.ContainsAny(lower, "dyhs") {
			forceFormat = dateTimeFormats[value.Type]
		}
		return "", "<v>" + serial + "</v>", forceFormat
	case "error":
		literal, ok := xlsxErrorLiteral(value.Code)
		if !ok {
			e.warn("value", location, fmt.Sprintf("error code %q has no XLSX equivalent and was written as #VALUE!", value.Code))
		}
		return ` t="e"`, "<v>" + escape(literal) + "</v>", ""
	}
	return "", "", ""
}

// ---------------------------------------------------------------------------------------------
// Worksheets.

type xlsxRow struct {
	number int
	cells  []string
}

func (e *xlsxExporter) worksheet(sheet *Sheet, index int) ([]byte, error) {
	var out strings.Builder
	out.WriteString(xmlHeader + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	print := printMap(sheet)
	if _, fit := print["fitToWidth"]; fit {
		out.WriteString(`<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr>`)
	} else if _, fit := print["fitToHeight"]; fit {
		out.WriteString(`<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr>`)
	}
	var cols strings.Builder
	for column, c := range sheet.Columns {
		if c.Width > 0 {
			cols.WriteString(fmt.Sprintf(`<col min="%d" max="%d" width="%s" customWidth="1"/>`, column+1, column+1, strconv.FormatFloat(c.Width, 'f', -1, 64)))
		}
	}
	if cols.Len() > 0 {
		out.WriteString("<cols>" + cols.String() + "</cols>")
	}

	rows := map[int]*xlsxRow{}
	rowFor := func(n int) *xlsxRow {
		if rows[n] == nil {
			rows[n] = &xlsxRow{number: n}
		}
		return rows[n]
	}
	for column, c := range sheet.Columns {
		coordinate := CoordinateFor(column, HeaderRow)
		metadata := sheet.Cells[coordinate]
		style := ""
		if xf := e.xfIndex(metadata.Style, ""); xf > 0 {
			style = ` s="` + strconv.Itoa(xf) + `"`
		}
		rowFor(1).cells = append(rowFor(1).cells, `<c r="`+coordinate+`"`+style+` t="s"><v>`+strconv.Itoa(e.sharedIndex(c.Name))+`</v></c>`)
	}
	for r, record := range sheet.Records {
		for column := range sheet.Columns {
			raw := ""
			if column < len(record) {
				raw = record[column]
			}
			coordinate := CoordinateFor(column, r)
			if cell := e.cellXML(sheet, coordinate, column, raw); cell != "" {
				row := rowFor(r + 2)
				row.cells = append(row.cells, cell)
			}
		}
	}
	// Cells that exist only as metadata, beyond the CSV's records or columns (a formula below the
	// last data row, say), still belong in the worksheet.
	extra := make([]string, 0)
	for coordinate := range sheet.Cells {
		if column, row, ok := IndicesForCoordinate(coordinate); ok && row >= 0 && (row >= len(sheet.Records) || column >= len(sheet.Columns)) {
			extra = append(extra, coordinate)
		}
	}
	sort.Slice(extra, func(i, j int) bool {
		ci, ri, _ := IndicesForCoordinate(extra[i])
		cj, rj, _ := IndicesForCoordinate(extra[j])
		if ri != rj {
			return ri < rj
		}
		return ci < cj
	})
	for _, coordinate := range extra {
		column, row, _ := IndicesForCoordinate(coordinate)
		if cell := e.cellXML(sheet, coordinate, column, ""); cell != "" {
			rowFor(row + 2).cells = append(rowFor(row+2).cells, cell)
		}
	}
	for number := range sheet.RowHeights {
		rowFor(number)
	}
	numbers := make([]int, 0, len(rows))
	for n := range rows {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	out.WriteString("<sheetData>")
	for _, n := range numbers {
		row := rows[n]
		height := ""
		if h, ok := sheet.RowHeights[n]; ok && h > 0 {
			height = ` ht="` + strconv.FormatFloat(h, 'f', -1, 64) + `" customHeight="1"`
		}
		out.WriteString(fmt.Sprintf(`<row r="%d"%s>%s</row>`, n, height, strings.Join(row.cells, "")))
	}
	out.WriteString("</sheetData>")
	out.WriteString(e.dataValidations(sheet))
	out.WriteString(e.printXML(sheet.Name, print))
	out.WriteString("</worksheet>")
	return []byte(out.String()), nil
}

func (e *xlsxExporter) cellXML(sheet *Sheet, coordinate string, column int, raw string) string {
	metadata := sheet.Cells[coordinate]
	declared := metadata.Type
	if declared == "" && column < len(sheet.Columns) {
		declared = sheet.Columns[column].Type
	}
	format := ""
	if style := e.styleMaps[metadata.Style]; style != nil {
		format, _ = style["numberFormat"].(string)
	}
	location := sheet.Name + "!" + coordinate

	if metadata.Formula != "" {
		formula := escape(strings.TrimPrefix(metadata.Formula, "="))
		typeAttr, body, force := "", "", ""
		if metadata.Cached != nil {
			cached := *metadata.Cached
			switch cached.Type {
			case "string":
				typeAttr, body = ` t="str"`, "<v>"+escape(stringOf(cached))+"</v>"
			case "blank", "":
			default:
				typeAttr, body, force = e.valueXML(location, cached, "", format)
			}
		}
		style := ""
		if xf := e.xfIndex(metadata.Style, force); xf > 0 {
			style = ` s="` + strconv.Itoa(xf) + `"`
		}
		return `<c r="` + coordinate + `"` + style + typeAttr + `><f>` + formula + `</f>` + body + `</c>`
	}

	value := ResolveCellValue(raw, declared, format)
	typeAttr, body, force := e.valueXML(location, value, raw, format)
	xf := e.xfIndex(metadata.Style, force)
	if body == "" && xf == 0 {
		return ""
	}
	style := ""
	if xf > 0 {
		style = ` s="` + strconv.Itoa(xf) + `"`
	}
	return `<c r="` + coordinate + `"` + style + typeAttr + `>` + body + `</c>`
}

var validOperators = map[string]bool{"between": true, "notBetween": true, "equal": true, "notEqual": true, "greaterThan": true, "lessThan": true, "greaterThanOrEqual": true, "lessThanOrEqual": true}

func (e *xlsxExporter) dataValidations(sheet *Sheet) string {
	coordinates := make([]string, 0)
	for coordinate, metadata := range sheet.Cells {
		if len(metadata.Validation) > 0 {
			coordinates = append(coordinates, coordinate)
		}
	}
	if len(coordinates) == 0 {
		return ""
	}
	sort.Slice(coordinates, func(i, j int) bool {
		ci, ri, _ := IndicesForCoordinate(coordinates[i])
		cj, rj, _ := IndicesForCoordinate(coordinates[j])
		if ri != rj {
			return ri < rj
		}
		return ci < cj
	})
	var items []string
	for _, coordinate := range coordinates {
		var rule map[string]any
		if err := json.Unmarshal(sheet.Cells[coordinate].Validation, &rule); err != nil {
			e.warn("validation", sheet.Name+"!"+coordinate, "validation rule is not an object and was not exported")
			continue
		}
		kind, _ := rule["type"].(string)
		var item strings.Builder
		item.WriteString("<dataValidation")
		if kind != "" {
			item.WriteString(attr("type", kind))
		}
		if op, _ := rule["operator"].(string); validOperators[op] {
			item.WriteString(attr("operator", op))
		} else if op != "" {
			e.warn("validation", sheet.Name+"!"+coordinate, fmt.Sprintf("operator %q is not an XLSX operator and was not exported", op))
		}
		if rule["allowBlank"] == true {
			item.WriteString(` allowBlank="1"`)
		}
		if message, _ := rule["message"].(string); message != "" {
			item.WriteString(` showErrorMessage="1"` + attr("error", message))
		}
		item.WriteString(attr("sqref", coordinate) + ">")
		for i, key := range []string{"formula1", "formula2"} {
			text, _ := rule[key].(string)
			if text == "" {
				continue
			}
			if strings.HasPrefix(text, "=") {
				text = text[1:]
			} else if kind == "list" {
				text = `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
			}
			item.WriteString(fmt.Sprintf("<formula%d>%s</formula%d>", i+1, escape(text), i+1))
		}
		item.WriteString("</dataValidation>")
		items = append(items, item.String())
	}
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprintf(`<dataValidations count="%d">%s</dataValidations>`, len(items), strings.Join(items, ""))
}

var xlsxPaperCodes = map[string]string{"letter": "1", "tabloid": "3", "legal": "5", "a3": "8", "a4": "9", "a5": "11"}

var knownPrintKeys = map[string]bool{
	"orientation": true, "paperSize": true, "margins": true, "scale": true, "fitToWidth": true, "fitToHeight": true,
	"area": true, "repeatRows": true, "repeatColumns": true, "pageOrder": true, "gridlines": true,
	"centerHorizontally": true, "columnBreaks": true, "rowBreaks": true, "xlsxPaperSize": true, "xlsxPrintArea": true,
}

func (e *xlsxExporter) printXML(sheetName string, print map[string]any) string {
	keys := make([]string, 0, len(print))
	for key := range print {
		if !knownPrintKeys[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		e.warn("print", key, fmt.Sprintf("print property %q on sheet %q cannot be represented in XLSX and was not exported", key, sheetName))
	}
	var out strings.Builder
	gridlines, centered := print["gridlines"] == true, print["centerHorizontally"] == true
	if gridlines || centered {
		out.WriteString("<printOptions")
		if gridlines {
			out.WriteString(` gridLines="1"`)
		}
		if centered {
			out.WriteString(` horizontalCentered="1"`)
		}
		out.WriteString("/>")
	}
	margins := map[string]float64{"left": 0.7, "right": 0.7, "top": 0.75, "bottom": 0.75}
	if given, ok := print["margins"].(map[string]any); ok {
		for key := range margins {
			if v, ok := given[key].(float64); ok {
				margins[key] = v
			}
		}
	}
	format := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	out.WriteString(`<pageMargins left="` + format(margins["left"]) + `" right="` + format(margins["right"]) + `" top="` + format(margins["top"]) + `" bottom="` + format(margins["bottom"]) + `" header="0.3" footer="0.3"/>`)

	var setup strings.Builder
	if code, ok := xlsxPaperCodes[asString(print["paperSize"])]; ok {
		setup.WriteString(attr("paperSize", code))
	} else if extra := asString(print["xlsxPaperSize"]); extra != "" {
		setup.WriteString(attr("paperSize", extra))
	}
	if v, ok := print["scale"].(float64); ok {
		setup.WriteString(attr("scale", format(v)))
	}
	if v, ok := print["fitToWidth"].(float64); ok {
		setup.WriteString(attr("fitToWidth", format(v)))
	}
	if v, ok := print["fitToHeight"].(float64); ok {
		setup.WriteString(attr("fitToHeight", format(v)))
	}
	if v := asString(print["pageOrder"]); v != "" {
		setup.WriteString(attr("pageOrder", v))
	}
	if v := asString(print["orientation"]); v != "" {
		setup.WriteString(attr("orientation", v))
	}
	if setup.Len() > 0 {
		out.WriteString("<pageSetup" + setup.String() + "/>")
	}
	breaks := func(key, tag string, max int) {
		list, _ := print[key].([]any)
		if len(list) == 0 {
			return
		}
		var items strings.Builder
		for _, item := range list {
			if n, ok := item.(float64); ok {
				items.WriteString(fmt.Sprintf(`<brk id="%d" max="%d" man="1"/>`, int(n), max))
			}
		}
		out.WriteString(fmt.Sprintf(`<%s count="%d" manualBreakCount="%d">%s</%s>`, tag, len(list), len(list), items.String(), tag))
	}
	breaks("rowBreaks", "rowBreaks", 16383)
	breaks("columnBreaks", "colBreaks", 1048575)
	return out.String()
}

// ---------------------------------------------------------------------------------------------
// Package parts.

func (e *xlsxExporter) contentTypes(sheets int) []byte {
	var out strings.Builder
	out.WriteString(xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`)
	out.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	for i := 1; i <= sheets; i++ {
		out.WriteString(fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i))
	}
	out.WriteString(`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/><Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/></Types>`)
	return []byte(out.String())
}

func (e *xlsxExporter) workbookRels(sheets int) []byte {
	var out strings.Builder
	out.WriteString(xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= sheets; i++ {
		out.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i, i))
	}
	out.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, sheets+1))
	out.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/></Relationships>`, sheets+2))
	return []byte(out.String())
}

func absoluteRange(text string) string {
	m := cellRangePattern.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	if m[3] == "" {
		return "$" + m[1] + "$" + m[2]
	}
	return "$" + m[1] + "$" + m[2] + ":$" + m[3] + "$" + m[4]
}

func (e *xlsxExporter) workbookXML() []byte {
	var out strings.Builder
	out.WriteString(xmlHeader + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, sheet := range e.workbook.Sheets {
		out.WriteString(fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(sheet.Name), i+1, i+1))
	}
	out.WriteString("</sheets>")
	var names []string
	for _, item := range e.workbook.NamedRanges {
		names = append(names, `<definedName name="`+escape(item.Name)+`">`+escape(strings.TrimPrefix(item.RefersTo, "="))+`</definedName>`)
	}
	for i, sheet := range e.workbook.Sheets {
		print := printMap(sheet)
		quoted := FormatSheetName(sheet.Name)
		if extra := asString(print["xlsxPrintArea"]); extra != "" {
			names = append(names, fmt.Sprintf(`<definedName name="_xlnm.Print_Area" localSheetId="%d">%s</definedName>`, i, escape(extra)))
		} else if area := absoluteRange(asString(print["area"])); area != "" {
			names = append(names, fmt.Sprintf(`<definedName name="_xlnm.Print_Area" localSheetId="%d">%s!%s</definedName>`, i, escape(quoted), area))
		}
		var titles []string
		if rows := asString(print["repeatRows"]); rows != "" {
			parts := strings.SplitN(rows, ":", 2)
			if len(parts) == 2 {
				titles = append(titles, quoted+"!$"+parts[0]+":$"+parts[1])
			}
		}
		if columns := asString(print["repeatColumns"]); columns != "" {
			parts := strings.SplitN(columns, ":", 2)
			if len(parts) == 2 {
				titles = append(titles, quoted+"!$"+parts[0]+":$"+parts[1])
			}
		}
		if len(titles) > 0 {
			names = append(names, fmt.Sprintf(`<definedName name="_xlnm.Print_Titles" localSheetId="%d">%s</definedName>`, i, escape(strings.Join(titles, ","))))
		}
	}
	if len(names) > 0 {
		out.WriteString("<definedNames>" + strings.Join(names, "") + "</definedNames>")
	}
	out.WriteString(`<calcPr calcId="191029" fullCalcOnLoad="1"/></workbook>`)
	return []byte(out.String())
}
