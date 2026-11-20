package dbtest

import (
	"encoding/json"
	"reflect"
	"testing"
)

// coaConfig: the key configuration plus dimensions, the demo classes and one client's codes.
func coaConfig(ar0, ar1 string) map[string]any {
	c := keyConfig()
	for _, e := range c["events"].([]any) {
		if e.(map[string]any)["code"] == "PAYMENT_RECEIVED" {
			e.(map[string]any)["dimensions"] = []any{"BANK_ACCOUNT", "COMPANY"}
		}
	}
	c["dimensions"] = []any{map[string]any{"code": "COMPANY", "values": []any{"C01", "C09"}}, map[string]any{"code": "BANK_ACCOUNT"}}
	c["accountingClasses"] = []any{
		map[string]any{"code": "AR0", "name": "Policy receivable", "nature": "ASSET"},
		map[string]any{"code": "AR1", "name": "Invoice receivable", "nature": "ASSET"},
		map[string]any{"code": "PR0", "name": "Cash at bank", "nature": "ASSET"},
		map[string]any{"code": "PW0", "name": "Premium written", "nature": "INCOME"},
	}
	c["accounts"] = []any{
		map[string]any{"code": ar0, "name": "Policy receivable", "class": "AR0", "activeFrom": "2025-07-01", "subledgerKey": "POLICY_NO"},
		map[string]any{"code": ar1, "name": "Invoice receivable", "class": "AR1", "activeFrom": "2025-07-01", "subledgerKey": "INVOICE_NO"},
		map[string]any{"code": "1000900", "name": "Operating bank", "class": "PR0", "activeFrom": "2025-07-01", "entities": []any{"C09"}, "dimensions": []any{"BANK_ACCOUNT"}},
		map[string]any{"code": "4100100", "name": "Gross written premium", "class": "PW0", "activeFrom": "2025-07-01", "activeTo": "2026-01-01"},
	}
	return c
}

type accountsReply struct {
	Accounts []struct {
		Code, Class, Ledger, Nature, NormalSide, Statement string
	} `json:"accounts"`
	Requirements struct {
		Keys       []string `json:"keys"`
		Dimensions []string `json:"dimensions"`
	} `json:"requirements"`
	Carrier struct {
		OK       bool                    `json:"ok"`
		Findings []struct{ Code string } `json:"findings"`
	} `json:"carrier"`
	Allowed bool   `json:"allowed"`
	Code    string `json:"code"`
}

func accountsOf(t *testing.T, r result) accountsReply {
	t.Helper()
	mustStatus(t, r, 200)
	var a accountsReply
	if err := json.Unmarshal([]byte(r.Raw), &a); err != nil {
		t.Fatal(err)
	}
	return a
}

