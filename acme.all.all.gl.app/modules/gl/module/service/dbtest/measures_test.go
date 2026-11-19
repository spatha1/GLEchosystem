package dbtest

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type measureView struct {
	Code       string   `json:"code"`
	Definition string   `json:"definition"`
	Sign       string   `json:"sign"`
	Origin     string   `json:"origin"`
	Overridden []string `json:"overridden"`
}

func measuresOf(t *testing.T, r result) map[string]measureView {
	t.Helper()
	var body struct {
		Measures []measureView `json:"measures"`
	}
	if err := json.Unmarshal([]byte(r.Raw), &body); err != nil {
		t.Fatalf("%v: %s", err, r.Raw)
	}
	out := map[string]measureView{}
	for _, m := range body.Measures {
		out[m.Code] = m
	}
	return out
}

// US-006 through the store: client A's override is visible only in client A's version; client
// B and the global catalog keep the default.
func TestMeasureOverridesAreIsolatedPerClient(t *testing.T) {
	needDB(t)
	global := measuresOf(t, do(t, "GET", "/catalog/measures", tenantA, ba, nil))
	if global["PREMIUM"].Origin != "OOTB" || len(global) < 10 {
		t.Fatalf("global catalog: %+v", global)
	}

	override := catalog(map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"})
	override["baseMeasures"] = []any{map[string]any{"code": "PREMIUM", "definition": "Client A: gross premium including policy fee", "sign": "POSITIVE"}}
	idA := uniq("MEAS")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": idA, "effectiveFrom": "2026-01-01", "content": override, "reason": "client A measures"}), 201)
	mustStatus(t, do(t, "POST", "/configs/"+idA+"/versions/1/validate", tenantA, ba, nil), 200)

	idB := uniq("MEAS")
	plain := catalog(map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"})
	mustStatus(t, do(t, "POST", "/configs", tenantB, ba, map[string]any{"configId": idB, "effectiveFrom": "2026-01-01", "content": plain, "reason": "client B measures"}), 201)

	a := measuresOf(t, do(t, "GET", "/configs/"+idA+"/versions/1/measures", tenantA, viewer, nil))
	b := measuresOf(t, do(t, "GET", "/configs/"+idB+"/versions/1/measures", tenantB, viewer, nil))
	after := measuresOf(t, do(t, "GET", "/catalog/measures", tenantB, ba, nil))

	if a["PREMIUM"].Origin != "OVERRIDDEN" || a["PREMIUM"].Sign != "POSITIVE" || a["PREMIUM"].Definition != "Client A: gross premium including policy fee" {
		t.Errorf("client A premium: %+v", a["PREMIUM"])
	}
	if !reflect.DeepEqual(b["PREMIUM"], global["PREMIUM"]) || !reflect.DeepEqual(after["PREMIUM"], global["PREMIUM"]) {
		t.Errorf("the override leaked: client B %+v, global after %+v, global before %+v", b["PREMIUM"], after["PREMIUM"], global["PREMIUM"])
	}
	if r := do(t, "GET", "/configs/"+idA+"/versions/1/measures", tenantB, viewer, nil); r.Status != 404 {
		t.Errorf("client B read client A's resolved measures: %s", r.Raw)
	}
	// An unknown measure blocks validation (and so approval and mapping compilation).
	bad := catalog(map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"})
	bad["events"].([]any)[1].(map[string]any)["measures"] = []any{"PAY_AMT", "SURCHARGE"}
	idC := uniq("MEAS")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": idC, "effectiveFrom": "2026-01-01", "content": bad, "reason": "unknown measure"}), 201)
	if v := do(t, "POST", "/configs/"+idC+"/versions/1/validate", tenantA, ba, nil); v.Status != 422 || !strings.Contains(v.Raw, `"code":"UNKNOWN_MEASURE"`) {
		t.Errorf("an unknown measure did not block validation: %s", v.Raw)
	}
}
