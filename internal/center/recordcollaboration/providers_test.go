package recordcollaboration

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
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
	if provider.ContractVersion() != CollaborationActivityContractVersionV1 {
		t.Fatalf("ContractVersion() = %d", provider.ContractVersion())
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
		CommentRevisions: []PortableCommentRevision{{
			RevisionID: "rcr_provider01", CommentID: "rcm_provider01", Version: 1,
			EditedBy: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", TombstoneID: "rct_provider01",
			CreatedAt: redactedAt.Add(-time.Minute), RedactedAt: &redactedAt,
		}}, Tombstones: []PortableCommentTombstone{{
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
	if adapter.ContractVersion() != CollaborationPortabilityContractVersionV1 {
		t.Fatalf("ContractVersion() = %d", adapter.ContractVersion())
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

func TestPortabilitySnapshotAllowsContentFreeVersionedDefaultWatchAnchor(t *testing.T) {
	t.Parallel()
	snapshot := validPortableAggregateSnapshot(t)
	stamp := time.Date(2026, time.August, 17, 19, 0, 0, 0, time.UTC)
	snapshot.Followers = append(snapshot.Followers, PortableFollower{
		UserID: "usr_cccccccccccccccccccccccc", Version: 2,
		Preference: FollowerPreferenceDefault, WatchReplayAnchor: true,
		CreatedAt: stamp.Add(-time.Minute), UpdatedAt: stamp,
	})
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate(versioned default watch anchor) error = %v", err)
	}
	snapshot.Followers[0].WatchReplayAnchor = false
	if err := snapshot.Validate(); !errors.Is(err, ErrInvalidPortabilitySnapshot) {
		t.Fatalf("Validate(unanchored default without sources) error = %v", err)
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
	zeroAuthorization := ActivityFact{
		ActivityID: "rac_zeroauth", RecordID: "rec_provider01", RevisionID: "rrv_provider01",
		Kind: ActivityFactActionCompleted, SourceEventID: "raev_zeroauth", SourceVersion: 1,
		ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", RecordLockVersion: 1,
		EventAt: time.Date(2026, time.August, 17, 18, 0, 0, 0, time.UTC),
	}
	provider, _ = NewActivityProvider(&providerStoreStub{facts: []ActivityFact{zeroAuthorization}})
	if _, err := provider.ListFacts(context.Background(), &collaborationProviderTxStub{}, collaborationProviderTestBinding(t)); !errors.Is(err, ErrInvalidActivityFact) {
		t.Fatalf("ListFacts(zero authorization epoch) error = %v, want ErrInvalidActivityFact", err)
	}
	overLimit := make([]ActivityFact, MaxCollaborationActivityFacts+1)
	provider, _ = NewActivityProvider(&providerStoreStub{facts: overLimit})
	if _, err := provider.ListFacts(context.Background(), &collaborationProviderTxStub{}, collaborationProviderTestBinding(t)); !errors.Is(err, ErrInvalidActivityFact) {
		t.Fatalf("ListFacts(over limit) error = %v, want ErrInvalidActivityFact", err)
	}
}

func TestPortabilitySnapshotRejectsPerSurfaceOverflow(t *testing.T) {
	t.Parallel()

	tooMany := PortabilitySnapshot{
		Actions: make([]PortableAction, MaxCollaborationPortabilityRowsPerSurface+1), ActionEvents: []PortableActionEvent{},
		Comments: []PortableComment{}, CommentRevisions: []PortableCommentRevision{}, Tombstones: []PortableCommentTombstone{},
		Replies: []PortableCommentReply{}, Mentions: []PortableCommentMention{}, Followers: []PortableFollower{}, NotificationAudits: []PortableNotificationAudit{},
	}
	if err := tooMany.Validate(); !errors.Is(err, ErrInvalidPortabilitySnapshot) {
		t.Fatalf("Validate(per-surface overflow) error = %v, want ErrInvalidPortabilitySnapshot", err)
	}
}

func TestPortabilitySnapshotRejectsNotificationAuditBigintAndSubtotalOverflow(t *testing.T) {
	t.Parallel()

	base := PortableNotificationAudit{
		NotificationID: "rnt_" + strings.Repeat("a", 64),
		Kind:           NotificationEventRecordOwnerChanged, SubjectKind: NotificationSubjectRecord,
		SourceVersion: 1, EventAt: time.Date(2026, time.August, 17, 18, 30, 0, 0, time.UTC),
		RecipientCount: 3, DeliveryCount: 3, SentCount: 1, UnknownCount: 1, PermanentFailed: 1,
	}
	cases := map[string]func(*PortableNotificationAudit){
		"recipient exceeds bigint": func(audit *PortableNotificationAudit) { audit.RecipientCount = math.MaxUint64 },
		"delivery exceeds bigint":  func(audit *PortableNotificationAudit) { audit.DeliveryCount = math.MaxUint64 },
		"sent exceeds bigint": func(audit *PortableNotificationAudit) {
			audit.DeliveryCount, audit.SentCount = math.MaxUint64, math.MaxUint64
		},
		"unknown exceeds bigint": func(audit *PortableNotificationAudit) {
			audit.DeliveryCount, audit.UnknownCount = math.MaxUint64, math.MaxUint64
		},
		"permanent failed exceeds bigint": func(audit *PortableNotificationAudit) {
			audit.DeliveryCount, audit.PermanentFailed = math.MaxUint64, math.MaxUint64
		},
		"delivery subtotal overflows uint64": func(audit *PortableNotificationAudit) {
			audit.DeliveryCount, audit.SentCount, audit.UnknownCount, audit.PermanentFailed =
				math.MaxInt64, math.MaxInt64, math.MaxInt64, 2
		},
	}
	for name, mutate := range cases {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			audit := base
			mutate(&audit)
			snapshot := PortabilitySnapshot{
				Actions: []PortableAction{}, ActionEvents: []PortableActionEvent{}, Comments: []PortableComment{},
				CommentRevisions: []PortableCommentRevision{}, Tombstones: []PortableCommentTombstone{},
				Replies: []PortableCommentReply{}, Mentions: []PortableCommentMention{}, Followers: []PortableFollower{},
				NotificationAudits: []PortableNotificationAudit{audit},
			}
			if err := snapshot.Validate(); !errors.Is(err, ErrInvalidPortabilitySnapshot) {
				t.Fatalf("Validate() error = %v, want ErrInvalidPortabilitySnapshot", err)
			}
		})
	}
}

func TestPortabilitySnapshotRejectsSparseOrDriftedAggregateHistory(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*PortabilitySnapshot){
		"sparse action versions": func(snapshot *PortabilitySnapshot) { snapshot.Actions[0].Version = 5 },
		"action status chain drift": func(snapshot *PortabilitySnapshot) {
			cancelled := ActionStatusCancelled
			snapshot.ActionEvents[1].Kind = ActionMutationReopen
			snapshot.ActionEvents[1].PreviousStatus = &cancelled
			snapshot.ActionEvents[1].CurrentStatus = ActionStatusOpen
			snapshot.Actions[0].Status = ActionStatusOpen
			snapshot.Actions[0].CompletedAt = nil
		},
		"action final status drift": func(snapshot *PortabilitySnapshot) {
			snapshot.Actions[0].Status = ActionStatusCancelled
			snapshot.Actions[0].CompletedAt = nil
		},
		"action final actor drift": func(snapshot *PortabilitySnapshot) {
			snapshot.Actions[0].UpdatedBy = "usr_cccccccccccccccccccccccc"
		},
		"sparse comment versions": func(snapshot *PortabilitySnapshot) { snapshot.Comments[0].Version = 5 },
		"comment latest content drift": func(snapshot *PortabilitySnapshot) {
			snapshot.CommentRevisions[1].BodyMarkdown = snapshot.CommentRevisions[0].BodyMarkdown
			snapshot.CommentRevisions[1].RenderModel = snapshot.CommentRevisions[0].RenderModel.Clone()
			snapshot.CommentRevisions[1].BodyDigest = snapshot.CommentRevisions[0].BodyDigest
		},
		"orphan action event":     func(snapshot *PortabilitySnapshot) { snapshot.ActionEvents[1].ActionID = "ract_missing" },
		"orphan comment revision": func(snapshot *PortabilitySnapshot) { snapshot.CommentRevisions[1].CommentID = "rcm_missing" },
		"orphan reply parent": func(snapshot *PortabilitySnapshot) {
			snapshot.Replies = append(snapshot.Replies, PortableCommentReply{
				ChildCommentID: snapshot.Comments[0].CommentID, ParentCommentID: "rcm_missing", CreatedAt: snapshot.Comments[0].CreatedAt,
			})
		},
		"mention missing revision": func(snapshot *PortabilitySnapshot) {
			snapshot.Mentions = append(snapshot.Mentions, PortableCommentMention{
				CommentID: snapshot.Comments[0].CommentID, CommentVersion: 3,
				MentionedUser: "usr_cccccccccccccccccccccccc", CreatedAt: snapshot.Comments[0].UpdatedAt,
			})
		},
	}
	for name, mutate := range cases {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snapshot := validPortableAggregateSnapshot(t)
			mutate(&snapshot)
			if err := snapshot.Validate(); !errors.Is(err, ErrInvalidPortabilitySnapshot) {
				t.Fatalf("Validate() error = %v, want ErrInvalidPortabilitySnapshot", err)
			}
		})
	}
}

