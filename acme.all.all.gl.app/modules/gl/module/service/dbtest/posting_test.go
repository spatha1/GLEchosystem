package dbtest

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// postingConfig: the chart of accounts configuration plus GROSS and posting rules for E1, E2.
func postingConfig(taxAmount string) map[string]any {
	c := coaConfig("9948500", "9948600")
	for _, e := range c["events"].([]any) {
		ev := e.(map[string]any)
		if ev["code"] != "PAYMENT_RECEIVED" {
			ev["dimensions"] = []any{"COMPANY"}
		}
	}
	c["accountingClasses"] = append(c["accountingClasses"].([]any), map[string]any{"code": "TX0", "name": "Tax payable", "nature": "LIABILITY"})
	c["accounts"] = append(c["accounts"].([]any), map[string]any{"code": "2200100", "name": "GST payable", "class": "TX0", "activeFrom": "2025-07-01"})
	c["derivedMeasures"] = []any{map[string]any{"code": "GROSS", "formula": "PREMIUM + TAX", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}}
	for _, a := range c["accounts"].([]any) {
		delete(a.(map[string]any), "activeTo") // premium posts in 2026
	}
	aa := func(code, class, account string) map[string]any {
		return map[string]any{"code": code, "class": class, "account": account}
	}
	c["accountAssignmentRules"] = []any{aa("AA_AR0", "AR0", "9948500"), aa("AA_AR1", "AR1", "9948600"), aa("AA_PR0", "PR0", "1000900"),
		aa("AA_PW0", "PW0", "4100100"), aa("AA_TX0", "TX0", "2200100")}
	l := func(code, class, side, amount, expandBy string) map[string]any {
		x := map[string]any{"code": code, "class": class, "side": side, "amount": amount}
		if expandBy != "" {
			x["expandBy"] = expandBy
		}
		return x
	}
	c["postingRules"] = []any{
		map[string]any{"code": "PR_PREM_ADDED", "eventType": "PREM_ADDED", "rank": 10, "lines": []any{
			l("AR0_DR", "AR0", "D", "GROSS", ""), l("PW0_CR", "PW0", "C", "GROSS - "+taxAmount, ""), l("TX0_CR", "TX0", "C", taxAmount, "")}},
		map[string]any{"code": "PR_INVOICED", "eventType": "INVOICED", "rank": 10, "lines": []any{
			l("AR1_DR", "AR1", "D", "INVOICE_AMT", "INVOICE_LIST"), l("AR0_CR", "AR0", "C", "INVOICE_AMT", "INVOICE_LIST")}},
		map[string]any{"code": "PR_PAYMENT", "eventType": "PAYMENT_RECEIVED", "rank": 10, "lines": []any{
			l("CASH", "PR0", "D", "PAY_AMT", ""), l("ALLOC", "AR1", "C", "ALLOC_AMT", "ALLOCATION_LIST")}},
	}
	return c
}

var e1Body = map[string]any{"eventType": "PREM_ADDED", "currency": "AUD", "transactionDate": "2026-01-01", "effectiveDate": "2026-01-01",
	"keys": map[string]string{"POLICY_NO": "P1", "TERM_NO": "1"}, "measures": map[string]string{"PREMIUM": "1000.00", "TAX": "100.00"},
	"dimensions": map[string]string{"COMPANY": "C01"}}

type planReply struct {
	Plan struct {
		Rule   string `json:"rule"`
		Config struct {
			ConfigVersion int `json:"configVersion"`
		} `json:"config"`
		Lines []struct {
			Class, Side, Amount, Entity, Account string
			Item                                 int
			Keys                                 map[string]string
			Assignment                           struct {
				Rule   string `json:"rule"`
				Config struct {
					ConfigVersion int `json:"configVersion"`
				} `json:"config"`
			} `json:"assignment"`
		} `json:"lines"`
		Balanced bool `json:"balanced"`
	} `json:"plan"`
}

