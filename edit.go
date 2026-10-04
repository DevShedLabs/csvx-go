package csvx

// Workbook edit operations, implementing csvx-spec/spec/15-edit-operations.md. Mirrors csvx-ts's
// edit.ts. Every function returns a new Workbook and never mutates its input. An invalid operation
// returns *InvalidEditError and changes nothing. By default each operation recalculates the
// workbook afterwards (spec/10-calculation.md); set EditOptions.SkipRecalculate to get the model as
// the operation left it, which is what the conformance vectors compare.
//
// Schema validation is deliberately not done here (csvx-spec/AGENTS.md rule 3.3): validate the
// saved package with the canonical validator instead.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// InvalidEditError reports an operation the spec says is invalid.
type InvalidEditError struct{ Message string }

func (e *InvalidEditError) Error() string { return e.Message }

func invalidEdit(format string, args ...any) error {
	return &InvalidEditError{Message: fmt.Sprintf(format, args...)}
}

// EditOptions tunes an edit operation. The zero value recalculates afterwards.
type EditOptions struct {
	SkipRecalculate bool
}

func finish(workbook *Workbook, options EditOptions) *Workbook {
	if options.SkipRecalculate {
		return workbook
	}
	return RecalculateWorkbook(workbook)
}

func cloneSheet(sheet *Sheet) *Sheet {
	copied := *sheet
	copied.Columns = append([]Column(nil), sheet.Columns...)
	copied.Records = make([][]string, len(sheet.Records))
	for i, row := range sheet.Records {
		copied.Records[i] = append([]string(nil), row...)
	}
	if sheet.Cells != nil {
		copied.Cells = make(map[string]CellMetadata, len(sheet.Cells))
		for k, v := range sheet.Cells {
			copied.Cells[k] = v
		}
	}
	if sheet.RowHeights != nil {
		copied.RowHeights = make(map[int]float64, len(sheet.RowHeights))
		for k, v := range sheet.RowHeights {
			copied.RowHeights[k] = v
		}
	}
	return &copied
}

func cloneWorkbook(workbook *Workbook) *Workbook {
	copied := *workbook
	copied.Sheets = make([]*Sheet, len(workbook.Sheets))
	for i, sheet := range workbook.Sheets {
		copied.Sheets[i] = cloneSheet(sheet)
	}
	copied.Styles = append([]Style(nil), workbook.Styles...)
	copied.NamedRanges = append([]NamedRange(nil), workbook.NamedRanges...)
	return &copied
}

func sheetIndex(workbook *Workbook, ref string) (int, error) {
	for i, sheet := range workbook.Sheets {
		if sheet.ID == ref {
			return i, nil
		}
	}
	for i, sheet := range workbook.Sheets {
		if sheet.Name == ref {
			return i, nil
		}
	}
	return 0, invalidEdit("No such sheet: %s", ref)
}

var coordinatePattern = regexp.MustCompile(`^([A-Z]+)([1-9][0-9]*)$`)

func parseCoordinate(coordinate string) (column, rowNumber int, err error) {
	m := coordinatePattern.FindStringSubmatch(coordinate)
	if m == nil {
		return 0, 0, invalidEdit("Invalid coordinate: %s", coordinate)
	}
	rowNumber, _ = strconv.Atoi(m[2])
	return columnIndexFromID(m[1]), rowNumber, nil
}

// rewriteCellFormulas rewrites every formula a cell's metadata carries: its own Formula, and a
// validation rule's formula1/formula2 when they begin with "=" (spec/15, "Reference rewriting").
func rewriteCellFormulas(metadata CellMetadata, rewrite func(string) string) CellMetadata {
	if metadata.Formula != "" {
		metadata.Formula = rewrite(metadata.Formula)
	}
	if len(metadata.Validation) == 0 {
		return metadata
	}
	var rule map[string]json.RawMessage
	if err := json.Unmarshal(metadata.Validation, &rule); err != nil {
		return metadata
	}
	changed := false
	for _, key := range []string{"formula1", "formula2"} {
		var text string
		if raw, ok := rule[key]; ok && json.Unmarshal(raw, &text) == nil && strings.HasPrefix(text, "=") {
			if body, err := json.Marshal(rewrite(text)); err == nil {
				rule[key], changed = body, true
			}
		}
	}
	if changed {
		if body, err := json.Marshal(rule); err == nil {
			metadata.Validation = body
		}
	}
	return metadata
}

