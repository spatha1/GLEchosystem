package dbtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gl/ai"
	"gl/logic"
)

// Onboarding is per client: an administrator onboards their client (and may initialise its
// empty draft); other clients see nothing of it; every change is audited and the audit is
// append-only.
func TestClientOnboardingIsPerClientAndAudited(t *testing.T) {
	needDB(t)
	cfg := uniq("ONB")
	body := map[string]any{"name": "Client A Insurance", "configId": cfg, "reason": "onboard client A",
		"profile":    map[string]any{"description": "GL for AU and NZ", "sourceSystems": []any{"POLICY", "BILLING"}},
		"initialize": map[string]any{"title": "Client A GL", "effectiveFrom": "2026-07-01"}}
	if r := do(t, "PUT", "/client", tenantA, ba, body); r.Status != 403 {
		t.Fatalf("an analyst onboarded a client: %s", r.Raw)
	}
	r := do(t, "PUT", "/client", tenantA, admin, body)
	mustStatus(t, r, 200)
	if r.str("initialized") != cfg {
		t.Fatalf("the draft was not initialised: %s", r.Raw)
	}
	v := do(t, "GET", "/configs/"+cfg+"/versions/1", tenantA, ba, nil)
	if v.str("status") != "draft" || v.str("createdBy") != admin {
		t.Fatalf("version 1: %s", v.Raw)
	}
	g := do(t, "GET", "/client", tenantA, ba, nil)
	mustStatus(t, g, 200)
	if g.str("name") != "Client A Insurance" || g.str("status") != "ONBOARDING" || g.str("configId") != cfg {
		t.Fatalf("profile: %s", g.Raw)
	}
	if o := do(t, "GET", "/client", tenantB, admin, nil); o.Status != 404 || o.code() != "NOT_ONBOARDED" {
		t.Fatalf("client B sees client A's profile: %s", o.Raw)
	}
	body["status"] = "ACTIVE"
	body["reason"] = "client A is live"
	mustStatus(t, do(t, "PUT", "/client", tenantA, admin, body), 200)
	a := do(t, "GET", "/client/audit", tenantA, admin, nil)
	mustStatus(t, a, 200)
	if !strings.Contains(a.Raw, `"action":"onboard"`) || !strings.Contains(a.Raw, `"action":"update"`) || !strings.Contains(a.Raw, "client A is live") {
		t.Fatalf("audit: %s", a.Raw)
	}
	if o := do(t, "GET", "/client/audit", tenantA, ba, nil); o.Status != 403 {
		t.Errorf("an analyst read the client audit: %s", o.Raw)
	}
	err := asApp(t, tenantA, func(tx *sql.Tx) error {
		_, err := tx.Exec(`delete from gl_client_audit`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "GL_AUDIT_APPEND_ONLY") {
		t.Errorf("the client audit could be deleted: %v", err)
	}
}

// A reviewer can send a validated version back to its editors, with a reason; nobody else can.
func TestReviewerReturnsAVersionToDraft(t *testing.T) {
	needDB(t)
	id := uniq("RET")
	create(t, tenantA, id, "2026-01-01")
	if r := do(t, "POST", "/configs/"+id+"/versions/1/return", tenantA, rev, reason("not yet")); r.Status != 409 {
		t.Fatalf("a draft was returned: %s", r.Raw)
	}
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil), 200)
	if r := do(t, "POST", "/configs/"+id+"/versions/1/return", tenantA, ba, reason("mine")); r.Status != 403 {
		t.Fatalf("an analyst returned a version: %s", r.Raw)
	}
	if r := do(t, "POST", "/configs/"+id+"/versions/1/return", tenantA, rev, nil); r.Status != 400 {
		t.Fatalf("returned without a reason: %s", r.Raw)
	}
	r := do(t, "POST", "/configs/"+id+"/versions/1/return", tenantA, rev, reason("map the remaining source classes first"))
	mustStatus(t, r, 200)
	if r.str("status") != "draft" {
		t.Fatalf("status: %s", r.Raw)
	}
	a := do(t, "GET", "/configs/"+id+"/audit", tenantA, ba, nil)
	if !strings.Contains(a.Raw, `"return_to_draft"`) || !strings.Contains(a.Raw, "map the remaining source classes first") {
		t.Fatalf("audit: %s", a.Raw)
	}
	// it can be edited, validated and approved again
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil), 200)
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, rev, reason("fine now")), 200)
}

