package portability

import (
	"errors"
	"testing"
)

func TestScanImportedMarkdownAllowsDocumentURLsAndEnglishVerbs(t *testing.T) {
	t.Parallel()

	err := scanImportedMember(ArchiveEntry{
		Path:           "records/rec_source01/document.md",
		Classification: ArchiveClassMarkdown,
		Payload: []byte("# Disk notes\n\nSee https://example.com and please update the note.\n" +
			"do not delete this. metadata: kept.\n"),
	})
	if err != nil {
		t.Fatalf("scanImportedMember(markdown with URL/verbs) = %v", err)
	}
}

func TestScanImportedMarkdownRejectsActiveSchemes(t *testing.T) {
	t.Parallel()

	for _, payload := range []string{
		"# A\n\n[run](javascript:alert(1))\n",
		"# A\n\nSee file://etc/passwd\n",
		"# A\n\n![](data:text/html;base64,PHNjcmlwdD4=)\n",
	} {
		if err := scanImportedMember(ArchiveEntry{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte(payload),
		}); !errors.Is(err, ErrUntrustedImportContent) {
			t.Fatalf("scanImportedMember(%q) = %v, want ErrUntrustedImportContent", payload, err)
		}
	}
}

func TestScanImportedJSONRejectsOnlyTopLevelTrustFields(t *testing.T) {
	t.Parallel()

	if err := scanImportedMember(ArchiveEntry{
		Path: "records/rec_source01/evidence/evs_ok.json", Classification: ArchiveClassEvidenceJSON,
		Payload: []byte(`{"schema":"comparison.result/v1","kind":"comparison.result","nested":{"url":"https://example.com"}}`),
	}); err != nil {
		t.Fatalf("nested url should pass top-level scan: %v", err)
	}
	if err := scanImportedMember(ArchiveEntry{
		Path: "records/rec_source01/grant.json", Classification: ArchiveClassEvidenceJSON,
		Payload: []byte(`{"authorization":"admin","role":"root"}`),
	}); !errors.Is(err, ErrUntrustedImportContent) {
		t.Fatalf("top-level authorization = %v", err)
	}
}
