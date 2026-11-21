// Command devhttp serves the gl module over plain HTTP for local development and the
// black-box module_test suite, without the Mycel CLI or app core. It runs the same pure
// logic.Route with the same transaction semantics as the SDK server (package pghost).
//
// It is a DEVELOPMENT ADAPTER: it trusts the x-mycel-user and x-gl-tenant headers exactly
// as the app core would set them after authentication, so it listens on loopback only by
// default and must never be deployed. In a deployed app, main.go (the gRPC module server)
// is the entry point and the app core authenticates every request.
//
//	DATABASE_URL=postgres://… go run ./cmd/devhttp
//	curl -H 'x-mycel-user: ba@example.com' -H 'x-gl-tenant: DEMO' localhost:9080/m/gl/configs
package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"gl/ai"
	"gl/logic"
	"gl/pghost"

	_ "github.com/lib/pq"
	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	listen := envOr("GL_DEV_LISTEN", "127.0.0.1:9080")
	if host, _, err := net.SplitHostPort(listen); err == nil && host != "127.0.0.1" && host != "localhost" && os.Getenv("GL_DEV_ALLOW_NON_LOOPBACK") != "1" {
		log.Fatalf("devhttp trusts identity headers; refusing to listen on %s (set GL_DEV_ALLOW_NON_LOOPBACK=1 to override in an isolated network)", listen)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	if bypass, err := pghost.RLSBypassed(db); err != nil {
		log.Fatalf("database unreachable: %v", err)
	} else if bypass && os.Getenv("GL_DEV_ALLOW_RLS_BYPASS") != "1" {
		log.Fatal("the database role is a superuser or BYPASSRLS: tenant isolation would not apply. Connect as an ordinary role (or set GL_DEV_ALLOW_RLS_BYPASS=1 for a throwaway test only)")
	}
	if err := pghost.ApplyMigrations(db, logic.Token, logic.Migrations()); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	if spec := os.Getenv("GL_DEV_SEED_ROLES"); spec != "" {
		if err := seedRoles(db, spec); err != nil {
			log.Fatalf("GL_DEV_SEED_ROLES: %v", err)
		}
	}
	// AI drafting (BRD steps) reads its provider settings from GL_AI_SETTINGS on each request; unset, the studio says AI is not set up.
	logic.AIDrafter = func() ai.Drafter { return ai.Load(os.Getenv("GL_AI_SETTINGS")) }
	prefix := "/m/" + logic.Token + "/"
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, logic.MaxContentBytes+64<<10))
		if err != nil {
			http.Error(w, "unreadable body", http.StatusBadRequest)
			return
		}
		headers := map[string]string{}
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[strings.ToLower(k)] = v[0]
			}
		}
		req := modulesdk.Request{Method: r.Method, Path: strings.TrimPrefix(r.URL.Path, strings.TrimSuffix(prefix, "/")), Query: r.URL.RawQuery, Headers: headers, Body: body}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		resp := pghost.Serve(ctx, db, logic.Token, logic.Route, req, pghost.Options{})
		ct := resp.Headers["content-type"]
		if ct == "" {
			ct = "application/json"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(int(resp.Status))
		io.WriteString(w, resp.Body)
	})
	log.Printf("gl devhttp on http://%s%s (development only)", listen, prefix)
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

// seedRoles bootstraps role assignments for development: a fresh tenant has no GL_ADMIN to
// grant the first roles. Format: "TENANT:user:ROLE|ROLE;TENANT:user:ROLE". Existing
// assignments are kept; each seeded grant is recorded in the role audit.
func seedRoles(db *sql.DB, spec string) error {
	for _, entry := range strings.Split(spec, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 3)
		if len(parts) != 3 {
			return fmt.Errorf("%q is not TENANT:user:ROLE|ROLE", entry)
		}
		tenant, actor, roles := parts[0], parts[1], strings.Split(parts[2], "|")
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec("set local search_path to " + logic.Token); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec("select set_config('gl.tenant_id', $1, true)", tenant); err != nil {
			tx.Rollback()
			return err
		}
		var granted []string
		for _, role := range roles {
			res, err := tx.Exec(`insert into role_assignment (tenant_id, actor, role, granted_by) values ($1, $2, $3, 'devhttp-seed')
				on conflict do nothing`, tenant, actor, role)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("%s: %w", entry, err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				granted = append(granted, role)
			}
		}
		if len(granted) > 0 {
			if _, err := tx.Exec(`insert into role_audit (tenant_id, actor, subject, granted, reason) values ($1, 'devhttp-seed', $2, $3::text[], 'development seed (GL_DEV_SEED_ROLES)')`,
				tenant, actor, "{"+strings.Join(granted, ",")+"}"); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
