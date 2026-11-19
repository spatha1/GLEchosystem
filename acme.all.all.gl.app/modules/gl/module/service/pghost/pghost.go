// Package pghost runs a module's pure Route against a real PostgreSQL database with the
// same semantics as the SDK server (github.com/mycelpf/mycel_module_sdk/server):
//
//   - one transaction per request, search_path pinned to the module schema;
//   - commit when the response status is < 500, roll back otherwise, and a failed commit
//     becomes a 500 (never a false success);
//   - every Query cell is the column rendered as text, SQL NULL as "";
//   - migrations applied in order, at most once, recorded in <schema>._mycel_migrations.
//
// It exists for module tests and the local development adapter (cmd/devhttp). In a
// deployed app the SDK server does this; nothing here is used on that path.
package pghost

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// TxHost implements modulesdk.Host over one transaction.
type TxHost struct {
	Ctx context.Context
	Tx  *sql.Tx
}

func args(params []string) []any {
	a := make([]any, len(params))
	for i, p := range params {
		a[i] = p
	}
	return a
}

// Query runs q and returns every row as text cells (NULL → "").
func (h *TxHost) Query(q string, params []string) ([]modulesdk.Row, error) {
	rows, err := h.Tx.QueryContext(h.Ctx, q, args(params)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []modulesdk.Row
	for rows.Next() {
		cells := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		strs := make([]string, len(cols))
		for i, c := range cells {
			if c.Valid {
				strs[i] = c.String
			}
		}
		out = append(out, modulesdk.Row{Cells: strs})
	}
	return out, rows.Err()
}

// Exec runs a statement and returns the affected row count.
func (h *TxHost) Exec(q string, params []string) (uint64, error) {
	res, err := h.Tx.ExecContext(h.Ctx, q, args(params)...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return uint64(n), nil
}

// Log writes a module log line.
func (h *TxHost) Log(level, msg string) { log.Printf("[%s] %s", level, msg) }

// NowMillis is the wall clock in unix milliseconds.
func (h *TxHost) NowMillis() uint64 { return uint64(time.Now().UnixMilli()) }

// CallExt reports that no extension is wired (none is used by this module).
func (h *TxHost) CallExt(envKey, method, path string, body []byte) (uint16, []byte, bool, error) {
	return 0, nil, false, nil
}

// Options tune Serve for tests.
type Options struct {
	// Role, when set, is assumed for the request (SET LOCAL ROLE) — tests use a
	// non-superuser so row-level security applies exactly as it will in production.
	Role string
}

// Serve handles one request: begin, pin search_path, route, then commit or roll back
// exactly as the SDK server does.
func Serve(ctx context.Context, db *sql.DB, schema string, route modulesdk.RouteFunc, req modulesdk.Request, opt Options) modulesdk.Response {
	if !identRe.MatchString(schema) || (opt.Role != "" && !identRe.MatchString(opt.Role)) {
		return modulesdk.Response{Status: 500, Body: `{"error":{"code":"INTERNAL","message":"bad schema or role name"}}`}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return modulesdk.Response{Status: 500, Body: `{"error":{"code":"INTERNAL","message":"begin failed"}}`}
	}
	defer tx.Rollback()
	if opt.Role != "" {
		if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE "+opt.Role); err != nil {
			return modulesdk.Response{Status: 500, Body: `{"error":{"code":"INTERNAL","message":"set role failed"}}`}
		}
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path TO "+schema); err != nil {
		return modulesdk.Response{Status: 500, Body: `{"error":{"code":"INTERNAL","message":"search_path failed"}}`}
	}
	resp := route(req, &TxHost{Ctx: ctx, Tx: tx})
	if resp.Status < 500 {
		if err := tx.Commit(); err != nil {
			return modulesdk.Response{Status: 500, Body: `{"error":{"code":"INTERNAL","message":"commit failed — the request was not applied"}}`}
		}
	}
	return resp
}

// ApplyMigrations creates the schema and applies each migration once, in order, each with
// its ledger row in one transaction — the SDK server's behaviour.
func ApplyMigrations(db *sql.DB, schema string, migrations []modulesdk.Migration) error {
	if !identRe.MatchString(schema) {
		return fmt.Errorf("bad schema name %q", schema)
	}
	if _, err := db.Exec("CREATE SCHEMA IF NOT EXISTS " + schema); err != nil {
		return err
	}
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS " + schema + "._mycel_migrations (id text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	for _, m := range migrations {
		if strings.TrimSpace(m.SQL) == "" {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec("SET LOCAL search_path TO " + schema); err != nil {
			tx.Rollback()
			return err
		}
		var claimed bool
		err = tx.QueryRow("INSERT INTO _mycel_migrations (id) VALUES ($1) ON CONFLICT (id) DO NOTHING RETURNING true", m.ID).Scan(&claimed)
		if err == sql.ErrNoRows {
			tx.Rollback()
			continue
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s ledger: %w", m.ID, err)
		}
		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", m.ID, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// RLSBypassed reports whether the connected role escapes row-level security (superuser or
// BYPASSRLS). The gl module must never run that way: tenant isolation depends on RLS.
func RLSBypassed(db *sql.DB) (bool, error) {
	var bypass bool
	err := db.QueryRow("select rolsuper or rolbypassrls from pg_roles where rolname = current_user").Scan(&bypass)
	return bypass, err
}
