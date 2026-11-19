package dbtest

import (
	"database/sql"
	"strings"
	"testing"
)

const (
	tenantA = "CLIENT_A"
	tenantB = "CLIENT_B"
	ba      = "ba@example.com"
	rev     = "reviewer@example.com"
)

var sampleContent = map[string]any{
	"sourceSystems": []any{map[string]any{"code": "POLICY", "kind": "POLICY"}, map[string]any{"code": "BILLING", "kind": "BILLING"}},
	"events": []any{
		map[string]any{"code": "PREM_ADDED", "economicKey": []any{"POLICY_NO", "TERM_NO"}, "measures": []any{"PREMIUM", "TAX"}},
	},
	"sourceAuthority": []any{
		map[string]any{"eventType": "PREM_ADDED", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT"},
	},
}

func create(t *testing.T, tenant, id, from string) result {
	t.Helper()
	r := do(t, "POST", "/configs", tenant, ba, map[string]any{"configId": id, "title": "GL rules", "effectiveFrom": from, "content": sampleContent, "reason": "initial setup"})
	mustStatus(t, r, 201)
	return r
}

func approve(t *testing.T, tenant, id string, v string) {
	t.Helper()
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/"+v+"/validate", tenant, ba, nil), 200)
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/"+v+"/approve", tenant, rev, map[string]string{"reason": "reviewed"}), 200)
}

func TestLifecycleDraftValidatedApprovedRetired(t *testing.T) {
	needDB(t)
	id := uniq("LIFE")
	r := create(t, tenantA, id, "2026-01-01")
	if r.str("status") != "draft" || r.num("version") != 1 || r.str("schemaVersion") != "gl.config/v1" || !strings.HasPrefix(r.str("contentDigest"), "sha256:") {
		t.Fatalf("created: %s", r.Raw)
	}
	v := do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil)
	mustStatus(t, v, 200)
	if v.str("status") != "validated" || v.str("validatedBy") != ba {
		t.Fatalf("validated: %s", v.Raw)
	}
	a := do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, rev, map[string]string{"reason": "go live"})
	mustStatus(t, a, 200)
	if a.str("status") != "approved" || a.str("approvedBy") != rev || a.str("contentDigest") != r.str("contentDigest") {
		t.Fatalf("approved: %s", a.Raw)
	}
	e := do(t, "GET", "/configs/"+id+"/effective?date=2026-06-30", tenantA, ba, nil)
	mustStatus(t, e, 200)
	if e.num("version") != 1 || e.Body["content"] == nil {
		t.Fatalf("effective: %s", e.Raw)
	}
	x := do(t, "POST", "/configs/"+id+"/versions/1/retire", tenantA, rev, map[string]string{"reason": "superseded"})
	mustStatus(t, x, 200)
	if x.str("status") != "retired" || x.str("retiredBy") != rev || x.str("approvedBy") != rev {
		t.Fatalf("retired must keep its approval: %s", x.Raw)
	}
	if got := do(t, "GET", "/configs/"+id+"/effective?date=2026-06-30", tenantA, ba, nil); got.code() != "NO_EFFECTIVE_VERSION" {
		t.Errorf("a retired version must not be effective: %s", got.Raw)
	}
}

// Invalid transitions are refused with precise codes.
func TestInvalidTransitionsAreRefused(t *testing.T) {
	needDB(t)
	id := uniq("TRANS")
	create(t, tenantA, id, "2026-01-01")
	if r := do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, rev, map[string]string{"reason": "test approval"}); r.code() != "INVALID_STATE" {
		t.Errorf("approving a draft: %s", r.Raw)
	}
	if r := do(t, "POST", "/configs/"+id+"/versions/1/retire", tenantA, rev, map[string]string{"reason": ""}); r.code() != "INVALID_ARGUMENT" {
		t.Errorf("retiring without a reason: %s", r.Raw)
	}
	approve(t, tenantA, id, "1")
	if r := do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil); r.code() != "INVALID_STATE" {
		t.Errorf("re-validating an approved version: %s", r.Raw)
	}
	if r := do(t, "POST", "/configs/"+id+"/versions/9/approve", tenantA, rev, map[string]string{"reason": "test approval"}); r.Status != 404 {
		t.Errorf("unknown version: %s", r.Raw)
	}
}