// noHomeSheet stands in for the sheet of a name's refersTo: every reference in it is
// sheet-qualified, so none targets "its own" sheet.
const noHomeSheet = "\x00"

func rewriteNamedRanges(workbook *Workbook, rewrite func(string) string) {
	for i := range workbook.NamedRanges {
		workbook.NamedRanges[i].RefersTo = rewrite(workbook.NamedRanges[i].RefersTo)
	}
}

// ---------------------------------------------------------------------------------------------
// Structural edits: insert/delete rows and columns.

// applyAxisEdit applies one axis edit to the whole workbook: it rewrites every formula on every
// sheet, then, for the target sheet only, moves cell metadata and row heights and applies reshape.
func applyAxisEdit(workbook *Workbook, target int, edit AxisEdit, reshape func(*Sheet)) *Workbook {
	next := cloneWorkbook(workbook)
	targetName := workbook.Sheets[target].Name
	for index, sheet := range next.Sheets {
		rewritten := map[string]CellMetadata{}
		for coordinate, metadata := range sheet.Cells {
			metadata = rewriteCellFormulas(metadata, func(f string) string { return RewriteFormulaForAxisEdit(f, sheet.Name, targetName, edit) })
			if index != target {
				rewritten[coordinate] = metadata
				continue
			}
			column, rowNumber, _ := parseCoordinate(coordinate)
			key := rowNumber
			if edit.Axis == "column" {
				key = column
			}
			if edit.Deleted[key] {
				continue
			}
			moved := edit.Map(key)
			if edit.Axis == "row" {
				rewritten[columnID(column)+strconv.Itoa(moved)] = metadata
			} else {
				rewritten[columnID(moved)+strconv.Itoa(rowNumber)] = metadata
			}
		}
		if sheet.Cells != nil || index == target {
			sheet.Cells = rewritten
		}
		if index != target {
			continue
		}
		if edit.Axis == "row" && sheet.RowHeights != nil {
			heights := map[int]float64{}
			for row, height := range sheet.RowHeights {
				if !edit.Deleted[row] {
					heights[edit.Map(row)] = height
				}
			}
			sheet.RowHeights = heights
		}
		rewritePrint(sheet, targetName, edit)
		reshape(sheet)
	}
	rewriteNamedRanges(next, func(f string) string { return RewriteFormulaForAxisEdit(f, noHomeSheet, targetName, edit) })
	return next
}

// printMap returns a sheet's print settings as a generic JSON object (including unknown
// properties), or nil.
func printMap(sheet *Sheet) map[string]any {
	if sheet.Print == nil {
		return nil
	}
	body, _ := json.Marshal(sheet.Print)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return out
}

func setPrintMap(sheet *Sheet, values map[string]any, removeEmpty bool) error {
	if len(values) == 0 && removeEmpty {
		sheet.Print = nil
		return nil
	}
	body, err := json.Marshal(values)
	if err != nil {
		return err
	}
	var print PrintSettings
	if err := json.Unmarshal(body, &print); err != nil {
		return err
	}
	sheet.Print = &print
	return nil
}

var (
	printRowRange    = regexp.MustCompile(`^(\d+):(\d+)$`)
	printColumnRange = regexp.MustCompile(`^([A-Z]+):([A-Z]+)$`)
)

func rewritePrint(sheet *Sheet, name string, edit AxisEdit) {
	print := printMap(sheet)
	if print == nil {
		return
	}
	rewriteRange := func(text string) (string, bool) {
		out := RewriteFormulaForAxisEdit("="+text, name, name, edit)
		if strings.Contains(out, "#REF!") {
			return "", false
		}
		return out[1:], true
	}
	if area, ok := print["area"].(string); ok {
		if next, ok := rewriteRange(area); ok {
			print["area"] = next
		} else {
			delete(print, "area")
		}
	}
	if edit.Axis == "row" {
		if text, ok := print["repeatRows"].(string); ok {
			if m := printRowRange.FindStringSubmatch(text); m != nil {
				if next, ok := rewriteRange("A" + m[1] + ":A" + m[2]); ok {
					parts := strings.SplitN(next, ":", 2)
					print["repeatRows"] = strings.TrimPrefix(parts[0], "A") + ":" + strings.TrimPrefix(parts[1], "A")
				} else {
					delete(print, "repeatRows")
				}
			}
		}
	} else if text, ok := print["repeatColumns"].(string); ok {
		if m := printColumnRange.FindStringSubmatch(text); m != nil {
			if next, ok := rewriteRange(m[1] + "1:" + m[2] + "1"); ok {
				parts := strings.SplitN(next, ":", 2)
				print["repeatColumns"] = strings.TrimRight(parts[0], "0123456789") + ":" + strings.TrimRight(parts[1], "0123456789")
			} else {
				delete(print, "repeatColumns")
			}
		}
	}
	key, offset := "rowBreaks", 0
	if edit.Axis == "column" {
		key, offset = "columnBreaks", 1 // column breaks are 1-based; the edit works 0-based
	}
	if breaks, ok := print[key].([]any); ok {
		var kept []any
		for _, item := range breaks {
			n, _ := item.(float64)
			if !edit.Deleted[int(n)-offset] {
				kept = append(kept, float64(edit.Map(int(n)-offset)+offset))
			}
		}
		if len(kept) == 0 {
			delete(print, key)
		} else {
			print[key] = kept
		}
	}
	_ = setPrintMap(sheet, print, false)
}

