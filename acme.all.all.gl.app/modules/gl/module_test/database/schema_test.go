// Package database checks the gl module's shipped SQL against a real PostgreSQL 16: every
// migration applies on an empty schema, its .down.sql removes exactly what it created, and
// it applies again cleanly (migration / rollback). It reads the canonical files in
// ../module/database/migrations, never the Go copies.
//
//	GL_TEST_DATABASE_URL=postgres://user:pass@host:port/postgres?sslmode=disable go test ./database/...
package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func freshDB(t *testing.T) *sql.DB {
	t.Helper()
	admin := os.Getenv("GL_TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("GL_TEST_DATABASE_URL not set")
	}
	adm, err := sql.Open("postgres", admin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("gl_schema_%d", time.Now().UnixNano())
	if _, err := adm.Exec("create database " + name); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		adm.Exec("select pg_terminate_backend(pid) from pg_stat_activity where datname = $1 and pid <> pg_backend_pid()", name)
		adm.Exec("drop database if exists " + name)
		adm.Close()
	})
	return db
}

type migration struct{ id, up, down string }

func migrations(t *testing.T) []migration {
	t.Helper()
	dir := filepath.Join("..", "..", "module", "database", "migrations")
	ups, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	var out []migration
	for _, f := range ups {
		if strings.HasSuffix(f, ".down.sql") {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(f), ".sql")
		up, _ := os.ReadFile(f)
		down, err := os.ReadFile(filepath.Join(dir, id+".down.sql"))
		if err != nil {
			t.Fatalf("%s has no rollback script: %v", id, err)
		}
		out = append(out, migration{id, string(up), string(down)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	if len(out) == 0 {
		t.Fatal("no migrations found")
	}
	return out
}

func inSchema(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("set local search_path to gl"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(stmt); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func objects(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
		select 'table:' || c.relname || ':rls=' || c.relrowsecurity || ':force=' || c.relforcerowsecurity
		  from pg_class c join pg_namespace n on n.oid = c.relnamespace
		 where n.nspname = 'gl' and c.relkind = 'r' and c.relname <> '_mycel_migrations'
		union all
		select 'function:' || p.proname from pg_proc p join pg_namespace n on n.oid = p.pronamespace where n.nspname = 'gl'
		union all
		select 'trigger:' || tg.tgname from pg_trigger tg join pg_class c on c.oid = tg.tgrelid
		  join pg_namespace n on n.oid = c.relnamespace where n.nspname = 'gl' and not tg.tgisinternal
		union all
		select 'policy:' || pol.tablename || '.' || pol.policyname from pg_policies pol where pol.schemaname = 'gl'
		union all
		select 'index:' || i.indexname from pg_indexes i where i.schemaname = 'gl' and i.indexname not like '%_pkey' and i.tablename <> '_mycel_migrations'
		union all
		select 'constraint:' || con.conname || ':valid=' || con.convalidated from pg_constraint con
		  join pg_class c on c.oid = con.conrelid join pg_namespace n on n.oid = c.relnamespace
		 where n.nspname = 'gl' and con.contype = 'c'
		union all
		select 'column:' || c.relname || '.' || a.attname from pg_attribute a join pg_class c on c.oid = a.attrelid
		  join pg_namespace n on n.oid = c.relnamespace
		 where n.nspname = 'gl' and c.relkind = 'r' and a.attnum > 0 and not a.attisdropped and c.relname <> '_mycel_migrations'
		order by 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

// Every migration applies, rolls back to nothing, and re-applies to the identical schema.
func TestMigrationsApplyRollBackAndReapply(t *testing.T) {
	db := freshDB(t)
	if _, err := db.Exec("create schema gl; create table gl._mycel_migrations (id text primary key, applied_at timestamptz not null default now())"); err != nil {
		t.Fatal(err)
	}
	ms := migrations(t)
	apply := func() {
		for _, m := range ms {
			inSchema(t, db, m.up)
			inSchema(t, db, fmt.Sprintf("insert into _mycel_migrations (id) values ('%s')", m.id))
		}
	}
	apply()
	first := objects(t, db)
	for _, want := range []string{
		"table:config_artifact:rls=true:force=true", "table:config_version:rls=true:force=true", "table:config_audit:rls=true:force=true",
		"trigger:config_version_guard", "trigger:config_audit_append_only",
		"policy:config_artifact.tenant_isolation", "policy:config_version.tenant_isolation", "policy:config_audit.tenant_isolation",
		"index:config_version_one_approved_start",
		// 0002 (US-004)
		"table:role_assignment:rls=true:force=true", "table:role_audit:rls=true:force=true",
		"trigger:role_audit_append_only", "policy:role_assignment.tenant_isolation", "policy:role_audit.tenant_isolation",
		"column:config_version.editors",
		"constraint:config_version_maker_checker:valid=true", "constraint:config_version_has_editor:valid=true",
	} {
		if !contains(first, want) {
			t.Errorf("after up: missing %s (have %v)", want, first)
		}
	}
	for i := len(ms) - 1; i >= 0; i-- {
		inSchema(t, db, ms[i].down)
	}
	if left := objects(t, db); len(left) != 0 {
		t.Fatalf("after down, objects remain: %v", left)
	}
	var ledger int
	db.QueryRow("select count(*) from gl._mycel_migrations").Scan(&ledger)
	if ledger != 0 {
		t.Errorf("after down, %d migration ledger rows remain (the server would not re-apply)", ledger)
	}
	apply()
	if again := objects(t, db); strings.Join(again, "|") != strings.Join(first, "|") {
		t.Errorf("re-applied schema differs:\nfirst %v\nagain %v", first, again)
	}
}

// 0002 on a database that already holds configuration (written under 0001, before roles
// existed): editors are backfilled for every tenant despite forced row-level security, the
// guard is back in force afterwards, a legacy self-approval is kept as history (the
// maker-checker constraint is then left NOT VALID but still binds every new approval), and
// a self-approval is refused from then on.
func TestMigration0002BackfillsExistingConfiguration(t *testing.T) {
	db := freshDB(t)
	if _, err := db.Exec("create schema gl; create table gl._mycel_migrations (id text primary key, applied_at timestamptz not null default now())"); err != nil {
		t.Fatal(err)
	}
	ms := migrations(t)
	inSchema(t, db, ms[0].up)
	// Legacy data in two tenants, written as the owner (superuser here: bypasses RLS).
	inSchema(t, db, `
		insert into config_artifact (tenant_id, config_id, created_by) values ('T1', 'CFG', 'ba'), ('T2', 'CFG', 'lead');
		insert into config_version (tenant_id, config_id, version, status, schema_version, effective_from, content, content_digest,
		        created_by, updated_by, validated_by, validated_at, approved_by, approved_at)
		values ('T1', 'CFG', 1, 'approved', 'gl.config/v1', '2026-01-01', '{}', 'sha256:`+strings.Repeat("a", 64)+`', 'ba', 'ba', 'ba', now(), 'rev', now()),
		       ('T2', 'CFG', 1, 'approved', 'gl.config/v1', '2026-01-01', '{}', 'sha256:`+strings.Repeat("b", 64)+`', 'lead', 'lead', 'lead', now(), 'lead', now());
		insert into config_audit (tenant_id, config_id, version, action, actor) values
		       ('T1', 'CFG', 1, 'create_version', 'ba'), ('T1', 'CFG', 1, 'update_draft', 'editor2'), ('T2', 'CFG', 1, 'create_version', 'lead');`)
	inSchema(t, db, ms[1].up)

	var e1, e2 string
	db.QueryRow(`select array_to_string(array(select unnest(editors) order by 1), ',') from gl.config_version where tenant_id = 'T1'`).Scan(&e1)
	db.QueryRow(`select array_to_string(editors, ',') from gl.config_version where tenant_id = 'T2'`).Scan(&e2)
	if e1 != "ba,editor2" || e2 != "lead" {
		t.Fatalf("backfilled editors: T1=%q (want ba,editor2) T2=%q (want lead)", e1, e2)
	}
	var valid bool
	db.QueryRow(`select convalidated from pg_constraint where conname = 'config_version_maker_checker'`).Scan(&valid)
	if valid {
		t.Error("with a legacy self-approval present, the constraint must be left NOT VALID (history kept)")
	}
	var force, trig bool
	db.QueryRow(`select relforcerowsecurity from pg_class where oid = 'gl.config_version'::regclass`).Scan(&force)
	db.QueryRow(`select tgenabled <> 'D' from pg_trigger where tgname = 'config_version_guard'`).Scan(&trig)
	if !force || !trig {
		t.Errorf("after 0002: forced RLS %v, guard enabled %v — both must be restored", force, trig)
	}
	// From now on a self-approval is refused, legacy history notwithstanding.
	_, err := db.Exec(`insert into gl.config_version (tenant_id, config_id, version, status, schema_version, effective_from, content, content_digest,
		created_by, updated_by, editors, validated_by, validated_at, approved_by, approved_at)
		values ('T1', 'CFG', 2, 'approved', 'gl.config/v1', '2026-02-01', '{}', 'sha256:` + strings.Repeat("c", 64) + `', 'x', 'x', '{x}', 'x', now(), 'x', now())`)
	if err == nil || !strings.Contains(err.Error(), "config_version_maker_checker") {
		t.Errorf("a new self-approval was accepted after 0002: %v", err)
	}
	// And the legacy approved rows are still protected by the guard.
	if _, err := db.Exec(`update gl.config_version set content = '{"x":1}' where tenant_id = 'T1' and version = 1`); err == nil || !strings.Contains(err.Error(), "GL_CONFIG_IMMUTABLE") {
		t.Errorf("legacy approved version was editable after 0002: %v", err)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
