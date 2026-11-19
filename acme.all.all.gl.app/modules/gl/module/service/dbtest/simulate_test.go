package dbtest

import (
	"encoding/json"
	"strings"
	"testing"
)

func formulaConfig(formula string) map[string]any {
	c := catalog(map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"})
	c["derivedMeasures"] = []any{
		map[string]any{"code": "GROSS", "formula": formula, "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}},
		map[string]any{"code": "TAX_RATE", "formula": "TAX / PREMIUM", "resultType": "NUMBER", "eventTypes": []any{"PREM_ADDED"}},
	}
	return c
}

// US-007 through the store: the simulator evaluates a version's formulas on a sample event and
// returns lineage naming the exact configuration version; nothing is stored or posted.
func TestSimulatorEvaluatesWithLineage(t *testing.T) {
	needDB(t)
	id := uniq("SIM")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": formulaConfig("PREMIUM + TAX"), "reason": "formulas"}), 201)
	e1 := map[string]any{"eventType": "PREM_ADDED", "currency": "AUD", "measures": map[string]string{"PREMIUM": "1000.00", "TAX": "100.00"}}

	r := do(t, "POST", "/configs/"+id+"/versions/1/evaluate", tenantA, viewer, e1)
	mustStatus(t, r, 200)
	var body struct {
		Status  string `json:"status"`
		Derived []struct {
			Code          string `json:"code"`
			Value         string `json:"value"`
			FormulaDigest string `json:"formulaDigest"`
			Config        struct {
				ConfigID      string `json:"configId"`
				ConfigVersion int    `json:"configVersion"`
				ContentDigest string `json:"contentDigest"`
			} `json:"config"`
			Trace struct {
				Reads []struct{ Name, Value string } `json:"reads"`
			} `json:"trace"`
		} `json:"derived"`
	}
	if err := json.Unmarshal([]byte(r.Raw), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "draft" || len(body.Derived) != 2 {
		t.Fatalf("a draft can be simulated: %s", r.Raw)
	}
	g := body.Derived[0]
	if g.Code != "GROSS" || g.Value != "1100 AUD" || g.Config.ConfigID != id || g.Config.ConfigVersion != 1 || !strings.HasPrefix(g.Config.ContentDigest, "sha256:") || !strings.HasPrefix(g.FormulaDigest, "sha256:") {
		t.Errorf("GROSS with lineage: %+v", g)
	}
	if len(g.Trace.Reads) != 2 || g.Trace.Reads[0].Name != "PREMIUM" || g.Trace.Reads[1].Value != "100 AUD" {
		t.Errorf("reads: %+v", g.Trace.Reads)
	}

	// A new version with a different formula is traceable to that version.
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"effectiveFrom": "2026-02-01", "content": formulaConfig("ROUND((PREMIUM + TAX) * 1.02, 2)"), "reason": "uplift"}), 201)
	if r2 := do(t, "POST", "/configs/"+id+"/versions/2/evaluate", tenantA, viewer, e1); !strings.Contains(r2.Raw, `"value":"1122 AUD"`) || !strings.Contains(r2.Raw, `"configVersion":2`) {
		t.Errorf("version 2: %s", r2.Raw)
	}

	// Explicit runtime errors, tenant isolation and membership.
	zero := map[string]any{"eventType": "PREM_ADDED", "currency": "AUD", "measures": map[string]string{"PREMIUM": "0.00", "TAX": "1.00"}}
	if z := do(t, "POST", "/configs/"+id+"/versions/1/evaluate", tenantA, viewer, zero); z.Status != 422 || z.code() != "DIVIDE_BY_ZERO" {
		t.Errorf("zero premium: %s", z.Raw)
	}
	if f := do(t, "POST", "/configs/"+id+"/versions/1/evaluate", tenantA, viewer, map[string]any{"eventType": "PREM_ADDED", "currency": "AUD", "measures": map[string]string{"PREMIUM": "1e3"}}); f.Status != 422 {
		t.Errorf("float-looking money: %s", f.Raw)
	}
	if x := do(t, "POST", "/configs/"+id+"/versions/1/evaluate", tenantB, viewer, e1); x.Status != 404 {
		t.Errorf("tenant B evaluated tenant A's version: %s", x.Raw)
	}
	if o := do(t, "POST", "/configs/"+id+"/versions/1/evaluate", tenantB, outsider, e1); o.code() != "NO_TENANT_ACCESS" {
		t.Errorf("outsider: %s", o.Raw)
	}
	// Nothing was written: the audit trail shows only the two version creations.
	a := do(t, "GET", "/configs/"+id+"/audit", tenantA, viewer, nil)
	if strings.Count(a.Raw, `"action":`) != 3 { // create_artifact, create_version v1, create_version v2
		t.Errorf("the simulator must not write: %s", a.Raw)
	}
}
