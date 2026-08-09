#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(dirname -- "$script_dir")
compose_file=$repo_root/compose.yaml
env_file=${1:-$repo_root/docs/deploy/compose.env}
case "$env_file" in
	/*) ;;
	*) env_file=$repo_root/$env_file ;;
esac

if [ ! -f "$env_file" ]; then
	printf 'Compose env file does not exist: %s\n' "$env_file" >&2
	exit 1
fi

ready_max_attempts=${HOUFENG_COMPOSE_DB_READY_MAX_ATTEMPTS:-60}
ready_interval_seconds=${HOUFENG_COMPOSE_DB_READY_INTERVAL_SECONDS:-2}
case "$ready_max_attempts" in
	''|*[!0-9]*|0)
		printf '%s\n' 'HOUFENG_COMPOSE_DB_READY_MAX_ATTEMPTS must be a positive integer' >&2
		exit 1
		;;
esac

compose() {
	docker compose --env-file "$env_file" -f "$compose_file" "$@"
}

compose config --quiet
compose up -d db

database_ready() {
	compose exec -T db sh -ceu '
  : "${POSTGRES_USER:?POSTGRES_USER is required}"
  : "${POSTGRES_DB:?POSTGRES_DB is required}"
  exec pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"
' >/dev/null 2>&1 &&
		compose exec -T db sh -ceu '
  exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "select 1"
' >/dev/null 2>&1
}

attempt=1
while ! database_ready; do
	if [ "$attempt" -ge "$ready_max_attempts" ]; then
		printf 'PostgreSQL did not become ready after %s attempts\n' "$ready_max_attempts" >&2
		exit 1
	fi
	attempt=$((attempt + 1))
	sleep "$ready_interval_seconds"
done

compose exec -T db sh -ceu '
  : "${POSTGRES_USER:?POSTGRES_USER is required}"
  : "${POSTGRES_DB:?POSTGRES_DB is required}"
  : "${HOUFENG_DATABASE_USER:?HOUFENG_DATABASE_USER is required}"
  if [ "$POSTGRES_USER" = "$HOUFENG_DATABASE_USER" ]; then
    printf "%s\n" "PostgreSQL bootstrap and Houfeng application principals must differ" >&2
    exit 1
  fi
'

compose exec -T db sh -ceu '
  exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"
' < "$repo_root/docs/deploy/app-acl-r2-pre-r1-provisioning.sql"

compose exec -T db sh -ceu '
  : "${HOUFENG_DATABASE_PASSWORD_FILE:?HOUFENG_DATABASE_PASSWORD_FILE is required}"
  if [ ! -r "$HOUFENG_DATABASE_PASSWORD_FILE" ]; then
    printf "%s\n" "Houfeng database password file is not readable" >&2
    exit 1
  fi
  HOUFENG_DATABASE_PASSWORD=$(cat "$HOUFENG_DATABASE_PASSWORD_FILE")
  if [ -z "$HOUFENG_DATABASE_PASSWORD" ]; then
    printf "%s\n" "Houfeng database password file is empty" >&2
    exit 1
  fi
  export HOUFENG_DATABASE_PASSWORD
  exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"
' < "$repo_root/docs/deploy/compose-application-role.sql"

compose up -d houfeng houfeng-content-processor
