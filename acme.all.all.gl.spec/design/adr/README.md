# Architecture decision records

| ADR | Decision | Status | Decided by |
|---|---|---|---|
| [ADR-0001](ADR-0001-stack-and-structure.md) | Technology stack and repository structure | **Accepted** | Product owner, 2026-11-19 |
| [ADR-0002](ADR-0002-data-workbench-existing-apis.md) | Data Workbench integration through its existing APIs only | **Accepted** | Product owner, 2026-11-19 |
| [ADR-0003](ADR-0003-configuration-not-modules.md) | New scenarios are configuration, not modules | **Accepted** | Product owner (backlog guardrail) |
| [ADR-0004](ADR-0004-no-reference-platform-connections.md) | No connection to reference-platform databases or APIs | **Accepted** | Product owner, 2026-11-19 |
| [ADR-0005](ADR-0005-money-and-determinism.md) | Exact money arithmetic and deterministic evaluation | **Proposed**; needs product-owner confirmation | Implementation team |

## Still open (product owner)

| Item | Note |
|---|---|
| Final product and repository naming | `acme.all.all.gl` is provisional |
| Identity provider for GL users and service accounts | US-004 implements roles behind an identity seam until decided |
| Hosting, CI/CD and environments | Required before US-043/US-044 |
