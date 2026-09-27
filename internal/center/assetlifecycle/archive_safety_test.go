package assetlifecycle

import (
	"testing"
	"time"
)

func TestArchiveSafetyBoundariesAndNeverConnected(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	old := now.Add(-ArchiveQuietPeriod)
	fresh := now.Add(-ArchiveQuietPeriod + time.Second)
	stale := now.Add(-ReceiverHealthTTL - time.Nanosecond)
	for _, test := range []struct {
		name    string
		e       ArchiveOnlineEvidence
		blocked bool
		manual  bool
	}{
		{"exact boundary", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &old, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{MonitoringInstanceID: "mi", SessionID: "s", LastTrustedOnlineAt: &old}}}, false, false},
		{"179m59s", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &old, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s", LastTrustedOnlineAt: &fresh}}}, true, false},
		{"new receiver", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &fresh, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s", LastTrustedOnlineAt: &old}}}, true, false},
		{"expired receiver", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &old, LastHealthCheckAt: &stale, Instances: []ArchiveInstanceEvidence{{SessionID: "s", LastTrustedOnlineAt: &old}}}, true, false},
		{"never issued session", ArchiveOnlineEvidence{ObservedAt: now, Instances: []ArchiveInstanceEvidence{{MonitoringInstanceID: "pending"}}}, false, true},
		{"issued no samples", ArchiveOnlineEvidence{ObservedAt: now, Instances: []ArchiveInstanceEvidence{{SessionID: "s"}}}, true, false},
		{"reinstall pending", ArchiveOnlineEvidence{ObservedAt: now, Instances: []ArchiveInstanceEvidence{{EverConnected: true}}}, true, false},
		{"new session no live", ArchiveOnlineEvidence{ObservedAt: now, ReceiverHealthy: true, HealthySince: &old, LastHealthCheckAt: &now, Instances: []ArchiveInstanceEvidence{{SessionID: "s", SessionStartedAt: &now}}}, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			e, blockers := EvaluateArchiveSafety(test.e)
			if (len(blockers) > 0) != test.blocked || e.ManualConfirmationRequired != test.manual {
				t.Fatalf("evidence=%+v blockers=%+v", e, blockers)
			}
		})
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
