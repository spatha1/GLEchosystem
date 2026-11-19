package formula

import (
	"fmt"
	"math/big"
	"strings"
)

// Item is one element of a 1:N list (e.g. one allocation of a receipt).
type Item interface {
	Lookup(field string) (Value, bool)
}

// Env supplies the values an expression may read. It is the only way data enters an
// evaluation. Call serves host functions (lookups, ledger state) registered by the caller;
// ok=false means the function is unknown.
type Env interface {
	Lookup(name string) (Value, bool)
	List(name string) ([]Item, bool)
	Call(name string, args []Value) (v Value, ok bool, err error)
}

// Read is one input an evaluation used.
type Read struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CallTrace is one function call an evaluation made.
type CallTrace struct {
	Name   string   `json:"name"`
	Args   []string `json:"args"`
	Result string   `json:"result"`
}

// Rounding is one explicit rounding step.
type Rounding struct {
	Scale  int    `json:"scale"`
	Mode   string `json:"mode"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// Trace is the calculation lineage of one evaluation.
type Trace struct {
	Reads     []Read      `json:"reads"`
	Calls     []CallTrace `json:"calls,omitempty"`
	Roundings []Rounding  `json:"roundings,omitempty"`
}

// Options tune evaluation.
type Options struct {
	DefaultRounding string // HALF_UP (default) or HALF_EVEN, when ROUND names no mode
}

type evaluator struct {
	env   Env
	opt   Options
	trace Trace
	read  map[string]bool
}

// Coerce gives a result its declared type at the point of use. Evaluation is lazy (an IF
// evaluates only the branch taken), so IF(c, PREMIUM, 0) may yield the untyped literal 0,
// which — like an untyped constant — takes the declared type: an amount in the event's
// currency. A computed (typed) number never becomes money, and any other kind is an error.
func Coerce(v Value, want Kind, currency string) (Value, error) {
	switch {
	case v.Kind == KNull || v.Kind == want:
		if want == KAmount && v.Kind == KAmount && currency != "" && v.Currency != currency {
			return Null, &Error{Code: "CURRENCY_MISMATCH", Message: fmt.Sprintf("%s is not in %s", v, currency)}
		}
		return v, nil
	case want == KAmount && v.Kind == KNumber && v.Untyped:
		if currency == "" {
			return Null, &Error{Code: "TYPE_MISMATCH", Message: "no currency to give the literal " + v.String()}
		}
		return Amount(v.Num, currency), nil
	case want == KNumber && v.Kind == KNumber:
		return Number(v.Num), nil
	}
	return Null, &Error{Code: "TYPE_MISMATCH", Message: fmt.Sprintf("expected %s, got %s (%s)", want, v.Kind, v)}
}

// Eval evaluates a parsed expression against env and returns the value and its lineage.
// Callers that know the expression's declared type apply Coerce to the result.
func Eval(n Node, env Env, opt Options) (Value, Trace, error) {
	if opt.DefaultRounding == "" {
		opt.DefaultRounding = "HALF_UP"
	}
	ev := &evaluator{env: env, opt: opt, read: map[string]bool{}}
	v, err := ev.eval(n)
	if err != nil {
		return Null, ev.trace, err
	}
	return v, ev.trace, nil
}

func (ev *evaluator) recordRead(name string, v Value) {
	if !ev.read[name] {
		ev.read[name] = true
		ev.trace.Reads = append(ev.trace.Reads, Read{name, v.String()})
	}
}

func numeric(v Value) bool { return v.Kind == KNumber || v.Kind == KAmount }

func (ev *evaluator) eval(n Node) (Value, error) {
	switch x := n.(type) {
	case numLit:
		r, err := ParseDecimal(x.text)
		if err != nil {
			return Null, errAt(x.p, "SYNTAX", "%v", err)
		}
		return Value{Kind: KNumber, Num: r, Untyped: true}, nil
	case strLit:
		return String(x.s), nil
	case boolLit:
		return Bool(x.b), nil
	case nullLit:
		return Null, nil
	case ident:
		v, ok := ev.env.Lookup(x.name)
		if !ok {
			return Null, errAt(x.p, "UNKNOWN_IDENTIFIER", "%s is not available here", x.name)
		}
		ev.recordRead(x.name, v)
		return v, nil
	case path:
		return Null, errAt(x.p, "LIST_OUTSIDE_AGGREGATE", "%s.%s is a list field; use SUM(%s.%s) or expand the list", x.list, x.field, x.list, x.field)
	case unary:
		v, err := ev.eval(x.x)
		if err != nil {
			return Null, err
		}
		if x.op == "NOT" {
			if v.Kind == KNull {
				return Null, errAt(x.p, "NULL_CONDITION", "NOT of a null value")
			}
			if v.Kind != KBool {
				return Null, errAt(x.p, "TYPE_MISMATCH", "NOT needs a condition, got %s", v.Kind)
			}
			return Bool(!v.Bool), nil
		}
		if v.Kind == KNull {
			return Null, nil
		}
		if !numeric(v) {
			return Null, errAt(x.p, "TYPE_MISMATCH", "cannot negate %s", v.Kind)
		}
		out := v
		out.Num = new(big.Rat).Neg(v.Num)
		return out, nil
	case binary:
		return ev.binary(x)
	case call:
		return ev.call(x)
	}
	return Null, errAt(n.pos(), "SYNTAX", "unsupported expression")
}

func (ev *evaluator) cond(n Node, what string) (bool, error) {
	v, err := ev.eval(n)
	if err != nil {
		return false, err
	}
	if v.Kind == KNull {
		return false, errAt(n.pos(), "NULL_CONDITION", "%s is null; use COALESCE or ISNULL to say what null means", what)
	}
	if v.Kind != KBool {
		return false, errAt(n.pos(), "TYPE_MISMATCH", "%s must be a condition, got %s", what, v.Kind)
	}
	return v.Bool, nil
}

func (ev *evaluator) binary(x binary) (Value, error) {
	if x.op == "AND" || x.op == "OR" {
		l, err := ev.cond(x.l, "the left side of "+x.op)
		if err != nil {
			return Null, err
		}
		if (x.op == "AND" && !l) || (x.op == "OR" && l) {
			return Bool(l), nil
		}
		r, err := ev.cond(x.r, "the right side of "+x.op)
		if err != nil {
			return Null, err
		}
		return Bool(r), nil
	}
	l, err := ev.eval(x.l)
	if err != nil {
		return Null, err
	}
	r, err := ev.eval(x.r)
	if err != nil {
		return Null, err
	}
	if l.Kind == KNull || r.Kind == KNull {
		return Null, nil
	}
	switch x.op {
	case "+", "-":
		return addSub(x.p, x.op, l, r)
	case "*":
		return mul(x.p, l, r)
	case "/":
		return div(x.p, l, r)
	default:
		return compare(x.p, x.op, l, r)
	}
}

// unify brings two numeric operands to a common kind for + - and comparisons: an untyped
// literal adopts the other side's kind (and currency); a typed number never becomes money.
func unify(p int, l, r Value) (Value, Value, *Error) {
	switch {
	case l.Kind == KAmount && r.Kind == KAmount:
		if l.Currency != r.Currency {
			return l, r, errAt(p, "CURRENCY_MISMATCH", "%s and %s are in different currencies", l, r)
		}
	case l.Kind == KAmount && r.Kind == KNumber:
		if !r.Untyped {
			return l, r, errAt(p, "TYPE_MISMATCH", "cannot combine the amount %s with the computed number %s", l, r)
		}
		r = Amount(r.Num, l.Currency)
	case l.Kind == KNumber && r.Kind == KAmount:
		if !l.Untyped {
			return l, r, errAt(p, "TYPE_MISMATCH", "cannot combine the computed number %s with the amount %s", l, r)
		}
		l = Amount(l.Num, r.Currency)
	case l.Kind == KNumber && r.Kind == KNumber:
	default:
		return l, r, errAt(p, "TYPE_MISMATCH", "%s and %s are not numbers", l.Kind, r.Kind)
	}
	return l, r, nil
}

func addSub(p int, op string, l, r Value) (Value, error) {
	l, r, e := unify(p, l, r)
	if e != nil {
		return Null, e
	}
	out := l
	if op == "+" {
		out.Num = new(big.Rat).Add(l.Num, r.Num)
	} else {
		out.Num = new(big.Rat).Sub(l.Num, r.Num)
	}
	out.Untyped = l.Untyped && r.Untyped
	return out, nil
}

func mul(p int, l, r Value) (Value, error) {
	if !numeric(l) || !numeric(r) {
		return Null, errAt(p, "TYPE_MISMATCH", "cannot multiply %s by %s", l.Kind, r.Kind)
	}
	if l.Kind == KAmount && r.Kind == KAmount {
		return Null, errAt(p, "TYPE_MISMATCH", "cannot multiply two amounts (%s × %s)", l, r)
	}
	out := Value{Kind: KNumber, Num: new(big.Rat).Mul(l.Num, r.Num), Untyped: l.Untyped && r.Untyped}
	if l.Kind == KAmount {
		out.Kind, out.Currency, out.Untyped = KAmount, l.Currency, false
	} else if r.Kind == KAmount {
		out.Kind, out.Currency, out.Untyped = KAmount, r.Currency, false
	}
	return out, nil
}

func div(p int, l, r Value) (Value, error) {
	if !numeric(l) || !numeric(r) {
		return Null, errAt(p, "TYPE_MISMATCH", "cannot divide %s by %s", l.Kind, r.Kind)
	}
	if r.Num.Sign() == 0 {
		return Null, errAt(p, "DIVIDE_BY_ZERO", "division by zero (%s / %s)", l, r)
	}
	q := new(big.Rat).Quo(l.Num, r.Num)
	switch {
	case l.Kind == KAmount && r.Kind == KAmount:
		if l.Currency != r.Currency {
			return Null, errAt(p, "CURRENCY_MISMATCH", "%s and %s are in different currencies", l, r)
		}
		return Value{Kind: KNumber, Num: q}, nil // a ratio
	case l.Kind == KAmount:
		return Value{Kind: KAmount, Num: q, Currency: l.Currency}, nil
	case r.Kind == KAmount:
		return Null, errAt(p, "TYPE_MISMATCH", "cannot divide a number by an amount (%s / %s)", l, r)
	}
	return Value{Kind: KNumber, Num: q, Untyped: l.Untyped && r.Untyped}, nil
}

func compare(p int, op string, l, r Value) (Value, error) {
	var c int
	switch {
	case numeric(l) && numeric(r):
		a, b, e := unify(p, l, r)
		if e != nil {
			return Null, e
		}
		c = a.Num.Cmp(b.Num)
	case l.Kind == KString && r.Kind == KString, l.Kind == KDate && r.Kind == KDate:
		c = strings.Compare(l.Str, r.Str)
	case l.Kind == KBool && r.Kind == KBool && (op == "==" || op == "!="):
		if l.Bool != r.Bool {
			c = 1
		}
	default:
		return Null, errAt(p, "TYPE_MISMATCH", "cannot compare %s with %s", l.Kind, r.Kind)
	}
	var b bool
	switch op {
	case "==":
		b = c == 0
	case "!=":
		b = c != 0
	case "<":
		b = c < 0
	case "<=":
		b = c <= 0
	case ">":
		b = c > 0
	case ">=":
		b = c >= 0
	}
	return Bool(b), nil
}

// Round rounds r to scale decimals. HALF_UP rounds halves away from zero; HALF_EVEN rounds
// halves to the even neighbour (banker's rounding).
func Round(r *big.Rat, scale int, mode string) *big.Rat {
	pow := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil))
	x := new(big.Rat).Mul(r, pow)
	neg := x.Sign() < 0
	if neg {
		x.Neg(x)
	}
	q := new(big.Int).Quo(x.Num(), x.Denom())
	frac := new(big.Rat).Sub(x, new(big.Rat).SetInt(q))
	half := big.NewRat(1, 2)
	switch c := frac.Cmp(half); {
	case c > 0:
		q.Add(q, big.NewInt(1))
	case c == 0:
		if mode == "HALF_UP" || q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	out := new(big.Rat).Quo(new(big.Rat).SetInt(q), pow)
	if neg {
		out.Neg(out)
	}
	return out
}

func (ev *evaluator) call(x call) (Value, error) {
	switch x.name {
	case "IF":
		if len(x.args) != 3 {
			return Null, errAt(x.p, "ARITY", "IF(condition, then, else) takes 3 arguments")
		}
		c, err := ev.cond(x.args[0], "the IF condition")
		if err != nil {
			return Null, err
		}
		if c {
			return ev.eval(x.args[1])
		}
		return ev.eval(x.args[2])
	case "COALESCE":
		if len(x.args) < 2 {
			return Null, errAt(x.p, "ARITY", "COALESCE takes at least 2 arguments")
		}
		for _, a := range x.args {
			v, err := ev.eval(a)
			if err != nil {
				return Null, err
			}
			if v.Kind != KNull {
				return v, nil
			}
		}
		return Null, nil
	case "ISNULL":
		if len(x.args) != 1 {
			return Null, errAt(x.p, "ARITY", "ISNULL takes 1 argument")
		}
		v, err := ev.eval(x.args[0])
		if err != nil {
			return Null, err
		}
		return Bool(v.Kind == KNull), nil
	case "ROUND":
		return ev.round(x)
	case "ABS":
		if len(x.args) != 1 {
			return Null, errAt(x.p, "ARITY", "ABS takes 1 argument")
		}
		v, err := ev.eval(x.args[0])
		if err != nil || v.Kind == KNull {
			return v, err
		}
		if !numeric(v) {
			return Null, errAt(x.p, "TYPE_MISMATCH", "ABS needs a number, got %s", v.Kind)
		}
		out := v
		out.Num = new(big.Rat).Abs(v.Num)
		return out, nil
	case "MIN", "MAX":
		if len(x.args) < 2 {
			return Null, errAt(x.p, "ARITY", "%s takes at least 2 arguments", x.name)
		}
		var best Value
		for i, a := range x.args {
			v, err := ev.eval(a)
			if err != nil {
				return Null, err
			}
			if v.Kind == KNull {
				return Null, nil
			}
			if i == 0 {
				if !numeric(v) {
					return Null, errAt(x.p, "TYPE_MISMATCH", "%s needs numbers, got %s", x.name, v.Kind)
				}
				best = v
				continue
			}
			c, err := compare(x.p, "<", v, best)
			if err != nil {
				return Null, err
			}
			if (x.name == "MIN" && c.Bool) || (x.name == "MAX" && !c.Bool) {
				b2, _, _ := unify(x.p, v, best)
				best = b2
			}
		}
		return best, nil
	case "IN":
		if len(x.args) < 2 {
			return Null, errAt(x.p, "ARITY", "IN(value, candidate, …) takes at least 2 arguments")
		}
		v, err := ev.eval(x.args[0])
		if err != nil || v.Kind == KNull {
			return v, err
		}
		for _, a := range x.args[1:] {
			c, err := ev.eval(a)
			if err != nil {
				return Null, err
			}
			if c.Kind == KNull {
				continue
			}
			eq, err := compare(x.p, "==", v, c)
			if err != nil {
				return Null, err
			}
			if eq.Bool {
				return Bool(true), nil
			}
		}
		return Bool(false), nil
	case "LIKE":
		if len(x.args) != 2 {
			return Null, errAt(x.p, "ARITY", "LIKE(value, 'pattern') takes 2 arguments")
		}
		pat, ok := x.args[1].(strLit)
		if !ok {
			return Null, errAt(x.p, "TYPE_MISMATCH", "the LIKE pattern must be a string literal (* = any run of characters, ? = one character)")
		}
		v, err := ev.eval(x.args[0])
		if err != nil || v.Kind == KNull {
			return v, err
		}
		if v.Kind != KString {
			return Null, errAt(x.p, "TYPE_MISMATCH", "LIKE needs a string, got %s", v.Kind)
		}
		return Bool(Glob(pat.s, v.Str)), nil
	case "SUM":
		return ev.sum(x)
	case "COUNT":
		if len(x.args) != 1 {
			return Null, errAt(x.p, "ARITY", "COUNT(LIST) takes 1 argument")
		}
		id, ok := x.args[0].(ident)
		if !ok {
			return Null, errAt(x.p, "TYPE_MISMATCH", "COUNT takes a list name")
		}
		items, ok := ev.env.List(id.name)
		if !ok {
			return Null, errAt(id.p, "UNKNOWN_IDENTIFIER", "%s is not a list here", id.name)
		}
		v := Value{Kind: KNumber, Num: big.NewRat(int64(len(items)), 1)}
		ev.recordRead(id.name+"[]", v)
		return v, nil
	}
	args := make([]Value, 0, len(x.args))
	for _, a := range x.args {
		v, err := ev.eval(a)
		if err != nil {
			return Null, err
		}
		args = append(args, v)
	}
	v, ok, err := ev.env.Call(x.name, args)
	if !ok {
		return Null, errAt(x.p, "UNKNOWN_FUNCTION", "%s is not a function available here", x.name)
	}
	if err != nil {
		if fe, isFE := err.(*Error); isFE {
			if fe.Pos == 0 {
				fe.Pos = x.p
			}
			return Null, fe
		}
		return Null, errAt(x.p, "FUNCTION_ERROR", "%s: %v", x.name, err)
	}
	ct := CallTrace{Name: x.name, Result: v.String()}
	for _, a := range args {
		ct.Args = append(ct.Args, a.String())
	}
	ev.trace.Calls = append(ev.trace.Calls, ct)
	return v, nil
}

func (ev *evaluator) round(x call) (Value, error) {
	if len(x.args) < 2 || len(x.args) > 3 {
		return Null, errAt(x.p, "ARITY", "ROUND(value, places[, 'HALF_UP'|'HALF_EVEN']) takes 2 or 3 arguments")
	}
	v, err := ev.eval(x.args[0])
	if err != nil {
		return Null, err
	}
	sc, ok := x.args[1].(numLit)
	if !ok {
		return Null, errAt(x.p, "TYPE_MISMATCH", "ROUND places must be a whole-number literal")
	}
	scale := 0
	for _, c := range sc.text {
		if c < '0' || c > '9' || scale > 10 {
			return Null, errAt(sc.p, "TYPE_MISMATCH", "ROUND places must be a whole number from 0 to 10")
		}
		scale = scale*10 + int(c-'0')
	}
	if scale > 10 {
		return Null, errAt(sc.p, "TYPE_MISMATCH", "ROUND places must be a whole number from 0 to 10")
	}
	mode := ev.opt.DefaultRounding
	if len(x.args) == 3 {
		m, ok := x.args[2].(strLit)
		if !ok || (m.s != "HALF_UP" && m.s != "HALF_EVEN") {
			return Null, errAt(x.p, "TYPE_MISMATCH", "ROUND mode must be 'HALF_UP' or 'HALF_EVEN'")
		}
		mode = m.s
	}
	if v.Kind == KNull {
		return Null, nil
	}
	if !numeric(v) {
		return Null, errAt(x.p, "TYPE_MISMATCH", "ROUND needs a number, got %s", v.Kind)
	}
	out := v
	out.Num = Round(v.Num, scale, mode)
	ev.trace.Roundings = append(ev.trace.Roundings, Rounding{Scale: scale, Mode: mode, Before: v.String(), After: out.String()})
	return out, nil
}

func (ev *evaluator) sum(x call) (Value, error) {
	if len(x.args) != 1 {
		return Null, errAt(x.p, "ARITY", "SUM(LIST.FIELD) takes 1 argument")
	}
	pth, ok := x.args[0].(path)
	if !ok {
		return Null, errAt(x.p, "TYPE_MISMATCH", "SUM takes a list field, e.g. SUM(ALLOCATION_LIST.ALLOC_AMT)")
	}
	items, ok := ev.env.List(pth.list)
	if !ok {
		return Null, errAt(pth.p, "UNKNOWN_IDENTIFIER", "%s is not a list here", pth.list)
	}
	total := Value{Kind: KNumber, Num: new(big.Rat), Untyped: true}
	for i, it := range items {
		v, ok := it.Lookup(pth.field)
		if !ok {
			return Null, errAt(pth.p, "UNKNOWN_IDENTIFIER", "%s item %d has no %s", pth.list, i, pth.field)
		}
		if v.Kind == KNull {
			return Null, errAt(pth.p, "NULL_IN_AGGREGATE", "%s item %d has no value for %s; an aggregate never skips data silently", pth.list, i, pth.field)
		}
		s, err := addSub(pth.p, "+", total, v)
		if err != nil {
			return Null, err
		}
		total = s
	}
	ev.recordRead("SUM("+pth.list+"."+pth.field+")", total)
	return total, nil
}
