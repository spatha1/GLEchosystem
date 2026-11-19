-- 0001_config_store — tenant-scoped, versioned, effective-dated GL configuration (US-003).
--
-- Applied by the module server inside schema "gl" (search_path pinned), at most once.
-- The canonical copy lives in database/migrations; service/logic/sql holds a byte-identical
-- copy that the server embeds (TestMigrationsMatchCanonicalFiles enforces it).
--
-- Invariants enforced here, not only in code:
--   * every row belongs to a tenant, and row-level security (FORCED, so it binds the table
--     owner too) shows a request only the rows of the tenant bound to its transaction
--     via set_config('gl.tenant_id', …, true);
--   * an approved or retired version never changes, except approved → retired;
--   * versions and audit entries are never deleted; audit entries are never updated;
--   * at most one approved version of a configuration starts on any given date.

create table config_artifact (
    tenant_id   text not null check (tenant_id ~ '^[A-Z][A-Z0-9_]*$'),
    config_id   text not null check (config_id ~ '^[A-Z][A-Z0-9_]*$'),
    title       text not null default '',
    created_by  text not null check (created_by <> ''),
    created_at  timestamptz not null default now(),
    primary key (tenant_id, config_id)
);

create table config_version (
    tenant_id          text not null,
    config_id          text not null,
    version            integer not null check (version >= 1),
    status             text not null check (status in ('draft', 'validated', 'approved', 'retired')),
    schema_version     text not null,
    effective_from     date not null,
    effective_to       date,
    content            jsonb not null,
    content_digest     text not null check (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    based_on_version   integer,
    created_by         text not null check (created_by <> ''),
    created_at         timestamptz not null default now(),
    updated_by         text not null check (updated_by <> ''),
    updated_at         timestamptz not null default now(),
    validated_by       text,
    validated_at       timestamptz,
    validation_report  jsonb,
    approved_by        text,
    approved_at        timestamptz,
    retired_by         text,
    retired_at         timestamptz,
    primary key (tenant_id, config_id, version),
    foreign key (tenant_id, config_id) references config_artifact (tenant_id, config_id),
    check (effective_to is null or effective_to > effective_from),
    check (status <> 'validated' or (validated_by is not null and validated_at is not null)),
    check (status <> 'approved' or (approved_by is not null and approved_at is not null)),
    check (status <> 'retired' or (retired_by is not null and retired_at is not null))
);

-- One approved version per start date: the effective version on a date is the approved
-- version with the latest effective_from on or before it, so two approvals starting the
-- same day would make that choice ambiguous.
create unique index config_version_one_approved_start
    on config_version (tenant_id, config_id, effective_from)
    where status = 'approved';

create index config_version_status_idx on config_version (tenant_id, config_id, status);

create table config_audit (
    audit_id        bigserial primary key,
    tenant_id       text not null,
    config_id       text not null,
    version         integer,
    action          text not null check (action in
                        ('create_artifact', 'create_version', 'update_draft', 'validate',
                         'validation_failed', 'approve', 'retire')),
    actor           text not null check (actor <> ''),
    reason          text,
    from_status     text,
    to_status       text,
    content_digest  text,
    detail          jsonb not null default '{}',
    at              timestamptz not null default now()
);

create index config_audit_target_idx on config_audit (tenant_id, config_id, version, audit_id);

-- Approved and retired versions are immutable. The single permitted change is
-- approved → retired, which may set only status, retired_by/at and updated_by/at.
create function config_version_guard() returns trigger language plpgsql as $$
begin
    if tg_op = 'DELETE' then
        raise exception 'GL_CONFIG_IMMUTABLE: configuration versions are never deleted'
            using errcode = 'restrict_violation';
    end if;
    if (new.tenant_id, new.config_id, new.version) is distinct from (old.tenant_id, old.config_id, old.version) then
        raise exception 'GL_CONFIG_IMMUTABLE: a version''s identity never changes'
            using errcode = 'restrict_violation';
    end if;
    if old.status in ('approved', 'retired') then
        if old.status = 'approved' and new.status = 'retired'
           and (new.schema_version, new.effective_from, new.effective_to, new.content, new.content_digest,
                new.based_on_version, new.created_by, new.created_at, new.validated_by, new.validated_at,
                new.validation_report, new.approved_by, new.approved_at)
               is not distinct from
               (old.schema_version, old.effective_from, old.effective_to, old.content, old.content_digest,
                old.based_on_version, old.created_by, old.created_at, old.validated_by, old.validated_at,
                old.validation_report, old.approved_by, old.approved_at)
        then
            return new;
        end if;
        raise exception 'GL_CONFIG_IMMUTABLE: version % of % is %; publish a new version instead',
            old.version, old.config_id, old.status
            using errcode = 'restrict_violation';
    end if;
    return new;
end;
$$;

create trigger config_version_guard
    before update or delete on config_version
    for each row execute function config_version_guard();

create function config_audit_append_only() returns trigger language plpgsql as $$
begin
    raise exception 'GL_AUDIT_APPEND_ONLY: audit entries are never changed or removed'
        using errcode = 'restrict_violation';
end;
$$;

create trigger config_audit_append_only
    before update or delete on config_audit
    for each row execute function config_audit_append_only();

-- Tenant isolation. FORCE makes the policies bind the table owner as well; only a
-- superuser or a BYPASSRLS role escapes them, so the module must not run as one.
alter table config_artifact enable row level security;
alter table config_artifact force row level security;
create policy tenant_isolation on config_artifact
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));

alter table config_version enable row level security;
alter table config_version force row level security;
create policy tenant_isolation on config_version
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));

alter table config_audit enable row level security;
alter table config_audit force row level security;
create policy tenant_isolation on config_audit
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));
