-- 0004_onboarding_ai.down — reverses 0004 exactly: the client register, the AI audit and budget,
-- and the return_to_draft audit action (refused while such audit rows exist).
alter table config_audit drop constraint config_audit_action_check;
alter table config_audit add constraint config_audit_action_check check (action in
    ('create_artifact', 'create_version', 'update_draft', 'validate',
     'validation_failed', 'approve', 'retire'));

drop table if exists ai_usage;
drop table if exists ai_decision;
drop table if exists gl_client_audit;
drop table if exists gl_client;
drop function if exists onboarding_append_only();

do $$
begin
    if to_regclass('_mycel_migrations') is not null then
        delete from _mycel_migrations where id = '0004_onboarding_ai';
    end if;
end;
$$;