func assertCount(count int) error {
	if count < 1 {
		return invalidEdit("count must be an integer of at least 1")
	}
	return nil
}

// InsertRows inserts count blank rows before row number at (an A1 row number, at least 2).
func InsertRows(workbook *Workbook, sheet string, at, count int, options EditOptions) (*Workbook, error) {
	if err := assertCount(count); err != nil {
		return nil, err
	}
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	current := workbook.Sheets[target]
	if at < 2 || at > len(current.Records)+2 {
		return nil, invalidEdit("Cannot insert before row %d", at)
	}
	edit := AxisEdit{Axis: "row", Deleted: map[int]bool{}, Map: func(row int) int {
		if row >= at {
			return row + count
		}
		return row
	}}
	next := applyAxisEdit(workbook, target, edit, func(s *Sheet) {
		records := append([][]string(nil), s.Records[:at-2]...)
		for i := 0; i < count; i++ {
			records = append(records, make([]string, len(s.Columns)))
		}
		s.Records = append(records, s.Records[at-2:]...)
	})
	return finish(next, options), nil
}

// DeleteRows deletes the given rows (A1 row numbers, each at least 2) in one pass.
func DeleteRows(workbook *Workbook, sheet string, rows []int, options EditOptions) (*Workbook, error) {
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	current := workbook.Sheets[target]
	doomed := map[int]bool{}
	var sorted []int
	for _, row := range rows {
		if row < 2 || row > len(current.Records)+1 {
			return nil, invalidEdit("Cannot delete row %d", row)
		}
		if !doomed[row] {
			doomed[row] = true
			sorted = append(sorted, row)
		}
	}
	sort.Ints(sorted)
	edit := AxisEdit{Axis: "row", Deleted: doomed, Map: func(row int) int { return row - countBelow(sorted, row) }}
	next := applyAxisEdit(workbook, target, edit, func(s *Sheet) {
		var records [][]string
		for i, record := range s.Records {
			if !doomed[i+2] {
				records = append(records, record)
			}
		}
		if records == nil {
			records = [][]string{}
		}
		s.Records = records
	})
	return finish(next, options), nil
}

func countBelow(sorted []int, value int) int {
	return sort.SearchInts(sorted, value)
}

func columnIndexArg(letter string) (int, error) {
	if !regexp.MustCompile(`^[A-Z]+$`).MatchString(letter) {
		return 0, invalidEdit("Invalid column: %s", letter)
	}
	return columnIndexFromID(letter), nil
}

func renumberColumns(columns []Column) {
	for i := range columns {
		columns[i].ID = columnID(i)
	}
}

// InsertColumns inserts count blank columns before the column with letter at. The new columns are
// named "Column N", N being the 1-based position at creation (spec/15).
func InsertColumns(workbook *Workbook, sheet, at string, count int, options EditOptions) (*Workbook, error) {
	if err := assertCount(count); err != nil {
		return nil, err
	}
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	index, err := columnIndexArg(at)
	if err != nil {
		return nil, err
	}
	if index > len(workbook.Sheets[target].Columns) {
		return nil, invalidEdit("Cannot insert before column %s", at)
	}
	edit := AxisEdit{Axis: "column", Deleted: map[int]bool{}, Map: func(column int) int {
		if column >= index {
			return column + count
		}
		return column
	}}
	next := applyAxisEdit(workbook, target, edit, func(s *Sheet) {
		columns := append([]Column(nil), s.Columns[:index]...)
		for i := 0; i < count; i++ {
			columns = append(columns, Column{Name: "Column " + strconv.Itoa(index+i+1)})
		}
		s.Columns = append(columns, s.Columns[index:]...)
		renumberColumns(s.Columns)
		for r, row := range s.Records {
			grown := append([]string(nil), row[:index]...)
			grown = append(grown, make([]string, count)...)
			s.Records[r] = append(grown, row[index:]...)
		}
	})
	return finish(next, options), nil
}

