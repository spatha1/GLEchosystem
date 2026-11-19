package formula

// Static checking: decide, before any data exists, whether an expression can be evaluated —
// every name known, every operation well-typed. Currencies are a runtime property (every
// amount of an event is in the event's currency), so the checker tracks kinds, not
// currencies; the evaluator still refuses a currency mismatch at runtime.

// Type is a static type: a kind, and whether a number is an untyped literal.
type Type struct {
	Kind    Kind
	Untyped bool
}

// FuncSig describes a host function (registered by the caller, e.g. LOOKUP, OPEN_BALANCE).
type FuncSig struct {
	Args   []Kind // KNull = any
	Result Kind
}

// Symbols is what an expression may refer to.
type Symbols struct {
	Fields map[string]Kind            // event-level names
	Lists  map[string]map[string]Kind // list -> field -> kind
	Funcs  map[string]FuncSig         // host functions
}

type checker struct {
	s    Symbols
	errs []*Error
}

// Check returns the expression's static type and every problem found (not just the first).
func Check(n Node, s Symbols) (Type, []*Error) {
	c := &checker{s: s}
	t := c.check(n, false)
	return t, c.errs
}

func (c *checker) fail(p int, code, format string, a ...any) Type {
	c.errs = append(c.errs, errAt(p, code, format, a...))
	return Type{Kind: KNull}
}

func num(t Type) bool { return t.Kind == KNumber || t.Kind == KAmount }

// unknown reports the unknown type produced after an error (or by NULL): it is compatible with
// everything, so one mistake is reported once, not cascaded.
func unknown(t Type) bool { return t.Kind == KNull }

func (c *checker) check(n Node, inAggregate bool) Type {
	switch x := n.(type) {
	case numLit:
		if _, err := ParseDecimal(x.text); err != nil {
			return c.fail(x.p, "SYNTAX", "%v", err)
		}
		return Type{Kind: KNumber, Untyped: true}
	case strLit:
		return Type{Kind: KString}
	case boolLit:
		return Type{Kind: KBool}
	case nullLit:
		return Type{Kind: KNull}
	case ident:
		k, ok := c.s.Fields[x.name]
		if !ok {
			if _, isList := c.s.Lists[x.name]; isList {
				return c.fail(x.p, "LIST_OUTSIDE_AGGREGATE", "%s is a list; aggregate it (SUM(%s.FIELD), COUNT(%s)) or expand the list", x.name, x.name, x.name)
			}
			return c.fail(x.p, "UNKNOWN_IDENTIFIER", "%s is not a known field or measure", x.name)
		}
		return Type{Kind: k}
	case path:
		fields, ok := c.s.Lists[x.list]
		if !ok {
			return c.fail(x.p, "UNKNOWN_IDENTIFIER", "%s is not a known list", x.list)
		}
		k, ok := fields[x.field]
		if !ok {
			return c.fail(x.p, "UNKNOWN_IDENTIFIER", "list %s has no field %s", x.list, x.field)
		}
		if !inAggregate {
			return c.fail(x.p, "LIST_OUTSIDE_AGGREGATE", "%s.%s is a list field; use SUM(%s.%s)", x.list, x.field, x.list, x.field)
		}
		return Type{Kind: k}
	case unary:
		t := c.check(x.x, inAggregate)
		if x.op == "NOT" {
			if !unknown(t) && t.Kind != KBool {
				return c.fail(x.p, "TYPE_MISMATCH", "NOT needs a condition, got %s", t.Kind)
			}
			return Type{Kind: KBool}
		}
		if !unknown(t) && !num(t) {
			return c.fail(x.p, "TYPE_MISMATCH", "cannot negate %s", t.Kind)
		}
		return t
	case binary:
		l := c.check(x.l, inAggregate)
		r := c.check(x.r, inAggregate)
		if unknown(l) || unknown(r) {
			if x.op == "AND" || x.op == "OR" || isCmp(x.op) {
				return Type{Kind: KBool}
			}
			if !unknown(l) {
				return l
			}
			return r
		}
		switch x.op {
		case "AND", "OR":
			if l.Kind != KBool || r.Kind != KBool {
				return c.fail(x.p, "TYPE_MISMATCH", "%s needs conditions on both sides", x.op)
			}
			return Type{Kind: KBool}
		case "+", "-":
			return c.addSub(x.p, l, r)
		case "*":
			if !num(l) || !num(r) {
				return c.fail(x.p, "TYPE_MISMATCH", "cannot multiply %s by %s", l.Kind, r.Kind)
			}
			if l.Kind == KAmount && r.Kind == KAmount {
				return c.fail(x.p, "TYPE_MISMATCH", "cannot multiply two amounts")
			}
			if l.Kind == KAmount || r.Kind == KAmount {
				return Type{Kind: KAmount}
			}
			return Type{Kind: KNumber, Untyped: l.Untyped && r.Untyped}
		case "/":
			if !num(l) || !num(r) {
				return c.fail(x.p, "TYPE_MISMATCH", "cannot divide %s by %s", l.Kind, r.Kind)
			}
			if lit, ok := x.r.(numLit); ok {
				if q, err := ParseDecimal(lit.text); err == nil && q.Sign() == 0 {
					return c.fail(x.p, "DIVIDE_BY_ZERO", "division by the literal zero")
				}
			}
			switch {
			case l.Kind == KAmount && r.Kind == KAmount:
				return Type{Kind: KNumber}
			case l.Kind == KAmount:
				return Type{Kind: KAmount}
			case r.Kind == KAmount:
				return c.fail(x.p, "TYPE_MISMATCH", "cannot divide a number by an amount")
			}
			return Type{Kind: KNumber, Untyped: l.Untyped && r.Untyped}
		default:
			return c.compare(x.p, x.op, l, r)
		}
	case call:
		return c.call(x, inAggregate)
	}
	return c.fail(n.pos(), "SYNTAX", "unsupported expression")
}

