# 02 — Canonical contracts

All contracts are JSON. Modules agree on **JSON shape**, not on shared code. Each contract has a JSON Schema in [schemas/](schemas/) and is exercised by the walk-through in [`_harness/contracts`](../../_harness/contracts/README.md).

## Common rules

| Rule | Detail |
|---|---|
| Money | Decimal **string**: `^-?[0-9]+(\.[0-9]+)?$`. Scale must not exceed the currency's minor units. Never a JSON number, never binary floating point |
| Dates | `YYYY-MM-DD` for business dates; RFC 3339 UTC for instants |
| Identity | Every record carries `tenantId`. Every runtime record carries the `configVersion` and (when sourced) the `mappingVersion` it was produced under |
| Codes | Upper snake case (`PREM_ADDED`, `POLICY_NO`). Codes are configuration, not enums baked into code |
| Immutability | Approved configuration and mapping versions, posted journals and imported source journals are never updated in place. A change is a new version or a new linked entry |
| Digests | `sha256:<hex>` over the canonical JSON content (sorted keys, no insignificant whitespace), **excluding** approval metadata, so approving a version does not change its digest |

## C1 — Configuration version ([schema](schemas/config-version.schema.json))

The executable form of a client's requirements. Envelope: `tenantId`, `configId`, `version`, `status` (`draft | validated | approved | retired`), `effectiveFrom`, `effectiveTo`, `schemaVersion`, `contentDigest`, `approval`, `content`.

`content` sections (each detailed by its story): `sourceSystems`, `sourceAuthority` (US-005), `events` (US-005), `baseMeasures` (US-006), `derivedMeasures` (US-007), `dimensions` and `lookups` (US-008), `datePolicies` and `calendars` (US-009), `keyProfiles` (US-010), `accountingClasses` and `accounts` (US-011), `postingRules` (US-012), `accountAssignmentRules` (US-013), `reconciliationEquations` (US-028), `outputProfiles` (US-039).

Invariants:
- `approval.approvedBy ≠ approval.submittedBy` (maker-checker).
- Approved versions are immutable.
- At most one approved version per (tenant, configId) is effective on any date.

## C2 — Required-field manifest ([schema](schemas/field-manifest.schema.json))

Compiled from one configuration version. It never names physical tables or columns. Per event type it lists the fields the configuration needs:

| Field | Meaning |
|---|---|
| `fieldCode`, `definition`, `dataType` | Semantic identity and meaning |
| `kind` | `SOURCE`: must be collected. `DERIVED`: computed from other fields, so its inputs are requested instead. `LEDGER_STATE`: read from GL state, e.g. `OPEN_BALANCE`; never requested from a source |
| `cardinality` | `ONE` or `LIST` (with `listCode`, e.g. `ALLOCATION_LIST`) |
| `mandatory`, `keyRole` | Whether the event is unusable without it; whether it is a business key, linkage key or dedup key |
| `requiredBy` | The configuration elements (rule, formula, filter, key, date policy) that need it. This is the lineage for "why is this field required?" |

The manifest has its own `manifestDigest`. Any change to the approved configuration that changes the manifest changes the digest, which makes every mapping bound to the old digest **stale**.

How the `gl` module compiles it (US-014):

- **One dependency graph per event.** Every configured behaviour is traced back to the raw values it reads: posting-rule filters, line amounts, conditions and legal entities, derived-measure formulas, lookup-derived dimensions, the date policy, account-assignment matches, the subledger keys and dimensions accounts require, and identity and business keys. A source field is in the manifest only if something reads it; a field the event declares but nothing reads is left out (and reported as unused).
- **Derived values are listed, never requested.** Derived measures (`GROSS`), lookup-derived dimensions (`BANKING_COMPANY`) and date-policy values (`BOOKING_CLASS`, `BOOK_DATE`) are `DERIVED` with `derivedFrom`; their inputs are the `SOURCE` fields. `LEDGER_STATE` (e.g. `OPEN_BALANCE`) is produced once the subledger exists (US-022/US-024).
- **`requiredBy`** names every element on the way: the formula or lookup in between and the rule line, filter, key profile, date policy, assignment rule or account that finally needs the value (`POSTING_RULE` refs are `RULE/LINE`).
- **`mandatory`** is true when the engine refuses the event without the value: identity, currency, economic and business keys, line amounts and legal entities, list expansion, subledger keys and account dimensions, the date policy's dates, and assignment matches every rule of the class names. Filters, line conditions and matches a fallback rule does not need are required but not mandatory.
- **Digest.** It covers exactly what the manifest says, not the configuration version. A cosmetic change (a name, a description) keeps it. A change that reads the same fields along a new path changes `requiredBy` and so the digest, but asks for no remapping; the diff (`GET …/manifest/diff?against=`) says per field whether a mapping must be added, reviewed or dropped.
- **Explanation.** Alongside the manifest, the API returns every use of every field in words, with the chain through formulas and lookups (e.g. PREMIUM → `GROSS = PREMIUM + TAX` → amount of line `AR0_DR`), where the canonical event (C4) carries it, the source systems authoritative for it, and its constraints (key pattern, closed values, source normalisations).

