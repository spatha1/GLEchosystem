package dbtest

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// keyConfig: the P1 catalog with invoices, allocations and key profiles (US-010).
func keyConfig() map[string]any {
	c := catalog(map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"},
		map[string]any{"eventType": "INVOICED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"})
	c["events"] = []any{
		map[string]any{"code": "PREM_ADDED", "economicKey": []any{"POLICY_NO", "TERM_NO"}, "measures": []any{"PREMIUM", "TAX"}},
		map[string]any{"code": "INVOICED", "economicKey": []any{"POLICY_NO", "TERM_NO", "INVOICE_RUN"},
			"lists": []any{map[string]any{"code": "INVOICE_LIST", "itemKey": []any{"INVOICE_NO"}, "measures": []any{"INVOICE_AMT"}}}},
		map[string]any{"code": "PAYMENT_RECEIVED", "economicKey": []any{"RECEIPT_NO"}, "measures": []any{"PAY_AMT"},
			"lists": []any{map[string]any{"code": "ALLOCATION_LIST", "itemKey": []any{"ALLOCATION_ID"}, "keys": []any{"INVOICE_NO"}, "measures": []any{"ALLOC_AMT"}}}},
	}
	manyToOne := func(to ...string) []any {
		var out []any
		for _, t := range to {
			out = append(out, map[string]any{"to": t, "cardinality": "MANY_TO_ONE"})
		}
		return out
	}
	c["keyProfiles"] = []any{
		map[string]any{"code": "POLICY_NO", "pattern": "P[0-9]+"},
		map[string]any{"code": "TERM_NO", "scope": []any{"POLICY_NO"}, "links": manyToOne("POLICY_NO")},
		map[string]any{"code": "INVOICE_RUN", "scope": []any{"TERM_NO"}, "links": manyToOne("TERM_NO")},
		map[string]any{"code": "INVOICE_NO", "pattern": "INV[0-9]+", "links": manyToOne("POLICY_NO", "INVOICE_RUN")},
		map[string]any{"code": "RECEIPT_NO"},
		map[string]any{"code": "ALLOCATION_ID", "scope": []any{"RECEIPT_NO"}, "links": manyToOne("RECEIPT_NO", "INVOICE_NO")},
	}
	return c
}

func p1Events() []any {
	return []any{
		map[string]any{"sourceSystem": "POLICY", "sourceEventId": "E1", "sourceEventVersion": 1, "eventType": "PREM_ADDED", "currency": "AUD",
			"keys": map[string]string{"POLICY_NO": "P1", "TERM_NO": "1"}, "measures": map[string]string{"PREMIUM": "1000.00", "TAX": "100.00"}},
		map[string]any{"sourceSystem": "BILLING", "sourceEventId": "E2", "sourceEventVersion": 1, "eventType": "INVOICED", "currency": "AUD",
			"keys":  map[string]string{"POLICY_NO": "P1", "TERM_NO": "1", "INVOICE_RUN": "RUN1"},
			"lists": map[string]any{"INVOICE_LIST": []any{map[string]string{"INVOICE_NO": "INV1", "INVOICE_AMT": "550.00"}, map[string]string{"INVOICE_NO": "INV2", "INVOICE_AMT": "550.00"}}}},
		map[string]any{"sourceSystem": "BILLING", "sourceEventId": "E3", "sourceEventVersion": 1, "eventType": "PAYMENT_RECEIVED", "currency": "AUD",
			"keys": map[string]string{"RECEIPT_NO": "R1"}, "measures": map[string]string{"PAY_AMT": "1099.50"},
			"lists": map[string]any{"ALLOCATION_LIST": []any{map[string]string{"ALLOCATION_ID": "A1", "INVOICE_NO": "INV1", "ALLOC_AMT": "550.00"},
				map[string]string{"ALLOCATION_ID": "A2", "INVOICE_NO": "INV2", "ALLOC_AMT": "549.50"}}}},
	}
}

type graphReply struct {
	Results []struct {
		SourceEventID string `json:"sourceEventId"`
		Result        string `json:"result"`
	} `json:"results"`
	Exceptions []struct {
		Category, SourceEventID, Detail string
	} `json:"exceptions"`
	Rebuild []struct {
		Traversal string              `json:"traversal"`
		Keys      map[string][]string `json:"keys"`
		Events    []string            `json:"events"`
	} `json:"rebuild"`
}

func graph(t *testing.T, r result) graphReply {
	t.Helper()
	mustStatus(t, r, 200)
	var g graphReply
	if err := json.Unmarshal([]byte(r.Raw), &g); err != nil {
		t.Fatal(err)
	}
	return g
}

var policyP1 = []any{map[string]any{"key": "POLICY_NO", "values": map[string]string{"POLICY_NO": "P1"}}}

