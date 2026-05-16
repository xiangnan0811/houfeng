# Fix Compose secret scan failure

## Goal

Fix PR #91 so GitGuardian no longer flags the published Docker Compose file for password-like environment assignments, while preserving the Docker/Compose deployment contract and quick-start behavior.

## Requirements

* `compose.yaml` must not contain password-like environment assignment lines for `POSTGRES_PASSWORD` or `HOUFENG_INITIAL_PASSWORD`.
* The Compose quick start must still read operator-provided values from untracked `docs/deploy/compose.env`.
* The Houfeng service must keep Docker defaults: service name `houfeng`, image `linnea7171/houfeng:latest`, internal/external default port `16001`, no local `build:` block, no agent service.
* PostgreSQL data remains a user-migratable bind mount under `./data/postgres/`.
* Docs/spec must explain that sensitive values live in the env file and the tracked Compose file avoids password assignments to satisfy secret scanners.

## Acceptance Criteria

* [ ] `docker compose --env-file docs/deploy/compose.env.example -f compose.yaml config --quiet` passes.
* [ ] `compose.yaml` has no `POSTGRES_PASSWORD:` or `HOUFENG_INITIAL_PASSWORD:` assignment lines.
* [ ] `compose.yaml` has no project `build:` block and no `agent` service.
* [ ] Docs remain accurate about copying `docs/deploy/compose.env.example` to untracked `docs/deploy/compose.env`.
* [ ] PR checks can be re-run on a new commit.

## Technical Approach

Add a small container entrypoint that assembles `HOUFENG_DATABASE_URL` at runtime from env-file variables before executing `houfeng-center`. Update the Docker image to include that entrypoint. Make `compose.yaml` use `env_file: docs/deploy/compose.env`, remove secret assignments from tracked Compose source, and pass only non-secret fixed env values in `environment`.

## Out of Scope

* Changing the GitGuardian incident state manually.
* Implementing Docker image publishing automation.
* Implementing file-based application logging.