func TestPortabilitySnapshotRejectsNestedAndCyclicReplies(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		cycle bool
	}{
		{name: "nested reply"},
		{name: "equal-time cycle", cycle: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := validPortableAggregateSnapshot(t)
			createdAt := snapshot.Comments[0].CreatedAt.Add(2 * time.Minute)
			appendPortableTestComment(t, &snapshot, "rcm_nestedb", "rcr_nestedb", createdAt)
			secondCreatedAt := createdAt.Add(time.Minute)
			if test.cycle {
				secondCreatedAt = createdAt
			}
			appendPortableTestComment(t, &snapshot, "rcm_nestedc", "rcr_nestedc", secondCreatedAt)
			if test.cycle {
				snapshot.Replies = []PortableCommentReply{
					{ChildCommentID: "rcm_nestedb", ParentCommentID: "rcm_nestedc", CreatedAt: createdAt},
					{ChildCommentID: "rcm_nestedc", ParentCommentID: "rcm_nestedb", CreatedAt: secondCreatedAt},
				}
			} else {
				snapshot.Replies = []PortableCommentReply{
					{ChildCommentID: "rcm_nestedb", ParentCommentID: "rcm_aggregate", CreatedAt: createdAt},
					{ChildCommentID: "rcm_nestedc", ParentCommentID: "rcm_nestedb", CreatedAt: secondCreatedAt},
				}
			}
			if err := snapshot.Validate(); !errors.Is(err, ErrInvalidPortabilitySnapshot) {
				t.Fatalf("Validate() error = %v, want ErrInvalidPortabilitySnapshot", err)
			}
		})
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

