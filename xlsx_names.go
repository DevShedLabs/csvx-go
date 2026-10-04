package csvx

import (
	"fmt"
	"strings"
)

// importXLSXDefinedNames maps workbook-scoped XLSX defined names onto namedRanges, per
// csvx-spec/spec/14-xlsx-interoperability.md 14.8. A name is imported when it has no localSheetId,
// its formula is a single sheet-qualified cell or range reference or a constant, and it satisfies
// the rules in spec/02-workbook.md. Print names are print settings (14.6), not names. Every other
// name is reported as a warning rather than silently dropped.
func importXLSXDefinedNames(defined []xlsxDefinedName) ([]NamedRange, []XLSXDiagnostic) {
	var names []NamedRange
	var warnings []XLSXDiagnostic
	skip := func(name xlsxDefinedName, reason string) {
		warnings = append(warnings, XLSXDiagnostic{Severity: "warning", Feature: "definedName", Path: name.Name, Message: fmt.Sprintf("defined name %q not imported: %s", name.Name, reason)})
	}
	for _, item := range defined {
		switch {
		case item.Name == "_xlnm.Print_Area" || item.Name == "_xlnm.Print_Titles":
			continue
		case strings.HasPrefix(item.Name, "_xlnm."):
			skip(item, "built-in name with no CSVX equivalent")
			continue
		case item.LocalSheetID != "":
			skip(item, "sheet-scoped names are not part of Core 1.0")
			continue
		}
		refersTo, reason := definedNameRefersTo(item.Value)
		if reason != "" {
			skip(item, reason)
			continue
		}
		candidate := NamedRange{Name: item.Name, RefersTo: refersTo}
		if problems := ValidateNamedRanges(append(append([]NamedRange(nil), names...), candidate)); len(problems) > 0 {
			skip(item, problems[len(problems)-1].Message)
			continue
		}
		names = append(names, candidate)
	}
	return names, warnings
}

// definedNameRefersTo converts an XLSX defined-name formula into a refersTo expression, or returns
// the reason it cannot be one.
func definedNameRefersTo(value string) (string, string) {
	formula := "=" + strings.TrimSpace(value)
	ast, err := ParseFormula(formula)
	if err != nil {
		return "", "formula does not parse: " + err.Error()
	}
	switch ast.Kind {
	case NodeReference, NodeRange, NodeNumber, NodeString, NodeBoolean:
	case NodeUnary:
		if ast.Operand.Kind != NodeNumber {
			return "", "formula is not a single reference or constant"
		}
	default:
		return "", "formula is not a single reference or constant"
	}
	// XLSX always quotes sheet names; write each qualifier the way spec/06 requires.
	var reps []replacement
	for _, span := range scanReferences(formula) {
		if span.hasSheet {
			reps = append(reps, replacement{span.sheetStart, span.start, FormatSheetName(span.sheet) + "!"})
		}
	}
	return applyReplacements(formula, reps), ""
}
