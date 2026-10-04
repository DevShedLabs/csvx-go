package csvx

import (
	"strconv"
	"strings"
)

// Row and coordinate conventions, defined by csvx-spec/spec/03-sheets.md: the CSV header is row 1
// and data records start at row 2, everywhere A1 coordinates are used. A row is addressed here by
// its record index, so the header row is HeaderRow (-1) and record i is row i + 2. Mirrors
// csvx-ts's coordinates.ts.

// HeaderRow is the record index of the CSV header row (spec row 1).
const HeaderRow = -1

// RowNumberFor returns the spec row number (1-based, header = 1) for a record index.
func RowNumberFor(rowIndex int) int { return rowIndex + 2 }

// RowIndexFor returns the record index for a spec row number; row 1 is HeaderRow.
func RowIndexFor(rowNumber int) int { return rowNumber - 2 }

// CoordinateFor returns the A1 coordinate of a cell, e.g. CoordinateFor(0, HeaderRow) is "A1".
func CoordinateFor(columnIndex, rowIndex int) string {
	return columnID(columnIndex) + strconv.Itoa(RowNumberFor(rowIndex))
}

// columnIndexFromID is the inverse of columnID ("A" -> 0, "AA" -> 26); case-insensitive.
func columnIndexFromID(id string) int {
	index := 0
	for _, c := range strings.ToUpper(id) {
		index = index*26 + int(c-'A') + 1
	}
	return index - 1
}

// IndicesForCoordinate splits "AB12" into a zero-based column and record row index ("A1" has row
// HeaderRow). ok is false for an unparseable coordinate.
func IndicesForCoordinate(coordinate string) (column, row int, ok bool) {
	i := 0
	for i < len(coordinate) && coordinate[i] >= 'A' && coordinate[i] <= 'Z' {
		i++
	}
	if i == 0 || i == len(coordinate) || coordinate[i] == '0' {
		return 0, 0, false
	}
	number, err := strconv.Atoi(coordinate[i:])
	if err != nil || number < 1 {
		return 0, 0, false
	}
	return columnIndexFromID(coordinate[:i]), RowIndexFor(number), true
}

// RawCellText is a cell's raw text. For the header row that is the column name, except that a name
// equal to the column's own letter is the placeholder an importer writes for an empty header cell
// (spec/14-xlsx-interoperability.md) and reads as blank.
func RawCellText(sheet *Sheet, row, column int) string {
	if row < 0 {
		if column >= len(sheet.Columns) {
			return ""
		}
		name := sheet.Columns[column].Name
		if name == columnID(column) {
			return ""
		}
		return name
	}
	if row >= len(sheet.Records) || column >= len(sheet.Records[row]) {
		return ""
	}
	return sheet.Records[row][column]
}