// AC: approved configuration cannot be modified in place — through the API or directly in SQL.
func TestApprovedVersionIsImmutable(t *testing.T) {
	needDB(t)
	id := uniq("IMMUT")
	c := create(t, tenantA, id, "2026-01-01")
	approve(t, tenantA, id, "1")
	digest := c.str("contentDigest")

	put := do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, map[string]any{"baseDigest": digest, "content": map[string]any{}, "reason": "sneaky edit"})
	if put.code() != "CONFIG_IMMUTABLE" {
		t.Fatalf("API edit of an approved version: %s", put.Raw)
	}
	for name, stmt := range map[string]string{
		"content":        `update config_version set content = '{}'::jsonb where config_id = $1 and version = 1`,
		"effective date": `update config_version set effective_from = '2025-01-01' where config_id = $1 and version = 1`,
		"approver":       `update config_version set approved_by = 'someone.else' where config_id = $1 and version = 1`,
		"back to draft":  `update config_version set status = 'draft', approved_by = null, approved_at = null where config_id = $1 and version = 1`,
		"delete":         `delete from config_version where config_id = $1 and version = 1`,
	} {
		err := asApp(t, tenantA, func(tx *sql.Tx) error { _, err := tx.Exec(stmt, id); return err })
		if err == nil || !strings.Contains(err.Error(), "GL_CONFIG_IMMUTABLE") {
			t.Errorf("SQL %s change of an approved version was not blocked by the database: %v", name, err)
		}
	}
	got := do(t, "GET", "/configs/"+id+"/versions/1", tenantA, ba, nil)
	if got.str("status") != "approved" || got.str("contentDigest") != digest {
		t.Fatalf("approved version changed: %s", got.Raw)
	}
	// Retirement is the one permitted change; after it, nothing changes.
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/retire", tenantA, rev, map[string]string{"reason": "end of life"}), 200)
	err := asApp(t, tenantA, func(tx *sql.Tx) error {
		_, err := tx.Exec(`update config_version set status = 'approved' where config_id = $1 and version = 1`, id)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "GL_CONFIG_IMMUTABLE") {
		t.Errorf("un-retiring a version was not blocked: %v", err)
	}
}

// A change to an approved configuration is a new version, copied from the approved one.
func TestChangesArePublishedAsNewVersions(t *testing.T) {
	needDB(t)
	id := uniq("NEWVER")
	c := create(t, tenantA, id, "2026-01-01")
	approve(t, tenantA, id, "1")
	n := do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"basedOnVersion": 1, "effectiveFrom": "2026-03-01", "reason": "Q2 change"})
	mustStatus(t, n, 201)
	if n.num("version") != 2 || n.str("status") != "draft" || n.num("basedOnVersion") != 1 || n.str("contentDigest") != c.str("contentDigest") {
		t.Fatalf("new version should copy v1's content as a draft: %s", n.Raw)
	}
	if v1 := do(t, "GET", "/configs/"+id+"/versions/1", tenantA, ba, nil); v1.str("status") != "approved" {
		t.Errorf("creating v2 must not touch v1: %s", v1.Raw)
	}
}

// AC: client A's configuration is invisible to client B — through the API, and in SQL even
// when a query forgets its tenant filter.
func TestTenantIsolation(t *testing.T) {
	needDB(t)
	id := uniq("ISO")
	create(t, tenantA, id, "2026-01-01")

	list := do(t, "GET", "/configs", tenantB, ba, nil)
	mustStatus(t, list, 200)
	if strings.Contains(list.Raw, id) {
		t.Fatalf("tenant B can list tenant A's configuration: %s", list.Raw)
	}
	for _, p := range []string{"/configs/" + id + "/versions", "/configs/" + id + "/versions/1", "/configs/" + id + "/audit"} {
		r := do(t, "GET", p, tenantB, ba, nil)
		if strings.Contains(r.Raw, `"tenantId":"`+tenantA+`"`) || strings.Contains(r.Raw, "initial setup") {
			t.Errorf("tenant B read tenant A's data via %s: %s", p, r.Raw)
		}
	}
	if r := do(t, "GET", "/configs/"+id+"/versions/1", tenantB, ba, nil); r.Status != 404 {
		t.Errorf("tenant B should get 404 for A's version, got %d", r.Status)
	}
	if r := do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantB, ba, nil); r.Status != 404 {
		t.Errorf("tenant B must not be able to act on A's version: %s", r.Raw)
	}
	// The same id is a different configuration in tenant B.
	mustStatus(t, do(t, "POST", "/configs", tenantB, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "reason": "test change"}), 201)

	// Database level: a query WITHOUT a tenant filter sees only the bound tenant's rows.
	var aRows, bRows, unbound int
	_ = asApp(t, tenantA, func(tx *sql.Tx) error {
		return tx.QueryRow(`select count(*) from config_version where config_id = $1`, id).Scan(&aRows)
	})
	_ = asApp(t, tenantB, func(tx *sql.Tx) error {
		return tx.QueryRow(`select count(*) from config_version where config_id = $1`, id).Scan(&bRows)
	})
	_ = asApp(t, "", func(tx *sql.Tx) error { return tx.QueryRow(`select count(*) from config_version`).Scan(&unbound) })
	if aRows != 1 || bRows != 1 || unbound != 0 {
		t.Errorf("row-level security: A sees %d, B sees %d, unbound sees %d (want 1, 1, 0)", aRows, bRows, unbound)
	}
	// Writing another tenant's row is refused by the policy's WITH CHECK.
	err := asApp(t, tenantB, func(tx *sql.Tx) error {
		_, err := tx.Exec(`insert into config_artifact (tenant_id, config_id, created_by) values ($1, $2, 'x')`, tenantA, uniq("SMUGGLE"))
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Errorf("tenant B wrote a row for tenant A: %v", err)
	}
	// Updating another tenant's rows affects nothing (they are invisible).
	var affected int64
	_ = asApp(t, tenantB, func(tx *sql.Tx) error {
		res, err := tx.Exec(`update config_artifact set title = 'hijacked' where tenant_id = $1`, tenantA)
		if err == nil {
			affected, _ = res.RowsAffected()
		}
		return err
	})
	if affected != 0 {
		t.Errorf("tenant B updated %d of tenant A's rows", affected)
	}
}

