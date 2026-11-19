package formula

import (
	"reflect"
	"strings"
	"testing"
)

// testEnv is a map-backed Env.
type testEnv struct {
	vals  map[string]Value
	lists map[string][]Item
	funcs map[string]func([]Value) (Value, error)
}

type mapItem map[string]Value

func (m mapItem) Lookup(f string) (Value, bool) { v, ok := m[f]; return v, ok }

func (e testEnv) Lookup(n string) (Value, bool) { v, ok := e.vals[n]; return v, ok }
func (e testEnv) List(n string) ([]Item, bool)  { l, ok := e.lists[n]; return l, ok }
func (e testEnv) Call(n string, a []Value) (Value, bool, error) {
	f, ok := e.funcs[n]
	if !ok {
		return Null, false, nil
	}
	v, err := f(a)
	return v, true, err
}

func aud(s string) Value { return Amount(MustDecimal(s), "AUD") }

func p1() testEnv {
	return testEnv{
		vals: map[string]Value{
			"PREMIUM": aud("1000.00"), "TAX": aud("100.00"), "PAY_AMT": aud("1099.50"),
			"USD_FEE": Amount(MustDecimal("5.00"), "USD"), "RATE": Number(MustDecimal("0.1")),
			"LOB": String("MOTOR"), "EFF": Date("2026-02-01"), "TXN": Date("2026-01-20"),
			"MISSING": Null, "ZERO": aud("0.00"),
		},
		lists: map[string][]Item{
			"ALLOCATION_LIST": {mapItem{"ALLOC_AMT": aud("550.00")}, mapItem{"ALLOC_AMT": aud("549.50")}},
			"EMPTY_LIST":      {},
		},
	}
}

func eval(t *testing.T, src string, env Env) (Value, Trace, error) {
	t.Helper()
	n, perr := Parse(src)
	if perr != nil {
		return Null, Trace{}, perr
	}
	return Eval(n, env, Options{})
}

func mustEval(t *testing.T, src string) Value {
	t.Helper()
	v, _, err := eval(t, src, p1())
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return v
}

func code(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	if err == nil {
		return ""
	}
	return "?"
}

// AC: GROSS = PREMIUM + TAX computes 1,100 for the demo.
func TestGrossIsPremiumPlusTax(t *testing.T) {
	v := mustEval(t, "PREMIUM + TAX")
	if v.Kind != KAmount || v.Currency != "AUD" || v.String() != "1100 AUD" {
		t.Fatalf("got %v", v)
	}
	if s, err := Exact(v.Num, 2); err != nil || s != "1100.00" {
		t.Errorf("at 2 places: %q %v", s, err)
	}
}

func TestArithmeticIsExactAndFollowsPrecedence(t *testing.T) {
	cases := map[string]string{
		"0.1 + 0.2":                      "0.3",
		"1 + 2 * 3":                      "7",
		"(1 + 2) * 3":                    "9",
		"10 - 4 - 3":                     "3",
		"100 / 8":                        "12.5",
		"-2 * -3":                        "6",
		"1 / 3 * 3":                      "1",
		"PREMIUM * 0.1":                  "100 AUD",
		"PREMIUM * RATE":                 "100 AUD",
		"PAY_AMT - 550.00":               "549.5 AUD",
		"SUM(ALLOCATION_LIST.ALLOC_AMT)": "1099.5 AUD",
		"COUNT(ALLOCATION_LIST)":         "2",
		"PREMIUM / TAX":                  "10",
		"MAX(PREMIUM, TAX, 0)":           "1000 AUD",
		"MIN(PREMIUM, TAX)":              "100 AUD",
		"ABS(0 - PREMIUM)":               "1000 AUD",
		"SUM(EMPTY_LIST.ALLOC_AMT)":      "0",
	}
	for src, want := range cases {
		if got := mustEval(t, src).String(); got != want {
			t.Errorf("%s = %s, want %s", src, got, want)
		}
	}
}

