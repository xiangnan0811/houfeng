# VPS detail refactor audit scope and validation inventory

## Purpose and evidence status

This planning inventory defines what the later audit must independently verify.
It is not a completion verdict and does not carry historical pass claims forward
as current evidence. The primary session compiled it from the live repository,
current Trellis specifications, current runner scripts, Git history, and the
archived parent handoff after two bounded research-agent attempts did not return
an artifact. The approved execution phase must still use an independent
`trellis-check` reviewer and must reproduce every finding and gate result.

Planning baseline: `origin/main` and `main` were `08730e79` when this task was
created. At execution start, fetch remote metadata and freeze one explicit review
commit. Never mix code evidence from different commits.

## Authoritative program map to recheck

The archived parent is
`.trellis/tasks/archive/2026-08/07-13-vps-detail-experience-design`.
Its `task.json` identifies twelve functional children plus one non-functional
closeout child. The following selected and protected-main merge SHAs are historical
claims from the current handoff and must be checked against GitHub and Git ancestry:

| Capability owner | Archived child | PR | Selected | Main merge |
| --- | --- | ---: | --- | --- |
| Platform foundation | `07-14-vps-records-platform-foundation` | #394 | `7858b30c` | `2cbeb1bb` |
| Records core | `07-14-vps-records-core` | #397 | `ba5f2d8d` | `2279a7fd` |
| Attachments/storage | `07-14-vps-records-attachments-storage` | #400 | `1887821c` | `78bf44c1` |
| Evidence platform | `07-14-vps-records-evidence-platform` | #408 | `9ac3e255` | `6a0122a7` |
| Markdown workspace | `07-14-vps-records-markdown-workspace` | #413 | `199d2a2b` | `d41c8630` |
| Search center | `07-14-vps-records-search-center` | #416 | `c01663df` | `bcd8e53a` |
| Activity/overview | `07-14-vps-records-activity-overview` | #422 | `b9698ffa` | `b3901e5f` |
| Comparison workbench | `07-14-vps-records-comparison-workbench` | #423 | `9924eae0` | `aacb9c50` |
| Collaboration | `07-14-vps-records-collaboration` | #410 | `c6742c3c` | `a3137864` |
| Portability/migration | `07-14-vps-records-portability-migration` | #425 | `808fda62` | `9e910d7c` |
| Archive/restore fidelity | `08-21-vps-records-archive-restore-fidelity` | #428 | `7ce5dbdd` | `c7081519` |
| Integration rollout | `07-14-vps-records-integration-rollout` | #433 | `1a440e86` | `79f62aac` |

Additional delivery claims to recheck:

- musl/S3 correction PR #436;
- overview management implementation PR #438, selected `7e9080f2`, protected-main
  merge `af23844a`;
- current-authority final audit PR #441 and parent archive PR #442;
- later receipt/current-main PR #443 and all relevant commits after `v0.75.0`;
- `v0.75.0` tag `ab1ad7cd`, release assets and published image manifest actually
  contain the reviewed product merge;
- each relevant PR's required checks and the corresponding post-merge main CI,
  without treating a task-only archive commit as a product release.

The review change-set starts at the first functional merge's first parent
(`2cbeb1bb^1`) and continues through the frozen review commit. Current consumers,
production wiring and later fixes must be read in addition to this historical diff.

## Current code and contract surface

- Web: `/vps/:id` capability gate; overview and lazy legacy fallback; overview
  composition; facts, decision, subscription, cancellation and archive actions;
  VPS activity, records and evidence routes; Records workspace, search, Markdown,
  comparison, collaboration, portability and archive/restore surfaces.
- API and backend: `vpsoverview`, `activity`, `records`, `recordsearch`, `evidence`,
  `attachments`, `recordmarkdown`, `recordauth`, collaboration/notification,
  `portability`, `recordbackup`, `recordrestore`, `recordreadiness`, HTTP handlers,
  router, config and bootstrap.
- Persistence/runtime: migrations `0052` through `0059`, hand-written SQL, ACL and
  admission, projections/cursors/watermarks, Blob local/S3 parity, async workers,
  official backup/restore and production readiness wiring.
- Shared VPS authorities that directly feed the page: IP-quality partial/unknown
  state and VPS-scoped subscription cost. Source failure must not be rendered as a
  negative risk or as an empty subscription fact.

