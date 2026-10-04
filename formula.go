package csvx

import (
	"fmt"
	"strings"
)

// Formula tokenizer and recursive-descent parser implementing csvx-spec/spec/06-formulas.md:
//
//	formula    = "=" expression
//	expression = literal | reference | ref-error | function | unary | binary | range | "(" expression ")"
//	reference  = [sheet "!"] cell
//	cell       = ["$"] column ["$"] row
//	ref-error  = "#REF!"
//
// Precedence, highest to lowest: unary signs, percent, multiplication/division, addition/
// subtraction, comparisons. This file only produces an AST; evaluation is in calculate.go.

// FormulaParseError reports invalid formula syntax.
type FormulaParseError struct{ Message string }

func (e *FormulaParseError) Error() string { return e.Message }

func parseErrorf(format string, args ...any) *FormulaParseError {
	return &FormulaParseError{Message: fmt.Sprintf(format, args...)}
}

// NodeKind identifies a FormulaNode variant.
type NodeKind int

const (
	NodeNumber NodeKind = iota
	NodeString
	NodeBoolean
	NodeRefError
	NodeReference
	NodeRange
	NodeCall
	NodeUnary
	NodePercent
	NodeBinary
)

// FormulaNode is a parsed formula expression. Which fields are set depends on Kind.
type FormulaNode struct {
	Kind     NodeKind
	Text     string // NodeNumber (literal text), NodeString, NodeCall (upper-cased name)
	Bool     bool
	Ref      CellRef      // NodeReference
	Start    *FormulaNode // NodeRange endpoints (NodeReference)
	End      *FormulaNode
	Args     []*FormulaNode // NodeCall
	Operator string         // NodeUnary, NodeBinary
	Operand  *FormulaNode   // NodeUnary, NodePercent
	Left     *FormulaNode   // NodeBinary
	Right    *FormulaNode
}

// CellRef is a parsed cell reference. Row is zero-based (A1 is Row 0); Column is the upper-cased
// column letters.
type CellRef struct {
	Sheet  string
	HasSht bool
	Column string
	Row    int
}

type tokenType int

const (
	tokNumber tokenType = iota
	tokString
	tokIdent
	tokQuotedSheet
	tokOp
	tokLParen
	tokRParen
	tokComma
	tokColon
	tokBang
	tokRefError
)

type token struct {
	typ   tokenType
	value string
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) }

func tokenize(source string) ([]token, error) {
	var tokens []token
	i := 0
	for i < len(source) {
		ch := source[i]
		switch {
		case ch == ' ' || ch == '\t':
			i++
		case ch == '(':
			tokens = append(tokens, token{typ: tokLParen})
			i++
		case ch == ')':
			tokens = append(tokens, token{typ: tokRParen})
			i++
		case ch == ',':
			tokens = append(tokens, token{typ: tokComma})
			i++
		case ch == ':':
			tokens = append(tokens, token{typ: tokColon})
			i++
		case ch == '#':
			if len(source) < i+5 || !strings.EqualFold(source[i:i+5], "#REF!") {
				return nil, parseErrorf("Unexpected character '#'")
			}
			tokens = append(tokens, token{typ: tokRefError})
			i += 5
		case ch == '!' && !(i+1 < len(source) && source[i+1] == '='):
			tokens = append(tokens, token{typ: tokBang})
			i++
		case ch == '\'' || ch == '"':
			value, next, ok := readQuoted(source, i, ch)
			if !ok {
				if ch == '\'' {
					return nil, parseErrorf("Unterminated quoted sheet name")
				}
				return nil, parseErrorf("Unterminated string literal")
			}
			if ch == '\'' {
				tokens = append(tokens, token{typ: tokQuotedSheet, value: value})
			} else {
				tokens = append(tokens, token{typ: tokString, value: value})
			}
			i = next
		case isDigit(ch) || (ch == '.' && i+1 < len(source) && isDigit(source[i+1])):
			j := i
			for j < len(source) && (isDigit(source[j]) || source[j] == '.') {
				j++
			}
			tokens = append(tokens, token{typ: tokNumber, value: source[i:j]})
			i = j
		case isIdentStart(ch):
			j := i
			for j < len(source) && isIdentPart(source[j]) {
				j++
			}
			tokens = append(tokens, token{typ: tokIdent, value: source[i:j]})
			i = j
		case ch == '<' || ch == '>' || ch == '=' || ch == '!':
			if i+1 < len(source) {
				two := source[i : i+2]
				if two == "<=" || two == ">=" || two == "!=" {
					tokens = append(tokens, token{typ: tokOp, value: two})
					i += 2
					continue
				}
			}
			tokens = append(tokens, token{typ: tokOp, value: string(ch)})
			i++
		case strings.IndexByte("+-*/%", ch) >= 0:
			tokens = append(tokens, token{typ: tokOp, value: string(ch)})
			i++
		default:
			return nil, parseErrorf("Unexpected character '%c'", ch)
		}
	}
	return tokens, nil
}

