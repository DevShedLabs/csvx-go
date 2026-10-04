package csvx

import (
	"math"
	"regexp"
	"strconv"
)

// Print pagination and the used range, implementing csvx-spec/spec/03-sheets.md ("Used range" and
// "Pagination"). Both are pure functions of a sheet, its styles, and its print settings. Rows are A1
// row numbers (the header is row 1) and columns are letters. Mirrors csvx-ts's paginate.ts.

const (
	printDPI            = 96.0
	defaultColumnWidth  = 8.43
	defaultRowPoints    = 15.0
	fitEpsilon          = 1e-6
	defaultScalePercent = 100.0
)

// paperInches is each paper size in inches, portrait (width, height).
var paperInches = map[string][2]float64{
	"letter": {8.5, 11}, "legal": {8.5, 14}, "tabloid": {11, 17},
	"a3": {11.69, 16.54}, "a4": {8.27, 11.69}, "a5": {5.83, 8.27},
}

func hasVisibleEdge(style map[string]any) bool {
	border, _ := style["border"].(map[string]any)
	for _, edge := range []string{"top", "right", "bottom", "left"} {
		styleName, _ := border["style"].(string)
		color, _ := border["color"].(string)
		if over, ok := border[edge].(map[string]any); ok {
			if v, ok := over["style"].(string); ok {
				styleName = v
			}
			if v, ok := over["color"].(string); ok {
				color = v
			}
		}
		if (styleName != "" || color != "") && styleName != "none" {
			return true
		}
	}
	return false
}

// UsedRange returns the rows and columns up to the last cell that prints something
// (spec/03-sheets.md, "Used range"); it is always at least 1 x 1.
func UsedRange(sheet *Sheet, styles []Style) (rows, columns int) {
	byID := map[string]map[string]any{}
	for _, style := range styles {
		byID[style.Id] = styleToMap(style)
	}
	rows, columns = 1, 1
	for row := HeaderRow; row < len(sheet.Records); row++ {
		for column := range sheet.Columns {
			metadata := sheet.Cells[CoordinateFor(column, row)]
			prints := metadata.Formula != ""
			if style, ok := byID[metadata.Style]; ok && metadata.Style != "" {
				if fill, ok := style["fill"].(map[string]any); ok && fill["color"] != nil && fill["color"] != "" {
					prints = true
				}
				prints = prints || hasVisibleEdge(style)
			}
			if RawCellText(sheet, row, column) != "" || prints {
				rows = max(rows, RowNumberFor(row))
				columns = max(columns, column+1)
			}
		}
	}
	return rows, columns
}

// Page is one printed page.
type Page struct {
	Number  int      `json:"number"`
	Rows    []int    `json:"rows"`    // A1 row numbers shown, repeated rows first
	Columns []string `json:"columns"` // column letters shown, repeated columns first
}

// Pagination is the layout of a sheet's print area into pages.
type Pagination struct {
	PrintableWidth  float64 // unscaled CSS pixels inside the margins
	PrintableHeight float64
	Scale           float64
	Area            string // the printed area as an A1 range, or "" when nothing is left to print
	Pages           []Page
}

var (
	areaPattern          = regexp.MustCompile(`^([A-Z]+)([1-9][0-9]*):([A-Z]+)([1-9][0-9]*)$`)
	repeatRowsPattern    = regexp.MustCompile(`^([1-9][0-9]*):([1-9][0-9]*)$`)
	repeatColumnsPattern = regexp.MustCompile(`^([A-Z]+):([A-Z]+)$`)
)

