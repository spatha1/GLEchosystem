package model

import (
	"reflect"
	"strings"
	"testing"
)

func assign(code, class, account string, match map[string]any, extra ...any) map[string]any {
	r := map[string]any{"code": code, "class": class, "account": account}
	if match != nil {
		r["match"] = match
	}
	for i := 0; i+1 < len(extra); i += 2 {
		r[extra[i].(string)] = extra[i+1]
	}
	return r
}

// demoAssignments: AR0 motor goes to 9948523, other AR0 to 9948500; one account per other class.
func demoAssignments() []any {
	return []any{
		assign("AA_AR0_MOTOR", "AR0", "9948523", map[string]any{"LOB": "MOTOR"}),
		assign("AA_AR0_DEFAULT", "AR0", "9948500", nil),
		assign("AA_AR1", "AR1", "9948600", nil),
		assign("AA_PW0", "PW0", "4100100", nil),
		assign("AA_TX0", "TX0", "2200100", nil),
		assign("AA_PR0", "PR0", "1000900", nil),
		assign("AA_IC0", "IC0", "1500109", nil),
		assign("AA_IC1", "IC1", "2500101", nil),
		assign("AA_WO", "WO", "6100100", nil),
	}
}

func assignmentCatalog(edit func(c map[string]any)) map[string]any {
	return postingCatalog(func(c map[string]any) {
		c["events"].([]any)[1].(map[string]any)["dimensions"] = []any{"COMPANY", "LOB"}
		c["accountAssignmentRules"] = demoAssignments()
		if edit != nil {
			edit(c)
		}
	})
}

func assignmentDoc(t *testing.T, edit func(c map[string]any)) *Document {
	t.Helper()
	doc, fs := parse(t, assignmentCatalog(edit))
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	return doc
}

func preview(t *testing.T, d *Document, in EventInput) *PostingPlan {
	t.Helper()
	p, err := PreviewPosting(d, demoRef, in)
	if err != nil {
		t.Fatalf("%s: %v", in.EventType, err)
	}
	return p
}

func accountsOfPlan(p *PostingPlan) []string {
	var out []string
	for _, l := range p.Lines {
		out = append(out, l.Class+"→"+l.Account)
	}
	return out
}

func home(in EventInput) EventInput {
	in.Dimensions["LOB"], in.Dimensions["PRODUCT"] = "HOME", "HOME_BUILDING"
	return in
}

// AC: AR0 + Motor resolves to 9948523; the AR0 default to 9948500.
func TestAr0MotorAndTheDefault(t *testing.T) {
	d := assignmentDoc(t, nil)
	motor := preview(t, d, e1In())
	if got := accountsOfPlan(motor); !reflect.DeepEqual(got, []string{"AR0→9948523", "PW0→4100100", "TX0→2200100"}) {
		t.Fatalf("E1 motor: %v", got)
	}
	a := motor.Lines[0].Assignment
	if a.Rule != "AA_AR0_MOTOR" || !reflect.DeepEqual(a.Why[0], "LOB = MOTOR (exact)") || !strings.Contains(a.Why[1], "chosen over 1 other") {
		t.Errorf("why 9948523: %+v", a)
	}
	if h := preview(t, d, home(e1In())); h.Lines[0].Account != "9948500" || h.Lines[0].Assignment.Rule != "AA_AR0_DEFAULT" {
		t.Errorf("home AR0 takes the default: %+v", h.Lines[0].Assignment)
	}
	// E2 clears the same AR0 account E1 debited — the invoice event carries LOB.
	e2 := e2In()
	e2.Dimensions["LOB"] = "MOTOR"
	if got := accountsOfPlan(preview(t, d, e2)); !reflect.DeepEqual(got, []string{"AR1→9948600", "AR1→9948600", "AR0→9948523", "AR0→9948523"}) {
		t.Errorf("E2: %v", got)
	}
	// E3: the bank account belongs to C09, the intercompany legs to their own accounts.
	if got := accountsOfPlan(preview(t, d, e3In("BANK-C09"))); !reflect.DeepEqual(got, []string{"PR0→1000900", "IC1→2500101", "IC0→1500109", "AR1→9948600", "AR1→9948600"}) {
		t.Errorf("E3: %v", got)
	}
}

