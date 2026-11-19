-- 0002_roles_maker_checker — per-tenant roles and maker-checker on configuration (US-004).
--
-- Identity comes from the platform (the app core sets x-mycel-user); WHAT a user may do in a
-- tenant is GL's own decision, recorded here. Enforced in the database, not only in code:
--   * the actor who changed a version's content can never be its approver (CHECK on editors);
--   * role grants are tenant-scoped (forced row-level security) and their history is
--     append-only.

-- Everyone who changed a version's content. Maintained with every content change; immutable
-- once the version is approved (the guard below now covers it).
alter table config_version add column editors text[] not null default '{}';
-- Backfill existing versions (all tenants). Inside this migration's single transaction the
-- owner briefly lifts FORCED row-level security (else it would see no rows) and the
-- immutability guard (else approved rows could not receive their editors); both are
-- restored before the transaction ends.
alter table config_version no force row level security;
alter table config_audit no force row level security;
alter table config_version disable trigger config_version_guard;
update config_version v set editors = array(
    select distinct a from unnest(array[v.created_by] || coalesce((
        select array_agg(au.actor) from config_audit au
         where au.tenant_id = v.tenant_id and au.config_id = v.config_id and au.version = v.version
           and au.action in ('create_version', 'update_draft')), '{}')) as a
    where a is not null);
alter table config_version enable trigger config_version_guard;
alter table config_version add constraint config_version_has_editor
    check (cardinality(editors) >= 1);
-- Maker-checker binds every approval from now on. A version approved by its own editor
-- before this migration (US-003 allowed it) stays as history: the constraint is then left
-- NOT VALID (still enforced on every insert and update) and the migration says so.
alter table config_version add constraint config_version_maker_checker
    check (status <> 'approved' or not (approved_by = any (editors))) not valid;
do $$
begin
    if exists (select 1 from config_version where status = 'approved' and approved_by = any (editors)) then
        raise notice 'GL 0002: approved versions approved by one of their own editors exist (pre-maker-checker history); the constraint applies to all new approvals';
    else
        alter table config_version validate constraint config_version_maker_checker;
    end if;
end;
$$;
alter table config_audit force row level security;
alter table config_version force row level security;

-- The guard of 0001, now also protecting editors.
create or replace function config_version_guard() returns trigger language plpgsql as $$
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
                new.validation_report, new.approved_by, new.approved_at, new.editors)
               is not distinct from
               (old.schema_version, old.effective_from, old.effective_to, old.content, old.content_digest,
                old.based_on_version, old.created_by, old.created_at, old.validated_by, old.validated_at,
                old.validation_report, old.approved_by, old.approved_at, old.editors)
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

create table role_assignment (
    tenant_id   text not null check (tenant_id ~ '^[A-Z][A-Z0-9_]*$'),
    actor       text not null check (actor <> ''),
    role        text not null check (role in ('GL_ADMIN', 'CONFIG_EDITOR', 'CONFIG_APPROVER', 'OPERATOR', 'VIEWER')),
    granted_by  text not null check (granted_by <> ''),
    granted_at  timestamptz not null default now(),
    primary key (tenant_id, actor, role)
);

create table role_audit (
    audit_id    bigserial primary key,
    tenant_id   text not null,
    actor       text not null check (actor <> ''),
    subject     text not null check (subject <> ''),
    granted     text[] not null default '{}',
    revoked     text[] not null default '{}',
    reason      text not null check (reason <> ''),
    at          timestamptz not null default now()
);

create function role_audit_append_only() returns trigger language plpgsql as $$
begin
    raise exception 'GL_AUDIT_APPEND_ONLY: audit entries are never changed or removed'
        using errcode = 'restrict_violation';
end;
$$;

create trigger role_audit_append_only
    before update or delete on role_audit
    for each row execute function role_audit_append_only();

alter table role_assignment enable row level security;
alter table role_assignment force row level security;
create policy tenant_isolation on role_assignment
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));

alter table role_audit enable row level security;
alter table role_audit force row level security;
create policy tenant_isolation on role_audit
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));
