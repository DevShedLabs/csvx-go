package csvx

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Presentation-only number formatting, per csvx-spec/spec/08-styles.md: "Number formats MUST NOT
// alter the underlying value. A renderer MAY fall back to a readable default when a format is
// unsupported." This implements the same documented, honest subset of Excel-style format codes as
// csvx-ts's format.ts (the real fixture in csvx-spec/examples/styled.csvx/styles.json uses
// "$#,##0.00") and falls back to the plain value string for anything outside that subset — exactly
// the fallback the spec allows, rather than guessing at the rest of Excel's much larger format-code
// grammar. Every engine implementing this MUST agree on the same subset; see format_test.go, which
// mirrors csvx-ts's format.test.ts case for case.
//
// Supported patterns: "0", "0.00", "#,##0", "#,##0.00", any of those prefixed with a literal
// currency-ish symbol run (e.g. "$#,##0.00", "€#,##0", or Excel's quoted-literal form `"$"#,##0.00`
// — real XLSX-imported data uses the quoted form), and "0%"/"0.00%".

var numberFormatPattern = regexp.MustCompile(`^([^\d#]*)([#0](?:[#0,]*[#0])?(?:\.[0#]+)?)(%?)$`)
var quotedLiteralPattern = regexp.MustCompile(`"([^"]*)"`)

type parsedNumberFormat struct {
	prefix        string
	hasGrouping   bool
	decimalPlaces int
	isPercent     bool
}

// unquoteLiterals strips Excel's `"literal text"` quoting from a format code, since the quotes
// themselves are never displayed — e.g. `"$"#,##0.00` means a literal "$" prefix, not a prefix
// containing quotes.
func unquoteLiterals(numberFormat string) string {
	return quotedLiteralPattern.ReplaceAllString(numberFormat, "$1")
}

// parseNumberFormatCode parses a numberFormat code into the shape both FormatValue (forward:
// value -> display text) and ParseFormattedLiteral (reverse: typed text -> value) need. Returns
// false for anything outside the documented supported subset — see this file's package comment.
func parseNumberFormatCode(numberFormat string) (parsedNumberFormat, bool) {
	match := numberFormatPattern.FindStringSubmatch(unquoteLiterals(numberFormat))
	if match == nil {
		return parsedNumberFormat{}, false
	}
	prefix, digits, percentSign := match[1], match[2], match[3]
	decimalPlaces := 0
	if dot := strings.IndexByte(digits, '.'); dot != -1 {
		decimalPlaces = len(digits) - dot - 1
	}
	return parsedNumberFormat{
		prefix:        prefix,
		hasGrouping:   strings.Contains(digits, ","),
		decimalPlaces: decimalPlaces,
		isPercent:     percentSign == "%",
	}, true
}

func numericValueOf(value Value) (float64, bool) {
	if value.Type != "integer" && value.Type != "decimal" {
		return 0, false
	}
	switch v := value.Value.(type) {
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case float64:
		return v, true
	case string:
		number, err := strconv.ParseFloat(v, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func plainString(value Value) string {
	switch value.Type {
	case "blank":
		return ""
	case "error":
		if value.Message != "" {
			return fmt.Sprintf("#%s: %s", value.Code, value.Message)
		}
		return "#" + value.Code
	case "boolean":
		if b, ok := value.Value.(bool); ok && b {
			return "TRUE"
		}
		return "FALSE"
	default:
		if value.Value == nil {
			return ""
		}
		return fmt.Sprintf("%v", value.Value)
	}
}

func groupThousands(integerPart string) string {
	n := len(integerPart)
	if n <= 3 {
		return integerPart
	}
	var out strings.Builder
	lead := n % 3
	if lead > 0 {
		out.WriteString(integerPart[:lead])
	}
	for i := lead; i < n; i += 3 {
		if out.Len() > 0 {
			out.WriteByte(',')
		}
		out.WriteString(integerPart[i : i+3])
	}
	return out.String()
}

// FormatValue renders a Value for display using an Excel-style numberFormat code, falling back to
// the plain value string when the value isn't numeric or the pattern isn't one of the supported
// shapes. Never mutates or reinterprets value — this is display text only. Mirrors csvx-ts's
// formatValue exactly; both engines must agree on every case in format_test.go.
func FormatValue(value Value, numberFormat string) string {
	if numberFormat == "" {
		return plainString(value)
	}
	number, ok := numericValueOf(value)
	if !ok {
		return plainString(value)
	}
	parsed, ok := parseNumberFormatCode(numberFormat)
	if !ok {
		return plainString(value)
	}
	scaled := number
	if parsed.isPercent {
		scaled *= 100
	}
	fixed := strconv.FormatFloat(math.Abs(scaled), 'f', parsed.decimalPlaces, 64)
	integerPart, fractionPart, hasFraction := strings.Cut(fixed, ".")
	groupedInteger := integerPart
	if parsed.hasGrouping {
		groupedInteger = groupThousands(integerPart)
	}
	sign := ""
	if scaled < 0 {
		sign = "-"
	}
	body := groupedInteger
	if hasFraction {
		body = groupedInteger + "." + fractionPart
	}
	percentSuffix := ""
	if parsed.isPercent {
		percentSuffix = "%"
	}
	return sign + parsed.prefix + body + percentSuffix
}

var plainNumberBodyPattern = regexp.MustCompile(`^-?\d+(?:\.\d+)?$`)

// ParseFormattedLiteral is the inverse of FormatValue: it attempts to parse typed literal text back
// into a numeric Value using a cell's own numberFormat, per spec/08-styles.md's symmetric allowance
// — e.g. "$7.00" against `"$"#,##0.00` becomes decimal "7.00" instead of falling through to string
// just because it has a currency symbol. Scoped strictly to the same documented format subset
// FormatValue supports; this is not general multi-locale currency parsing (see the package comment
// for why). Returns (Value{}, false) for text that doesn't match the format's own affix/grouping
// shape, or when there is no numberFormat at all — the caller falls through to its own generic
// literal rules either way. Mirrors csvx-ts's parseFormattedLiteral.
func ParseFormattedLiteral(text string, numberFormat string) (Value, bool) {
	if numberFormat == "" {
		return Value{}, false
	}
	parsed, ok := parseNumberFormatCode(numberFormat)
	if !ok {
		return Value{}, false
	}

	body := strings.TrimSpace(text)
	if parsed.prefix != "" {
		if !strings.HasPrefix(body, parsed.prefix) {
			return Value{}, false
		}
		body = body[len(parsed.prefix):]
	}
	isPercentLiteral := false
	if parsed.isPercent {
		if !strings.HasSuffix(body, "%") {
			return Value{}, false
		}
		body = strings.TrimSuffix(body, "%")
		isPercentLiteral = true
	}
	if parsed.hasGrouping {
		body = strings.ReplaceAll(body, ",", "")
	}
	if !plainNumberBodyPattern.MatchString(body) {
		return Value{}, false
	}

	if isPercentLiteral {
		number, err := strconv.ParseFloat(body, 64)
		if err != nil {
			return Value{}, false
		}
		return Value{Type: "decimal", Value: strconv.FormatFloat(number/100, 'f', -1, 64)}, true
	}
	if !strings.Contains(body, ".") {
		number, err := strconv.ParseInt(body, 10, 64)
		if err != nil {
			return Value{}, false
		}
		return Value{Type: "integer", Value: number}, true
	}
	return Value{Type: "decimal", Value: body}, true
}