// AC: tied matches produce a validation failure, not a random selection.
func TestTiedMatchesFailValidation(t *testing.T) {
	ambiguous := func(rules ...map[string]any) bool {
		_, fs := parse(t, assignmentCatalog(func(c map[string]any) {
			c["accountAssignmentRules"] = append(c["accountAssignmentRules"].([]any), toAny(rules)...)
		}))
		return hasCode(errorsOf(fs), "AMBIGUOUS_ASSIGNMENT")
	}
	if !ambiguous(assign("AA_AR0_C01", "AR0", "9948500", map[string]any{"COMPANY": "C01"})) {
		t.Error("LOB = MOTOR and COMPANY = C01 tie for a C01 motor line")
	}
	if !ambiguous(assign("AA_AR0_DEFAULT2", "AR0", "9948523", nil)) {
		t.Error("two defaults tie")
	}
	if !ambiguous(assign("AA_AR0_MOTOR_WILD", "AR0", "9948500", map[string]any{"LOB": "MOTOR"}, "effectiveFrom", "")) {
		t.Error("the same criteria tie")
	}
	if ambiguous(assign("AA_AR0_HOME", "AR0", "9948500", map[string]any{"LOB": "HOME"})) {
		t.Error("LOB = HOME never meets LOB = MOTOR")
	}
	if ambiguous(assign("AA_AR0_C01", "AR0", "9948500", map[string]any{"COMPANY": "C01"}, "priority", 1)) {
		t.Error("a priority resolves the tie")
	}
	if ambiguous(assign("AA_AR0_FEB", "AR0", "9948500", nil, "effectiveFrom", "2026-02-01")) {
		t.Error("a later-dated default is an override, not a tie")
	}
	if ambiguous(assign("AA_AR0_OLD", "AR0", "9948500", nil, "effectiveFrom", "2024-01-01", "effectiveTo", "2025-01-01"),
		assign("AA_AR0_OLDER", "AR0", "9948500", nil, "effectiveFrom", "2024-01-01", "effectiveTo", "2025-01-01", "ledger", "PRIMARY", "priority", 3)) {
		t.Error("windows and priorities that separate rules are not ties")
	}
	// At runtime too (a version that never passed validation): refused, not picked.
	raw := assignmentCatalog(func(c map[string]any) {
		c["accountAssignmentRules"] = append(c["accountAssignmentRules"].([]any), assign("AA_AR0_C01", "AR0", "9948500", map[string]any{"COMPANY": "C01"}))
	})
	doc, _ := parse(t, raw)
	if _, err := AssignAccount(doc, demoRef, "AR0", map[string]string{"LOB": "MOTOR", "COMPANY": "C01"}, day("2026-01-01")); codeOf(err) != "AMBIGUOUS_ASSIGNMENT" {
		t.Errorf("runtime tie: %v", err)
	}
}

// AC: the posting preview shows the matched account rule and the configuration version.
func TestPreviewShowsTheAccountRuleAndVersion(t *testing.T) {
	p := preview(t, assignmentDoc(t, nil), e1In())
	if p.BookDate != "2026-01-01" {
		t.Errorf("book date: %q", p.BookDate)
	}
	for _, l := range p.Lines {
		a := l.Assignment
		if a == nil || a.Rule == "" || a.Config != demoRef || a.Account != l.Account || l.Ledger != "PRIMARY" || len(a.Considered) == 0 || l.Problem != "" {
			t.Errorf("line %d: %+v %+v", l.LineNo, l, a)
		}
	}
	considered := p.Lines[0].Assignment.Considered
	if len(considered) != 2 || !considered[0].Matched || !considered[1].Matched {
		t.Errorf("both AR0 rules were considered: %+v", considered)
	}
	h := preview(t, assignmentDoc(t, nil), home(e1In()))
	if c := h.Lines[0].Assignment.Considered[0]; c.Matched || c.Reason != `LOB is "HOME", not "MOTOR"` {
		t.Errorf("why the motor rule did not match: %+v", c)
	}
}

