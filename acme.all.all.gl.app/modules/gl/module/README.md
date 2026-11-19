# gl module

Token `gl`: Postgres schema `gl`, routes `/m/gl/*`, element `<gl-app>`.

```
mycelfile                      module identity (token "gl")
Dockerfile                     the gRPC module server image (runs as a non-root user)
service/
  main.go                      gRPC server adapter (SDK) — identity, migrations, route
  logic/                       the pure core: Route + capabilities (config_store.go, …)
    sql/                       byte-identical copies of database/migrations (embedded)
    openapi.json               OpenAPI document, paths prefixed /m/gl
  pghost/                      Host over a PostgreSQL transaction with the SDK's semantics
  cmd/devhttp/                 local HTTP adapter (development only; trusts identity headers)
  dbtest/                      PostgreSQL integration tests of logic.Route (as an ordinary role)
database/
  migrations/NNNN_*.sql        runtime schema for "gl" (+ NNNN_*.down.sql rollbacks)
  schema.sql                   human reference of the final shape
ui/                            <gl-app> custom element
```

## Configuration store (US-003)

| Concern | How |
|---|---|
| Versions | `draft → validated → approved → retired`; a change to an approved version is a **new version** (`POST …/versions` with `basedOnVersion`) |
| Immutability | Database triggers refuse any change to an approved or retired version (except approved → retired) and any deletion. The audit log is append-only |
| Effective dating | `GET /configs/{id}/effective?date=` returns the approved version whose `[effectiveFrom, effectiveTo)` contains the date, choosing the latest `effectiveFrom`. Two approved versions cannot start on the same day |
| Tenant isolation | Tenant from identity only (`x-gl-tenant`), bound per transaction. **Forced row-level security** on every table |
| Concurrency | Edits must quote the `baseDigest` they started from (`STALE_VERSION` otherwise) |
| Digest | `sha256` of the canonical JSON (sorted keys; numbers kept exactly as written; never floats) |

## Configuration model (package `model`)

`POST …/validate` runs the typed configuration model (`service/model`), with every section strictly decoded (unknown fields are findings) and checked across sections. A version with blocking findings cannot be validated, so it cannot be approved.

| Story | Section | Rules |
|---|---|---|
| US-005 | `sourceSystems`, `events`, `sourceAuthority` | Event types are configuration (code, economic key, measures, 1:N lists with item keys). Authority is per **event × measure × effective window `[from, to)`**: a specific measure takes precedence over `"*"`; any other overlap is `AMBIGUOUS_SOURCE_AUTHORITY`; a carried measure without authority is `MISSING_SOURCE_AUTHORITY`. A measure has one grain (event or list). Runtime asks `model.ResolveAuthority` / `model.MayPost`: a non-authoritative copy is reconciliation-only |

## Roles and maker-checker (US-004)

Identity comes from the platform: the app core sets `x-mycel-user`, and the request names its tenant in `x-gl-tenant`. **What a user may do in a tenant is GL's own decision**, stored per tenant in `role_assignment`:

| Role | May |
|---|---|
| `VIEWER` (and every other role) | Read configurations, versions, the effective version and the audit trail |
| `CONFIG_EDITOR` (BA / implementer) | Create configurations and versions, edit drafts, validate |
| `CONFIG_APPROVER` (reviewer / finance) | Approve and retire, **never a version they changed** |
| `OPERATOR` | Run approved configuration (enforced on run endpoints when they arrive) |
| `GL_ADMIN` | Manage role assignments (`PUT /roles/{user}`), but never their own |

- **Maker-checker.** Every user who changed a version's content is recorded in `editors`. A database CHECK makes `approved_by ∈ editors` impossible, and the API answers `403 SELF_APPROVAL`.
- **Membership.** A user with no role in the requested tenant gets `403 NO_TENANT_ACCESS` before any tenant data is read.
- **Why.** Every change needs a `reason`, kept in the append-only audit (`config_audit`, `role_audit`).

**Bootstrapping a tenant's first administrator** (operator task, as the module's database owner):

```sql
begin;
set local search_path to gl;
select set_config('gl.tenant_id', 'CLIENT_X', true);
insert into role_assignment (tenant_id, actor, role, granted_by) values ('CLIENT_X', 'admin@client-x.example', 'GL_ADMIN', 'bootstrap');
insert into role_audit (tenant_id, actor, subject, granted, reason) values ('CLIENT_X', 'bootstrap', 'admin@client-x.example', '{GL_ADMIN}', 'tenant onboarding');
commit;
```

For local development, `cmd/devhttp` accepts `GL_DEV_SEED_ROLES="TENANT:user:ROLE|ROLE;…"`.

## Identity

Every `/m/gl/…` request other than `/info` and `/openapi.json` needs `x-mycel-user` and `x-gl-tenant`. A tenant in a request body is rejected.