func TestConditionsAndComparisons(t *testing.T) {
	cases := map[string]string{
		"IF(LOB == 'MOTOR', PREMIUM, 0)":   "1000 AUD",
		"IF(LOB != 'MOTOR', PREMIUM, 0)":   "0", // the untyped literal; Coerce gives it the event currency
		"EFF > TXN":                        "true",
		"PREMIUM >= 1000 AND TAX < 100.01": "true",
		"NOT (PREMIUM < 1) OR MISSING > 1": "true", // short-circuit: MISSING never compared
		"COALESCE(MISSING, TAX)":           "100 AUD",
		"ISNULL(MISSING)":                  "true",
		"IF(ISNULL(MISSING), 0, MISSING)":  "0",
		"'O''BRIEN' == 'O''BRIEN'":         "true",
		"PREMIUM <> TAX":                   "true",
	}
	for src, want := range cases {
		if got := mustEval(t, src).String(); got != want {
			t.Errorf("%s = %s, want %s", src, got, want)
		}
	}
}

// Tests: rounding (both modes, both signs) — signed reversals round symmetrically.
func TestRounding(t *testing.T) {
	cases := map[string]string{
		"ROUND(2.345, 2)":               "2.35",
		"ROUND(2.345, 2, 'HALF_EVEN')":  "2.34",
		"ROUND(2.355, 2, 'HALF_EVEN')":  "2.36",
		"ROUND(-2.345, 2)":              "-2.35", // half away from zero
		"ROUND(-2.345, 2, 'HALF_EVEN')": "-2.34",
		"ROUND(1 / 3, 2)":               "0.33",
		"ROUND(2 / 3, 2)":               "0.67",
		"ROUND(PREMIUM / 3, 2)":         "333.33 AUD",
		"ROUND(0 - PREMIUM / 3, 2)":     "-333.33 AUD",
		"ROUND(12.5, 0)":                "13",
		"ROUND(12.5, 0, 'HALF_EVEN')":   "12",
		"ROUND(MISSING, 2)":             "null",
	}
	for src, want := range cases {
		if got := mustEval(t, src).String(); got != want {
			t.Errorf("%s = %s, want %s", src, got, want)
		}
	}
	// A reversal is the exact negation of the original, before and after rounding.
	orig := mustEval(t, "ROUND(PREMIUM * 0.0725, 2)")
	rev := mustEval(t, "ROUND(0 - PREMIUM * 0.0725, 2)")
	if orig.String() != "72.5 AUD" || rev.String() != "-72.5 AUD" {
		t.Errorf("signed reversal: %s / %s", orig, rev)
	}
}

// Tests: nulls propagate through arithmetic, are an error in a condition or an aggregate,
// and are only replaced when the expression says so.
func TestNullSemantics(t *testing.T) {
	for _, src := range []string{"MISSING + 1", "PREMIUM * MISSING", "-MISSING", "MISSING > 1", "MIN(PREMIUM, MISSING)"} {
		if v := mustEval(t, src); v.Kind != KNull {
			t.Errorf("%s should be null, got %v", src, v)
		}
	}
	for _, src := range []string{"IF(MISSING > 1, 1, 2)", "NOT MISSING", "MISSING > 1 AND TRUE"} {
		if _, _, err := eval(t, src, p1()); code(err) != "NULL_CONDITION" {
			t.Errorf("%s: want NULL_CONDITION, got %v", src, err)
		}
	}
	env := p1()
	env.lists["HOLEY"] = []Item{mapItem{"ALLOC_AMT": aud("1.00")}, mapItem{"ALLOC_AMT": Null}}
	if _, _, err := eval(t, "SUM(HOLEY.ALLOC_AMT)", env); code(err) != "NULL_IN_AGGREGATE" {
		t.Errorf("a null inside SUM must be an error, got %v", err)
	}
}