// Tests: specificity, ordering, wildcard and effective dates.
func TestSpecificityOrderingWildcardAndDates(t *testing.T) {
	d := assignmentDoc(t, func(c map[string]any) {
		c["accounts"] = append(c["accounts"].([]any),
			map[string]any{"code": "9948501", "name": "AR0 any LOB", "class": "AR0", "activeFrom": "2025-07-01"},
			map[string]any{"code": "9948524", "name": "AR0 motor C01", "class": "AR0", "activeFrom": "2025-07-01"},
			map[string]any{"code": "9948599", "name": "AR0 from February", "class": "AR0", "activeFrom": "2026-02-01"},
			map[string]any{"code": "9948598", "name": "AR0 priority", "class": "AR0", "activeFrom": "2025-07-01"})
		c["accountAssignmentRules"] = append(c["accountAssignmentRules"].([]any),
			assign("AA_AR0_ANY_LOB", "AR0", "9948501", map[string]any{"LOB": "*"}),
			assign("AA_AR0_MOTOR_C01", "AR0", "9948524", map[string]any{"LOB": "MOTOR", "COMPANY": "C01"}),
			assign("AA_AR0_FEB", "AR0", "9948599", nil, "effectiveFrom", "2026-02-01"))
	})
	pick := func(vals map[string]string, on string) string {
		a, err := AssignAccount(d, demoRef, "AR0", vals, day(on))
		if err != nil {
			t.Fatalf("%v on %s: %v", vals, on, err)
		}
		return a.Account
	}
	cases := []struct {
		vals map[string]string
		on   string
		want string
	}{
		{map[string]string{"LOB": "MOTOR", "COMPANY": "C01"}, "2026-01-10", "9948524"}, // two exact beat one
		{map[string]string{"LOB": "MOTOR", "COMPANY": "C09"}, "2026-01-10", "9948523"}, // one exact
		{map[string]string{"LOB": "HOME", "COMPANY": "C01"}, "2026-01-10", "9948501"},  // wildcard beats default
		{map[string]string{"COMPANY": "C01"}, "2026-01-10", "9948500"},                 // no LOB: the wildcard needs one
		{map[string]string{"COMPANY": "C01"}, "2026-02-01", "9948599"},                 // a dated default overrides from its date
		{map[string]string{"LOB": "HOME"}, "2026-02-01", "9948501"},                    // but not a more specific rule
	}
	for _, c := range cases {
		if got := pick(c.vals, c.on); got != c.want {
			t.Errorf("%v on %s: got %s, want %s", c.vals, c.on, got, c.want)
		}
	}
	// A priority beats specificity.
	pri := assignmentDoc(t, func(c map[string]any) {
		c["accounts"] = append(c["accounts"].([]any), map[string]any{"code": "9948598", "name": "AR0 priority", "class": "AR0", "activeFrom": "2025-07-01"})
		c["accountAssignmentRules"] = append(c["accountAssignmentRules"].([]any), assign("AA_AR0_FIRST", "AR0", "9948598", nil, "priority", -1))
	})
	if a, err := AssignAccount(pri, demoRef, "AR0", map[string]string{"LOB": "MOTOR"}, day("2026-01-10")); err != nil || a.Account != "9948598" {
		t.Errorf("priority -1 wins: %+v %v", a, err)
	}
	// Order of rules in the configuration never matters.
	rev := assignmentDoc(t, func(c map[string]any) {
		rs := c["accountAssignmentRules"].([]any)
		for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
			rs[i], rs[j] = rs[j], rs[i]
		}
	})
	if got := accountsOfPlan(preview(t, rev, e1In())); !reflect.DeepEqual(got, []string{"AR0→9948523", "PW0→4100100", "TX0→2200100"}) {
		t.Errorf("reversed rule order: %v", got)
	}
}

// Tests: missing mappings and unsuitable accounts are refused, never redirected.
func TestMissingMappingsAndRefusedAccounts(t *testing.T) {
	noDefault := assignmentDoc(t, func(c map[string]any) {
		c["accountAssignmentRules"] = c["accountAssignmentRules"].([]any)[:1] // AR0 motor only
	})
	p, err := PreviewPosting(noDefault, demoRef, home(e1In()))
	if codeOf(err) != "MISSING_ACCOUNT" || !strings.Contains(err.Error(), "line 1 (AR0 D)") || p == nil || p.Lines[0].Problem == "" {
		t.Errorf("home AR0 has no rule: %v", err)
	}
	if p != nil && p.Lines[1].Problem == "" {
		t.Error("every unassignable line is annotated, not just the first")
	}
	// A same-company receipt's cash leg resolves to the C09-only bank account: refused.
	sameCo, err := PreviewPosting(assignmentDoc(t, nil), demoRef, e3In("BANK-C01"))
	if codeOf(err) != "ENTITY_NOT_ALLOWED" || sameCo.Lines[0].Account != "1000900" {
		t.Errorf("the C09 bank account must not take a C01 receipt: %v", err)
	}
	// An account closed before the book date.
	closed := assignmentDoc(t, func(c map[string]any) {
		for _, a := range c["accounts"].([]any) {
			if a.(map[string]any)["code"] == "2200100" {
				a.(map[string]any)["activeTo"] = "2026-01-01"
			}
		}
	})
	if _, err := PreviewPosting(closed, demoRef, e1In()); codeOf(err) != "INACTIVE_ACCOUNT" {
		t.Errorf("closed GST account: %v", err)
	}
	// Warnings that make these visible before anything runs.
	_, fs := parse(t, assignmentCatalog(func(c map[string]any) {
		c["accountAssignmentRules"] = c["accountAssignmentRules"].([]any)[:3]
		delete(c["events"].([]any)[1].(map[string]any), "dimensions")
		c["events"].([]any)[1].(map[string]any)["dimensions"] = []any{"COMPANY"}
	}))
	if !hasCode(fs, "MISSING_ASSIGNMENT") {
		t.Errorf("classes without rules are reported: %+v", fs)
	}
	var notCarried []string
	for _, f := range fs {
		if f.Code == "ASSIGNMENT_DIMENSION_NOT_CARRIED" {
			notCarried = append(notCarried, f.Message)
		}
	}
	if len(notCarried) != 1 || !strings.Contains(notCarried[0], "AA_AR0_MOTOR reads LOB, which event INVOICED does not carry") {
		t.Errorf("E2 without LOB would clear AR0 motor to the default account: %v", notCarried)
	}
}

