package dbtest

import (
	"database/sql"
	"strings"
	"testing"
)

const (
	lead     = "lead@example.com" // holds CONFIG_EDITOR and CONFIG_APPROVER: the classic self-approval risk
	viewer   = "viewer@example.com"
	operator = "operator@example.com"
	admin    = "admin@example.com"
	outsider = "outsider@example.com" // has a role in CLIENT_A only
)

func reason(r string) map[string]string { return map[string]string{"reason": r} }

// AC: the actor who changed a version cannot approve it — even when they hold the approver role.
func TestSelfApprovalIsRefused(t *testing.T) {
	needDB(t)
	id := uniq("SELF")
	mustStatus(t, do(t, "POST", "/configs", tenantA, lead, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "reason": "lead drafts"}), 201)
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, lead, nil), 200)
	r := do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, lead, reason("approve my own work"))
	if r.Status != 403 || r.code() != "SELF_APPROVAL" {
		t.Fatalf("the author approved their own version: %s", r.Raw)
	}
	if g := do(t, "GET", "/configs/"+id+"/versions/1", tenantA, lead, nil); g.str("status") != "validated" {
		t.Fatalf("a refused self-approval must leave the version validated: %s", g.Raw)
	}
	a := do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, rev, reason("independent review"))
	mustStatus(t, a, 200)
	if a.str("approvedBy") != rev {
		t.Errorf("approver: %s", a.Raw)
	}

	// Editing someone else's draft makes you one of its makers too.
	id2 := uniq("SELF2")
	c := create(t, tenantA, id2, "2026-01-01")
	mustStatus(t, do(t, "PUT", "/configs/"+id2+"/versions/1", tenantA, lead, map[string]any{"baseDigest": c.str("contentDigest"), "content": map[string]any{}, "reason": "lead tweak"}), 200)
	mustStatus(t, do(t, "POST", "/configs/"+id2+"/versions/1/validate", tenantA, ba, nil), 200)
	if r := do(t, "POST", "/configs/"+id2+"/versions/1/approve", tenantA, lead, reason("looks fine")); r.code() != "SELF_APPROVAL" {
		t.Errorf("an editor of the draft approved it: %s", r.Raw)
	}
	g := do(t, "GET", "/configs/"+id2+"/versions/1", tenantA, ba, nil)
	if !strings.Contains(g.Raw, `"editors":["ba@example.com","lead@example.com"]`) {
		t.Errorf("both makers must be recorded: %s", g.Raw)
	}
}

