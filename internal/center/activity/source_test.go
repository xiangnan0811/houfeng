package activity

import (
	"testing"
	"time"
)

func testHeadMoment() time.Time {
	return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
}

// An incremental head lags on purpose. A transaction that started before the
// watermark can still commit after we read it, so scanning right up to "now"
// would step over rows that were never visible.
func TestIncrementalHeadLagsBehindTheClock(t *testing.T) {
	head := NewIncrementalSourceHead(SourceKindRecordDomain, testHeadMoment(), DefaultSourceSafetyLag)
	if !head.RecordedThrough.Before(testHeadMoment()) {
		t.Fatalf("incremental head %s does not lag behind %s", head.RecordedThrough, testHeadMoment())
	}
	if got, want := testHeadMoment().Sub(head.RecordedThrough), DefaultSourceSafetyLag; got != want {
		t.Fatalf("head lag = %s, want %s", got, want)
	}
	if head.Kind != SourceKindRecordDomain {
		t.Fatalf("head kind = %q, want %q", head.Kind, SourceKindRecordDomain)
	}
}

// This is the whole point of the hybrid rule. A lagging watermark is good enough
// to keep projecting, because a late row is re-scanned and lands in its true
// chronological position. It is not good enough to tell Child 10 that an archive
// is complete, so it must refuse to back that claim.
func TestIncrementalHeadCannotBackACompletenessClaim(t *testing.T) {
	incremental := NewIncrementalSourceHead(SourceKindRecordDomain, testHeadMoment(), DefaultSourceSafetyLag)
	if incremental.SupportsCompletenessClaim() {
		t.Fatal("an incremental head must not be usable as export completeness evidence")
	}

	settled := NewSettledSourceHead(SourceKindRecordDomain, testHeadMoment(), 918273)
	if !settled.SupportsCompletenessClaim() {
		t.Fatal("a head carrying a transaction horizon must support a completeness claim")
	}
	if settled.TransactionHorizon != 918273 {
		t.Fatalf("transaction horizon = %d, want 918273", settled.TransactionHorizon)
	}
}

// The trailing window is what turns "eventually visible" into "eventually
// projected". It has to reach back past the checkpoint, or a row that committed
// just under the previous watermark is never looked at again.
func TestScanWindowReachesBackPastTheCheckpoint(t *testing.T) {
	checkpoint := SourceCheckpoint{
		Kind:            SourceKindRecordDomain,
		RecordedThrough: testHeadMoment().Add(-time.Hour),
	}
	head := NewIncrementalSourceHead(SourceKindRecordDomain, testHeadMoment(), DefaultSourceSafetyLag)

	trailing, ok := checkpoint.TrailingWindow(DefaultReprojectionWindow)
	if !ok {
		t.Fatalf("a checkpoint with a position must have a trailing window")
	}
	if !trailing.From.Before(checkpoint.RecordedThrough) {
		t.Fatalf("trailing window starts at %s, which does not reach back past the checkpoint %s",
			trailing.From, checkpoint.RecordedThrough)
	}
	if got, want := checkpoint.RecordedThrough.Sub(trailing.From), DefaultReprojectionWindow; got != want {
		t.Fatalf("trailing overlap = %s, want %s", got, want)
	}
	// The trailing window stops at the checkpoint. Everything above it belongs to
	// the forward window, which is what advances the position.
	if !trailing.Through.Equal(checkpoint.RecordedThrough) {
		t.Fatalf("trailing window ends at %s, want the checkpoint %s", trailing.Through, checkpoint.RecordedThrough)
	}

	frontier := checkpoint.FrontierWindow(head)
	if !frontier.From.Equal(checkpoint.RecordedThrough) {
		t.Fatalf("forward window starts at %s, want the checkpoint %s so boundary ties are re-read",
			frontier.From, checkpoint.RecordedThrough)
	}
	if !frontier.Through.Equal(head.RecordedThrough) {
		t.Fatalf("forward window ends at %s, want the head %s", frontier.Through, head.RecordedThrough)
	}
}

// A first run has no checkpoint. It must scan from the beginning rather than
// from "now minus a window", which would skip all existing history.
func TestFirstScanCoversAllHistory(t *testing.T) {
	head := NewIncrementalSourceHead(SourceKindRecordDomain, testHeadMoment(), DefaultSourceSafetyLag)
	checkpoint := SourceCheckpoint{Kind: SourceKindRecordDomain}
	window := checkpoint.FrontierWindow(head)
	if !window.From.IsZero() {
		t.Fatalf("first scan starts at %s, want the zero time so nothing is skipped", window.From)
	}
	if !window.Through.Equal(head.RecordedThrough) {
		t.Fatalf("first scan ends at %s, want %s", window.Through, head.RecordedThrough)
	}
	// Re-reading behind a position that does not exist yet would scan a window
	// the forward pass already covers.
	if _, ok := checkpoint.TrailingWindow(DefaultReprojectionWindow); ok {
		t.Fatalf("a first run must not have a trailing window")
	}
}

