# Agent command boundary hardening

## Goal

Close gap #23 by making the agent command/Docker boundary explicit, test-backed, and aligned with the post-V1 product plan. The task should confirm that the agent remains a thin observe/buffer/sync/apply-plan process: command execution is limited to compile-time whitelist entries, and Docker usage is limited to best-effort local container facts.

## What I already know

- `houfeng_codex_下一步开发计划.md` says post-V1/MVP should pause non-essential Agent expansion and keep asset/provider/subscription decisions out of the Agent.
- `docs/release/asset-ledger-roadmap-completion.md` marks Asset Ledger Task 1-8 as complete except real-data execution, which is user-data-dependent and intentionally deferred.
- `docs/release/v1-gap-checklist.md` gap #23 is still Open: the repo already has whitelisted node actions and best-effort Docker facts, so docs must not summarize the current system as "agent does not execute commands" or "agent does not run Docker".
- `agent/exec/whitelist.go` defines a compile-time whitelist including `docker_ps`.
- `agent/exec/runner.go` uses `exec.CommandContext` with fixed bin/args passed in by the lookup path and does not invoke a shell.
- `agent/containersample/sample.go` uses `docker ps` and `docker stats --no-stream` with fixed arguments and returns `nil, nil` when Docker is missing or unavailable.
- `agent/runtime/runtime.go` executes only whitelisted pending actions and attaches container facts only when available.
- Existing tests cover baseline whitelist lookup, runner timeout/output truncation, Docker unavailable behavior, Docker stats fallback, and runtime container attachment.

## Requirements

- Preserve the current product boundary: no arbitrary scripts, no user-supplied command arguments, no Docker control/orchestration, no Agent-side asset business logic.
- Harden the command whitelist API so callers cannot mutate internal command definitions through returned argument slices.
- Add tests that prove:
  - all whitelisted commands have stable IDs, binaries, and fixed arguments;
  - mutating a `Lookup` result cannot change later lookup results;
  - runner calls do not invoke a shell implicitly;
  - Docker sampling invokes only the expected fixed `docker ps` / `docker stats` argument shapes;
  - Docker absence/unavailability stays best-effort and does not fail host sample collection.
- Update `.trellis/spec/backend/` with the final command/Docker boundary so future tasks do not reopen ambiguous Agent scope.
- Update release/roadmap docs so gap #23 is Closed with concrete evidence.

## Acceptance Criteria

- [x] `agent/exec` tests cover immutable whitelist lookup and no implicit shell execution.
- [x] `agent/containersample` tests cover fixed Docker CLI argument shape and best-effort skip semantics without relying on the developer machine's real Docker installation.
- [x] `agent/runtime` behavior remains a thin composition layer and still ignores unknown command IDs without blocking sync.
- [x] `.trellis/spec/backend/directory-structure.md` no longer says command/Docker product boundary remains unresolved.
- [x] `docs/release/v1-gap-checklist.md` marks gap #23 Closed with evidence and explicit non-goals.
- [x] `docs/release/next-phase-plan.md` no longer lists command_id durability as a remaining follow-up and accurately states the Docker/exec boundary.
- [x] Go verification passes with the repo-local temp directories.

## Out of Scope

- Adding new command IDs or changing current command semantics.
- Allowing user-provided command arguments or shell snippets.
- Docker orchestration, lifecycle operations, compose/kubernetes support, or container management UI.
- Full command history/audit log, multi-action queue, or action retry semantics.
- Real host Docker smoke execution.
- Real 40+ VPS data import/dry-run.
- Release/publish workflow.

## Technical Notes

- Relevant code paths:
  - `agent/exec/whitelist.go`
  - `agent/exec/runner.go`
  - `agent/containersample/sample.go`
  - `agent/runtime/runtime.go`
  - `internal/contracts/agentapi/types.go`
  - `internal/center/store/sync_batches.go`
- Relevant docs/specs:
  - `houfeng_codex_下一步开发计划.md`
  - `docs/release/asset-ledger-roadmap-completion.md`
  - `docs/release/next-phase-plan.md`
  - `docs/release/v1-gap-checklist.md`
  - `.trellis/spec/backend/directory-structure.md`
  - `.trellis/spec/backend/database-guidelines.md`
- No external research is needed for this task; the boundary is defined by current repo implementation and product plan constraints.

## Definition of Done

- Tests added or updated for the hardened boundary.
- `make verify-go` passes.
- `git diff --check` passes.
- Trellis task is archived after commit.
- Changes land through feature branch PR, CI green, squash merge, and local `main` sync.
