package logic

// The AI assistant on every screen of administration, onboarding and Stage 1 (ai.Assist), and
// the controls around it:
//   - external calls happen only when the server is told they are allowed (GL_AI_EXTERNAL=on),
//     on top of a configured provider: until the owner confirms that the documents may be
//     shared, every screen says so and manual configuration works as before;
//   - a daily request budget per client (GL_AI_DAILY_REQUESTS, default 40), document size caps,
//     and the usage reported with every answer;
//   - the context is built here, from what the caller may see in the bound tenant only, and
//     e-mail addresses are replaced by aliases before anything leaves the server;
//   - every suggestion a person accepted, edited or rejected is recorded (append-only).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"gl/ai"
	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// AIExternal says whether calls to the external AI provider are allowed. main and the dev
// adapter may replace it; by default it reads GL_AI_EXTERNAL.
var AIExternal = func() bool { return strings.EqualFold(os.Getenv("GL_AI_EXTERNAL"), "on") }

func aiDailyLimit() int {
	if n, err := strconv.Atoi(os.Getenv("GL_AI_DAILY_REQUESTS")); err == nil && n >= 0 {
		return n
	}
	return 40
}

const externalOffReason = "External AI calls are switched off: they start once the owner confirms that the client's configuration documents may be shared with the AI provider. Everything can be done by hand meanwhile."

// assistantStatus is the provider's status with the external switch and the budget.
func assistantStatus(c *call) map[string]any {
	st := drafter().Status()
	out := map[string]any{"available": st.Available, "provider": st.Provider, "model": st.Model, "reason": st.Reason, "external": AIExternal()}
	if st.Available && !AIExternal() {
		out["available"] = false
		out["reason"] = externalOffReason
	}
	used, _ := aiUsedToday(c)
	out["budget"] = map[string]int{"dailyRequests": aiDailyLimit(), "usedToday": used}
	return out
}

// aiStatus — GET /ai/status: whether AI is available here, and why not.
func aiStatus(c *call) modulesdk.Response {
	return jsonResponse(200, assistantStatus(c))
}

func aiUsedToday(c *call) (int, error) {
	rows, err := c.host.Query(`select coalesce((select requests from ai_usage where tenant_id = $1 and day = current_date), 0)::text`, []string{c.id.Tenant})
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	n, _ := strconv.Atoi(rows[0].Cells[0])
	return n, nil
}

// aiGate refuses a call when AI is unavailable, external calls are off, or the budget is spent.
func aiGate(c *call) *apiError {
	st := drafter().Status()
	if !st.Available {
		return &apiError{Status: 503, Code: "AI_UNAVAILABLE", Message: st.Reason}
	}
	if !AIExternal() {
		return &apiError{Status: 503, Code: "AI_EXTERNAL_OFF", Message: externalOffReason}
	}
	used, err := aiUsedToday(c)
	if err != nil {
		return errInternal("storage error")
	}
	if limit := aiDailyLimit(); used >= limit {
		return &apiError{Status: 429, Code: "AI_BUDGET_EXCEEDED", Message: "today's AI budget for this client (" + strconv.Itoa(limit) + " requests) is used up; continue by hand or try tomorrow", Retryable: true}
	}
	return nil
}

func aiRecordUsage(c *call, u ai.Usage) {
	_, _ = c.host.Exec(`insert into ai_usage (tenant_id, day, requests, prompt_tokens, completion_tokens) values ($1, current_date, 1, $2::bigint, $3::bigint)
		on conflict (tenant_id, day) do update set requests = ai_usage.requests + 1, prompt_tokens = ai_usage.prompt_tokens + excluded.prompt_tokens,
			completion_tokens = ai_usage.completion_tokens + excluded.completion_tokens`,
		[]string{c.id.Tenant, strconv.Itoa(u.PromptTokens), strconv.Itoa(u.CompletionTokens)})
}

// areaRoles: who may ask the assistant where.
func areaRoles(area string) []string {
	switch area {
	case ai.AreaAdmin, ai.AreaOnboarding:
		return roleAdmins
	case ai.AreaStage1:
		return []string{RoleConfigEditor, RoleConfigApprover}
	}
	return nil
}

func hasAnyRole(c *call, roles []string) bool {
	for _, r := range roles {
		if c.roles[r] {
			return true
		}
	}
	return false
}

type assistBody struct {
	Area      string         `json:"area"`
	Step      string         `json:"step"`
	ConfigID  string         `json:"configId"`
	Version   int            `json:"version"`
	Question  string         `json:"question"`
	Documents []ai.Doc       `json:"documents"`
	Selection map[string]any `json:"selection"`
}

