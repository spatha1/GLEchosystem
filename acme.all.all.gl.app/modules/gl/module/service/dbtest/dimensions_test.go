package dbtest

import (
	"encoding/json"
	"strings"
	"testing"
)

func lookupConfig(bankC09 string) map[string]any {
	c := catalog(map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"})
	c["events"].([]any)[1].(map[string]any)["dimensions"] = []any{"BANK_ACCOUNT", "COMPANY"}
	c["dimensions"] = []any{
		map[string]any{"code": "COMPANY", "values": []any{"C01", "C09"}},
		map[string]any{"code": "BANK_ACCOUNT"},
		map[string]any{"code": "BANKING_COMPANY", "values": []any{"C01", "C09"}, "derivedBy": map[string]any{"lookup": "BANKING_COMPANY_BY_ACCOUNT"}},
	}
	c["lookups"] = []any{map[string]any{"code": "BANKING_COMPANY_BY_ACCOUNT", "input": "BANK_ACCOUNT", "output": "BANKING_COMPANY",
		"entries": []any{map[string]any{"key": "BANK-C09", "value": bankC09}}}}
	return c
}

func bankingCompany(t *testing.T, r result) (string, int) {
	t.Helper()
	var body struct {
		Dimensions []struct {
			Dimension, Value, Lookup, Key string
			Config                        struct {
				ConfigVersion int `json:"configVersion"`
			} `json:"config"`
		} `json:"dimensions"`
	}
	if err := json.Unmarshal([]byte(r.Raw), &body); err != nil {
		t.Fatalf("%v: %s", err, r.Raw)
	}
	for _, d := range body.Dimensions {
		if d.Dimension == "BANKING_COMPANY" {
			if d.Lookup != "BANKING_COMPANY_BY_ACCOUNT" || d.Key != "BANK-C09" {
				t.Errorf("lookup lineage: %+v", d)
			}
			return d.Value, d.Config.ConfigVersion
		}
	}
	t.Fatalf("no BANKING_COMPANY in %s", r.Raw)
	return "", 0
}

// US-008 AC: the BANK_ACCOUNT → BANKING_COMPANY lookup is versioned and audited. Changing it is a
// new configuration version, edited with a reason, approved by someone else; the old version
// keeps its old answer; and every resolution names the version that produced it.
func TestLookupChangesAreVersionedApprovedAndAudited(t *testing.T) {
	needDB(t)
	id := uniq("LKP")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": lookupConfig("C09"), "reason": "banking companies"}), 201)
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil), 200)
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, rev, reason("matches treasury's account list")), 200)

	v1 := do(t, "GET", "/configs/"+id+"/versions/1", tenantA, ba, nil)
	if r := do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, map[string]any{"baseDigest": v1.str("contentDigest"), "content": lookupConfig("C01"), "reason": "edit in place"}); r.code() != "CONFIG_IMMUTABLE" {
		t.Fatalf("the approved lookup was editable: %s", r.Raw)
	}

	v2 := do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"basedOnVersion": 1, "effectiveFrom": "2026-04-01", "reason": "prepare re-homing"})
	mustStatus(t, v2, 201)
	mustStatus(t, do(t, "PUT", "/configs/"+id+"/versions/2", tenantA, ba, map[string]any{"baseDigest": v2.str("contentDigest"), "content": lookupConfig("C01"), "reason": "BANK-C09 re-homed to C01 from April"}), 200)
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/2/validate", tenantA, ba, nil), 200)
	if r := do(t, "POST", "/configs/"+id+"/versions/2/approve", tenantA, ba, reason("self")); r.code() != "FORBIDDEN" {
		t.Errorf("the BA approved the lookup change: %s", r.Raw)
	}
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/2/approve", tenantA, rev, reason("treasury confirmed re-homing")), 200)

	pay := map[string]any{"eventType": "PAYMENT_RECEIVED", "currency": "AUD", "measures": map[string]string{"PAY_AMT": "1099.50"},
		"dimensions": map[string]string{"BANK_ACCOUNT": "BANK-C09", "COMPANY": "C01"}}
	c1, n1 := bankingCompany(t, do(t, "POST", "/configs/"+id+"/versions/1/evaluate", tenantA, viewer, pay))
	c2, n2 := bankingCompany(t, do(t, "POST", "/configs/"+id+"/versions/2/evaluate", tenantA, viewer, pay))
	if c1 != "C09" || n1 != 1 || c2 != "C01" || n2 != 2 {
		t.Errorf("v1 → %s (v%d), v2 → %s (v%d); want C09 (v1), C01 (v2)", c1, n1, c2, n2)
	}
	for date, want := range map[string]int{"2026-03-31": 1, "2026-04-01": 2} {
		if e := do(t, "GET", "/configs/"+id+"/effective?date="+date, tenantA, viewer, nil); e.num("version") != want {
			t.Errorf("effective on %s: v%d, want v%d", date, e.num("version"), want)
		}
	}
	a := do(t, "GET", "/configs/"+id+"/audit?version=2", tenantA, viewer, nil)
	for _, want := range []string{`"action":"update_draft","actor":"ba@example.com","reason":"BANK-C09 re-homed to C01 from April"`, `"action":"approve","actor":"reviewer@example.com","reason":"treasury confirmed re-homing"`} {
		if !strings.Contains(a.Raw, want) {
			t.Errorf("audit lacks %s: %s", want, a.Raw)
		}
	}
	// Unknown codes are refused at the API too.
	bad := map[string]any{"eventType": "PAYMENT_RECEIVED", "currency": "AUD", "measures": map[string]string{"PAY_AMT": "1"}, "dimensions": map[string]string{"BANK_ACCOUNT": "BANK-XX", "COMPANY": "C01"}}
	if r := do(t, "POST", "/configs/"+id+"/versions/1/evaluate", tenantA, viewer, bad); r.Status != 422 || r.code() != "UNKNOWN_CODE" {
		t.Errorf("unknown bank account: %s", r.Raw)
	}
}
