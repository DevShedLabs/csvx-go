package csvx

import "testing"

func TestResolveCellValueInference(t *testing.T) {
	cases := []struct {
		raw      string
		declared string
		format   string
		want     Value
	}{
		{"", "", "", Value{Type: "blank"}},
		{"42", "", "", Value{Type: "integer", Value: int64(42)}},
		{"19.95", "", "", Value{Type: "decimal", Value: "19.95"}},
		{"true", "", "", Value{Type: "boolean", Value: true}},
		{"hello", "", "", Value{Type: "string", Value: "hello"}},
		{"1", "string", "", Value{Type: "string", Value: "1"}},
		{"3.0", "integer", "", Value{Type: "integer", Value: int64(3)}},
		{"not a number", "integer", "", Value{Type: "error", Code: "VALUE"}},
		// Currency-literal parsing against the cell's own numberFormat — mirrors csvx-ts's
		// resolveCellValue currency test cases exactly (cross-engine parity).
		{"$7.00", "", `"$"#,##0.00`, Value{Type: "decimal", Value: "7.00"}},
		{"$1,234.56", "", `"$"#,##0.00`, Value{Type: "decimal", Value: "1234.56"}},
		{"$7", "", `"$"#,##0.00`, Value{Type: "integer", Value: int64(7)}},
		{"not money", "", `"$"#,##0.00`, Value{Type: "string", Value: "not money"}},
		{"42", "", `"$"#,##0.00`, Value{Type: "integer", Value: int64(42)}},
		{"$7.00", "string", `"$"#,##0.00`, Value{Type: "string", Value: "$7.00"}},
	}
	for _, c := range cases {
		got := ResolveCellValue(c.raw, c.declared, c.format)
		if got.Type != c.want.Type || got.Value != c.want.Value || got.Code != c.want.Code {
			t.Errorf("ResolveCellValue(%q, %q, %q) = %+v, want %+v", c.raw, c.declared, c.format, got, c.want)
		}
	}
}

func TestNextCellMetadata(t *testing.T) {
	existing := CellMetadata{Type: "blank", Style: "s4"}
	next, ok := NextCellMetadata(existing, "")
	if !ok || next.Type != "" || next.Style != "s4" {
		t.Errorf("literal edit: got %+v, ok=%v", next, ok)
	}

	_, ok = NextCellMetadata(CellMetadata{Type: "blank"}, "")
	if ok {
		t.Errorf("expected no metadata left when only a stale type existed")
	}

	formulaExisting := CellMetadata{Formula: "=A1", Cached: &Value{Type: "integer", Value: int64(1)}, Type: "integer", Style: "s1"}
	next, ok = NextCellMetadata(formulaExisting, "=B1+1")
	if !ok || next.Formula != "=B1+1" || next.Cached != nil || next.Type != "" || next.Style != "s1" {
		t.Errorf("formula edit: got %+v, ok=%v", next, ok)
	}
}
