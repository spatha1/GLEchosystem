# Contract walk-through (US-002 evidence)

Runs the **premium, invoice and payment paths** of GL-TEST-001 (E1–E3) through the canonical contracts in [design/02_canonical_contracts](../../design/02_canonical_contracts/specification.md), for two client setups:

- **Setup A** (`authority.raw-events.json`): premium from Policy, invoices and payments from Billing, all as raw events.
- **Setup B** (`authority.source-journal.json`): payments posted from Billing's own pre-balanced GL feed.

This is not the GL engine. It checks that the contracts can carry every path, and it states the invariants every implementation must keep. Standard library only; no service, database or network.

```sh
cd acme.all.all.gl.spec/_harness/contracts
go test -count=1 -v ./...
```

| Test | Proves |
|---|---|
| `TestSchemasParseAndPinTheirContractVersion` | All 7 JSON Schemas parse and pin their contract version |
| `TestEventsSatisfyTheEventContract` / `…RejectsBadInput` | C4 instances are valid. Float-looking money, excess decimals, missing identity or lineage, duplicate list items and unknown fields are rejected |
| `TestSourceAuthorityIsUnambiguousPerEventAndMeasure` | Authority is per (event, measure); a second authority is a configuration error |
| `TestOverlappingSourcesPostEachEconomicEventOnce` | The same premium from Policy and Billing posts once, from the authoritative source |
| `TestWalkPremiumPath` | E1 → 3 balanced lines; AR0 = PREMIUM + TAX |
| `TestWalkInvoicePath` | E2 → one DR/CR pair per invoice; list totals tie to the event |
| `TestWalkPaymentPath` | E3 → two entity entries (C09 bank, C01 policy), each balanced; intercompany nets to zero; allocations reduce each invoice; PAY_AMT is never multiplied |
| `TestSourceJournalImportAddsNoCounterLegAndPostsOnce` | Setup B imports Billing journals line for line (no counter-leg); the raw payment event does not also post; unbalanced or unmapped journals are refused |
| `TestGLTest001FixtureIsConsistent` | The fixture's 7 groups / 22 lines add up |