func validPortableAggregateSnapshot(t *testing.T) PortabilitySnapshot {
	t.Helper()
	t0 := time.Date(2026, time.August, 17, 18, 45, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	completed := ActionStatusCompleted
	firstContent, err := NewCommentContent("First comment body.")
	if err != nil {
		t.Fatal(err)
	}
	secondContent, err := NewCommentContent("Second **comment** body.")
	if err != nil {
		t.Fatal(err)
	}
	return PortabilitySnapshot{
		Actions: []PortableAction{{
			ActionID: "ract_aggregate", Version: 2, Title: "Aggregate action", Status: ActionStatusCompleted,
			CompletedAt: &t1, CreatedBy: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", UpdatedBy: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
			CreatedAt: t0, UpdatedAt: t1,
		}},
		ActionEvents: []PortableActionEvent{
			{EventID: "raev_aggregate01", ActionID: "ract_aggregate", Version: 1, Kind: ActionMutationCreate,
				CurrentStatus: ActionStatusOpen, ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", OccurredAt: t0, CreatedAt: t0},
			{EventID: "raev_aggregate02", ActionID: "ract_aggregate", Version: 2, Kind: ActionMutationComplete,
				PreviousStatus: func() *ActionStatus { value := ActionStatusOpen; return &value }(), CurrentStatus: completed,
				ActorID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", OccurredAt: t1, CreatedAt: t1},
		},
		Comments: []PortableComment{{
			CommentID: "rcm_aggregate", AuthorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Version: 2,
			State: CommentStateActive, BodyMarkdown: secondContent.Source(), RenderModel: secondContent.Model(),
			BodyDigest: secondContent.Digest(), CreatedAt: t0, UpdatedAt: t1,
		}},
		CommentRevisions: []PortableCommentRevision{
			{RevisionID: "rcr_aggregate01", CommentID: "rcm_aggregate", Version: 1,
				EditedBy: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", BodyMarkdown: firstContent.Source(),
				RenderModel: firstContent.Model(), BodyDigest: firstContent.Digest(), CreatedAt: t0},
			{RevisionID: "rcr_aggregate02", CommentID: "rcm_aggregate", Version: 2,
				EditedBy: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", BodyMarkdown: secondContent.Source(),
				RenderModel: secondContent.Model(), BodyDigest: secondContent.Digest(), CreatedAt: t1},
		},
		Tombstones: []PortableCommentTombstone{}, Replies: []PortableCommentReply{},
		Mentions: []PortableCommentMention{}, Followers: []PortableFollower{}, NotificationAudits: []PortableNotificationAudit{},
	}
}

func appendPortableTestComment(t *testing.T, snapshot *PortabilitySnapshot, commentID, revisionID string, createdAt time.Time) {
	t.Helper()
	content, err := NewCommentContent("Comment for " + commentID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Comments = append(snapshot.Comments, PortableComment{
		CommentID: commentID, AuthorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Version: 1,
		State: CommentStateActive, BodyMarkdown: content.Source(), RenderModel: content.Model(), BodyDigest: content.Digest(),
		CreatedAt: createdAt, UpdatedAt: createdAt,
	})
	snapshot.CommentRevisions = append(snapshot.CommentRevisions, PortableCommentRevision{
		RevisionID: revisionID, CommentID: commentID, Version: 1, EditedBy: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		BodyMarkdown: content.Source(), RenderModel: content.Model(), BodyDigest: content.Digest(), CreatedAt: createdAt,
	})
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
