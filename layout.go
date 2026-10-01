package csvx

import "math"

// Canonical unit conversions for Column.width and Sheet.rowHeights, per spec/03-sheets.md. Mirrors
// csvx-ts's layout.ts exactly — both engines must agree on this formula; see that file's comment
// for the full rationale (both fields have a real, specific unit — XLSX character-width units and
// points, respectively — not pixels, because that's what every real producer already emits).

func roundTo(value float64, places int) float64 {
	factor := math.Pow(10, float64(places))
	return math.Round(value*factor) / factor
}

// ColumnWidthToPixels converts an XLSX character-width unit (Column.width) to CSS pixels.
func ColumnWidthToPixels(width float64) int {
	return int(math.Round(width*7 + 5))
}

// PixelsToColumnWidth converts CSS pixels back to an XLSX character-width unit — the inverse of
// ColumnWidthToPixels. Rounded to 2 decimal places, matching csvx-ts.
func PixelsToColumnWidth(pixels float64) float64 {
	return roundTo((pixels-5)/7, 2)
}

// RowHeightToPixels converts a row height in points (Sheet.rowHeights) to CSS pixels, at the
// standard 96 DPI / 72-points-per-inch ratio every renderer (and XLSX itself) assumes.
func RowHeightToPixels(points float64) int {
	return int(math.Round(points * 4 / 3))
}

// PixelsToRowHeight converts CSS pixels back to points — the inverse of RowHeightToPixels.
func PixelsToRowHeight(pixels float64) float64 {
	return roundTo(pixels*3/4, 2)
}
