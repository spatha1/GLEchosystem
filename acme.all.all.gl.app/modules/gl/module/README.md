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
| US-014 | (compiled, not configured) | **Required-field manifest** (contract C2). `model.BuildEventGraph` traces every configured behaviour of an event back to the raw values it reads: rule filters, line amounts, conditions and entities, derived formulas, lookup-derived dimensions, the date policy, assignment matches, the subledger keys and dimensions accounts require, identity and business keys. `model.CompileManifest` lists, per event, the **SOURCE** fields some behaviour reads (with `requiredBy`, `mandatory` when the engine refuses the event without it, `keyRole`, `LIST` cardinality for 1:N items) and the **DERIVED** values GL computes (`GROSS`, `BANKING_COMPANY`, `BOOKING_CLASS`) with `derivedFrom`, never requested from a source. Fields declared but read by nothing are left out and reported as unused. The digest covers only what is asked for, so cosmetic changes keep mappings valid; `DiffManifests` says which fields were added, dropped or changed and which need a mapping added, reviewed or dropped. `GET …/versions/{n}/manifest` (with the explanation: every use and the chain through formulas and lookups), `GET …/versions/{n}/manifest/diff?against=m`, `GET …/effective/manifest?date=` (what collection asks for) |

## Insurance catalogs (proposed standard)

Two global, read-only catalogs give a business analyst general-insurance (property and casualty) starting points; a client adopts, overrides or extends them in its own configuration, and nothing about an event is fixed in code. Both are **proposed** until the product owner approves the catalog (the open "OOTB catalog governance" decision).

