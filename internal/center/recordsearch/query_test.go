package recordsearch

import (
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/records"
)

func TestNormalizeQueryCanonicalizesLogicallyEqualInput(t *testing.T) {
	t.Parallel()

	shanghai := time.FixedZone("CST", 8*60*60)
	from := time.Date(2026, 8, 1, 8, 0, 0, 0, shanghai)
	to := time.Date(2026, 8, 2, 8, 0, 0, 0, shanghai)
	fromUTC := from.UTC()
	toUTC := to.UTC()

	base := QueryValues{
		Text:           "磁盘 latency",
		Types:          []records.RecordType{records.RecordTypeTroubleshooting, records.RecordTypeBilling},
		StatusGroups:   []records.StatusGroup{records.StatusGroupPending, records.StatusGroupCompleted},
		Lifecycles:     []records.Lifecycle{records.LifecycleActive},
		OwnerIDs:       []string{"usr_000000000000000000000002", "usr_000000000000000000000001"},
		ParticipantIDs: []string{"usr_000000000000000000000003"},
		Tags:           []string{"Disk", "network"},
		Subjects: []SubjectFilter{
			{Kind: records.SubjectKindTarget, SourceID: "tg_0000000000000001"},
			{Kind: records.SubjectKindVPS, SourceID: "vps_0000000000000001"},
		},
		Updated:  TimeRange{From: &from, To: &to},
		Sort:     SortUpdatedDesc,
		PageSize: 25,
	}

	// Every variant below is the same question asked differently. If any of them
	// produced a different digest, a cursor would stop working when the browser
	// reordered a filter or the operator retyped the text with extra spaces.
	tests := []struct {
		name    string
		variant func(values *QueryValues)
	}{
		{name: "identical input", variant: func(*QueryValues) {}},
		{name: "reordered types", variant: func(values *QueryValues) {
			values.Types = []records.RecordType{records.RecordTypeBilling, records.RecordTypeTroubleshooting}
		}},
		{name: "reordered status groups", variant: func(values *QueryValues) {
			values.StatusGroups = []records.StatusGroup{records.StatusGroupCompleted, records.StatusGroupPending}
		}},
		{name: "reordered owners", variant: func(values *QueryValues) {
			values.OwnerIDs = []string{"usr_000000000000000000000001", "usr_000000000000000000000002"}
		}},
		{name: "reordered subjects", variant: func(values *QueryValues) {
			values.Subjects = []SubjectFilter{
				{Kind: records.SubjectKindVPS, SourceID: "vps_0000000000000001"},
				{Kind: records.SubjectKindTarget, SourceID: "tg_0000000000000001"},
			}
		}},
		{name: "tag case and order", variant: func(values *QueryValues) {
			values.Tags = []string{"NETWORK", "disk"}
		}},
		{name: "surrounding and repeated text whitespace", variant: func(values *QueryValues) {
			values.Text = "  磁盘\t\n latency  "
		}},
		{name: "range expressed in utc", variant: func(values *QueryValues) {
			values.Updated = TimeRange{From: &fromUTC, To: &toUTC}
		}},
	}

	want, err := NormalizeQuery(base)
	if err != nil {
		t.Fatalf("NormalizeQuery(base) error = %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			values := base
			tt.variant(&values)
			got, err := NormalizeQuery(values)
			if err != nil {
				t.Fatalf("NormalizeQuery() error = %v", err)
			}
			if got.Digest() != want.Digest() {
				t.Fatalf("digest = %x, want %x", got.Digest(), want.Digest())
			}
		})
	}
}

// A term typed on a macOS keyboard arrives decomposed and the same term pasted
// from the record body arrives composed. They are one question, so they must
// digest identically or the second page of a search would 400.
func TestNormalizeQueryFoldsUnicodeToComposedForm(t *testing.T) {
	t.Parallel()

	composed, err := NormalizeQuery(QueryValues{Text: "caf\u00e9 pr\u00fcfung"})
	if err != nil {
		t.Fatalf("NormalizeQuery(composed) error = %v", err)
	}
	decomposed, err := NormalizeQuery(QueryValues{Text: "cafe\u0301 pru\u0308fung"})
	if err != nil {
		t.Fatalf("NormalizeQuery(decomposed) error = %v", err)
	}
	if composed.Digest() != decomposed.Digest() {
		t.Fatalf("decomposed digest = %x, want %x", decomposed.Digest(), composed.Digest())
	}
	if composed.Text() != "caf\u00e9 pr\u00fcfung" {
		t.Fatalf("Text() = %q, want the composed form", composed.Text())
	}
}

