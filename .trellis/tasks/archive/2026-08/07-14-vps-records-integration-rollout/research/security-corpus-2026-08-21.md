# Security corpus run 2026-08-21

`./scripts/run-records-security.sh` ran the compile-owned inventory in
`recordreadiness.RequiredSecurityCorpusTests` plus Child 11 leak-scan tests.
No Docker. `TMPDIR=/tmp`. Any `--- SKIP:` is a failure.

Inventoried owning-child tests (all ok):

- authorization / response allowlist:
  `TestRecordActionsHandlerUsesTrustedActorAndResponseAllowlist`
- XSS / Markdown:
  `TestDocumentMarkdownV1RejectsHostileModels`
  `TestDocumentMarkdownV1SharedHostileCommentCasesRemainRejected`
  `TestRenderSafeHTMLEscapesTextAndDropsScripts`
- MIME / archive:
  `TestReadArchiveV1BoundedHostileCorpus`
  `TestPortabilityImportRejectsHostileAndUntrustedMembers`
  `TestDownloadResponseMetadataUsesSafeFilenameAndAllowlistedMediaType`
- network isolation:
  `TestIsolatedDerivedPDFCommandDisablesNetworkAndProxy`
- permission-revoke streaming:
  `TestContentDeliveryDoesNotStartWriteAfterBackgroundRenewalRevokes`
  `TestPortabilityOpenContentStopsAfterRevoke`
- secret / draft leak:
  `TestRedactionRejectsHostileSecretContentCorpus`
  `TestRecordDraftsHandlerRejectsUntrustedPayloadAndMapsNoLeakErrors`

Child 11 assembly scan (`ScanContentSafe`) covered readiness Encode, backup
manifest Encode, profile-report Encode, `EncodeExternalCopies`, both
integration/recovery scripts, `run-records-security.sh`, and
`cmd/houfeng-backup` / `cmd/houfeng-restore` sources.

No new assembly leak. No new domain defect from this unit corpus. The Task 4
Alpine `0058` `blob_key` CHECK defect remains returned to the portability
owner and is not re-run here.
