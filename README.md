# GL Ecosystem

An independent, **configuration-driven insurance General Ledger** ecosystem.

A client states its accounting and reporting requirements. The implementation team expresses them as **versioned, approved configuration**: events, source authority, measures, formulas, dimensions, dates, keys, chart of accounts, posting rules and reconciliation equations. A deterministic engine then:

1. derives the **required source fields** from that configuration;
2. uses the existing **Data Workbench / AI Data Agent** (through its existing APIs) to discover and propose source mappings, which a human approves;
3. collects the insurance data, normalises it into canonical financial events, and posts **balanced journals** and a **subledger**;
4. reconciles, and produces reports and files.

New client scenarios are **new configuration, not new code**. AI proposes; it never approves or posts.

## Status

Built story by story from the product backlog (US-001 … US-044). Each story is delivered as its own pull request with tests and evidence, independently reviewed, and approved by the product owner before merge.

| Decision | Status |
|---|---|
| Technology stack | Go services, Svelte 5 UI, PostgreSQL 16, Temporal (approved by the product owner) |
| Structure | Product units `.app` / `.wfc` / `.spec` with the Mycel module layout (approved) |
| Data Workbench integration | Existing Data Workbench APIs only |
| Repository naming | Provisional, to be confirmed by the product owner |