// DeleteColumns deletes the columns with the given letters in one pass; a sheet keeps at least one
// column.
func DeleteColumns(workbook *Workbook, sheet string, columns []string, options EditOptions) (*Workbook, error) {
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	current := workbook.Sheets[target]
	doomed := map[int]bool{}
	var sorted []int
	for _, letter := range columns {
		index, err := columnIndexArg(letter)
		if err != nil {
			return nil, err
		}
		if index >= len(current.Columns) {
			return nil, invalidEdit("Cannot delete column %s", letter)
		}
		if !doomed[index] {
			doomed[index] = true
			sorted = append(sorted, index)
		}
	}
	if len(doomed) >= len(current.Columns) {
		return nil, invalidEdit("A sheet must keep at least one column")
	}
	sort.Ints(sorted)
	edit := AxisEdit{Axis: "column", Deleted: doomed, Map: func(column int) int { return column - countBelow(sorted, column) }}
	next := applyAxisEdit(workbook, target, edit, func(s *Sheet) {
		var kept []Column
		for i, column := range s.Columns {
			if !doomed[i] {
				kept = append(kept, column)
			}
		}
		s.Columns = kept
		renumberColumns(s.Columns)
		for r, row := range s.Records {
			var keptRow []string
			for i, field := range row {
				if !doomed[i] {
					keptRow = append(keptRow, field)
				}
			}
			s.Records[r] = keptRow
		}
	})
	return finish(next, options), nil
}

// ---------------------------------------------------------------------------------------------
// Sheets.

// AddSheet appends an empty sheet: one column named "Column 1" and no data rows.
func AddSheet(workbook *Workbook, options EditOptions) *Workbook {
	ids, names := map[string]bool{}, map[string]bool{}
	for _, sheet := range workbook.Sheets {
		ids[sheet.ID], names[sheet.Name] = true, true
	}
	n := len(workbook.Sheets) + 1
	for ids[fmt.Sprintf("sheet-%d", n)] || names[fmt.Sprintf("Sheet %d", n)] {
		n++
	}
	id := fmt.Sprintf("sheet-%d", n)
	next := cloneWorkbook(workbook)
	next.Sheets = append(next.Sheets, &Sheet{
		ID: id, Name: fmt.Sprintf("Sheet %d", n), Path: "sheets/" + id + ".csv",
		Columns: []Column{{ID: "A", Name: "Column 1"}}, Records: [][]string{}, Cells: map[string]CellMetadata{},
	})
	return finish(next, options)
}

func rewriteAllFormulas(workbook *Workbook, rewrite func(string) string) {
	for _, sheet := range workbook.Sheets {
		for coordinate, metadata := range sheet.Cells {
			sheet.Cells[coordinate] = rewriteCellFormulas(metadata, rewrite)
		}
	}
	rewriteNamedRanges(workbook, rewrite)
}

// RenameSheet renames a sheet and rewrites every sheet-qualified reference to it.
func RenameSheet(workbook *Workbook, sheet, name string, options EditOptions) (*Workbook, error) {
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	if name == "" || len([]rune(name)) > 255 || strings.ContainsAny(name, ":\x7f") || strings.IndexFunc(name, func(r rune) bool { return r < 0x20 }) >= 0 {
		return nil, invalidEdit("Invalid sheet name: %q", name)
	}
	for i, other := range workbook.Sheets {
		if i != target && other.Name == name {
			return nil, invalidEdit("Sheet name already in use: %s", name)
		}
	}
	old := workbook.Sheets[target].Name
	next := cloneWorkbook(workbook)
	next.Sheets[target].Name = name
	rewriteAllFormulas(next, func(f string) string { return RewriteFormulaForSheetChange(f, old, name, false) })
	return finish(next, options), nil
}