// aiAssist — POST /ai/assist {area, step, configId, version, question, documents, selection}:
// the assistant's answer and suggestions. Nothing is changed.
func aiAssist(c *call) modulesdk.Response {
	var b assistBody
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	if !ai.ValidArea(b.Area, b.Step) {
		return errInvalid("area", "area is admin, onboarding, or stage1 with a step: "+strings.Join(ai.Stage1Steps, ", ")).response()
	}
	if !hasAnyRole(c, areaRoles(b.Area)) {
		return (&apiError{Status: 403, Code: "FORBIDDEN", Message: "your roles in this client do not include the assistant for " + b.Area}).response()
	}
	if strings.TrimSpace(b.Question) == "" || len([]rune(b.Question)) > ai.MaxQuestionChars {
		return errInvalid("question", "ask a question of at most 4000 characters").response()
	}
	total := 0
	for _, d := range b.Documents {
		total += len([]rune(d.Text))
	}
	if total > 4*ai.MaxDocChars {
		return errInvalid("documents", "the documents are too large for one question; attach the relevant sheets only").response()
	}
	if e := aiGate(c); e != nil {
		return e.response()
	}
	ctxData, doc, e := assistContext(c, b)
	if e != nil {
		return e.response()
	}
	known := ai.KnownFrom(doc, model.StandardClasses(), AllRoles, b.Documents)
	ctx, cancel := context.WithTimeout(context.Background(), ai.Timeout)
	defer cancel()
	res, err := ai.Assist(ctx, drafter(), ai.AssistRequest{Area: b.Area, Step: b.Step, Question: b.Question, Documents: b.Documents, Context: ctxData}, known)
	if err != nil {
		var pe ai.ErrProvider
		if errors.As(err, &pe) {
			return (&apiError{Status: 503, Code: "AI_UNAVAILABLE", Message: pe.Error(), Retryable: true}).response()
		}
		return errInternal("the assistant failed: " + err.Error()).response()
	}
	aiRecordUsage(c, res.Usage)
	used, _ := aiUsedToday(c)
	return jsonResponse(200, map[string]any{"result": res, "budget": map[string]int{"dailyRequests": aiDailyLimit(), "usedToday": used}})
}

// assistContext builds what the assistant may see: only data of the bound tenant that the
// caller's roles allow, trimmed to what the place needs.
func assistContext(c *call, b assistBody) (map[string]any, *model.Document, *apiError) {
	out := map[string]any{"client": c.id.Tenant, "area": b.Area, "step": b.Step, "today": time.Now().UTC().Format(time.DateOnly)}
	if b.Selection != nil {
		out["selection"] = b.Selection
	}
	lib := model.StandardClasses()
	libBrief := []map[string]string{}
	for _, x := range lib.Classes {
		libBrief = append(libBrief, map[string]string{"code": x.Code, "name": x.Name, "definition": x.Definition, "nature": x.Nature, "status": x.Status})
	}
	if p, _ := loadClient(c); p != nil {
		out["clientProfile"] = p
	}
	switch b.Area {
	case ai.AreaAdmin, ai.AreaOnboarding:
		rows, err := c.host.Query(`select actor, string_agg(role, ',' order by role) from role_assignment where tenant_id = $1 group by actor order by actor`, []string{c.id.Tenant})
		if err != nil {
			return nil, nil, errInternal("storage error")
		}
		users := []map[string]any{}
		for _, r := range rows {
			users = append(users, map[string]any{"user": r.Cells[0], "roles": strings.Split(r.Cells[1], ",")})
		}
		out["users"] = users
		out["roleDefinitions"] = roleDefinitions
		if b.Area == ai.AreaOnboarding {
			cfgs, err := c.host.Query(`select a.config_id, a.title, coalesce((select max(v.version) from config_version v where v.tenant_id = a.tenant_id and v.config_id = a.config_id), 0)::text
				from config_artifact a where a.tenant_id = $1 order by a.config_id`, []string{c.id.Tenant})
			if err == nil {
				list := []map[string]string{}
				for _, r := range cfgs {
					list = append(list, map[string]string{"configId": r.Cells[0], "title": r.Cells[1], "latestVersion": r.Cells[2]})
				}
				out["configurations"] = list
			}
			out["standardClassLibrary"] = libBrief
		}
		return out, nil, nil
	}
	// Stage 1: the version the person works on
	if e := pathCode("configId", b.ConfigID); e != nil {
		return nil, nil, e
	}
	v, e := loadVersion(c.host, c.id, b.ConfigID, b.Version, true)
	if e != nil {
		return nil, nil, e
	}
	doc, fs := model.Parse(v.Content)
	fs = append(fs, model.Validate(doc)...)
	var content map[string]json.RawMessage
	_ = json.Unmarshal(v.Content, &content)
	keep := map[string]json.RawMessage{}
	for _, s := range []string{"accountingClasses", "accounts", "accountAssignmentRules", "sourceClasses", "sourceFiles", "dimensions"} {
		if raw, ok := content[s]; ok {
			if len(raw) > 60_000 {
				raw = json.RawMessage(`"(` + strconv.Itoa(len(raw)) + ` bytes: too large to include; ask about specific items)"`)
			}
			keep[s] = raw
		}
	}
	out["configuration"] = map[string]any{"configId": v.ConfigID, "version": v.Version, "status": v.Status, "content": keep}
	if len(fs) > 120 {
		fs = fs[:120]
	}
	out["findings"] = fs
	out["standardClassLibrary"] = libBrief
	return out, doc, nil
}

