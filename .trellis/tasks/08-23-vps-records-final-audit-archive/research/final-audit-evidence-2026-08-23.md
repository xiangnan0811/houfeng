# Final audit evidence — 2026-08-23

This report records independently rechecked evidence for the bounded final-audit
documentation change. It does not authorize or claim PR delivery, protected-main
merge, task completion, or archive execution.

## Repository boundary

- Branch: `codex/vps-records-final-audit-archive`.
- `HEAD` and `origin/main` at audit start:
  `62f975c535f076ef7c322a07e25c4c158a9efe34`.
- Hooks: `core.hooksPath=.githooks`.
- Active task status: `in_progress`; branch metadata set with
  `python3 ./.trellis/scripts/task.py set-branch
  .trellis/tasks/08-23-vps-records-final-audit-archive
  codex/vps-records-final-audit-archive`.
- The only pre-existing diff was the active task status transition from
  `planning` to `in_progress`; it is preserved together with the requested branch
  metadata.

## Overview entry gate

Live GitHub queries and Git ancestry checks established:

| Evidence | Live result |
|---|---|
| Implementation PR | [#438](https://github.com/xiangnan0811/houfeng/pull/438) `MERGED`; selected `7e9080f208a5f1f5cce7e563f5030b9d068629de`; merge `af23844adc82ce97e6815a3dbd8706f7fdab10e8` |
| PR checks | 7/7 `SUCCESS`, workflow run `32637157947` |
| Main inclusion | `git merge-base --is-ancestor af23844adc82ce97e6815a3dbd8706f7fdab10e8 origin/main` returned success |
| Post-merge main CI | Run `32637395760`, head `af23844adc82ce97e6815a3dbd8706f7fdab10e8`, conclusion `success`; seven jobs successful |
| Release | [v0.75.0](https://github.com/xiangnan0811/houfeng/releases/tag/v0.75.0), tag target `ab1ad7cdaab4a7ee57b782a3a9a45e5074b591bd`; overview merge is an ancestor |
| Archive PR | [#440](https://github.com/xiangnan0811/houfeng/pull/440) `MERGED`; selected `6c0901b58bb523b7d0c86d004010bda7d8dc58f0`; merge/current main `62f975c535f076ef7c322a07e25c4c158a9efe34` |
| Archive checks | 7/7 `SUCCESS`, workflow run `32637979055` |
| Post-archive main CI | Run `32638216017`, head `62f975c535f076ef7c322a07e25c4c158a9efe34`, conclusion `success`; seven jobs successful |

The seven-job main matrices contained Go, Web, Web Browser, Docker Image, and
PostgreSQL 16.0/16.6/16.12 jobs.

Direct code/test inspection established:

- `web/src/pages/VPSDetailPage.tsx` selects `VPSOverviewRoute` when the overview
  capability is available, with lazy `LegacyVPSDetailPage` fallback.
- `VPSOverviewRoute` mounts `VPSOverviewManagementActions`.
- `VPSOverviewManagementActions.tsx` calls real facts, decision, subscription,
  cancellation preview/apply and archive review/archive APIs. Successful writes
  refresh overview state; cancellation blockers and archive exact-name
  confirmation remain enforced; route-switch stale responses and duplicate
  submits are guarded.
- `VPSDetailPage.test.tsx` covers each action, retry/failure distinctions,
  confirmation/blocker contracts, navigation and legacy fallback.
- `VPSOverviewManagementActions.test.tsx` covers stale route mutation and
  duplicate-submit locking.

The archived overview child's recorded focused/full/browser counts were also
cross-checked against its immutable research evidence: 19 focused tests,
`make verify-web` at 192 files / 1,254 tests, and five Playwright tests covering
desktop/390px, keyboard/focus and Axe. Those historical artifacts were read but
not edited.

## Twelve functional children

The original parent lists exactly twelve functional children plus one later
closeout coordination child. All twelve functional tasks are archived under
`.trellis/tasks/archive/2026-08/`, and every archive `task.json` has
`status=completed`. Live `gh pr view` results reported each PR below as `MERGED`;
each listed main merge passed a fresh `git merge-base --is-ancestor ...
origin/main` check.

| Parent-list order | PR | Selected | Main merge |
|---:|---:|---|---|
| 1 | #394 | `7858b30c1b4a48eac3d926caababc153790982d0` | `2cbeb1bbc28faa97b07320086304a0e5ac8f4826` |
| 2 | #397 | `ba5f2d8d21cc09d1a47318b6ecbe1239aa60a331` | `2279a7fdee837fdab9c714e12b0651367e0a0875` |
| 3 | #400 | `1887821ceed82cc1e01ec829be0c70923cf5203c` | `78bf44c16e5dbd93f5a238d1442021702d000f2d` |
| 4 | #408 | `9ac3e255bcb62389ee9d6fde1fe3d294d9e59bd9` | `6a0122a7fd6aa86145e4e7d6ac70609ab8f59f22` |
| 5 | #413 | `199d2a2b71ac14b18177aa3f11f55cc1a29404a2` | `d41c8630b7d3d9e2b18a97caf6ca2618a0765f30` |
| 6 | #416 | `c01663df684665d94b1378a63c01592afe91637c` | `bcd8e53aad2b31bb5c3adca432291d0241f05f2a` |
| 7 | #422 | `b9698ffa693d45197e5357bafc703ee3f4d7e9f0` | `b3901e5f1617fb0d9eb91999687f7a91f382b85a` |
| 8 | #423 | `9924eae09e3f36bfeb3d41e3bd01fec2a465bc56` | `aacb9c5096cb484fdfddb3ce78056fc834ed3569` |
| 9 | #410 | `c6742c3c27d33a8c065b6471255c9bea2269a5e5` | `a3137864e475bec27efb7c173099a2d2a9c57701` |
| 10 | #425 | `808fda62af7341e10145a067dc49975d0609628d` | `9e910d7c4c2b743e930de051e9590f6e5171d744` |
| 11 (original Child 12: archive/restore fidelity) | #428 | `7ce5dbdd61b27b0979c7e37cabe0c55b65d5030a` | `c708151981eda451cf0f064e708fb71dea4c24e2` |
| 12 (original Child 11: integration) | #433 | `1a440e86bd640c77ee4c4fadae2c42cca7bbff9f` | `79f62aac20dadd945d0159a1ca2d79076054c68a` |

The named archive paths and child-to-PR mapping are preserved in the current
parent handoff. No archived child artifact is part of this change.

## Permanent-delete closing boundary

Current source/config inspection found:

- `cmd/houfeng-center/bootstrap.go`: `handlers.RecordDeletions(nil)` is the
  production HTTP dependency; `bootstrap_test.go` asserts it remains nil.
- `internal/center/config/config.go`: Records default false; permanent deletion
  defaults false and true is rejected; Comparison and Portability default false.
- Production deletion adapters present: core, attachments, evidence, search,
  activity, collaboration, portability.
- Production recovery adapters present: core, attachments, evidence, activity.
- Production readiness has no backup/restore members and therefore has the pair
  absent rather than half-wired.

Exactly seven required readiness rows are missing:

1. `deletion.record_markdown_client`
2. `deletion.record_comparison`
3. `recovery.record_search`
4. `recovery.record_collaboration`
5. `recovery.record_portability`
6. `backup.orchestration`
7. `restore.replay`

The nil HTTP handler is separate from that seven-row count. Together these facts
prove the feature is unimplemented/unavailable and cannot be enabled by flags.
They are now an accepted deferral boundary, not an instruction to patch owners.

The user decision is authoritative: single-record irreversible permanent deletion
is abandoned for this scope. A future trigger—real external users, a compliance
deletion promise, long-lived managed backups, formal disaster recovery, or a
product request for one-record non-recoverability—requires a new task.

## Lifecycle semantics

- Ordinary record archive/restore is live and reversible. The Web records API,
  HTTP lifecycle mapping, domain lifecycle values and service/store/handler tests
  all cover archive and restore.
- Deleting/rebuilding the whole disposable online test environment is a separate
  deployment-level action.
- Irreversibly removing one record across online storage, managed copies and
  official restore paths is the abandoned permanent-delete capability.

## Three accepted deferrals

1. Activity group-granted digest: `activity/auth_filter.go` documents and enforces
   one project visibility digest; service and PostgreSQL tests keep restricted
   revisions hidden. Status: unimplemented. Trigger: viewer permissions extend
   beyond project digest.
2. Comparison sticky row headers at 390px: the matrix has a named horizontal
   scroll region and semantic row headers, but no sticky body-row-header CSS or
   test. Status: unimplemented. Trigger: an actual 390px positioning/usability
   issue.
3. Mixed-load harness: `scripts/run-records-capacity.sh` is focused test/optional
   PostgreSQL verification, not the specified 4 GiB container, 512 MiB aggregate,
   15-minute fixed-arrival, three-round workload;
   `scripts/run-comparison-capacity.sh` does not exist. Status: unverified.
   Trigger: formal capacity SLO, target hardware, or continuous regression
   benchmark.

## Local validation record

The following local checks passed on the documentation branch:

- `python3 ./.trellis/scripts/task.py validate
  .trellis/tasks/07-13-vps-detail-experience-design`
- `python3 ./.trellis/scripts/task.py validate
  .trellis/tasks/08-23-vps-records-parent-closeout`
- `python3 ./.trellis/scripts/task.py validate
  .trellis/tasks/08-23-vps-records-final-audit-archive`
- `python3 ./.trellis/scripts/task.py validate
  .trellis/tasks/archive/2026-08/08-23-vps-overview-management-actions`
- `TMPDIR=/tmp go test ./internal/center/config -run
  'TestLoadRecordPlatformMode|TestLoadCenterConfigComparisonEnabledRequiresRecords|TestLoadCenterConfigPortabilityEnabledRequiresRecords'
  -count=1`
- `TMPDIR=/tmp go test ./cmd/houfeng-center -run
  'TestBootstrapWiresRecordReadinessRegistry|TestBootstrapCenterUsesRuntimeAdmissionWhenRecordPlatformEnabled'
  -count=1`
- `git diff --check`
- Reference/path/status inspection confirmed the original parent is `planning`,
  the closeout parent is `planning`, the overview child is archived/completed,
  and the active final-audit child is `in_progress` on the recorded branch.
- The complete tracked diff and the full new evidence file were reviewed. A
  `git status --porcelain` allowlist check found no path outside the explicitly
  owned task artifacts; `.trellis/tasks/archive/**` status/diff is empty. Product
  code, tests, migrations, config, deploy, CI and specs therefore have zero diff.

Protected delivery, post-merge verification and the archive sequence remain open
for the root session.
