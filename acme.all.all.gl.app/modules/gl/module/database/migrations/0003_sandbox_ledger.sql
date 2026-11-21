-- 0003_sandbox_ledger — the Ledger, sandbox first (ALTB-05).
--
-- A sandbox book posts synthetic events with one approved configuration version, through the
-- same engine as a posting preview, and keeps the journal entries (contract C6) they produced
-- and the lines later events read back (OPEN_BALANCE, balances).
--
-- Invariants enforced here, not only in code:
--   * every book is a sandbox (CHECK): nothing here is real financial data, and no table for
--     real postings exists until that ledger is designed and approved;
--   * a book pins one configuration version, which must exist;
--   * books, postings and lines are append-only: never updated or deleted;
--   * a source event (system, id, version) posts at most once per book, and a line id (so an
--     economic event's journal) at most once per book;
--   * row-level security, FORCED, by tenant, as for configuration.

create table sandbox_book (
    tenant_id       text not null check (tenant_id ~ '^[A-Z][A-Z0-9_]*$'),
    book_id         text not null check (book_id ~ '^[A-Z][A-Z0-9_]*$'),
    sandbox         boolean not null default true check (sandbox),
    config_id       text not null,
    config_version  integer not null,
    content_digest  text not null check (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    reason          text not null check (reason <> ''),
    created_by      text not null check (created_by <> ''),
    created_at      timestamptz not null default now(),
    primary key (tenant_id, book_id),
    foreign key (tenant_id, config_id, config_version) references config_version (tenant_id, config_id, version)
);

create table sandbox_posting (
    tenant_id             text not null,
    book_id               text not null,
    seq                   integer not null check (seq >= 1),
    step_code             text not null default '',
    source_system         text not null check (source_system <> ''),
    source_event_id       text not null check (source_event_id <> ''),
    source_event_version  integer not null check (source_event_version >= 1),
    input_digest          text not null check (input_digest ~ '^sha256:[0-9a-f]{64}$'),
    event_type            text not null,
    economic_event_key    text not null,
    book_date             date not null,
    journals              jsonb not null,
    posted_by             text not null check (posted_by <> ''),
    posted_at             timestamptz not null default now(),
    primary key (tenant_id, book_id, seq),
    constraint sandbox_posting_once unique (tenant_id, book_id, source_system, source_event_id, source_event_version),
    foreign key (tenant_id, book_id) references sandbox_book (tenant_id, book_id)
);

create table sandbox_line (
    tenant_id   text not null,
    book_id     text not null,
    seq         integer not null,
    line_id     text not null,
    journal_id  text not null,
    class       text not null,
    account     text not null,
    side        text not null check (side in ('D', 'C')),
    entity      text not null,
    currency    text not null check (currency ~ '^[A-Z]{3}$'),
    book_date   date not null,
    amount      numeric not null check (amount >= 0),
    keys        jsonb not null default '{}',
    primary key (tenant_id, book_id, line_id),
    foreign key (tenant_id, book_id, seq) references sandbox_posting (tenant_id, book_id, seq)
);

create function sandbox_append_only() returns trigger language plpgsql as $$
begin
    raise exception 'GL_SANDBOX_APPEND_ONLY: % rows are never changed or removed; open a new book instead', tg_table_name
        using errcode = 'restrict_violation';
end;
$$;

create trigger sandbox_book_append_only
    before update or delete on sandbox_book
    for each row execute function sandbox_append_only();
create trigger sandbox_posting_append_only
    before update or delete on sandbox_posting
    for each row execute function sandbox_append_only();
create trigger sandbox_line_append_only
    before update or delete on sandbox_line
    for each row execute function sandbox_append_only();

alter table sandbox_book enable row level security;
alter table sandbox_book force row level security;
create policy tenant_isolation on sandbox_book
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));

alter table sandbox_posting enable row level security;
alter table sandbox_posting force row level security;
create policy tenant_isolation on sandbox_posting
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));

alter table sandbox_line enable row level security;
alter table sandbox_line force row level security;
create policy tenant_isolation on sandbox_line
    using (tenant_id = current_setting('gl.tenant_id', true))
    with check (tenant_id = current_setting('gl.tenant_id', true));
