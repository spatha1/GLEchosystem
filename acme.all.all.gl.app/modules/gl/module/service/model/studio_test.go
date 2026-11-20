package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func findingCodes(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

func validateMap(c map[string]any) []Finding {
	b, _ := json.Marshal(c)
	d, fs := Parse(b)
	return append(fs, Validate(d)...)
}

func scenarioOf(c map[string]any, code string) map[string]any {
	for _, s := range c["scenarios"].([]any) {
		if s.(map[string]any)["code"] == code {
			return s.(map[string]any)
		}
	}
	return nil
}

func lineOf(rule map[string]any, code string) map[string]any {
	for _, l := range rule["lines"].([]any) {
		if l.(map[string]any)["code"] == code {
			return l.(map[string]any)
		}
	}
	return nil
}

// The GL-TEST-001 scenarios are part of the configuration and all hold: E1 (motor and home),
// E2, E3 with intercompany, and the refused same-company receipt.
func TestGLTest001ScenariosHold(t *testing.T) {
	d := docOf(t, fixtureContent(t))
	rs := RunScenarios(d, fixtureRef, nil)
	if len(rs) != 5 {
		t.Fatalf("%d scenarios", len(rs))
	}
	for _, r := range rs {
		if r.Status != "PASSED" {
			t.Errorf("%s: %s %v %+v", r.Code, r.Status, r.Mismatches, r.Error)
		}
	}
	if one := RunScenarios(d, fixtureRef, []string{"E3"}); len(one) != 1 || len(one[0].Plan.Lines) != 5 {
		t.Errorf("E3 alone: %+v", one)
	}
}

// An expectation is checked, not just displayed.
func TestScenarioExpectationsAreChecked(t *testing.T) {
	c := fixtureContent(t)
	e1 := scenarioOf(c, "E1")
	legs := e1["expect"].(map[string]any)["legs"].([]any)
	legs[0].(map[string]any)["amount"] = "1099.00"
	scenarioOf(c, "E2")["expect"] = map[string]any{"refused": "MISSING_RULE"}
	delete(scenarioOf(c, "E1_HOME"), "expect")
	// A refusal with another code, and the wrong rule, are failures too.
	wrongRefusal := map[string]any{}
	b, _ := json.Marshal(scenarioOf(c, "E3_SAME_COMPANY"))
	_ = json.Unmarshal(b, &wrongRefusal)
	wrongRefusal["code"], wrongRefusal["expect"] = "E3_WRONG_REFUSAL", map[string]any{"refused": "MISSING_ACCOUNT"}
	old := map[string]any{}
	b, _ = json.Marshal(rulesOf(c, "PR_PREM_ADDED"))
	_ = json.Unmarshal(b, &old)
	old["code"], old["active"] = "PR_PREM_ADDED_OLD", false
	c["postingRules"] = append(c["postingRules"].([]any), old)
	wrongRule := map[string]any{}
	b, _ = json.Marshal(scenarioOf(c, "E1"))
	_ = json.Unmarshal(b, &wrongRule)
	wrongRule["code"] = "E1_WRONG_RULE"
	wrongRule["expect"].(map[string]any)["rule"] = "PR_PREM_ADDED_OLD"
	wrongRule["expect"].(map[string]any)["legs"] = scenarioOf(fixtureContent(t), "E1")["expect"].(map[string]any)["legs"]
	c["scenarios"] = append(c["scenarios"].([]any), wrongRefusal, wrongRule)
	delete(scenarioOf(c, "E3_SAME_COMPANY"), "expect")
	byCode := map[string]ScenarioResult{}
	for _, r := range RunScenarios(docOf(t, c), fixtureRef, nil) {
		byCode[r.Code] = r
	}
	if r := byCode["E1"]; r.Status != "FAILED" || strings.Join(r.Mismatches, ";") != "missing leg AR0 D 1099.00 in C01 to 9948523;unexpected leg AR0 D 1100.00 in C01 to 9948523" {
		t.Errorf("E1 with a wrong amount: %s %v", r.Status, r.Mismatches)
	}
	if r := byCode["E2"]; r.Status != "FAILED" || len(r.Mismatches) != 1 || !strings.Contains(r.Mismatches[0], "expected refusal MISSING_RULE") {
		t.Errorf("E2 expected to be refused: %s %v", r.Status, r.Mismatches)
	}
	if r := byCode["E1_HOME"]; r.Status != "RAN" || r.Plan == nil {
		t.Errorf("no expectation: %s", r.Status)
	}
	if r := byCode["E3_SAME_COMPANY"]; r.Status != "REFUSED" || r.Error == nil || r.Error.Code != "ENTITY_NOT_ALLOWED" {
		t.Errorf("refused without expectation: %+v", r)
	}
	if r := byCode["E3_WRONG_REFUSAL"]; r.Status != "FAILED" || len(r.Mismatches) != 1 || !strings.HasPrefix(r.Mismatches[0], "expected refusal MISSING_ACCOUNT, got ENTITY_NOT_ALLOWED") {
		t.Errorf("another refusal code: %s %v", r.Status, r.Mismatches)
	}
	if r := byCode["E1_WRONG_RULE"]; r.Status != "FAILED" || strings.Join(r.Mismatches, ";") != "expected rule PR_PREM_ADDED_OLD, got PR_PREM_ADDED" {
		t.Errorf("another rule: %s %v", r.Status, r.Mismatches)
	}
}

func TestScenariosAreValidated(t *testing.T) {
	c := fixtureContent(t)
	ss := c["scenarios"].([]any)
	bad := func(code, event string, input map[string]any, expect map[string]any) map[string]any {
		return map[string]any{"code": code, "eventType": event, "input": input, "expect": expect}
	}
	c["scenarios"] = append(ss,
		bad("E1", "PREM_ADDED", map[string]any{"currency": "AUD"}, nil),
		bad("NOPE", "NOT_AN_EVENT", map[string]any{"currency": "AUD"}, nil),
		bad("MIX", "PREM_ADDED", map[string]any{"eventType": "INVOICED", "currency": "AUD"}, nil),
		bad("LEGS", "PREM_ADDED", map[string]any{"currency": "AUD"}, map[string]any{"rule": "PR_INVOICED",
			"legs": []any{map[string]any{"class": "ZZ9", "side": "X", "amount": "1e3", "account": "0000000"}}}),
		bad("BOTH", "PREM_ADDED", map[string]any{"currency": "AUD"}, map[string]any{"refused": "MISSING_RULE",
			"legs": []any{map[string]any{"class": "AR0", "side": "D", "amount": "1.00"}}}),
	)
	got := strings.Join(findingCodes(validateMap(c)), ",")
	for _, want := range []string{"DUPLICATE_SCENARIO", "UNKNOWN_EVENT", "SCENARIO_EVENT_MISMATCH", "UNKNOWN_RULE", "UNKNOWN_CLASS", "INVALID_SIDE", "INVALID_AMOUNT", "UNKNOWN_ACCOUNT", "INVALID_EXPECTATION"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	// An unknown field in a scenario's input is refused, not ignored.
	c = fixtureContent(t)
	scenarioOf(c, "E1")["input"].(map[string]any)["premium"] = "1000"
	if got := strings.Join(findingCodes(validateMap(c)), ","); !strings.Contains(got, "INVALID_ITEM") {
		t.Errorf("unknown input field: %s", got)
	}
}

// A deactivated rule stays in the version for the record, is never applied, reads nothing
// and does not clash with the rule that replaces it.
func TestDeactivatedRulesAreKeptButNeverApplied(t *testing.T) {
	c := fixtureContent(t)
	pr := rulesOf(c, "PR_PREM_ADDED")
	pr["active"] = false
	fs := validateMap(c)
	if !strings.Contains(strings.Join(findingCodes(fs), ","), "MISSING_RULE") {
		t.Errorf("all PREM_ADDED rules deactivated: %v", findingCodes(fs))
	}
	d := docOf(t, c)
	_, err := PreviewPosting(d, fixtureRef, e1In())
	if ErrorCode(err) != "MISSING_RULE" {
		t.Fatalf("E1 with its rule deactivated: %v", err)
	}
	m, _ := CompileManifest(d, "DEMO", fixtureRef)
	if _, ok := fieldIn(m, "PREM_ADDED", "", "PREMIUM"); ok {
		t.Error("a deactivated rule still requires PREMIUM")
	}

	// A copy replaces it at the same rank: no ambiguity, the copy applies.
	cp := map[string]any{}
	b, _ := json.Marshal(pr)
	_ = json.Unmarshal(b, &cp)
	cp["code"], cp["active"] = "PR_PREM_ADDED_V2", true
	c["postingRules"] = append(c["postingRules"].([]any), cp)
	d = docOf(t, c)
	p, err := PreviewPosting(d, fixtureRef, e1In())
	if err != nil || p.Rule != "PR_PREM_ADDED_V2" {
		t.Fatalf("the replacement: %v %+v", err, p)
	}
	if p.Considered[0].Rule != "PR_PREM_ADDED" || p.Considered[0].Matched || !strings.HasPrefix(p.Considered[0].Reason, "deactivated") {
		t.Errorf("considered: %+v", p.Considered)
	}
}

// Rule windows split a version by book date; overlapping windows at one rank are refused.
func TestRuleWindowsSplitAVersionByBookDate(t *testing.T) {
	c := fixtureContent(t)
	pr := rulesOf(c, "PR_PREM_ADDED")
	cp := map[string]any{}
	b, _ := json.Marshal(pr)
	_ = json.Unmarshal(b, &cp)
	pr["effectiveTo"] = "2026-02-01"
	cp["code"], cp["effectiveFrom"] = "PR_PREM_ADDED_FEB", "2026-02-01"
	lineOf(cp, "TX0_CR")["amount"] = "ROUND(PREMIUM / 10, 2)"
	lineOf(cp, "AR0_DR")["amount"] = "PREMIUM + ROUND(PREMIUM / 10, 2)"
	c["postingRules"] = append(c["postingRules"].([]any), cp)
	d := docOf(t, c)

	odd := e1In()
	odd.Measures = map[string]string{"PREMIUM": "1000.00", "TAX": "99.00"}
	jan, err := PreviewPosting(d, fixtureRef, odd)
	if err != nil || jan.Rule != "PR_PREM_ADDED" || !strings.HasPrefix(legs(jan)[2], "TX0 C 99.00") {
		t.Fatalf("January: %v %v", err, legs(jan))
	}
	odd.TransactionDate, odd.EffectiveDate = "2026-02-10", "2026-02-10"
	feb, err := PreviewPosting(d, fixtureRef, odd)
	if err != nil || feb.Rule != "PR_PREM_ADDED_FEB" || !strings.HasPrefix(legs(feb)[2], "TX0 C 100.00") {
		t.Fatalf("February: %v %v", err, legs(feb))
	}
	if r := feb.Considered[0]; r.Matched || !strings.Contains(r.Reason, "not in force on 2026-02-10") {
		t.Errorf("January rule in February: %+v", r)
	}

	delete(pr, "effectiveTo")
	if got := strings.Join(findingCodes(validateMap(c)), ","); !strings.Contains(got, "AMBIGUOUS_RULE") {
		t.Errorf("overlapping windows at one rank: %s", got)
	}
	pr["effectiveFrom"], pr["effectiveTo"] = "2026-03-01", "2026-02-01"
	cp["effectiveTo"] = "2026-02-30"
	got := strings.Join(findingCodes(validateMap(c)), ",")
	if !strings.Contains(got, "INVALID_WINDOW") || !strings.Contains(got, "INVALID_DATE") {
		t.Errorf("bad windows: %s", got)
	}
}

// A plan carries every formula it evaluated, with the values read.
func TestPlansShowTheirFormulaCalculations(t *testing.T) {
	p, err := PreviewPosting(docOf(t, fixtureContent(t)), fixtureRef, e1In())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Derived) != 1 || p.Derived[0].Code != "GROSS" || p.Derived[0].Value != "1100 AUD" || len(p.Derived[0].Trace.Reads) != 2 ||
		p.Derived[0].Trace.Reads[0].Name != "PREMIUM" || p.Derived[0].Config != fixtureRef {
		t.Fatalf("derived: %+v", p.Derived)
	}
}

// The studio's event view connects an event to its sources, measures, formulas, keys, dates,
// rules, classes, candidate accounts and scenarios.
func TestExplainEventConnectsTheEventToItsRules(t *testing.T) {
	d := docOf(t, fixtureContent(t))
	v, ok := ExplainEvent(d, nil, "DEMO", fixtureRef, "PREM_ADDED")
	if !ok {
		t.Fatal("PREM_ADDED not found")
	}
	if len(v.SourceSystems) != 1 || v.SourceSystems[0].Code != "POLICY" || len(v.Authority) != 1 || v.Path != "events/0" {
		t.Errorf("sources: %+v %+v", v.SourceSystems, v.Authority)
	}
	req := map[string]bool{}
	for _, m := range v.Measures {
		req[m.Code] = m.Required
	}
	for _, x := range v.Dimensions {
		req[x.Code] = x.Required
	}
	if !req["PREMIUM"] || !req["TAX"] || !req["LOB"] || req["PRODUCT"] {
		t.Errorf("required: %v", req)
	}
	if len(v.Derived) != 1 || v.Derived[0].Code != "GROSS" || strings.Join(v.Derived[0].Inputs, ",") != "PREMIUM,TAX" || !v.Derived[0].Required {
		t.Errorf("derived: %+v", v.Derived)
	}
	if len(v.Keys) != 2 || v.Keys[0].Code != "POLICY_NO" || strings.Join(v.Keys[0].Roles, ",") != "ECONOMIC_KEY,EVENT_KEY" || v.Keys[0].Pattern != "P[0-9]+" {
		t.Errorf("keys: %+v", v.Keys)
	}
	if v.DatePolicy == nil || v.DatePolicy.Code != "DP_STANDARD" || v.Calendar == nil || v.Calendar.Code != "AU_SYDNEY" {
		t.Errorf("dates: %+v %+v", v.DatePolicy, v.Calendar)
	}
	if len(v.Rules) != 1 || len(v.Rules[0].Lines) != 3 || !v.Rules[0].IsActive {
		t.Fatalf("rules: %+v", v.Rules)
	}
	ar0 := v.Rules[0].Lines[0]
	if ar0.Class == nil || ar0.Class.NormalSide != "D" || len(ar0.Candidates) != 2 || ar0.Candidates[0].Rule.Code != "AA_AR0_MOTOR" ||
		ar0.Candidates[0].Account == nil || ar0.Candidates[0].Account.Code != "9948523" || ar0.Candidates[1].Rule.Code != "AA_AR0_DEFAULT" {
		t.Errorf("AR0 line: %+v", ar0)
	}
	if len(v.Scenarios) != 2 || len(v.Manifest.Fields) == 0 || len(v.Explanation.Unused) == 0 {
		t.Errorf("scenarios %d, manifest %d, unused %d", len(v.Scenarios), len(v.Manifest.Fields), len(v.Explanation.Unused))
	}
	gross := false
	for _, e := range v.Graph.Edges {
		gross = gross || (e.From == "derived:GROSS" && e.To == "line:PR_PREM_ADDED/AR0_DR" && e.Role == RoleAmount)
	}
	if !gross {
		t.Error("the graph does not connect GROSS to the AR0 line")
	}

	p, _ := ExplainEvent(d, nil, "DEMO", fixtureRef, "PAYMENT_RECEIVED")
	var bc DimensionUse
	for _, x := range p.Dimensions {
		if x.Code == "BANKING_COMPANY" {
			bc = x
		}
	}
	if bc.Carried || bc.DerivedBy != "BANKING_COMPANY_BY_ACCOUNT" || !bc.Required || len(p.Lookups) != 1 {
		t.Errorf("BANKING_COMPANY: %+v, lookups %d", bc, len(p.Lookups))
	}
	roles := map[string]string{}
	for _, k := range p.Keys {
		roles[k.Code] = strings.Join(k.Roles, ",")
	}
	if roles["ALLOCATION_ID"] != "ITEM_KEY ALLOCATION_LIST" || roles["INVOICE_NO"] != "LINK_KEY ALLOCATION_LIST" || roles["RECEIPT_NO"] != "ECONOMIC_KEY,EVENT_KEY" {
		t.Errorf("payment keys: %v", roles)
	}
	if _, ok := ExplainEvent(d, nil, "DEMO", fixtureRef, "NOPE"); ok {
		t.Error("an unknown event was explained")
	}
}

func TestSummariesCountWhatEachEventHas(t *testing.T) {
	c := fixtureContent(t)
	rulesOf(c, "PR_INVOICED")["filter"] = "NOT_A_FIELD > 0"
	b, _ := json.Marshal(c)
	d, fs := Parse(b)
	fs = append(fs, Validate(d)...)
	got := map[string]EventSummary{}
	for _, s := range SummarizeEvents(d, fs, "DEMO", fixtureRef) {
		got[s.Code] = s
	}
	if s := got["PREM_ADDED"]; s.Rules != 1 || s.ActiveRules != 1 || s.Lines != 3 || s.Scenarios != 2 || s.DerivedValues != 1 || s.RequiredFields != 11 || s.Errors != 0 ||
		strings.Join(s.SourceSystems, ",") != "POLICY" {
		t.Errorf("PREM_ADDED: %+v", s)
	}
	if s := got["INVOICED"]; s.Errors == 0 {
		t.Errorf("INVOICED's broken filter is not counted: %+v", s)
	}
	if s := got["PAYMENT_RECEIVED"]; strings.Join(s.Lists, ",") != "ALLOCATION_LIST" || !reflect.DeepEqual(s.Measures, []string{"PAY_AMT", "ALLOC_AMT"}) || s.Errors != 0 {
		t.Errorf("PAYMENT_RECEIVED: %+v", s)
	}
}

func TestDiffContentNamesItemsAndLines(t *testing.T) {
	a := fixtureContent(t)
	b := fixtureContent(t)
	lineOf(rulesOf(b, "PR_PREM_ADDED"), "PW0_CR")["amount"] = "PREMIUM - 10"
	rulesOf(b, "PR_PREM_ADDED")["lines"] = append(rulesOf(b, "PR_PREM_ADDED")["lines"].([]any), map[string]any{"code": "FEE", "class": "PW0", "side": "C", "amount": "10"})
	b["accounts"].([]any)[0].(map[string]any)["name"] = "Renamed"
	var kept []any
	for _, s := range b["scenarios"].([]any) {
		if s.(map[string]any)["code"] != "E1_HOME" {
			kept = append(kept, s)
		}
	}
	b["scenarios"] = append(kept, map[string]any{"code": "E9", "eventType": "PREM_ADDED", "input": map[string]any{"currency": "AUD"}})
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	got := DescribeChanges(DiffContent(ja, jb))
	want := []string{
		"changed accounts " + a["accounts"].([]any)[0].(map[string]any)["code"].(string) + " (name)",
		"changed postingRules PR_PREM_ADDED (lines/PW0_CR, lines/FEE added)",
		"added scenarios E9",
		"removed scenarios E1_HOME",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changes:\n got %q\nwant %q", got, want)
	}
	if len(DiffContent(ja, ja)) != 0 {
		t.Error("identical contents differ")
	}
}

// Impact traces a rule change to the events it affects, the fields they need and the
// scenarios' journals; an account rename reaches only the events posting to that account.
func TestImpactTracesAChangeToEventsFieldsAndJournals(t *testing.T) {
	a := fixtureContent(t)
	b := fixtureContent(t)
	pr := rulesOf(b, "PR_PREM_ADDED")
	lineOf(pr, "PW0_CR")["amount"] = "PREMIUM - 10"
	lineOf(pr, "AR0_DR")["amount"] = "GROSS - 10"
	pr["filter"] = "PRODUCT != 'HOME_BUILDING'"
	for _, acc := range b["accounts"].([]any) {
		if acc.(map[string]any)["code"] == "9948600" {
			acc.(map[string]any)["name"] = "Invoice receivable (renamed)"
		}
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	to := fixtureRef
	to.Version = 2
	im := ComputeImpact(docOf(t, a), docOf(t, b), ja, jb, "DEMO", fixtureRef, to)

	events := map[string]EventImpact{}
	for _, e := range im.Events {
		events[e.EventType] = e
	}
	if e := events["PREM_ADDED"]; strings.Join(e.Because, ",") != "postingRules:PR_PREM_ADDED" || e.FieldsChanged == 0 || e.Remap != 1 {
		t.Errorf("PREM_ADDED: %+v", e)
	}
	if e := events["INVOICED"]; strings.Join(e.Because, ",") != "accounts:9948600" || e.Remap != 0 {
		t.Errorf("INVOICED: %+v", e)
	}
	if e := events["PAYMENT_RECEIVED"]; strings.Join(e.Because, ",") != "accounts:9948600" {
		t.Errorf("PAYMENT_RECEIVED: %+v", e)
	}
	if !im.Manifest.Changed {
		t.Error("PRODUCT is newly read but the manifest did not change")
	}
	sc := map[string]ScenarioImpact{}
	for _, s := range im.Scenarios {
		sc[s.Code] = s
	}
	if s := sc["E1"]; !s.Changed || s.After.Status != "FAILED" || !strings.Contains(strings.Join(s.Differences, ";"), "new leg C01 9948523 AR0 D 1090.00") {
		t.Errorf("E1: %+v", s.Differences)
	}
	if s := sc["E1_HOME"]; !s.Changed || s.After.Error == nil || s.After.Error.Code != "MISSING_RULE" || !strings.Contains(strings.Join(s.Differences, ";"), "now refused: MISSING_RULE") {
		t.Errorf("E1_HOME (now filtered out): %+v", s.Differences)
	}
	if s := sc["E3"]; s.Changed || s.Differences == nil {
		t.Errorf("E3 does not change (and says so with an empty list, never null): %+v", s.Differences)
	}
	if raw, _ := json.Marshal(im); strings.Contains(string(raw), ":null") {
		t.Errorf("the impact holds a null list: %s", raw)
	}
	if len(im.Downstream) != 5 || im.Downstream[0].Stage != "mapped" || !strings.Contains(im.Downstream[0].Detail, "1 field") {
		t.Errorf("downstream: %+v", im.Downstream)
	}
}

// The studio reads these documents directly: a list is always a list, never null.
func TestStudioDocumentsNeverHoldNullLists(t *testing.T) {
	d := docOf(t, fixtureContent(t))
	var docs []any
	for _, e := range d.Events {
		v, _ := ExplainEvent(d, Validate(d), "DEMO", fixtureRef, e.Code)
		docs = append(docs, v)
	}
	m, x := CompileManifest(d, "DEMO", fixtureRef)
	docs = append(docs, m, x, SummarizeEvents(d, nil, "DEMO", fixtureRef), RunScenarios(d, fixtureRef, nil))
	for i, doc := range docs {
		raw, _ := json.Marshal(doc)
		if j := strings.Index(string(raw), ":null"); j >= 0 {
			lo := j - 80
			if lo < 0 {
				lo = 0
			}
			t.Errorf("document %d holds a null: …%s…", i, raw[lo:j+5])
		}
	}
}
