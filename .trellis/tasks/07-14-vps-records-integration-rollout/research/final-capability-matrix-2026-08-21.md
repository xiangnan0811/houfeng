# Final capability matrix 2026-08-21

Source: `newProductionRecordReadinessRegistry` +
`recordreadiness.RequiredCapabilityKinds` + recovery profile report.

`permanent_delete`: **disabled**

Exact reasons (any one is enough; all currently apply):

1. `deletion.record_markdown_client` is `missing` (name only; no adapter).
2. `deletion.record_comparison` is `missing` (name only; no adapter).
3. `recovery.record_search` is `missing`.
4. `recovery.record_collaboration` is `missing`.
5. `recovery.record_portability` is `missing`.
6. `backup.orchestration` and `restore.replay` are both absent
   (pairing rule: both present or both absent). Packages exist but are not
   wired into the production registry.
7. Production HTTP stays `handlers.RecordDeletions(nil)`.

Present in production construction when dependencies construct:

- deletion: `record_core`, `record_attachments`, `record_evidence`,
  `record_search`, `record_activity_projection`, `record_collaboration`,
  `record_portability`
- recovery: `record_core`, `record_attachments`, `record_evidence`,
  `record_activity_projection`
- authority: `deployment_membership` + `source_deletion_witness` when the
  named gate is non-nil

Child 11 does not invent the missing markdown/comparison deletion adapters
or the missing search/collaboration/portability recoveries.
