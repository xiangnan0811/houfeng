\set ON_ERROR_STOP on
\getenv houfeng_database_user HOUFENG_DATABASE_USER
\getenv houfeng_database_password HOUFENG_DATABASE_PASSWORD

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
