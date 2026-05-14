# Local runtime check

## Initial state

- `main` was clean at `226ac06`.
- There was no active Trellis task.
- `psql` and `pg_isready` were not available on PATH.
- `docker` CLI is available at `/usr/local/bin/docker`.
- `docker context ls` showed `orbstack` as the active context.
- `docker ps` failed because the Docker daemon socket at `/Users/weibo/.orbstack/run/docker.sock` was not available.
- `orb status` returned `Stopped`.
- Docker Desktop context was also not running.
- `podman machine list` failed because the temp storage-run path was not writable only by the current user.

## Implication

The local center sample path likely needs OrbStack or another Docker runtime to be started before PostgreSQL can run. If the runtime cannot be started from the session, record the local-center evidence as environment-blocked rather than claiming real center validation.
