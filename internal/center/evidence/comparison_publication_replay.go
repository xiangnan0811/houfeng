package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordidentity"
)

// VerifyComparisonSaveReplay authenticates a historical comparison intent and
// derives the ordered snapshot identities it would have produced. An expired
// authenticated intent is usable for this comparison only; it is not a live
// credential. The caller must independently authorize the stored first revision
// and compare its complete ordered evidence references with these identities.
func (service *Service) VerifyComparisonSaveReplay(
	ctx context.Context, actor recordauth.ActorScope, recordID string, token string,
) ([]string, error) {
	if ctx == nil {
		return nil, ErrInvalidComparisonSelection
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if service == nil || nilRevisionPreparationDependency(service.comparisonSigner) {
		return nil, ErrComparisonIntentUnavailable
	}
	if !recordidentity.IsValid(recordID) || token == "" {
		return nil, ErrComparisonIntentInvalid
	}
	normalized, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return nil, ErrComparisonIntentInvalid
	}
	claims, err := service.comparisonSigner.Verify(token, time.Now().UTC())
	if err != nil && !errors.Is(err, ErrComparisonIntentExpired) {
		return nil, err
	}
	actorHash := normalized.CanonicalHash()
	if claims.Purpose != ComparisonIntentPurpose || claims.ProjectID != string(normalized.ProjectID) ||
		claims.ActorHash != hex.EncodeToString(actorHash[:]) || claims.IssuedAt.IsZero() ||
		!claims.ExpiresAt.After(claims.IssuedAt) {
		return nil, ErrComparisonIntentInvalid
	}
	items, err := comparisonReplayItems(claims)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items)+1)
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.SnapshotID == "" {
			continue
		}
		id := DeriveComparisonCopySnapshotID(recordID, item.SnapshotID, claims.Digest)
		if _, exists := seen[id]; exists {
			return nil, ErrComparisonIntentInvalid
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	ids = append(ids, DeriveComparisonResultSnapshotID(recordID, claims.Digest))
	return ids, nil
}

