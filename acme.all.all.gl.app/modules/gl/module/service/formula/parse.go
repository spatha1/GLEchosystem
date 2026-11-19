package formula

import (
	"strings"
)

// Bounds keep every expression small enough to reason about and cheap to evaluate.
const (
	MaxLength = 4096
	MaxDepth  = 48
	MaxNodes  = 800
)

// Node is a parsed expression.
type Node interface{ pos() int }

type (
	numLit struct {
		p    int
		text string
	}
	strLit struct {
		p int
		s string
	}
	boolLit struct {
		p int
		b bool
	}
	nullLit struct{ p int }
	ident   struct {
		p    int
		name string
	}
	path struct {
		p           int
		list, field string
	} // LIST.FIELD, only inside an aggregate
	unary struct {
		p  int
		op string
		x  Node
	}
	binary struct {
		p    int
		op   string
		l, r Node
	}
	call struct {
		p    int
		name string
		args []Node
	}
)

func (n numLit) pos() int  { return n.p }
func (n strLit) pos() int  { return n.p }
func (n boolLit) pos() int { return n.p }
func (n nullLit) pos() int { return n.p }
func (n ident) pos() int   { return n.p }
func (n path) pos() int    { return n.p }
func (n unary) pos() int   { return n.p }
func (n binary) pos() int  { return n.p }
func (n call) pos() int    { return n.p }

type token struct {
	kind string // num str ident op eof
	text string
	p    int
}

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

func lex(src string) ([]token, *Error) {
	var out []token
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case isDigit(c):
			j := i
			for j < len(src) && isDigit(src[j]) {
				j++
			}
			if j < len(src) && src[j] == '.' {
				j++
				if j >= len(src) || !isDigit(src[j]) {
					return nil, errAt(i, "SYNTAX", "a decimal point must be followed by digits")
				}
				for j < len(src) && isDigit(src[j]) {
					j++
				}
			}
			if j < len(src) && (isUpper(src[j]) || isLower(src[j]) || src[j] == '_') {
				return nil, errAt(j, "SYNTAX", "numbers are plain decimals (no exponents or suffixes)")
			}
			out = append(out, token{"num", src[i:j], i})
			i = j
		case c == '\'':
			var b strings.Builder
			j := i + 1
			for {
				if j >= len(src) {
					return nil, errAt(i, "SYNTAX", "unterminated string")
				}
				if src[j] == '\'' {
					if j+1 < len(src) && src[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					break
				}
				b.WriteByte(src[j])
				j++
			}
			out = append(out, token{"str", b.String(), i})
			i = j + 1
		case isUpper(c) || isLower(c):
			j := i
			for j < len(src) && (isUpper(src[j]) || isLower(src[j]) || isDigit(src[j]) || src[j] == '_') {
				j++
			}
			out = append(out, token{"ident", src[i:j], i})
			i = j
		default:
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case "==", "!=", "<>", "<=", ">=":
				out = append(out, token{"op", two, i})
				i += 2
				continue
			}
			if strings.ContainsRune("+-*/(),.<>", rune(c)) {
				out = append(out, token{"op", string(c), i})
				i++
				continue
			}
			return nil, errAt(i, "SYNTAX", "unexpected character %q", c)
		}
	}
	return append(out, token{"eof", "", len(src)}), nil
}

type parser struct {
	toks  []token
	i     int
	nodes int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }

func (p *parser) isOp(s string) bool { t := p.peek(); return t.kind == "op" && t.text == s }

func (p *parser) isWord(w string) bool {
	t := p.peek()
	return t.kind == "ident" && strings.EqualFold(t.text, w)
}

func (p *parser) count(at int) *Error {
	p.nodes++
	if p.nodes > MaxNodes {
		return errAt(at, "TOO_COMPLEX", "expression exceeds %d nodes", MaxNodes)
	}
	return nil
}

