package csvx

import "testing"

// Mirrors csvx-ts's format behavior case for case — both engines must agree on every row here.
func TestFormatValue(t *testing.T) {
	cases := []struct {
		value  Value
		format string
		want   string
	}{
		{Value{Type: "decimal", Value: "97000"}, `"$"#,##0.00`, "$97,000.00"},
		{Value{Type: "decimal", Value: "1923.076923"}, `"$"#,##0.00`, "$1,923.08"},
		{Value{Type: "integer", Value: int64(9)}, `"$"#,##0.00`, "$9.00"},
		{Value{Type: "decimal", Value: "0.5"}, "0.00%", "50.00%"},
		{Value{Type: "string", Value: "hello"}, `"$"#,##0.00`, "hello"},
		{Value{Type: "blank"}, `"$"#,##0.00`, ""},
		{Value{Type: "integer", Value: int64(42)}, "", "42"},
		{Value{Type: "decimal", Value: "-1234.5"}, "#,##0.00", "-1,234.50"},
	}
	for _, c := range cases {
		got := FormatValue(c.value, c.format)
		if got != c.want {
			t.Errorf("FormatValue(%+v, %q) = %q, want %q", c.value, c.format, got, c.want)
		}
	}
}

func TestParseFormattedLiteral(t *testing.T) {
	cases := []struct {
		text   string
		format string
		want   Value
		wantOK bool
	}{
		{"$7.00", `"$"#,##0.00`, Value{Type: "decimal", Value: "7.00"}, true},
		{"$1,234.56", `"$"#,##0.00`, Value{Type: "decimal", Value: "1234.56"}, true},
		{"$7", `"$"#,##0.00`, Value{Type: "integer", Value: int64(7)}, true},
		{"not money", `"$"#,##0.00`, Value{}, false},
		{"7.00", "", Value{}, false},
		{"50%", "0%", Value{Type: "decimal", Value: "0.5"}, true},
	}
	for _, c := range cases {
		got, ok := ParseFormattedLiteral(c.text, c.format)
		if ok != c.wantOK || (ok && (got.Type != c.want.Type || got.Value != c.want.Value)) {
			t.Errorf("ParseFormattedLiteral(%q, %q) = %+v, %v; want %+v, %v", c.text, c.format, got, ok, c.want, c.wantOK)
		}
	}
}