func planOf(t *testing.T, r result) planReply {
	t.Helper()
	mustStatus(t, r, 200)
	var p planReply
	if err := json.Unmarshal([]byte(r.Raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func legsOf(p planReply) []string {
	var out []string
	for _, l := range p.Plan.Lines {
		out = append(out, l.Class+" "+l.Side+" "+l.Amount)
	}
	return out
}

// US-012 through the store: E1 and E2 plans from stored rules, and a rule change published as
// a new approved version — no engine change — changes what posts from its effective date.
func TestPostingRulesAreVersionedData(t *testing.T) {
	needDB(t)
	id := uniq("POST")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": postingConfig("TAX"), "reason": "demo rules"}), 201)
	approve(t, tenantA, id, "1")

	p1 := planOf(t, do(t, "POST", "/configs/"+id+"/versions/1/plan", tenantA, viewer, e1Body))
	if !reflect.DeepEqual(legsOf(p1), []string{"AR0 D 1100.00", "PW0 C 1000.00", "TX0 C 100.00"}) || !p1.Plan.Balanced || p1.Plan.Rule != "PR_PREM_ADDED" || p1.Plan.Config.ConfigVersion != 1 {
		t.Fatalf("E1 under version 1: %+v", p1)
	}
	// US-013: the preview shows each leg's account, the rule that chose it and the version.
	if l := p1.Plan.Lines[0]; l.Account != "9948500" || l.Assignment.Rule != "AA_AR0" || l.Assignment.Config.ConfigVersion != 1 {
		t.Errorf("AR0 leg assignment: %+v", l)
	}
	e2 := map[string]any{"eventType": "INVOICED", "currency": "AUD", "transactionDate": "2026-01-05", "keys": map[string]string{"POLICY_NO": "P1", "TERM_NO": "1", "INVOICE_RUN": "RUN1"},
		"dimensions": map[string]string{"COMPANY": "C01"},
		"lists":      map[string]any{"INVOICE_LIST": []any{map[string]string{"INVOICE_NO": "INV1", "INVOICE_AMT": "550.00"}, map[string]string{"INVOICE_NO": "INV2", "INVOICE_AMT": "550.00"}}}}
	p2 := planOf(t, do(t, "POST", "/configs/"+id+"/versions/1/plan", tenantA, viewer, e2))
	if !reflect.DeepEqual(legsOf(p2), []string{"AR1 D 550.00", "AR1 D 550.00", "AR0 C 550.00", "AR0 C 550.00"}) || p2.Plan.Lines[1].Keys["INVOICE_NO"] != "INV2" {
		t.Errorf("E2 expands two pairs: %+v", p2)
	}

	// Version 2 changes the tax leg's formula (tax recomputed at 10% of premium) from February.
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"effectiveFrom": "2026-02-01", "content": postingConfig("ROUND(PREMIUM / 10, 2)"), "reason": "tax from premium"}), 201)
	approve(t, tenantA, id, "2")
	odd := map[string]any{}
	for k, v := range e1Body {
		odd[k] = v
	}
	odd["measures"] = map[string]string{"PREMIUM": "1000.00", "TAX": "99.00"}
	if got := legsOf(planOf(t, do(t, "POST", "/configs/"+id+"/versions/1/plan", tenantA, viewer, odd))); !reflect.DeepEqual(got, []string{"AR0 D 1099.00", "PW0 C 1000.00", "TX0 C 99.00"}) {
		t.Errorf("version 1 posts the source tax: %v", got)
	}
	if got := legsOf(planOf(t, do(t, "POST", "/configs/"+id+"/versions/2/plan", tenantA, viewer, odd))); !reflect.DeepEqual(got, []string{"AR0 D 1099.00", "PW0 C 999.00", "TX0 C 100.00"}) {
		t.Errorf("version 2 posts the recomputed tax: %v", got)
	}
	eff := do(t, "GET", "/configs/"+id+"/effective?date=2026-02-15", tenantA, viewer, nil)
	if eff.num("version") != 2 {
		t.Errorf("version 2 is in force from February: %s", eff.Raw)
	}

	// Refusals are explicit, tenants are isolated, and nothing is written.
	missing := map[string]any{"eventType": "PREM_ADDED", "currency": "AUD", "keys": map[string]string{"POLICY_NO": "P1", "TERM_NO": "1"},
		"measures": map[string]string{"PREMIUM": "1000.00", "TAX": "100.00"}}
	if r := do(t, "POST", "/configs/"+id+"/versions/1/plan", tenantA, viewer, missing); r.Status != 422 || r.code() != "MISSING_ENTITY" {
		t.Errorf("a leg without a company: %s", r.Raw)
	}
	if r := do(t, "POST", "/configs/"+id+"/versions/1/plan", tenantB, viewer, e1Body); r.Status != 404 {
		t.Errorf("tenant B planned with tenant A's rules: %s", r.Raw)
	}
	a := do(t, "GET", "/configs/"+id+"/audit", tenantA, viewer, nil)
	if n := strings.Count(a.Raw, `"action":`); n != 7 { // create_artifact, create_version, validate, approve (v1); create_version, validate, approve (v2)
		t.Errorf("planning must not write: %d audit rows: %s", n, a.Raw)
	}
}

// An unbalanced rule is shown with its plan, so it can be fixed — and validation would not
// have caught it, because balance depends on values.
func TestUnbalancedPlansAreRefusedWithThePlan(t *testing.T) {
	needDB(t)
	id := uniq("UNBAL")
	c := postingConfig("TAX")
	c["postingRules"].([]any)[0].(map[string]any)["lines"].([]any)[1].(map[string]any)["amount"] = "PREMIUM - 1"
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": c, "reason": "bad rule"}), 201)
	r := do(t, "POST", "/configs/"+id+"/versions/1/plan", tenantA, viewer, e1Body)
	if r.Status != 422 || r.code() != "UNBALANCED" || !strings.Contains(r.Raw, `"debit":"1100.00"`) || !strings.Contains(r.Raw, `"credit":"1099.00"`) {
		t.Errorf("unbalanced: %s", r.Raw)
	}
}

// US-013 through the store: a class without an assignment rule is refused at preview, with
// the plan annotated line by line — no fallback account.
func TestPreviewRefusesALegWithoutAnAccount(t *testing.T) {
	needDB(t)
	id := uniq("NOACCT")
	c := postingConfig("TAX")
	rules := c["accountAssignmentRules"].([]any)
	c["accountAssignmentRules"] = rules[:len(rules)-1] // drop AA_TX0
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": c, "reason": "no tax mapping"}), 201)
	v := do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil)
	if !strings.Contains(v.Raw, "MISSING_ASSIGNMENT") {
		t.Errorf("validation warns about the unmapped class: %s", v.Raw)
	}
	r := do(t, "POST", "/configs/"+id+"/versions/1/plan", tenantA, viewer, e1Body)
	if r.Status != 422 || r.code() != "MISSING_ACCOUNT" || !strings.Contains(r.Raw, "line 3 (TX0 C)") || !strings.Contains(r.Raw, `"problem":"MISSING_ACCOUNT`) || !strings.Contains(r.Raw, `"account":"9948500"`) {
		t.Errorf("the TX0 leg has no account: %s", r.Raw)
	}
}
