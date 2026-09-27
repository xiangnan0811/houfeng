package assetlifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const ArchiveQuietPeriod = 180 * time.Minute
const ReceiverHealthTTL = 15 * time.Second
const ArchivePreviewTTL = 5 * time.Minute

// EvaluateArchiveSafety is deterministic at the Center observation time. A
// session which has not sent a signal still cannot use the never-enrolled bypass.
func EvaluateArchiveSafety(e ArchiveOnlineEvidence) (ArchiveOnlineEvidence, []BlockerDetail) {
	e.NeverConnected = true
	var latest *time.Time
	for _, instance := range e.Instances {
		if instance.SessionID != "" || instance.EverConnected || instance.LastTrustedOnlineAt != nil {
			e.NeverConnected = false
		}
		for _, candidate := range []*time.Time{instance.SessionStartedAt, instance.LastTrustedOnlineAt} {
			if candidate != nil && (latest == nil || candidate.After(*latest)) {
				t := *candidate
				latest = &t
			}
		}
	}
	e.ManualConfirmationRequired = e.NeverConnected
	if e.NeverConnected {
		return e, nil
	}
	var blockers []BlockerDetail
	add := func(code, id string) {
		blockers = append(blockers, BlockerDetail{Code: code, ObjectType: "monitoring_instance", ObjectID: id, BlockedAction: "archive", ResolutionAction: "wait_for_continuous_offline_observation"})
	}
	healthy := e.ReceiverHealthy && e.HealthySince != nil && e.LastHealthCheckAt != nil && !e.ObservedAt.Before(*e.LastHealthCheckAt) && e.ObservedAt.Sub(*e.LastHealthCheckAt) <= ReceiverHealthTTL && !e.HealthySince.After(e.ObservedAt)
	e.ReceiverHealthy = healthy
	if !healthy {
		add("receiver_observation_unhealthy", "")
		e.EarliestArchiveAt = nil
		return e, blockers
	}
	start := *e.HealthySince
	if latest != nil && latest.After(start) {
		start = *latest
	}
	earliest := start.Add(ArchiveQuietPeriod)
	e.EarliestArchiveAt = &earliest
	if e.ObservedAt.Before(earliest) {
		add("continuous_offline_window_incomplete", "")
		for _, instance := range e.Instances {
			if instance.LastTrustedOnlineAt != nil && e.ObservedAt.Before(instance.LastTrustedOnlineAt.Add(ArchiveQuietPeriod)) {
				add("recent_trusted_online_signal", instance.MonitoringInstanceID)
			}
		}
	}
	return e, blockers
}

// Exclude advancing wall-clock/check timestamps, but include health generation,
// its continuity origin, durable live evidence and every affected dependency.
func DigestArchiveReview(review ArchiveReview) string {
	review.PreviewDigest = ""
	review.PreviewExpiresAt = time.Time{}
	review.OnlineEvidence.ObservedAt = time.Time{}
	review.OnlineEvidence.LastHealthCheckAt = nil
	encoded, err := json.Marshal(review)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func AttachArchivePreviewDigest(review *ArchiveReview) {
	issuedAt := review.OnlineEvidence.ObservedAt.UTC().Truncate(time.Second)
	review.PreviewExpiresAt = issuedAt.Add(ArchivePreviewTTL)
	review.PreviewDigest = strconv.FormatInt(issuedAt.Unix(), 10) + "." + DigestArchiveReview(*review)
}

func MatchesArchivePreview(review ArchiveReview, token string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	issued, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	issuedAt := time.Unix(issued, 0)
	if review.OnlineEvidence.ObservedAt.Before(issuedAt) || !review.OnlineEvidence.ObservedAt.Before(issuedAt.Add(ArchivePreviewTTL)) {
		return false
	}
	return parts[1] == DigestArchiveReview(review)
}

func DigestArchiveRequest(input ApplyArchiveInput) string {
	encoded, _ := json.Marshal(input)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
