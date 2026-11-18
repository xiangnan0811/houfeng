package portability

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"houfeng/internal/center/store"
)

func TestMixedRecordIdentityArchivePlanningAndReplay(t *testing.T) {
	const legacy = "rec_legacy"
	const ordinary = "rec2_70000000000000000000000000000000"
	const certifiable = "rec2_80000000000000000000000000000000"
	archive, err := WriteArchiveV1([]ArchiveEntry{
		{Path: "records/" + legacy + "/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Legacy\n")},
		{Path: "records/" + ordinary + "/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Ordinary\n")},
		{Path: "records/" + certifiable + "/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Certifiable\n")},
		{Path: "records/" + certifiable + "/attachments/att_source/notes.txt", Classification: ArchiveClassAttachment, Payload: []byte("notes")},
	})
	if err != nil {
		t.Fatalf("WriteArchiveV1() error = %v", err)
	}
	service, _, _ := mustImportService(t)
	actor := portabilityTestActor(t)
	job := store.RecordImportJob{ImportJobID: "rij_identity", ActorID: actor.UserID, ArchiveDigest: sha256.Sum256(archive), DestinationSubject: testImportDestination()}
	planned, err := service.planArchive(context.Background(), archive, actor, job)
	if err != nil {
		t.Fatalf("planArchive() error = %v", err)
	}
	if len(planned.documents) != 3 || len(planned.attachments) != 1 || planned.attachments[0].RecordSourceID != certifiable {
		t.Fatalf("planned archive identities = %#v / %#v", planned.documents, planned.attachments)
	}
	seen := map[string]bool{}
	for _, document := range planned.documents {
		seen[document.SourceID] = true
	}
	if !seen[legacy] || !seen[ordinary] || !seen[certifiable] {
		t.Fatalf("source identities = %#v", seen)
	}
	replayed, err := rebindArchive(archive, planned.storeRemaps(), testImportDestination())
	if err != nil || replayed.digest != planned.digest || len(replayed.attachments) != 1 || replayed.attachments[0].RecordSourceID != certifiable {
		t.Fatalf("rebindArchive() = %#v, %v; original digest %x", replayed, err, planned.digest)
	}
}

func TestImportedArchiveRejectsInvalidRecordPathIdentity(t *testing.T) {
	for _, recordID := range []string{"rec_", "rrv_revision", "rec2_7000000000000000000000000000000g", "rec2_7000000000000000000000000000000"} {
		entry := ArchiveEntry{Path: "records/" + recordID + "/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Invalid\n")}
		if _, _, _, err := parseImportedMarkdown(entry); !errors.Is(err, ErrUntrustedImportContent) {
			t.Fatalf("parseImportedMarkdown(%q) error = %v", recordID, err)
		}
		if got := recordSourceFromArchivePath(entry.Path); got != "" {
			t.Fatalf("recordSourceFromArchivePath(%q) = %q", entry.Path, got)
		}
	}
}
