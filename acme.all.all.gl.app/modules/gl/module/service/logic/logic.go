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
	//go:embed sql/0002_roles_maker_checker.sql
	Migration0002SQL string
)

// Ledger ids of the shipped migrations.
const (
	Migration0001ID = "0001_config_store"
	Migration0002ID = "0002_roles_maker_checker"
)

// Migrations is the ordered list the module ships. main.go, the dev adapter and the tests
// all take it from here, so there is exactly one list.
func Migrations() []modulesdk.Migration {
	return []modulesdk.Migration{
		{ID: Migration0001ID, SQL: Migration0001SQL},
		{ID: Migration0002ID, SQL: Migration0002SQL},
	}
}

// OpenAPIFragment is this module's OpenAPI document, paths prefixed /m/gl.
//
//go:embed openapi.json
var OpenAPIFragment string

type handler func(*call) modulesdk.Response

// access is who may call a route.
type access int

const (
	public        access = iota // no identity, touches no table
	authenticated               // any authenticated user (roles are reported, not required)
	roleGated                   // the caller needs one of the route's roles in the tenant
)

type route struct {
	method   string
	segments []string // "{name}" marks a path parameter
	access   access
	anyOf    []string // roles allowed when access == roleGated
	h        handler
}

// call is one request in flight.
type call struct {
	req    modulesdk.Request
	host   modulesdk.Host
	params map[string]string
	id     Identity
	roles  map[string]bool
	q      url.Values
}

func (c *call) query(k string) string { return c.q.Get(k) }

func r(method, pattern string, a access, h handler, anyOf ...string) route {
	return route{method: method, segments: strings.Split(strings.Trim(pattern, "/"), "/"), access: a, anyOf: anyOf, h: h}
}

// Who may do what (US-004): editors change configuration, approvers publish and retire it,
// administrators manage roles, and every member of the tenant may read.
var (
	roleReaders   = []string{RoleViewer, RoleConfigEditor, RoleConfigApprover, RoleOperator, RoleAdmin}
	roleEditors   = []string{RoleConfigEditor}
	roleApprovers = []string{RoleConfigApprover}
	roleAdmins    = []string{RoleAdmin}
)

var routes = []route{
	r("GET", "/info", public, info),
	r("GET", "/openapi.json", public, openapi),
	r("GET", "/me", authenticated, me),
	r("GET", "/roles", roleGated, listRoles, roleAdmins...),
	r("GET", "/roles/audit", roleGated, listRoleAudit, roleAdmins...),
	r("PUT", "/roles/{actor}", roleGated, setRoles, roleAdmins...),
	r("GET", "/configs", roleGated, listConfigs, roleReaders...),
	r("POST", "/configs", roleGated, createConfig, roleEditors...),
	r("GET", "/configs/{configId}/versions", roleGated, listVersions, roleReaders...),
	r("POST", "/configs/{configId}/versions", roleGated, createVersion, roleEditors...),
	r("GET", "/configs/{configId}/versions/{version}", roleGated, getVersion, roleReaders...),
	r("PUT", "/configs/{configId}/versions/{version}", roleGated, updateDraft, roleEditors...),
	r("POST", "/configs/{configId}/versions/{version}/validate", roleGated, validateVersion, roleEditors...),
	r("POST", "/configs/{configId}/versions/{version}/approve", roleGated, approveVersion, roleApprovers...),
	r("POST", "/configs/{configId}/versions/{version}/retire", roleGated, retireVersion, roleApprovers...),
	r("GET", "/configs/{configId}/effective", roleGated, effectiveVersion, roleReaders...),
	r("GET", "/configs/{configId}/effective/manifest", roleGated, effectiveManifest, roleReaders...),
	r("GET", "/configs/{configId}/audit", roleGated, listAudit, roleReaders...),
	r("GET", "/catalog/measures", authenticated, ootbMeasures),
	r("GET", "/catalog/dimensions", authenticated, ootbDimensions),
	r("GET", "/catalog/events", authenticated, ootbEvents),
	r("GET", "/configs/{configId}/versions/{version}/measures", roleGated, versionMeasures, roleReaders...),
	r("POST", "/configs/{configId}/versions/{version}/evaluate", roleGated, evaluateVersion, roleReaders...),
	r("POST", "/configs/{configId}/versions/{version}/keygraph", roleGated, keyGraphVersion, roleReaders...),
	r("POST", "/configs/{configId}/versions/{version}/plan", roleGated, planVersion, roleReaders...),
	r("GET", "/configs/{configId}/versions/{version}/accounts", roleGated, listAccounts, roleReaders...),
	r("GET", "/configs/{configId}/versions/{version}/manifest", roleGated, versionManifest, roleReaders...),
	r("GET", "/configs/{configId}/versions/{version}/manifest/diff", roleGated, manifestDiff, roleReaders...),
	r("GET", "/configs/{configId}/versions/{version}/events", roleGated, versionEvents, roleReaders...),
	r("GET", "/configs/{configId}/versions/{version}/events/{eventType}", roleGated, versionEvent, roleReaders...),
	r("POST", "/configs/{configId}/versions/{version}/scenarios/run", roleGated, runScenarios, roleReaders...),
	r("GET", "/configs/{configId}/versions/{version}/impact", roleGated, versionImpact, roleReaders...),
	r("GET", "/configs/{configId}/versions/{version}/pipeline", roleGated, versionPipeline, roleReaders...),
	r("POST", "/configs/{configId}/versions/{version}/check", roleGated, checkDraft, roleEditors...),
	r("POST", "/configs/{configId}/versions/{version}/accounts/check", roleGated, checkAccount, roleReaders...),
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
		if rt.access != public {
			id, e := identityFrom(req)
			if e != nil {
				return e.response()
			}
			if err := bindTenant(host, id.Tenant); err != nil {
				return errInternal("could not scope the request to its tenant").response()
			}
			c.id = id
			roles, err := loadRoles(host, id)
			if err != nil {
				return errInternal("could not load roles").response()
			}
			c.roles = roles
			if rt.access == roleGated {
				if e := authorize(roles, rt.anyOf); e != nil {
					return e.response()
				}
			}
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
