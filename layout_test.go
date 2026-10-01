package csvx

import "testing"

// Mirrors csvx-ts's layout.test.ts case for case — both engines must agree on this formula.
func TestColumnWidthPixels(t *testing.T) {
	if got := ColumnWidthToPixels(26.25); got != 189 {
		t.Errorf("ColumnWidthToPixels(26.25) = %d, want 189", got)
	}
	if got := PixelsToColumnWidth(189); got < 26.2 || got > 26.3 {
		t.Errorf("PixelsToColumnWidth(189) = %v, want ~26.29", got)
	}
	if got := ColumnWidthToPixels(PixelsToColumnWidth(150)); got != 150 {
		t.Errorf("round-trip ColumnWidthToPixels(PixelsToColumnWidth(150)) = %d, want 150", got)
	}
}

func TestRowHeightPixels(t *testing.T) {
	if got := RowHeightToPixels(15); got != 20 {
		t.Errorf("RowHeightToPixels(15) = %d, want 20", got)
	}
	if got := PixelsToRowHeight(20); got < 14.9 || got > 15.1 {
		t.Errorf("PixelsToRowHeight(20) = %v, want ~15", got)
	}
	if got := RowHeightToPixels(PixelsToRowHeight(28)); got != 28 {
		t.Errorf("round-trip RowHeightToPixels(PixelsToRowHeight(28)) = %d, want 28", got)
	}
}
