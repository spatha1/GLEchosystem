package logic

// Stage 1 and onboarding: the standard accounting class library, testing which account a line
// would get, the reviewer's return to draft, the client register, and what each role may do.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// standardClasses — GET /catalog/classes: the standard accounting class library.
func standardClasses(c *call) modulesdk.Response {
	return jsonResponse(200, model.StandardClasses())
}

// assignTest — POST …/versions/{n}/assign {class | sourceSystem+sourceClass, conditions,
// entity, currency, bookDate}: which assignment rule a line with these values would match, the
// account it gets and why, every rule considered, and whether the posting gate accepts it.
// Read-only: Stage 1 rule testing, not a journal simulation.
func assignTest(c *call) modulesdk.Response {
	var b struct {
		Class        string            `json:"class"`
		SourceSystem string            `json:"sourceSystem"`
		SourceClass  string            `json:"sourceClass"`
		Conditions   map[string]string `json:"conditions"`
		Entity       string            `json:"entity"`
		Currency     string            `json:"currency"`
		BookDate     string            `json:"bookDate"`
	}
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	day, ok := model.ParseDay(b.BookDate)
	if !ok {
		return errInvalid("bookDate", "bookDate must be YYYY-MM-DD").response()
	}
	v, doc, _, ref, e := parsed(c)
	if e != nil {
		return e.response()
	}
	vals := map[string]string{}
	for k, x := range b.Conditions {
		if strings.TrimSpace(x) != "" {
			vals[k] = strings.TrimSpace(x)
		}
	}
	if b.Entity != "" {
		vals["ENTITY"] = b.Entity
	}
	if b.Currency != "" {
		vals["CURRENCY"] = b.Currency
	}
	out := map[string]any{"configId": v.ConfigID, "version": v.Version, "status": v.Status, "bookDate": b.BookDate}
	class := b.Class
	if class == "" {
		if b.SourceClass == "" {
			return errInvalid("class", "give a class, or a source system and source class").response()
		}
		mapped, why := model.MapSourceClass(doc, b.SourceSystem, b.SourceClass, vals)
		out["mappedBy"] = why
		if mapped == "" {
			out["error"] = map[string]string{"code": "UNMAPPED_SOURCE_CLASS", "message": why}
			return jsonResponse(200, out)
		}
		class = mapped
	}
	out["class"] = class
	a, err := model.AssignAccount(doc, ref, class, vals, day)
	out["assignment"] = a
	if err != nil {
		out["error"] = codeMessage(err)
		return jsonResponse(200, out)
	}
	_, gate := model.CheckPosting(doc, model.PostingLine{Account: a.Account, Ledger: a.Ledger, Class: class, BookDate: day, Entity: b.Entity, Currency: b.Currency, Dimensions: vals})
	out["allowed"] = gate == nil
	if gate != nil {
		out["gate"] = codeMessage(gate)
	}
	return jsonResponse(200, out)
}

func codeMessage(err error) map[string]string {
	if ce, ok := err.(*model.CodeError); ok {
		return map[string]string{"code": ce.Code, "message": ce.Message}
	}
	return map[string]string{"code": "ERROR", "message": err.Error()}
}

// returnToDraft — POST …/versions/{n}/return {reason}: a reviewer sends a validated version back
// to its editors instead of approving it. It becomes a draft again; nothing else changes.
func returnToDraft(c *call) modulesdk.Response {
	configID, n, e := versionParams(c)
	if e != nil {
		return e.response()
	}
	reason, e := optionalReason(c.req)
	if e != nil {
		return e.response()
	}
	if strings.TrimSpace(reason) == "" {
		return errInvalid("reason", "a reason is required: say what the editors should change").response()
	}
	cur, e := loadVersion(c.host, c.id, configID, n, false)
	if e != nil {
		return e.response()
	}
	if cur.Status != "validated" {
		return errConflict("INVALID_STATE", fmt.Sprintf("only a validated version can be returned to draft; version %d is %s", n, cur.Status), false).response()
	}
	err := guarded(c.host, func() error {
		_, err := c.host.Exec(`update config_version set status = 'draft', validated_by = null, validated_at = null, updated_by = $4, updated_at = now()
			where tenant_id = $1 and config_id = $2 and version = $3::integer and status = 'validated'`,
			[]string{c.id.Tenant, configID, strconv.Itoa(n), c.id.Actor})
		return err
	})
	if err != nil {
		return classify(err).response()
	}
	if err := audit(c.host, c.id, configID, n, "return_to_draft", reason, "validated", "draft", cur.ContentDigest, nil); err != nil {
		return errInternal("audit write failed").response()
	}
	v, ae := loadVersion(c.host, c.id, configID, n, true)
	if ae != nil {
		return ae.response()
	}
	return jsonResponse(200, v)
}

