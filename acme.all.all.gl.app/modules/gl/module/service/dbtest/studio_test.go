package dbtest

import (
	"encoding/json"
	"strings"
	"testing"
)

func decode[T any](t *testing.T, r result, want int) T {
	t.Helper()
	mustStatus(t, r, want)
	var v T
	if err := json.Unmarshal([]byte(r.Raw), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

type stage struct{ Stage, Status, Detail string }

func stagesOf(t *testing.T, r result) map[string]stage {
	t.Helper()
	p := decode[struct {
		Stages []stage `json:"stages"`
	}](t, r, 200)
	out := map[string]stage{}
	for _, s := range p.Stages {
		out[s.Stage] = s
	}
	return out
}

// The configuration studio over the store: an approved version explained event by event,
// its scenarios run, a rule edited in a new draft (checked before saving, audited with what
// changed), and the change's impact and pipeline state — all read from stored versions.
func TestStudioExplainsEditsAndTracesAStoredVersion(t *testing.T) {
	needDB(t)
	id := uniq("STUDIO")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": fixtureConfig(t), "reason": "GL-TEST-001"}), 201)
	approve(t, tenantA, id, "1")
	base := "/configs/" + id + "/versions/"

	ev := decode[struct {
		Events []struct {
			Code                            string
			Rules, Lines, Scenarios, Errors int
		} `json:"events"`
	}](t, do(t, "GET", base+"1/events", tenantA, viewer, nil), 200)
	if len(ev.Events) != 3 || ev.Events[0].Code != "PREM_ADDED" || ev.Events[0].Rules != 1 || ev.Events[0].Lines != 3 || ev.Events[0].Scenarios != 2 {
		t.Fatalf("events: %+v", ev.Events)
	}
	view := decode[struct {
		View struct {
			Rules []struct {
				Rule struct{ Code string } `json:"rule"`
			} `json:"rules"`
			Graph struct {
				Nodes []struct{ ID string } `json:"nodes"`
			} `json:"graph"`
			Manifest struct {
				Fields []struct{ FieldCode string } `json:"fields"`
			} `json:"manifest"`
		} `json:"view"`
	}](t, do(t, "GET", base+"1/events/PREM_ADDED", tenantA, viewer, nil), 200)
	if len(view.View.Rules) != 1 || view.View.Rules[0].Rule.Code != "PR_PREM_ADDED" || len(view.View.Graph.Nodes) < 10 || len(view.View.Manifest.Fields) == 0 {
		t.Fatalf("PREM_ADDED view: %+v", view.View)
	}
	if r := do(t, "GET", base+"1/events/NOT_AN_EVENT", tenantA, viewer, nil); r.Status != 404 {
		t.Errorf("unknown event: %s", r.Raw)
	}
	if r := do(t, "GET", base+"1/events", tenantB, viewer, nil); r.Status != 404 {
		t.Errorf("another tenant reads the events: %s", r.Raw)
	}

	run := decode[struct {
		Summary map[string]int `json:"summary"`
		Results []struct {
			Code string
			Plan struct {
				Lines []any `json:"lines"`
			} `json:"plan"`
		} `json:"results"`
	}](t, do(t, "POST", base+"1/scenarios/run", tenantA, viewer, nil), 200)
	if run.Summary["PASSED"] != 5 || len(run.Results) != 5 {
		t.Fatalf("scenarios: %+v", run.Summary)
	}
	one := decode[struct {
		Results []struct {
			Plan struct {
				Lines []any `json:"lines"`
			} `json:"plan"`
		} `json:"results"`
	}](t, do(t, "POST", base+"1/scenarios/run", tenantA, viewer, map[string]any{"codes": []string{"E3"}}), 200)
	if len(one.Results) != 1 || len(one.Results[0].Plan.Lines) != 5 {
		t.Errorf("E3 alone: %+v", one)
	}
	if r := do(t, "POST", base+"1/scenarios/run", tenantA, viewer, map[string]any{"codes": []string{"NOPE"}}); r.Status != 404 {
		t.Errorf("unknown scenario: %s", r.Raw)
	}
	s1 := stagesOf(t, do(t, "GET", base+"1/pipeline", tenantA, viewer, nil))
	for st, want := range map[string]string{"configured": "DONE", "validated": "DONE", "approved": "DONE", "fields": "DONE", "mapped": "NOT_IMPLEMENTED",
		"collected": "NOT_IMPLEMENTED", "simulated": "DONE", "posted": "NOT_IMPLEMENTED", "subledger": "NOT_IMPLEMENTED", "reconciled": "NOT_IMPLEMENTED", "reported": "NOT_IMPLEMENTED"} {
		if s1[st].Status != want {
			t.Errorf("v1 stage %s: %+v, want %s", st, s1[st], want)
		}
	}
	if s1["simulated"].Detail != "5 of 5 scenarios hold" {
		t.Errorf("simulated: %+v", s1["simulated"])
	}

	// A new draft based on version 1; the BA edits PR_PREM_ADDED (a 10.00 fee waiver).
	v2 := do(t, "POST", base[:len(base)-1], tenantA, ba, map[string]any{"basedOnVersion": 1, "effectiveFrom": "2026-02-01", "reason": "fee waiver from February"})
	mustStatus(t, v2, 201)
	edited := fixtureConfig(t)
	for _, r := range edited["postingRules"].([]any) {
		rule := r.(map[string]any)
		if rule["code"] != "PR_PREM_ADDED" {
			continue
		}
		for _, l := range rule["lines"].([]any) {
			switch line := l.(map[string]any); line["code"] {
			case "AR0_DR":
				line["amount"] = "GROSS - 10"
			case "PW0_CR":
				line["amount"] = "PREMIUM - 10"
			}
		}
	}
	chk := decode[struct {
		OK      bool `json:"ok"`
		Changes []struct {
			Section, Key, Change string
			Fields               []string
		} `json:"changes"`
	}](t, do(t, "POST", base+"2/check", tenantA, ba, map[string]any{"content": edited}), 200)
	if !chk.OK || len(chk.Changes) != 1 || chk.Changes[0].Key != "PR_PREM_ADDED" || strings.Join(chk.Changes[0].Fields, ",") != "lines/AR0_DR,lines/PW0_CR" {
		t.Fatalf("check: %+v", chk)
	}
	if r := do(t, "POST", base+"2/check", tenantA, viewer, map[string]any{"content": edited}); r.Status != 403 {
		t.Errorf("a viewer checks an edit: %s", r.Raw)
	}
	stored := do(t, "GET", base+"2", tenantA, viewer, nil)
	if r := do(t, "GET", base+"2", tenantA, viewer, nil); r.str("contentDigest") != v2.str("contentDigest") {
		t.Fatalf("check saved something: %s", r.Raw)
	}
	mustStatus(t, do(t, "PUT", base+"2", tenantA, ba, map[string]any{"baseDigest": stored.str("contentDigest"), "content": edited, "reason": "waive the 10.00 policy fee"}), 200)

	aud := decode[struct {
		Entries []struct {
			Action string          `json:"action"`
			Detail json.RawMessage `json:"detail"`
		} `json:"entries"`
	}](t, do(t, "GET", "/configs/"+id+"/audit", tenantA, viewer, nil), 200)
	var saved string
	for _, e := range aud.Entries {
		if e.Action == "update_draft" {
			saved = string(e.Detail)
		}
	}
	if !strings.Contains(saved, "changed postingRules PR_PREM_ADDED (lines/AR0_DR, lines/PW0_CR)") {
		t.Errorf("the audit does not say what changed: %s", saved)
	}

	im := decode[struct {
		From struct {
			ConfigVersion int `json:"configVersion"`
		} `json:"from"`
		Events []struct {
			EventType string
			Because   []string
		} `json:"events"`
		Scenarios []struct {
			Code        string
			Changed     bool
			Differences []string
		} `json:"scenarios"`
	}](t, do(t, "GET", base+"2/impact", tenantA, viewer, nil), 200)
	if im.From.ConfigVersion != 1 || len(im.Events) != 1 || im.Events[0].EventType != "PREM_ADDED" || strings.Join(im.Events[0].Because, ",") != "postingRules:PR_PREM_ADDED" {
		t.Fatalf("impact: %+v", im)
	}
	changed := map[string]bool{}
	for _, s := range im.Scenarios {
		changed[s.Code] = s.Changed
	}
	if !changed["E1"] || !changed["E1_HOME"] || changed["E2"] || changed["E3"] {
		t.Errorf("scenario impact: %v", changed)
	}
	s2 := stagesOf(t, do(t, "GET", base+"2/pipeline", tenantA, viewer, nil))
	if s2["validated"].Status != "PENDING" || s2["approved"].Status != "PENDING" || s2["simulated"].Status != "FAILED" || s2["simulated"].Detail != "3 of 5 scenarios hold" {
		t.Errorf("v2 pipeline: %+v", s2)
	}
}
