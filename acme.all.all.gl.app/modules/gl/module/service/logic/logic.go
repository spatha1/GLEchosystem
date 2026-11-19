// Package logic is the pure, natively-testable core of the gl module: GL configuration,
// rule engine, journals and subledger (spec design/01_boundaries). It depends only on the
// standard library and the root mycel_module_sdk package (Request / Response / Host / Row),
// never on gRPC, the contract bindings or a database driver, so it stays transport-agnostic.
//
// Route is a thin dispatcher; each capability lives in its own file (config_store.go, …).
package logic

import (
	_ "embed"
	"net/url"
	"strings"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// Token is this module's identity: its Postgres schema, its /m/<token>/* route prefix and
// its <token>-app custom element. It MUST match the module's mycelfile.
const Token = "gl"

// Title is the human-readable module title.
const Title = "GL — configuration and rule engine"

// Migrations are shipped in order. Each SQL text is a byte-identical copy of the canonical
// file in database/migrations (TestMigrationsMatchCanonicalFiles).
var (
	//go:embed sql/0001_config_store.sql
	Migration0001SQL string
)

// Migration0001ID is the ledger id of the first migration.
const Migration0001ID = "0001_config_store"

// Migrations is the ordered list the module ships. main.go, the dev adapter and the tests
// all take it from here, so there is exactly one list.
func Migrations() []modulesdk.Migration {
	return []modulesdk.Migration{
		{ID: Migration0001ID, SQL: Migration0001SQL},
	}
}

// OpenAPIFragment is this module's OpenAPI document, paths prefixed /m/gl.
//
//go:embed openapi.json
var OpenAPIFragment string

type handler func(*call) modulesdk.Response

type route struct {
	method   string
	segments []string // "{name}" marks a path parameter
	public   bool     // public routes need no identity and touch no table
	h        handler
}

// call is one request in flight.
type call struct {
	req    modulesdk.Request
	host   modulesdk.Host
	params map[string]string
	id     Identity
	q      url.Values
}

func (c *call) query(k string) string { return c.q.Get(k) }

func r(method, pattern string, public bool, h handler) route {
	return route{method: method, segments: strings.Split(strings.Trim(pattern, "/"), "/"), public: public, h: h}
}

var routes = []route{
	r("GET", "/info", true, info),
	r("GET", "/openapi.json", true, openapi),
	r("GET", "/configs", false, listConfigs),
	r("POST", "/configs", false, createConfig),
	r("GET", "/configs/{configId}/versions", false, listVersions),
	r("POST", "/configs/{configId}/versions", false, createVersion),
	r("GET", "/configs/{configId}/versions/{version}", false, getVersion),
	r("PUT", "/configs/{configId}/versions/{version}", false, updateDraft),
	r("POST", "/configs/{configId}/versions/{version}/validate", false, validateVersion),
	r("POST", "/configs/{configId}/versions/{version}/approve", false, approveVersion),
	r("POST", "/configs/{configId}/versions/{version}/retire", false, retireVersion),
	r("GET", "/configs/{configId}/effective", false, effectiveVersion),
	r("GET", "/configs/{configId}/audit", false, listAudit),
}

func match(rt route, segs []string) (map[string]string, bool) {
	if len(rt.segments) != len(segs) {
		return nil, false
	}
	params := map[string]string{}
	for i, s := range rt.segments {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			v, err := url.PathUnescape(segs[i])
			if err != nil || v == "" {
				return nil, false
			}
			params[s[1:len(s)-1]] = v
			continue
		}
		if s != segs[i] {
			return nil, false
		}
	}
	return params, true
}

// Route is the module's pure request handler. It is total: every request gets a response,
// 404 for an unknown path and 405 for a known path with the wrong method.
func Route(req modulesdk.Request, host modulesdk.Host) modulesdk.Response {
	path := strings.Trim(req.Path, "/")
	segs := strings.Split(path, "/")
	q, _ := url.ParseQuery(req.Query)
	pathKnown := false
	for _, rt := range routes {
		params, ok := match(rt, segs)
		if !ok {
			continue
		}
		pathKnown = true
		if rt.method != req.Method {
			continue
		}
		c := &call{req: req, host: host, params: params, q: q}
		if !rt.public {
			id, e := identityFrom(req)
			if e != nil {
				return e.response()
			}
			if err := bindTenant(host, id.Tenant); err != nil {
				return errInternal("could not scope the request to its tenant").response()
			}
			c.id = id
		}
		return rt.h(c)
	}
	if pathKnown {
		return (&apiError{Status: 405, Code: "METHOD_NOT_ALLOWED", Message: req.Method + " is not supported on /m/gl/" + path}).response()
	}
	return errNotFound("no route /m/gl/" + path).response()
}

func info(c *call) modulesdk.Response {
	return jsonResponse(200, map[string]any{"token": Token, "title": Title, "configSchemaVersion": SchemaVersion})
}

func openapi(c *call) modulesdk.Response {
	return modulesdk.Response{Status: 200, Headers: map[string]string{"content-type": "application/json"}, Body: OpenAPIFragment}
}