// Parse parses an expression. Grammar, lowest precedence first:
//
//	or  := and { OR and }          and := not { AND not }        not := NOT not | cmp
//	cmp := add [ (== != <> < <= > >=) add ]                       add := mul { (+|-) mul }
//	mul := una { (*|/) una }       una := - una | atom
//	atom := number | 'string' | TRUE | FALSE | NULL | NAME | LIST.FIELD | FUNC(args) | ( or )
func Parse(src string) (Node, *Error) {
	if len(src) > MaxLength {
		return nil, errAt(0, "TOO_COMPLEX", "expression exceeds %d characters", MaxLength)
	}
	if strings.TrimSpace(src) == "" {
		return nil, errAt(0, "SYNTAX", "empty expression")
	}
	toks, e := lex(src)
	if e != nil {
		return nil, e
	}
	p := &parser{toks: toks}
	n, e := p.or(0)
	if e != nil {
		return nil, e
	}
	if t := p.peek(); t.kind != "eof" {
		return nil, errAt(t.p, "SYNTAX", "unexpected %q", t.text)
	}
	return n, nil
}

func (p *parser) or(d int) (Node, *Error) {
	if d > MaxDepth {
		return nil, errAt(p.peek().p, "TOO_COMPLEX", "expression nests deeper than %d", MaxDepth)
	}
	l, e := p.and(d + 1)
	if e != nil {
		return nil, e
	}
	for p.isWord("OR") {
		t := p.next()
		r, e := p.and(d + 1)
		if e != nil {
			return nil, e
		}
		if e := p.count(t.p); e != nil {
			return nil, e
		}
		l = binary{t.p, "OR", l, r}
	}
	return l, nil
}

func (p *parser) and(d int) (Node, *Error) {
	l, e := p.not(d + 1)
	if e != nil {
		return nil, e
	}
	for p.isWord("AND") {
		t := p.next()
		r, e := p.not(d + 1)
		if e != nil {
			return nil, e
		}
		if e := p.count(t.p); e != nil {
			return nil, e
		}
		l = binary{t.p, "AND", l, r}
	}
	return l, nil
}

func (p *parser) not(d int) (Node, *Error) {
	if d > MaxDepth {
		return nil, errAt(p.peek().p, "TOO_COMPLEX", "expression nests deeper than %d", MaxDepth)
	}
	if p.isWord("NOT") {
		t := p.next()
		x, e := p.not(d + 1)
		if e != nil {
			return nil, e
		}
		return unary{t.p, "NOT", x}, p.count(t.p)
	}
	return p.cmp(d + 1)
}

func (p *parser) cmp(d int) (Node, *Error) {
	l, e := p.add(d + 1)
	if e != nil {
		return nil, e
	}
	for _, op := range []string{"==", "!=", "<>", "<=", ">=", "<", ">"} {
		if p.isOp(op) {
			t := p.next()
			r, e := p.add(d + 1)
			if e != nil {
				return nil, e
			}
			if op == "<>" {
				op = "!="
			}
			return binary{t.p, op, l, r}, p.count(t.p)
		}
	}
	return l, nil
}

func (p *parser) add(d int) (Node, *Error) {
	l, e := p.mul(d + 1)
	if e != nil {
		return nil, e
	}
	for p.isOp("+") || p.isOp("-") {
		t := p.next()
		r, e := p.mul(d + 1)
		if e != nil {
			return nil, e
		}
		if e := p.count(t.p); e != nil {
			return nil, e
		}
		l = binary{t.p, t.text, l, r}
	}
	return l, nil
}

func (p *parser) mul(d int) (Node, *Error) {
	l, e := p.una(d + 1)
	if e != nil {
		return nil, e
	}
	for p.isOp("*") || p.isOp("/") {
		t := p.next()
		r, e := p.una(d + 1)
		if e != nil {
			return nil, e
		}
		if e := p.count(t.p); e != nil {
			return nil, e
		}
		l = binary{t.p, t.text, l, r}
	}
	return l, nil
}

