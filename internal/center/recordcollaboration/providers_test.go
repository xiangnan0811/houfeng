package recordcollaboration

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

func TestActivityProviderReturnsClosedTypedDefensiveFacts(t *testing.T) {
	t.Parallel()

	binding := collaborationProviderTestBinding(t)
	fact := ActivityFact{
		ActivityID: "rac_provider01", RecordID: binding.RecordID(), RevisionID: "rrv_provider01",
		Kind: ActivityFactActionCompleted, SourceEventID: "raev_provider01", SourceVersion: 2,
		ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", AuthorizationEpoch: 3, RecordLockVersion: 4,
		EventAt: time.Date(2026, time.August, 17, 18, 0, 0, 0, time.UTC),
	}
	store := &providerStoreStub{facts: []ActivityFact{fact}}
	provider, err := NewActivityProvider(store)
	if err != nil {
		t.Fatalf("NewActivityProvider() error = %v", err)
	}
	tx := &collaborationProviderTxStub{}
	facts, err := provider.ListFacts(context.Background(), tx, binding)
	if err != nil || len(facts) != 1 || facts[0] != fact {
		t.Fatalf("ListFacts() = %#v, %v", facts, err)
	}
	facts[0].ActivityID = "rac_tampered"
	again, err := provider.ListFacts(context.Background(), tx, binding)
	if err != nil || again[0] != fact {
		t.Fatalf("ListFacts() after mutation = %#v, %v", again, err)
	}
}

func TestPortabilityAdapterRejectsRedactedContentAndClonesBackupRestore(t *testing.T) {
	t.Parallel()

	binding := collaborationProviderTestBinding(t)
	redactedAt := time.Date(2026, time.August, 17, 18, 15, 0, 0, time.UTC)
	snapshot := PortabilitySnapshot{
		Actions:      []PortableAction{},
		ActionEvents: []PortableActionEvent{},
		Comments: []PortableComment{{
			CommentID: "rcm_provider01", AuthorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Version: 2,
			State: CommentStateRedacted, TombstoneID: "rct_provider01",
			CreatedAt: redactedAt.Add(-time.Minute), UpdatedAt: redactedAt, RedactedAt: &redactedAt,
		}},
		CommentRevisions: []PortableCommentRevision{}, Tombstones: []PortableCommentTombstone{{
			TombstoneID: "rct_provider01", CommentID: "rcm_provider01", Version: 2,
			DeletedBy: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", ReasonCode: PortableTombstoneAuthorDeleted, DeletedAt: redactedAt,
		}},
		Replies: []PortableCommentReply{}, Mentions: []PortableCommentMention{},
		Followers: []PortableFollower{}, NotificationAudits: []PortableNotificationAudit{},
	}
	store := &providerStoreStub{snapshot: snapshot}
	if err := validatePortableCommentTombstone(snapshot.Tombstones[0]); err != nil {
		t.Fatalf("portable tombstone fixture invalid: %v", err)
	}
	if err := validatePortableComment(snapshot.Comments[0]); err != nil {
		t.Fatalf("portable comment fixture invalid: %v", err)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("portability fixture invalid: %v", err)
	}
	adapter, err := NewPortabilityAdapter(store)
	if err != nil {
		t.Fatalf("NewPortabilityAdapter() error = %v", err)
	}
	tx := &collaborationProviderTxStub{}
	backup, err := adapter.Backup(context.Background(), tx, binding)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	backup.Comments[0].TombstoneID = "rct_tampered"
	if store.snapshot.Comments[0].TombstoneID != "rct_provider01" {
		t.Fatal("Backup() exposed repository storage")
	}
	if err := adapter.Restore(context.Background(), tx, binding, snapshot); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	store.restored.Comments[0].TombstoneID = "rct_mutated"
	if snapshot.Comments[0].TombstoneID != "rct_provider01" {
		t.Fatal("Restore() passed caller-owned storage through")
	}

	unsafe := snapshot.Clone()
	unsafe.Comments[0].BodyMarkdown = "secret must stay absent"
	unsafe.Comments[0].BodyDigest = sha256.Sum256([]byte(unsafe.Comments[0].BodyMarkdown))
	if err := adapter.Restore(context.Background(), tx, binding, unsafe); !errors.Is(err, ErrInvalidPortabilitySnapshot) {
		t.Fatalf("Restore(redacted content) error = %v, want ErrInvalidPortabilitySnapshot", err)
	}
}

func TestProvidersRejectTypedNilDependenciesTransactionsAndInvalidFacts(t *testing.T) {
	t.Parallel()

	var typedNil *providerStoreStub
	if _, err := NewActivityProvider(typedNil); !errors.Is(err, ErrInvalidActivityProvider) {
		t.Fatalf("NewActivityProvider(typed nil) error = %v", err)
	}
	if _, err := NewPortabilityAdapter(typedNil); !errors.Is(err, ErrInvalidPortabilityAdapter) {
		t.Fatalf("NewPortabilityAdapter(typed nil) error = %v", err)
	}
	provider, _ := NewActivityProvider(&providerStoreStub{facts: []ActivityFact{{}}})
	var nilTx *collaborationProviderTxStub
	if _, err := provider.ListFacts(context.Background(), nilTx, collaborationProviderTestBinding(t)); !errors.Is(err, ErrInvalidActivityProvider) {
		t.Fatalf("ListFacts(typed nil tx) error = %v", err)
	}
	if _, err := provider.ListFacts(context.Background(), &collaborationProviderTxStub{}, collaborationProviderTestBinding(t)); !errors.Is(err, ErrInvalidActivityFact) {
		t.Fatalf("ListFacts(invalid fact) error = %v", err)
	}
}

func TestPortableNotificationAuditHasContentFreeClosedSchema(t *testing.T) {
	t.Parallel()

	typeOf := reflect.TypeOf(PortableNotificationAudit{})
	for index := 0; index < typeOf.NumField(); index++ {
		name := strings.ToLower(typeOf.Field(index).Name)
		for _, forbidden := range []string{"actor", "recipientid", "subjectid", "binding", "credential", "provider", "response", "body", "content"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("PortableNotificationAudit field %q carries forbidden external/content data", typeOf.Field(index).Name)
			}
		}
	}
}

type providerStoreStub struct {
	facts    []ActivityFact
	snapshot PortabilitySnapshot
	restored PortabilitySnapshot
}

func (store *providerStoreStub) ReadCollaborationActivityFacts(context.Context, pgx.Tx, RecordFenceBinding) ([]ActivityFact, error) {
	return append([]ActivityFact(nil), store.facts...), nil
}

func (store *providerStoreStub) BackupCollaboration(context.Context, pgx.Tx, RecordFenceBinding) (PortabilitySnapshot, error) {
	return store.snapshot.Clone(), nil
}

func (store *providerStoreStub) RestoreCollaboration(_ context.Context, _ pgx.Tx, _ RecordFenceBinding, snapshot PortabilitySnapshot) error {
	store.restored = snapshot.Clone()
	return nil
}

type collaborationProviderTxStub struct{ pgx.Tx }

func collaborationProviderTestBinding(t *testing.T) RecordFenceBinding {
	t.Helper()
	binding, err := NewRecordFenceBinding(recordplatform.ProjectIDDefault, "rec_provider01", 7)
	if err != nil {
		t.Fatalf("NewRecordFenceBinding() error = %v", err)
	}
	return binding
}
