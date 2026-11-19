# gl module_test

Black-box checks of the gl module from the outside. They are separate from the unit tests in `../module/service`.

| Suite | Needs | Proves |
|---|---|---|
| `database/` | `GL_TEST_DATABASE_URL`: a PostgreSQL 16 role that may `CREATE DATABASE` | Every shipped migration applies on an empty schema, its `.down.sql` removes exactly what it created (including the ledger row), and it re-applies to an identical schema. The tables have RLS enabled **and forced**, and the triggers, policies and unique index exist |
| `service/` | A running gl module at `GL_TEST_BASE_URL` (default `http://127.0.0.1:9080`) | The wire contract: public info and OpenAPI, identity required, the configuration lifecycle, tenant isolation and approved-version immutability over HTTP |

Each suite **skips** when its environment is absent.

```sh
# database
GL_TEST_DATABASE_URL=postgres://user:pass@127.0.0.1:55432/postgres?sslmode=disable go test ./database/...

# service — start the module first (development adapter shown), as an ORDINARY database role
DATABASE_URL=postgres://gl_dev:…@127.0.0.1:55432/gl_dev?sslmode=disable go run ../module/service/cmd/devhttp &
go test ./service/...
```