// A window whose head is behind the checkpoint means the clock or the source
// moved backwards. Scanning it would be a no-op that still advanced the
// checkpoint, so it has to be reported rather than silently accepted.
func TestScanWindowRejectsAHeadBehindTheCheckpoint(t *testing.T) {
	checkpoint := SourceCheckpoint{
		Kind:            SourceKindRecordDomain,
		RecordedThrough: testHeadMoment(),
	}
	stale := NewIncrementalSourceHead(SourceKindRecordDomain, testHeadMoment().Add(-2*time.Hour), DefaultSourceSafetyLag)
	if err := checkpoint.ValidateHead(stale); err == nil {
		t.Fatal("a head behind the checkpoint was accepted")
	}
	fresh := NewIncrementalSourceHead(SourceKindRecordDomain, testHeadMoment().Add(time.Hour), DefaultSourceSafetyLag)
	if err := checkpoint.ValidateHead(fresh); err != nil {
		t.Fatalf("a forward head was rejected: %v", err)
	}
	if err := checkpoint.ValidateHead(NewIncrementalSourceHead(SourceKindCommandAudit, testHeadMoment().Add(time.Hour), DefaultSourceSafetyLag)); err == nil {
		t.Fatal("a head from a different source was accepted")
	}
}

// Readiness is the only thing Child 10 may trust. It fails closed on anything
// that would let an incomplete archive look complete.
func TestReadinessVectorFailsClosedOnIncompleteEvidence(t *testing.T) {
	snapshot := ActivitySnapshot{ProjectionGeneration: 4, PublishedIngestSequence: 900}
	settled := NewSettledSourceHead(SourceKindRecordDomain, testHeadMoment(), 918273)

	ready := ReadinessVector{
		Snapshot: snapshot,
		Sources: []SourceReadiness{
			{Kind: SourceKindRecordDomain, Head: settled, CaughtUp: true},
		},
	}
	if err := ready.ValidateForExport([]SourceKind{SourceKindRecordDomain}); err != nil {
		t.Fatalf("a caught-up settled vector was rejected: %v", err)
	}

	cases := map[string]ReadinessVector{
		"missing snapshot generation": {
			Snapshot: ActivitySnapshot{PublishedIngestSequence: 900},
			Sources:  []SourceReadiness{{Kind: SourceKindRecordDomain, Head: settled, CaughtUp: true}},
		},
		"source not caught up": {
			Snapshot: snapshot,
			Sources:  []SourceReadiness{{Kind: SourceKindRecordDomain, Head: settled, CaughtUp: false}},
		},
		"incremental head only": {
			Snapshot: snapshot,
			Sources: []SourceReadiness{{
				Kind:     SourceKindRecordDomain,
				Head:     NewIncrementalSourceHead(SourceKindRecordDomain, testHeadMoment(), DefaultSourceSafetyLag),
				CaughtUp: true,
			}},
		},
		"missing a required source": {
			Snapshot: snapshot,
			Sources:  []SourceReadiness{},
		},
	}
	for name, vector := range cases {
		t.Run(name, func(t *testing.T) {
			if err := vector.ValidateForExport([]SourceKind{SourceKindRecordDomain}); err == nil {
				t.Fatalf("%s was accepted as export evidence", name)
			}
		})
	}
}

// The digest is what proves two export passes saw the same projection. It has to
// move when anything in the vector moves.
func TestReadinessDigestCoversSnapshotAndEverySource(t *testing.T) {
	settled := NewSettledSourceHead(SourceKindRecordDomain, testHeadMoment(), 918273)
	base := ReadinessVector{
		Snapshot: ActivitySnapshot{ProjectionGeneration: 4, PublishedIngestSequence: 900},
		Sources:  []SourceReadiness{{Kind: SourceKindRecordDomain, Head: settled, CaughtUp: true}},
	}
	baseDigest := base.Digest()

	variants := map[string]ReadinessVector{
		"different generation": {
			Snapshot: ActivitySnapshot{ProjectionGeneration: 5, PublishedIngestSequence: 900},
			Sources:  base.Sources,
		},
		"different watermark": {
			Snapshot: ActivitySnapshot{ProjectionGeneration: 4, PublishedIngestSequence: 901},
			Sources:  base.Sources,
		},
		"different horizon": {
			Snapshot: base.Snapshot,
			Sources: []SourceReadiness{{
				Kind:     SourceKindRecordDomain,
				Head:     NewSettledSourceHead(SourceKindRecordDomain, testHeadMoment(), 918274),
				CaughtUp: true,
			}},
		},
		"extra source": {
			Snapshot: base.Snapshot,
			Sources: append(append([]SourceReadiness{}, base.Sources...), SourceReadiness{
				Kind:     SourceKindCommandAudit,
				Head:     NewSettledSourceHead(SourceKindCommandAudit, testHeadMoment(), 918273),
				CaughtUp: true,
			}),
		},
	}
	for name, variant := range variants {
		t.Run(name, func(t *testing.T) {
			if variant.Digest() == baseDigest {
				t.Fatalf("%s did not change the readiness digest", name)
			}
		})
	}
}

// Source order must not change the digest, or two equivalent vectors would
// disagree purely because a registry iterated differently.
func TestReadinessDigestIsOrderIndependent(t *testing.T) {
	first := SourceReadiness{Kind: SourceKindRecordDomain, Head: NewSettledSourceHead(SourceKindRecordDomain, testHeadMoment(), 1), CaughtUp: true}
	second := SourceReadiness{Kind: SourceKindCommandAudit, Head: NewSettledSourceHead(SourceKindCommandAudit, testHeadMoment(), 2), CaughtUp: true}
	snapshot := ActivitySnapshot{ProjectionGeneration: 1, PublishedIngestSequence: 1}

	forward := ReadinessVector{Snapshot: snapshot, Sources: []SourceReadiness{first, second}}
	reverse := ReadinessVector{Snapshot: snapshot, Sources: []SourceReadiness{second, first}}
	if forward.Digest() != reverse.Digest() {
		t.Fatal("readiness digest depends on source ordering")
	}
}