// The database refuses a self-approval even when the API is bypassed.
func TestDatabaseRefusesSelfApprovalInSQL(t *testing.T) {
	needDB(t)
	id := uniq("SQLSELF")
	create(t, tenantA, id, "2026-01-01")
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil), 200)
	err := asApp(t, tenantA, func(tx *sql.Tx) error {
		_, err := tx.Exec(`update config_version set status = 'approved', approved_by = created_by, approved_at = now() where config_id = $1 and version = 1`, id)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "config_version_maker_checker") {
		t.Fatalf("SQL self-approval was not refused by the database: %v", err)
	}
	err = asApp(t, tenantA, func(tx *sql.Tx) error {
		_, err := tx.Exec(`update config_version set editors = '{}' where config_id = $1 and version = 1`, id)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "config_version_has_editor") {
		t.Errorf("erasing the makers was not refused: %v", err)
	}
}

// AC: unauthorised roles are rejected at the API — each action needs its role.
func TestEveryActionNeedsItsRole(t *testing.T) {
	needDB(t)
	id := uniq("ROLES")
	c := create(t, tenantA, id, "2026-01-01")
	cases := []struct {
		who, method, path string
		body              any
		want              int
	}{
		{viewer, "GET", "/configs/" + id + "/versions/1", nil, 200},
		{viewer, "POST", "/configs", map[string]any{"configId": uniq("V"), "effectiveFrom": "2026-01-01", "reason": "x"}, 403},
		{viewer, "POST", "/configs/" + id + "/versions/1/validate", nil, 403},
		{operator, "GET", "/configs/" + id + "/effective?date=2026-01-01", nil, 404}, // allowed to read; nothing approved yet
		{operator, "PUT", "/configs/" + id + "/versions/1", map[string]any{"baseDigest": c.str("contentDigest"), "reason": "x"}, 403},
		{rev, "PUT", "/configs/" + id + "/versions/1", map[string]any{"baseDigest": c.str("contentDigest"), "reason": "x"}, 403},
		{rev, "POST", "/configs/" + id + "/versions", map[string]any{"effectiveFrom": "2026-02-01", "reason": "x"}, 403},
		{ba, "POST", "/configs/" + id + "/versions/1/retire", reason("x"), 403},
		{admin, "POST", "/configs/" + id + "/versions/1/validate", nil, 403},
		{ba, "GET", "/roles", nil, 403},
		{ba, "PUT", "/roles/someone@example.com", map[string]any{"roles": []string{"VIEWER"}, "reason": "x"}, 403},
	}
	for _, k := range cases {
		r := do(t, k.method, k.path, tenantA, k.who, k.body)
		if r.Status != k.want {
			t.Errorf("%s %s %s: got %d, want %d (%s)", k.who, k.method, k.path, r.Status, k.want, r.Raw)
		}
		if k.want == 403 && r.code() != "FORBIDDEN" {
			t.Errorf("%s %s %s: code %s, want FORBIDDEN", k.who, k.method, k.path, r.code())
		}
	}
	// Approving needs the approver role: the BA who drafted cannot, for lack of the role.
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil), 200)
	if r := do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, ba, reason("x")); r.code() != "FORBIDDEN" {
		t.Errorf("an editor without the approver role approved: %s", r.Raw)
	}
}

// Negative: a user of one tenant cannot reach another tenant at all.
func TestCrossTenantAccessIsRefused(t *testing.T) {
	needDB(t)
	id := uniq("XT")
	create(t, tenantB, id, "2026-01-01")
	for _, p := range []string{"/configs", "/configs/" + id + "/versions", "/configs/" + id + "/versions/1", "/configs/" + id + "/audit"} {
		r := do(t, "GET", p, tenantB, outsider, nil)
		if r.Status != 403 || r.code() != "NO_TENANT_ACCESS" {
			t.Errorf("outsider reached CLIENT_B via %s: %s", p, r.Raw)
		}
	}
	if r := do(t, "POST", "/configs", tenantB, outsider, map[string]any{"configId": uniq("X"), "effectiveFrom": "2026-01-01", "reason": "x"}); r.code() != "NO_TENANT_ACCESS" {
		t.Errorf("outsider wrote into CLIENT_B: %s", r.Raw)
	}
	m := do(t, "GET", "/me", tenantB, outsider, nil)
	if m.Status != 200 || !strings.Contains(m.Raw, `"roles":[]`) {
		t.Errorf("/me must answer with no roles for a non-member: %s", m.Raw)
	}
	if m := do(t, "GET", "/me", tenantA, outsider, nil); !strings.Contains(m.Raw, `"roles":["CONFIG_EDITOR"]`) {
		t.Errorf("/me in the outsider's own tenant: %s", m.Raw)
	}
}

