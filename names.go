package csvx

import (
	"fmt"
	"regexp"
	"strings"
)

// Validity rules for workbook names that JSON Schema cannot express (csvx-spec/spec/02-workbook.md,
// Named ranges). Shape (patterns, required fields) is the canonical validator's job and is not
// repeated here (csvx-spec/AGENTS.md rule 3.3); this only checks the cross-field and semantic
// rules. Mirrors csvx-ts's names.ts.

// NamedRangeDiagnostic is one violation of the named-range rules.
type NamedRangeDiagnostic struct {
	Code    string
	Name    string
	Message string
}

// InvalidNamedRangeError is returned when a package declares invalid names.
type InvalidNamedRangeError struct{ Diagnostics []NamedRangeDiagnostic }

func (e *InvalidNamedRangeError) Error() string {
	parts := make([]string, len(e.Diagnostics))
	for i, d := range e.Diagnostics {
		parts[i] = d.Name + ": " + d.Message
	}
	return "INVALID_NAMED_RANGE: " + strings.Join(parts, "; ")
}

var (
	coreFunctionNames = map[string]bool{"SUM": true, "COUNT": true, "IF": true, "ROUND": true, "ABS": true}
	cellLikeName      = regexp.MustCompile(`^\$?[A-Za-z]+\$?\d+$`)
)

func problemWithExpression(node *FormulaNode) string {
	switch node.Kind {
	case NodeName:
		return "refersTo must not use a name"
	case NodeReference:
		if !node.Ref.HasSht {
			return "every reference in refersTo must be sheet-qualified"
		}
	case NodeRange:
		if !node.Start.Ref.HasSht && !node.End.Ref.HasSht {
			return "every reference in refersTo must be sheet-qualified"
		}
	case NodeCall:
		for _, arg := range node.Args {
			if p := problemWithExpression(arg); p != "" {
				return p
			}
		}
	case NodeUnary, NodePercent:
		return problemWithExpression(node.Operand)
	case NodeBinary:
		if p := problemWithExpression(node.Left); p != "" {
			return p
		}
		return problemWithExpression(node.Right)
	}
	return ""
}

// ValidateNamedRanges checks declared names against the rules in spec/02-workbook.md. It returns one
// diagnostic per offending name; an empty result means valid.
func ValidateNamedRanges(namedRanges []NamedRange) []NamedRangeDiagnostic {
	var diagnostics []NamedRangeDiagnostic
	seen := map[string]bool{}
	for _, item := range namedRanges {
		fail := func(message string) {
			diagnostics = append(diagnostics, NamedRangeDiagnostic{Code: "INVALID_NAMED_RANGE", Name: item.Name, Message: message})
		}
		switch {
		case cellLikeName.MatchString(item.Name):
			fail("a name must not look like a cell reference")
		case strings.EqualFold(item.Name, "true") || strings.EqualFold(item.Name, "false"):
			fail("TRUE and FALSE are reserved")
		case coreFunctionNames[strings.ToUpper(item.Name)]:
			fail("a Core function name is reserved")
		case seen[strings.ToLower(item.Name)]:
			fail("names must be unique ignoring case")
		default:
			ast, err := ParseFormula(item.RefersTo)
			if err != nil {
				fail(fmt.Sprintf("refersTo does not parse: %v", err))
			} else if problem := problemWithExpression(ast); problem != "" {
				fail(problem)
			}
		}
		seen[strings.ToLower(item.Name)] = true
	}
	return diagnostics
}
