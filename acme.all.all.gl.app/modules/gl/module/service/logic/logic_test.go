package logic

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// noDBHost fails every datastore call, proving a route never touched the database.
type noDBHost struct{ touched bool }

func (h *noDBHost) Query(string, []string) ([]modulesdk.Row, error) {
	h.touched = true
	return nil, errors.New("no database in this test")
}
func (h *noDBHost) Exec(string, []string) (uint64, error) {
	h.touched = true
	return 0, errors.New("no database in this test")
}
func (h *noDBHost) Log(string, string) {}
func (h *noDBHost) NowMillis() uint64  { return 0 }
func (h *noDBHost) CallExt(string, string, string, []byte) (uint16, []byte, bool, error) {
	return 0, nil, false, nil
}

func errCode(t *testing.T, resp modulesdk.Response) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("response is not JSON: %s", resp.Body)
	}
	return body.Error.Code
}

func TestCanonicalContentIsOrderIndependentAndExact(t *testing.T) {
	a := `{"sourceSystems":[{"kind":"POLICY","code":"POLICY"}],"baseMeasures":[{"code":"PREMIUM","rank":12345678901234567890,"rate":"0.1","x":1e3}]}`
	b := `{ "baseMeasures": [ { "x": 1e3, "rate": "0.1", "rank": 12345678901234567890, "code": "PREMIUM" } ],
	        "sourceSystems": [ { "code": "POLICY", "kind": "POLICY" } ] }`
	ca, da, _, err := CanonicalContent([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	cb, db, _, err := CanonicalContent([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	if string(ca) != string(cb) || da != db {
		t.Fatalf("same document, different canonical form:\n%s\n%s", ca, cb)
	}
	if !strings.Contains(string(ca), "12345678901234567890") || !strings.Contains(string(ca), "1e3") {
		t.Errorf("numbers must be kept exactly as written (never through float64): %s", ca)
	}
	if !strings.HasPrefix(da, "sha256:") || len(da) != len("sha256:")+64 {
		t.Errorf("digest format: %s", da)
	}
	c2, d2, _, _ := CanonicalContent([]byte(`{"baseMeasures":[{"code":"PREMIUM","note":"<a&b>"}]}`))
	if !strings.Contains(string(c2), "<a&b>") || d2 == da {
		t.Errorf("HTML characters must not be escaped and different content must differ: %s", c2)
	}
	empty, de, _, err := CanonicalContent(nil)
	if err != nil || string(empty) != "{}" || de == "" {
		t.Errorf("empty content should canonicalise to {}: %s %v", empty, err)
	}
}

func TestCanonicalContentRejectsBadShapes(t *testing.T) {
	cases := map[string]string{
		"not an object":     `[1,2]`,
		"unknown section":   `{"journalHacks":[]}`,
		"section not array": `{"baseMeasures":{"code":"X"}}`,
		"item not object":   `{"baseMeasures":["PREMIUM"]}`,
		"trailing data":     `{} {}`,
		"invalid json":      `{"baseMeasures":[}`,
		"too large":         `{"baseMeasures":[{"pad":"` + strings.Repeat("x", MaxContentBytes) + `"}]}`,
	}
	for name, doc := range cases {
		if _, _, _, err := CanonicalContent([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseDateAndEffectiveWindow(t *testing.T) {
	for _, bad := range []string{"2026-02-30", "2026-1-01", "26-01-01", "2026-13-01", "", "2026-01-01T00:00:00Z"} {
		if _, e := ParseDate("d", bad); e == nil {
			t.Errorf("%q accepted as a date", bad)
		}
	}
	if _, e := ParseDate("d", "2028-02-29"); e != nil {
		t.Errorf("a leap day was rejected")
	}
	to := "2026-01-01"
	if e := checkEffective("2026-01-01", &to); e == nil {
		t.Error("an empty window [d, d) was accepted")
	}
	to = "2026-01-02"
	if e := checkEffective("2026-01-01", &to); e != nil {
		t.Errorf("a one-day window was rejected: %v", e)
	}
}

func TestValidatorsRunAndReportDeterministically(t *testing.T) {
	saved := sectionValidators
	defer func() { sectionValidators = saved }()
	sectionValidators = map[string][]SectionValidator{}
	RegisterSectionValidator("reconciliationEquations", func(items []map[string]any, _ map[string][]map[string]any) []Finding {
		var fs []Finding
		for i := len(items) - 1; i >= 0; i-- {
			if items[i]["code"] == nil {
				fs = append(fs, Finding{Path: "reconciliationEquations/" + string(rune('0'+i)), Code: "MISSING_CODE", Severity: "error", Message: "code is required"})
			}
		}
		return fs
	})
	cb, _, shape, _ := CanonicalContent([]byte(`{"reconciliationEquations":[{},{"code":"X"},{}]}`))
	rep := ValidateContent(cb, shape)
	if rep.OK || len(rep.Findings) != 2 || rep.Findings[0].Path != "reconciliationEquations/0" {
		t.Fatalf("report: %+v", rep)
	}
	cok, _, ok, _ := CanonicalContent([]byte(`{"reconciliationEquations":[{"code":"X"}]}`))
	if r := ValidateContent(cok, ok); !r.OK || r.Findings == nil {
		t.Errorf("a clean section should validate with an empty (non-null) findings list: %+v", r)
	}
	defer func() {
		if recover() == nil {
			t.Error("registering a validator for an unknown section must panic")
		}
	}()
	RegisterSectionValidator("noSuchSection", nil)
}

func TestIdentityIsRequiredForConfigRoutes(t *testing.T) {
	h := &noDBHost{}
	base := modulesdk.Request{Method: "GET", Path: "/configs", Headers: map[string]string{}}
	if got := errCode(t, Route(base, h)); got != "UNAUTHENTICATED" {
		t.Errorf("no user: got %s", got)
	}
	base.Headers = map[string]string{headerActor: "ba@example.com"}
	if got := errCode(t, Route(base, h)); got != "UNAUTHENTICATED" {
		t.Errorf("no tenant: got %s", got)
	}
	base.Headers[headerTenant] = "demo"
	if got := errCode(t, Route(base, h)); got != "INVALID_ARGUMENT" {
		t.Errorf("lower-case tenant: got %s", got)
	}
	if h.touched {
		t.Error("an unauthenticated request reached the database")
	}
}

func TestRouteIsTotalAndPublicRoutesNeedNoDatabase(t *testing.T) {
	h := &noDBHost{}
	if resp := Route(modulesdk.Request{Method: "GET", Path: "/info"}, h); resp.Status != 200 || h.touched {
		t.Errorf("/info: status %d, touched db %v", resp.Status, h.touched)
	}
	if resp := Route(modulesdk.Request{Method: "GET", Path: "/openapi.json"}, h); resp.Status != 200 || resp.Body != OpenAPIFragment {
		t.Error("/openapi.json must serve the embedded document")
	}
	if resp := Route(modulesdk.Request{Method: "GET", Path: "/nope"}, h); resp.Status != 404 {
		t.Errorf("unknown path: %d", resp.Status)
	}
	if resp := Route(modulesdk.Request{Method: "DELETE", Path: "/configs/X/versions/1"}, h); resp.Status != 405 {
		t.Errorf("known path, wrong method: %d", resp.Status)
	}
}

// The embedded migration SQL must be byte-identical to the canonical database/migrations file.
func TestMigrationsMatchCanonicalFiles(t *testing.T) {
	for _, m := range Migrations() {
		canon, err := os.ReadFile(filepath.Join("..", "..", "database", "migrations", m.ID+".sql"))
		if err != nil {
			t.Fatalf("%s: %v", m.ID, err)
		}
		if string(canon) != m.SQL {
			t.Errorf("%s: embedded SQL differs from database/migrations/%s.sql", m.ID, m.ID)
		}
		if _, err := os.Stat(filepath.Join("..", "..", "database", "migrations", m.ID+".down.sql")); err != nil {
			t.Errorf("%s: no rollback script (%s.down.sql)", m.ID, m.ID)
		}
	}
}

// Every route is documented in the OpenAPI document under /m/gl, with the same method.
func TestEveryRouteIsDocumented(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(OpenAPIFragment), &doc); err != nil {
		t.Fatal(err)
	}
	for _, rt := range routes {
		p := "/m/gl/" + strings.Join(rt.segments, "/")
		ops, ok := doc.Paths[p]
		if !ok {
			t.Errorf("%s is not in openapi.json", p)
			continue
		}
		if _, ok := ops[strings.ToLower(rt.method)]; !ok {
			t.Errorf("%s %s is not in openapi.json", rt.method, p)
		}
	}
	for p := range doc.Paths {
		if !strings.HasPrefix(p, "/m/gl/") {
			t.Errorf("openapi path %s lacks the /m/gl/ prefix", p)
		}
	}
}
