package dbtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureConfig is the canonical GL-TEST-001 configuration (spec fixture).
func fixtureConfig(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "acme.all.all.gl.spec", "_harness", "fixtures", "GL-TEST-001.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

type manifestReply struct {
	Version          int  `json:"version"`
	Complete         bool `json:"complete"`
	BlockingFindings int  `json:"blockingFindings"`
	Manifest         struct {
		SchemaVersion  string `json:"schemaVersion"`
		TenantID       string `json:"tenantId"`
		ConfigVersion  int    `json:"configVersion"`
		ConfigDigest   string `json:"configDigest"`
		ManifestDigest string `json:"manifestDigest"`
		Events         []struct {
			EventType string `json:"eventType"`
			Fields    []struct {
				FieldCode, Kind, ListCode string
			} `json:"fields"`
		} `json:"events"`
	} `json:"manifest"`
	Explanation struct {
		Events []struct {
			EventType string `json:"eventType"`
			Unused    []struct {
				FieldCode string `json:"fieldCode"`
			} `json:"unused"`
		} `json:"events"`
	} `json:"explanation"`
}

func manifestOf(t *testing.T, r result) manifestReply {
	t.Helper()
	mustStatus(t, r, 200)
	var m manifestReply
	if err := json.Unmarshal([]byte(r.Raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func kindOf(m manifestReply, event, list, code string) string {
	for _, e := range m.Manifest.Events {
		for _, f := range e.Fields {
			if e.EventType == event && f.ListCode == list && f.FieldCode == code {
				return f.Kind
			}
		}
	}
	return ""
}

// US-014 through the store: the manifest is compiled from the stored version, the version in
// force on a date decides what collection asks for, and a newly approved rule change
// regenerates it — with the field-level diff saying which mappings it affects.
func TestManifestFollowsTheApprovedConfiguration(t *testing.T) {
	needDB(t)
	id := uniq("MANI")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": fixtureConfig(t), "reason": "GL-TEST-001"}), 201)
	approve(t, tenantA, id, "1")

	v1 := do(t, "GET", "/configs/"+id+"/versions/1", tenantA, viewer, nil)
	m1 := manifestOf(t, do(t, "GET", "/configs/"+id+"/versions/1/manifest", tenantA, viewer, nil))
	if m1.Manifest.SchemaVersion != "gl.manifest/v1" || m1.Manifest.TenantID != tenantA || m1.Manifest.ConfigVersion != 1 ||
		m1.Manifest.ConfigDigest != v1.str("contentDigest") || !m1.Complete || len(m1.Manifest.Events) != 3 {
		t.Fatalf("version 1 manifest: %+v", m1)
	}
	if kindOf(m1, "PAYMENT_RECEIVED", "", "BANK_ACCOUNT") != "SOURCE" || kindOf(m1, "PAYMENT_RECEIVED", "", "BANKING_COMPANY") != "DERIVED" ||
		kindOf(m1, "PAYMENT_RECEIVED", "ALLOCATION_LIST", "ALLOC_AMT") != "SOURCE" || kindOf(m1, "PREM_ADDED", "", "PRODUCT") != "" {
		t.Fatalf("version 1 fields: %+v", m1.Manifest.Events)
	}

	// Version 2 (from February) filters premium events by product: PRODUCT becomes required.
	c2 := fixtureConfig(t)
	for _, r := range c2["postingRules"].([]any) {
		if rule := r.(map[string]any); rule["code"] == "PR_PREM_ADDED" {
			rule["filter"] = "PRODUCT != 'HOME_BUILDING'"
		}
	}
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"basedOnVersion": 1, "effectiveFrom": "2026-02-01", "content": c2, "reason": "no home building premium"}), 201)
	approve(t, tenantA, id, "2")

	jan := manifestOf(t, do(t, "GET", "/configs/"+id+"/effective/manifest?date=2026-01-15", tenantA, viewer, nil))
	feb := manifestOf(t, do(t, "GET", "/configs/"+id+"/effective/manifest?date=2026-02-15", tenantA, viewer, nil))
	if jan.Version != 1 || feb.Version != 2 || jan.Manifest.ManifestDigest != m1.Manifest.ManifestDigest || feb.Manifest.ManifestDigest == jan.Manifest.ManifestDigest {
		t.Fatalf("effective manifests: January v%d %s, February v%d %s", jan.Version, jan.Manifest.ManifestDigest, feb.Version, feb.Manifest.ManifestDigest)
	}
	if kindOf(feb, "PREM_ADDED", "", "PRODUCT") != "SOURCE" {
		t.Fatalf("PRODUCT is not requested from February: %+v", feb.Manifest.Events)
	}
	d := do(t, "GET", "/configs/"+id+"/versions/2/manifest/diff?against=1", tenantA, viewer, nil)
	mustStatus(t, d, 200)
	var diff struct {
		Changed bool `json:"changed"`
		Events  []struct {
			EventType string `json:"eventType"`
			Fields    []struct {
				FieldCode, Change string
				Remap             bool
			} `json:"fields"`
		} `json:"events"`
	}
	_ = json.Unmarshal([]byte(d.Raw), &diff)
	if !diff.Changed || len(diff.Events) != 1 || diff.Events[0].EventType != "PREM_ADDED" || len(diff.Events[0].Fields) != 1 ||
		diff.Events[0].Fields[0].FieldCode != "PRODUCT" || diff.Events[0].Fields[0].Change != "ADDED" || !diff.Events[0].Fields[0].Remap {
		t.Fatalf("diff: %s", d.Raw)
	}

	// One event at a time; unknown events, other tenants and missing dates are refused.
	if one := manifestOf(t, do(t, "GET", "/configs/"+id+"/versions/1/manifest?eventType=PAYMENT_RECEIVED", tenantA, viewer, nil)); len(one.Manifest.Events) != 1 || len(one.Explanation.Events) != 1 {
		t.Errorf("filtered manifest: %+v", one)
	}
	if r := do(t, "GET", "/configs/"+id+"/versions/1/manifest?eventType=NOT_AN_EVENT", tenantA, viewer, nil); r.Status != 404 {
		t.Errorf("unknown event: %s", r.Raw)
	}
	if r := do(t, "GET", "/configs/"+id+"/versions/1/manifest", tenantB, viewer, nil); r.Status != 404 {
		t.Errorf("another tenant reads the manifest: %s", r.Raw)
	}
	if r := do(t, "GET", "/configs/"+id+"/effective/manifest?date=2025-12-31", tenantA, viewer, nil); r.Status != 404 || r.code() != "NO_EFFECTIVE_VERSION" {
		t.Errorf("before any version: %s", r.Raw)
	}
	if r := do(t, "GET", "/configs/"+id+"/versions/2/manifest/diff", tenantA, viewer, nil); r.Status != 400 {
		t.Errorf("diff without against: %s", r.Raw)
	}

	// A draft with a blocking finding still shows what it can, marked incomplete.
	broken := fixtureConfig(t)
	broken["postingRules"].([]any)[0].(map[string]any)["filter"] = "NOT_A_FIELD > 0"
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"effectiveFrom": "2026-03-01", "content": broken, "reason": "work in progress"}), 201)
	if m3 := manifestOf(t, do(t, "GET", "/configs/"+id+"/versions/3/manifest", tenantA, viewer, nil)); m3.Complete || m3.BlockingFindings == 0 || len(m3.Manifest.Events) != 3 {
		t.Errorf("draft with a broken filter: complete=%v blocking=%d", m3.Complete, m3.BlockingFindings)
	}
}