// The effective version on a date is the approved one with the latest start on or before it.
func TestEffectiveDatedResolution(t *testing.T) {
	needDB(t)
	id := uniq("EFF")
	create(t, tenantA, id, "2026-01-01")
	approve(t, tenantA, id, "1")
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"basedOnVersion": 1, "effectiveFrom": "2026-03-01", "reason": "test change"}), 201)
	approve(t, tenantA, id, "2")
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"basedOnVersion": 2, "effectiveFrom": "2026-04-01", "effectiveTo": "2026-05-01", "reason": "test change"}), 201)
	approve(t, tenantA, id, "3")
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"basedOnVersion": 2, "effectiveFrom": "2026-06-01", "reason": "test change"}), 201) // v4 stays draft

	for date, want := range map[string]int{
		"2026-01-01": 1, "2026-02-28": 1, "2026-03-01": 2, "2026-03-31": 2,
		"2026-04-01": 3, "2026-04-30": 3, "2026-05-01": 2, "2026-07-01": 2, // v4 is a draft: never effective
	} {
		r := do(t, "GET", "/configs/"+id+"/effective?date="+date, tenantA, ba, nil)
		if r.Status != 200 || r.num("version") != want {
			t.Errorf("%s: got version %d (%d), want %d", date, r.num("version"), r.Status, want)
		}
	}
	if r := do(t, "GET", "/configs/"+id+"/effective?date=2025-12-31", tenantA, ba, nil); r.code() != "NO_EFFECTIVE_VERSION" {
		t.Errorf("before the first start: %s", r.Raw)
	}
	if r := do(t, "GET", "/configs/"+id+"/effective?date=2026-02-30", tenantA, ba, nil); r.code() != "INVALID_ARGUMENT" {
		t.Errorf("impossible date: %s", r.Raw)
	}
	// Two approved versions may not start on the same date.
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions", tenantA, ba, map[string]any{"basedOnVersion": 2, "effectiveFrom": "2026-03-01", "reason": "test change"}), 201)
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/5/validate", tenantA, ba, nil), 200)
	if r := do(t, "POST", "/configs/"+id+"/versions/5/approve", tenantA, rev, map[string]string{"reason": "test approval"}); r.code() != "EFFECTIVE_DATE_CONFLICT" {
		t.Errorf("a second approval starting 2026-03-01: %s", r.Raw)
	}
	if r := do(t, "GET", "/configs/"+id+"/versions/5", tenantA, ba, nil); r.str("status") != "validated" {
		t.Errorf("the refused approval must leave v5 validated: %s", r.Raw)
	}
}

// Editing needs the digest the editor started from; editing a validated version returns it to draft.
func TestDraftEditsAreOptimisticAndResetValidation(t *testing.T) {
	needDB(t)
	id := uniq("EDIT")
	c := create(t, tenantA, id, "2026-01-01")
	d0 := c.str("contentDigest")
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil), 200)

	newContent := map[string]any{"sourceSystems": []any{map[string]any{"code": "CLAIMS", "kind": "CLAIMS"}}}
	e := do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, map[string]any{"baseDigest": d0, "content": newContent, "reason": "add claims"})
	mustStatus(t, e, 200)
	if e.str("status") != "draft" || e.str("contentDigest") == d0 || e.Body["validatedBy"] != nil {
		t.Fatalf("an edit must return the version to draft with a new digest: %s", e.Raw)
	}
	stale := do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, map[string]any{"baseDigest": d0, "content": map[string]any{}, "reason": "lost update"})
	if stale.code() != "STALE_VERSION" {
		t.Errorf("an edit from a stale digest must be refused: %s", stale.Raw)
	}
	if bad := do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, map[string]any{"baseDigest": e.str("contentDigest"), "content": map[string]any{"madeUp": []any{}}, "reason": "test change"}); bad.code() != "INVALID_ARGUMENT" {
		t.Errorf("an unknown section must be refused: %s", bad.Raw)
	}
	if bad := do(t, "PUT", "/configs/"+id+"/versions/1", tenantA, ba, map[string]any{"baseDigest": e.str("contentDigest"), "effectiveTo": "2025-12-31", "reason": "test change"}); bad.code() != "INVALID_ARGUMENT" {
		t.Errorf("an effectiveTo before effectiveFrom must be refused: %s", bad.Raw)
	}
}

