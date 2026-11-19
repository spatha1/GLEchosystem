package model

import (
	"strings"
	"testing"
)

func withDerived(c map[string]any, dms ...map[string]any) map[string]any {
	var items []any
	for _, d := range dms {
		items = append(items, d)
	}
	c["derivedMeasures"] = items
	return c
}

func gross() map[string]any {
	return map[string]any{"code": "GROSS", "formula": "PREMIUM + TAX", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}
}

var ref = ConfigRef{ConfigID: "DEMO_GL", Version: 1, ContentDigest: "sha256:" + strings.Repeat("a", 64)}

func e1() EventInput {
	return EventInput{EventType: "PREM_ADDED", Currency: "AUD", Measures: map[string]string{"PREMIUM": "1000.00", "TAX": "100.00"}}
}

func byCode(ds []Derived) map[string]Derived {
	out := map[string]Derived{}
	for _, d := range ds {
		out[d.Code] = d
	}
	return out
}

// AC: GROSS = PREMIUM + TAX computes 1,100 for the demo — configured, validated and evaluated.
func TestGrossComputes1100ForTheDemo(t *testing.T) {
	doc, fs := parse(t, withDerived(p1Catalog("POLICY"), gross()))
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	got, err := EvaluateDerived(doc, ref, e1())
	if err != nil {
		t.Fatal(err)
	}
	g := byCode(got)["GROSS"]
	if g.Value != "1100 AUD" {
		t.Fatalf("GROSS = %s", g.Value)
	}
	if s := g.Val().Num.FloatString(2); s != "1100.00" {
		t.Errorf("at 2 places: %s", s)
	}
}

// AC: formula versions and calculation lineage are visible — each result carries the formula,
// its digest, the configuration version and every input it read.
func TestLineageNamesFormulaVersionAndInputs(t *testing.T) {
	v1, _ := parse(t, withDerived(p1Catalog("POLICY"), gross()))
	g2 := gross()
	g2["formula"] = "ROUND((PREMIUM + TAX) * 1.02, 2)"
	v2, fs := parse(t, withDerived(p1Catalog("POLICY"), g2))
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	a, _ := EvaluateDerived(v1, ConfigRef{"DEMO_GL", 1, ref.ContentDigest}, e1())
	b, _ := EvaluateDerived(v2, ConfigRef{"DEMO_GL", 2, "sha256:" + strings.Repeat("b", 64)}, e1())
	ga, gb := byCode(a)["GROSS"], byCode(b)["GROSS"]
	if ga.Config.Version != 1 || gb.Config.Version != 2 || ga.FormulaDigest == gb.FormulaDigest || ga.FormulaDigest != FormulaDigest("PREMIUM + TAX") {
		t.Errorf("versions/digests not distinguishable: %+v / %+v", ga, gb)
	}
	if gb.Value != "1122 AUD" || len(gb.Trace.Roundings) != 1 {
		t.Errorf("v2 GROSS = %s, roundings %+v", gb.Value, gb.Trace.Roundings)
	}
	reads := map[string]string{}
	for _, r := range ga.Trace.Reads {
		reads[r.Name] = r.Value
	}
	if reads["PREMIUM"] != "1000 AUD" || reads["TAX"] != "100 AUD" || len(reads) != 2 {
		t.Errorf("lineage reads: %+v", ga.Trace.Reads)
	}
}

