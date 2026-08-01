\set ON_ERROR_STOP on

BEGIN;

DO $app_acl_r2_pre_r1$
DECLARE
    function_count bigint;
    function_owner bigint;
    function_identity_arguments text;
    actor_authorized boolean;
BEGIN
    SELECT pg_catalog.count(*)::bigint,
           pg_catalog.min(procedure.proowner::bigint),
           pg_catalog.min(pg_catalog.pg_get_function_identity_arguments(procedure.oid))
      INTO function_count, function_owner, function_identity_arguments
      FROM pg_catalog.pg_proc procedure
      JOIN pg_catalog.pg_namespace namespace
        ON namespace.oid = procedure.pronamespace
     WHERE namespace.nspname = 'pg_catalog'
       AND procedure.proname = 'pg_control_system'
       AND procedure.pronargs = 0;

    IF function_count <> 1 THEN
        RAISE EXCEPTION 'expected exactly one pg_catalog.pg_control_system() function, found %', function_count;
    END IF;
    IF function_owner <> 10 THEN
        RAISE EXCEPTION 'pg_catalog.pg_control_system() owner is %, expected bootstrap OID 10', function_owner;
    END IF;
    IF function_identity_arguments IS DISTINCT FROM 'OUT pg_control_version integer, OUT catalog_version_no integer, OUT system_identifier bigint, OUT pg_control_last_modified timestamp with time zone' THEN
        RAISE EXCEPTION 'pg_catalog.pg_control_system() identity arguments are %, expected PostgreSQL 16 catalog shape', function_identity_arguments;
    END IF;

    SELECT role.rolsuper OR role.oid = function_owner
      INTO actor_authorized
      FROM pg_catalog.pg_roles role
     WHERE role.rolname = current_user;
    IF actor_authorized IS DISTINCT FROM true THEN
        RAISE EXCEPTION 'pre-R1 provisioning must run as a superuser or pg_control_system() owner';
    END IF;
END
$app_acl_r2_pre_r1$;

REVOKE EXECUTE ON FUNCTION pg_catalog.pg_control_system() FROM PUBLIC;

DO $app_acl_r2_pre_r1_verify$
DECLARE
    exact_acl_count bigint;
    function_identity_arguments text;
BEGIN
    SELECT pg_catalog.count(*)::bigint,
           pg_catalog.min(pg_catalog.pg_get_function_identity_arguments(procedure.oid))
      INTO exact_acl_count, function_identity_arguments
      FROM pg_catalog.pg_proc procedure
      JOIN pg_catalog.pg_namespace namespace
        ON namespace.oid = procedure.pronamespace
      CROSS JOIN LATERAL pg_catalog.aclexplode(procedure.proacl) acl_grant
     WHERE namespace.nspname = 'pg_catalog'
       AND procedure.proname = 'pg_control_system'
       AND procedure.pronargs = 0
       AND procedure.proowner = 10
       AND acl_grant.grantor = procedure.proowner
       AND acl_grant.grantee = procedure.proowner
       AND acl_grant.privilege_type = 'EXECUTE'
       AND NOT acl_grant.is_grantable;

    IF function_identity_arguments IS DISTINCT FROM 'OUT pg_control_version integer, OUT catalog_version_no integer, OUT system_identifier bigint, OUT pg_control_last_modified timestamp with time zone' THEN
        RAISE EXCEPTION 'pg_catalog.pg_control_system() identity arguments are %, expected PostgreSQL 16 catalog shape', function_identity_arguments;
    END IF;

    IF exact_acl_count <> 1 OR EXISTS (
        SELECT 1
          FROM pg_catalog.pg_proc procedure
          JOIN pg_catalog.pg_namespace namespace
            ON namespace.oid = procedure.pronamespace
          CROSS JOIN LATERAL pg_catalog.aclexplode(procedure.proacl) acl_grant
         WHERE namespace.nspname = 'pg_catalog'
           AND procedure.proname = 'pg_control_system'
           AND procedure.pronargs = 0
           AND (acl_grant.grantor <> procedure.proowner
                OR acl_grant.grantee <> procedure.proowner
                OR acl_grant.privilege_type <> 'EXECUTE'
                OR acl_grant.is_grantable)
    ) THEN
        RAISE EXCEPTION 'pg_catalog.pg_control_system() ACL is not explicit owner-only EXECUTE';
    END IF;
END
$app_acl_r2_pre_r1_verify$;

COMMIT;
