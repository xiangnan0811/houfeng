\set ON_ERROR_STOP on

BEGIN;

DO $app_acl_r2_pre_r1$
DECLARE
    function_count bigint;
    function_owner bigint;
    actor_authorized boolean;
BEGIN
    SELECT pg_catalog.count(*)::bigint,
           pg_catalog.min(procedure.proowner::bigint)
      INTO function_count, function_owner
      FROM pg_catalog.pg_proc procedure
      JOIN pg_catalog.pg_namespace namespace
        ON namespace.oid = procedure.pronamespace
     WHERE namespace.nspname = 'pg_catalog'
       AND procedure.proname = 'pg_control_system'
       AND pg_catalog.pg_get_function_identity_arguments(procedure.oid) = '';

    IF function_count <> 1 THEN
        RAISE EXCEPTION 'expected exactly one pg_catalog.pg_control_system() function, found %', function_count;
    END IF;
    IF function_owner <> 10 THEN
        RAISE EXCEPTION 'pg_catalog.pg_control_system() owner is %, expected bootstrap OID 10', function_owner;
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
BEGIN
    SELECT pg_catalog.count(*)::bigint
      INTO exact_acl_count
      FROM pg_catalog.pg_proc procedure
      JOIN pg_catalog.pg_namespace namespace
        ON namespace.oid = procedure.pronamespace
      CROSS JOIN LATERAL pg_catalog.aclexplode(procedure.proacl) acl_grant
     WHERE namespace.nspname = 'pg_catalog'
       AND procedure.proname = 'pg_control_system'
       AND pg_catalog.pg_get_function_identity_arguments(procedure.oid) = ''
       AND procedure.proowner = 10
       AND acl_grant.grantor = procedure.proowner
       AND acl_grant.grantee = procedure.proowner
       AND acl_grant.privilege_type = 'EXECUTE'
       AND NOT acl_grant.is_grantable;

    IF exact_acl_count <> 1 OR EXISTS (
        SELECT 1
          FROM pg_catalog.pg_proc procedure
          JOIN pg_catalog.pg_namespace namespace
            ON namespace.oid = procedure.pronamespace
          CROSS JOIN LATERAL pg_catalog.aclexplode(procedure.proacl) acl_grant
         WHERE namespace.nspname = 'pg_catalog'
           AND procedure.proname = 'pg_control_system'
           AND pg_catalog.pg_get_function_identity_arguments(procedure.oid) = ''
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