// Derived measures can build on each other; evaluation follows dependency order whatever the
// declaration order, and list aggregates read every item.
func TestDerivedMeasuresChainAndAggregate(t *testing.T) {
	c := withDerived(p1Catalog("POLICY"),
		map[string]any{"code": "NET_OF_TAX_RATIO", "formula": "PREMIUM / GROSS", "resultType": "NUMBER", "eventTypes": []any{"PREM_ADDED"}},
		gross(),
		map[string]any{"code": "ALLOCATED", "formula": "SUM(ALLOCATION_LIST.ALLOC_AMT)", "resultType": "AMOUNT"},
		map[string]any{"code": "UNAPPLIED", "formula": "PAY_AMT - ALLOCATED", "resultType": "AMOUNT", "eventTypes": []any{"PAYMENT_RECEIVED"}},
	)
	doc, fs := parse(t, c)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	got, err := EvaluateDerived(doc, ref, e1())
	if err != nil {
		t.Fatal(err)
	}
	if r := byCode(got)["NET_OF_TAX_RATIO"].Value; r != "10/11" {
		t.Errorf("ratio = %s (exact 10/11 expected)", r)
	}
	pay := EventInput{EventType: "PAYMENT_RECEIVED", Currency: "AUD", Measures: map[string]string{"PAY_AMT": "1099.50"},
		Lists: map[string][]map[string]string{"ALLOCATION_LIST": {{"ALLOC_AMT": "550.00"}, {"ALLOC_AMT": "549.50"}}}}
	got, err = EvaluateDerived(doc, ref, pay)
	if err != nil {
		t.Fatal(err)
	}
	m := byCode(got)
	if m["ALLOCATED"].Value != "1099.5 AUD" || m["UNAPPLIED"].Value != "0 AUD" {
		t.Errorf("payment: allocated %s, unapplied %s", m["ALLOCATED"].Value, m["UNAPPLIED"].Value)
	}
	if _, applies := m["GROSS"]; applies {
		t.Error("GROSS is scoped to PREM_ADDED and must not run for a payment")
	}
}

// AC + tests: dependency cycles are rejected with their path.
func TestCyclesAreRejected(t *testing.T) {
	c := withDerived(p1Catalog("POLICY"),
		map[string]any{"code": "A1", "formula": "B1 + PREMIUM", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}},
		map[string]any{"code": "B1", "formula": "C1", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}},
		map[string]any{"code": "C1", "formula": "A1 - TAX", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}},
		map[string]any{"code": "SELF", "formula": "SELF + 1", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}},
	)
	doc, fs := parse(t, c)
	var cyc []string
	for _, f := range fs {
		if f.Code == "CYCLE" {
			cyc = append(cyc, f.Message)
		}
	}
	joined := strings.Join(cyc, "|")
	if len(cyc) != 2 || !strings.Contains(joined, "A1 → B1 → C1 → A1") || !strings.Contains(joined, "SELF → SELF") {
		t.Fatalf("cycles: %v", cyc)
	}
	if _, err := EvaluateDerived(doc, ref, e1()); err == nil || !strings.Contains(err.Error(), "CYCLE") {
		t.Errorf("evaluation must refuse a cyclic configuration, got %v", err)
	}
}