// AC: divide by zero and currency mismatches produce explicit errors.
func TestExplicitErrors(t *testing.T) {
	cases := map[string]string{
		"PREMIUM / 0":               "DIVIDE_BY_ZERO",
		"PREMIUM / ZERO":            "DIVIDE_BY_ZERO",
		"PREMIUM + USD_FEE":         "CURRENCY_MISMATCH",
		"PREMIUM > USD_FEE":         "CURRENCY_MISMATCH",
		"PREMIUM / USD_FEE":         "CURRENCY_MISMATCH",
		"PREMIUM * TAX":             "TYPE_MISMATCH",
		"PREMIUM + RATE":            "TYPE_MISMATCH", // a computed number is not money
		"PREMIUM + LOB":             "TYPE_MISMATCH",
		"LOB > 1":                   "TYPE_MISMATCH",
		"IF(PREMIUM, 1, 2)":         "TYPE_MISMATCH",
		"1 / PREMIUM":               "TYPE_MISMATCH",
		"NOPE + 1":                  "UNKNOWN_IDENTIFIER",
		"FROBNICATE(PREMIUM)":       "UNKNOWN_FUNCTION",
		"ALLOCATION_LIST.ALLOC_AMT": "LIST_OUTSIDE_AGGREGATE",
		"IF(TRUE, 1)":               "ARITY",
		"ROUND(PREMIUM, 2, 'UP')":   "TYPE_MISMATCH",
		"ROUND(PREMIUM, TAX)":       "TYPE_MISMATCH",
	}
	for src, want := range cases {
		if _, _, err := eval(t, src, p1()); code(err) != want {
			t.Errorf("%s: want %s, got %v", src, want, err)
		}
	}
	env := p1()
	env.lists["MIXED"] = []Item{mapItem{"ALLOC_AMT": aud("1.00")}, mapItem{"ALLOC_AMT": Amount(MustDecimal("1.00"), "USD")}}
	if _, _, err := eval(t, "SUM(MIXED.ALLOC_AMT)", env); code(err) != "CURRENCY_MISMATCH" {
		t.Errorf("mixed currencies in a list: %v", err)
	}
	if _, err := Exact(MustDecimal("1"), 2); err != nil {
		t.Error(err)
	}
	if _, err := Exact(mustEval(t, "PREMIUM / 3").Num, 2); code(err) != "NOT_REPRESENTABLE" {
		t.Errorf("1000/3 at 2 places must need explicit rounding, got %v", err)
	}
}

// No arbitrary code: anything that is not the language is refused at parse time.
func TestNotALanguageForCode(t *testing.T) {
	for _, src := range []string{
		"__import__('os')", "PREMIUM; DROP TABLE gl.config_version", "eval('1')", "1e3", "0x10", "PREMIUM.__class__",
		"premium + tax", "exec(PREMIUM)", "PREMIUM ? 1 : 2", "{1}", "PREMIUM[0]", "`ls`", "1..2", "", "   ",
	} {
		n, perr := Parse(src)
		if perr == nil {
			if _, _, err := Eval(n, p1(), Options{}); err == nil {
				t.Errorf("%q was accepted and evaluated", src)
			}
		}
	}
}

func TestBounds(t *testing.T) {
	if _, e := Parse(strings.Repeat("1+", MaxLength)); e == nil || e.Code != "TOO_COMPLEX" {
		t.Errorf("over-long expression: %v", e)
	}
	if _, e := Parse(strings.Repeat("(", 60) + "1" + strings.Repeat(")", 60)); e == nil || e.Code != "TOO_COMPLEX" {
		t.Errorf("over-deep expression: %v", e)
	}
	if _, e := Parse(strings.Repeat("1+", MaxNodes) + "1"); e == nil || e.Code != "TOO_COMPLEX" {
		t.Errorf("too many nodes: %v", e)
	}
}

// Host functions are called only if the caller provides them, and appear in the lineage.
func TestHostFunctionsAndLineage(t *testing.T) {
	env := p1()
	env.funcs = map[string]func([]Value) (Value, error){
		"OPEN_BALANCE": func(a []Value) (Value, error) { return aud("0.50"), nil },
	}
	v, tr, err := eval(t, "ROUND(OPEN_BALANCE('INV2') + PREMIUM * 0, 2)", env)
	if err != nil || v.String() != "0.5 AUD" {
		t.Fatalf("%v %v", v, err)
	}
	if len(tr.Calls) != 1 || tr.Calls[0].Name != "OPEN_BALANCE" || tr.Calls[0].Args[0] != "'INV2'" || tr.Calls[0].Result != "0.5 AUD" {
		t.Errorf("calls: %+v", tr.Calls)
	}
	if len(tr.Reads) != 1 || tr.Reads[0] != (Read{"PREMIUM", "1000 AUD"}) {
		t.Errorf("reads: %+v", tr.Reads)
	}
	if len(tr.Roundings) != 1 || tr.Roundings[0].Mode != "HALF_UP" {
		t.Errorf("roundings: %+v", tr.Roundings)
	}
	_, tr2, _ := eval(t, "PREMIUM + TAX + PREMIUM", p1())
	if !reflect.DeepEqual(tr2.Reads, []Read{{"PREMIUM", "1000 AUD"}, {"TAX", "100 AUD"}}) {
		t.Errorf("each input is recorded once, in first-use order: %+v", tr2.Reads)
	}
}