// comparisonReplayItems restores exactly the fields included in the existing
// comparisonDigestBody. Source ownership is authenticated by the signed claims
// but, as in the original codec, is not added to its canonical digest.
func comparisonReplayItems(claims ComparisonIntentClaims) ([]ResolvedComparisonItem, error) {
	if len(claims.Items) < 2 || len(claims.Items) > 6 ||
		claims.BaselineIndex < 0 || claims.BaselineIndex >= len(claims.Items) ||
		(claims.Alignment != CoverageActual && claims.Alignment != CoverageCommonOverlap) ||
		claims.RegistryVersion != "evidence-kinds/v1" || claims.CalculationVersion != ComparisonCalculationVersion ||
		!canonicalComparisonReplayHash(claims.Digest) || !canonicalComparisonReplayHash(claims.WarningsDigest) {
		return nil, ErrComparisonIntentInvalid
	}
	window, err := parseComparisonClaimWindow(claims)
	if err != nil || window.Start.IsZero() || !window.End.After(window.Start) ||
		window.Start.Format(time.RFC3339Nano) != claims.RequestedStart ||
		window.End.Format(time.RFC3339Nano) != claims.RequestedEnd {
		return nil, ErrComparisonIntentInvalid
	}
	const maxSeconds = int64((1<<63 - 1) / int64(time.Second))
	const minSeconds = int64((-1 << 63) / int64(time.Second))
	if claims.ToleranceSeconds < minSeconds || claims.ToleranceSeconds > maxSeconds {
		return nil, ErrComparisonIntentInvalid
	}
	var detail *ComparisonDetail
	if claims.DetailKind != "" {
		detail = comparisonDetailFromClaims(claims)
		if _, err := ParseKindKey(detail.Kind.String()); err != nil {
			return nil, ErrComparisonIntentInvalid
		}
	} else if claims.DetailSchemaVersion != 0 || claims.DetailMetric != "" {
		return nil, ErrComparisonIntentInvalid
	}

	items := make([]ResolvedComparisonItem, 0, len(claims.Items))
	for _, claim := range claims.Items {
		// SourceRecordID was added after the original signed token format and
		// is deliberately outside its canonical comparison digest.
		if (claim.SourceRecordID != "" && !recordidentity.IsValid(claim.SourceRecordID)) || !canonicalComparisonReplayHash(claim.Hash) {
			return nil, ErrComparisonIntentInvalid
		}
		var hash [sha256.Size]byte
		if _, err := hex.Decode(hash[:], []byte(claim.Hash)); err != nil {
			return nil, ErrComparisonIntentInvalid
		}
		item := ResolvedComparisonItem{
			SnapshotID: claim.SnapshotID, Hash: hash, RevisionContext: claim.RevisionContext,
			RecordID: claim.RecordID, RevisionID: claim.RevisionID, SourceRecordID: claim.SourceRecordID,
		}
		if claim.SnapshotID == "" {
			if claim.Kind != (KindKey{}).String() || hash != [sha256.Size]byte{} {
				return nil, ErrComparisonIntentInvalid
			}
		} else {
			if !ValidSnapshotID(claim.SnapshotID) {
				return nil, ErrComparisonIntentInvalid
			}
			item.Kind, err = ParseKindKey(claim.Kind)
			if err != nil {
				return nil, ErrComparisonIntentInvalid
			}
		}
		switch claim.RevisionContext {
		case RevisionContextBound:
			if !recordidentity.IsValid(claim.RecordID) ||
				(claim.SourceRecordID != "" && claim.RecordID != claim.SourceRecordID) ||
				!validClosedPreparedID(claim.RevisionID, "rrv_") {
				return nil, ErrComparisonIntentInvalid
			}
			if claim.SnapshotID == "" && len(claim.ChosenSnapshotIDs) != 0 ||
				claim.SnapshotID != "" && (len(claim.ChosenSnapshotIDs) != 1 || claim.ChosenSnapshotIDs[0] != claim.SnapshotID) {
				return nil, ErrComparisonIntentInvalid
			}
			item.Revision = &RevisionMetadataSnapshot{
				RecordType: claim.RecordType, BusinessStatus: claim.BusinessStatus,
				StatusGroup: claim.StatusGroup, ImpactLevel: claim.ImpactLevel,
			}
			if claim.OccurredAt != "" {
				occurredAt, err := time.Parse(time.RFC3339Nano, claim.OccurredAt)
				if err != nil || occurredAt.UTC().Format(time.RFC3339Nano) != claim.OccurredAt {
					return nil, ErrComparisonIntentInvalid
				}
				item.Revision.OccurredAt, item.Revision.HasOccurredAt = occurredAt.UTC(), true
			}
		case RevisionContextNotApplicable:
			if claim.SnapshotID == "" || claim.RecordID != "" || claim.RevisionID != "" || len(claim.ChosenSnapshotIDs) != 0 ||
				claim.RecordType != "" || claim.BusinessStatus != "" || claim.StatusGroup != "" ||
				claim.ImpactLevel != "" || claim.OccurredAt != "" {
				return nil, ErrComparisonIntentInvalid
			}
		default:
			return nil, ErrComparisonIntentInvalid
		}
		items = append(items, item)
	}
	digest, err := comparisonDigest(comparisonDigestBody{
		Items: items, BaselineIndex: claims.BaselineIndex, Alignment: claims.Alignment,
		RequestedWindow: window, Tolerance: time.Duration(claims.ToleranceSeconds) * time.Second, Detail: detail,
	})
	if err != nil || hex.EncodeToString(digest[:]) != claims.Digest {
		return nil, ErrComparisonIntentInvalid
	}
	return items, nil
}

func canonicalComparisonReplayHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
