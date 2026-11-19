# ADR-0002 — Data Workbench integration through its existing APIs only

**Status:** Accepted. **Decided by:** product owner, 2026-11-19 ("Existing DWB APIs only"; "Use data workbench apis").

## Decision

GL reuses the Data Workbench for source metadata, candidate ranking, AI mapping suggestions and read-only query execution, **through its existing HTTP APIs** ([03_data_workbench_integration](../03_data_workbench_integration/specification.md)). No change is made to the Data Workbench for GL. GL does not rebuild a metadata catalog, an embedding store or a query generator.

## Consequences

- The required-field manifest is presented to the Data Workbench as a JSON-Schema target, the existing entry point for externally defined field lists.
- Mapping approval, its binding to `configVersion` and the manifest digest, and maker-checker are **GL's** responsibility (module `glsource`).
- Collection queries are compiled by GL from approved mappings. Data Workbench-side guards are defence in depth, not the only control.
- Data Workbench API changes surface as failing `glsource` contract tests.
