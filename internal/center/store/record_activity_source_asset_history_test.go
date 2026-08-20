package store

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/records"
)

func assetHistoryTestRow() assetHistoryActivityRow {
	occurred := time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)
	return assetHistoryActivityRow{
		factType:    assetFactRenewalDecision,
		factID:      "dec_0001",
		vpsID:       testEvidenceVPSSourceID,
		displayName: "hk-edge-01",
		occurredAt:  occurred,
		createdAt:   occurred.Add(time.Second),
		detail:      "keep",
	}
}

// All four tables are unioned into one source, so all four need a label. One
// without a label would be a fact type that silently never appears.
func TestAssetHistorySourceLabelsAllFourFactTypes(t *testing.T) {
	for _, factType := range []string{
		assetFactRenewalDecision,
		assetFactPriceChange,
		assetFactIPChange,
		assetFactSpecSnapshot,
	} {
		if _, known := assetFactTitles[factType]; !known {
			t.Errorf("fact type %q has no label", factType)
		}
	}
	if got, want := len(AssetHistoryFactTypes()), 4; got != want {
		t.Fatalf("source unions %d fact types, want %d", got, want)
	}
}

// The four tables have independent primary keys. Without a per-table prefix two
// of them could mint the same coordinate and silently merge into one activity.
func TestBuildAssetHistoryCandidateSeparatesTablesThatShareAKey(t *testing.T) {
	namespace := activityTestNamespace()
	seen := map[string]string{}
	for _, factType := range AssetHistoryFactTypes() {
		row := assetHistoryTestRow()
		row.factType = factType
		// The same primary key value in every table, which is what a collision
		// would look like.
		row.factID = "shared_key_0001"
		if factType != assetFactRenewalDecision {
			row.detail = ""
		}

		candidate, err := buildAssetHistoryCandidate(namespace, row)
		if err != nil {
			t.Fatalf("build %s: %v", factType, err)
		}
		if previous, clash := seen[candidate.ActivityID]; clash {
			t.Fatalf("%q collides with %q on activity id %s", factType, previous, candidate.ActivityID)
		}
		seen[candidate.ActivityID] = factType
		if candidate.EventKind != activity.EventKindAssetFactChanged {
			t.Fatalf("%s event kind = %q, want asset_fact_changed", factType, candidate.EventKind)
		}
	}
}

func TestBuildAssetHistoryCandidateProjectsAgainstTheVPS(t *testing.T) {
	candidate, err := buildAssetHistoryCandidate(activityTestNamespace(), assetHistoryTestRow())
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	subject := candidate.Subjects[0]
	if subject.Kind != records.SubjectKindVPS || subject.SourceID != testEvidenceVPSSourceID {
		t.Fatalf("subject = %+v", subject)
	}
	// These are business facts about the VPS itself, so the VPS is affected rather
	// than merely context.
	if subject.Role != records.RelationRoleAffected || !subject.Primary {
		t.Fatalf("subject role = %q primary = %v", subject.Role, subject.Primary)
	}
	if subject.Identity["display_name"] != "hk-edge-01" {
		t.Fatalf("identity = %v", subject.Identity)
	}
	// The decision was made when it was decided, not when the row was written.
	if !candidate.EventAt.Equal(assetHistoryTestRow().occurredAt) {
		t.Fatalf("event time = %s, want the decided time", candidate.EventAt)
	}
	if !candidate.RecordedAt.After(candidate.EventAt) {
		t.Fatalf("recorded time must stay distinct from the occurrence")
	}
}

// A closed enum is vocabulary rather than content, and the resulting decision is
// the one detail that makes a renewal entry actionable.
func TestBuildAssetHistoryCandidateProjectsTheRenewalDecisionEnum(t *testing.T) {
	for decision := range assetRenewalDecisions {
		row := assetHistoryTestRow()
		row.detail = decision

		candidate, err := buildAssetHistoryCandidate(activityTestNamespace(), row)
		if err != nil {
			t.Fatalf("build %s: %v", decision, err)
		}
		if candidate.Presentation.Summary != decision {
			t.Fatalf("summary = %q, want %q", candidate.Presentation.Summary, decision)
		}
	}

	row := assetHistoryTestRow()
	row.detail = "renew_forever"
	if _, err := buildAssetHistoryCandidate(activityTestNamespace(), row); !errors.Is(err, activity.ErrInvalidEventKind) {
		t.Fatalf("a decision outside the enum must be rejected, got %v", err)
	}
}

// An IP address, an SSH host, or a price on a timeline is topology or billing
// detail. The timeline says which fact changed; the VPS page says what it
// changed to.
func TestAssetHistorySourceNeverReadsFactValues(t *testing.T) {
	for _, forbidden := range []string{
		"from_ipv4", "to_ipv4", "from_ipv6", "to_ipv6",
		"ssh_host", "ssh_user", "ssh_port",
		"from_price", "to_price", "from_monthly_price", "to_monthly_price",
		"product_name", "os_name", "reason",
	} {
		if assetHistoryScanReadsColumn(forbidden) {
			t.Errorf("the scan must not read %q", forbidden)
		}
	}
	// The one value column it may read is the closed renewal enum.
	if !assetHistoryScanReadsColumn("to_decision") {
		t.Error("the renewal decision enum is the one projectable detail and must be read")
	}
}

// Only renewals carry a projectable detail. A non-renewal arriving with one means
// a value column leaked into the scan, which is worth failing over rather than
// quietly dropping.
func TestBuildAssetHistoryCandidateRefusesALeakedValue(t *testing.T) {
	row := assetHistoryTestRow()
	row.factType = assetFactIPChange
	row.detail = "203.0.113.7"

	if _, err := buildAssetHistoryCandidate(activityTestNamespace(), row); !errors.Is(err, activity.ErrInvalidPresentation) {
		t.Fatalf("error = %v, want ErrInvalidPresentation", err)
	}
}

func TestBuildAssetHistoryCandidateRejectsRowsItCannotProject(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(*assetHistoryActivityRow)
		want   error
	}{
		"a fact type the union does not produce": {
			mutate: func(row *assetHistoryActivityRow) { row.factType = "xyz" },
			want:   activity.ErrInvalidEventKind,
		},
		"a vps id no route resolves": {
			mutate: func(row *assetHistoryActivityRow) { row.vpsID = "vps_nope" },
			want:   activity.ErrUnreachableCandidate,
		},
		"a fact with no occurrence time": {
			mutate: func(row *assetHistoryActivityRow) { row.occurredAt = time.Time{} },
			want:   activity.ErrInvalidEventTime,
		},
	} {
		t.Run(name, func(t *testing.T) {
			row := assetHistoryTestRow()
			test.mutate(&row)
			if _, err := buildAssetHistoryCandidate(activityTestNamespace(), row); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNewAssetHistoryActivitySourceRejectsUnusableConfiguration(t *testing.T) {
	if _, err := NewAssetHistoryActivitySource(nil, activityTestNamespace()); err == nil {
		t.Fatal("a source without a pool must be rejected")
	}
	if _, err := NewAssetHistoryActivitySource(&pgxpool.Pool{}, activity.Namespace{}); err == nil {
		t.Fatal("a source without a project namespace must be rejected")
	}
}
