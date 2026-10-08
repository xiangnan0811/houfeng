package recordreadiness

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRequiredSecurityCorpusTestsAreClosedOrderedAndPresent(t *testing.T) {
	t.Parallel()

	want := []string{
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
	got := RequiredSecurityCorpusTests()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RequiredSecurityCorpusTests() = %#v, want %#v", got, want)
	}
	got[0] = "tampered"
	if fresh := RequiredSecurityCorpusTests(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("RequiredSecurityCorpusTests() after caller mutation = %#v", fresh)
	}

	root := filepath.Join("..", "..", "..")
	present, err := findRepositoryTestNames(root, want)
	if err != nil {
		t.Fatalf("walk tests: %v", err)
	}
	for _, testName := range want {
		if present[testName] == "" {
			t.Fatalf("security corpus test %s is missing from the tree", testName)
		}
	}
}

func TestScanContentSafeRejectsLeakCorpus(t *testing.T) {
	t.Parallel()

	if err := ScanContentSafe([]byte("ok")); err != nil {
		t.Fatalf("ScanContentSafe(ok) error = %v", err)
	}
	if err := ScanContentSafe([]byte("postgres://houfeng:secret@db/houfeng")); !errors.Is(err, ErrContentLeak) {
		t.Fatalf("ScanContentSafe(url) error = %v, want ErrContentLeak", err)
	}

	want := []string{
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
	got := SecurityLeakTokens()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SecurityLeakTokens() = %#v, want %#v", got, want)
	}
	got[0] = "tampered"
	if fresh := SecurityLeakTokens(); !reflect.DeepEqual(fresh, want) {
		t.Fatalf("SecurityLeakTokens() after caller mutation = %#v", fresh)
	}

	for _, leaked := range want {
		if err := ScanContentSafe([]byte("prefix " + leaked + " suffix")); !errors.Is(err, ErrContentLeak) {
			t.Fatalf("ScanContentSafe(%q) error = %v, want ErrContentLeak", leaked, err)
		}
	}
}

func TestRecordsSecurityScriptOwnsInventoriedCorpus(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "run-records-security.sh"))
	if err != nil {
		t.Fatalf("read run-records-security.sh: %v", err)
	}
	script := string(payload)
	if err := ScanContentSafe(payload); err != nil {
		t.Fatalf("run-records-security.sh leaked: %v", err)
	}
	for _, want := range append([]string{
		"go test",
		"--- SKIP:",
		"internal/center/http/handlers",
		"internal/center/recordmarkdown",
		"internal/center/portability",
		"internal/center/attachments",
		"internal/center/evidence",
		"internal/center/recordreadiness",
	}, RequiredSecurityCorpusTests()...) {
		if !strings.Contains(script, want) {
			t.Fatalf("run-records-security.sh missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"AdmissionGateFunc(",
		"houfeng-record-platform-admin",
		"postgres://houfeng",
		"command -v docker",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("run-records-security.sh must not contain %q", forbidden)
		}
	}
}
