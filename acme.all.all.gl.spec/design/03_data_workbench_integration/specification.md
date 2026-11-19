# 03 — Data Workbench integration (existing APIs only)

The Data Workbench (DWB) is the existing platform for source metadata, embeddings, AI-assisted mapping and read-only query execution. GL uses it **only through its existing HTTP APIs** and changes nothing in it ([ADR-0002](../adr/ADR-0002-data-workbench-existing-apis.md)). Only the `glsource` module talks to the DWB. All DWB paths are under `/api`.

## 1. APIs used

| Step | DWB API | Request | Response used by GL |
|---|---|---|---|
| Authenticate | `POST /api/auth/login` | `{username, password}` of a DWB **service account** | `access_token` (Bearer), `refresh_token`. If `requires_totp` is true, the account cannot be used for automation, and GL raises a configuration error instead of prompting |
| Rank candidates | `POST /api/connections/knowledge/rank` | `{source_id, target_id, target_paths[≤200], top_k}` | Per target path: candidates `{ref, table, column, score, method, definition}` and `warnings` |
| Register the manifest as a target | `POST /api/ai-migration/studio/json-targets` | `{project_id, name, schema, description}`, where `schema` is the manifest rendered as JSON Schema (§2) | The created target connection id |
| AI suggestions with joins | `POST /api/ai-migration/studio/mappings/{mapping_id}/suggest` | `{use_ai, context, use_business_context}` | Rows `{target_path, source_object, source_field, transforms, confidence, rationale, evidence, status:"suggested"}` and tables `{target_table, source_object, joins[{parent_column, child_column}]}` |
| Collect (read-only) | `POST /api/connections/{conn_id}/run` | `{query, limit}` with `limit ≤ 5000` | `{columns[], rows[{column: value}], total}` |

Every request carries `Authorization: Bearer <access_token>`, and the service account must be a member of the DWB project that owns the connections. Contract tests in `glsource` pin these shapes against a recorded fake. A change on the DWB side is detected as a failing contract test, not as silent misbehaviour.

## 2. Manifest → DWB target

Only `SOURCE` fields are sent. `DERIVED` and `LEDGER_STATE` fields are never asked for (C2), so the DWB is never asked to find, for example, `BOOKING_CLASS` or `OPEN_BALANCE` in a source table. The manifest for one event type becomes one JSON Schema object:

- each `ONE` field becomes a property (type from `dataType`; `format: date` for dates; the definition goes in `description`), listed in `required` when `mandatory`;
- each `LIST` (e.g. `ALLOCATION_LIST`) becomes an `array` of objects whose properties are the list's fields.

The target's name embeds `configId`, `configVersion` and the first 12 characters of the `manifestDigest`, so a stale target is recognisable.

## 3. Proposal → approval (GL side)

1. GL stores every DWB answer as a **suggested** mapping version (C3), with the DWB score or confidence, method, evidence and joins exactly as received.
2. A reviewer accepts, corrects or rejects each field. A different, authorised approver approves the version (maker ≠ checker).
3. Validation before approval:
   - every mandatory `SOURCE` field is mapped;
   - key fields are present;
   - list cardinality matches the join (`ONE_TO_MANY` for `ALLOCATION_LIST`);
   - a representative read (step 4 with a small limit) shows no row multiplication of parent measures.

## 4. Collection

- GL compiles the **approved** mapping into a SELECT. **AI-generated SQL is never executed.** Suggestions contribute only identifiers (object, column, join columns), and those are checked against an allowlist pattern and quoted.
- Literal values (watermarks, keys) are typed, validated and rendered by GL. Nothing is concatenated from free text.
- Paging is keyset-based on the configured update watermark (not the transaction date), plus a stable tie-breaker column, in pages of at most 5,000 rows. Each page is recorded with its watermark, row count and the query hash.
- Rows are normalised into canonical events (C4) or source journals (C5), keeping `sourceRowIds` for every contributing row.

## 5. Configuration and secrets

| Setting | Purpose |
|---|---|
| `DWB_BASE_URL` | The DWB origin |
| `DWB_USERNAME`, `DWB_PASSWORD` | Service-account credentials, injected from the secret store; never committed, never logged |
| `DWB_PROJECT_ID` | The DWB project that owns the source connections and the GL targets |

## 6. Failure handling

| Condition | GL behaviour |
|---|---|
| 401 | Re-authenticate once, then fail the run with a clear error |
| 403 / 404 on a connection | Configuration error (wrong project membership or connection id) |
| 422 from `run` | The compiled query was refused. Raise a case. Never relax the query |
| DWB unavailable | The run step fails and is retried by the worker with backoff. No partial page is committed |