- **Measures** (`model/ootb_measures.json`, `gl.measures.ootb/v2`, `GET /catalog/measures`): premium; taxes and levies (GST/IPT, stamp duty, statutory levies); fees; commission and tax on commission; invoiced, paid, allocated, refunded, dishonoured and written-off amounts; earned and unearned premium; claim reserves, payments, handling expenses, excess and recoveries; reinsurance premium ceded, reinsurance commission and recoveries. The 12 v1 measures are unchanged.
- **Events** (`model/ootb_events.json`, `gl.events.ootb/v1`, `GET /catalog/events`): policy lifecycle (new business, renewal, endorsement, cancellation, reinstatement, premium posted), billing and cash (invoice, payment received, dishonoured, refund, write-off), distribution (commission paid), claims (notified, reserve changed, payment, recovery), reinsurance (premium ceded, recovery) and period end (premium earned). Each template has its purpose, typical source kind, economic key, measures, dimensions and lists, and the business keys it uses (with their scopes and links). A test applies every template to a configuration and validates it.
- Source kinds gain `REINSURANCE`. In the studio, *Add business event* starts from a template; saving declares the business keys and standard dimensions the event uses that the configuration lacks (closed dimensions then need the client's values, which the live check asks for).

## Configuration studio API

Read models over one stored version, computed from its content on every read, so the studio shows exactly what the engine does with that version. Nothing here is stored.

| Endpoint | What it gives |
|---|---|
| `GET …/versions/{n}/events` | Every event type: sources, measures, lists, rules and lines, required fields, scenarios, findings |
| `GET …/versions/{n}/events/{eventType}` | One event explained: sources and authority, measures and formulas, dimensions and lookups, keys and their roles, date policy and calendar, posting rules with each line's class and candidate accounts (in precedence order), scenarios, the dependency graph (`model.BuildEventGraph`), its manifest and explanation, and the findings that concern it |
| `POST …/versions/{n}/scenarios/run` | The version's `scenarios` as posting previews, checked against their expected journals or refusals (`PASSED`, `FAILED`, `RAN`, `REFUSED`) |
| `GET …/versions/{n}/impact[?against=m]` | Against the version it is based on (or `m`): the items changed (rule lines named), the events they affect and through which items, the manifest diff and mappings to revisit, every scenario's journal before and after, and the downstream stages |
| `POST …/versions/{n}/check` | Validates edited content without saving it and lists the items it changes (editors) |
| `GET …/versions/{n}/pipeline` | The version's state per stage: configured, validated, approved, required fields, mapped, collected, simulated, posted, subledger, reconciled, reported. Stages not built yet are `NOT_IMPLEMENTED` with their story |

Saving a draft (`PUT`) or creating a version from another (`basedOnVersion`) records in the audit trail which items changed, e.g. `changed postingRules PR_PREM_ADDED (lines/AR0_DR, lines/PW0_CR)`. Posting rules can be deactivated (`active: false`) and dated (`effectiveFrom`/`effectiveTo` on the book date) within a version. Plans carry every derived measure evaluated, with its lineage.

## AI-assisted configuration from a BRD (proposals only)

An editor uploads a business requirements document (Word `.docx`, text or Markdown, at most 1.5 MB), types the requirements in their own words, or both (the typed text follows the BRD under its own heading, and quotes from it are checked the same way), and the AI proposes the configuration one step at a time, each approved by the editor before the next: **1. business events → 2. measures → 3. formulas → 4. debit and credit lines with synthetic test cases (LHS = RHS)**. Mapping and data collection are separate (Data Workbench) and never pass through the AI.

- **Every proposal is checked.** Each item quotes the BRD sentences it relies on, and every quote is checked against the text; a quote not found is flagged. The engine validates the draft with the proposal exactly as saving would, runs the test cases (debits must equal credits per company and currency), and the AI repairs what the engine finds, at most 3 attempts. Anything still wrong is shown, never hidden.
- **Nothing is saved by the AI.** The editor approves a step and the studio saves it to the draft with a reason; the audit trail records `origin: {kind: AI_STEP, step, provider, model, brd}`. Validation, and approval by someone who did not edit the version, are unchanged.
- **Guardrails in code, not only in the prompt.** Account numbers and assignment rules are never proposed: a person enters them (gaps), and so are a closed dimension's values. Catalog events and measures come first; custom ones are flagged. An event that lists business keys always lists its economic key. Only drafts, only `CONFIG_EDITOR`. A trial run gives classes still without an account an in-memory placeholder so the balance can be checked; it is never saved.
- **Provider.** OpenAI chat completions in JSON mode, configured by a settings file named in `GL_AI_SETTINGS`, in the QA Workbench's format: `AI_PROVIDER=openai`, `OPENAI_API_KEY`, `OPENAI_MODEL` (default `gpt-4.1`), `OPENAI_BASE_URL` (HTTPS or this machine only), `AI_ENABLED`. The file is read on every request; the key is never logged, stored or sent to the browser. Without it, `GET /ai/status` says why and the studio works without AI.
- **Data sent to the provider:** the BRD text and the configuration (codes, names, formulas, rules). Never policy or customer data. Do not upload a BRD that contains personal data.
- **API:** `GET /ai/status`; `POST /configs/{id}/versions/{n}/ai/brd` (read the document; nothing goes to the AI); `POST /configs/{id}/versions/{n}/ai/steps/{events|measures|formulas|rules}`.
- **Measuring the AI:** `go run ./cmd/aieval -brd <BRD> -config <configuration.json> [-out report.json]` (with `GL_AI_SETTINGS`) runs the four steps against the real provider on a configuration's reference data, accepting every item, and reports per step the attempts, BRD quotes found, test cases balanced, gaps and problems; it passes only when no step has problems and every test case balances. It costs a provider call per step and attempt and writes to no database.
- **Sample:** `acme.all.all.gl.spec/_harness/fixtures/brd/motor-renewals-brd.md` and the same BRD as Word, `BRD-GL-MOTOR-001.docx` (synthetic). First real run (gpt-4.1, 21 November 2026): both events (RENEWAL, CANCELLATION), measures, two formulas and the rules found; 3 of 3 test cases balanced after one engine-driven repair; 4 account numbers left to a person; 51 of 53 BRD quotes matched word for word. The tests take it through all four steps with a stand-in for the provider and end with a configuration that validates once a person has entered the account numbers.

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

## UI (`ui/`, Svelte 5): the GL Configuration Studio

`<gl-app tenant="CLIENT_A">` is the module's screen, built by `vite build` into `ui/dist/entry.js` (the file the app build mounts). It calls only `/m/gl/*`, sends the tenant (`x-gl-tenant`) and never an identity: the app core authenticates the user and sets `x-mycel-user`. With the `hash-routing` attribute every view has a link and the back button works.

The studio is event-centric and follows the GL flow: client → configuration → version → business event → definition → rules → lines → required fields → mapping → simulation → journal. Everything it shows is read from the stored, versioned configuration through the API (nothing is hard-coded, including the sample events, which are the version's `scenarios`), and every change is saved to a draft through the API.

Layout: a side navigation lists the configurations and, inside a version, its views (the screens below), with the open event under *Business events*. It narrows to a 72px icon rail and becomes a drawer on a phone. A top bar shows the client, the breadcrumbs and a light/dark theme switch; by default the theme follows the system. The rail and theme choices are kept in the viewer's browser only. Each page opens with an eyebrow (where you are), a title and its purpose. The studio draws with the Inter font, which the host page loads (a shadow root cannot), falling back to the system font. A host page that keeps a bar fixed above the studio sets `--gl-top-offset` on `<gl-app>` to that bar's height.

| Screen | What it shows and does |
|---|---|
| **Configurations** | The client's configurations and versions: status, effective window, based-on version, editors, approver, digest; which version is in force |
| **Version** | Header with maker-checker actions (validate, approve with reason, retire, *new draft from this version*) and the **pipeline**: configured, validated, approved, required fields, mapped, collected, simulated, posted, subledger, reconciled, reports — stages not built yet say so, with their story |
| **Business events** | One card per event: source, measures, lists, rules, lines, source fields, calculated values, scenarios, findings |
| **Event · Definition** | "How this event is processed" chain; source and authority; business keys and the economic key; date policy and calendar; dimensions (carried or derived by lookup, normalisations, used or not); measures; formulas (editable in a draft, checked live); lists; lookups; findings |
| **Event · Lineage map** | The event's dependency graph (`model.BuildEventGraph`) in five columns — source fields, calculated by GL, rules and controls, classes, accounts — with connectors; selecting any element lights up what it reads and what reads it, and lists each use in words |
| **Event · Posting rules / Rule** | Each rule's applicability (filter, rank, window, other rules) and every debit and credit line explained: class (nature, normal side, statement), amount formula (with the formula behind a calculated measure), per-item expansion, condition, legal entity (and the lookup behind it), the account choice in precedence order. In a draft: edit, add, copy, deactivate/activate, delete, each saved with a reason; the editor checks the edit live (`POST …/check`) and lists what will change. In an approved version: *change this rule in a new draft* |
| **Configure from a BRD** | Upload a BRD and/or type the requirements in your own words, then four AI steps (events, measures, formulas, debit/credit lines with test cases), each unlocked by approving the one before. A proposal shows each item's BRD quotes (flagged when not found in the text), the guardrail notes, the AI's questions, the engine's remaining problems, LHS = RHS per rule and the synthetic test cases' journals. Nothing is pre-chosen: the editor picks events or items, types the account numbers the AI may not invent, sees the live check of what saving would do, and approves with a reason (saved with origin AI_STEP). Says plainly when AI is not set up |
| **Required fields** | The C2 manifest: per event, the source fields with type, mandatory, key role, authoritative source, canonical event path and every reason (with the chain through formulas and lookups); calculated values with their inputs; fields declared but unused; manifest digest; download |
| **Data Workbench mapping** | Not built yet (US-015/US-016), and said so: the fields that will be sent, with empty proposal, confidence, transformation, approval and version columns and the configured value normalisations |
| **Simulation** | Pick a scenario of the version (or a blank event) — the form is generated from the event's definition — and run it: configuration, matched rule and rules considered, formula calculations with values, derived dimensions, book date and period, the journal as one balanced entry per legal entity and currency, the scenario's expectation, and for any line its lineage: source record → source fields read (value, canonical field, source, mapping) → calculation → rule line → account assignment → journal leg. Run all scenarios; save the inputs and result as a scenario in a draft |
| **Change impact** | Against the version it is based on (or any earlier one): changed items (rule lines named), events affected and why, required-field and mapping changes, every scenario's journal before and after, downstream stages |
| **Chart of accounts** | Classes, accounts (with what each requires on every line) and assignment rules |
| **Findings and history** | Live validation findings, maker-checker, and the audit trail with reasons and, for every save, which items changed |

Local run (development only):

```
# 1. the dev adapter, with roles for Client A and Client B (loopback only; an ordinary database role)
R=""; for t in CLIENT_A CLIENT_B; do R="$R$t:ba@example.com:CONFIG_EDITOR;$t:reviewer@example.com:CONFIG_APPROVER;$t:lead@example.com:CONFIG_EDITOR|CONFIG_APPROVER;$t:viewer@example.com:VIEWER;"; done
GL_DEV_SEED_ROLES="${R%;}" DATABASE_URL=postgres://<role>:<password>@127.0.0.1:55432/gl_dev go run ./service/cmd/devhttp
# 2. the UI: seed GL-TEST-001 for Client A (v1, approved by a second person) through the API, then open http://127.0.0.1:5174
cd ui && npm install && npm run seed:dev && npm run dev
```

The dev harness (`index.html`, `dev/`) chooses the user and the client; its proxy turns the user into `x-mycel-user`, which only the dev adapter trusts. `npm test` runs the UI tests (vitest, jsdom; event views and plans recorded from the API in `src/test/`), and `npm run check` runs the type check.