func TestAssignmentRuleValidation(t *testing.T) {
	add := func(r map[string]any) func(c map[string]any) {
		return func(c map[string]any) {
			c["accountAssignmentRules"] = append(c["accountAssignmentRules"].([]any), r)
		}
	}
	cases := map[string]func(c map[string]any){
		"INVALID_CODE":            add(assign("aa", "AR0", "9948500", map[string]any{"LOB": "HOME"})),
		"DUPLICATE_RULE":          add(assign("AA_AR1", "AR0", "9948500", map[string]any{"LOB": "HOME"})),
		"UNKNOWN_CLASS":           add(assign("AA_X", "AR9", "9948500", nil)),
		"UNKNOWN_ACCOUNT":         add(assign("AA_X", "AR0", "9999999", map[string]any{"LOB": "HOME"})),
		"CLASS_MISMATCH":          add(assign("AA_X", "AR0", "9948600", map[string]any{"LOB": "HOME"})),
		"UNKNOWN_DIMENSION":       add(assign("AA_X", "AR0", "9948500", map[string]any{"COST_CENTRE": "X"})),
		"UNKNOWN_DIMENSION_VALUE": add(assign("AA_X", "AR0", "9948500", map[string]any{"LOB": "MOTR"})),
		"INVALID_MATCH":           add(assign("AA_X", "AR0", "9948500", map[string]any{"LOB": " "})),
		"INVALID_CURRENCY":        add(assign("AA_X", "AR0", "9948500", map[string]any{"CURRENCY": "aud"})),
		"INVALID_DATE":            add(assign("AA_X", "AR0", "9948500", map[string]any{"LOB": "HOME"}, "effectiveFrom", "2026-13-01")),
		"INVALID_WINDOW":          add(assign("AA_X", "AR0", "9948500", map[string]any{"LOB": "HOME"}, "effectiveFrom", "2026-02-01", "effectiveTo", "2026-01-01")),
		"INACTIVE_ACCOUNT":        add(assign("AA_X", "WO", "6100199", map[string]any{"LOB": "HOME"}, "effectiveFrom", "2026-02-01")),
	}
	for code, edit := range cases {
		_, fs := parse(t, assignmentCatalog(edit))
		if !hasCode(errorsOf(fs), code) {
			t.Errorf("%s: %+v", code, errorsOf(fs))
		}
	}
	// ENTITY and CURRENCY are matchable, and BOOKING_CLASS from the date policy.
	for _, r := range []map[string]any{
		assign("AA_X", "AR0", "9948500", map[string]any{"ENTITY": "C09"}, "priority", -1),
		assign("AA_Y", "AR0", "9948500", map[string]any{"CURRENCY": "NZD"}, "priority", -2),
	} {
		if _, fs := parse(t, assignmentCatalog(add(r))); len(errorsOf(fs)) > 0 {
			t.Errorf("%v: %+v", r, errorsOf(fs))
		}
	}
}

// withRule adds one assignment rule to the demo set.
func withRule(r map[string]any) func(c map[string]any) {
	return func(c map[string]any) { c["accountAssignmentRules"] = append(c["accountAssignmentRules"].([]any), r) }
}

