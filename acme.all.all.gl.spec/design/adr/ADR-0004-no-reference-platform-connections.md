# ADR-0004 — No connection to reference-platform databases or APIs

**Status:** Accepted. **Decided by:** product owner, 2026-11-19 ("Do not connect to any Siddhi dbs or apis").

## Decision

GL never connects to any database or API of the reference platform: its canonical store, its client clusters or its product services. This applies at runtime, in tests and in tooling. The reference code is read-only study material.

## Consequences

- GL keeps its own configuration, ledger and subledger in its own PostgreSQL schemas.
- The only external system GL calls is the Data Workbench ([ADR-0002](ADR-0002-data-workbench-existing-apis.md)).
- Integration tests use local PostgreSQL and recorded fakes. They never point at reference-platform endpoints.
- Public build inputs (the Go module SDK, base container images) are dependencies, not connections to reference-platform systems.
