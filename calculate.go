package csvx

import (
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// Formula evaluation and recalculation, per csvx-spec/spec/10-calculation.md and
// spec/07-functions.md. Mirrors csvx-ts's calculate.ts: decimal values are strings on the wire, so
// the number of digits after the decimal point (scale) is tracked explicitly and arithmetic is exact
// (math/big), never floating point.

// ReferenceRequest is a cell a formula reads. Row is zero-based.
type ReferenceRequest struct {
	Sheet    string
	HasSheet bool
	Column   string
	Row      int
}

// ReferenceResolver returns the current value of a referenced cell.
type ReferenceResolver func(ReferenceRequest) Value

func errorValue(code string, message string) Value {
	return Value{Type: "error", Code: code, Message: message}
}

func isError(v Value) bool { return v.Type == "error" }

type numeric struct {
	value     *big.Rat
	scale     int
	isInteger bool
}

func scaleOf(text string) int {
	if dot := strings.IndexByte(text, '.'); dot >= 0 {
		return len(text) - dot - 1
	}
	return 0
}

func parseRat(text string) (*big.Rat, bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "eExXpP_") || strings.EqualFold(text, "inf") || strings.EqualFold(text, "nan") {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(text)
	return r, ok
}

func intValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), n == float64(int64(n))
	}
	return 0, false
}

// toNumeric coerces a value to a numeric reading, or returns the VALUE/propagated error to produce.
// Blanks count as zero.
func toNumeric(v Value) (numeric, *Value) {
	if isError(v) {
		return numeric{}, &v
	}
	switch v.Type {
	case "blank":
		return numeric{big.NewRat(0, 1), 0, true}, nil
	case "boolean":
		if b, _ := v.Value.(bool); b {
			return numeric{big.NewRat(1, 1), 0, true}, nil
		}
		return numeric{big.NewRat(0, 1), 0, true}, nil
	case "integer":
		n, _ := intValue(v.Value)
		return numeric{big.NewRat(n, 1), 0, true}, nil
	case "decimal":
		text, _ := v.Value.(string)
		if r, ok := parseRat(text); ok {
			return numeric{r, scaleOf(strings.TrimSpace(text)), false}, nil
		}
	case "string":
		text, _ := v.Value.(string)
		text = strings.TrimSpace(text)
		if r, ok := parseRat(text); ok {
			return numeric{r, scaleOf(text), !strings.Contains(text, ".")}, nil
		}
	}
	e := errorValue("VALUE", "")
	return numeric{}, &e
}

func numericToValue(n numeric) Value {
	if n.isInteger && n.value.IsInt() {
		return Value{Type: "integer", Value: n.value.Num().Int64()}
	}
	scale := n.scale
	if scale < 0 {
		scale = 0
	}
	return Value{Type: "decimal", Value: n.value.FloatString(scale)}
}