// roleDefinitions says what each role may do, in business words (the server enforces it).
var roleDefinitions = map[string]string{
	RoleAdmin:          "Administrator: onboards the client, gives people their roles, reads everything; cannot change their own roles.",
	RoleConfigEditor:   "Business analyst (editor): creates and edits configuration drafts, imports, validates and submits them; cannot approve what they edited.",
	RoleConfigApprover: "Reviewer (approver): approves or returns validated versions, retires versions; cannot edit.",
	RoleOperator:       "Operator: runs approved configuration in sandbox books (posting, files).",
	RoleViewer:         "Viewer (finance): reads configuration, books and audit; changes nothing.",
}

// aiDecision — POST /ai/decisions {area, step, configId, version, suggestionId, title, decision,
// detail, provider, model}: records what a person did with a suggestion.
func aiDecision(c *call) modulesdk.Response {
	var b struct {
		Area         string         `json:"area"`
		Step         string         `json:"step"`
		ConfigID     string         `json:"configId"`
		Version      int            `json:"version"`
		SuggestionID string         `json:"suggestionId"`
		Title        string         `json:"title"`
		Decision     string         `json:"decision"`
		Detail       map[string]any `json:"detail"`
		Provider     string         `json:"provider"`
		Model        string         `json:"model"`
	}
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	if !ai.ValidArea(b.Area, b.Step) {
		return errInvalid("area", "unknown area or step").response()
	}
	if !hasAnyRole(c, areaRoles(b.Area)) {
		return (&apiError{Status: 403, Code: "FORBIDDEN", Message: "your roles in this client do not include the assistant for " + b.Area}).response()
	}
	if !oneOfStr(b.Decision, []string{"ACCEPTED", "EDITED", "REJECTED"}) || strings.TrimSpace(b.SuggestionID) == "" {
		return errInvalid("decision", "decision is ACCEPTED, EDITED or REJECTED, with the suggestion's id").response()
	}
	if b.ConfigID != "" {
		if e := pathCode("configId", b.ConfigID); e != nil {
			return e.response()
		}
	}
	detail, _ := json.Marshal(b.Detail)
	if len(detail) > 64<<10 || len(b.Title) > 300 || len(b.Provider) > 40 || len(b.Model) > 100 {
		return errInvalid("detail", "the decision record is too large").response()
	}
	ver := ""
	if b.Version > 0 {
		ver = strconv.Itoa(b.Version)
	}
	if _, err := c.host.Exec(`insert into ai_decision (tenant_id, area, step, config_id, version, suggestion_id, title, decision, detail, provider, model, actor)
			values ($1, $2, $3, nullif($4, ''), nullif($5, '')::integer, $6, $7, $8, $9::jsonb, $10, $11, $12)`,
		[]string{c.id.Tenant, b.Area, b.Step, b.ConfigID, ver, b.SuggestionID, b.Title, b.Decision, string(detail), b.Provider, b.Model, c.id.Actor}); err != nil {
		return errInternal("audit write failed").response()
	}
	return jsonResponse(201, map[string]any{"recorded": true})
}

// aiDecisions — GET /ai/decisions?area=&configId=: the suggestions people accepted, edited or
// rejected in this client.
func aiDecisions(c *call) modulesdk.Response {
	rows, err := c.host.Query(`select decision_id::text, area, step, coalesce(config_id, ''), coalesce(version::text, ''), suggestion_id, title, decision,
			detail::text, provider, model, actor, `+ts("at")+`
		from ai_decision where tenant_id = $1 and ($2 = '' or area = $2) and ($3 = '' or config_id = $3) order by decision_id desc limit 500`,
		[]string{c.id.Tenant, c.query("area"), c.query("configId")})
	if err != nil {
		return errInternal("storage error").response()
	}
	out := []map[string]any{}
	for _, r := range rows {
		x := r.Cells
		v, _ := strconv.Atoi(x[4])
		out = append(out, map[string]any{"id": x[0], "area": x[1], "step": x[2], "configId": x[3], "version": v, "suggestionId": x[5], "title": x[6],
			"decision": x[7], "detail": json.RawMessage(x[8]), "provider": x[9], "model": x[10], "actor": x[11], "at": x[12]})
	}
	return jsonResponse(200, map[string]any{"decisions": out})
}
