package csvx

// Workbook-level recalculation: gathers each sheet's cells into the coordinate map RecalculateCells
// evaluates, then writes results back as each formula cell's cached value and its visible CSV text.
// Mirrors csvx-ts's recalculate.ts.

// CanonicalCellText converts a value back to the raw text the sheet CSV stores.
func CanonicalCellText(v Value) string {
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

func numberFormatFor(styles []Style, id string) string {
	if id == "" {
		return ""
	}
	for _, style := range styles {
		if style.Id == id && style.NumberFormat != nil {
			return *style.NumberFormat
		}
	}
	return ""
}

// BuildCellMap builds the flat coordinate -> {formula | value} map RecalculateCells expects for one
// sheet. Every cell gets an entry so a formula can read a plain cell, including a header cell.
func BuildCellMap(sheet *Sheet, styles []Style) CellMap {
	cells := CellMap{}
	for column := range sheet.Columns {
		cells[CoordinateFor(column, HeaderRow)] = FormulaCellInput{Value: ResolveCellValue(RawCellText(sheet, HeaderRow, column), "string", "")}
	}
	for row, record := range sheet.Records {
		for column, raw := range record {
			coordinate := CoordinateFor(column, row)
			metadata := sheet.Cells[coordinate]
			if metadata.Formula != "" {
				cells[coordinate] = FormulaCellInput{Formula: metadata.Formula}
				continue
			}
			declared := metadata.Type
			if declared == "" && column < len(sheet.Columns) {
				declared = sheet.Columns[column].Type
			}
			cells[coordinate] = FormulaCellInput{Value: ResolveCellValue(raw, declared, numberFormatFor(styles, metadata.Style))}
		}
	}
	return cells
}

// RecalculateWorkbook recalculates every formula cell in every sheet and returns a new workbook;
// the input is not modified. Cross-sheet references resolve against each other sheet's cell map by
// name.
func RecalculateWorkbook(workbook *Workbook) *Workbook {
	byName := map[string]CellMap{}
	for _, sheet := range workbook.Sheets {
		byName[sheet.Name] = BuildCellMap(sheet, workbook.Styles)
	}
	resultsByName := RecalculateSheets(byName, nil, workbook.NamedRanges)
	next := *workbook
	next.Sheets = make([]*Sheet, len(workbook.Sheets))
	for i, sheet := range workbook.Sheets {
		results := resultsByName[sheet.Name]
		copied := cloneSheet(sheet)
		for coordinate, value := range results {
			column, row, ok := IndicesForCoordinate(coordinate)
			if !ok || row < 0 || row >= len(copied.Records) || column >= len(copied.Records[row]) {
				continue
			}
			copied.Records[row][column] = CanonicalCellText(value)
			cached := value
			meta := copied.Cells[coordinate]
			meta.Cached = &cached
			if copied.Cells == nil {
				copied.Cells = map[string]CellMetadata{}
			}
			copied.Cells[coordinate] = meta
		}
		next.Sheets[i] = copied
	}
	return &next
}