func (p *parser) una(d int) (Node, *Error) {
	if d > MaxDepth {
		return nil, errAt(p.peek().p, "TOO_COMPLEX", "expression nests deeper than %d", MaxDepth)
	}
	if p.isOp("-") {
		t := p.next()
		x, e := p.una(d + 1)
		if e != nil {
			return nil, e
		}
		return unary{t.p, "-", x}, p.count(t.p)
	}
	return p.atom(d + 1)
}

func (p *parser) atom(d int) (Node, *Error) {
	t := p.next()
	if e := p.count(t.p); e != nil {
		return nil, e
	}
	switch t.kind {
	case "num":
		return numLit{t.p, t.text}, nil
	case "str":
		return strLit{t.p, t.s()}, nil
	case "ident":
		switch strings.ToUpper(t.text) {
		case "TRUE":
			return boolLit{t.p, true}, nil
		case "FALSE":
			return boolLit{t.p, false}, nil
		case "NULL":
			return nullLit{t.p}, nil
		case "AND", "OR", "NOT":
			return nil, errAt(t.p, "SYNTAX", "unexpected %s", t.text)
		}
		if p.isOp("(") {
			p.next()
			var args []Node
			if !p.isOp(")") {
				for {
					a, e := p.or(d + 1)
					if e != nil {
						return nil, e
					}
					args = append(args, a)
					if p.isOp(",") {
						p.next()
						continue
					}
					break
				}
			}
			if !p.isOp(")") {
				return nil, errAt(p.peek().p, "SYNTAX", "expected ) to close %s(", t.text)
			}
			p.next()
			return call{t.p, strings.ToUpper(t.text), args}, nil
		}
		if !codeLike(t.text) {
			return nil, errAt(t.p, "SYNTAX", "%q is not a field name (field names are upper-case codes)", t.text)
		}
		if p.isOp(".") {
			p.next()
			f := p.next()
			if f.kind != "ident" || !codeLike(f.text) {
				return nil, errAt(f.p, "SYNTAX", "expected a field name after %s.", t.text)
			}
			return path{t.p, t.text, f.text}, nil
		}
		return ident{t.p, t.text}, nil
	case "op":
		if t.text == "(" {
			n, e := p.or(d + 1)
			if e != nil {
				return nil, e
			}
			if !p.isOp(")") {
				return nil, errAt(p.peek().p, "SYNTAX", "expected )")
			}
			p.next()
			return n, nil
		}
	case "eof":
		return nil, errAt(t.p, "SYNTAX", "the expression ends too early")
	}
	return nil, errAt(t.p, "SYNTAX", "unexpected %q", t.text)
}

func (t token) s() string { return t.text }

func codeLike(s string) bool {
	if s == "" || !isUpper(s[0]) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(isUpper(s[i]) || isDigit(s[i]) || s[i] == '_') {
			return false
		}
	}
	return true
}

// Identifiers returns every field name an expression reads (NAME) and every list path
// (LIST.FIELD as "LIST.FIELD"), in first-use order — the input to dependency analysis.
func Identifiers(n Node) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(Node)
	walk = func(n Node) {
		switch x := n.(type) {
		case ident:
			if !seen[x.name] {
				seen[x.name] = true
				out = append(out, x.name)
			}
		case path:
			k := x.list + "." + x.field
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		case unary:
			walk(x.x)
		case binary:
			walk(x.l)
			walk(x.r)
		case call:
			if x.name == "COUNT" && len(x.args) == 1 {
				if id, ok := x.args[0].(ident); ok {
					k := id.name + "[]"
					if !seen[k] {
						seen[k] = true
						out = append(out, k)
					}
					return
				}
			}
			for _, a := range x.args {
				walk(a)
			}
		}
	}
	walk(n)
	return out
}

// Calls returns the names of every function an expression calls.
func Calls(n Node) []string {
	var out []string
	var walk func(Node)
	walk = func(n Node) {
		switch x := n.(type) {
		case unary:
			walk(x.x)
		case binary:
			walk(x.l)
			walk(x.r)
		case call:
			out = append(out, x.name)
			for _, a := range x.args {
				walk(a)
			}
		}
	}
	walk(n)
	return out
}
