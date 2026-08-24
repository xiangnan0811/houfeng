package recordsearch

import (
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/records"
)

func testProjectionFacts() DocumentFactValues {
	occurred := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	followUp := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	return DocumentFactValues{
		RecordID:           "rec_000000000000000000000001",
		CurrentRevisionID:  "rrv_000000000000000000000001",
		LockVersion:        3,
		AuthorizationEpoch: 3,
		FenceEpoch:         0,
		Lifecycle:          records.LifecycleActive,
		RecordType:         records.RecordTypeTroubleshooting,
		BusinessStatus:     records.StatusInvestigating,
		ImpactLevel:        "medium",
		OwnerID:            "usr_000000000000000000000001",
		Title:              "磁盘故障排查",
		Text:               "根因是 NVMe 掉盘",
		Tags:               []string{"disk", "nvme"},
		ParticipantIDs:     []string{"usr_000000000000000000000002"},
		VisibilityKind:     "project",
		VisibilityDigest:   [32]byte{1, 2, 3},
		OccurredAt:         &occurred,
		FollowUpAt:         &followUp,
		RecordCreatedAt:    time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC),
		RecordUpdatedAt:    time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC),
		Subjects: []DocumentSubject{
			{Kind: records.SubjectKindVPS, Role: records.RelationRoleAffected, SourceID: testProjectionVPSID, Primary: true},
			{Kind: records.SubjectKindTarget, Role: records.RelationRoleContext, SourceID: testProjectionTargetID},
		},
	}
}

const (
	testProjectionVPSID    = "vps_0123456789abcdef"
	testProjectionTargetID = "tg_fedcba9876543210"
)

// The digest is what a rebuild compares to decide whether a generation covered a
// record, so inputs that describe the same document must produce the same bytes
// no matter what order the write path happened to assemble them in.
func TestNormalizeDocumentFactsCanonicalizesEqualDocuments(t *testing.T) {
	t.Parallel()

	base, err := NormalizeDocumentFacts(testProjectionFacts())
	if err != nil {
		t.Fatalf("NormalizeDocumentFacts() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*DocumentFactValues)
	}{
		{name: "reordered tags", mutate: func(values *DocumentFactValues) {
			values.Tags = []string{"nvme", "disk"}
		}},
		{name: "duplicate tags", mutate: func(values *DocumentFactValues) {
			values.Tags = []string{"disk", "nvme", "disk"}
		}},
		{name: "reordered subjects", mutate: func(values *DocumentFactValues) {
			values.Subjects = []DocumentSubject{
				{Kind: records.SubjectKindTarget, Role: records.RelationRoleContext, SourceID: testProjectionTargetID},
				{Kind: records.SubjectKindVPS, Role: records.RelationRoleAffected, SourceID: testProjectionVPSID, Primary: true},
			}
		}},
		{name: "duplicate subjects", mutate: func(values *DocumentFactValues) {
			values.Subjects = append(values.Subjects, values.Subjects[0])
		}},
		{name: "non-utc timestamps", mutate: func(values *DocumentFactValues) {
			zone := time.FixedZone("test", 8*60*60)
			values.RecordUpdatedAt = values.RecordUpdatedAt.In(zone)
			shifted := values.OccurredAt.In(zone)
			values.OccurredAt = &shifted
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			values := testProjectionFacts()
			tt.mutate(&values)
			got, err := NormalizeDocumentFacts(values)
			if err != nil {
				t.Fatalf("NormalizeDocumentFacts() error = %v", err)
			}
			if got.Digest() != base.Digest() {
				t.Fatalf("digest = %x, want %x for a logically equal document", got.Digest(), base.Digest())
			}
		})
	}
}

// Every projected content field has to move the digest, or a rebuild would
// accept a row carrying the wrong content as covered.
func TestNormalizeDocumentFactsDigestCoversEveryContentField(t *testing.T) {
	t.Parallel()

	base, err := NormalizeDocumentFacts(testProjectionFacts())
	if err != nil {
		t.Fatalf("NormalizeDocumentFacts() error = %v", err)
	}
	later := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		mutate func(*DocumentFactValues)
	}{
		{name: "revision", mutate: func(v *DocumentFactValues) { v.CurrentRevisionID = "rrv_000000000000000000000002" }},
		{name: "record type", mutate: func(v *DocumentFactValues) {
			v.RecordType = records.RecordTypeMaintenance
			v.BusinessStatus = records.StatusExecuting
		}},
		{name: "business status and derived group", mutate: func(v *DocumentFactValues) {
			v.BusinessStatus = records.StatusVerifying
		}},
		{name: "impact level", mutate: func(v *DocumentFactValues) { v.ImpactLevel = "high" }},
		{name: "owner", mutate: func(v *DocumentFactValues) { v.OwnerID = "usr_000000000000000000000009" }},
		{name: "title", mutate: func(v *DocumentFactValues) { v.Title = "磁盘更换" }},
		{name: "text", mutate: func(v *DocumentFactValues) { v.Text = "根因是电源" }},
		{name: "tags", mutate: func(v *DocumentFactValues) { v.Tags = []string{"disk"} }},
		{name: "participants", mutate: func(v *DocumentFactValues) { v.ParticipantIDs = nil }},
		{name: "visibility kind", mutate: func(v *DocumentFactValues) { v.VisibilityKind = "restricted" }},
		{name: "visibility digest", mutate: func(v *DocumentFactValues) { v.VisibilityDigest = [32]byte{9} }},
		{name: "occurred at", mutate: func(v *DocumentFactValues) { v.OccurredAt = &later }},
		{name: "completed at", mutate: func(v *DocumentFactValues) { v.CompletedAt = &later }},
		{name: "follow up at", mutate: func(v *DocumentFactValues) { v.FollowUpAt = &later }},
		{name: "subject source", mutate: func(v *DocumentFactValues) {
			v.Subjects[0].SourceID = "vps_00000000000000ff"
		}},
		{name: "subject role", mutate: func(v *DocumentFactValues) { v.Subjects[0].Role = records.RelationRoleContext }},
		{name: "subject placement", mutate: func(v *DocumentFactValues) { v.Subjects[0].Primary = false }},
		{name: "dropped subject", mutate: func(v *DocumentFactValues) { v.Subjects = v.Subjects[:1] }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			values := testProjectionFacts()
			tt.mutate(&values)
			got, err := NormalizeDocumentFacts(values)
			if err != nil {
				t.Fatalf("NormalizeDocumentFacts() error = %v", err)
			}
			if got.Digest() == base.Digest() {
				t.Fatalf("digest unchanged after changing %s", tt.name)
			}
		})
	}
}

