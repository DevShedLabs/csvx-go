package csvx

import (
	"fmt"
	"regexp"
	"strings"
)

// threeDReference matches `Sheet1:Sheet3!A1`, quoted or bare, the form XLSX uses for a range across
// sheets. It also catches `B2:Other!B10`, which CSVX rejects for the same reason (spec 06).
var threeDReference = regexp.MustCompile(`(?:'(?:[^']|'')+'|[A-Za-z0-9_.]+):(?:'(?:[^']|'')+'|[A-Za-z0-9_.]+)!`)

// stringLiteral matches a double-quoted formula string, whose text is never a reference.
var stringLiteral = regexp.MustCompile(`"(?:[^"]|"")*"`)

// unsupportedReference names why a formula uses a reference Core 1.0 does not have (spec 14.11), or
// returns "" when it does not.
func unsupportedReference(formula string) string {
	code := stringLiteral.ReplaceAllString(formula, `""`)
	switch {
	case strings.Contains(code, "["):
		return "reference into another workbook"
	case threeDReference.MatchString(code):
		return "three-dimensional reference across sheets"
	}
	return ""
}

// importXLSXFormula converts the text of an XLSX `<f>` into a CSVX formula (spec 14.11). Each sheet
// qualifier is written as spec 06 requires of a writer: bare only when it may be. A reference CSVX
// has no meaning for (a range across sheets, a reference into another workbook) is not guessed at:
// the formula is dropped, so the cell keeps its cached result as its value, and a warning says so.
// Any other text is kept as it is.
func importXLSXFormula(sheet, ref, source string) (string, *XLSXDiagnostic) {
	formula := formulaValue(source)
	if _, err := ParseFormula(formula); err != nil {
		if reason := unsupportedReference(formula); reason != "" {
			return "", &XLSXDiagnostic{
				Severity: "warning", Feature: "formula", Path: sheet + "!" + ref,
				Message: fmt.Sprintf("formula %s in %s!%s not imported: %s; the cached result is kept as the value", formula, sheet, ref, reason),
			}
		}
		return formula, nil
	}
	var reps []replacement
	for _, span := range scanReferences(formula) {
		if span.hasSheet {
			reps = append(reps, replacement{span.sheetStart, span.start, FormatSheetName(span.sheet) + "!"})
		}
	}
	return applyReplacements(formula, reps), nil
}
