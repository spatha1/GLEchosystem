package logic

import (
	"regexp"
	"strings"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// Identity is who is calling and for which tenant. The app core authenticates the user
// and injects x-mycel-user; the tenant arrives as x-gl-tenant. Until the identity
// provider decision is made (ADR README "still open"), these two headers are the seam:
// nothing in this module trusts a tenant or actor from the request body.
type Identity struct {
	Tenant string
	Actor  string
}

const (
	headerTenant = "x-gl-tenant"
	headerActor  = "x-mycel-user"
)

var codeRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

func identityFrom(req modulesdk.Request) (Identity, *apiError) {
	tenant := strings.TrimSpace(req.Headers[headerTenant])
	actor := strings.TrimSpace(req.Headers[headerActor])
	if actor == "" {
		return Identity{}, errUnauthenticated("no authenticated user (" + headerActor + ")")
	}
	if len(actor) > 200 {
		return Identity{}, errUnauthenticated("user identity is too long")
	}
	if tenant == "" {
		return Identity{}, errUnauthenticated("no tenant (" + headerTenant + ")")
	}
	if !codeRe.MatchString(tenant) {
		return Identity{}, errInvalid(headerTenant, "tenant must be an upper-case code such as DEMO")
	}
	return Identity{Tenant: tenant, Actor: actor}, nil
}

// bindTenant scopes the request's transaction to one tenant. Row-level security on every
// GL table compares against this setting, so a query that forgets its tenant filter still
// sees only this tenant's rows. The setting is transaction-local (is_local = true).
func bindTenant(h modulesdk.Host, tenant string) error {
	_, err := h.Query("select set_config('gl.tenant_id', $1, true)", []string{tenant})
	return err
}
