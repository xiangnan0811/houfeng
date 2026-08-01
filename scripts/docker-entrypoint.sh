#!/bin/sh
set -eu

uri_encode_component() {
	component=$1
	encoded=
	octets=$(printf '%s' "$component" | od -An -v -tu1) || {
		printf '%s\n' "houfeng container entrypoint: database connection component could not be percent-encoded" >&2
		return 1
	}
	for octet in $octets; do
		case "$octet" in
		''|*[!0-9]*)
			printf '%s\n' "houfeng container entrypoint: database connection component could not be percent-encoded" >&2
			return 1
			;;
		esac
		if [ "$octet" -lt 32 ] || [ "$octet" -eq 127 ]; then
			printf '%s\n' "houfeng container entrypoint: database connection component contains an ASCII control byte" >&2
			return 1
		fi
		if { [ "$octet" -ge 48 ] && [ "$octet" -le 57 ]; } ||
			{ [ "$octet" -ge 65 ] && [ "$octet" -le 90 ]; } ||
			{ [ "$octet" -ge 97 ] && [ "$octet" -le 122 ]; } ||
			[ "$octet" -eq 45 ] || [ "$octet" -eq 46 ] ||
			[ "$octet" -eq 95 ] || [ "$octet" -eq 126 ]; then
			octal=$(printf '%03o' "$octet")
			encoded=$encoded$(printf "\\$octal")
		else
			encoded=$encoded$(printf '%%%02X' "$octet")
		fi
	done
	printf '%s' "$encoded"
}

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
		password_file_octets=$(od -An -v -tu1 < "$HOUFENG_DATABASE_PASSWORD_FILE") || {
			printf '%s\n' "houfeng container entrypoint: HOUFENG_DATABASE_PASSWORD_FILE is not readable" >&2
			exit 1
		}
		for octet in $password_file_octets; do
			if [ "$octet" -eq 0 ]; then
				printf '%s\n' "houfeng container entrypoint: database connection component contains an ASCII control byte" >&2
				exit 1
			fi
		done
		database_password=$(cat "$HOUFENG_DATABASE_PASSWORD_FILE")
	fi
	if [ -z "$database_password" ]; then
		printf '%s\n' "houfeng container entrypoint: HOUFENG_DATABASE_PASSWORD or HOUFENG_DATABASE_PASSWORD_FILE is required when HOUFENG_DATABASE_URL is not set" >&2
		exit 1
	fi

	encoded_database_user=$(uri_encode_component "$HOUFENG_DATABASE_USER") || exit 1
	encoded_database_password=$(uri_encode_component "$database_password") || exit 1
	encoded_database_name=$(uri_encode_component "$HOUFENG_DATABASE_NAME") || exit 1
	database_url=$(printf 'postgres://%s:%s@db:5432/?dbname=%s&sslmode=disable' "$encoded_database_user" "$encoded_database_password" "$encoded_database_name")
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