// ── the client register ──────────────────────────────────────────────────────────────────────

// ClientProfile is a client as onboarded in GL. The tenant itself (sign-in, its existence)
// belongs to the platform.
type ClientProfile struct {
	TenantID  string          `json:"tenantId"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	ConfigID  string          `json:"configId,omitempty"`
	Profile   json.RawMessage `json:"profile"`
	CreatedBy string          `json:"createdBy"`
	CreatedAt string          `json:"createdAt"`
	UpdatedBy string          `json:"updatedBy"`
	UpdatedAt string          `json:"updatedAt"`
}

var clientStatuses = []string{"ONBOARDING", "ACTIVE", "SUSPENDED"}

func loadClient(c *call) (*ClientProfile, *apiError) {
	rows, err := c.host.Query(`select tenant_id, name, status, coalesce(config_id, ''), profile::text, created_by, `+ts("created_at")+`, updated_by, `+ts("updated_at")+`
		from gl_client where tenant_id = $1`, []string{c.id.Tenant})
	if err != nil {
		return nil, errInternal("storage error")
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := rows[0].Cells
	return &ClientProfile{TenantID: r[0], Name: r[1], Status: r[2], ConfigID: r[3], Profile: json.RawMessage(r[4]), CreatedBy: r[5], CreatedAt: r[6], UpdatedBy: r[7], UpdatedAt: r[8]}, nil
}

// getClient — GET /client: this client's onboarding profile (404 NOT_ONBOARDED when none).
func getClient(c *call) modulesdk.Response {
	p, e := loadClient(c)
	if e != nil {
		return e.response()
	}
	if p == nil {
		return (&apiError{Status: 404, Code: "NOT_ONBOARDED", Message: "client " + c.id.Tenant + " has not been onboarded in GL yet"}).response()
	}
	return jsonResponse(200, p)
}

// putClient — PUT /client {name, status, configId, profile, reason, initialize: {title,
// effectiveFrom}}: onboards or updates this client. With initialize and a configuration that
// does not exist yet, its empty draft (version 1) is created for the analysts. Administrators
// only; role grants stay with PUT /roles/{actor}.
func putClient(c *call) modulesdk.Response {
	var b struct {
		Name       string          `json:"name"`
		Status     string          `json:"status"`
		ConfigID   string          `json:"configId"`
		Profile    json.RawMessage `json:"profile"`
		Reason     string          `json:"reason"`
		Initialize *struct {
			Title         string `json:"title"`
			EffectiveFrom string `json:"effectiveFrom"`
		} `json:"initialize"`
	}
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	if e := requireReason(b.Reason); e != nil {
		return e.response()
	}
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || len(b.Name) > 200 {
		return errInvalid("name", "name is the client's name, at most 200 characters").response()
	}
	if b.Status == "" {
		b.Status = "ONBOARDING"
	}
	if !oneOfStr(b.Status, clientStatuses) {
		return errInvalid("status", "status is one of "+strings.Join(clientStatuses, ", ")).response()
	}
	if b.ConfigID != "" {
		if e := pathCode("configId", b.ConfigID); e != nil {
			return e.response()
		}
	}
	if len(b.Profile) == 0 {
		b.Profile = json.RawMessage("{}")
	}
	var obj map[string]any
	if err := json.Unmarshal(b.Profile, &obj); err != nil || obj == nil {
		return errInvalid("profile", "profile must be a JSON object").response()
	}
	if len(b.Profile) > 64<<10 {
		return errInvalid("profile", "profile is limited to 64 KB").response()
	}
	before, e := loadClient(c)
	if e != nil {
		return e.response()
	}
	profile, _ := json.Marshal(obj)
	cfg := b.ConfigID
	err := guarded(c.host, func() error {
		_, err := c.host.Exec(`insert into gl_client (tenant_id, name, status, config_id, profile, created_by, updated_by)
				values ($1, $2, $3, nullif($4, ''), $5::jsonb, $6, $6)
			on conflict (tenant_id) do update set name = excluded.name, status = excluded.status, config_id = excluded.config_id,
				profile = excluded.profile, updated_by = excluded.updated_by, updated_at = now()`,
			[]string{c.id.Tenant, b.Name, b.Status, cfg, string(profile), c.id.Actor})
		return err
	})
	if err != nil {
		return classify(err).response()
	}
	after, e := loadClient(c)
	if e != nil {
		return e.response()
	}
	action := "update"
	var beforeJSON any
	if before == nil {
		action = "onboard"
	} else {
		beforeJSON = before
	}
	bj, _ := json.Marshal(beforeJSON)
	aj, _ := json.Marshal(after)
	if _, err := c.host.Exec(`insert into gl_client_audit (tenant_id, actor, action, reason, before, after) values ($1, $2, $3, $4, nullif($5, 'null')::jsonb, $6::jsonb)`,
		[]string{c.id.Tenant, c.id.Actor, action, b.Reason, string(bj), string(aj)}); err != nil {
		return errInternal("audit write failed").response()
	}
	out := map[string]any{"client": after}
	if b.Initialize != nil && cfg != "" {
		exists, ae := artifactExists(c.host, c.id, cfg)
		if ae != nil {
			return ae.response()
		}
		if !exists {
			if ae := insertConfig(c, cfg, b.Initialize.Title, b.Initialize.EffectiveFrom, nil, json.RawMessage("{}"), b.Reason); ae != nil {
				return ae.response()
			}
			out["initialized"] = cfg
		} else {
			out["initialized"] = ""
		}
	}
	return jsonResponse(200, out)
}

// clientAudit — GET /client/audit: the history of this client's profile.
func clientAudit(c *call) modulesdk.Response {
	rows, err := c.host.Query(`select audit_id::text, actor, action, reason, coalesce(before::text, 'null'), after::text, `+ts("at")+`
		from gl_client_audit where tenant_id = $1 order by audit_id`, []string{c.id.Tenant})
	if err != nil {
		return errInternal("storage error").response()
	}
	type entry struct {
		ID     string          `json:"id"`
		Actor  string          `json:"actor"`
		Action string          `json:"action"`
		Reason string          `json:"reason"`
		Before json.RawMessage `json:"before"`
		After  json.RawMessage `json:"after"`
		At     string          `json:"at"`
	}
	out := []entry{}
	for _, r := range rows {
		out = append(out, entry{r.Cells[0], r.Cells[1], r.Cells[2], r.Cells[3], json.RawMessage(r.Cells[4]), json.RawMessage(r.Cells[5]), r.Cells[6]})
	}
	return jsonResponse(200, map[string]any{"entries": out})
}

func oneOfStr(s string, set []string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

// ── what each role may do ────────────────────────────────────────────────────────────────────

var routeTable []route

func init() { routeTable = routes }

// permissions — GET /roles/permissions: every API operation and the roles that may call it,
// read from the route table itself, so the screen shows exactly what the server enforces.
func permissions(c *call) modulesdk.Response {
	type op struct {
		Method string   `json:"method"`
		Path   string   `json:"path"`
		Access string   `json:"access"`
		Roles  []string `json:"roles"`
	}
	ops := []op{}
	for _, r := range routeTable {
		a := map[access]string{public: "public", authenticated: "any signed-in user", roleGated: "roles"}[r.access]
		roles := append([]string{}, r.anyOf...)
		if r.access != roleGated {
			roles = []string{}
		}
		ops = append(ops, op{r.method, "/" + strings.Join(r.segments, "/"), a, roles})
	}
	return jsonResponse(200, map[string]any{"roles": AllRoles, "operations": ops, "at": time.Now().UTC().Format(time.RFC3339)})
}