// Role administration: admins only, never yourself, always one admin left, always a reason,
// tenant-scoped, and every change in the append-only role audit.
func TestRoleAdministration(t *testing.T) {
	needDB(t)
	newbie := uniq("newbie") + "@example.com"
	g := do(t, "PUT", "/roles/"+newbie, tenantA, admin, map[string]any{"roles": []string{"CONFIG_EDITOR", "VIEWER"}, "reason": "joined the BA team"})
	mustStatus(t, g, 200)
	if !strings.Contains(g.Raw, `"granted":["CONFIG_EDITOR","VIEWER"]`) {
		t.Fatalf("grant: %s", g.Raw)
	}
	if r := do(t, "GET", "/configs", tenantA, newbie, nil); r.Status != 200 {
		t.Errorf("the new editor cannot read: %s", r.Raw)
	}
	if r := do(t, "GET", "/configs", tenantB, newbie, nil); r.code() != "NO_TENANT_ACCESS" {
		t.Errorf("a grant in CLIENT_A leaked into CLIENT_B: %s", r.Raw)
	}
	cases := []struct {
		who, subject string
		body         map[string]any
		code         string
	}{
		{admin, admin, map[string]any{"roles": []string{"GL_ADMIN", "CONFIG_APPROVER"}, "reason": "self-elevate"}, "SELF_ROLE_CHANGE"},
		{admin, newbie, map[string]any{"roles": []string{"SUPERUSER"}, "reason": "x"}, "INVALID_ARGUMENT"},
		{admin, newbie, map[string]any{"roles": []string{"VIEWER"}}, "INVALID_ARGUMENT"},
		{ba, newbie, map[string]any{"roles": []string{"GL_ADMIN"}, "reason": "x"}, "FORBIDDEN"},
	}
	for _, k := range cases {
		if r := do(t, "PUT", "/roles/"+k.subject, tenantA, k.who, k.body); r.code() != k.code {
			t.Errorf("%s → %s %v: got %s, want %s", k.who, k.subject, k.body, r.Raw, k.code)
		}
	}
	// Admins can rotate: another admin demotes and later restores admin@.
	other := uniq("admin2") + "@example.com"
	mustStatus(t, do(t, "PUT", "/roles/"+other, tenantA, admin, map[string]any{"roles": []string{"GL_ADMIN"}, "reason": "second admin"}), 200)
	mustStatus(t, do(t, "PUT", "/roles/"+admin, tenantA, other, map[string]any{"roles": []string{"VIEWER"}, "reason": "rotate"}), 200)
	if r := do(t, "PUT", "/roles/"+other, tenantA, admin, map[string]any{"roles": []string{}, "reason": "remove"}); r.code() != "FORBIDDEN" {
		t.Errorf("a demoted admin still administers roles: %s", r.Raw)
	}
	mustStatus(t, do(t, "PUT", "/roles/"+admin, tenantA, other, map[string]any{"roles": []string{"GL_ADMIN"}, "reason": "restore"}), 200)
	mustStatus(t, do(t, "PUT", "/roles/"+other, tenantA, admin, map[string]any{"roles": []string{}, "reason": "temporary admin leaves"}), 200)
	// Rotation is done; the audit must show every grant and revocation with its reason.
	a := do(t, "GET", "/roles/audit", tenantA, admin, nil)
	if !strings.Contains(a.Raw, `"subject":"`+newbie+`"`) || !strings.Contains(a.Raw, `"reason":"joined the BA team"`) || !strings.Contains(a.Raw, `"revoked":["GL_ADMIN"]`) {
		t.Errorf("role audit incomplete: %s", a.Raw)
	}
	err := asApp(t, tenantA, func(tx *sql.Tx) error { _, err := tx.Exec(`update role_audit set reason = 'rewritten'`); return err })
	if err == nil || !strings.Contains(err.Error(), "GL_AUDIT_APPEND_ONLY") {
		t.Errorf("role audit was rewritable: %v", err)
	}
}

