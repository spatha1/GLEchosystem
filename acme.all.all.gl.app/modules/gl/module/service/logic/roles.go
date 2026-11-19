package logic

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// Roles a user can hold in a tenant (US-004). Identity comes from the platform; what an
// identity may do in a tenant is GL's own decision, stored per tenant.
const (
	RoleAdmin          = "GL_ADMIN"        // manages role assignments in the tenant
	RoleConfigEditor   = "CONFIG_EDITOR"   // BA / implementation team: creates and edits configuration
	RoleConfigApprover = "CONFIG_APPROVER" // independent reviewer / finance: approves and retires
	RoleOperator       = "OPERATOR"        // runs approved configuration (run endpoints arrive with US-017/US-033)
	RoleViewer         = "VIEWER"          // reads configuration and audit
)

// AllRoles lists every role, in a stable order.
var AllRoles = []string{RoleAdmin, RoleConfigEditor, RoleConfigApprover, RoleOperator, RoleViewer}

func validRole(r string) bool {
	for _, x := range AllRoles {
		if x == r {
			return true
		}
	}
	return false
}

// loadRoles returns the caller's roles in the bound tenant.
func loadRoles(h modulesdk.Host, id Identity) (map[string]bool, error) {
	rows, err := h.Query("select role from role_assignment where tenant_id = $1 and actor = $2", []string{id.Tenant, id.Actor})
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, r := range rows {
		out[r.Cells[0]] = true
	}
	return out, nil
}

// authorize enforces a route's role requirement. A user with no role at all in the tenant
// is refused before any tenant data is read; otherwise the user needs one of `anyOf`.
func authorize(roles map[string]bool, anyOf []string) *apiError {
	if len(anyOf) == 0 {
		return nil
	}
	if len(roles) == 0 {
		return &apiError{Status: 403, Code: "NO_TENANT_ACCESS", Message: "you have no role in this tenant"}
	}
	for _, r := range anyOf {
		if roles[r] {
			return nil
		}
	}
	return &apiError{Status: 403, Code: "FORBIDDEN", Message: "requires role " + strings.Join(anyOf, " or ")}
}