// readQuoted reads a quote-delimited run starting at source[start], where a doubled quote is an
// escaped quote. It returns the unescaped text and the index just past the closing quote.
func readQuoted(source string, start int, quote byte) (string, int, bool) {
	var out strings.Builder
	j := start + 1
	for j < len(source) {
		if source[j] == quote {
			if j+1 < len(source) && source[j+1] == quote {
				out.WriteByte(quote)
				j += 2
				continue
			}
			return out.String(), j + 1, true
		}
		out.WriteByte(source[j])
		j++
	}
	return "", 0, false
}

// splitCellToken splits "$A$1" into its column letters and row number. ok is false if the token is
// not a cell reference.
func splitCellToken(text string) (column string, row int, ok bool) {
	i := 0
	if i < len(text) && text[i] == '$' {
		i++
	}
	start := i
	for i < len(text) && ((text[i] >= 'A' && text[i] <= 'Z') || (text[i] >= 'a' && text[i] <= 'z')) {
		i++
	}
	if i == start {
		return "", 0, false
	}
	column = strings.ToUpper(text[start:i])
	if i < len(text) && text[i] == '$' {
		i++
	}
	digitsStart := i
	for i < len(text) && isDigit(text[i]) {
		i++
	}
	if i == digitsStart || i != len(text) {
		return "", 0, false
	}
	for _, d := range text[digitsStart:] {
		row = row*10 + int(d-'0')
		if row > 1<<30 {
			return "", 0, false
		}
	}
	return column, row, true
}

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() *token {
	if p.pos < len(p.tokens) {
		return &p.tokens[p.pos]
	}
	return nil
}

func (p *parser) next() (token, error) {
	if p.pos >= len(p.tokens) {
		return token{}, parseErrorf("Unexpected end of formula")
	}
	t := p.tokens[p.pos]
	p.pos++
	return t, nil
}

func (p *parser) peekOp() string {
	if t := p.peek(); t != nil && t.typ == tokOp {
		return t.value
	}
	return ""
}

func referenceFrom(sheet string, hasSheet bool, cell string) (*FormulaNode, error) {
	column, row, ok := splitCellToken(cell)
	if !ok || row < 1 {
		return nil, parseErrorf("Invalid cell reference '%s'", cell)
	}
	return &FormulaNode{Kind: NodeReference, Ref: CellRef{Sheet: sheet, HasSht: hasSheet, Column: column, Row: row - 1}}, nil
}

// ParseFormula parses a formula string (including the leading "=") into an AST. It returns a
// *FormulaParseError for invalid syntax, trailing tokens, or invalid references — spec/06-formulas.md
// requires parsing to reject these rather than guess.
func ParseFormula(source string) (*FormulaNode, error) {
	if !strings.HasPrefix(source, "=") {
		return nil, parseErrorf("Formula must start with '='")
	}
	tokens, err := tokenize(source[1:])
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	node, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.tokens) {
		return nil, parseErrorf("Unexpected trailing tokens in formula")
	}
	return node, nil
}