// AC: divide by zero and currency mismatches are explicit errors at evaluation.
func TestRuntimeErrorsAreExplicit(t *testing.T) {
	doc, fs := parse(t, withDerived(p1Catalog("POLICY"),
		map[string]any{"code": "TAX_RATE", "formula": "TAX / PREMIUM", "resultType": "NUMBER", "eventTypes": []any{"PREM_ADDED"}},
		map[string]any{"code": "ALLOCATED", "formula": "SUM(ALLOCATION_LIST.ALLOC_AMT)", "resultType": "AMOUNT"},
	))
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	in := e1()
	in.Measures["PREMIUM"] = "0.00"
	if _, err := EvaluateDerived(doc, ref, in); err == nil || !strings.Contains(err.Error(), "DIVIDE_BY_ZERO") {
		t.Errorf("division by a zero premium: %v", err)
	}
	pay := EventInput{EventType: "PAYMENT_RECEIVED", Currency: "AUD", Measures: map[string]string{"PAY_AMT": "10.00"},
		Lists: map[string][]map[string]string{"ALLOCATION_LIST": {{"ALLOC_AMT": "5.00"}, {"ALLOC_AMT": "5.00", "@currency": "USD"}}}}
	if _, err := EvaluateDerived(doc, ref, pay); err == nil || !strings.Contains(err.Error(), "CURRENCY_MISMATCH") {
		t.Errorf("mixed currencies: %v", err)
	}
	for name, bad := range map[string]EventInput{
		"float money":   {EventType: "PREM_ADDED", Currency: "AUD", Measures: map[string]string{"PREMIUM": "1e3"}},
		"unknown field": {EventType: "PREM_ADDED", Currency: "AUD", Measures: map[string]string{"PREMIUM": "1", "BONUS": "1"}},
		"bad currency":  {EventType: "PREM_ADDED", Currency: "aud", Measures: map[string]string{"PREMIUM": "1"}},
		"unknown event": {EventType: "NOPE", Currency: "AUD"},
	} {
		if _, err := EvaluateDerived(doc, ref, bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Tests: nulls (missing inputs) propagate unless the formula handles them; signed reversals
// are exact negations.
func TestNullsAndSignedReversals(t *testing.T) {
	doc, _ := parse(t, withDerived(p1Catalog("POLICY"), gross(),
		map[string]any{"code": "GROSS_SAFE", "formula": "PREMIUM + COALESCE(TAX, 0)", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}))
	in := e1()
	delete(in.Measures, "TAX")
	got, err := EvaluateDerived(doc, ref, in)
	if err != nil {
		t.Fatal(err)
	}
	m := byCode(got)
	if m["GROSS"].Value != "null" || m["GROSS_SAFE"].Value != "1000 AUD" {
		t.Errorf("missing TAX: GROSS %s (want null), GROSS_SAFE %s", m["GROSS"].Value, m["GROSS_SAFE"].Value)
	}
	rev := EventInput{EventType: "PREM_ADDED", Currency: "AUD", Measures: map[string]string{"PREMIUM": "-1000.00", "TAX": "-100.00"}}
	got, _ = EvaluateDerived(doc, ref, rev)
	if v := byCode(got)["GROSS"].Value; v != "-1100 AUD" {
		t.Errorf("reversal GROSS = %s", v)
	}
}

// Static validation catches formula problems before approval, at the item's path.
func TestFormulaProblemsBlockValidation(t *testing.T) {
	cases := map[string]struct {
		dm   map[string]any
		code string
	}{
		"unknown field":          {map[string]any{"code": "X", "formula": "PREMIUM + BONUS", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}, "UNKNOWN_IDENTIFIER"},
		"amount × amount":        {map[string]any{"code": "X", "formula": "PREMIUM * TAX", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}, "TYPE_MISMATCH"},
		"declared wrong type":    {map[string]any{"code": "X", "formula": "PREMIUM + TAX", "resultType": "NUMBER", "eventTypes": []any{"PREM_ADDED"}}, "RESULT_TYPE_MISMATCH"},
		"bad result type":        {map[string]any{"code": "X", "formula": "PREMIUM", "resultType": "MONEY", "eventTypes": []any{"PREM_ADDED"}}, "INVALID_RESULT_TYPE"},
		"syntax":                 {map[string]any{"code": "X", "formula": "PREMIUM +", "resultType": "AMOUNT"}, "SYNTAX"},
		"code of a base measure": {map[string]any{"code": "TAX", "formula": "PREMIUM", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}, "AMBIGUOUS_MEASURE"},
		"unknown event scope":    {map[string]any{"code": "X", "formula": "PREMIUM", "resultType": "AMOUNT", "eventTypes": []any{"NOPE"}}, "UNKNOWN_EVENT"},
		"applies nowhere":        {map[string]any{"code": "X", "formula": "COMMISSION * 2", "resultType": "AMOUNT"}, "UNKNOWN_IDENTIFIER"},
		"python injection":       {map[string]any{"code": "X", "formula": "__import__('os')", "resultType": "AMOUNT"}, "SYNTAX"},
		"literal division by 0":  {map[string]any{"code": "X", "formula": "PREMIUM / 0", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}, "DIVIDE_BY_ZERO"},
	}
	for name, k := range cases {
		_, fs := parse(t, withDerived(p1Catalog("POLICY"), k.dm))
		found := false
		for _, f := range errorsOf(fs) {
			found = found || (f.Code == k.code && strings.HasPrefix(f.Path, "derivedMeasures"))
		}
		if !found {
			t.Errorf("%s: want %s at derivedMeasures/…, got %+v", name, k.code, fs)
		}
	}
}
