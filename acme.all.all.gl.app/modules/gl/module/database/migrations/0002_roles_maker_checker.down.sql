-- 0002_roles_maker_checker.down — reverses 0002 exactly, restoring 0001's guard.
drop table if exists role_audit;
drop table if exists role_assignment;
drop function if exists role_audit_append_only();
alter table config_version drop constraint if exists config_version_has_editor;
alter table config_version drop constraint if exists config_version_maker_checker;

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

alter table config_version drop column if exists editors;

do $$
begin
    if to_regclass('_mycel_migrations') is not null then
        delete from _mycel_migrations where id = '0002_roles_maker_checker';
    end if;
end;
$$;
