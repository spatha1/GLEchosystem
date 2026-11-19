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

Maker-checker between editor and approver arrives with US-004.

## Identity

Every `/m/gl/configs…` request needs `x-mycel-user` (set by the app core after authentication) and `x-gl-tenant`. A tenant in a request body is rejected.