// ALTB-03: an assignment rule may match on the event type, like ENTITY and CURRENCY. The more
// specific rule wins for that event only; other events keep their account.
func TestEventTypeChoosesTheAccountForThatEventOnly(t *testing.T) {
	d := assignmentDoc(t, withRule(assign("AA_AR0_INVOICED_MOTOR", "AR0", "9948500", map[string]any{"EVENT_TYPE": "INVOICED", "LOB": "MOTOR"})))
	e2 := e2In()
	e2.Dimensions["LOB"] = "MOTOR"
	p := preview(t, d, e2)
	for _, l := range p.Lines {
		if l.Class != "AR0" {
			continue
		}
		if l.Account != "9948500" || l.Assignment == nil || l.Assignment.Rule != "AA_AR0_INVOICED_MOTOR" {
			t.Fatalf("an INVOICED motor AR0 line goes to 9948500 by the event-type rule: %+v", l.Assignment)
		}
		if !strings.Contains(strings.Join(l.Assignment.Why, "; "), "EVENT_TYPE = INVOICED (exact)") {
			t.Errorf("the reason names the event type: %v", l.Assignment.Why)
		}
	}
	if got := accountsOfPlan(preview(t, d, e1In())); got[0] != "AR0→9948523" {
		t.Errorf("a PREM_ADDED motor AR0 line keeps 9948523: %v", got)
	}
}

// ALTB-03: two equally specific rules that one line could match are still refused, never guessed.
func TestEventTypeTiesAreRefused(t *testing.T) {
	fs := validateMap(assignmentCatalog(withRule(assign("AA_AR0_INVOICED", "AR0", "9948500", map[string]any{"EVENT_TYPE": "INVOICED"}))))
	if !hasCode(fs, "AMBIGUOUS_ASSIGNMENT") {
		t.Errorf("EVENT_TYPE = INVOICED and LOB = MOTOR tie for an INVOICED motor line: %v", findingCodes(fs))
	}
}

// ALTB-03: the event type must be one of the configuration's events; a rule for an event that
// never posts the class is flagged; and the event type is never a field to collect.
func TestEventTypeMatchValidationAndManifest(t *testing.T) {
	fs := validateMap(assignmentCatalog(withRule(assign("AA_AR0_NOPE", "AR0", "9948500", map[string]any{"EVENT_TYPE": "NOPE", "LOB": "HOME"}))))
	if !hasCode(fs, "UNKNOWN_EVENT_TYPE") {
		t.Errorf("an unknown event type is refused: %v", findingCodes(fs))
	}
	fs = validateMap(assignmentCatalog(withRule(assign("AA_AR0_PAY", "AR0", "9948500", map[string]any{"EVENT_TYPE": "PAYMENT_RECEIVED", "LOB": "HOME"}))))
	if !hasCode(fs, "ASSIGNMENT_NEVER_APPLIES") || hasCode(fs, "UNKNOWN_EVENT_TYPE") {
		t.Errorf("PAYMENT_RECEIVED posts no AR0 line, so the rule never applies: %v", findingCodes(fs))
	}
	c := assignmentCatalog(withRule(assign("AA_AR0_INVOICED_MOTOR", "AR0", "9948500", map[string]any{"EVENT_TYPE": "INVOICED", "LOB": "MOTOR"})))
	fs = validateMap(c)
	if hasCode(fs, "ASSIGNMENT_DIMENSION_NOT_CARRIED") || hasCode(fs, "UNKNOWN_DIMENSION") || len(errorsOf(fs)) > 0 {
		t.Errorf("EVENT_TYPE is matched from the event itself: %v", findingCodes(fs))
	}
	m, _ := CompileManifest(docOf(t, c), "DEMO", demoRef)
	for _, ev := range m.Events {
		for _, f := range ev.Fields {
			if f.FieldCode == "EVENT_TYPE" {
				t.Errorf("%s asks the source for EVENT_TYPE", ev.EventType)
			}
		}
	}
}

// ALTB-03: a line's dimensions cannot overwrite the event type it is matched on.
func TestEventTypeCannotBeOverwrittenByADimension(t *testing.T) {
	d := assignmentDoc(t, withRule(assign("AA_AR0_INVOICED_MOTOR", "AR0", "9948500", map[string]any{"EVENT_TYPE": "INVOICED", "LOB": "MOTOR"})))
	p := &PostingPlan{EventType: "INVOICED", Lines: []PlannedLine{{Class: "AR0", Side: "C", Amount: "1.00", Currency: "AUD", Entity: "C01",
		Keys: map[string]string{"POLICY_NO": "P1"}, Dimensions: map[string]string{"LOB": "MOTOR", "EVENT_TYPE": "PREM_ADDED"}}}}
	_ = AssignAccounts(d, demoRef, p, day("2026-01-05"))
	if a := p.Lines[0].Assignment; a == nil || a.Rule != "AA_AR0_INVOICED_MOTOR" {
		t.Errorf("the plan's event type is used, not a dimension value: %+v", a)
	}
}
