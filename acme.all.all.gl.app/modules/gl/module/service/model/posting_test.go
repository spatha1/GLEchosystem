package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func line(code, class, side, amount string, extra ...string) map[string]any {
	l := map[string]any{"code": code, "class": class, "side": side, "amount": amount}
	for i := 0; i+1 < len(extra); i += 2 {
		l[extra[i]] = extra[i+1]
	}
	return l
}

func rule(code, event string, rank int, filter string, lines ...map[string]any) map[string]any {
	r := map[string]any{"code": code, "eventType": event, "rank": rank, "lines": toAny(lines)}
	if filter != "" {
		r["filter"] = filter
	}
	return r
}

func toAny[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// demoRules: the GL-TEST-001 posting rules for E1, E2 and E3.
func demoRules() []any {
	return []any{
		rule("PR_PREM_ADDED", "PREM_ADDED", 10, "",
			line("AR0_DR", "AR0", "D", "GROSS", "explanation", "Policy receivable: premium plus tax"),
			line("PW0_CR", "PW0", "C", "PREMIUM"),
			line("TX0_CR", "TX0", "C", "TAX")),
		rule("PR_INVOICED", "INVOICED", 10, "",
			line("AR1_DR", "AR1", "D", "INVOICE_AMT", "expandBy", "INVOICE_LIST"),
			line("AR0_CR", "AR0", "C", "INVOICE_AMT", "expandBy", "INVOICE_LIST")),
		rule("PR_PAYMENT_RECEIVED", "PAYMENT_RECEIVED", 10, "",
			line("CASH", "PR0", "D", "PAY_AMT", "entity", "BANKING_COMPANY"),
			line("DUE_TO", "IC1", "C", "PAY_AMT", "entity", "BANKING_COMPANY", "when", "BANKING_COMPANY != COMPANY"),
			line("DUE_FROM", "IC0", "D", "PAY_AMT", "when", "BANKING_COMPANY != COMPANY"),
			line("ALLOC", "AR1", "C", "ALLOC_AMT", "expandBy", "ALLOCATION_LIST")),
	}
}

func postingCatalog(edit func(c map[string]any)) map[string]any {
	return coaCatalog(func(c map[string]any) {
		ev := c["events"].([]any)
		ev[1].(map[string]any)["dimensions"] = []any{"COMPANY"}
		c["derivedMeasures"] = []any{map[string]any{"code": "GROSS", "formula": "PREMIUM + TAX", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}}
		c["postingRules"] = demoRules()
		if edit != nil {
			edit(c)
		}
	})
}

func postingDoc(t *testing.T, edit func(c map[string]any)) *Document {
	t.Helper()
	doc, fs := parse(t, postingCatalog(edit))
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	return doc
}

var demoRef = ConfigRef{ConfigID: "DEMO_GL", Version: 1, ContentDigest: "sha256:demo"}

func e1In() EventInput {
	return EventInput{EventType: "PREM_ADDED", Currency: "AUD", TransactionDate: "2026-01-01", EffectiveDate: "2026-01-01",
		Keys: map[string]string{"POLICY_NO": "P1", "TERM_NO": "1"}, Measures: map[string]string{"PREMIUM": "1000.00", "TAX": "100.00"},
		Dimensions: map[string]string{"LOB": "MOTOR", "COMPANY": "C01", "PRODUCT": "MOTOR_PRIVATE"}}
}

func e2In() EventInput {
	return EventInput{EventType: "INVOICED", Currency: "AUD", TransactionDate: "2026-01-05", EffectiveDate: "2026-01-05",
		Keys: map[string]string{"POLICY_NO": "P1", "TERM_NO": "1", "INVOICE_RUN": "RUN1"}, Dimensions: map[string]string{"COMPANY": "C01"},
		Lists: map[string][]map[string]string{"INVOICE_LIST": {{"INVOICE_NO": "INV1", "INVOICE_AMT": "550.00"}, {"INVOICE_NO": "INV2", "INVOICE_AMT": "550.00"}}}}
}

func e3In(bank string) EventInput {
	return EventInput{EventType: "PAYMENT_RECEIVED", Currency: "AUD", TransactionDate: "2026-01-10", EffectiveDate: "2026-01-10",
		Keys: map[string]string{"RECEIPT_NO": "R1"}, Measures: map[string]string{"PAY_AMT": "1099.50"},
		Dimensions: map[string]string{"BANK_ACCOUNT": bank, "COMPANY": "C01"},
		Lists: map[string][]map[string]string{"ALLOCATION_LIST": {
			{"ALLOCATION_ID": "A1", "INVOICE_NO": "INV1", "ALLOC_AMT": "550.00"},
			{"ALLOCATION_ID": "A2", "INVOICE_NO": "INV2", "ALLOC_AMT": "549.50"}}}}
}

func plan(t *testing.T, d *Document, in EventInput) *PostingPlan {
	t.Helper()
	p, err := PlanPosting(d, demoRef, in)
	if err != nil {
		t.Fatalf("%s: %v", in.EventType, err)
	}
	return p
}

// legs renders lines compactly: "AR0 D 1100.00 C01".
func legs(p *PostingPlan) []string {
	var out []string
	for _, l := range p.Lines {
		s := l.Class + " " + l.Side + " " + l.Amount + " " + l.Entity
		if l.Item > 0 {
			s += " #" + string(rune('0'+l.Item))
		}
		out = append(out, s)
	}
	return out
}

// AC: PREM_ADDED produces exactly three demo legs: 1100 DR, 1000 CR, 100 CR.
func TestPremAddedProducesThreeDemoLegs(t *testing.T) {
	d := postingDoc(t, nil)
	p := plan(t, d, e1In())
	if want := []string{"AR0 D 1100.00 C01", "PW0 C 1000.00 C01", "TX0 C 100.00 C01"}; !reflect.DeepEqual(legs(p), want) {
		t.Fatalf("E1 legs: %v", legs(p))
	}
	if p.Rule != "PR_PREM_ADDED" || !p.Balanced || len(p.Totals) != 1 || p.Totals[0] != (BalanceTotal{"C01", "AUD", "1100.00", "1100.00", true}) {
		t.Errorf("E1 plan: rule %s balanced %v totals %+v", p.Rule, p.Balanced, p.Totals)
	}
	ar0 := p.Lines[0]
	if ar0.Keys["POLICY_NO"] != "P1" || ar0.Dimensions["LOB"] != "MOTOR" || ar0.Currency != "AUD" || ar0.Explanation != "Policy receivable: premium plus tax" {
		t.Errorf("the AR0 leg carries its keys, dimensions and why: %+v", ar0)
	}
	if len(ar0.Trace.Reads) != 1 || ar0.Trace.Reads[0].Name != "GROSS" || ar0.Trace.Reads[0].Value != "1100 AUD" {
		t.Errorf("lineage: %+v", ar0.Trace)
	}
	if len(p.Considered) != 1 || !p.Considered[0].Matched || p.Config != demoRef {
		t.Errorf("explain: %+v %+v", p.Considered, p.Config)
	}
}

// AC: INVOICED expands exactly two DR/CR invoice pairs.
func TestInvoicedExpandsTwoInvoicePairs(t *testing.T) {
	p := plan(t, postingDoc(t, nil), e2In())
	if want := []string{"AR1 D 550.00 C01 #1", "AR1 D 550.00 C01 #2", "AR0 C 550.00 C01 #1", "AR0 C 550.00 C01 #2"}; !reflect.DeepEqual(legs(p), want) {
		t.Fatalf("E2 legs: %v", legs(p))
	}
	pairs := map[int][]string{}
	for _, l := range p.Lines {
		pairs[l.Item] = append(pairs[l.Item], l.Side+":"+l.Keys["INVOICE_NO"])
	}
	if !reflect.DeepEqual(pairs, map[int][]string{1: {"D:INV1", "C:INV1"}, 2: {"D:INV2", "C:INV2"}}) {
		t.Errorf("one DR/CR pair per invoice, each keyed by its invoice: %v", pairs)
	}
	if p.Lines[0].Keys["POLICY_NO"] != "P1" || !strings.Contains(p.Lines[0].Explanation, "INVOICE_LIST item 1") {
		t.Errorf("item lines keep the event's keys: %+v", p.Lines[0])
	}
	// 1:N with one, three and zero items.
	in := e2In()
	in.Lists["INVOICE_LIST"] = []map[string]string{{"INVOICE_NO": "INV1", "INVOICE_AMT": "1100.00"}}
	if got := plan(t, postingDoc(t, nil), in); len(got.Lines) != 2 {
		t.Errorf("one invoice, one pair: %v", legs(got))
	}
	in.Lists["INVOICE_LIST"] = []map[string]string{{"INVOICE_NO": "INV1", "INVOICE_AMT": "400.00"}, {"INVOICE_NO": "INV2", "INVOICE_AMT": "400.00"}, {"INVOICE_NO": "INV3", "INVOICE_AMT": "300.00"}}
	if got := plan(t, postingDoc(t, nil), in); len(got.Lines) != 6 || !got.Balanced {
		t.Errorf("three invoices, three pairs: %v", legs(got))
	}
	in.Lists["INVOICE_LIST"] = nil
	if _, err := PlanPosting(postingDoc(t, nil), demoRef, in); codeOf(err) != "UNBALANCED" {
		t.Errorf("no invoices produce no lines, which is refused: %v", err)
	}
}

// E3: one receipt, two allocations, cash in the banking company, intercompany legs.
func TestPaymentPostsPerEntityWithIntercompanyLegs(t *testing.T) {
	d := postingDoc(t, nil)
	p := plan(t, d, e3In("BANK-C09"))
	want := []string{"PR0 D 1099.50 C09", "IC1 C 1099.50 C09", "IC0 D 1099.50 C01", "AR1 C 550.00 C01 #1", "AR1 C 549.50 C01 #2"}
	if !reflect.DeepEqual(legs(p), want) {
		t.Fatalf("E3 legs: %v", legs(p))
	}
	if !p.Balanced || len(p.Totals) != 2 || !p.Totals[0].Balanced || !p.Totals[1].Balanced {
		t.Errorf("each company balances: %+v", p.Totals)
	}
	if p.Lines[3].Keys["INVOICE_NO"] != "INV1" || p.Lines[3].Keys["ALLOCATION_ID"] != "A1" || p.Lines[3].Keys["RECEIPT_NO"] != "R1" {
		t.Errorf("allocation legs keep receipt, allocation and invoice: %+v", p.Lines[3].Keys)
	}
	// Same company: no intercompany legs, and the plan says why.
	same := plan(t, d, e3In("BANK-C01"))
	if want := []string{"PR0 D 1099.50 C01", "AR1 C 550.00 C01 #1", "AR1 C 549.50 C01 #2"}; !reflect.DeepEqual(legs(same), want) {
		t.Errorf("same-company receipt: %v", legs(same))
	}
	if len(same.Skipped) != 2 || !strings.Contains(same.Skipped[0], `condition "BANKING_COMPANY != COMPANY" is not true`) {
		t.Errorf("skipped legs are explained: %v", same.Skipped)
	}
}

// AC: rules can be added or modified without touching engine source.
func TestRulesAreDataNotCode(t *testing.T) {
	levy := postingDoc(t, func(c map[string]any) {
		c["accountingClasses"] = append(c["accountingClasses"].([]any), map[string]any{"code": "PW1", "name": "Motor levy", "nature": "LIABILITY"})
		c["accounts"] = append(c["accounts"].([]any), map[string]any{"code": "2300100", "name": "Motor levy payable", "class": "PW1", "activeFrom": "2025-07-01"})
		c["postingRules"] = append(c["postingRules"].([]any), rule("PR_PREM_MOTOR", "PREM_ADDED", 5, "LOB == 'MOTOR'",
			line("AR0_DR", "AR0", "D", "GROSS"),
			line("PW0_CR", "PW0", "C", "ROUND(PREMIUM * 0.98, 2)"),
			line("PW1_CR", "PW1", "C", "PREMIUM - ROUND(PREMIUM * 0.98, 2)"),
			line("TX0_CR", "TX0", "C", "TAX")))
	})
	motor := plan(t, levy, e1In())
	if motor.Rule != "PR_PREM_MOTOR" || !reflect.DeepEqual(legs(motor), []string{"AR0 D 1100.00 C01", "PW0 C 980.00 C01", "PW1 C 20.00 C01", "TX0 C 100.00 C01"}) {
		t.Errorf("a new, more specific rule applies to motor: %s %v", motor.Rule, legs(motor))
	}
	if len(motor.Considered) != 2 || motor.Considered[1].Reason != "filter is true" {
		t.Errorf("both rules were considered: %+v", motor.Considered)
	}
	home := e1In()
	home.Dimensions["LOB"], home.Dimensions["PRODUCT"] = "HOME", "HOME_BUILDING"
	if h := plan(t, levy, home); h.Rule != "PR_PREM_ADDED" || len(h.Lines) != 3 {
		t.Errorf("home keeps the general rule: %s %v", h.Rule, legs(h))
	}
	// Modifying a formula changes the output; nothing else changes.
	gst := postingDoc(t, func(c map[string]any) {
		r := c["postingRules"].([]any)[0].(map[string]any)
		r["lines"].([]any)[2].(map[string]any)["amount"] = "ROUND(PREMIUM / 10, 2)"
	})
	if p := plan(t, gst, e1In()); !reflect.DeepEqual(legs(p), []string{"AR0 D 1100.00 C01", "PW0 C 1000.00 C01", "TX0 C 100.00 C01"}) {
		t.Errorf("tax recomputed from premium: %v", legs(p))
	}
}

func TestRuleSelectionNeverGuesses(t *testing.T) {
	two := postingDoc(t, func(c map[string]any) {
		c["postingRules"] = append(c["postingRules"].([]any),
			rule("PR_MOTOR_A", "PREM_ADDED", 5, "LOB == 'MOTOR'", line("D1", "AR0", "D", "GROSS"), line("C1", "PW0", "C", "GROSS")),
			rule("PR_C01_B", "PREM_ADDED", 5, "COMPANY == 'C01'", line("D1", "AR0", "D", "GROSS"), line("C1", "PW0", "C", "GROSS")))
	})
	if _, err := PlanPosting(two, demoRef, e1In()); codeOf(err) != "AMBIGUOUS_RULE" || !strings.Contains(err.Error(), "PR_MOTOR_A and PR_C01_B both match at rank 5") {
		t.Errorf("a tie is refused: %v", err)
	}
	_, fs := parse(t, postingCatalog(func(c map[string]any) {
		c["postingRules"] = append(c["postingRules"].([]any),
			rule("PR_MOTOR_A", "PREM_ADDED", 5, "LOB == 'MOTOR'", line("D1", "AR0", "D", "GROSS"), line("C1", "PW0", "C", "GROSS")),
			rule("PR_C01_B", "PREM_ADDED", 5, "COMPANY == 'C01'", line("D1", "AR0", "D", "GROSS"), line("C1", "PW0", "C", "GROSS")))
	}))
	if !hasCode(fs, "POSSIBLE_AMBIGUITY") {
		t.Errorf("validation warns about same-rank rules: %+v", fs)
	}
	onlyMotor := postingDoc(t, func(c map[string]any) {
		c["postingRules"].([]any)[0].(map[string]any)["filter"] = "LOB == 'MOTOR'"
	})
	home := e1In()
	home.Dimensions["LOB"], home.Dimensions["PRODUCT"] = "HOME", "HOME_BUILDING"
	if _, err := PlanPosting(onlyMotor, demoRef, home); codeOf(err) != "MISSING_RULE" {
		t.Errorf("no applicable rule is refused: %v", err)
	}
	noLob := e1In()
	delete(noLob.Dimensions, "LOB")
	if _, err := PlanPosting(onlyMotor, demoRef, noLob); codeOf(err) != "MISSING_RULE" {
		t.Errorf("a null filter does not match: %v", err)
	}
}

func TestRuntimePlanProblemsAreExplicit(t *testing.T) {
	unbalanced := postingDoc(t, func(c map[string]any) {
		c["postingRules"].([]any)[0].(map[string]any)["lines"] = []any{line("D", "AR0", "D", "GROSS"), line("C", "PW0", "C", "PREMIUM")}
	})
	p, err := PlanPosting(unbalanced, demoRef, e1In())
	if codeOf(err) != "UNBALANCED" || p == nil || p.Balanced || !strings.Contains(err.Error(), "C01 AUD debits 1100.00 ≠ credits 1000.00") {
		t.Errorf("unbalanced: %v %+v", err, p)
	}
	third := postingDoc(t, func(c map[string]any) {
		c["postingRules"].([]any)[0].(map[string]any)["lines"] = []any{line("D", "AR0", "D", "PREMIUM / 3"), line("C", "PW0", "C", "PREMIUM / 3")}
	})
	if _, err := PlanPosting(third, demoRef, e1In()); codeOf(err) != "UNROUNDED_AMOUNT" {
		t.Errorf("1000/3 must be rounded by the rule: %v", err)
	}
	noTax := e1In()
	delete(noTax.Measures, "TAX")
	if _, err := PlanPosting(postingDoc(t, nil), demoRef, noTax); codeOf(err) != "NULL_AMOUNT" {
		t.Errorf("a missing measure is not a zero: %v", err)
	}
	zeroTax := e1In()
	zeroTax.Measures["TAX"] = "0.00"
	if z := plan(t, postingDoc(t, nil), zeroTax); len(z.Lines) != 2 || len(z.Skipped) != 1 || !strings.Contains(z.Skipped[0], "amount is zero") {
		t.Errorf("a zero leg is skipped, with the reason: %v %v", legs(z), z.Skipped)
	}
	// A negative premium (a reduction) flips every side and still balances.
	minus := e1In()
	minus.Measures["PREMIUM"], minus.Measures["TAX"] = "-20.00", "-2.00"
	m := plan(t, postingDoc(t, nil), minus)
	if !reflect.DeepEqual(legs(m), []string{"AR0 C 22.00 C01", "PW0 D 20.00 C01", "TX0 D 2.00 C01"}) || !strings.Contains(m.Lines[0].Explanation, "side flipped") {
		t.Errorf("negative amounts: %v %+v", legs(m), m.Lines[0])
	}
	noCompany := e1In()
	delete(noCompany.Dimensions, "COMPANY")
	if _, err := PlanPosting(postingDoc(t, nil), demoRef, noCompany); codeOf(err) != "MISSING_ENTITY" {
		t.Errorf("a leg without a legal entity: %v", err)
	}
}

func TestPlansAreDeterministic(t *testing.T) {
	d := postingDoc(t, nil)
	first, _ := json.Marshal(plan(t, d, e3In("BANK-C09")))
	for i := 0; i < 20; i++ {
		again, _ := json.Marshal(plan(t, d, e3In("BANK-C09")))
		if string(again) != string(first) {
			t.Fatalf("run %d differs", i)
		}
	}
}

// Tests: invalid line plans are caught at validation, before anything runs.
func TestInvalidLinePlansFailValidation(t *testing.T) {
	setLines := func(i int, ls ...map[string]any) func(c map[string]any) {
		return func(c map[string]any) { c["postingRules"].([]any)[i].(map[string]any)["lines"] = toAny(ls) }
	}
	cases := map[string]func(c map[string]any){
		"INVALID_LINE_PLAN":       setLines(0, line("D1", "AR0", "D", "GROSS"), line("D2", "PW0", "D", "PREMIUM")),
		"UNKNOWN_CLASS":           setLines(0, line("D", "AR9", "D", "GROSS"), line("C", "PW0", "C", "GROSS")),
		"INVALID_SIDE":            setLines(0, line("D", "AR0", "DR", "GROSS"), line("C", "PW0", "C", "GROSS")),
		"DUPLICATE_LINE":          setLines(0, line("X", "AR0", "D", "GROSS"), line("X", "PW0", "C", "GROSS")),
		"MISSING_AMOUNT":          setLines(0, line("D", "AR0", "D", " "), line("C", "PW0", "C", "GROSS")),
		"RESULT_TYPE_MISMATCH":    setLines(0, line("D", "AR0", "D", "LOB"), line("C", "PW0", "C", "GROSS")),
		"UNKNOWN_IDENTIFIER":      setLines(1, line("D", "AR1", "D", "INVOICE_AMT"), line("C", "AR0", "C", "INVOICE_AMT")),
		"UNKNOWN_LIST":            setLines(1, line("D", "AR1", "D", "INVOICE_AMT", "expandBy", "ALLOCATION_LIST"), line("C", "AR0", "C", "INVOICE_AMT", "expandBy", "INVOICE_LIST")),
		"MISSING_SUBLEDGER_KEY":   setLines(2, line("D", "PR0", "D", "PAY_AMT", "entity", "BANKING_COMPANY"), line("C", "AR1", "C", "PAY_AMT", "entity", "BANKING_COMPANY")),
		"MISSING_DIMENSION":       setLines(0, line("D", "PR0", "D", "GROSS"), line("C", "PW0", "C", "GROSS")),
		"UNKNOWN_DIMENSION_VALUE": func(c map[string]any) { c["postingRules"].([]any)[0].(map[string]any)["filter"] = "LOB == 'MOTR'" },
		"UNKNOWN_EVENT":           func(c map[string]any) { c["postingRules"].([]any)[0].(map[string]any)["eventType"] = "WRITE_OFF" },
		"DUPLICATE_RULE":          func(c map[string]any) { c["postingRules"].([]any)[1].(map[string]any)["code"] = "PR_PREM_ADDED" },
		"AMBIGUOUS_RULE": func(c map[string]any) {
			c["postingRules"] = append(c["postingRules"].([]any), rule("PR_COPY", "PREM_ADDED", 10, "", line("D", "AR0", "D", "GROSS"), line("C", "PW0", "C", "GROSS")))
		},
		"MISSING_ENTITY": func(c map[string]any) { delete(c["events"].([]any)[1].(map[string]any), "dimensions") },
	}
	for code, edit := range cases {
		_, fs := parse(t, postingCatalog(edit))
		if !hasCode(errorsOf(fs), code) {
			t.Errorf("%s: %+v", code, errorsOf(fs))
		}
	}
	// A filter or condition must be boolean; an entity must be a string.
	for _, edit := range []func(c map[string]any){
		func(c map[string]any) { c["postingRules"].([]any)[0].(map[string]any)["filter"] = "PREMIUM" },
		setLines(2, line("D", "PR0", "D", "PAY_AMT", "entity", "PAY_AMT"), line("C", "AR1", "C", "ALLOC_AMT", "expandBy", "ALLOCATION_LIST")),
	} {
		if _, fs := parse(t, postingCatalog(edit)); !hasCode(errorsOf(fs), "RESULT_TYPE_MISMATCH") {
			t.Errorf("type: %+v", errorsOf(fs))
		}
	}
	// An event without a rule is reported; posting it is refused.
	_, fs := parse(t, postingCatalog(func(c map[string]any) { c["postingRules"] = c["postingRules"].([]any)[:2] }))
	if !hasCode(fs, "MISSING_RULE") {
		t.Errorf("PAYMENT_RECEIVED has no rule: %+v", fs)
	}
	if _, err := PlanPosting(postingDoc(t, func(c map[string]any) { c["postingRules"] = c["postingRules"].([]any)[:2] }), demoRef, e3In("BANK-C09")); codeOf(err) != "MISSING_RULE" {
		t.Errorf("posting without a rule: %v", err)
	}
}
