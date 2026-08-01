\set ON_ERROR_STOP on
\getenv houfeng_database_user HOUFENG_DATABASE_USER
\getenv houfeng_database_password HOUFENG_DATABASE_PASSWORD

BEGIN;

SELECT pg_catalog.format(
           'CREATE ROLE %I LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L',
           :'houfeng_database_user',
           :'houfeng_database_password'
       )
 WHERE NOT EXISTS (
           SELECT 1
             FROM pg_catalog.pg_roles
            WHERE rolname = :'houfeng_database_user'
       )
\gexec

SELECT CASE
           WHEN EXISTS (
               SELECT 1
                 FROM pg_catalog.pg_auth_members AS membership
                 JOIN pg_catalog.pg_roles AS application_role
                   ON application_role.oid IN (membership.roleid, membership.member)
                WHERE application_role.rolname = :'houfeng_database_user'
           ) THEN 'true'
           ELSE 'false'
       END AS houfeng_application_role_has_membership
\gset
\if :houfeng_application_role_has_membership
  DO $houfeng_membership_drift$
  BEGIN
      RAISE EXCEPTION 'Houfeng application role must not have direct or recursive role membership';
  END
  $houfeng_membership_drift$;
\endif

SELECT pg_catalog.format(
           'ALTER ROLE %I WITH LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L',
           :'houfeng_database_user',
           :'houfeng_database_password'
       )
\gexec

SELECT pg_catalog.format(
           'ALTER DATABASE %I OWNER TO %I',
           pg_catalog.current_database(),
           :'houfeng_database_user'
       )
\gexec

COMMIT;