func sortedRoles(m map[string]bool) []string {
	out := []string{}
	for r := range m {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// me reports the caller's identity and roles in the tenant (for the UI to show only the
// actions the user may take; the API remains the authority).
func me(c *call) modulesdk.Response {
	return jsonResponse(200, map[string]any{"tenantId": c.id.Tenant, "actor": c.id.Actor, "roles": sortedRoles(c.roles)})
}

type roleGrant struct {
	Role      string `json:"role"`
	GrantedBy string `json:"grantedBy"`
	GrantedAt string `json:"grantedAt"`
}

func listRoles(c *call) modulesdk.Response {
	rows, err := c.host.Query("select actor, role, granted_by, "+ts("granted_at")+" from role_assignment where tenant_id = $1 order by actor, role",
		[]string{c.id.Tenant})
	if err != nil {
		return errInternal("storage error").response()
	}
	type entry struct {
		Actor string      `json:"actor"`
		Roles []roleGrant `json:"roles"`
	}
	out := []entry{}
	for _, r := range rows {
		x := r.Cells
		if len(out) == 0 || out[len(out)-1].Actor != x[0] {
			out = append(out, entry{Actor: x[0]})
		}
		out[len(out)-1].Roles = append(out[len(out)-1].Roles, roleGrant{x[1], x[2], x[3]})
	}
	return jsonResponse(200, map[string]any{"assignments": out})
}

type setRolesBody struct {
	Roles  []string `json:"roles"`
	Reason string   `json:"reason"`
}

// setRoles replaces a user's roles in the tenant. Admins cannot change their own roles (no
// self-elevation, no accidental lock-out), the tenant always keeps at least one admin, and
// every change is recorded in the append-only role audit with its reason.
func setRoles(c *call) modulesdk.Response {
	subject := strings.TrimSpace(c.params["actor"])
	if subject == "" || len(subject) > 200 {
		return errInvalid("actor", "a user identity is required").response()
	}
	var b setRolesBody
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	if strings.TrimSpace(b.Reason) == "" {
		return errInvalid("reason", "a reason is required to change roles").response()
	}
	want := map[string]bool{}
	for _, r := range b.Roles {
		if !validRole(r) {
			return errInvalid("roles", "unknown role "+strconv.Quote(r)+"; known: "+strings.Join(AllRoles, ", ")).response()
		}
		want[r] = true
	}
	if subject == c.id.Actor {
		return (&apiError{Status: 403, Code: "SELF_ROLE_CHANGE", Message: "you cannot change your own roles; ask another administrator"}).response()
	}
	rows, err := c.host.Query("select role from role_assignment where tenant_id = $1 and actor = $2", []string{c.id.Tenant, subject})
	if err != nil {
		return errInternal("storage error").response()
	}
	have := map[string]bool{}
	for _, r := range rows {
		have[r.Cells[0]] = true
	}
	var granted, revoked []string
	for _, r := range AllRoles {
		switch {
		case want[r] && !have[r]:
			granted = append(granted, r)
		case have[r] && !want[r]:
			revoked = append(revoked, r)
		}
	}
	if len(granted) == 0 && len(revoked) == 0 {
		return jsonResponse(200, map[string]any{"actor": subject, "roles": sortedRoles(have), "changed": false})
	}
	for _, r := range revoked {
		if r == RoleAdmin {
			n, err := c.host.Query("select count(*)::text from role_assignment where tenant_id = $1 and role = 'GL_ADMIN' and actor <> $2", []string{c.id.Tenant, subject})
			if err != nil {
				return errInternal("storage error").response()
			}
			if n[0].Cells[0] == "0" {
				return errConflict("LAST_ADMIN", "the tenant must keep at least one GL_ADMIN", false).response()
			}
		}
		if _, err := c.host.Exec("delete from role_assignment where tenant_id = $1 and actor = $2 and role = $3", []string{c.id.Tenant, subject, r}); err != nil {
			return errInternal("storage error").response()
		}
	}
	for _, r := range granted {
		if _, err := c.host.Exec("insert into role_assignment (tenant_id, actor, role, granted_by) values ($1, $2, $3, $4)", []string{c.id.Tenant, subject, r, c.id.Actor}); err != nil {
			return errInternal("storage error").response()
		}
	}
	if _, err := c.host.Exec("insert into role_audit (tenant_id, actor, subject, granted, revoked, reason) values ($1, $2, $3, $4::text[], $5::text[], $6)",
		[]string{c.id.Tenant, c.id.Actor, subject, pgArray(granted), pgArray(revoked), b.Reason}); err != nil {
		return errInternal("audit write failed").response()
	}
	return jsonResponse(200, map[string]any{"actor": subject, "roles": sortedRoles(want), "granted": nonNil(granted), "revoked": nonNil(revoked), "changed": true})
}

func listRoleAudit(c *call) modulesdk.Response {
	rows, err := c.host.Query(`select audit_id::text, actor, subject, array_to_json(granted)::text, array_to_json(revoked)::text, reason, `+ts("at")+`
		from role_audit where tenant_id = $1 order by audit_id`, []string{c.id.Tenant})
	if err != nil {
		return errInternal("storage error").response()
	}
	type entry struct {
		AuditID int64           `json:"auditId"`
		Actor   string          `json:"actor"`
		Subject string          `json:"subject"`
		Granted json.RawMessage `json:"granted"`
		Revoked json.RawMessage `json:"revoked"`
		Reason  string          `json:"reason"`
		At      string          `json:"at"`
	}
	out := []entry{}
	for _, r := range rows {
		x := r.Cells
		id, _ := strconv.ParseInt(x[0], 10, 64)
		out = append(out, entry{id, x[1], x[2], json.RawMessage(x[3]), json.RawMessage(x[4]), x[5], x[6]})
	}
	return jsonResponse(200, map[string]any{"entries": out})
}

// pgArray renders role codes as a Postgres text[] literal. Role codes are validated
// upper-case identifiers, so no quoting is needed.
func pgArray(xs []string) string {
	return "{" + strings.Join(xs, ",") + "}"
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
