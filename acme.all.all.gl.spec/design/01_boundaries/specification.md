# 01 — Boundaries and responsibilities

## 1. Priority order (binding on planning)

1. **Versioned configuration and deterministic rule execution** (module `gl`).
2. **Requirements-driven data identification and collection** (module `glsource`, using the Data Workbench).
3. Everything else consumes 1 and 2: reconciliation views, reports, TRF files, finance APIs, dashboards, UI polish and infrastructure hardening. None of these may be built ahead of, or instead of, 1 and 2. None may contain business rules of its own.

## 2. Unit and module responsibilities

| Owner | Owns | Must not |
|---|---|---|
| `gl` module | Configuration versions (tenant-scoped, effective-dated, immutable once approved) and their audit; validation; the formula DSL; dimension/filter evaluation; calendars and periods; business keys; COA and account assignment; posting rules; the **required-field manifest compiler**; event intake and lifecycle; journal generation and atomic posting; subledger; reconciliation equations; simulation | Call any source system directly; call the Data Workbench; contain client- or vendor-specific branches |
| `glsource` module | Turning a manifest into Data Workbench requests; storing proposals as **suggested**; mapping review, validation and approval (`mappingVersion` bound to `configVersion` + manifest digest); compiling approved mappings into read-only queries; collection with watermarks; normalisation into **canonical financial events** and **source journals**; collection lineage | Evaluate accounting rules; post journals; approve anything automatically; send AI-generated SQL for execution |
| `acme.all.all.gl.wfc` | Orchestrating long-running, resumable, idempotent runs (collection batches, posting batches, reclassification, period close, replay) by calling module endpoints | Contain business rules: workflows only sequence module calls and remain deterministic |
| Data Workbench (existing, external) | Source metadata, embeddings, candidate ranking, AI suggestions, read-only query execution against registered source connections | Be modified for GL ([ADR-0002](../adr/ADR-0002-data-workbench-existing-apis.md)) |
| Reference platform (Siddhi) | — | Be connected to in any way ([ADR-0004](../adr/ADR-0004-no-reference-platform-connections.md)) |

## 3. Source authority (event × measure)

Configuration declares, per **event type and measure**, which source system is authoritative for **posting**, and in which intake mode:

```json
{ "eventType": "PREM_ADDED",       "measure": "PREMIUM", "sourceSystem": "POLICY",  "intakeMode": "RAW_EVENT" }
{ "eventType": "PAYMENT_RECEIVED", "measure": "PAY_AMT", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT" }
```

Rules:

- Exactly one authority per (event type, measure, effective period). Zero authorities or more than one is a **configuration validation error**, never resolved at runtime.
- An event from a non-authoritative source may be **stored for reconciliation** but **never posts**.
- Every economic event has a stable `economicEventKey`. Posting is idempotent on (tenant, economicEventKey, posting fingerprint), so a second arrival of the same economic event, from the same or another source or intake mode, cannot post twice.

## 4. Two intake modes

| Mode | Input | What GL does | What GL never does |
|---|---|---|---|
| `RAW_EVENT` | A canonical financial event with measures, keys, dates and lists | Applies approved posting rules to produce journal lines | — |
| `SOURCE_JOURNAL` | An already-balanced source journal (e.g. a billing system's GL feed) | Validates balance per entity and currency; maps account references to the client COA; keeps the original grouping, line identifiers and lineage | **Synthesise a counter-leg**; re-derive amounts from raw measures |

If configuration makes `SOURCE_JOURNAL` authoritative for an event type, raw events of that type are **reconciliation-only** for posting purposes. The authority check in §3 guarantees the same economic effect cannot be booked by both modes.

## 5. Design time vs runtime boundary

- Runtime **reads only approved, effective** configuration and mapping versions. The version is selected by effective date and the posting period, never chosen by a caller.
- Every runtime output (event, journal, exception, reconciliation result) records `configId`, `configVersion`, `mappingId` and `mappingVersion` ([02](../02_canonical_contracts/specification.md)).
- A runtime exception may lead to a **new configuration version**. Runtime never edits configuration.

## 6. Non-goals until a demonstrated engine limitation

- No per-client, per-event or per-scenario modules ([ADR-0003](../adr/ADR-0003-configuration-not-modules.md)).
- No vendor-specific (DCT, Guidewire, …) logic in the engine. Vendor differences live in configuration and mappings.
- No autonomous AI approval, posting or execution of generated SQL.
