package dbtest

import (
	"strings"
	"testing"
)

// ALTB-04: a stored, approved version's scenarios previewed as one lifecycle: the events run
// in order against a temporary ledger, the balances carry from step to step, and nothing is
// posted. Any reader may run it; another tenant cannot; a bad step is refused.
func TestLifecyclePreviewOfAStoredVersion(t *testing.T) {
	needDB(t)
	id := uniq("LIFE")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": fixtureConfig(t), "reason": "GL-TEST-001"}), 201)
	approve(t, tenantA, id, "1")
	path := "/configs/" + id + "/versions/1/lifecycle"
	steps := map[string]any{"steps": []map[string]any{{"scenario": "E1"}, {"scenario": "E2"}, {"scenario": "E3"}}}

	type balance struct{ Account, Value, Balance string }
	got := decode[struct {
		Lifecycle struct {
			Complete       bool
			Stopped        string
			Lines, Entries int
			Posted         bool
			Steps          []struct {
				Code     string
				Balances []balance
				Check    *struct{ Status string } // ALTB-08: each scenario step checked against its expectation
			}
		} `json:"lifecycle"`
	}](t, do(t, "POST", path, tenantA, viewer, steps), 200)
	l := got.Lifecycle
	if !l.Complete || l.Stopped != "" || l.Lines != 12 || l.Entries != 4 || l.Posted || len(l.Steps) != 3 || l.Steps[2].Code != "E3" {
		t.Fatalf("lifecycle: %+v", l)
	}
	for _, st := range l.Steps {
		if st.Check == nil || st.Check.Status != "PASSED" {
			t.Errorf("%s: each scenario step is checked against its expectation: %+v", st.Code, st.Check)
		}
	}
	for _, b := range l.Steps[2].Balances {
		if b.Account == "9948600" && b.Value == "INV2" && b.Balance != "0.50" {
			t.Errorf("INV2 after E3: %+v", b)
		}
	}

	stop := decode[struct {
		Lifecycle struct {
			Complete bool
			Stopped  string
		} `json:"lifecycle"`
	}](t, do(t, "POST", path, tenantA, viewer, map[string]any{"steps": []map[string]any{{"scenario": "E3_SAME_COMPANY"}, {"scenario": "E1"}}}), 200)
	if stop.Lifecycle.Complete || !strings.Contains(stop.Lifecycle.Stopped, "ENTITY_NOT_ALLOWED") {
		t.Errorf("stops at a refused event: %+v", stop.Lifecycle)
	}

	if r := do(t, "POST", path, tenantB, viewer, steps); r.Status != 404 {
		t.Errorf("another tenant previews the lifecycle: %d %s", r.Status, r.Raw)
	}
	for name, body := range map[string]any{
		"no steps":    map[string]any{"steps": []any{}},
		"both":        map[string]any{"steps": []map[string]any{{"scenario": "E1", "input": map[string]any{"eventType": "INVOICED", "currency": "AUD"}}}},
		"neither":     map[string]any{"steps": []map[string]any{{"code": "X"}}},
		"unknown key": map[string]any{"steps": []map[string]any{{"scenario": "E1", "post": true}}},
	} {
		if r := do(t, "POST", path, tenantA, viewer, body); r.Status != 400 {
			t.Errorf("%s: %d %s", name, r.Status, r.Raw)
		}
	}
	if r := do(t, "POST", path, tenantA, viewer, map[string]any{"steps": []map[string]any{{"scenario": "NOPE"}}}); r.Status != 404 {
		t.Errorf("unknown scenario: %d %s", r.Status, r.Raw)
	}
}
