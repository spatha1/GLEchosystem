-- 0003_sandbox_ledger.down — reverses 0003 exactly: the sandbox books, their postings and lines.
drop table if exists sandbox_line;
drop table if exists sandbox_posting;
drop table if exists sandbox_book;
drop function if exists sandbox_append_only();

do $$
begin
    if to_regclass('_mycel_migrations') is not null then
        delete from _mycel_migrations where id = '0003_sandbox_ledger';
    end if;
end;
$$;
