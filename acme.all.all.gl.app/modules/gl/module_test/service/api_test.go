// Package service is the black-box HTTP suite for the gl module. It speaks only HTTP to a
// RUNNING module — the app (through the core) or the development adapter
// (module/service/cmd/devhttp) — and asserts the wire contract: identity is required,
// the configuration lifecycle works end to end, tenants cannot see each other, approved
// versions cannot change, and errors use the documented envelope.
//
// Point GL_TEST_BASE_URL at the running module (default http://127.0.0.1:9080). If nothing
// answers, the suite SKIPS — integration tests gate on their environment.
package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	base   = strings.TrimRight(envOr("GL_TEST_BASE_URL", "http://127.0.0.1:9080"), "/")
	client = &http.Client{Timeout: 10 * time.Second}
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type resp struct {
	status int
	body   map[string]any
	raw    string
}

func (r resp) code() string {
	e, _ := r.body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func call(t *testing.T, method, path, tenant, actor string, body any) resp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+"/m/gl"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req.Header.Set("x-gl-tenant", tenant)
	}
	if actor != "" {
		req.Header.Set("x-mycel-user", actor)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func requireModule(t *testing.T) {
	t.Helper()
	res, err := client.Get(base + "/m/gl/info")
	if err != nil {
		t.Skipf("gl module not reachable at %s (%v)", base, err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Skipf("gl module at %s answered %d", base, res.StatusCode)
	}
}

func TestInfoAndOpenAPIArePublic(t *testing.T) {
	requireModule(t)
	if r := call(t, "GET", "/info", "", "", nil); r.status != 200 || r.body["token"] != "gl" {
		t.Errorf("info: %s", r.raw)
	}
	r := call(t, "GET", "/openapi.json", "", "", nil)
	if r.status != 200 || r.body["openapi"] == nil || !strings.Contains(r.raw, "/m/gl/configs") {
		t.Errorf("openapi: %d", r.status)
	}
}

func TestConfigRoutesRequireIdentity(t *testing.T) {
	requireModule(t)
	if r := call(t, "GET", "/configs", "", "", nil); r.status != 401 || r.code() != "UNAUTHENTICATED" {
		t.Errorf("anonymous: %s", r.raw)
	}
	if r := call(t, "GET", "/configs", "", "ba@example.com", nil); r.status != 401 {
		t.Errorf("no tenant: %s", r.raw)
	}
}

func TestLifecycleIsolationAndImmutabilityOverHTTP(t *testing.T) {
	requireModule(t)
	id := fmt.Sprintf("HTTP_%d", time.Now().UnixNano()%1e12)
	c := call(t, "POST", "/configs", "API_A", "ba@example.com", map[string]any{"configId": id, "effectiveFrom": "2026-01-01",
		"content": map[string]any{"sourceSystems": []any{map[string]any{"code": "POLICY", "kind": "POLICY"}}}})
	if c.status != 201 || c.body["status"] != "draft" {
		t.Fatalf("create: %s", c.raw)
	}
	if r := call(t, "POST", "/configs/"+id+"/versions/1/validate", "API_A", "ba@example.com", nil); r.status != 200 {
		t.Fatalf("validate: %s", r.raw)
	}
	if r := call(t, "POST", "/configs/"+id+"/versions/1/approve", "API_A", "reviewer@example.com", map[string]any{"reason": "ok"}); r.status != 200 || r.body["status"] != "approved" {
		t.Fatalf("approve: %s", r.raw)
	}
	if r := call(t, "PUT", "/configs/"+id+"/versions/1", "API_A", "ba@example.com", map[string]any{"baseDigest": c.body["contentDigest"], "content": map[string]any{}}); r.status != 409 || r.code() != "CONFIG_IMMUTABLE" {
		t.Errorf("edit approved: %s", r.raw)
	}
	if r := call(t, "GET", "/configs/"+id+"/effective?date=2026-02-01", "API_A", "ba@example.com", nil); r.status != 200 || r.body["version"] != float64(1) {
		t.Errorf("effective: %s", r.raw)
	}
	if r := call(t, "GET", "/configs/"+id+"/versions/1", "API_B", "ba@example.com", nil); r.status != 404 {
		t.Errorf("tenant B read tenant A's version: %s", r.raw)
	}
	if r := call(t, "GET", "/configs", "API_B", "ba@example.com", nil); strings.Contains(r.raw, id) {
		t.Errorf("tenant B lists tenant A's configuration: %s", r.raw)
	}
}
