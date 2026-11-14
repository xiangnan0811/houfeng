package records

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"houfeng/internal/center/recordauth"
)

func TestCompletedCreateFingerprintPreservesLegacyVector(t *testing.T) {
	actor := mustRecordActor(t)
	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	etag, err := ParseDraftETag(`"draft-v1-1111111111111111111111111111111111111111111111111111111111111111"`)
	if err != nil {
		t.Fatal(err)
	}
	revision := RecordRevision{RecordID: "rec_service1", RevisionID: "rrv_legacy", RevisionNo: 1, Input: input}
	got, err := CompletedCreateFingerprint(actor, "rdf_0123456789abcdef", etag, revision)
	if err != nil {
		t.Fatalf("CompletedCreateFingerprint() error = %v", err)
	}
	// Independently framed with the published v1 actor, revision and platform
	// codecs, using the existing empty-evidence canonical revision vector.
	const want = "b68c1dd07513801b42208a08980ec932abd8f0c749d8f60872b4a12e1279b125"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("legacy fingerprint = %x, want %s", got, want)
	}

	changedETag, err := ParseDraftETag(`"draft-v1-2222222222222222222222222222222222222222222222222222222222222222"`)
	if err != nil {
		t.Fatal(err)
	}
	changedActor := actor.Clone()
	changedActor.Role = recordauth.RoleViewer
	for _, test := range []struct {
		name  string
		actor recordauth.ActorScope
		draft string
		etag  DraftETag
	}{
		{name: "draft", actor: actor, draft: "rdf_other", etag: etag},
		{name: "etag", actor: actor, draft: "rdf_0123456789abcdef", etag: changedETag},
		{name: "actor scope", actor: changedActor, draft: "rdf_0123456789abcdef", etag: etag},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed, err := CompletedCreateFingerprint(test.actor, test.draft, test.etag, revision)
			if err != nil || changed == got {
				t.Fatalf("changed binding = %x, %v; want different fingerprint", changed, err)
			}
		})
	}

	wrongAuthor := actor.Clone()
	wrongAuthor.UserID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
	badRevision := revision
	badRevision.RevisionNo = 2
	badBase := revision
	badBase.BaseRevisionID = "rrv_previous"
	newDomain := revision
	newDomain.RecordID = "rec2_00000000000000000000000000000000"
	for _, test := range []struct {
		name     string
		actor    recordauth.ActorScope
		draft    string
		etag     DraftETag
		revision RecordRevision
	}{
		{name: "author", actor: wrongAuthor, draft: "rdf_valid", etag: etag, revision: revision},
		{name: "missing draft", actor: actor, etag: etag, revision: revision},
		{name: "missing etag", actor: actor, draft: "rdf_valid", revision: revision},
		{name: "later revision", actor: actor, draft: "rdf_valid", etag: etag, revision: badRevision},
		{name: "base revision", actor: actor, draft: "rdf_valid", etag: etag, revision: badBase},
		{name: "new identity", actor: actor, draft: "rdf_valid", etag: etag, revision: newDomain},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CompletedCreateFingerprint(test.actor, test.draft, test.etag, test.revision); !errors.Is(err, ErrInvalidRevisionServiceRequest) {
				t.Fatalf("invalid binding error = %v", err)
			}
		})
	}
}

func TestCompletedCreateFingerprintMatchesActualPublication(t *testing.T) {
	actor := mustRecordActor(t)
	request, resolved := testRevisionServiceRequest(t, actor, DomainActivityRecordCreated)
	payload, err := NewDraftPayload([]byte(`{"title":"Publish"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.DraftID = "rdf_0123456789abcdef"
	request.DraftETag, err = NewDraftETag(request.DraftID, actor.UserID, 4, payload)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewSubjectAdapterRegistry([]SubjectSourceAdapter{&revisionServiceSubjectAdapter{kind: SubjectKindVPS, resolved: resolved}})
	if err != nil {
		t.Fatal(err)
	}
	store := &revisionCommitStoreStub{}
	service, err := NewRevisionService(registry, &currentRecordAuthorizationSourceStub{}, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveRevision(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	revisionID, err := store.command.RevisionID()
	if err != nil {
		t.Fatal(err)
	}
	revision := RecordRevision{RecordID: request.RecordID, RevisionID: revisionID, RevisionNo: 1, Input: store.command.Input}
	got, err := CompletedCreateFingerprint(actor, request.DraftID, request.DraftETag, revision)
	if err != nil {
		t.Fatal(err)
	}
	want, err := store.command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil || got != want {
		t.Fatalf("reconstructed = %x, publication = %x, error = %v", got, want, err)
	}
}
