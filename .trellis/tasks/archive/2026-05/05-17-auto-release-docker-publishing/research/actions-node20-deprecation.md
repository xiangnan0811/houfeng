# Research: GitHub Actions Node.js 20 deprecation for Docker official actions

- **Query**: Research current GitHub Actions Node.js 20 deprecation handling for Docker official actions used in Houfeng: `docker/setup-buildx-action@v3`, `docker/build-push-action@v6`, `docker/login-action@v3`, `docker/metadata-action@v5`. Include whether newer major versions exist, whether setting `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24` is an accepted mitigation, risks of using it, and the recommended minimal change for Houfeng workflows.
- **Scope**: mixed
- **Date**: 2026-05-17

## Findings

### Files Found

| File Path | Description |
|---|---|
| `.github/workflows/publish-images.yml` | Docker image publishing workflow using Docker official actions. |

Relevant workflow references:

| File Path | Lines | Current action |
|---|---:|---|
| `.github/workflows/publish-images.yml` | 117 | `docker/setup-buildx-action@v3` |
| `.github/workflows/publish-images.yml` | 119 | `docker/login-action@v3` |
| `.github/workflows/publish-images.yml` | 127 | `docker/build-push-action@v6` |
| `.github/workflows/publish-images.yml` | 172 | `docker/setup-buildx-action@v3` |
| `.github/workflows/publish-images.yml` | 174 | `docker/login-action@v3` |
| `.github/workflows/publish-images.yml` | 180 | `docker/metadata-action@v5` |

### Code Patterns

Houfeng's `publish-images` workflow uses pinned major tags for Docker's JavaScript actions:

```yaml
- uses: docker/setup-buildx-action@v3
- uses: docker/login-action@v3
- uses: docker/build-push-action@v6
- uses: docker/metadata-action@v5
```

The currently referenced major versions still declare Node 20 in their action metadata:

| Action reference currently in Houfeng | `action.yml` runtime |
|---|---|
| `docker/setup-buildx-action@v3` | `runs.using: node20` |
| `docker/build-push-action@v6` | `runs.using: node20` |
| `docker/login-action@v3` | `runs.using: node20` |
| `docker/metadata-action@v5` | `runs.using: node20` |

### External References

- [GitHub Changelog: Deprecation of Node 20 on GitHub Actions runners](https://github.blog/changelog/2025-09-19-deprecation-of-node-20-on-github-actions-runners/) — GitHub says Node 20 reaches EOL in April 2026, runners support Node 20 and Node 24, `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true` can be used to test/force Node 24 before the default migration, Node 24 becomes default on June 2, 2026, and `ACTIONS_ALLOW_USE_UNSECURE_NODE_VERSION=true` is the temporary opt-out afterward until Node 20 is removed later in fall 2026.
- [docker/setup-buildx-action v4.0.0 release](https://github.com/docker/setup-buildx-action/releases/tag/v4.0.0) — release notes state: “Node 24 as default runtime (requires Actions Runner v2.327.1 or later)”. Latest release observed: `v4.0.0` published 2026-03-05.
- [docker/build-push-action v7.0.0 release](https://github.com/docker/build-push-action/releases/tag/v7.0.0) — release notes state: “Node 24 as default runtime (requires Actions Runner v2.327.1 or later)”. Latest release observed: `v7.1.0` published 2026-04-10.
- [docker/login-action v4.0.0 release](https://github.com/docker/login-action/releases/tag/v4.0.0) — release notes state: “Node 24 as default runtime (requires Actions Runner v2.327.1 or later)”. Latest release observed: `v4.1.0` published 2026-04-02.
- [docker/metadata-action v6.0.0 release](https://github.com/docker/metadata-action/releases/tag/v6.0.0) — release notes state: “Node 24 as default runtime (requires Actions Runner v2.327.1 or later)”. Latest release observed: `v6.0.0` published 2026-03-05.

Newer major versions exist for all four Docker actions used by Houfeng:

| Current Houfeng reference | Newer Node 24 major | Latest observed release | Newer `action.yml` runtime |
|---|---|---|---|
| `docker/setup-buildx-action@v3` | `docker/setup-buildx-action@v4` | `v4.0.0` | `runs.using: node24` |
| `docker/build-push-action@v6` | `docker/build-push-action@v7` | `v7.1.0` | `runs.using: node24` |
| `docker/login-action@v3` | `docker/login-action@v4` | `v4.1.0` | `runs.using: node24` |
| `docker/metadata-action@v5` | `docker/metadata-action@v6` | `v6.0.0` | `runs.using: node24` |

### `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`

GitHub's changelog explicitly documents `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true` as an accepted way to test Node 24 ahead of the default migration. It can be set as workflow `env` or as an environment variable on the runner machine.

Observed implications:

- It is an official runner-level compatibility/testing switch, not a Docker-action-specific release migration.
- It forces JavaScript actions to execute with Node 24 even when their `action.yml` declares an older runtime.
- It can reduce immediate Node 20 deprecation warnings without changing action versions.

Risks / limitations:

- It may run actions on a runtime version they did not declare and may not have tested for their pinned major version.
- It changes behavior globally for JavaScript actions in the workflow/job scope, not just Docker's actions, if placed at workflow/job level.
- It depends on runner support for Node 24. Docker's Node 24 major releases document a minimum Actions Runner version of `v2.327.1`; GitHub's hosted runners should be current, but older self-hosted runners may fail.
- GitHub notes Node 24 is incompatible with macOS 13.4 and lower. Houfeng's relevant Docker workflow uses `ubuntu-latest` and `ubuntu-24.04-arm`, so that macOS-specific risk does not apply to this workflow.
- The switch is transitional. The durable migration path is to use action releases that declare Node 24.

### Recommended Minimal Change for Houfeng Workflows

For Houfeng's GitHub-hosted Ubuntu Docker publishing workflow, the minimal durable change is to update the Docker official actions to their Node 24 major versions instead of adding `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`:

```yaml
docker/setup-buildx-action@v4
docker/login-action@v4
docker/build-push-action@v7
docker/metadata-action@v6
```

This matches each Docker action's own Node 24 release line and avoids relying on a global runtime-forcing environment variable. Since the workflow uses GitHub-hosted Ubuntu runners (`ubuntu-latest`, `ubuntu-24.04-arm`), the documented minimum runner requirement should be satisfied by GitHub's hosted infrastructure.

If the repository needed a short-term deprecation-warning mitigation before upgrading action majors, `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true` would be accepted by GitHub, but it should be treated as a temporary compatibility test/bridge rather than the final workflow state.

### Related Specs

No `.trellis/spec/**/*.md` file was required to answer this query. The relevant internal source is the GitHub Actions workflow file listed above.

## Caveats / Not Found

- Research used GitHub release metadata and raw `action.yml` files fetched on 2026-05-17; action versions may advance after this date.
- This research did not perform a full behavioral diff between the old and new Docker action majors. It only verifies that newer majors exist and that their declared runtime is Node 24.
- Docker action release notes found for the Node 24 major bumps mention the runtime/runner requirement but may include other changes in linked pull requests or dependency updates not exhaustively analyzed here.