## Permanent-delete boundary

Single-record irreversible permanent deletion is explicitly outside this delivery
and must remain unimplemented, disabled and fail-closed. Ordinary record
archive/restore and whole disposable-environment rebuild are different semantics.

Current claims that require live-code verification:

- production uses `handlers.RecordDeletions(nil)`;
- Records and `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED` default false, and an
  attempted true production configuration is rejected;
- seven readiness members remain absent:
  `deletion.record_markdown_client`, `deletion.record_comparison`,
  `recovery.record_search`, `recovery.record_collaboration`,
  `recovery.record_portability`, `backup.orchestration`, `restore.replay`;
- backup/restore registry pairing is absent as an unavailable capability;
- existing helper packages, tests or CLIs must not be mistaken for production
  transport/readiness availability.

If any of these boundaries has silently opened, that is a finding. Their intended
absence is not a missing-feature finding.

## Accepted deferrals

These three items do not block the current scope unless their trigger is now true,
or current code/docs falsely claim they were delivered:

1. Activity group-granted digest. Trigger: viewer authorization expands beyond the
   current project visibility digest.
2. Comparison sticky body-row headers at 390 px. Trigger: reproducible mobile
   positioning/usability failure; named horizontal scroll and semantic row headers
   alone do not claim sticky body rows.
3. A 4 GiB / 512 MiB, fixed-arrival, three-round mixed-load harness. Trigger: an
   approved formal capacity SLO/hardware target. The current bounded capacity runner
   is useful evidence but does not prove this deferred harness.

## Exact fresh verification inventory

Prerequisites: Go toolchain, Node 22.x, npm lockfile install support, Playwright
Chromium, Docker, PostgreSQL 16 fixture ability, and MinIO image/network support.
Record versions and stop on missing infrastructure or any `--- SKIP:` from strict
runners. Use temporary/ignored outputs only and check Git status after each gate.

Read-only formatting and full Go gate:

```bash
git ls-files '*.go' -z | xargs -0 gofmt -l
go vet ./agent/... ./cmd/... ./db/... ./internal/...
go test ./agent/... ./cmd/... ./db/... ./internal/... -count=1
```

Web and complete browser contracts under Node 22:

```bash
make verify-web
npm --prefix web run test:e2e
scripts/run-records-browser.sh
```

Strict Records gates:

```bash
scripts/run-records-security.sh
scripts/run-records-capacity.sh --profile local
scripts/run-records-integration.sh --profile local
scripts/run-records-integration.sh --profile s3
scripts/run-records-recovery.sh --profile local --all
scripts/run-records-recovery.sh --profile s3 --all
```

Runner semantics verified from current scripts:

- `make verify-web` checks the Node toolchain, performs `npm ci`, lint, coverage,
  production build, bundle budget and CSS analysis;
- `run-records-browser.sh` executes the bounded Playwright corpus and rejects test
  fixtures/helpers in `web/dist`;
- security, capacity, integration and recovery wrappers reject skipped tests;
- capacity `--profile local` brings up a real PostgreSQL fixture with the bounded
  scale; it is not the deferred mixed-load harness;
- integration/recovery require Docker; S3 profiles start an isolated MinIO; their
  trap may remove only runner-created randomized containers/workspaces;
- recovery requires `--all` and both local/S3 profiles report permanent deletion
  disabled.

Before full gates, run focused owner tests and focused Vitest/Playwright suites from
the execution plan so failures remain attributable. Browser inspection must use the
production preview with controlled local fixtures at 1440x1000 and 390x900, exercise
loading/empty/error/revoked/submitting states and all five management dialogs, and
cover keyboard, focus return, overflow, touch targets and Axe serious/critical.

## Planning corrections applied or required

- `implement.md` correctly replaces mutating `make verify-go`/`go fmt` with
  `gofmt -l`, `go vet` and `go test`.
- The exact script flags above match the current runner usage blocks.
- Both manifests include the current IP-quality, subscription-cost, Records Web,
  database, error and logging contracts; large injected specs must be opened and
  read completely from disk by the reviewer.
- The plan correctly keeps product fixes, deployments, PR mutation, staging data,
  permanent-delete implementation and the three deferrals out of scope.
- No further planning correction is required before user approval. Fresh GitHub,
  release, test and browser evidence remains execution work and must not be claimed
  by this inventory.
