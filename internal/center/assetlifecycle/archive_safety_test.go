package assetlifecycle

import (
	"testing"
	"time"
)

func TestArchiveSafetyBoundariesAndNeverConnected(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	old := now.Add(-ArchiveQuietPeriod)
	fresh := now.Add(-ArchiveQuietPeriod + time.Second)
	restarted := now.Add(-30 * time.Second)
	stale := now.Add(-ReceiverHealthTTL - time.Nanosecond)
	for _, test := range []struct {
		name    string
		e       ArchiveOnlineEvidence
		blocked bool
		manual  bool
	}{
		{"exact boundary", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &old, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{MonitoringInstanceID: "mi", SessionID: "s", LastTrustedOnlineAt: &old}}}, false, false},
		{"179m59s", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &old, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s", LastTrustedOnlineAt: &fresh}}}, true, false},
		{"recent receiver restart with old heartbeat", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &restarted, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s", LastTrustedOnlineAt: &old}}}, false, false},
		{"recent receiver restart with recent heartbeat", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &restarted, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s", LastTrustedOnlineAt: &fresh}}}, true, false},
		{"expired receiver", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &old, LastHealthCheckAt: &stale, Instances: []ArchiveInstanceEvidence{{SessionID: "s", LastTrustedOnlineAt: &old}}}, true, false},
		{"never issued session", ArchiveOnlineEvidence{ObservedAt: now, Instances: []ArchiveInstanceEvidence{{MonitoringInstanceID: "pending"}}}, false, true},
		{"issued no samples", ArchiveOnlineEvidence{ObservedAt: now, Instances: []ArchiveInstanceEvidence{{SessionID: "s"}}}, true, false},
		{"reinstall pending", ArchiveOnlineEvidence{ObservedAt: now, Instances: []ArchiveInstanceEvidence{{EverConnected: true}}}, true, false},
		{"new session no live", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &old, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s", SessionStartedAt: &now}}}, true, false},
		{"old session no live", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &restarted, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s", SessionStartedAt: &old}}}, false, false},
		{"missing durable times with healthy receiver", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &old, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s"}}}, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, blockers := EvaluateArchiveSafety(test.e)
			if (len(blockers) > 0) != test.blocked || e.ManualConfirmationRequired != test.manual {
				t.Fatalf("evidence=%+v blockers=%+v", e, blockers)
			}
			if !test.blocked && !test.manual && (e.EarliestArchiveAt == nil || !e.EarliestArchiveAt.Equal(now)) {
				t.Fatalf("earliest archive time = %v, want %v", e.EarliestArchiveAt, now)
			}
		})
	}
}

func TestArchiveSafetyEarliestUsesNewestEvidenceAcrossHistoricalSessions(t *testing.T) {
	now := time.Date(2026, 11, 28, 7, 43, 0, 0, time.UTC)
	restarted := now.Add(-90 * time.Minute)
	old := now.Add(-72 * time.Hour)
	latest := now.Add(-2 * time.Hour)
	e, blockers := EvaluateArchiveSafety(ArchiveOnlineEvidence{
		ObservedAt: now, ReceiverHealthy: true, HealthySince: &restarted, LastHealthCheckAt: &now,
		Instances: []ArchiveInstanceEvidence{
			{MonitoringInstanceID: "current", SessionID: "current-session", LastTrustedOnlineAt: &old},
			{MonitoringInstanceID: "historical", SessionID: "old-session", LastTrustedOnlineAt: &latest},
		},
	})
	if e.EarliestArchiveAt == nil || !e.EarliestArchiveAt.Equal(latest.Add(ArchiveQuietPeriod)) {
		t.Fatalf("earliest archive time = %v, want %v", e.EarliestArchiveAt, latest.Add(ArchiveQuietPeriod))
	}
	if len(blockers) != 2 || blockers[1].Code != "recent_trusted_online_signal" || blockers[1].ObjectID != "historical" {
		t.Fatalf("unexpected blockers: %+v", blockers)
	}
}

func TestArchiveDigestIgnoresClockTickButIncludesLiveAndGeneration(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-ArchiveQuietPeriod)
	review := ArchiveReview{OnlineEvidence: ArchiveOnlineEvidence{ObservedAt: now, ReceiverGeneration: "boot1", HealthySince: &old, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s", LastTrustedOnlineAt: &old}}}}
	want := DigestArchiveReview(review)
	next := now.Add(5 * time.Second)
	review.OnlineEvidence.ObservedAt = next
	review.OnlineEvidence.LastHealthCheckAt = &next
	if DigestArchiveReview(review) != want {
		t.Fatal("ordinary health tick invalidated preview")
	}
	review.OnlineEvidence.Instances[0].LastTrustedOnlineAt = &next
	if DigestArchiveReview(review) == want {
		t.Fatal("new live evidence did not invalidate preview")
	}
	review.OnlineEvidence.Instances[0].LastTrustedOnlineAt = &old
	review.OnlineEvidence.ReceiverGeneration = "boot2"
	if DigestArchiveReview(review) == want {
		t.Fatal("receiver restart did not invalidate preview")
	}
}

func TestArchivePreviewExpirationAndFutureClock(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	review := ArchiveReview{OnlineEvidence: ArchiveOnlineEvidence{ObservedAt: now}}
	AttachArchivePreviewDigest(&review)
	token := review.PreviewDigest
	review.OnlineEvidence.ObservedAt = now.Add(ArchivePreviewTTL - time.Nanosecond)
	if !MatchesArchivePreview(review, token) {
		t.Fatal("unexpired unchanged preview rejected")
	}
	review.OnlineEvidence.ObservedAt = now.Add(ArchivePreviewTTL)
	if MatchesArchivePreview(review, token) {
		t.Fatal("expired preview accepted")
	}
	review.OnlineEvidence.ObservedAt = now.Add(-time.Second)
	if MatchesArchivePreview(review, token) {
		t.Fatal("future preview accepted after clock reversal")
	}
}
