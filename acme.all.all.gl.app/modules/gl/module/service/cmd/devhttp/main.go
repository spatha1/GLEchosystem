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
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

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

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