The GL-TEST-001 manifest is pinned as [`_harness/fixtures/GL-TEST-001.manifest.json`](../../_harness/fixtures/GL-TEST-001.manifest.json): the engine's tests regenerate and compare it, and the contract tests check it satisfies C2.

## C3 — Mapping version ([schema](schemas/mapping-version.schema.json))

Binds manifest fields to source fields for one tenant and source system. It records `configId`, `configVersion`, `manifestDigest` and the Data Workbench connection used. Each field mapping holds the source object, column, optional transform, join path, cardinality, Data Workbench confidence and evidence, and `origin` (`DWB_RANK | DWB_SUGGEST | MANUAL`).

- Status: `suggested → reviewed → approved | rejected`; `stale` when the manifest digest no longer matches.
- Only `approved`, non-stale versions can be compiled into collection queries.
- `approval.approvedBy ≠ approval.proposedBy`.

## C4 — Financial event ([schema](schemas/financial-event.schema.json))

The canonical runtime input for `RAW_EVENT` intake: `tenantId`, `sourceSystem`, `sourceEventId`, `sourceEventVersion`, `eventType`, `economicEventKey`, `company`, `currency`, `dates` (`transactionDate`, `effectiveDate`, `recordedAt`), `keys`, `measures` (decimal strings), `dimensions`, `lists` (1:N children such as `ALLOCATION_LIST`, each with its own identity), `sourceRowIds`, `mappingId`, `mappingVersion`, `collectedAt`.

- `economicEventKey` identifies the economic effect independently of source and intake mode. It is the duplicate guard across sources (01 §3).
- List children carry their own amounts. A parent measure is never multiplied by list cardinality.

## C5 — Source journal ([schema](schemas/source-journal.schema.json))

The canonical input for `SOURCE_JOURNAL` intake: an already-balanced journal from a source system. It has `sourceJournalId`, `sourceJournalVersion`, `economicEventKey`, `company`, `currency`, `bookingDate`, and `lines` (`sourceLineId`, `accountRef`, `side`, `amount`, `dimensions`).

GL validates balance per company and currency, maps `accountRef` to the client COA, and imports the lines **as they are**: same grouping, same line identities, **no synthetic counter-leg**.

## C6 — Journal entry ([schema](schemas/journal-entry.schema.json))

The posted ledger record. **One journal entry per (posting group, legal entity, currency)**. A business event that spans entities (e.g. a receipt banked by another company) produces several entries linked by one `groupId`, each balanced on its own.

| Field | Content |
|---|---|
| Header | `journalId`, `groupId`, `tenantId`, `economicEventKey`, `legalEntity`, `currency`, `intakeMode`, `bookingDate`, `effectiveDate`, `accountingPeriod`, `status` (`posted | reversed`), `configId`, `configVersion`, `mappingVersion`, `reversalOf`, `correctionOf` |
| Line | `lineId` (stable across retries), `account`, `accountingClass`, `side` (`D | C`), positive `amount`, `dimensions`, `subledgerKey`, `sourceRowIds`, `ruleId`, `ruleVersion`, `explanation` |

Invariant: Σ debits = Σ credits exactly, per entry. An entry that does not balance is never posted.

## C7 — Exception case ([schema](schemas/exception-case.schema.json))

Every condition that would otherwise lose or fabricate data becomes a case: `MISSING_MAPPING`, `STALE_MAPPING`, `UNKNOWN_CODE`, `MISSING_KEY`, `KEY_CONFLICT`, `INVALID_KEY` (a key that fails its configured format, or one the event type does not declare), `DUPLICATE_EVENT`, `SOURCE_AUTHORITY_CONFLICT`, `AMBIGUOUS_RULE`, `MISSING_ACCOUNT`, `FORMULA_ERROR`, `UNBALANCED`, `CLOSED_PERIOD`, `RECONCILIATION_BREAK`, `TENANT_MISMATCH` (an event naming another tenant; it is refused without being read, and nothing of it is echoed into the case). A case carries source references, the configuration and mapping versions, a detail payload and a status (`open | assigned | resolved | reprocessed | closed`).

A technical exception **never** creates a suspense posting. Suspense postings exist only through an approved posting rule.