// The tenant always keeps an administrator. Admins cannot change their own roles, so the
// caller of any role change is an admin other than its subject and remains an admin; the
// handler's LAST_ADMIN check is a defensive backstop that this rule makes unreachable.
func TestTheTenantAlwaysKeepsAnAdmin(t *testing.T) {
	needDB(t)
	countAdmins := func() int {
		var n int
		if err := asApp(t, tenantB, func(tx *sql.Tx) error {
			return tx.QueryRow(`select count(*) from role_assignment where role = 'GL_ADMIN'`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := countAdmins(); n != 1 {
		t.Fatalf("fixture: CLIENT_B should start with exactly one admin, has %d", n)
	}
	// The only admin cannot remove itself.
	if r := do(t, "PUT", "/roles/"+admin, tenantB, admin, map[string]any{"roles": []string{}, "reason": "leaving"}); r.code() != "SELF_ROLE_CHANGE" {
		t.Fatalf("the only admin removed itself: %s", r.Raw)
	}
	// A second admin can take over and remove the first — and is then the remaining admin.
	second := uniq("b2") + "@example.com"
	mustStatus(t, do(t, "PUT", "/roles/"+second, tenantB, admin, map[string]any{"roles": []string{"GL_ADMIN"}, "reason": "cover"}), 200)
	mustStatus(t, do(t, "PUT", "/roles/"+admin, tenantB, second, map[string]any{"roles": []string{"VIEWER"}, "reason": "admin@ steps down"}), 200)
	if n := countAdmins(); n != 1 {
		t.Fatalf("after hand-over: %d admins, want 1", n)
	}
	// The former admin can no longer administer; the remaining admin cannot remove itself.
	if r := do(t, "PUT", "/roles/"+second, tenantB, admin, map[string]any{"roles": []string{}, "reason": "x"}); r.code() != "FORBIDDEN" {
		t.Errorf("a former admin changed roles: %s", r.Raw)
	}
	if r := do(t, "PUT", "/roles/"+second, tenantB, second, map[string]any{"roles": []string{}, "reason": "x"}); r.code() != "SELF_ROLE_CHANGE" {
		t.Errorf("the remaining admin removed itself: %s", r.Raw)
	}
	// Restore the fixture (admin@ the only admin) through the same rules.
	mustStatus(t, do(t, "PUT", "/roles/"+admin, tenantB, second, map[string]any{"roles": []string{"GL_ADMIN"}, "reason": "restore"}), 200)
	mustStatus(t, do(t, "PUT", "/roles/"+second, tenantB, admin, map[string]any{"roles": []string{}, "reason": "cover ends"}), 200)
	if n := countAdmins(); n != 1 {
		t.Fatalf("after restore: %d admins, want 1", n)
	}
}

// Every configuration change records who did it, when and why; changes without a reason are refused.
func TestEveryChangeRecordsWhoWhenAndWhy(t *testing.T) {
	needDB(t)
	id := uniq("WHY")
	if r := do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01"}); r.code() != "INVALID_ARGUMENT" || !strings.Contains(r.Raw, "reason") {
		t.Errorf("create without a reason: %s", r.Raw)
	}
	c := create(t, tenantA, id, "2026-01-01")
	if r := do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, map[string]any{"baseDigest": c.str("contentDigest")}); r.code() != "INVALID_ARGUMENT" {
		t.Errorf("edit without a reason: %s", r.Raw)
	}
	if r := do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"effectiveFrom": "2026-02-01"}); r.code() != "INVALID_ARGUMENT" {
		t.Errorf("new version without a reason: %s", r.Raw)
	}
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil), 200)
	if r := do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, rev, nil); r.code() != "INVALID_ARGUMENT" {
		t.Errorf("approval without a reason: %s", r.Raw)
	}
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, rev, reason("matches the client's requirements v3")), 200)
	a := do(t, "GET", "/configs/"+id+"/audit?version=1", tenantA, viewer, nil)
	entries, _ := a.Body["entries"].([]any)
	for _, e := range entries {
		m := e.(map[string]any)
		if m["actor"] == "" || m["at"] == "" {
			t.Errorf("audit entry without actor or time: %v", m)
		}
		if m["action"] != "validate" && m["reason"] == nil {
			t.Errorf("change without a recorded reason: %v", m)
		}
	}
	if !strings.Contains(a.Raw, `"action":"approve","actor":"reviewer@example.com","reason":"matches the client's requirements v3"`) {
		t.Errorf("the approval is not traceable to its approver and reason: %s", a.Raw)
	}
}
