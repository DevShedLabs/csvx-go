package csvx

import (
	"strings"
	"unicode"
)

// TextCaseMode selects a case conversion (csvx-spec/spec/05-cell-values.md, "Text case").
type TextCaseMode string

const (
	CaseUpper TextCaseMode = "upper"
	CaseLower TextCaseMode = "lower"
	CaseTitle TextCaseMode = "title"
)

// IsCaseEligible reports whether a cell's text may be case-converted: no formula, and it resolves
// to a string. declaredType is the cell's resolved type ("" when nothing declares one).
func IsCaseEligible(text, declaredType, formula string) bool {
	if formula != "" {
		return false
	}
	if declaredType != "" {
		return declaredType == "string" && text != ""
	}
	return literalType(text) == "string"
}

// ChangeCase returns the converted text, or text unchanged when the cell is not eligible. Mapping
// is per code point; a code point whose mapping is not exactly one code point is left unchanged.
// Mirrors csvx-ts's changeCase.
func ChangeCase(text string, mode TextCaseMode, declaredType, formula string) string {
	if !IsCaseEligible(text, declaredType, formula) {
		return text
	}
	runes := []rune(text)
	if mode == CaseUpper || mode == CaseLower {
		for i, r := range runes {
			runes[i] = mapRune(r, mode == CaseUpper)
		}
		return string(runes)
	}
	for i, r := range runes {
		runes[i] = mapRune(r, false)
	}
	isLetter := func(i int) bool { return i >= 0 && i < len(runes) && unicode.IsLetter(runes[i]) }
	var out strings.Builder
	inWord := false
	for i, r := range runes {
		if (r == '\'' || r == '’') && isLetter(i-1) && isLetter(i+1) {
			out.WriteRune(r)
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) {
			inWord = false
			out.WriteRune(r)
			continue
		}
		if !inWord {
			r = mapRune(r, true)
		}
		inWord = true
		out.WriteRune(r)
	}
	return out.String()
}

// mapRune applies the simple one-to-one mapping. Go's unicode.To* are always one-to-one, so ß and
// İ come back unchanged or single-rune, matching the spec's "leave unchanged" rule; the explicit
// check below guards the one place Go's simple mapping differs from "no one-to-one mapping".
func mapRune(r rune, upper bool) rune {
	var mapped rune
	if upper {
		mapped = unicode.ToUpper(r)
	} else {
		mapped = unicode.ToLower(r)
	}
	switch r {
	case 'İ': // lowercases to two code points in the full mapping; spec leaves it unchanged
		return r
	}
	return mapped
}