func isCmp(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

func (c *checker) addSub(p int, l, r Type) Type {
	switch {
	case l.Kind == KAmount && r.Kind == KAmount:
		return Type{Kind: KAmount}
	case l.Kind == KAmount && r.Kind == KNumber:
		if !r.Untyped {
			return c.fail(p, "TYPE_MISMATCH", "cannot combine an amount with a computed number")
		}
		return Type{Kind: KAmount}
	case l.Kind == KNumber && r.Kind == KAmount:
		if !l.Untyped {
			return c.fail(p, "TYPE_MISMATCH", "cannot combine a computed number with an amount")
		}
		return Type{Kind: KAmount}
	case l.Kind == KNumber && r.Kind == KNumber:
		return Type{Kind: KNumber, Untyped: l.Untyped && r.Untyped}
	}
	return c.fail(p, "TYPE_MISMATCH", "%s and %s are not numbers", l.Kind, r.Kind)
}

func (c *checker) compare(p int, op string, l, r Type) Type {
	switch {
	case num(l) && num(r):
		if (l.Kind == KAmount && r.Kind == KNumber && !r.Untyped) || (r.Kind == KAmount && l.Kind == KNumber && !l.Untyped) {
			return c.fail(p, "TYPE_MISMATCH", "cannot compare an amount with a computed number")
		}
	case l.Kind == KString && r.Kind == KString, l.Kind == KDate && r.Kind == KDate:
	case l.Kind == KBool && r.Kind == KBool && (op == "==" || op == "!="):
	default:
		return c.fail(p, "TYPE_MISMATCH", "cannot compare %s with %s", l.Kind, r.Kind)
	}
	return Type{Kind: KBool}
}

// merge is the type two branches (IF, COALESCE) agree on.
func (c *checker) merge(p int, what string, a, b Type) Type {
	switch {
	case unknown(a):
		return b
	case unknown(b):
		return a
	case a.Kind == b.Kind:
		return Type{Kind: a.Kind, Untyped: a.Untyped && b.Untyped}
	case a.Kind == KAmount && b.Kind == KNumber && b.Untyped, b.Kind == KAmount && a.Kind == KNumber && a.Untyped:
		return Type{Kind: KAmount}
	}
	return c.fail(p, "TYPE_MISMATCH", "%s branches have different types (%s and %s)", what, a.Kind, b.Kind)
}

func (c *checker) call(x call, inAggregate bool) Type {
	args := func(n int) bool {
		if len(x.args) != n {
			c.fail(x.p, "ARITY", "%s takes %d argument(s)", x.name, n)
			return false
		}
		return true
	}
	switch x.name {
	case "IF":
		if !args(3) {
			return Type{Kind: KNull}
		}
		if t := c.check(x.args[0], inAggregate); !unknown(t) && t.Kind != KBool {
			c.fail(x.args[0].pos(), "TYPE_MISMATCH", "the IF condition must be a condition, got %s", t.Kind)
		}
		return c.merge(x.p, "IF", c.check(x.args[1], inAggregate), c.check(x.args[2], inAggregate))
	case "COALESCE":
		if len(x.args) < 2 {
			return c.fail(x.p, "ARITY", "COALESCE takes at least 2 arguments")
		}
		t := c.check(x.args[0], inAggregate)
		for _, a := range x.args[1:] {
			t = c.merge(x.p, "COALESCE", t, c.check(a, inAggregate))
		}
		return t
	case "ISNULL":
		if args(1) {
			c.check(x.args[0], inAggregate)
		}
		return Type{Kind: KBool}
	case "ROUND":
		if len(x.args) < 2 || len(x.args) > 3 {
			return c.fail(x.p, "ARITY", "ROUND takes 2 or 3 arguments")
		}
		t := c.check(x.args[0], inAggregate)
		if !unknown(t) && !num(t) {
			c.fail(x.p, "TYPE_MISMATCH", "ROUND needs a number, got %s", t.Kind)
		}
		if lit, ok := x.args[1].(numLit); !ok || !wholeUpTo(lit.text, 10) {
			c.fail(x.p, "TYPE_MISMATCH", "ROUND places must be a whole-number literal from 0 to 10")
		}
		if len(x.args) == 3 {
			if m, ok := x.args[2].(strLit); !ok || (m.s != "HALF_UP" && m.s != "HALF_EVEN") {
				c.fail(x.p, "TYPE_MISMATCH", "ROUND mode must be 'HALF_UP' or 'HALF_EVEN'")
			}
		}
		return t
	case "ABS":
		if !args(1) {
			return Type{Kind: KNull}
		}
		t := c.check(x.args[0], inAggregate)
		if !unknown(t) && !num(t) {
			return c.fail(x.p, "TYPE_MISMATCH", "ABS needs a number")
		}
		return t
	case "MIN", "MAX":
		if len(x.args) < 2 {
			return c.fail(x.p, "ARITY", "%s takes at least 2 arguments", x.name)
		}
		t := c.check(x.args[0], inAggregate)
		for _, a := range x.args[1:] {
			u := c.check(a, inAggregate)
			if !unknown(t) && !unknown(u) {
				c.compare(x.p, "<", t, u)
			}
			t = c.merge(x.p, x.name, t, u)
		}
		if !unknown(t) && !num(t) {
			return c.fail(x.p, "TYPE_MISMATCH", "%s needs numbers", x.name)
		}
		return t
	case "SUM":
		if !args(1) {
			return Type{Kind: KNull}
		}
		if _, ok := x.args[0].(path); !ok {
			return c.fail(x.p, "TYPE_MISMATCH", "SUM takes a list field, e.g. SUM(ALLOCATION_LIST.ALLOC_AMT)")
		}
		t := c.check(x.args[0], true)
		if !unknown(t) && !num(t) {
			return c.fail(x.p, "TYPE_MISMATCH", "SUM needs a numeric list field")
		}
		return t
	case "COUNT":
		if !args(1) {
			return Type{Kind: KNull}
		}
		id, ok := x.args[0].(ident)
		if !ok {
			return c.fail(x.p, "TYPE_MISMATCH", "COUNT takes a list name")
		}
		if _, ok := c.s.Lists[id.name]; !ok {
			return c.fail(id.p, "UNKNOWN_IDENTIFIER", "%s is not a known list", id.name)
		}
		return Type{Kind: KNumber}
	}
	sig, ok := c.s.Funcs[x.name]
	if !ok {
		return c.fail(x.p, "UNKNOWN_FUNCTION", "%s is not a function available here", x.name)
	}
	if len(x.args) != len(sig.Args) {
		return c.fail(x.p, "ARITY", "%s takes %d argument(s)", x.name, len(sig.Args))
	}
	for i, a := range x.args {
		t := c.check(a, inAggregate)
		if want := sig.Args[i]; want != KNull && !unknown(t) && t.Kind != want && !(want == KAmount && t.Kind == KNumber && t.Untyped) {
			c.fail(a.pos(), "TYPE_MISMATCH", "argument %d of %s must be %s, got %s", i+1, x.name, want, t.Kind)
		}
	}
	return Type{Kind: sig.Result}
}

func wholeUpTo(s string, max int) bool {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
		if n > max {
			return false
		}
	}
	return s != ""
}
