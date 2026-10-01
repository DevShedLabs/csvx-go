package csvx

import (
	"math"
	"strconv"
	"strings"
)

// ResolveCellValue resolves a sheet CSV cell's raw text to a typed Value, per
// csvx-spec/spec/05-cell-values.md and 03-sheets.md: "Column types provide defaults and
// validation hints; an individual cell MAY override a column type." This is the one place that
// decision gets made — consumer apps must call this rather than sniffing "looks like a number"
// themselves (csvx-spec/AGENTS.md rule 1). declaredType is the resolved cell/column type (a
// cell's own Type override wins over its column's); pass "" when nothing declares a type at all.
//
// When nothing declares a type (true for any hand-authored or freshly-edited CSVX, as opposed to
// an exhaustively cell-annotated XLSX import), this falls back to the same narrow, well-established
// literal-shape inference every CSV-consuming spreadsheet tool uses (blank/boolean/integer/decimal
// by shape, otherwise string) rather than defaulting everything untyped to "string" and silently
// breaking formula arithmetic over it. A declared type (including an explicit "string") always
// wins and is never second-guessed. Mirrors csvx-ts's resolveCellValue — see that implementation
// for the TypeScript engine's identical contract.
func ResolveCellValue(raw string, declaredType string) Value {
	if raw == "" {
		return Value{Type: "blank"}
	}
	if declaredType != "" {
		switch declaredType {
		case "blank":
			return Value{Type: "blank"}
		case "boolean":
			return Value{Type: "boolean", Value: strings.EqualFold(strings.TrimSpace(raw), "true")}
		case "integer":
			// Matches csvx-ts's resolveCellValue, which coerces with Number() then checks
			// Number.isInteger — "3.0" with a declared integer type is integer 3, not a VALUE
			// error, even though it isn't a plain integer literal. Cross-engine behavior must
			// agree here; a stricter ParseInt-only check would silently diverge from the TS engine.
			if number, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && !math.IsInf(number, 0) && number == math.Trunc(number) {
				return Value{Type: "integer", Value: int64(number)}
			}
			return Value{Type: "error", Code: "VALUE"}
		case "decimal":
			if _, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
				return Value{Type: "decimal", Value: raw}
			}
			return Value{Type: "error", Code: "VALUE"}
		case "date", "time", "datetime":
			return Value{Type: declaredType, Value: raw}
		case "error":
			return Value{Type: "error", Code: raw}
		default:
			return Value{Type: "string", Value: raw}
		}
	}
	trimmed := strings.TrimSpace(raw)
	if strings.EqualFold(trimmed, "true") || strings.EqualFold(trimmed, "false") {
		return Value{Type: "boolean", Value: strings.EqualFold(trimmed, "true")}
	}
	if number, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return Value{Type: "integer", Value: number}
	}
	if isPlainDecimalLiteral(trimmed) {
		return Value{Type: "decimal", Value: trimmed}
	}
	return Value{Type: "string", Value: raw}
}

// isPlainDecimalLiteral matches the same shape csvx-ts's resolveCellValue requires for decimal
// inference: an optional sign, one or more digits, a literal ".", and one or more digits — not
// Go's broader ParseFloat grammar (which also accepts exponents, "Inf", "NaN", etc.), since those
// aren't what a person typing "19.95" into a cell means.
func isPlainDecimalLiteral(text string) bool {
	rest := text
	if len(rest) > 0 && (rest[0] == '+' || rest[0] == '-') {
		rest = rest[1:]
	}
	dot := strings.IndexByte(rest, '.')
	if dot <= 0 || dot == len(rest)-1 {
		return false
	}
	for i, r := range rest {
		if i == dot {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// NextCellMetadata computes the next cell metadata after an edit overwrites a cell's content, per
// spec/05-cell-values.md: Type, Formula, and Cached describe a cell's *content* and must never
// survive past what they described, while Style and Validation describe the cell itself and are
// untouched. Consumer apps must call this rather than deciding for themselves which fields survive
// an edit (AGENTS.md rule 5.2 — no second opinion about what a CSVX value/type means). Pass a
// non-empty formula when the new content is a formula (starts with "="); pass "" for a literal
// edit. The second return value is false when nothing is left worth keeping (no metadata entry
// needed at all).
//
// Note: CellMetadata in this engine is a closed struct (Type/Formula/Cached/Style/Validation only)
// rather than an open map, so unlike csvx-ts's nextCellMetadata this cannot yet preserve a field
// neither engine recognizes — that's an existing gap in this type (see its own doc comment),
// not one this function introduces.
func NextCellMetadata(existing CellMetadata, formula string) (CellMetadata, bool) {
	next := CellMetadata{Style: existing.Style, Validation: existing.Validation}
	if formula != "" {
		next.Formula = formula
		return next, true
	}
	if next.Style == "" && len(next.Validation) == 0 {
		return CellMetadata{}, false
	}
	return next, true
}
