#!/bin/sh
set -eu

if [ -z "${HOUFENG_DATABASE_URL:-}" ]; then
	if [ -z "${HOUFENG_DATABASE_USER:-}" ]; then
		printf '%s\n' "houfeng container entrypoint: HOUFENG_DATABASE_USER is required when HOUFENG_DATABASE_URL is not set" >&2
		exit 1
	fi
	if [ -z "${HOUFENG_DATABASE_NAME:-}" ]; then
		printf '%s\n' "houfeng container entrypoint: HOUFENG_DATABASE_NAME is required when HOUFENG_DATABASE_URL is not set" >&2
		exit 1
	fi

	database_password=${HOUFENG_DATABASE_PASSWORD:-}
	if [ -n "${HOUFENG_DATABASE_PASSWORD_FILE:-}" ]; then
		if [ ! -r "$HOUFENG_DATABASE_PASSWORD_FILE" ]; then
			printf '%s\n' "houfeng container entrypoint: HOUFENG_DATABASE_PASSWORD_FILE is not readable" >&2
			exit 1
		fi
		database_password=$(cat "$HOUFENG_DATABASE_PASSWORD_FILE")
	fi
	if [ -z "$database_password" ]; then
		printf '%s\n' "houfeng container entrypoint: HOUFENG_DATABASE_PASSWORD or HOUFENG_DATABASE_PASSWORD_FILE is required when HOUFENG_DATABASE_URL is not set" >&2
		exit 1
	fi

	database_url=$(printf 'postgres://%s:%s@db:5432/%s?sslmode=disable' "$HOUFENG_DATABASE_USER" "$database_password" "$HOUFENG_DATABASE_NAME")
	export HOUFENG_DATABASE_URL="$database_url"
fi

if [ -z "${HOUFENG_INITIAL_USERNAME:-}" ]; then
	export HOUFENG_INITIAL_USERNAME=admin
fi

if [ -z "${HOUFENG_INITIAL_PASSWORD:-}" ] && [ -z "${HOUFENG_INITIAL_PASSWORD_FILE:-}" ]; then
	printf '%s\n' "houfeng container entrypoint: HOUFENG_INITIAL_PASSWORD or HOUFENG_INITIAL_PASSWORD_FILE is required in docs/deploy/compose.env" >&2
	exit 1
fi

if [ -z "${HOUFENG_SESSION_HMAC_KEY:-}" ] && [ -z "${HOUFENG_SESSION_HMAC_KEY_FILE:-}" ]; then
	printf '%s\n' "houfeng container entrypoint: HOUFENG_SESSION_HMAC_KEY or HOUFENG_SESSION_HMAC_KEY_FILE is required in docs/deploy/compose.env" >&2
	exit 1
fi

exec "$@"