// The control columns are stored plainly and compared column by column, so they
// stay out of the digest. That exclusion is what lets a lifecycle move update the
// index in place, and it has to be deliberate rather than accidental.
func TestNormalizeDocumentFactsDigestExcludesControlColumns(t *testing.T) {
	t.Parallel()

	base, err := NormalizeDocumentFacts(testProjectionFacts())
	if err != nil {
		t.Fatalf("NormalizeDocumentFacts() error = %v", err)
	}
	later := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		mutate func(*DocumentFactValues)
	}{
		{name: "lifecycle", mutate: func(v *DocumentFactValues) { v.Lifecycle = records.LifecycleArchived }},
		{name: "lock version", mutate: func(v *DocumentFactValues) { v.LockVersion = 4 }},
		{name: "authorization epoch", mutate: func(v *DocumentFactValues) { v.AuthorizationEpoch = 4 }},
		{name: "fence epoch", mutate: func(v *DocumentFactValues) { v.FenceEpoch = 1 }},
		{name: "record updated at", mutate: func(v *DocumentFactValues) { v.RecordUpdatedAt = later }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			values := testProjectionFacts()
			tt.mutate(&values)
			got, err := NormalizeDocumentFacts(values)
			if err != nil {
				t.Fatalf("NormalizeDocumentFacts() error = %v", err)
			}
			if got.Digest() != base.Digest() {
				t.Fatalf("digest moved after changing control column %s", tt.name)
			}
		})
	}
}