func maxInt(values ...int) int {
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func combine(a, b numeric, op string) (numeric, *Value) {
	both := a.isInteger && b.isInteger
	switch op {
	case "+", "-":
		r := new(big.Rat)
		if op == "+" {
			r.Add(a.value, b.value)
		} else {
			r.Sub(a.value, b.value)
		}
		return numeric{r, maxInt(a.scale, b.scale), both && r.IsInt()}, nil
	case "*":
		r := new(big.Rat).Mul(a.value, b.value)
		return numeric{r, maxInt(a.scale+b.scale, 0), both && r.IsInt()}, nil
	default: // "/"
		if b.value.Sign() == 0 {
			e := errorValue("DIV0", "")
			return numeric{}, &e
		}
		r := new(big.Rat).Quo(a.value, b.value)
		exact := both && r.IsInt()
		scale := maxInt(a.scale, b.scale, 2)
		if exact {
			scale = 0
		}
		return numeric{r, scale, exact}, nil
	}
}

func isNumericType(v Value) bool {
	switch v.Type {
	case "integer", "decimal", "boolean", "blank":
		return true
	case "string":
		text, _ := v.Value.(string)
		_, ok := parseRat(text)
		return ok
	}
	return false
}

func stringOf(v Value) string {
	switch v.Type {
	case "blank":
		return ""
	case "boolean":
		if b, _ := v.Value.(bool); b {
			return "TRUE"
		}
		return "FALSE"
	}
	switch x := v.Value.(type) {
	case nil:
		return ""
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func applyComparison(cmp int, op string) bool {
	switch op {
	case "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	}
	return cmp >= 0
}

func compareValues(a, b Value, op string) Value {
	if isError(a) {
		return a
	}
	if isError(b) {
		return b
	}
	if isNumericType(a) && isNumericType(b) {
		an, _ := toNumeric(a)
		bn, _ := toNumeric(b)
		return Value{Type: "boolean", Value: applyComparison(an.value.Cmp(bn.value), op)}
	}
	if op != "=" && op != "!=" {
		return errorValue("VALUE", "")
	}
	return Value{Type: "boolean", Value: applyComparison(strings.Compare(stringOf(a), stringOf(b)), op)}
}

// truthy reports a condition's truth, or the error value to propagate.
func truthy(v Value) (bool, *Value) {
	if isError(v) {
		return false, &v
	}
	switch v.Type {
	case "boolean":
		b, _ := v.Value.(bool)
		return b, nil
	case "integer", "decimal":
		n, e := toNumeric(v)
		if e != nil {
			return false, e
		}
		return n.value.Sign() != 0, nil
	case "string":
		text, _ := v.Value.(string)
		switch strings.ToUpper(strings.TrimSpace(text)) {
		case "TRUE":
			return true, nil
		case "FALSE":
			return false, nil
		}
	}
	e := errorValue("VALUE", "")
	return false, &e
}

// nameTable maps a lower-cased declared name to its parsed refersTo (nil when it does not parse).
type nameTable map[string]*FormulaNode

func buildNameTable(namedRanges []NamedRange) nameTable {
	table := nameTable{}
	for _, item := range namedRanges {
		ast, err := ParseFormula(item.RefersTo)
		if err != nil {
			ast = nil
		}
		table[strings.ToLower(item.Name)] = ast
	}
	return table
}

type evalContext struct {
	resolve ReferenceResolver
	names   nameTable
	// insideName is set while evaluating a name's own refersTo, where names are not allowed.
	insideName bool
}

// lookupName returns what a name stands for, or nil when it is not declared (or not usable here).
func (c evalContext) lookupName(node *FormulaNode) *FormulaNode {
	if c.insideName {
		return nil
	}
	return c.names[strings.ToLower(node.Text)]
}

func (c evalContext) inside() evalContext {
	c.insideName = true
	return c
}

func columnIndexOfLetters(letters string) int { return columnIndexFromID(letters) }

func (c evalContext) flattenRange(node *FormulaNode) []Value {
	startCol, endCol := columnIndexOfLetters(node.Start.Ref.Column), columnIndexOfLetters(node.End.Ref.Column)
	minCol, maxCol := min(startCol, endCol), max(startCol, endCol)
	minRow, maxRow := min(node.Start.Ref.Row, node.End.Ref.Row), max(node.Start.Ref.Row, node.End.Ref.Row)
	sheet, hasSheet := node.Start.Ref.Sheet, node.Start.Ref.HasSht
	if !hasSheet {
		sheet, hasSheet = node.End.Ref.Sheet, node.End.Ref.HasSht
	}
	var values []Value
	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			values = append(values, c.resolve(ReferenceRequest{Sheet: sheet, HasSheet: hasSheet, Column: columnID(col), Row: row}))
		}
	}
	return values
}

func isAggregatable(v Value) bool { return v.Type == "integer" || v.Type == "decimal" }

func (c evalContext) argValues(arg *FormulaNode) []Value {
	if arg.Kind == NodeRange {
		return c.flattenRange(arg)
	}
	if arg.Kind == NodeName {
		if target := c.lookupName(arg); target != nil && target.Kind == NodeRange {
			return c.inside().flattenRange(target)
		}
	}
	return []Value{c.eval(arg)}
}

func (c evalContext) call(node *FormulaNode) Value {
	args := node.Args
	switch node.Text {
	case "SUM":
		sum, scale, sawDecimal, sawAny := new(big.Rat), 0, false, false
		for _, arg := range args {
			for _, v := range c.argValues(arg) {
				if isError(v) {
					return v
				}
				if !isAggregatable(v) {
					continue
				}
				n, e := toNumeric(v)
				if e != nil {
					return *e
				}
				sum.Add(sum, n.value)
				if !n.isInteger {
					sawDecimal = true
				}
				scale = maxInt(scale, n.scale)
				sawAny = true
			}
		}
		if !sawAny {
			return Value{Type: "integer", Value: int64(0)}
		}
		return numericToValue(numeric{sum, scale, !sawDecimal})
	case "COUNT":
		count := int64(0)
		for _, arg := range args {
			for _, v := range c.argValues(arg) {
				if isError(v) {
					return v
				}
				if isAggregatable(v) {
					count++
				}
			}
		}
		return Value{Type: "integer", Value: count}
	case "IF":
		if len(args) < 2 || len(args) > 3 {
			return errorValue("VALUE", "IF requires 2 or 3 arguments")
		}
		cond, e := truthy(c.eval(args[0]))
		if e != nil {
			return *e
		}
		if cond {
			return c.eval(args[1])
		}
		if len(args) == 3 {
			return c.eval(args[2])
		}
		return Value{Type: "blank"}
	case "ROUND":
		if len(args) != 2 {
			return errorValue("VALUE", "ROUND requires 2 arguments")
		}
		number, e := toNumeric(c.eval(args[0]))
		if e != nil {
			return *e
		}
		digitsValue := c.eval(args[1])
		if isError(digitsValue) {
			return digitsValue
		}
		digits, ok := intValue(digitsValue.Value)
		if digitsValue.Type != "integer" || !ok {
			return errorValue("VALUE", "ROUND digits must be an integer")
		}
		// Half away from zero (spec/07-functions.md): scale by 10^digits, round, scale back.
		factor := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(abs64(digits)), nil))
		scaled := new(big.Rat)
		if digits >= 0 {
			scaled.Mul(number.value, factor)
		} else {
			scaled.Quo(number.value, factor)
		}
		rounded := roundHalfAway(scaled)
		result := new(big.Rat).SetInt(rounded)
		if digits >= 0 {
			result.Quo(result, factor)
		} else {
			result.Mul(result, factor)
		}
		if digits <= 0 {
			return Value{Type: "integer", Value: result.Num().Int64()}
		}
		return Value{Type: "decimal", Value: result.FloatString(int(digits))}
	case "ABS":
		if len(args) != 1 {
			return errorValue("VALUE", "ABS requires 1 argument")
		}
		n, e := toNumeric(c.eval(args[0]))
		if e != nil {
			return *e
		}
		n.value = new(big.Rat).Abs(n.value)
		return numericToValue(n)
	}
	return errorValue("NAME", "")
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// roundHalfAway rounds a rational to the nearest integer, ties away from zero.
func roundHalfAway(r *big.Rat) *big.Int {
	twice := new(big.Int).Mul(r.Num(), big.NewInt(2))
	twice.Add(twice, new(big.Int).Mul(big.NewInt(int64(r.Sign())), r.Denom()))
	return twice.Quo(twice, new(big.Int).Mul(big.NewInt(2), r.Denom()))
}

func (c evalContext) eval(node *FormulaNode) Value {
	switch node.Kind {
	case NodeNumber:
		if !strings.Contains(node.Text, ".") {
			n, err := strconv.ParseInt(node.Text, 10, 64)
			if err != nil {
				return errorValue("VALUE", "")
			}
			return Value{Type: "integer", Value: n}
		}
		return Value{Type: "decimal", Value: node.Text}
	case NodeString:
		return Value{Type: "string", Value: node.Text}
	case NodeBoolean:
		return Value{Type: "boolean", Value: node.Bool}
	case NodeRefError:
		return errorValue("REF", "")
	case NodeName:
		if target := c.lookupName(node); target != nil {
			return c.inside().eval(target)
		}
		return errorValue("NAME", "")
	case NodeReference:
		return c.resolve(ReferenceRequest{Sheet: node.Ref.Sheet, HasSheet: node.Ref.HasSht, Column: node.Ref.Column, Row: node.Ref.Row})
	case NodeRange:
		if values := c.flattenRange(node); len(values) > 0 {
			return values[0]
		}
		return Value{Type: "blank"}
	case NodeCall:
		return c.call(node)
	case NodeUnary:
		n, e := toNumeric(c.eval(node.Operand))
		if e != nil {
			return *e
		}
		if node.Operator == "-" {
			n.value = new(big.Rat).Neg(n.value)
		}
		return numericToValue(n)
	case NodePercent:
		n, e := toNumeric(c.eval(node.Operand))
		if e != nil {
			return *e
		}
		return numericToValue(numeric{new(big.Rat).Quo(n.value, big.NewRat(100, 1)), maxInt(n.scale+2, 2), false})
	}
	// NodeBinary
	switch node.Operator {
	case "=", "!=", "<", "<=", ">", ">=":
		return compareValues(c.eval(node.Left), c.eval(node.Right), node.Operator)
	}
	left, e := toNumeric(c.eval(node.Left))
	if e != nil {
		return *e
	}
	right, e := toNumeric(c.eval(node.Right))
	if e != nil {
		return *e
	}
	combined, e := combine(left, right, node.Operator)
	if e != nil {
		return *e
	}
	return numericToValue(combined)
}

// EvaluateFormula evaluates a formula string (including the leading "=") against a reference
// resolver. A parse error surfaces as a NAME error rather than a Go error, since a formula cell
// with invalid syntax is still a valid cell state a UI must be able to render.
func EvaluateFormula(formula string, resolve ReferenceResolver) Value {
	ast, err := ParseFormula(formula)
	if err != nil {
		return errorValue("NAME", err.Error())
	}
	return evalContext{resolve: resolve}.eval(ast)
}

// FormulaCellInput is one cell handed to RecalculateCells: a formula to evaluate or a plain value.
type FormulaCellInput struct {
	Formula string
	Value   Value
}

// CellMap maps A1 coordinates to cell inputs.
type CellMap map[string]FormulaCellInput

// RecalculateOptions configures RecalculateCells.
type RecalculateOptions struct {
	// ResolveSheet returns the cell map of a sheet named by a `Sheet!A1` reference, or nil.
	ResolveSheet func(name string) CellMap
}

func refCoordinate(ref CellRef) string { return ref.Column + strconv.Itoa(ref.Row+1) }

type rangeRef struct {
	sheet    string
	hasSheet bool
	fromCol  int
	toCol    int
	fromRow  int
	toRow    int
}

// collectReferences gathers the single-cell references and the ranges a formula reads.
func collectReferences(node *FormulaNode, cells *[]CellRef, ranges *[]rangeRef, names nameTable, insideName bool) {
	switch node.Kind {
	case NodeName:
		if !insideName {
			if target := names[strings.ToLower(node.Text)]; target != nil {
				collectReferences(target, cells, ranges, names, true)
			}
		}
	case NodeReference:
		*cells = append(*cells, node.Ref)
	case NodeRange:
		a, b := columnIndexFromID(node.Start.Ref.Column), columnIndexFromID(node.End.Ref.Column)
		r := rangeRef{fromCol: min(a, b), toCol: max(a, b), fromRow: min(node.Start.Ref.Row, node.End.Ref.Row), toRow: max(node.Start.Ref.Row, node.End.Ref.Row)}
		r.sheet, r.hasSheet = node.Start.Ref.Sheet, node.Start.Ref.HasSht
		if !r.hasSheet {
			r.sheet, r.hasSheet = node.End.Ref.Sheet, node.End.Ref.HasSht
		}
		*ranges = append(*ranges, r)
	case NodeCall:
		for _, a := range node.Args {
			collectReferences(a, cells, ranges, names, insideName)
		}
	case NodeUnary, NodePercent:
		collectReferences(node.Operand, cells, ranges, names, insideName)
	case NodeBinary:
		collectReferences(node.Left, cells, ranges, names, insideName)
		collectReferences(node.Right, cells, ranges, names, insideName)
	}
}

type calcNode struct {
	sheet, coordinate string
	ast               *FormulaNode
	parseErr          string
	deps              []string
}

type formulaCellPos struct {
	id       string
	col, row int
}

// RecalculateSheets recalculates every formula cell across a set of named sheets in dependency order
// (spec/10-calculation.md). The graph has an edge for every cell a formula reads — each cell inside
// a range, and cells on other sheets — so a reference to another sheet's formula cell sees its
// calculated value, and a cycle that crosses sheets is CYCLE in every cell on it. A reference to a
// sheet that is not in sheets (and not supplied by external) is REF. A declared name contributes
// the edges of its refersTo. It returns results only for
// cells that had a formula, keyed by sheet name and then coordinate.
func RecalculateSheets(sheets map[string]CellMap, external func(name string) CellMap, namedRanges []NamedRange) map[string]map[string]Value {
	names := buildNameTable(namedRanges)
	idOf := func(sheet, coordinate string) string { return sheet + "\n" + coordinate }
	nodes := map[string]*calcNode{}
	var ids []string
	positions := map[string][]formulaCellPos{}
	sheetNames := make([]string, 0, len(sheets))
	for name := range sheets {
		sheetNames = append(sheetNames, name)
	}
	sort.Strings(sheetNames) // deterministic evaluation order
	for _, sheet := range sheetNames {
		cells := sheets[sheet]
		coordinates := make([]string, 0, len(cells))
		for coordinate, cell := range cells {
			if cell.Formula != "" {
				coordinates = append(coordinates, coordinate)
			}
		}
		sort.Strings(coordinates)
		for _, coordinate := range coordinates {
			id := idOf(sheet, coordinate)
			node := &calcNode{sheet: sheet, coordinate: coordinate}
			if ast, err := ParseFormula(cells[coordinate].Formula); err != nil {
				node.parseErr = err.Error()
			} else {
				node.ast = ast
			}
			nodes[id] = node
			ids = append(ids, id)
			if column, row, ok := IndicesForCoordinate(coordinate); ok {
				positions[sheet] = append(positions[sheet], formulaCellPos{id: id, col: column, row: row + 1})
			}
		}
	}
	for _, id := range ids {
		node := nodes[id]
		if node.ast == nil {
			continue
		}
		var cells []CellRef
		var ranges []rangeRef
		collectReferences(node.ast, &cells, &ranges, names, false)
		for _, ref := range cells {
			sheet := node.sheet
			if ref.HasSht {
				sheet = ref.Sheet
			}
			if dep := idOf(sheet, refCoordinate(ref)); nodes[dep] != nil {
				node.deps = append(node.deps, dep)
			}
		}
		for _, r := range ranges {
			sheet := node.sheet
			if r.hasSheet {
				sheet = r.sheet
			}
			for _, candidate := range positions[sheet] {
				if candidate.col >= r.fromCol && candidate.col <= r.toCol && candidate.row-1 >= r.fromRow && candidate.row-1 <= r.toRow {
					node.deps = append(node.deps, candidate.id)
				}
			}
		}
	}

	const (
		white = iota
		gray
		black
	)
	color := map[string]int{}
	cyclic := map[string]bool{}
	var order, stack []string
	var visit func(string)
	visit = func(id string) {
		switch color[id] {
		case black:
			return
		case gray:
			start := 0
			for i, s := range stack {
				if s == id {
					start = i
				}
			}
			for _, s := range stack[start:] {
				cyclic[s] = true
			}
			return
		}
		color[id] = gray
		stack = append(stack, id)
		for _, dep := range nodes[id].deps {
			visit(dep)
		}
		stack = stack[:len(stack)-1]
		color[id] = black
		order = append(order, id)
	}
	for _, id := range ids {
		visit(id)
	}

	results := map[string]Value{}
	resolver := func(own string) ReferenceResolver {
		return func(ref ReferenceRequest) Value {
			sheet := own
			if ref.HasSheet {
				sheet = ref.Sheet
			}
			cells, ok := sheets[sheet]
			if !ok && ref.HasSheet && external != nil {
				cells = external(sheet)
				ok = cells != nil
			}
			if !ok {
				return errorValue("REF", "")
			}
			coordinate := ref.Column + strconv.Itoa(ref.Row+1)
			id := idOf(sheet, coordinate)
			if cyclic[id] {
				return errorValue("CYCLE", "")
			}
			if v, done := results[id]; done {
				return v
			}
			if cell, found := cells[coordinate]; found && cell.Formula == "" && cell.Value.Type != "" {
				return cell.Value
			}
			return Value{Type: "blank"}
		}
	}
	for _, id := range order {
		if cyclic[id] {
			continue
		}
		node := nodes[id]
		if node.ast == nil {
			results[id] = errorValue("NAME", node.parseErr)
			continue
		}
		results[id] = evalContext{resolve: resolver(node.sheet), names: names}.eval(node.ast)
	}
	for id := range cyclic {
		results[id] = errorValue("CYCLE", "")
	}
	out := map[string]map[string]Value{}
	for _, sheet := range sheetNames {
		out[sheet] = map[string]Value{}
	}
	for id, node := range nodes {
		out[node.sheet][node.coordinate] = results[id]
	}
	return out
}

// RecalculateCells recalculates every formula cell in a single-sheet coordinate map in dependency
// order and returns a result for each formula cell. Cells in a circular dependency all resolve to
// CYCLE.
func RecalculateCells(cells CellMap, options RecalculateOptions) map[string]Value {
	const self = "\x00self"
	return RecalculateSheets(map[string]CellMap{self: cells}, options.ResolveSheet, nil)[self]
}
