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

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