// A projection is derived, so refusing bad input here is what keeps a malformed
// document out of the index instead of letting a check constraint abort the
// record write that produced it.
func TestNormalizeDocumentFactsRejectsUnusableInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*DocumentFactValues)
	}{
		{name: "missing record id", mutate: func(v *DocumentFactValues) { v.RecordID = "" }},
		{name: "malformed record id", mutate: func(v *DocumentFactValues) { v.RecordID = "rec_UPPER" }},
		{name: "missing revision", mutate: func(v *DocumentFactValues) { v.CurrentRevisionID = "" }},
		{name: "unknown lifecycle", mutate: func(v *DocumentFactValues) { v.Lifecycle = records.Lifecycle("deleted") }},
		{name: "unknown record type", mutate: func(v *DocumentFactValues) { v.RecordType = records.RecordType("rumor") }},
		{name: "unknown business status", mutate: func(v *DocumentFactValues) {
			v.BusinessStatus = records.BusinessStatus("stalled")
		}},
		{name: "status from another record type", mutate: func(v *DocumentFactValues) {
			v.BusinessStatus = records.StatusExecuting
		}},
		{name: "empty title", mutate: func(v *DocumentFactValues) { v.Title = "" }},
		{name: "empty impact level", mutate: func(v *DocumentFactValues) { v.ImpactLevel = "" }},
		{name: "unknown visibility kind", mutate: func(v *DocumentFactValues) { v.VisibilityKind = "public" }},
		{name: "zero visibility digest", mutate: func(v *DocumentFactValues) { v.VisibilityDigest = [32]byte{} }},
		{name: "zero created at", mutate: func(v *DocumentFactValues) { v.RecordCreatedAt = time.Time{} }},
		{name: "updated before created", mutate: func(v *DocumentFactValues) {
			v.RecordUpdatedAt = v.RecordCreatedAt.Add(-time.Hour)
		}},
		{name: "unknown subject kind", mutate: func(v *DocumentFactValues) {
			v.Subjects[0].Kind = records.SubjectKind("rack")
		}},
		{name: "two primary subjects", mutate: func(v *DocumentFactValues) { v.Subjects[1].Primary = true }},
		{name: "malformed owner", mutate: func(v *DocumentFactValues) { v.OwnerID = "owner-1" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			values := testProjectionFacts()
			tt.mutate(&values)
			if _, err := NormalizeDocumentFacts(values); !errors.Is(err, ErrInvalidProjection) {
				t.Fatalf("NormalizeDocumentFacts() error = %v, want %v", err, ErrInvalidProjection)
			}
		})
	}
}

// A record with no owner, no status, and no dates is normal for a short note, so
// the projection has to accept it rather than force the write path to invent
// values.
func TestNormalizeDocumentFactsAcceptsSparseRecord(t *testing.T) {
	t.Parallel()

	values := testProjectionFacts()
	values.OwnerID = ""
	values.RecordType = records.RecordTypeNote
	values.BusinessStatus = ""
	values.Text = ""
	values.Tags = nil
	values.ParticipantIDs = nil
	values.OccurredAt = nil
	values.FollowUpAt = nil
	values.Subjects = []DocumentSubject{
		{Kind: records.SubjectKindVPS, Role: records.RelationRoleAffected, SourceID: testProjectionVPSID, Primary: true},
	}

	facts, err := NormalizeDocumentFacts(values)
	if err != nil {
		t.Fatalf("NormalizeDocumentFacts() error = %v", err)
	}
	if facts.Digest() == ([32]byte{}) {
		t.Fatal("Digest() = zero for a valid sparse document")
	}
	if got := facts.Subjects(); len(got) != 1 || !got[0].Primary {
		t.Fatalf("Subjects() = %#v, want the single primary subject", got)
	}
	if facts.Tags() == nil || facts.ParticipantIDs() == nil {
		t.Fatalf("sparse repeated facts = tags %#v participants %#v, want non-nil empty slices for NOT NULL storage arrays",
			facts.Tags(), facts.ParticipantIDs())
	}
}

// The accessors hand out slices that the store writes from, so a caller must not
// be able to reach back into normalized state.
func TestDocumentFactsAccessorsCopyState(t *testing.T) {
	t.Parallel()

	facts, err := NormalizeDocumentFacts(testProjectionFacts())
	if err != nil {
		t.Fatalf("NormalizeDocumentFacts() error = %v", err)
	}
	tags := facts.Tags()
	tags[0] = "tampered"
	subjects := facts.Subjects()
	subjects[0].SourceID = "tampered"
	participants := facts.ParticipantIDs()
	participants[0] = "tampered"

	if facts.Tags()[0] == "tampered" || facts.Subjects()[0].SourceID == "tampered" ||
		facts.ParticipantIDs()[0] == "tampered" {
		t.Fatal("normalized document facts expose mutable internal state")
	}
}