// DeleteSheet deletes a sheet; references to it become #REF!. A workbook keeps at least one sheet.
func DeleteSheet(workbook *Workbook, sheet string, options EditOptions) (*Workbook, error) {
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	if len(workbook.Sheets) <= 1 {
		return nil, invalidEdit("A workbook must keep at least one sheet")
	}
	removed := workbook.Sheets[target].Name
	next := cloneWorkbook(workbook)
	next.Sheets = append(next.Sheets[:target], next.Sheets[target+1:]...)
	rewriteAllFormulas(next, func(f string) string { return RewriteFormulaForSheetChange(f, removed, "", true) })
	return finish(next, options), nil
}

// ---------------------------------------------------------------------------------------------
// Cells.

func extendSheet(sheet *Sheet, rowNumber, column int) {
	for len(sheet.Columns) <= column {
		index := len(sheet.Columns)
		sheet.Columns = append(sheet.Columns, Column{ID: columnID(index), Name: "Column " + strconv.Itoa(index+1)})
		for r := range sheet.Records {
			sheet.Records[r] = append(sheet.Records[r], "")
		}
	}
	for len(sheet.Records) < rowNumber-1 {
		sheet.Records = append(sheet.Records, make([]string, len(sheet.Columns)))
	}
}

func setCellOn(sheet *Sheet, styles []Style, coordinate, text string) error {
	column, rowNumber, err := parseCoordinate(coordinate)
	if err != nil {
		return err
	}
	if rowNumber == 1 && text == "" {
		return invalidEdit("A column name must not be empty")
	}
	extendSheet(sheet, rowNumber, column)
	isFormula := strings.HasPrefix(text, "=")
	formula := ""
	if isFormula {
		formula = text
	}
	if sheet.Cells == nil {
		sheet.Cells = map[string]CellMetadata{}
	}
	metadata, keep := NextCellMetadata(sheet.Cells[coordinate], formula)
	if keep {
		sheet.Cells[coordinate] = metadata
	} else {
		delete(sheet.Cells, coordinate)
	}
	if rowNumber == 1 {
		sheet.Columns[column].Name = text
		return nil
	}
	record := sheet.Records[rowNumber-2]
	if isFormula {
		record[column] = ""
		return nil
	}
	record[column] = text
	if sheet.Columns[column].Type == "" {
		if formatted, ok := ParseFormattedLiteral(text, numberFormatFor(styles, metadata.Style)); ok && (formatted.Type == "integer" || formatted.Type == "decimal") {
			record[column] = CanonicalCellText(formatted)
		}
	}
	return nil
}

// SetCell replaces a cell's content with what the user typed (a formula if it starts with "=").
func SetCell(workbook *Workbook, sheet, coordinate, text string, options EditOptions) (*Workbook, error) {
	return Paste(workbook, sheet, coordinate, [][]string{{text}}, options)
}

// Paste applies a rectangle of texts, top-left at anchor, as one SetCell each. It is atomic: if any
// element is invalid, nothing is applied. Formula text is stored verbatim.
func Paste(workbook *Workbook, sheet, anchor string, rows [][]string, options EditOptions) (*Workbook, error) {
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	startColumn, startRow, err := parseCoordinate(anchor)
	if err != nil {
		return nil, err
	}
	next := cloneWorkbook(workbook)
	for r, row := range rows {
		for c, text := range row {
			if err := setCellOn(next.Sheets[target], next.Styles, columnID(startColumn+c)+strconv.Itoa(startRow+r), text); err != nil {
				return nil, err
			}
		}
	}
	return finish(next, options), nil
}

// ---------------------------------------------------------------------------------------------
// Styles.

func styleToMap(style Style) map[string]any {
	body, _ := json.Marshal(style)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return out
}

func canonicalJSON(value any) string {
	body, _ := json.Marshal(value) // map keys are marshalled in sorted order
	return string(body)
}

func withoutNulls(group map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range group {
		if v != nil {
			out[k] = v
		}
	}
	return out
}

func mergeStyle(base, patch map[string]any) map[string]any {
	merged := map[string]any{}
	for k, v := range base {
		merged[k] = v
	}
	for key, value := range patch {
		if value == nil {
			delete(merged, key)
			continue
		}
		if group, ok := value.(map[string]any); ok && key != "numberFormat" {
			combined := map[string]any{}
			if existing, ok := base[key].(map[string]any); ok {
				for k, v := range existing {
					combined[k] = v
				}
			}
			for k, v := range group {
				combined[k] = v
			}
			merged[key] = withoutNulls(combined)
			continue
		}
		merged[key] = value
	}
	for key, value := range merged {
		if group, ok := value.(map[string]any); ok && len(group) == 0 {
			delete(merged, key)
		}
	}
	return merged
}

