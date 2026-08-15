# Research: Child 4 plan audit against current main

- Query: check the existing Child 4 plan for drift after Children 1-3 and the Trellis 0.6.14 upgrade.
- Date: 2026-08-10

## Findings

1. Child 1 (`07-14-vps-records-platform-foundation`), Child 2
   (`07-14-vps-records-core`) and Child 3
   (`07-14-vps-records-attachments-storage`) are archived completed tasks.
   Their migrations are present as `0051`, `0052` and `0053` on the current
   main baseline. `0054` is therefore the next free migration number.
2. The current APP ACL registry is closed-world. A Child 4 migration without
   one exact `0054` fragment, managed-object set, privilege set and fresh/exact
   repeat admission tests fails before the migration transaction begins.
3. The child plan still says `Codex inline`; the project now uses explicit
   `codex.dispatch_mode: auto` with the bundled native workflow. Dispatch
   prompts must begin with `Active task: .trellis/tasks/07-14-vps-records-evidence-platform`.
4. `implement.jsonl` and `check.jsonl` are seed-only, so the task is not ready
   for `task.py start` under the 0.6.14 planning gate.
5. The parent §12.2 registry names are authoritative. The child plan's
   `monitoring_timeseries/v1` and `monitoring_event/v1` labels must be aligned
   to `monitoring.host/v1`, `monitoring.probe/v2` and `monitoring.event/v2`.
   Asset history is a source/activity family, not an additional registry kind
   in the parent contract.
6. Production Records handlers intentionally receive a nil admission gate
   until Child 10. The child must not wire a permissive fallback or claim live
   capture/save behavior.

## Planned correction

Curate the manifests with the backend/web indexes and the source map plus
precondition evidence, update the execution wording and kind names, then run
`task.py validate`, review the complete planning artifacts, and only then
activate the task.
