-- 0004_onboarding_ai — client onboarding, the reviewer's return to draft, and the audit and
-- budget of the AI assistant.
--
-- Invariants enforced here, not only in code:
--   * a client's profile is one row per tenant; every change to it is an append-only audit row;
--   * a reviewer may send a validated version back to draft (return_to_draft), with a reason;
--   * every AI suggestion a person accepted, edited or rejected is an append-only row;
--   * AI usage is counted per tenant and day, so a daily budget can be enforced;
--   * row-level security, FORCED, by tenant, as for configuration.
-- Tenants themselves (sign-in, the client's existence) still belong to the platform.

create table gl_client (
    tenant_id       text primary key check (tenant_id ~ '^[A-Z][A-Z0-9_]*$'),
    name            text not null check (name <> ''),
    status          text not null check (status in ('ONBOARDING', 'ACTIVE', 'SUSPENDED')),
    config_id       text check (config_id is null or config_id ~ '^[A-Z][A-Z0-9_]*$'),
    profile         jsonb not null default '{}'::jsonb,
    created_by      text not null check (created_by <> ''),
    created_at      timestamptz not null default now(),
    updated_by      text not null check (updated_by <> ''),
    updated_at      timestamptz not null default now()
);

create table gl_client_audit (
    audit_id    bigserial primary key,
    tenant_id   text not null,
    actor       text not null check (actor <> ''),
    action      text not null check (action in ('onboard', 'update')),
    reason      text not null check (reason <> ''),
    before      jsonb,
    after       jsonb not null,
    at          timestamptz not null default now()
);

create table ai_decision (
    decision_id     bigserial primary key,
    tenant_id       text not null,
    area            text not null check (area in ('admin', 'onboarding', 'stage1')),
    step            text not null default '',
    config_id       text,
    version         integer,
    suggestion_id   text not null check (suggestion_id <> ''),
    title           text not null default '',
    decision        text not null check (decision in ('ACCEPTED', 'EDITED', 'REJECTED')),
    detail          jsonb not null default '{}'::jsonb,
    provider        text not null default '',
    model           text not null default '',
    actor           text not null check (actor <> ''),
    at              timestamptz not null default now()
);

create table ai_usage (
    tenant_id           text not null,
    day                 date not null,
    requests            integer not null default 0 check (requests >= 0),
    prompt_tokens       bigint not null default 0 check (prompt_tokens >= 0),
    completion_tokens   bigint not null default 0 check (completion_tokens >= 0),
    primary key (tenant_id, day)
);

create function onboarding_append_only() returns trigger language plpgsql as $$
begin
    raise exception 'GL_AUDIT_APPEND_ONLY: audit entries are never changed or removed'
        using errcode = 'restrict_violation';
end;
$$;

create trigger gl_client_audit_append_only
    before update or delete on gl_client_audit
    for each row execute function onboarding_append_only();

create trigger ai_decision_append_only
    before update or delete on ai_decision
    for each row execute function onboarding_append_only();

alter table config_audit drop constraint config_audit_action_check;
alter table config_audit add constraint config_audit_action_check check (action in
    ('create_artifact', 'create_version', 'update_draft', 'validate',
     'validation_failed', 'approve', 'retire', 'return_to_draft'));

alter table gl_client enable row level security;
alter table gl_client force row level security;
create policy tenant_isolation on gl_client
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));

alter table gl_client_audit enable row level security;
alter table gl_client_audit force row level security;
create policy tenant_isolation on gl_client_audit
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));

alter table ai_decision enable row level security;
alter table ai_decision force row level security;
create policy tenant_isolation on ai_decision
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));

alter table ai_usage enable row level security;
alter table ai_usage force row level security;
create policy tenant_isolation on ai_usage
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));
