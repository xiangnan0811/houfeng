package evidence

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestComparisonIntentClaimsOmitPayloadAndUseSavePurpose(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("comparison-digest"))
	claims := BuildComparisonIntentClaims(ComparisonIntentClaimsInput{
		Actor:           testActor(t),
		Items:           []ResolvedComparisonItem{{SnapshotID: "evs_fixeda", Hash: digest, Kind: CommandAuditV1Key(), RevisionContext: RevisionContextNotApplicable}},
		BaselineIndex:   0,
		Alignment:       CoverageActual,
		RequestedWindow: testWindow(),
		Tolerance:       time.Minute,
		Digest:          digest,
		Review:          []ComparabilityFinding{{Reason: ReasonMetadataOnly}},
		Now:             now,
		KeyID:           "cmp_test",
	})
	if claims.Purpose != ComparisonIntentPurpose || claims.KeyID != "cmp_test" {
		t.Fatalf("claims purpose/key = %#v", claims)
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt) != ComparisonIntentTTL || claims.IssuedAt != now.UTC() {
		t.Fatalf("claims lifetime = %s -> %s", claims.IssuedAt, claims.ExpiresAt)
	}
	encoded, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	body := string(encoded)
	for _, forbidden := range []string{`"payload"`, `"markdown"`, `"title"`, `"canonical_bytes"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("intent claims leaked %s: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, ComparisonIntentPurpose) || !strings.Contains(body, "impact_level") && strings.Contains(body, `"impact"`) {
		t.Fatalf("intent claims missing purpose or used impact instead of impact_level: %s", body)
	}
}
