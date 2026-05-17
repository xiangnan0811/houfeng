#!/bin/sh
set -eu

if [ -z "${HOUFENG_DATABASE_URL:-}" ]; then
	if [ -z "${POSTGRES_PASSWORD:-}" ]; then
		printf '%s\n' "houfeng container entrypoint: POSTGRES_PASSWORD is required in docs/deploy/compose.env when HOUFENG_DATABASE_URL is not set" >&2
		exit 1
	fi

	database_url=$(printf '%s%s%s' 'postgres://houfeng:' "$POSTGRES_PASSWORD" '@db:5432/houfeng?sslmode=disable')
	export HOUFENG_DATABASE_URL="$database_url"
fi

if [ -z "${HOUFENG_INITIAL_USERNAME:-}" ]; then
	export HOUFENG_INITIAL_USERNAME=admin
fi

if [ -z "${HOUFENG_INITIAL_PASSWORD:-}" ]; then
	printf '%s\n' "houfeng container entrypoint: HOUFENG_INITIAL_PASSWORD is required in docs/deploy/compose.env" >&2
	exit 1
fi

if [ "$(id -u)" = "0" ]; then
	if [ -n "${HOUFENG_LOG_FILE:-}" ]; then
		log_dir=$(dirname -- "$HOUFENG_LOG_FILE")
		install -d -o houfeng -g houfeng -m 0755 -- "$log_dir"
	fi

	exec gosu houfeng "$@"
fi

exec "$@"