// US-010 through the store: rebuild-by-policy over a stored configuration version, with key
// exceptions reported and nothing written.
func TestKeyGraphOverTheAPI(t *testing.T) {
	needDB(t)
	id := uniq("KEYS")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": keyConfig(), "reason": "keys"}), 201)
	path := "/configs/" + id + "/versions/1/keygraph"

	bad := map[string]any{"sourceSystem": "BILLING", "sourceEventId": "E9", "sourceEventVersion": 1, "eventType": "INVOICED", "currency": "AUD",
		"keys":  map[string]string{"POLICY_NO": "P7", "TERM_NO": "1", "INVOICE_RUN": "RUN1"},
		"lists": map[string]any{"INVOICE_LIST": []any{map[string]string{"INVOICE_NO": "INV1"}}}}
	g := graph(t, do(t, "POST", path, tenantA, viewer, map[string]any{"events": append(p1Events(), bad, p1Events()[0]), "rebuild": policyP1}))
	var results []string
	for _, r := range g.Results {
		results = append(results, r.SourceEventID+":"+r.Result)
	}
	if !reflect.DeepEqual(results, []string{"E1:ACCEPTED", "E2:ACCEPTED", "E3:ACCEPTED", "E9:REJECTED", "E1:UNCHANGED"}) {
		t.Errorf("results: %v", results)
	}
	if len(g.Exceptions) != 1 || g.Exceptions[0].Category != "KEY_CONFLICT" || !strings.Contains(g.Exceptions[0].Detail, "INVOICE_NO INV1 already belongs to POLICY_NO P1, not P7") {
		t.Errorf("exceptions: %+v", g.Exceptions)
	}
	want := map[string][]string{"ALLOCATION_ID": {"R1/A1", "R1/A2"}, "INVOICE_NO": {"INV1", "INV2"}, "INVOICE_RUN": {"P1/1/RUN1"}, "RECEIPT_NO": {"R1"}, "TERM_NO": {"P1/1"}}
	if len(g.Rebuild) != 1 || !reflect.DeepEqual(g.Rebuild[0].Keys, want) || !reflect.DeepEqual(g.Rebuild[0].Events, []string{"POLICY/E1", "BILLING/E2", "BILLING/E3"}) {
		t.Errorf("rebuild P1: %+v", g.Rebuild)
	}

	// Query errors are explicit.
	termOnly := []any{map[string]any{"key": "TERM_NO", "values": map[string]string{"TERM_NO": "1"}}}
	if r := do(t, "POST", path, tenantA, viewer, map[string]any{"events": p1Events(), "rebuild": termOnly}); r.Status != 422 || r.code() != "MISSING_KEY" {
		t.Errorf("a scoped key needs its scope: %s", r.Raw)
	}
	if r := do(t, "POST", path, tenantA, viewer, map[string]any{"events": p1Events(), "surprise": true}); r.Status != 400 {
		t.Errorf("unknown request fields are refused: %s", r.Raw)
	}

	// Nothing was written: the audit trail holds only the creation.
	a := do(t, "GET", "/configs/"+id+"/audit", tenantA, viewer, nil)
	if strings.Count(a.Raw, `"action":`) != 2 { // create_artifact, create_version
		t.Errorf("the key graph simulator must not write: %s", a.Raw)
	}
}

// Tests: cross-tenant keys. The same configuration id, policy and invoice numbers in two
// tenants stay apart, and an event naming another tenant is refused without being read.
func TestKeysNeverCrossTenants(t *testing.T) {
	needDB(t)
	id := uniq("XKEYS")
	for _, tn := range []string{tenantA, tenantB} {
		mustStatus(t, do(t, "POST", "/configs", tn, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": keyConfig(), "reason": "keys"}), 201)
	}
	path := "/configs/" + id + "/versions/1/keygraph"
	theirs := map[string]any{"sourceSystem": "BILLING", "sourceEventId": "E2", "sourceEventVersion": 1, "eventType": "INVOICED", "currency": "AUD",
		"keys":  map[string]string{"POLICY_NO": "P1", "TERM_NO": "1", "INVOICE_RUN": "RUN1"},
		"lists": map[string]any{"INVOICE_LIST": []any{map[string]string{"INVOICE_NO": "INV9"}}}}

	a := graph(t, do(t, "POST", path, tenantA, viewer, map[string]any{"events": p1Events(), "rebuild": policyP1}))
	b := graph(t, do(t, "POST", path, tenantB, viewer, map[string]any{"events": []any{theirs}, "rebuild": policyP1}))
	if got := a.Rebuild[0].Keys["INVOICE_NO"]; !reflect.DeepEqual(got, []string{"INV1", "INV2"}) {
		t.Errorf("tenant A's P1: %v", got)
	}
	if got := b.Rebuild[0].Keys["INVOICE_NO"]; !reflect.DeepEqual(got, []string{"INV9"}) {
		t.Errorf("tenant B's P1: %v", got)
	}

	// Tenant B sends an event stamped as tenant A's: refused, nothing echoed back.
	forged := map[string]any{"tenantId": tenantA}
	for k, v := range theirs {
		forged[k] = v
	}
	f := do(t, "POST", path, tenantB, viewer, map[string]any{"events": []any{forged}, "rebuild": policyP1})
	fg := graph(t, f)
	if fg.Results[0].Result != "REJECTED" || len(fg.Exceptions) != 1 || fg.Exceptions[0].Category != "TENANT_MISMATCH" {
		t.Errorf("forged tenant: %s", f.Raw)
	}
	if strings.Contains(fg.Exceptions[0].Detail, "INV9") || fg.Exceptions[0].SourceEventID != "" || len(fg.Rebuild[0].Keys) != 0 {
		t.Errorf("nothing of the refused event may appear: %s", f.Raw)
	}

	// Tenant B cannot reach tenant A's configuration at all, and outsiders are refused.
	if r := do(t, "POST", "/configs/"+uniq("NONE")+"/versions/1/keygraph", tenantB, viewer, map[string]any{"events": []any{}}); r.Status != 404 {
		t.Errorf("unknown configuration: %s", r.Raw)
	}
	if r := do(t, "POST", path, tenantB, outsider, map[string]any{"events": p1Events()}); r.code() != "NO_TENANT_ACCESS" {
		t.Errorf("outsider: %s", r.Raw)
	}
}
