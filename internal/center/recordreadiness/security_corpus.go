package recordreadiness

import (
	"fmt"
	"strings"
)

var requiredSecurityCorpusTests = []string{
	"TestRecordActionsHandlerUsesTrustedActorAndResponseAllowlist",
	"TestDocumentMarkdownV1RejectsHostileModels",
	"TestDocumentMarkdownV1SharedHostileCommentCasesRemainRejected",
	"TestRenderSafeHTMLEscapesTextAndDropsScripts",
	"TestReadArchiveV1BoundedHostileCorpus",
	"TestPortabilityImportRejectsHostileAndUntrustedMembers",
	"TestDownloadResponseMetadataUsesSafeFilenameAndAllowlistedMediaType",
	"TestIsolatedDerivedPDFCommandDisablesNetworkAndProxy",
	"TestContentDeliveryDoesNotStartWriteAfterBackgroundRenewalRevokes",
	"TestPortabilityOpenContentStopsAfterRevoke",
	"TestRedactionRejectsHostileSecretContentCorpus",
	"TestRecordDraftsHandlerRejectsUntrustedPayloadAndMapsNoLeakErrors",
}

var securityLeakTokens = []string{
	"# title",
	"comment body",
	"evidence payload",
	"attachment bytes",
	"archive content",
	"password=secret",
	"postgres://",
	"DATABASE_URL",
	"houfeng:secret",
	"filename.md",
	`"note"`,
}

func RequiredSecurityCorpusTests() []string {
	return append([]string(nil), requiredSecurityCorpusTests...)
}

func SecurityLeakTokens() []string {
	return append([]string(nil), securityLeakTokens...)
}

func ScanContentSafe(payload []byte) error {
	text := string(payload)
	for _, leaked := range securityLeakTokens {
		if strings.Contains(text, leaked) {
			return fmt.Errorf("%w: %s", ErrContentLeak, leaked)
		}
	}
	return nil
}