func TestNormalizeQuerySeparatesDifferentQuestions(t *testing.T) {
	t.Parallel()

	base := QueryValues{Sort: SortUpdatedDesc, PageSize: 50}

	// Two different questions must never share a digest, or one question's
	// cursor would silently keep paging through the other one's result set.
	tests := []struct {
		name   string
		values QueryValues
	}{
		{name: "empty", values: base},
		{name: "text", values: QueryValues{Text: "disk", Sort: SortUpdatedDesc, PageSize: 50}},
		{name: "other text", values: QueryValues{Text: "disks", Sort: SortUpdatedDesc, PageSize: 50}},
		{name: "one type", values: QueryValues{
			Types: []records.RecordType{records.RecordTypeNote}, Sort: SortUpdatedDesc, PageSize: 50,
		}},
		{name: "two types", values: QueryValues{
			Types:    []records.RecordType{records.RecordTypeNote, records.RecordTypeBilling},
			Sort:     SortUpdatedDesc,
			PageSize: 50,
		}},
		{name: "tag ab and c", values: QueryValues{
			Tags: []string{"ab", "c"}, Sort: SortUpdatedDesc, PageSize: 50,
		}},
		{name: "tag a and bc", values: QueryValues{
			Tags: []string{"a", "bc"}, Sort: SortUpdatedDesc, PageSize: 50,
		}},
		{name: "owner as participant", values: QueryValues{
			OwnerIDs: []string{"usr_000000000000000000000001"}, Sort: SortUpdatedDesc, PageSize: 50,
		}},
		{name: "participant as owner", values: QueryValues{
			ParticipantIDs: []string{"usr_000000000000000000000001"}, Sort: SortUpdatedDesc, PageSize: 50,
		}},
		{name: "subject kind only", values: QueryValues{
			Subjects: []SubjectFilter{{Kind: records.SubjectKindVPS}}, Sort: SortUpdatedDesc, PageSize: 50,
		}},
		{name: "subject kind as primary", values: QueryValues{
			Subjects: []SubjectFilter{{Kind: records.SubjectKindVPS, Placement: SubjectPlacementPrimary}},
			Sort:     SortUpdatedDesc, PageSize: 50,
		}},
		{name: "subject kind with role", values: QueryValues{
			Subjects: []SubjectFilter{{Kind: records.SubjectKindVPS, Role: records.RelationRoleAffected}},
			Sort:     SortUpdatedDesc, PageSize: 50,
		}},
		{name: "follow up overdue", values: QueryValues{
			FollowUp: FollowUpOverdue, Sort: SortUpdatedDesc, PageSize: 50,
		}},
		{name: "follow up scheduled", values: QueryValues{
			FollowUp: FollowUpScheduled, Sort: SortUpdatedDesc, PageSize: 50,
		}},
		{name: "open action", values: QueryValues{
			Action: ActionOpen, Sort: SortUpdatedDesc, PageSize: 50,
		}},
		{name: "ascending sort", values: QueryValues{Sort: SortUpdatedAsc, PageSize: 50}},
		{name: "smaller page", values: QueryValues{Sort: SortUpdatedDesc, PageSize: 25}},
	}

	digests := make(map[[32]byte]string, len(tests))
	for _, tt := range tests {
		normalized, err := NormalizeQuery(tt.values)
		if err != nil {
			t.Fatalf("NormalizeQuery(%s) error = %v", tt.name, err)
		}
		if previous, exists := digests[normalized.Digest()]; exists {
			t.Fatalf("%s shares a digest with %s", tt.name, previous)
		}
		digests[normalized.Digest()] = tt.name
	}
}

func TestNormalizeQueryRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	future := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	past := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	zero := time.Time{}
	longText := make([]rune, MaxQueryTextRunes+1)
	for index := range longText {
		longText[index] = 'x'
	}
	tooManyTags := make([]string, MaxQueryFilterValues+1)
	for index := range tooManyTags {
		tooManyTags[index] = string(rune('a'+index%26)) + string(rune('a'+index/26))
	}

	tests := []struct {
		name   string
		values QueryValues
	}{
		{name: "unknown type", values: QueryValues{Types: []records.RecordType{"custom"}}},
		{name: "unknown status", values: QueryValues{Statuses: []records.BusinessStatus{"almost"}}},
		{name: "unknown status group", values: QueryValues{StatusGroups: []records.StatusGroup{"soon"}}},
		{name: "unknown lifecycle", values: QueryValues{Lifecycles: []records.Lifecycle{"deleted"}}},
		{name: "unknown sort", values: QueryValues{Sort: "title_asc"}},
		{name: "unknown sort", values: QueryValues{Sort: Sort("relevance_desc")}},
		{name: "text over budget", values: QueryValues{Text: string(longText)}},
		{name: "text with control character", values: QueryValues{Text: "disk\x00fail"}},
		{name: "invalid utf8 text", values: QueryValues{Text: "disk\xff"}},
		{name: "empty tag", values: QueryValues{Tags: []string{""}}},
		{name: "whitespace tag", values: QueryValues{Tags: []string{"   "}}},
		{name: "duplicate tag", values: QueryValues{Tags: []string{"disk", "DISK"}}},
		{name: "too many tags", values: QueryValues{Tags: tooManyTags}},
		{name: "invalid owner", values: QueryValues{OwnerIDs: []string{"root"}}},
		{name: "duplicate owner", values: QueryValues{
			OwnerIDs: []string{"usr_000000000000000000000001", "usr_000000000000000000000001"},
		}},
		{name: "invalid participant", values: QueryValues{ParticipantIDs: []string{"root"}}},
		{name: "unknown subject kind", values: QueryValues{Subjects: []SubjectFilter{{Kind: "subscription"}}}},
		{name: "unknown subject role", values: QueryValues{
			Subjects: []SubjectFilter{{Kind: records.SubjectKindVPS, Role: "billing"}},
		}},
		{name: "subject source id for wrong kind", values: QueryValues{
			Subjects: []SubjectFilter{{Kind: records.SubjectKindVPS, SourceID: "tg_0000000000000001"}},
		}},
		{name: "malformed subject source id", values: QueryValues{
			Subjects: []SubjectFilter{{Kind: records.SubjectKindVPS, SourceID: "vps_short"}},
		}},
		{name: "unknown subject placement", values: QueryValues{
			Subjects: []SubjectFilter{{Kind: records.SubjectKindVPS, Placement: "owner"}},
		}},
		{name: "duplicate subject filter", values: QueryValues{Subjects: []SubjectFilter{
			{Kind: records.SubjectKindVPS, SourceID: "vps_0000000000000001"},
			{Kind: records.SubjectKindVPS, SourceID: "vps_0000000000000001"},
		}}},
		{name: "unknown follow up state", values: QueryValues{FollowUp: "later"}},
		{name: "unknown action state", values: QueryValues{Action: "assigned"}},
		{name: "inverted updated range", values: QueryValues{Updated: TimeRange{From: &future, To: &past}}},
		{name: "empty updated range", values: QueryValues{Updated: TimeRange{From: &past, To: &past}}},
		{name: "inverted occurred range", values: QueryValues{Occurred: TimeRange{From: &future, To: &past}}},
		{name: "zero range bound", values: QueryValues{Updated: TimeRange{From: &zero}}},
		{name: "page size over budget", values: QueryValues{PageSize: MaxPageSize + 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NormalizeQuery(tt.values); !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("NormalizeQuery() error = %v, want %v", err, ErrInvalidQuery)
			}
		})
	}
}

func TestNormalizeQueryAppliesDocumentedDefaults(t *testing.T) {
	t.Parallel()

	normalized, err := NormalizeQuery(QueryValues{})
	if err != nil {
		t.Fatalf("NormalizeQuery() error = %v", err)
	}
	if normalized.Sort() != SortUpdatedDesc {
		t.Fatalf("Sort() = %q, want %q", normalized.Sort(), SortUpdatedDesc)
	}
	if normalized.PageSize() != DefaultPageSize {
		t.Fatalf("PageSize() = %d, want %d", normalized.PageSize(), DefaultPageSize)
	}
	if normalized.Lifecycles() != nil {
		t.Fatalf("Lifecycles() = %v, want nil so the caller decides the default scope", normalized.Lifecycles())
	}
}

// Accessors must hand out copies and UTC instants: the store builds SQL from
// them and a caller that mutated a slice would change a query that has already
// been digested and possibly already encoded into a live cursor.
func TestQueryAccessorsReturnStableCopies(t *testing.T) {
	t.Parallel()

	shanghai := time.FixedZone("CST", 8*60*60)
	from := time.Date(2026, 8, 1, 8, 0, 0, 0, shanghai)
	normalized, err := NormalizeQuery(QueryValues{
		Types:   []records.RecordType{records.RecordTypeNote},
		Tags:    []string{"disk"},
		Updated: TimeRange{From: &from},
	})
	if err != nil {
		t.Fatalf("NormalizeQuery() error = %v", err)
	}

	digest := normalized.Digest()
	types := normalized.Types()
	types[0] = records.RecordTypeBilling
	tags := normalized.Tags()
	tags[0] = "network"
	canonical := normalized.CanonicalBytes()
	for index := range canonical {
		canonical[index] = 0
	}
	if normalized.Digest() != digest {
		t.Fatal("mutating accessor output changed the query digest")
	}
	if got := normalized.Types(); len(got) != 1 || got[0] != records.RecordTypeNote {
		t.Fatalf("Types() = %v, want the normalized value", got)
	}
	if got := normalized.Tags(); len(got) != 1 || got[0] != "disk" {
		t.Fatalf("Tags() = %v, want the normalized value", got)
	}
	updated := normalized.Updated()
	if updated.From == nil || updated.From.Location() != time.UTC || !updated.From.Equal(from) {
		t.Fatalf("Updated().From = %v, want the same instant in UTC", updated.From)
	}
}
