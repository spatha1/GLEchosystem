package dbtest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gl/ai"
	"gl/logic"
)

// stubDrafter stands in for the AI provider: it returns prepared answers in order.
type stubDrafter struct{ answers []string }

func (s *stubDrafter) Status() ai.Status {
	return ai.Status{Available: true, Provider: "openai", Model: "test-double"}
}

func (s *stubDrafter) JSON(context.Context, []ai.Message) (string, ai.Usage, error) {
	if len(s.answers) == 0 {
		return "", ai.Usage{}, ai.ErrProvider("no answer left")
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	return a, ai.Usage{PromptTokens: 10, CompletionTokens: 5}, nil
}

const renewalEvent = `{"events":[{"code":"RENEWAL","template":"RENEWAL","name":"Policy renewed","purpose":"A policy is renewed for a new term.",
 "sourceKind":"POLICY","economicKey":["POLICY_NO","TERM_NO"],"otherKeys":["INTERMEDIARY_ID"],"dimensions":["COMPANY","LOB"],
 "evidence":["the policy system sends a renewal event identified by the policy number and the term number"],"confidence":"high"}],"questions":[]}`

type stepReply struct {
	ContentDigest string `json:"contentDigest"`
	Proposal      struct {
		Step     string    `json:"step"`
		Model    string    `json:"model"`
		Items    []ai.Item `json:"items"`
		Problems []string  `json:"problems"`
	} `json:"proposal"`
}

func TestAIStepsWorkOnADraftAndRecordTheirOrigin(t *testing.T) {
	needDB(t)
	logic.AIDrafter = nil
	defer func() { logic.AIDrafter = nil }()

	st := decode[ai.Status](t, do(t, "GET", "/ai/status", tenantA, ba, nil), 200)
	if st.Available || !strings.Contains(st.Reason, "not set up") {
		t.Fatalf("without settings AI is not set up: %+v", st)
	}

	id := uniq("AI")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": fixtureConfig(t), "reason": "AI steps"}), 201)
	base := "/configs/" + id + "/versions/1/"

	// Reading the BRD sends nothing to the AI.
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "acme.all.all.gl.spec", "_harness", "fixtures", "brd", "motor-renewals-brd.md"))
	if err != nil {
		t.Fatal(err)
	}
	upload := map[string]string{"name": "motor-renewals-brd.md", "data": base64.StdEncoding.EncodeToString(raw)}
	brd := decode[struct {
		Name  string
		Chars int
		Text  string
	}](t, do(t, "POST", base+"ai/brd", tenantA, ba, upload), 200)
	if !strings.Contains(brd.Text, "3.1 Renewal") || brd.Chars < 1000 {
		t.Fatalf("BRD text: %d chars", brd.Chars)
	}
	if r := do(t, "POST", base+"ai/brd", tenantA, viewer, upload); r.Status != 403 {
		t.Fatalf("a viewer may not run AI steps: %s", r.Raw)
	}
	if r := do(t, "POST", base+"ai/brd", tenantA, ba, map[string]string{"name": "brd.pdf", "data": base64.StdEncoding.EncodeToString([]byte("%PDF-1.7"))}); r.code() != "INVALID_ARGUMENT" {
		t.Fatalf("PDF is refused with a reason: %s", r.Raw)
	}

	step := map[string]any{"brd": map[string]string{"name": brd.Name, "text": brd.Text}}
	if r := do(t, "POST", base+"ai/steps/events", tenantA, ba, step); r.Status != 503 || r.code() != "AI_UNAVAILABLE" {
		t.Fatalf("without AI the step says so: %s", r.Raw)
	}
	if r := do(t, "POST", base+"ai/steps/accounts", tenantA, ba, step); r.code() != "INVALID_ARGUMENT" {
		t.Fatalf("there is no accounts step: %s", r.Raw)
	}

	logic.AIDrafter = func() ai.Drafter { return &stubDrafter{answers: []string{renewalEvent}} }
	rep := decode[stepReply](t, do(t, "POST", base+"ai/steps/events", tenantA, ba, step), 200)
	if rep.Proposal.Step != "events" || rep.Proposal.Model != "test-double" || len(rep.Proposal.Problems) != 0 {
		t.Fatalf("proposal: %+v", rep.Proposal)
	}
	found := false
	for _, it := range rep.Proposal.Items {
		found = found || it.ID == "events/RENEWAL"
	}
	if !found {
		t.Fatalf("no RENEWAL event proposed: %+v", rep.Proposal.Items)
	}

	// Nothing was saved: the person approves and the studio saves, with the AI origin recorded.
	cur := decode[struct {
		ContentDigest string          `json:"contentDigest"`
		Content       json.RawMessage `json:"content"`
	}](t, do(t, "GET", "/configs/"+id+"/versions/1", tenantA, ba, nil), 200)
	if cur.ContentDigest != rep.ContentDigest || strings.Contains(string(cur.Content), `"RENEWAL"`) {
		t.Fatal("an AI step must not change the draft")
	}
	content, err := ai.Apply(cur.Content, rep.Proposal.Items)
	if err != nil {
		t.Fatal(err)
	}
	origin := map[string]string{"kind": "AI_STEP", "step": "events", "provider": "openai", "model": "test-double", "brd": brd.Name}
	save := map[string]any{"baseDigest": cur.ContentDigest, "content": json.RawMessage(content), "reason": "Step 1 events approved", "origin": origin}
	mustStatus(t, do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, save), 200)
	audit := decode[struct {
		Entries []struct {
			Action string          `json:"action"`
			Detail json.RawMessage `json:"detail"`
		} `json:"entries"`
	}](t, do(t, "GET", "/configs/"+id+"/audit", tenantA, ba, nil), 200)
	recorded := false
	for _, e := range audit.Entries {
		if e.Action == "update_draft" && strings.Contains(string(e.Detail), `"origin"`) && strings.Contains(string(e.Detail), `"AI_STEP"`) && strings.Contains(string(e.Detail), "motor-renewals-brd.md") {
			recorded = true
		}
	}
	if !recorded {
		t.Fatalf("the audit trail must record the AI origin: %+v", audit.Entries)
	}
	bad := map[string]any{"baseDigest": "x", "content": json.RawMessage(content), "reason": "r", "origin": map[string]string{"kind": "AI_APPROVED", "step": "events"}}
	if r := do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, bad); r.code() != "INVALID_ARGUMENT" {
		t.Fatalf("only AI_STEP is a valid origin: %s", r.Raw)
	}

	// AI steps never touch an approved version.
	done := uniq("AIDONE")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": done, "effectiveFrom": "2026-01-01", "content": fixtureConfig(t), "reason": "approved"}), 201)
	approve(t, tenantA, done, "1")
	if r := do(t, "POST", "/configs/"+done+"/versions/1/ai/steps/events", tenantA, ba, step); r.Status != 409 || r.code() != "CONFIG_IMMUTABLE" {
		t.Fatalf("an approved version is not open to AI steps: %s", r.Raw)
	}
}