func (p *parser) parsePrimary() (*FormulaNode, error) {
	t, err := p.next()
	if err != nil {
		return nil, err
	}
	switch t.typ {
	case tokNumber:
		return &FormulaNode{Kind: NodeNumber, Text: t.value}, nil
	case tokString:
		return &FormulaNode{Kind: NodeString, Text: t.value}, nil
	case tokRefError:
		return &FormulaNode{Kind: NodeRefError}, nil
	case tokLParen:
		expr, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		if c, err := p.next(); err != nil || c.typ != tokRParen {
			return nil, parseErrorf("Expected ')'")
		}
		return expr, nil
	case tokQuotedSheet:
		return p.sheetReference(t.value)
	case tokIdent:
		if n := p.peek(); n != nil && n.typ == tokBang {
			return p.sheetReference(t.value)
		}
		if n := p.peek(); n != nil && n.typ == tokLParen {
			p.pos++
			var args []*FormulaNode
			if n := p.peek(); n == nil || n.typ != tokRParen {
				arg, err := p.parseComparison()
				if err != nil {
					return nil, err
				}
				args = append(args, arg)
				for n := p.peek(); n != nil && n.typ == tokComma; n = p.peek() {
					p.pos++
					arg, err := p.parseComparison()
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
				}
			}
			if c, err := p.next(); err != nil || c.typ != tokRParen {
				return nil, parseErrorf("Expected ')'")
			}
			return &FormulaNode{Kind: NodeCall, Text: strings.ToUpper(t.value), Args: args}, nil
		}
		upper := strings.ToUpper(t.value)
		if upper == "TRUE" || upper == "FALSE" {
			return &FormulaNode{Kind: NodeBoolean, Bool: upper == "TRUE"}, nil
		}
		if _, _, ok := splitCellToken(t.value); ok {
			ref, err := referenceFrom("", false, t.value)
			if err != nil {
				return nil, err
			}
			return p.finishReferenceOrRange(ref)
		}
		return nil, parseErrorf("Unexpected identifier '%s'", t.value)
	}
	return nil, parseErrorf("Unexpected token in formula")
}

// sheetReference parses `!cell` after a sheet name that has already been consumed.
func (p *parser) sheetReference(sheet string) (*FormulaNode, error) {
	if b, err := p.next(); err != nil || b.typ != tokBang {
		return nil, parseErrorf("Expected '!' after sheet name")
	}
	cell, err := p.next()
	if err != nil || cell.typ != tokIdent {
		return nil, parseErrorf("Expected cell reference after sheet name")
	}
	ref, err := referenceFrom(sheet, true, cell.value)
	if err != nil {
		return nil, err
	}
	return p.finishReferenceOrRange(ref)
}

func (p *parser) finishReferenceOrRange(start *FormulaNode) (*FormulaNode, error) {
	if t := p.peek(); t == nil || t.typ != tokColon {
		return start, nil
	}
	p.pos++
	endToken, err := p.next()
	if err != nil {
		return nil, err
	}
	var end *FormulaNode
	switch endToken.typ {
	case tokQuotedSheet:
		end, err = p.sheetReferenceOnly(endToken.value)
	case tokIdent:
		if n := p.peek(); n != nil && n.typ == tokBang {
			end, err = p.sheetReferenceOnly(endToken.value)
		} else {
			end, err = referenceFrom("", false, endToken.value)
		}
	default:
		return nil, parseErrorf("Expected reference after ':'")
	}
	if err != nil {
		return nil, err
	}
	return &FormulaNode{Kind: NodeRange, Start: start, End: end}, nil
}

func (p *parser) sheetReferenceOnly(sheet string) (*FormulaNode, error) {
	if b, err := p.next(); err != nil || b.typ != tokBang {
		return nil, parseErrorf("Expected '!' after sheet name")
	}
	cell, err := p.next()
	if err != nil || cell.typ != tokIdent {
		return nil, parseErrorf("Expected cell reference")
	}
	return referenceFrom(sheet, true, cell.value)
}

func (p *parser) parseUnary() (*FormulaNode, error) {
	if op := p.peekOp(); op == "+" || op == "-" {
		p.pos++
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &FormulaNode{Kind: NodeUnary, Operator: op, Operand: operand}, nil
	}
	node, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.peekOp() == "%" {
		p.pos++
		node = &FormulaNode{Kind: NodePercent, Operand: node}
	}
	return node, nil
}

func (p *parser) parseBinaryLevel(ops string, sub func() (*FormulaNode, error)) (*FormulaNode, error) {
	node, err := sub()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peekOp()
		if op == "" || !strings.Contains(" "+ops+" ", " "+op+" ") {
			return node, nil
		}
		p.pos++
		right, err := sub()
		if err != nil {
			return nil, err
		}
		node = &FormulaNode{Kind: NodeBinary, Operator: op, Left: node, Right: right}
	}
}

func (p *parser) parseMultiplicative() (*FormulaNode, error) {
	return p.parseBinaryLevel("* /", p.parseUnary)
}

func (p *parser) parseAdditive() (*FormulaNode, error) {
	return p.parseBinaryLevel("+ -", p.parseMultiplicative)
}

func (p *parser) parseComparison() (*FormulaNode, error) {
	return p.parseBinaryLevel("= != < <= > >=", p.parseAdditive)
}
