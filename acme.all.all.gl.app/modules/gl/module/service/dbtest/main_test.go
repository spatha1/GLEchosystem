// Package dbtest runs the gl module's pure Route against a real PostgreSQL 16 database,
// through pghost (the SDK server's transaction semantics), as an ORDINARY role — never a
// superuser, which would silently bypass row-level security and prove nothing about tenant
// isolation.
//
//	GL_TEST_DATABASE_URL=postgres://user:pass@127.0.0.1:55432/postgres?sslmode=disable go test ./dbtest/...
//
// Each run creates a fresh database (gl_dbtest_<n>), applies the shipped migrations, and
// drops it afterwards. Without GL_TEST_DATABASE_URL every test here is skipped.
package dbtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gl/logic"
	"gl/pghost"

	_ "github.com/lib/pq"
	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

const appRole = "gl_app_test"

var db *sql.DB

func TestMain(m *testing.M) {
	admin := os.Getenv("GL_TEST_DATABASE_URL")
	if admin == "" {
		fmt.Println("dbtest: GL_TEST_DATABASE_URL not set — PostgreSQL tests skipped")
		os.Exit(m.Run())
	}
	code, err := run(m, admin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbtest setup:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M, admin string) (int, error) {
	adm, err := sql.Open("postgres", admin)
	if err != nil {
		return 0, err
	}
	defer adm.Close()
	name := fmt.Sprintf("gl_dbtest_%d", time.Now().UnixNano())
	if _, err := adm.Exec("create database " + name); err != nil {
		return 0, err
	}
	defer func() {
		adm.Exec("select pg_terminate_backend(pid) from pg_stat_activity where datname = $1 and pid <> pg_backend_pid()", name)
		adm.Exec("drop database if exists " + name)
	}()
	u, err := url.Parse(admin)
	if err != nil {
		return 0, err
	}
	u.Path = "/" + name
	db, err = sql.Open("postgres", u.String())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	if err := pghost.ApplyMigrations(db, logic.Token, logic.Migrations()); err != nil {
		return 0, err
	}
	setup := []string{
		`do $$ begin if not exists (select from pg_roles where rolname = '` + appRole + `') then
			create role ` + appRole + ` nologin nosuperuser nobypassrls; end if; end $$`,
		"grant usage on schema gl to " + appRole,
		// DELETE is granted on purpose: the triggers, not missing privileges, must stop deletes.
		"grant select, insert, update, delete on all tables in schema gl to " + appRole,
		"grant usage, select on all sequences in schema gl to " + appRole,
	}
	for _, s := range setup {
		if _, err := db.Exec(s); err != nil {
			return 0, fmt.Errorf("%s: %w", s, err)
		}
	}
	// Role seeding (as the test owner, outside any request): the people of the scenarios.
	for _, tenant := range []string{"CLIENT_A", "CLIENT_B"} {
		for actor, roles := range map[string][]string{
			"ba@example.com":       {"CONFIG_EDITOR"},
			"reviewer@example.com": {"CONFIG_APPROVER"},
			"lead@example.com":     {"CONFIG_EDITOR", "CONFIG_APPROVER"},
			"viewer@example.com":   {"VIEWER"},
			"operator@example.com": {"OPERATOR"},
			"admin@example.com":    {"GL_ADMIN"},
		} {
			for _, role := range roles {
				if _, err := db.Exec("insert into gl.role_assignment (tenant_id, actor, role, granted_by) values ($1, $2, $3, 'test-seed')", tenant, actor, role); err != nil {
					return 0, err
				}
			}
		}
	}
	// outsider@ belongs to CLIENT_A only.
	if _, err := db.Exec("insert into gl.role_assignment (tenant_id, actor, role, granted_by) values ('CLIENT_A', 'outsider@example.com', 'CONFIG_EDITOR', 'test-seed')"); err != nil {
		return 0, err
	}
	// A validator that fails on purpose, so the 422 path and its audit can be exercised.
	logic.RegisterSectionValidator("outputProfiles", func(items []map[string]any, _ map[string][]map[string]any) []logic.Finding {
		var fs []logic.Finding
		for i, it := range items {
			if it["invalid"] == true {
				fs = append(fs, logic.Finding{Path: fmt.Sprintf("outputProfiles/%d", i), Code: "TEST_INVALID", Severity: "error", Message: "marked invalid by the test"})
			}
		}
		return fs
	})
	return m.Run(), nil
}

func needDB(t *testing.T) {
	t.Helper()
	if db == nil {
		t.Skip("GL_TEST_DATABASE_URL not set")
	}
}

type result struct {
	Status int
	Body   map[string]any
	Raw    string
}

func (r result) code() string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		s, _ := e["code"].(string)
		return s
	}
	return ""
}

func (r result) str(k string) string { s, _ := r.Body[k].(string); return s }

func (r result) num(k string) int { f, _ := r.Body[k].(float64); return int(f) }

// do sends one request through the module exactly as the app core would deliver it.
func do(t *testing.T, method, path, tenant, actor string, body any) result {
	t.Helper()
	var raw []byte
	switch b := body.(type) {
	case nil:
	case string:
		raw = []byte(b)
	default:
		var err error
		if raw, err = json.Marshal(b); err != nil {
			t.Fatal(err)
		}
	}
	path, query, _ := strings.Cut(path, "?")
	headers := map[string]string{}
	if tenant != "" {
		headers["x-gl-tenant"] = tenant
	}
	if actor != "" {
		headers["x-mycel-user"] = actor
	}
	req := modulesdk.Request{Method: method, Path: path, Query: query, Headers: headers, Body: raw}
	resp := pghost.Serve(context.Background(), db, logic.Token, logic.Route, req, pghost.Options{Role: appRole})
	out := result{Status: int(resp.Status), Raw: resp.Body}
	_ = json.Unmarshal([]byte(resp.Body), &out.Body)
	return out
}

// asApp runs raw SQL as the application role, bound to a tenant ("" = no tenant bound),
// to prove the database itself — not just the handlers — enforces the invariants.
func asApp(t *testing.T, tenant string, f func(tx *sql.Tx) error) error {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("set local role " + appRole); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("set local search_path to gl"); err != nil {
		t.Fatal(err)
	}
	if tenant != "" {
		if _, err := tx.Exec("select set_config('gl.tenant_id', $1, true)", tenant); err != nil {
			t.Fatal(err)
		}
	}
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}

var seq int

// uniq returns a fresh configuration id so tests never collide.
func uniq(prefix string) string {
	seq++
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano()%1e9, seq)
}

func mustStatus(t *testing.T, r result, want int) {
	t.Helper()
	if r.Status != want {
		t.Fatalf("status %d, want %d: %s", r.Status, want, r.Raw)
	}
}
