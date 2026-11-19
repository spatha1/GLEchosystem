# ADR-0005 — Exact money arithmetic and deterministic evaluation

**Status:** Proposed by the implementation team. Needs product-owner confirmation (rounding default).

## Decision

- **Representation.** Amounts travel as decimal strings (C-contracts) and are stored as `numeric` in PostgreSQL. In Go, amounts are exact rationals (`math/big.Rat`) during evaluation and are rounded **only** at explicitly configured points to the currency's minor units (ISO 4217 exponent; e.g. AUD and USD use 2).
- **Rounding.** The mode is configuration (`HALF_UP` or `HALF_EVEN`). The proposed default is `HALF_UP`. The rounding point and mode used are recorded in the calculation lineage.
- **Never float.** No `float32`/`float64` appears on any money path. Any JSON number appearing where money is expected is rejected.
- **Determinism.** Rule evaluation is a pure function of (approved configuration version, approved mapping version, canonical inputs, ledger state as-of). It reads no wall clock, no randomness and no network. Line and journal identifiers are derived from stable inputs, so a retry or replay produces identical IDs.

## Consequences

- Rule and journal snapshots can be compared byte for byte across runs.
- Divide-by-zero, currency mismatch and unrepresentable precision are explicit errors (US-007), never silent coercions.