// US-011 through the store: two clients give the same class their own codes; lookups,
// requirements and the posting gate answer per tenant.
func TestClientsMapClassesToTheirOwnAccounts(t *testing.T) {
	needDB(t)
	id := uniq("COA")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": coaConfig("9948500", "9948600"), "reason": "client A chart"}), 201)
	mustStatus(t, do(t, "POST", "/configs", tenantB, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": coaConfig("1200-AR", "1210-INV"), "reason": "client B chart"}), 201)
	base := "/configs/" + id + "/versions/1/accounts"

	a := accountsOf(t, do(t, "GET", base+"?class=AR0&date=2026-01-10", tenantA, viewer, nil))
	b := accountsOf(t, do(t, "GET", base+"?class=AR0&date=2026-01-10", tenantB, viewer, nil))
	if len(a.Accounts) != 1 || a.Accounts[0].Code != "9948500" || len(b.Accounts) != 1 || b.Accounts[0].Code != "1200-AR" {
		t.Fatalf("AR0 per client: A %+v, B %+v", a.Accounts, b.Accounts)
	}
	if x := a.Accounts[0]; x.Nature != "ASSET" || x.NormalSide != "D" || x.Statement != "BALANCE_SHEET" || x.Ledger != "PRIMARY" {
		t.Errorf("the class describes the account: %+v", x)
	}
	if !reflect.DeepEqual(a.Requirements.Keys, []string{"POLICY_NO"}) {
		t.Errorf("AR0 requires POLICY_NO: %+v", a.Requirements)
	}
	// Effective dates: PW0's only account closed at the start of 2026.
	if pw := accountsOf(t, do(t, "GET", base+"?class=PW0&date=2026-01-10", tenantA, viewer, nil)); len(pw.Accounts) != 0 {
		t.Errorf("no active PW0 account in 2026: %+v", pw.Accounts)
	}
	if pw := accountsOf(t, do(t, "GET", base+"?class=PW0&date=2025-12-31", tenantA, viewer, nil)); len(pw.Accounts) != 1 {
		t.Errorf("PW0 was active in 2025: %+v", pw.Accounts)
	}

	// Before intake: can PAYMENT_RECEIVED feed AR1 lines? Per allocation, yes; per receipt, no.
	if c := accountsOf(t, do(t, "GET", base+"?class=AR1&eventType=PAYMENT_RECEIVED&list=ALLOCATION_LIST", tenantA, viewer, nil)); !c.Carrier.OK {
		t.Errorf("allocations carry INVOICE_NO: %+v", c.Carrier)
	}
	if c := accountsOf(t, do(t, "GET", base+"?class=AR1&eventType=PAYMENT_RECEIVED", tenantA, viewer, nil)); c.Carrier.OK || len(c.Carrier.Findings) != 1 || c.Carrier.Findings[0].Code != "MISSING_SUBLEDGER_KEY" {
		t.Errorf("a receipt has no invoice: %+v", c.Carrier)
	}

	// The posting gate.
	check := func(tenant string, line map[string]any) accountsReply {
		return accountsOf(t, do(t, "POST", "/configs/"+id+"/versions/1/accounts/check", tenant, viewer, line))
	}
	line := map[string]any{"account": "9948500", "class": "AR0", "bookDate": "2026-01-01", "entity": "C01", "currency": "AUD", "keys": map[string]string{"POLICY_NO": "P1"}}
	if r := check(tenantA, line); !r.Allowed {
		t.Errorf("E1's AR0 line posts for client A: %+v", r)
	}
	if r := check(tenantB, line); r.Allowed || r.Code != "MISSING_ACCOUNT" {
		t.Errorf("client A's code means nothing to client B: %+v", r)
	}
	noKey := map[string]any{"account": "9948500", "class": "AR0", "bookDate": "2026-01-01", "entity": "C01", "currency": "AUD"}
	if r := check(tenantA, noKey); r.Allowed || r.Code != "MISSING_SUBLEDGER_KEY" {
		t.Errorf("AR0 needs POLICY_NO: %+v", r)
	}
	closed := map[string]any{"account": "4100100", "class": "PW0", "bookDate": "2026-01-01", "entity": "C01", "currency": "AUD"}
	if r := check(tenantA, closed); r.Allowed || r.Code != "INACTIVE_ACCOUNT" {
		t.Errorf("a closed account blocks posting: %+v", r)
	}
	if r := do(t, "POST", "/configs/"+id+"/versions/1/accounts/check", tenantA, viewer, map[string]any{"account": "9948500", "class": "AR0", "bookDate": "1/1/2026", "currency": "AUD"}); r.Status != 400 {
		t.Errorf("a malformed date is a bad request: %s", r.Raw)
	}
	if r := do(t, "GET", base+"?eventType=PAYMENT_RECEIVED", tenantA, viewer, nil); r.Status != 400 {
		t.Errorf("a carrier check needs the class: %s", r.Raw)
	}
	if r := do(t, "GET", base, tenantB, outsider, nil); r.code() != "NO_TENANT_ACCESS" {
		t.Errorf("outsider: %s", r.Raw)
	}
}
