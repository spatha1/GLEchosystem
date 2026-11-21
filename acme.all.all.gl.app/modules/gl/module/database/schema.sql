-- Reference view of the gl module's schema (schema name = token "gl"). NOT applied by the
-- server: database/migrations is the source of truth (applied in order, search_path = gl).
-- Keep this file in step with the migrations for human readers.
create schema if not exists gl;
set search_path to gl;

-- 0001_config_store (US-003)
--   config_artifact (tenant_id, config_id) — a configuration's identity per tenant
--   config_version  (tenant_id, config_id, version) — draft | validated | approved | retired,
--                   effective_from / effective_to ([from, to)), content jsonb + content_digest,
--                   who/when for create, update, validate, approve, retire
--   config_audit    append-only history of every action (actor, reason, statuses, digest)
--   triggers        config_version_guard (approved/retired immutable; no deletes),
--                   config_audit_append_only
--   index           config_version_one_approved_start (one approved start per date)
--   row-level security, FORCED, on all three tables: tenant_id = current_setting('gl.tenant_id')

-- 0003_sandbox_ledger (ALTB-05)
--   sandbox_book    (tenant_id, book_id) — sandbox = true (CHECK), pins config_id + config_version
--   sandbox_posting (tenant_id, book_id, seq) — one source event (system, id, version) once per
--                   book; its C6 journal entries (jsonb), book date, who/when
--   sandbox_line    (tenant_id, book_id, line_id) — each posted line with the keys later events read
--   triggers        sandbox_*_append_only (never updated or deleted)
--   row-level security, FORCED, on all three tables: tenant_id = current_setting('gl.tenant_id')