func stage1Config() map[string]any {
	return map[string]any{
		"dimensions": []any{map[string]any{"code": "TRANSACTION_TYPE_CODE", "custom": true, "open": true}},
		"accountingClasses": []any{
			map[string]any{"code": "POLICY_RECEIVABLE", "name": "Policy receivable", "nature": "ASSET", "custom": true},
			map[string]any{"code": "PREMIUM_WRITTEN", "name": "Premium written", "nature": "INCOME", "custom": true},
			map[string]any{"code": "TAX_PAYABLE", "name": "Tax payable", "nature": "LIABILITY", "custom": true},
		},
		"accounts": []any{
			map[string]any{"code": "120010", "name": "Debtors control", "activeFrom": "2020-07-01"},
			map[string]any{"code": "400000", "name": "Premium written", "activeFrom": "2020-07-01"},
			map[string]any{"code": "230015", "name": "GST", "activeFrom": "2020-07-01"},
		},
		"sourceClasses": []any{
			map[string]any{"system": "PAS", "code": "AR0", "mappings": []any{map[string]any{"class": "POLICY_RECEIVABLE"}}},
			map[string]any{"system": "PAS", "code": "PW0", "mappings": []any{
				map[string]any{"class": "PREMIUM_WRITTEN", "when": map[string]any{"TRANSACTION_TYPE_CODE": "PREM"}},
				map[string]any{"class": "TAX_PAYABLE", "when": map[string]any{"TRANSACTION_TYPE_CODE": "TAX"}},
			}},
		},
		"accountAssignmentRules": []any{
			map[string]any{"code": "AA_1", "class": "POLICY_RECEIVABLE", "account": "120010"},
			map[string]any{"code": "AA_2", "class": "PREMIUM_WRITTEN", "account": "400000", "match": map[string]any{"TRANSACTION_TYPE_CODE": "PREM"}},
			map[string]any{"code": "AA_3", "class": "TAX_PAYABLE", "account": "230015", "match": map[string]any{"TRANSACTION_TYPE_CODE": "TAX"}},
		},
	}
}

// Stage 1 rule testing: the same source class gives different accounts under different
// conditions, with the rule and the reason.
func TestAssignTestResolvesSourceClassesToAccounts(t *testing.T) {
	needDB(t)
	id := uniq("ASG")
	c := create(t, tenantA, id, "2026-01-01")
	mustStatus(t, do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, map[string]any{"baseDigest": c.str("contentDigest"), "content": stage1Config(), "reason": "stage 1"}), 200)
	for _, tc := range []struct{ tt, class, account, rule string }{{"PREM", "PREMIUM_WRITTEN", "400000", "AA_2"}, {"TAX", "TAX_PAYABLE", "230015", "AA_3"}} {
		r := do(t, "POST", "/configs/"+id+"/versions/1/assign", tenantA, viewer, map[string]any{"sourceSystem": "PAS", "sourceClass": "PW0",
			"conditions": map[string]any{"TRANSACTION_TYPE_CODE": tc.tt}, "bookDate": "2026-07-15"})
		mustStatus(t, r, 200)
		var out struct {
			Class      string `json:"class"`
			MappedBy   string `json:"mappedBy"`
			Allowed    bool   `json:"allowed"`
			Assignment struct {
				Rule, Account string
				Considered    []json.RawMessage
			} `json:"assignment"`
		}
		_ = json.Unmarshal([]byte(r.Raw), &out)
		if out.Class != tc.class || out.Assignment.Account != tc.account || out.Assignment.Rule != tc.rule || !out.Allowed || len(out.Assignment.Considered) != 1 {
			t.Fatalf("PW0 %s: %s", tc.tt, r.Raw)
		}
		if !strings.Contains(out.MappedBy, "TRANSACTION_TYPE_CODE = "+tc.tt) {
			t.Errorf("mapped by: %s", out.MappedBy)
		}
	}
	r := do(t, "POST", "/configs/"+id+"/versions/1/assign", tenantA, viewer, map[string]any{"sourceSystem": "PAS", "sourceClass": "PW0",
		"conditions": map[string]any{"TRANSACTION_TYPE_CODE": "FEE"}, "bookDate": "2026-07-15"})
	if !strings.Contains(r.Raw, "UNMAPPED_SOURCE_CLASS") {
		t.Errorf("no mapping applies to FEE: %s", r.Raw)
	}
	r = do(t, "POST", "/configs/"+id+"/versions/1/assign", tenantA, viewer, map[string]any{"class": "POLICY_RECEIVABLE", "bookDate": "2026-07-15"})
	if !strings.Contains(r.Raw, `"account":"120010"`) {
		t.Errorf("AR0's class: %s", r.Raw)
	}
}

// captureDrafter records what would be sent to the provider.
type captureDrafter struct {
	answer string
	sent   []ai.Message
}

func (s *captureDrafter) Status() ai.Status {
	return ai.Status{Available: true, Provider: "openai", Model: "test-double"}
}

func (s *captureDrafter) JSON(_ context.Context, m []ai.Message) (string, ai.Usage, error) {
	s.sent = m
	return s.answer, ai.Usage{PromptTokens: 100, CompletionTokens: 20}, nil
}

