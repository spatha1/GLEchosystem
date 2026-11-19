# acme.all.all.gl.app — GL Ecosystem app

The GL Ecosystem's mycel app (reference structure, [ADR-0001](../acme.all.all.gl.spec/design/adr/ADR-0001-stack-and-structure.md)). The specification is in [`../acme.all.all.gl.spec`](../acme.all.all.gl.spec/README.md).

| Module | Token | Serves | Status |
|---|---|---|---|
| [`modules/gl`](modules/gl/module/README.md) | `gl` | `/m/gl/*`: configuration (versioned, tenant-scoped, effective-dated), and the rule engine as later stories land | US-003 |

## A product is one folder of units

```
GLEcosystem/
  acme.all.all.gl.app/    this unit: the mycel app (mycelfile, modules/)
  acme.all.all.gl.wfc/    the Temporal workflows (from the run stories)
  acme.all.all.gl.spec/   specification: design/, _harness/
```

## Develop and test

```sh
cd modules/gl/module/service
go test ./...                                     # unit tests (no database)
GL_TEST_DATABASE_URL=postgres://… go test ./dbtest/...   # PostgreSQL integration (fresh database per run)

cd ../../module_test
GL_TEST_DATABASE_URL=postgres://… go test ./database/... # migration up → down → up
go test ./service/...                             # black-box HTTP against a running module
```

The module must connect as an **ordinary** PostgreSQL role, never a superuser or a `BYPASSRLS` role. Tenant isolation is enforced by row-level security, and those roles bypass it. The development adapter refuses to start otherwise.
