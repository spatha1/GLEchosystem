# ADR-0003 — New scenarios are configuration, not modules

**Status:** Accepted (product-owner guardrail in the backlog).

## Decision

A new client, event, lifecycle variant or reporting need is delivered as **new or changed configuration plus a regression test**, using the existing engine. No per-client, per-event or per-scenario module, code branch, SQL view or adapter logic is added.

## The only exception

If the generic engine demonstrably cannot express a behaviour:

1. Add a failing test that shows the limitation.
2. Propose the smallest generic engine primitive that would express it.
3. Obtain product-owner approval before building it, as its own narrowly scoped story.

## How it is checked

- Every story's tests include at least one case that would have needed code in a hardcoded design and here needs only configuration.
- Code review rejects identifiers such as client names, vendor names or fixed event codes in engine packages. Engine code reads codes from configuration.
