-- 0001_config_store.down — reverses 0001_config_store exactly (US-003 rollback).
-- Not applied by the module server (it only moves forward); used by operators and by
-- module_test's up → down → up check. Run inside schema "gl".
drop table if exists config_audit;
drop table if exists config_version;
drop table if exists config_artifact;
drop function if exists config_audit_append_only();
drop function if exists config_version_guard();
-- Forget the applied-migration record so the module server re-applies 0001 on next boot.
do $$
begin
    if to_regclass('_mycel_migrations') is not null then
        delete from _mycel_migrations where id = '0001_config_store';
    end if;
end;
$$;
