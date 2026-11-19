# ADR-0001 — Technology stack and repository structure

**Status:** Accepted. **Decided by:** product owner, 2026-11-19.

## Decision

- **Stack:** Go services, a Svelte 5 UI, PostgreSQL 16, and Temporal for long-running runs. This is the reference (Siddhi) stack.
- **Structure:** the reference structure. One product consists of the units `.app` (Mycel app), `.wfc` (Temporal worker) and `.spec` (specification). Modules use the layout `modules/<token>/module/{mycelfile, Dockerfile, service/{main.go, logic/}, database/{migrations, schema.sql}, ui/}` and a black-box `module_test/`. Module servers are built on the public Go module `github.com/mycelpf/mycel_module_sdk`.
- **Independence:** GL is an independent product. It is not a member of any reference-platform client cluster and has no runtime dependency on reference-platform apps ([ADR-0004](ADR-0004-no-reference-platform-connections.md)). The reference code is study material, not something to copy wholesale.
- The Mycel CLI is **not** installed. The structure is laid down by hand to match the reference files.

## Owner statements (verbatim)

- "Go + Svelte + PostgreSQL", from the stack options, where that option read "Go services, Svelte 5 UI, PostgreSQL 16, Temporal for runs"
- "We have to use Siddhi stack tech and same structure hope its clear"
- "use siddhi as a reference only our is indipendent Eco system"

## Consequences

- Pure `service/logic` packages are unit-tested natively with an in-memory host. Schema behaviour is tested against PostgreSQL 16 in `module_test`.
- Multiple units live in one repository as top-level folders, following the reference precedent of one repository holding several product units.
- Naming is provisional until the product owner confirms it.