// The assistant calls the provider only when external calls are allowed, within the client's
// daily budget, for the roles of the area, without e-mail addresses; decisions are recorded.
func TestAssistantIsGatedAliasedBudgetedAndAudited(t *testing.T) {
	needDB(t)
	d := &captureDrafter{answer: `{"answer":"person-2 approves; person-1 edits.","suggestions":[
		{"kind":"roles","title":"Make person-1 a reviewer","why":"they review","confidence":"high","evidence":["CONFIG_EDITOR"],"data":{"user":"person-1","roles":["CONFIG_APPROVER","SUPERUSER"]}},
		{"kind":"rule","title":"not allowed here","data":{}}],"questions":[]}`}
	logic.AIDrafter = func() ai.Drafter { return d }
	defer func() { logic.AIDrafter = nil; logic.AIExternal = func() bool { return true } }()
	ask := map[string]any{"area": "admin", "question": "Who can approve in this client? Should ba@example.com review?"}

	logic.AIExternal = func() bool { return false }
	r := do(t, "POST", "/ai/assist", tenantB, admin, ask)
	if r.Status != 503 || r.code() != "AI_EXTERNAL_OFF" || d.sent != nil {
		t.Fatalf("the provider was called while external calls are off: %s", r.Raw)
	}
	if s := do(t, "GET", "/ai/status", tenantB, admin, nil); !strings.Contains(s.Raw, `"available":false`) || !strings.Contains(s.Raw, "switched off") {
		t.Fatalf("status: %s", s.Raw)
	}

	logic.AIExternal = func() bool { return true }
	if r := do(t, "POST", "/ai/assist", tenantB, ba, ask); r.Status != 403 {
		t.Fatalf("an analyst used the admin assistant: %s", r.Raw)
	}
	os.Setenv("GL_AI_DAILY_REQUESTS", "1")
	defer os.Unsetenv("GL_AI_DAILY_REQUESTS")
	r = do(t, "POST", "/ai/assist", tenantB, admin, ask)
	mustStatus(t, r, 200)
	for _, m := range d.sent {
		if strings.Contains(m.Content, "@example.com") {
			t.Fatalf("an e-mail address was sent to the provider: %.300s", m.Content)
		}
	}
	var out struct {
		Result ai.AssistResult `json:"result"`
	}
	_ = json.Unmarshal([]byte(r.Raw), &out)
	res := out.Result
	if len(res.Suggestions) != 1 || res.Suggestions[0].Kind != "roles" {
		t.Fatalf("suggestions (a rule is not an admin suggestion): %+v", res.Suggestions)
	}
	s := res.Suggestions[0]
	if u, _ := s.Data["user"].(string); !strings.HasSuffix(u, "@example.com") {
		t.Errorf("the alias was not restored: %v", s.Data)
	}
	if roles, _ := s.Data["roles"].([]any); len(roles) != 1 || s.Confidence != "low" {
		t.Errorf("an unknown role must be dropped and flagged: %+v", s)
	}
	if !strings.Contains(res.Answer, "@example.com") || res.Aliased == 0 {
		t.Errorf("answer: %q aliased %d", res.Answer, res.Aliased)
	}
	if r := do(t, "POST", "/ai/assist", tenantB, admin, ask); r.Status != 429 || r.code() != "AI_BUDGET_EXCEEDED" {
		t.Fatalf("the daily budget was not enforced: %s", r.Raw)
	}
	// decisions
	dec := map[string]any{"area": "admin", "suggestionId": s.ID, "title": s.Title, "decision": "REJECTED", "detail": map[string]any{"why": "not now"}, "provider": "openai", "model": "test-double"}
	if r := do(t, "POST", "/ai/decisions", tenantB, ba, dec); r.Status != 403 {
		t.Fatalf("an analyst recorded an admin decision: %s", r.Raw)
	}
	mustStatus(t, do(t, "POST", "/ai/decisions", tenantB, admin, dec), 201)
	l := do(t, "GET", "/ai/decisions?area=admin", tenantB, viewer, nil)
	if !strings.Contains(l.Raw, `"decision":"REJECTED"`) || !strings.Contains(l.Raw, s.ID) {
		t.Fatalf("decisions: %s", l.Raw)
	}
	if o := do(t, "GET", "/ai/decisions", tenantA, admin, nil); strings.Contains(o.Raw, s.ID+`","title":"`+s.Title) {
		t.Errorf("client A sees client B's decisions: %s", o.Raw)
	}
}

// The permissions screen shows what the server enforces.
func TestPermissionsComeFromTheRouteTable(t *testing.T) {
	needDB(t)
	r := do(t, "GET", "/roles/permissions", tenantA, viewer, nil)
	mustStatus(t, r, 200)
	if !strings.Contains(r.Raw, `{"method":"POST","path":"/configs/{configId}/versions/{version}/approve","access":"roles","roles":["CONFIG_APPROVER"]}`) {
		t.Fatalf("permissions: %.400s", r.Raw)
	}
	c := do(t, "GET", "/catalog/classes", tenantA, viewer, nil)
	if !strings.Contains(c.Raw, `"POLICY_RECEIVABLE"`) || !strings.Contains(c.Raw, `"status":"PROPOSED"`) {
		t.Fatalf("library: %.300s", c.Raw)
	}
}