var styleIDPattern = regexp.MustCompile(`^s(\d+)$`)

// ApplyStyle merges patch into each listed cell's style, reusing an identical existing style or
// adding one (s<N>). Existing styles are never modified.
func ApplyStyle(workbook *Workbook, sheet string, coordinates []string, patch map[string]any, options EditOptions) (*Workbook, error) {
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	next := cloneWorkbook(workbook)
	current := next.Sheets[target]
	if current.Cells == nil {
		current.Cells = map[string]CellMetadata{}
	}
	maps := make([]map[string]any, len(next.Styles))
	bySignature := map[string]string{}
	nextID := 0
	for i, style := range next.Styles {
		maps[i] = styleToMap(style)
		props := map[string]any{}
		for k, v := range maps[i] {
			if k != "id" {
				props[k] = v
			}
		}
		if _, seen := bySignature[canonicalJSON(props)]; !seen {
			bySignature[canonicalJSON(props)] = style.Id
		}
		if m := styleIDPattern.FindStringSubmatch(style.Id); m != nil {
			if n, _ := strconv.Atoi(m[1]); n >= nextID {
				nextID = n + 1
			}
		}
	}
	for _, coordinate := range coordinates {
		if _, _, err := parseCoordinate(coordinate); err != nil {
			return nil, err
		}
		existing := current.Cells[coordinate]
		base := map[string]any{}
		for i, style := range next.Styles {
			if style.Id == existing.Style && existing.Style != "" {
				for k, v := range maps[i] {
					if k != "id" {
						base[k] = v
					}
				}
			}
		}
		merged := mergeStyle(base, patch)
		if len(merged) == 0 {
			if existing.Style != "" {
				existing.Style = ""
				if existing.Type == "" && existing.Formula == "" && existing.Cached == nil && len(existing.Validation) == 0 {
					delete(current.Cells, coordinate)
				} else {
					current.Cells[coordinate] = existing
				}
			}
			continue
		}
		id, found := bySignature[canonicalJSON(merged)]
		if !found {
			id = "s" + strconv.Itoa(nextID)
			nextID++
			withID := map[string]any{"id": id}
			for k, v := range merged {
				withID[k] = v
			}
			body, _ := json.Marshal(withID)
			var style Style
			if err := json.Unmarshal(body, &style); err != nil {
				return nil, err
			}
			next.Styles = append(next.Styles, style)
			maps = append(maps, withID)
			bySignature[canonicalJSON(merged)] = id
		}
		existing.Style = id
		current.Cells[coordinate] = existing
	}
	return finish(next, options), nil
}

// ClearStyle removes the style reference from each listed cell; styles are untouched.
func ClearStyle(workbook *Workbook, sheet string, coordinates []string, options EditOptions) (*Workbook, error) {
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	next := cloneWorkbook(workbook)
	current := next.Sheets[target]
	for _, coordinate := range coordinates {
		if _, _, err := parseCoordinate(coordinate); err != nil {
			return nil, err
		}
		existing, ok := current.Cells[coordinate]
		if !ok {
			continue
		}
		existing.Style = ""
		if existing.Type == "" && existing.Formula == "" && existing.Cached == nil && len(existing.Validation) == 0 {
			delete(current.Cells, coordinate)
		} else {
			current.Cells[coordinate] = existing
		}
	}
	return finish(next, options), nil
}

// ---------------------------------------------------------------------------------------------
// Print settings.

// SetPrint merges patch into the sheet's print settings: a key set to nil is removed, unmentioned
// and unknown keys are kept, and an emptied object is removed.
func SetPrint(workbook *Workbook, sheet string, patch map[string]any, options EditOptions) (*Workbook, error) {
	target, err := sheetIndex(workbook, sheet)
	if err != nil {
		return nil, err
	}
	next := cloneWorkbook(workbook)
	print := printMap(next.Sheets[target])
	if print == nil {
		print = map[string]any{}
	}
	for key, value := range patch {
		if value == nil {
			delete(print, key)
		} else {
			print[key] = value
		}
	}
	if err := setPrintMap(next.Sheets[target], print, true); err != nil {
		return nil, err
	}
	return finish(next, options), nil
}