// A failing validation keeps the draft, returns the report, and is audited.
func TestValidationFailureIsReportedAndAudited(t *testing.T) {
	needDB(t)
	id := uniq("VALFAIL")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{
		"configId": id, "effectiveFrom": "2026-01-01", "reason": "test change",
		"content": map[string]any{"outputProfiles": []any{map[string]any{"code": "TRF", "invalid": true}}},
	}), 201)
	v := do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, map[string]string{"reason": "try"})
	if v.Status != 422 || v.code() != "VALIDATION_FAILED" || !strings.Contains(v.Raw, "outputProfiles/0") {
		t.Fatalf("validation failure: %s", v.Raw)
	}
	if g := do(t, "GET", "/configs/"+id+"/versions/1", tenantA, ba, nil); g.str("status") != "draft" {
		t.Errorf("a failed validation must leave the draft: %s", g.Raw)
	}
	a := do(t, "GET", "/configs/"+id+"/audit?version=1", tenantA, ba, nil)
	if !strings.Contains(a.Raw, `"action":"validation_failed"`) || !strings.Contains(a.Raw, "TEST_INVALID") {
		t.Errorf("the failed validation must be audited with its findings: %s", a.Raw)
	}
}

// Every change is audited in order with actor, reason and digest; audit rows cannot change.
func TestAuditTrailIsCompleteAndAppendOnly(t *testing.T) {
	needDB(t)
	id := uniq("AUDIT")
	create(t, tenantA, id, "2026-01-01")
	approve(t, tenantA, id, "1")
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/retire", tenantA, rev, map[string]string{"reason": "replaced"}), 200)
	a := do(t, "GET", "/configs/"+id+"/audit", tenantA, ba, nil)
	mustStatus(t, a, 200)
	entries, _ := a.Body["entries"].([]any)
	var actions []string
	for _, e := range entries {
		m := e.(map[string]any)
		actions = append(actions, m["action"].(string)+"/"+m["actor"].(string))
	}
	want := []string{"create_artifact/" + ba, "create_version/" + ba, "validate/" + ba, "approve/" + rev, "retire/" + rev}
	if strings.Join(actions, ",") != strings.Join(want, ",") {
		t.Fatalf("audit trail\n got %v\nwant %v", actions, want)
	}
	if !strings.Contains(a.Raw, `"reason":"replaced"`) || !strings.Contains(a.Raw, `"contentDigest":"sha256:`) {
		t.Errorf("audit must carry reasons and digests: %s", a.Raw)
	}
	for _, stmt := range []string{`update config_audit set actor = 'nobody' where config_id = $1`, `delete from config_audit where config_id = $1`} {
		err := asApp(t, tenantA, func(tx *sql.Tx) error { _, err := tx.Exec(stmt, id); return err })
		if err == nil || !strings.Contains(err.Error(), "GL_AUDIT_APPEND_ONLY") {
			t.Errorf("%q was not blocked: %v", stmt, err)
		}
	}
}

func TestDuplicateAndUnknownConfigurations(t *testing.T) {
	needDB(t)
	id := uniq("DUP")
	create(t, tenantA, id, "2026-01-01")
	if r := do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "reason": "test change"}); r.code() != "ALREADY_EXISTS" {
		t.Errorf("duplicate id: %s", r.Raw)
	}
	if r := do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": "lower_case", "effectiveFrom": "2026-01-01", "reason": "test change"}); r.code() != "INVALID_ARGUMENT" {
		t.Errorf("bad id: %s", r.Raw)
	}
	if r := do(t, "POST", "/configs", tenantA, ba, `{"configId":"X","effectiveFrom":"2026-01-01","tenantId":"CLIENT_B"}`); r.code() != "INVALID_ARGUMENT" {
		t.Errorf("a tenant in the body must be rejected (tenant comes only from identity): %s", r.Raw)
	}
	if r := do(t, "GET", "/configs/NOPE_"+id+"/versions", tenantA, ba, nil); r.Status != 404 {
		t.Errorf("unknown configuration: %s", r.Raw)
	}
}
