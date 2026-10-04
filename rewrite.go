package csvx

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Formula reference rewriting for structural edits (csvx-spec/spec/15-edit-operations.md,
// "Reference rewriting"). It works on the formula's source text and replaces only the reference
// spans, so everything else — spelling, case, spacing, `$` markers, string literals — is preserved.
// A formula that does not parse is returned unchanged. Mirrors csvx-ts's rewrite.ts.

// AxisEdit describes one axis of a structural edit. Rows are numbered 1-based (A1 row numbers,
// header = 1) and columns 0-based; Map and Deleted use whichever convention Axis names.
type AxisEdit struct {
	Axis    string // "row" or "column"
	Map     func(index int) int
	Deleted map[int]bool
}

var (
	plainSheetName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	cellLike       = regexp.MustCompile(`^\$?[A-Za-z]+\$?\d+$`)
	cellAt         = regexp.MustCompile(`^(\$?)([A-Za-z]+)(\$?)(\d+)`)
	bareSheetAt    = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)!`)
)

// FormatSheetName formats a sheet name for use before `!`, quoting it unless it is a bare
// identifier.
func FormatSheetName(name string) string {
	bare := plainSheetName.MatchString(name) && !cellLike.MatchString(name) && !strings.EqualFold(name, "true") && !strings.EqualFold(name, "false")
	if bare {
		return name
	}
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}

type refSpan struct {
	start, end, sheetStart int
	sheet                  string
	hasSheet               bool
	colAbs, col            string
	rowAbs, row            string
}

// scanReferences finds every cell reference in the formula text, in order, skipping string
// literals.
func scanReferences(source string) []refSpan {
	var spans []refSpan
	i := 0
	for i < len(source) {
		ch := source[i]
		if ch == '"' {
			i++
			for i < len(source) {
				if source[i] == '"' {
					if i+1 < len(source) && source[i+1] == '"' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if len(source) >= i+5 && strings.EqualFold(source[i:i+5], "#REF!") {
			i += 5
			continue
		}
		if isDigit(ch) || (ch == '.' && i+1 < len(source) && isDigit(source[i+1])) {
			for i < len(source) && (isDigit(source[i]) || source[i] == '.') {
				i++
			}
			continue
		}
		sheetStart, cursor := i, i
		var sheet string
		hasSheet := false
		if ch == '\'' {
			name, next, ok := readQuoted(source, i, '\'')
			if ok && next < len(source) && source[next] == '!' {
				sheet, hasSheet, cursor = name, true, next+1
			} else {
				if !ok {
					next = len(source)
				}
				i = next
				continue
			}
		} else if m := bareSheetAt.FindStringSubmatch(source[i:]); m != nil {
			sheet, hasSheet, cursor = m[1], true, i+len(m[0])
		}
		if m := cellAt.FindStringSubmatch(source[cursor:]); m != nil {
			end := cursor + len(m[0])
			if end >= len(source) || !(isIdentPart(source[end]) || source[end] == '(') {
				spans = append(spans, refSpan{start: cursor, end: end, sheetStart: sheetStart, sheet: sheet, hasSheet: hasSheet, colAbs: m[1], col: m[2], rowAbs: m[3], row: m[4]})
				i = end
				continue
			}
		}
		if hasSheet {
			i = cursor
			continue
		}
		if isIdentStart(ch) {
			for i < len(source) && isIdentPart(source[i]) {
				i++
			}
			continue
		}
		i++
	}
	return spans
}

type refSegment struct {
	a, b *refSpan
}

// segments groups adjacent `ref:ref` spans into ranges.
func segments(source string, spans []refSpan) []refSegment {
	var out []refSegment
	for k := 0; k < len(spans); k++ {
		a := &spans[k]
		if k+1 < len(spans) && source[a.end:spans[k+1].sheetStart] == ":" {
			out = append(out, refSegment{a: a, b: &spans[k+1]})
			k++
		} else {
			out = append(out, refSegment{a: a})
		}
	}
	return out
}

func spanText(span *refSpan, index int, axis string) string {
	col, row := span.col, span.row
	if axis == "column" {
		col = columnID(index)
	} else {
		row = strconv.Itoa(index)
	}
	return span.colAbs + col + span.rowAbs + row
}

func spanIndex(span *refSpan, axis string) int {
	if axis == "row" {
		n, _ := strconv.Atoi(span.row)
		return n
	}
	return columnIndexFromID(span.col)
}

type replacement struct {
	start, end int
	text       string
}

func applyReplacements(source string, replacements []replacement) string {
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	for _, r := range replacements {
		source = source[:r.start] + r.text + source[r.end:]
	}
	return source
}

func formulaParses(formula string) bool {
	_, err := ParseFormula(formula)
	return err == nil
}

// RewriteFormulaForAxisEdit rewrites a formula for a row or column insert/delete on targetSheet.
// ownSheet is the name of the sheet the formula lives on, which is what its unqualified references
// target.
func RewriteFormulaForAxisEdit(formula, ownSheet, targetSheet string, edit AxisEdit) string {
	if !formulaParses(formula) {
		return formula
	}
	var reps []replacement
	for _, seg := range segments(formula, scanReferences(formula)) {
		a, b := seg.a, seg.b
		sheet, hasSheet := a.sheet, a.hasSheet
		if !hasSheet && b != nil {
			sheet, hasSheet = b.sheet, b.hasSheet
		}
		if !hasSheet {
			sheet = ownSheet
		}
		if sheet != targetSheet {
			continue
		}
		if b == nil {
			index := spanIndex(a, edit.Axis)
			if edit.Deleted[index] {
				reps = append(reps, replacement{a.sheetStart, a.end, "#REF!"})
			} else if next := edit.Map(index); next != index {
				reps = append(reps, replacement{a.start, a.end, spanText(a, next, edit.Axis)})
			}
			continue
		}
		ia, ib := spanIndex(a, edit.Axis), spanIndex(b, edit.Axis)
		lo, hi := min(ia, ib), max(ia, ib)
		newLo, newHi := lo, hi
		if len(edit.Deleted) > 0 {
			for newLo <= hi && edit.Deleted[newLo] {
				newLo++
			}
			for newHi >= lo && edit.Deleted[newHi] {
				newHi--
			}
			if newLo > hi || newHi < lo || newLo > newHi {
				reps = append(reps, replacement{a.sheetStart, b.end, "#REF!"})
				continue
			}
		}
		mappedLo, mappedHi := edit.Map(newLo), edit.Map(newHi)
		nextA, nextB := mappedLo, mappedHi
		if ia > ib {
			nextA, nextB = mappedHi, mappedLo
		}
		if nextA != ia {
			reps = append(reps, replacement{a.start, a.end, spanText(a, nextA, edit.Axis)})
		}
		if nextB != ib {
			reps = append(reps, replacement{b.start, b.end, spanText(b, nextB, edit.Axis)})
		}
	}
	return applyReplacements(formula, reps)
}

// RewriteFormulaForSheetChange rewrites sheet-qualified references when a sheet is renamed
// (newName set) or deleted (deleted true: every reference to it becomes #REF!).
func RewriteFormulaForSheetChange(formula, oldName, newName string, deleted bool) string {
	if !formulaParses(formula) {
		return formula
	}
	var reps []replacement
	for _, seg := range segments(formula, scanReferences(formula)) {
		a, b := seg.a, seg.b
		sheet, hasSheet := a.sheet, a.hasSheet
		if !hasSheet && b != nil {
			sheet, hasSheet = b.sheet, b.hasSheet
		}
		if !hasSheet || sheet != oldName {
			continue
		}
		if deleted {
			end := a.end
			if b != nil {
				end = b.end
			}
			reps = append(reps, replacement{a.sheetStart, end, "#REF!"})
			continue
		}
		qualifier := FormatSheetName(newName) + "!"
		if a.hasSheet {
			reps = append(reps, replacement{a.sheetStart, a.start, qualifier})
		}
		if b != nil && b.hasSheet {
			reps = append(reps, replacement{b.sheetStart, b.start, qualifier})
		}
	}
	return applyReplacements(formula, reps)
}