func TestDeterministic(t *testing.T) {
	src := "ROUND(IF(LOB == 'MOTOR', PREMIUM * 1.0725, TAX) + SUM(ALLOCATION_LIST.ALLOC_AMT), 2, 'HALF_EVEN')"
	a, ta, ea := eval(t, src, p1())
	for i := 0; i < 50; i++ {
		b, tb, eb := eval(t, src, p1())
		if a.String() != b.String() || !reflect.DeepEqual(ta, tb) || code(ea) != code(eb) {
			t.Fatalf("run %d differs: %v %v", i, b, tb)
		}
	}
}

func TestIdentifiersAndCalls(t *testing.T) {
	n, _ := Parse("IF(LOB == 'MOTOR', PREMIUM + TAX, SUM(ALLOCATION_LIST.ALLOC_AMT)) + COUNT(INVOICE_LIST) + LOOKUP('X', BANK_ACCOUNT)")
	if got := Identifiers(n); !reflect.DeepEqual(got, []string{"LOB", "PREMIUM", "TAX", "ALLOCATION_LIST.ALLOC_AMT", "INVOICE_LIST[]", "BANK_ACCOUNT"}) {
		t.Errorf("identifiers: %v", got)
	}
	if got := Calls(n); !reflect.DeepEqual(got, []string{"IF", "SUM", "COUNT", "LOOKUP"}) {
		t.Errorf("calls: %v", got)
	}
}

func TestStaticCheck(t *testing.T) {
	syms := Symbols{
		Fields: map[string]Kind{"PREMIUM": KAmount, "TAX": KAmount, "RATE": KNumber, "LOB": KString, "EFF": KDate, "PAY_AMT": KAmount},
		Lists:  map[string]map[string]Kind{"ALLOCATION_LIST": {"ALLOC_AMT": KAmount, "INVOICE_NO": KString}},
		Funcs:  map[string]FuncSig{"OPEN_BALANCE": {Args: []Kind{KString}, Result: KAmount}},
	}
	ok := map[string]Kind{
		"PREMIUM + TAX":                             KAmount,
		"PREMIUM * RATE":                            KAmount,
		"PREMIUM / TAX":                             KNumber,
		"IF(LOB == 'MOTOR', PREMIUM, 0)":            KAmount,
		"SUM(ALLOCATION_LIST.ALLOC_AMT) == PAY_AMT": KBool,
		"OPEN_BALANCE('INV1')":                      KAmount,
		"COUNT(ALLOCATION_LIST) > 0":                KBool,
		"COALESCE(PREMIUM, 0)":                      KAmount,
	}
	for src, want := range ok {
		n, perr := Parse(src)
		if perr != nil {
			t.Fatalf("%s: %v", src, perr)
		}
		if typ, errs := Check(n, syms); len(errs) > 0 || typ.Kind != want {
			t.Errorf("%s: type %v errs %v, want %v", src, typ.Kind, errs, want)
		}
	}
	bad := map[string]string{
		"PREMIUM + RATE":                  "TYPE_MISMATCH",
		"PREMIUM * TAX":                   "TYPE_MISMATCH",
		"NOPE":                            "UNKNOWN_IDENTIFIER",
		"ALLOCATION_LIST":                 "LIST_OUTSIDE_AGGREGATE",
		"ALLOCATION_LIST.ALLOC_AMT + 1":   "LIST_OUTSIDE_AGGREGATE",
		"SUM(ALLOCATION_LIST.INVOICE_NO)": "TYPE_MISMATCH",
		"IF(LOB, 1, 2)":                   "TYPE_MISMATCH",
		"IF(TRUE, PREMIUM, 'X')":          "TYPE_MISMATCH",
		"PREMIUM / 0":                     "DIVIDE_BY_ZERO",
		"OPEN_BALANCE(1)":                 "TYPE_MISMATCH",
		"FROB(1)":                         "UNKNOWN_FUNCTION",
		"EFF > 1":                         "TYPE_MISMATCH",
	}
	for src, want := range bad {
		n, perr := Parse(src)
		if perr != nil {
			t.Fatalf("%s: %v", src, perr)
		}
		_, errs := Check(n, syms)
		found := false
		for _, e := range errs {
			found = found || e.Code == want
		}
		if !found {
			t.Errorf("%s: want %s, got %v", src, want, errs)
		}
	}
	// Several problems are all reported, not just the first.
	n, _ := Parse("NOPE + ALSO_NOPE")
	if _, errs := Check(n, syms); len(errs) != 2 {
		t.Errorf("want both unknown names reported, got %v", errs)
	}
}