func span(from, to int) []int {
	var out []int
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

type repeatSpec struct {
	end     int
	indices []int
}

func repeatOf(text string, rows bool) *repeatSpec {
	var a, b int
	if rows {
		m := repeatRowsPattern.FindStringSubmatch(text)
		if m == nil {
			return nil
		}
		a, _ = strconv.Atoi(m[1])
		b, _ = strconv.Atoi(m[2])
		a, b = a-1, b-1
	} else {
		m := repeatColumnsPattern.FindStringSubmatch(text)
		if m == nil {
			return nil
		}
		a, b = columnIndexFromID(m[1]), columnIndexFromID(m[2])
	}
	return &repeatSpec{end: max(a, b), indices: span(min(a, b), max(a, b))}
}

func sumSizes(indices []int, size func(int) float64) float64 {
	total := 0.0
	for _, index := range indices {
		total += size(index)
	}
	return total
}

// splitIntoGroups splits indices into groups that fit limit, honoring forced breaks (1-based
// "break after item n") and prepending the repeated items to every group that does not start at or
// before them. A group always holds at least one item.
func splitIntoGroups(indices []int, sizeOf func(int) float64, limit float64, breaksAfter map[int]bool, repeat *repeatSpec) [][]int {
	repeatSize := 0.0
	if repeat != nil {
		repeatSize = sumSizes(repeat.indices, sizeOf)
	}
	overheadFor := func(first int) float64 {
		if repeat != nil && first > repeat.end {
			return repeatSize
		}
		return 0
	}
	var groups [][]int
	var current []int
	used := 0.0
	for _, index := range indices {
		size := sizeOf(index)
		if len(current) == 0 {
			used = overheadFor(index)
		}
		if len(current) > 0 && used+size > limit+fitEpsilon {
			groups = append(groups, current)
			current = nil
			used = overheadFor(index)
		}
		current = append(current, index)
		used += size
		if breaksAfter[index+1] {
			groups = append(groups, current)
			current = nil
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}

func intSet(values any) map[int]bool {
	set := map[int]bool{}
	if list, ok := values.([]any); ok {
		for _, v := range list {
			if n, ok := v.(float64); ok {
				set[int(n)] = true
			}
		}
	}
	return set
}

// Paginate lays a sheet's print area out as pages, exactly as spec/03-sheets.md ("Pagination")
// describes.
func Paginate(sheet *Sheet, styles []Style) Pagination {
	print := printMap(sheet)
	if print == nil {
		print = map[string]any{}
	}
	text := func(key, fallback string) string {
		if v, ok := print[key].(string); ok {
			return v
		}
		return fallback
	}
	number := func(key string) (float64, bool) { v, ok := print[key].(float64); return v, ok }

	paper, ok := paperInches[text("paperSize", "letter")]
	if !ok {
		paper = paperInches["letter"]
	}
	paperW, paperH := paper[0], paper[1]
	if text("orientation", "portrait") == "landscape" {
		paperW, paperH = paperH, paperW
	}
	margins := map[string]float64{"top": 0.75, "right": 0.7, "bottom": 0.75, "left": 0.7}
	if given, ok := print["margins"].(map[string]any); ok {
		for key := range margins {
			if v, ok := given[key].(float64); ok {
				margins[key] = v
			}
		}
	}
	result := Pagination{
		PrintableWidth:  math.Max(1, (paperW-margins["left"]-margins["right"])*printDPI),
		PrintableHeight: math.Max(1, (paperH-margins["top"]-margins["bottom"])*printDPI),
	}

	columnPx := func(index int) float64 {
		width := defaultColumnWidth
		if index < len(sheet.Columns) && sheet.Columns[index].Width > 0 {
			width = sheet.Columns[index].Width
		}
		return float64(ColumnWidthToPixels(width))
	}
	rowPx := func(index int) float64 {
		points := defaultRowPoints
		if h, ok := sheet.RowHeights[index+1]; ok && h > 0 {
			points = h
		}
		return math.Round(points * 4 / 3)
	}

	var r0, r1, c0, c1 int
	if m := areaPattern.FindStringSubmatch(text("area", "")); m != nil {
		ra, _ := strconv.Atoi(m[2])
		rb, _ := strconv.Atoi(m[4])
		ca, cb := columnIndexFromID(m[1]), columnIndexFromID(m[3])
		r0, r1 = min(ra, rb)-1, min(max(ra, rb)-1, len(sheet.Records))
		c0, c1 = min(ca, cb), min(max(ca, cb), len(sheet.Columns)-1)
	} else {
		rows, columns := UsedRange(sheet, styles)
		r0, r1, c0, c1 = 0, rows-1, 0, columns-1
	}
	rowIndices, columnIndices := span(r0, r1), span(c0, c1)
	if len(rowIndices) > 0 && len(columnIndices) > 0 {
		result.Area = columnID(c0) + strconv.Itoa(r0+1) + ":" + columnID(c1) + strconv.Itoa(r1+1)
	}

	repeatRows, repeatColumns := repeatOf(text("repeatRows", ""), true), repeatOf(text("repeatColumns", ""), false)
	fitWidth, hasFitWidth := number("fitToWidth")
	fitHeight, hasFitHeight := number("fitToHeight")
	percent, ok := number("scale")
	if !ok {
		percent = defaultScalePercent
	}
	scale := percent / 100
	if hasFitWidth || hasFitHeight {
		scale = 1
		if hasFitWidth && fitWidth > 0 {
			repeatW := 0.0
			if repeatColumns != nil {
				repeatW = sumSizes(repeatColumns.indices, columnPx)
			}
			scale = math.Min(scale, fitWidth*result.PrintableWidth/(sumSizes(columnIndices, columnPx)+(fitWidth-1)*repeatW))
		}
		if hasFitHeight && fitHeight > 0 {
			repeatH := 0.0
			if repeatRows != nil {
				repeatH = sumSizes(repeatRows.indices, rowPx)
			}
			scale = math.Min(scale, fitHeight*result.PrintableHeight/(sumSizes(rowIndices, rowPx)+(fitHeight-1)*repeatH))
		}
	}
	result.Scale = math.Min(4, math.Max(0.1, scale))

	columnGroups := splitIntoGroups(columnIndices, columnPx, result.PrintableWidth/result.Scale, intSet(print["columnBreaks"]), repeatColumns)
	rowGroups := splitIntoGroups(rowIndices, rowPx, result.PrintableHeight/result.Scale, intSet(print["rowBreaks"]), repeatRows)
	withRepeat := func(group []int, repeat *repeatSpec) []int {
		if repeat != nil && group[0] > repeat.end {
			return append(append([]int(nil), repeat.indices...), group...)
		}
		return group
	}

	across := text("pageOrder", "downThenOver") == "overThenDown"
	outer, inner := columnGroups, rowGroups
	if across {
		outer, inner = rowGroups, columnGroups
	}
	for _, outerGroup := range outer {
		for _, innerGroup := range inner {
			columnGroup, rowGroup := outerGroup, innerGroup
			if across {
				columnGroup, rowGroup = innerGroup, outerGroup
			}
			page := Page{Number: len(result.Pages) + 1}
			for _, row := range withRepeat(rowGroup, repeatRows) {
				page.Rows = append(page.Rows, row+1)
			}
			for _, column := range withRepeat(columnGroup, repeatColumns) {
				page.Columns = append(page.Columns, columnID(column))
			}
			result.Pages = append(result.Pages, page)
		}
	}
	return result
}
