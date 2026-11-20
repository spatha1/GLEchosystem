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
ui/                            <gl-app> custom element (Svelte 5): configurations, posting preview
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
| US-006 | `baseMeasures` | A global, read-only **OOTB catalog** (`model/ootb_measures.json`, `gl.measures.ootb/v1`: premium, tax, fees, receivables, cash, commissions, claims, earnings — each with sign, grain, currency, definition, source scope). A client overrides only descriptive attributes (name, definition, sign, sourceScope) of an OOTB measure, or defines a `custom` measure; changing what a measure is (dataType, currency, grain, category) is `OVERRIDE_NOT_ALLOWED`. Unknown, shadowing or duplicate measures block. Measures carry no account or side. `GET /catalog/measures` (global) and `GET /configs/{id}/versions/{v}/measures` (resolved, with origin OOTB / OVERRIDDEN / CUSTOM) |
| US-007 | `derivedMeasures` | Formulas in GL's bounded language (`service/formula`): exact rational arithmetic, `IF`, `COALESCE`, `ROUND` (`HALF_UP` / `HALF_EVEN`), `ABS`, `MIN`, `MAX`, `ISNULL`, `SUM(LIST.FIELD)`, `COUNT(LIST)`; no code, no SQL, size limits. Statically checked per event (`UNKNOWN_IDENTIFIER`, `TYPE_MISMATCH`, `RESULT_TYPE_MISMATCH`, `CYCLE` with its path). At runtime `DIVIDE_BY_ZERO`, `CURRENCY_MISMATCH`, `NULL_CONDITION` are explicit. Every result carries lineage: formula, formula digest, config id/version/digest, inputs read, roundings. `POST /configs/{id}/versions/{v}/evaluate` is the read-only simulator |
| US-008 | `dimensions`, `lookups`, event `dimensions` | A global OOTB **dimension** catalog (`gl.dimensions.ootb/v1`: country, company, LOB, product, transaction type, currency, bank account, banking company, booking class). Clients give canonical values and per-source **normalisations** (optionally case-insensitive); `model.Normalize` maps a raw value or returns `UNKNOWN_CODE` (never a default). **Lookups** (e.g. `BANK_ACCOUNT → BANKING_COMPANY`, effective-dated entries) live in the versioned configuration and every resolution records lookup, key and config version. Filters use `==`, `IN(…)`, `LIKE(x, 'MOTOR_*')`; comparing a closed dimension with a value it lacks (`LOB == 'MOTR'`) is `UNKNOWN_DIMENSION_VALUE`. The simulator returns resolved dimensions |
| US-009 | `calendars`, `datePolicies` | Calendars (IANA time zone, weekend, holidays; tz database embedded). A date policy per event type (or a default) sets the **book date** (transaction, effective or recorded instant converted to the calendar date), an optional roll to the next business day, the fiscal year start, **advance classification** (`BOOKING_CLASS` A when effective after the book period, else C), which date selects lookups, and the **close rule** (Nth business day after month end) with the late policy (`NEXT_OPEN_PERIOD` or `REJECT` → `CLOSED_PERIOD`). `model.DecidePeriod` is pure and returns its steps; book, effective and transaction dates stay separate. Formulas read `TRANSACTION_DATE`, `EFFECTIVE_DATE`, `BOOK_DATE`, `ACCOUNTING_PERIOD`, `BOOKING_CLASS` |
| US-010 | `keyProfiles`, event `keys` | Business keys with a format (anchored pattern, optional case folding), a **scope** (TERM_NO is unique only within its POLICY_NO; graph values read `P1/1`) and **ownership links** (INVOICE_NO → POLICY_NO, ALLOCATION_ID → RECEIPT_NO and INVOICE_NO; `MANY_TO_ONE`, `ONE_TO_ONE`, `MANY_TO_MANY`). `model.KeyGraph` is per tenant: dedup identity is source system + source event id + version (an identical resend is `UNCHANGED`, a later version replaces what the earlier one asserted); natural grain is the economic key plus each list's item key. Missing, malformed, undeclared or conflicting keys and duplicate events become exceptions (`MISSING_KEY`, `INVALID_KEY`, `UNKNOWN_CODE`, `KEY_CONFLICT`, `DUPLICATE_EVENT`, `TENANT_MISMATCH`) and change nothing. `Rebuild` follows ownership down (policy → invoices → allocations) and returns every event to reprocess; `Related` is the whole connected component. `POST …/keygraph` runs it read-only |
| US-011 | `accountingClasses`, `accounts` | **Accounting classes** are semantic roles (AR0 policy receivable, AR1 invoice receivable, PW0 premium written, TX0 tax payable, PR0 cash, IC0/IC1 intercompany, WO write-off) with a nature (`ASSET`, `LIABILITY`, `EQUITY`, `INCOME`, `EXPENSE`), normal side and statement. **Accounts** are the client's own ledger codes, each serving one class, per ledger (default `PRIMARY`), active over [`activeFrom`, `activeTo`), optionally limited to entities (COMPANY values) and currencies, and optionally requiring a subledger key (a US-010 key profile) and dimensions on every line. `model.CheckPosting` is the posting gate: `MISSING_ACCOUNT`, `INACTIVE_ACCOUNT`, `CLASS_MISMATCH`, `ENTITY_NOT_ALLOWED`, `CURRENCY_NOT_ALLOWED`, `MISSING_SUBLEDGER_KEY`, `MISSING_DIMENSION`; never a fallback account. `model.CheckCarrier` checks before intake that an event (or its list items) carries what a class's accounts require. `GET …/accounts` is the COA lookup; `POST …/accounts/check` previews the gate |
| US-012 | `postingRules` | A **posting rule** turns one event into journal legs, as data: event type, optional `filter` (BOOL formula), `rank`, and **line templates** (accounting class, side, `amount` formula, optional `expandBy` list for one leg per item reading the item's measures, `entity` override such as `BANKING_COMPANY`, `when` condition). Rules compile to a checked plan at validation (formulas type-checked, classes known, at least one debit and one credit, every leg's class satisfiable by the event's keys and dimensions, `AMBIGUOUS_RULE` for identical same-rank rules, `MISSING_RULE` warnings). `model.PlanPosting` picks the lowest-ranked matching rule (a tie is `AMBIGUOUS_RULE`, none is `MISSING_RULE`), produces legs in template then item order with lineage, skips zero or conditioned-out legs with the reason, flips the side of a negative amount, refuses `NULL_AMOUNT`, `UNROUNDED_AMOUNT`, `MISSING_ENTITY`, and checks balance per entity and currency (`UNBALANCED`, with the plan). `POST …/plan` is the read-only posting preview |
| US-013 | `accountAssignmentRules` | **Account assignment**: class + dimensions → account. A rule matches dimensions (and `ENTITY`, `CURRENCY`, `BOOKING_CLASS`) by exact value or `*` (any present value); unnamed dimensions do not matter. Precedence: `priority`, then exact matches, then wildcards, then the later `effectiveFrom` (a dated override). Equal-precedence rules that could match the same line on the same day are `AMBIGUOUS_ASSIGNMENT` at validation and at runtime, never a random pick. Validation also checks accounts exist and serve the class, values exist, windows are valid, and warns `MISSING_ASSIGNMENT` and `ASSIGNMENT_DIMENSION_NOT_CARRIED` (e.g. an invoice event without LOB would clear motor AR0 to the default account). `model.PreviewPosting` assigns every leg on the book date, records the rule, configuration version, why and every rule considered, then applies the US-011 gate; a refused leg is annotated and never redirected |

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

## UI (`ui/`, Svelte 5)

`<gl-app tenant="DEMO">` is the module's screen, built by `vite build` into `ui/dist/entry.js` (the file the app build mounts). It calls only `/m/gl/*`, sends the tenant (`x-gl-tenant`) and never an identity: the app core authenticates the user and sets `x-mycel-user`.

| Screen | What it does |
|---|---|
| **Configurations** | Lists configurations and versions (status, effective window, who changed and approved, digest). Each version shows what it configures, its validation findings and the audit trail with reasons. Validate, approve and retire follow the user's roles. Approve is disabled, with the reason, for anyone who changed the version (maker-checker). Approve and retire ask for a reason. The server enforces all of it. |
| **Posting preview** | Runs `POST …/plan` for a GL-TEST-001 sample event (or an edited one) against any version. It shows the rule chosen and the rules considered, and each journal leg with class, account, entity, debit or credit, and keys. **Why** opens a leg's amount lineage and the assignment rule (with configuration version) and rules considered. It also shows the balance per entity and currency, skipped legs with reasons, and refusals with the refused leg highlighted. Read-only. |

Local run (development only):

```
# 1. the dev adapter, with roles for tenant DEMO (loopback only; an ordinary database role)
GL_DEV_SEED_ROLES="DEMO:ba@example.com:CONFIG_EDITOR;DEMO:reviewer@example.com:CONFIG_APPROVER;DEMO:lead@example.com:CONFIG_EDITOR|CONFIG_APPROVER;DEMO:viewer@example.com:VIEWER" DATABASE_URL=postgres://<role>:<password>@127.0.0.1:55432/gl_dev go run ./service/cmd/devhttp
# 2. the UI: seed the GL-TEST-001 demo configuration through the API, then open http://127.0.0.1:5174
cd ui && npm install && npm run seed:dev && npm run dev
```

The dev harness (`index.html`, `dev/`) chooses the user; its proxy turns that choice into `x-mycel-user`, which only the dev adapter trusts. `npm test` runs the UI tests (vitest, jsdom), and `npm run check` runs the type check.