// Lazily evaluated branches can yield an untyped literal; Coerce gives it the declared type
// at the point of use, and refuses anything that is not the declared type.
func TestCoerceGivesLiteralsTheirDeclaredType(t *testing.T) {
	v := mustEval(t, "IF(LOB != 'MOTOR', PREMIUM, 0)")
	c, err := Coerce(v, KAmount, "AUD")
	if err != nil || c.String() != "0 AUD" {
		t.Fatalf("coerced: %v %v", c, err)
	}
	if _, err := Coerce(mustEval(t, "PREMIUM / TAX"), KAmount, "AUD"); code(err) != "TYPE_MISMATCH" {
		t.Errorf("a computed ratio became money: %v", err)
	}
	if _, err := Coerce(mustEval(t, "PREMIUM"), KAmount, "USD"); code(err) != "CURRENCY_MISMATCH" {
		t.Errorf("an AUD amount was accepted as USD: %v", err)
	}
	if _, err := Coerce(mustEval(t, "LOB"), KAmount, "AUD"); code(err) != "TYPE_MISMATCH" {
		t.Errorf("a string became money: %v", err)
	}
	if n, err := Coerce(mustEval(t, "MISSING"), KAmount, "AUD"); err != nil || n.Kind != KNull {
		t.Errorf("null stays null: %v %v", n, err)
	}
}

// Filter operators for dimensions: IN and LIKE (glob wildcards).
func TestInAndLikeOperators(t *testing.T) {
	env := p1()
	env.vals["PRODUCT"] = String("MOTOR_PRIVATE")
	cases := map[string]string{
		"IN(LOB, 'MOTOR', 'HOME')":                     "true",
		"IN(LOB, 'HOME', 'COMMERCIAL')":                "false",
		"NOT IN(LOB, 'HOME')":                          "true",
		"LIKE(PRODUCT, 'MOTOR_*')":                     "true",
		"LIKE(PRODUCT, 'MOTOR_?RIVATE')":               "true",
		"LIKE(PRODUCT, '*PRIVATE')":                    "true",
		"LIKE(PRODUCT, 'HOME*')":                       "false",
		"LIKE(PRODUCT, 'motor_*')":                     "false", // case-sensitive: values are canonical
		"LIKE(LOB, '*')":                               "true",
		"IN(PREMIUM, 1000, 2000)":                      "true",
		"IN(MISSING, 'X')":                             "null",
		"IN(LOB, 'MOTOR') AND LIKE(PRODUCT, 'MOTOR*')": "true",
	}
	for src, want := range cases {
		v, _, err := eval(t, src, env)
		if err != nil || v.String() != want {
			t.Errorf("%s = %v (%v), want %s", src, v, err, want)
		}
	}
	for src, want := range map[string]string{"LIKE(PREMIUM, '1*')": "TYPE_MISMATCH", "LIKE(LOB, LOB)": "TYPE_MISMATCH", "IN(LOB)": "ARITY", "IN(LOB, 1)": "TYPE_MISMATCH"} {
		if _, _, err := eval(t, src, env); code(err) != want {
			t.Errorf("%s: want %s, got %v", src, want, err)
		}
	}
	for p, s := range map[string]string{"": "", "*": "", "a*b*c": "aXbYc", "?": "x"} {
		if !Glob(p, s) {
			t.Errorf("Glob(%q, %q) should match", p, s)
		}
	}
	for p, s := range map[string]string{"": "x", "a*b": "acd", "?": "", "a?": "abc"} {
		if Glob(p, s) {
			t.Errorf("Glob(%q, %q) should not match", p, s)
		}
	}
	n, _ := Parse("LOB == 'MOTOR' OR IN(COUNTRY, 'AU', 'NZ') OR 'C01' != COMPANY")
	got := StringComparisons(n)
	if len(got) != 3 || got[0].Field != "LOB" || len(got[1].Values) != 2 || got[2].Field != "COMPANY" {
		t.Errorf("comparisons: %+v", got)
	}
}
