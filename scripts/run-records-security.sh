#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
workspace=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-records-security.XXXXXX")

cleanup() {
  status=$?
  if [ "${HOUFENG_RECORDS_KEEP_WORKSPACE-}" != "1" ]
  then
    rm -rf "$workspace"
  fi
  exit "$status"
}
trap cleanup EXIT

stdout_file="$workspace/child-stdout.log"
stderr_file="$workspace/child-stderr.log"

set +e
(
  cd "$root"
  go test \
    ./internal/center/http/handlers \
    ./internal/center/recordmarkdown \
    ./internal/center/portability \
    ./internal/center/attachments \
    ./internal/center/evidence \
    ./internal/center/recordreadiness \
    ./internal/center/recordrestore \
    -run 'TestRecordActionsHandlerUsesTrustedActorAndResponseAllowlist|TestDocumentMarkdownV1RejectsHostileModels|TestDocumentMarkdownV1SharedHostileCommentCasesRemainRejected|TestRenderSafeHTMLEscapesTextAndDropsScripts|TestReadArchiveV1BoundedHostileCorpus|TestPortabilityImportRejectsHostileAndUntrustedMembers|TestDownloadResponseMetadataUsesSafeFilenameAndAllowlistedMediaType|TestIsolatedDerivedPDFCommandDisablesNetworkAndProxy|TestContentDeliveryDoesNotStartWriteAfterBackgroundRenewalRevokes|TestPortabilityOpenContentStopsAfterRevoke|TestRedactionRejectsHostileSecretContentCorpus|TestRecordDraftsHandlerRejectsUntrustedPayloadAndMapsNoLeakErrors|TestScanContentSafeRejectsLeakCorpus|TestChild11AssemblyArtifactsRejectLeakCorpus|TestRequiredSecurityCorpusTestsAreClosedOrderedAndPresent|TestRecordsSecurityScriptOwnsInventoriedCorpus' \
    -count=1
) >"$stdout_file" 2>"$stderr_file"
command_status=$?
set -e

if grep -Fq -- '--- SKIP:' "$stdout_file" "$stderr_file"
then
  printf 'records security command skipped a test\n' >&2
  exit 1
fi

if [ "$command_status" -ne 0 ]
then
  cat "$stdout_file"
  cat "$stderr_file" >&2
  exit "$command_status"
fi

cat "$stdout_file"
