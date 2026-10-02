package csvx

import (
	"time"
)

// literalType classifies text by the canonical literal forms of csvx-spec/spec/04-data-types.md
// ("Literal forms"): exact, case-sensitive, no trimming, no locale handling. It returns "blank",
// "boolean", "integer", "decimal", "date", "time", "datetime", or "string". It is the one
// classifier shared by untyped cell resolution and CSV import inference.
func literalType(text string) string {
	switch {
	case text == "":
		return "blank"
	case text == "true" || text == "false":
		return "boolean"
	case isIntegerLiteral(text):
		return "integer"
	case isDecimalLiteral(text):
		return "decimal"
	}
	if len(text) == 10 && text[4] == '-' {
		if _, err := time.Parse("2006-01-02", text); err == nil {
			return "date"
		}
	}
	if len(text) == 8 && text[2] == ':' {
		if _, err := time.Parse("15:04:05", text); err == nil {
			return "time"
		}
	}
	if len(text) > 10 && text[10] == 'T' {
		if _, err := time.Parse(time.RFC3339, text); err == nil {
			return "datetime"
		}
	}
	return "string"
}

func digitsFrom(text string, start int) (end int) {
	end = start
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	return end
}

// isIntegerLiteral matches -?(0|[1-9][0-9]*), excluding "-0".
func isIntegerLiteral(text string) bool {
	start := 0
	if len(text) > 0 && text[0] == '-' {
		start = 1
	}
	end := digitsFrom(text, start)
	if end != len(text) || end == start {
		return false
	}
	if text[start] == '0' {
		return end-start == 1 && start == 0
	}
	return true
}

// isDecimalLiteral matches -?(0|[1-9][0-9]*)\.[0-9]+.
func isDecimalLiteral(text string) bool {
	start := 0
	if len(text) > 0 && text[0] == '-' {
		start = 1
	}
	whole := digitsFrom(text, start)
	if whole == start || whole >= len(text)-1 || text[whole] != '.' {
		return false
	}
	if text[start] == '0' && whole-start != 1 {
		return false
	}
	return digitsFrom(text, whole+1) == len(text)
}
