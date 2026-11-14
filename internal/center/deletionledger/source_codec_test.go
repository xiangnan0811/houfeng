package deletionledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/sourceretirement"
)

func sourceTestScope(t *testing.T, roles []recordauth.Role, groups []string) recordauth.VisibilityScope {
	t.Helper()
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{Version: recordauth.VisibilityScopeVersionV1,
		Kind: recordauth.VisibilityKindRestricted, ProjectID: recordauth.ProjectIDDefault, AllowedRoles: roles,
		AllowedGroupIDs: groups, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 3})
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
func sourceTestRequest(t *testing.T, kind recordauth.SourceKind, id, operation string) SourceRetirementRequest {
	t.Helper()
	live := sourceTestScope(t, []recordauth.Role{recordauth.RoleViewer}, []string{"rag_alpha", "rag_beta"})
	floor := sourceTestScope(t, nil, []string{"rag_alpha"})
	key := sourceretirement.SourceKey{DeploymentID: "dp-" + strings.Repeat("a", 64), ProjectID: recordauth.ProjectIDDefault, Kind: kind, ID: id}
	commitment, err := SourceTokenCommitment(key, sha256.Sum256([]byte("private source token")))
	if err != nil {
		t.Fatal(err)
	}
	request := SourceRetirementRequest{OperationID: operation, ActorUserID: "usr_" + strings.Repeat("b", 24),
		Origin: SourceOrigin{Application: SourceApplicationIdentity{ID: "rd-" + strings.Repeat("c", 64), SystemIdentifier: "123456789", DatabaseOID: 16384, DatabaseName: "application", Epoch: 1},
			Source: key, Revision: sha256.Sum256([]byte("original source revision")), LastLive: live, TombstoneRecordedAt: time.UnixMicro(1700000000000000).UTC()},
		FinalFloor: floor, TokenCommitment: commitment}
	request.RequestFingerprint, err = SourceRequestFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
func sourceTestEntry(t *testing.T) SourceRetirementEntry {
	t.Helper()
	entry := SourceRetirementEntry{Request: sourceTestRequest(t, recordauth.SourceKindVPS, "vps_0123456789abcdef", "sro_first"),
		LedgerDomain: sha256.Sum256([]byte("W1 domain")), Sequence: 1, ConfirmedAt: time.UnixMicro(1700000001000000).UTC()}
	var err error
	entry.Canonical, err = EncodeSourceRetirementEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.EntryHash = sha256.Sum256(entry.Canonical)
	return entry
}

// Wire builder deliberately does not call the production framing or codec.
func sourceTestWire(fields ...any) []byte {
	var body []byte
	for _, field := range fields {
		switch value := field.(type) {
		case string:
			body = binary.BigEndian.AppendUint32(body, uint32(len(value)))
			body = append(body, value...)
		case []byte:
			body = binary.BigEndian.AppendUint32(body, uint32(len(value)))
			body = append(body, value...)
		case uint64:
			body = binary.BigEndian.AppendUint64(body, value)
		default:
			panic("unsupported wire test field")
		}
	}
	return body
}

func TestSourceRetirementCanonicalWireAndCopies(t *testing.T) {
	for _, tc := range []struct {
		kind recordauth.SourceKind
		id   string
	}{
		{recordauth.SourceKindVPS, "vps_0123456789abcdef"},
		{recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef"},
		{recordauth.SourceKindTarget, "tg_0123456789abcdef"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			r := sourceTestRequest(t, tc.kind, tc.id, "sro_wire")
			key := r.Origin.Source
			token := sha256.Sum256([]byte("private source token"))
			wantToken := sha256.Sum256(sourceTestWire("HOUFENG-SOURCE-RETIREMENT-TOKEN-V1", key.DeploymentID, string(key.ProjectID), token[:]))
			if r.TokenCommitment != wantToken {
				t.Fatal("token commitment wire mismatch")
			}
			live, floor := r.Origin.LastLive.CanonicalBytes(), r.FinalFloor.CanonicalBytes()
			wantOrigin := sourceTestWire("HOUFENG-SOURCE-RETIREMENT-ORIGIN-V1", uint64(1), r.Origin.Application.ID,
				r.Origin.Application.SystemIdentifier, uint64(r.Origin.Application.DatabaseOID), r.Origin.Application.DatabaseName, r.Origin.Application.Epoch,
				"retire_source_authorization", key.DeploymentID, string(key.ProjectID), string(key.Kind), key.ID, r.Origin.Revision[:], live, uint64(r.Origin.TombstoneRecordedAt.UnixMicro()))
			gotOrigin, err := EncodeSourceOrigin(r.Origin)
			if err != nil || !bytes.Equal(gotOrigin, wantOrigin) {
				t.Fatalf("origin wire mismatch: %v", err)
			}
			wantFingerprint := sha256.Sum256(sourceTestWire("HOUFENG-SOURCE-RETIREMENT-REQUEST-V1", key.DeploymentID, string(key.ProjectID), r.OperationID, r.ActorUserID,
				string(key.Kind), key.ID, r.TokenCommitment[:], wantOrigin, live, floor, uint64(2)))
			if r.RequestFingerprint != wantFingerprint {
				t.Fatal("fingerprint wire mismatch")
			}
			entry := sourceTestEntry(t)
			entry.Request = r
			entry.Canonical, err = EncodeSourceRetirementEntry(entry)
			if err != nil {
				t.Fatal(err)
			}
			entry.EntryHash = sha256.Sum256(entry.Canonical)
			floorHash := sha256.Sum256(floor)
			want := sourceTestWire("HOUFENG-SOURCE-RETIREMENT-LEDGER-V1", uint64(1), entry.LedgerDomain[:], entry.Sequence, entry.PreviousHash[:],
				"delete_commit", key.DeploymentID, string(key.ProjectID), r.OperationID, r.ActorUserID, "source_permanent_delete", string(key.Kind), key.ID,
				wantOrigin, floor, floorHash[:], r.TokenCommitment[:], r.RequestFingerprint[:], "user_confirmed", uint64(2), uint64(0), uint64(entry.ConfirmedAt.UnixMicro()))
			if !bytes.Equal(entry.Canonical, want) {
				t.Fatal("canonical wire mismatch")
			}
			decoded, err := DecodeSourceRetirementEntry(entry.Canonical)
			if err != nil || decoded.Validate() != nil || !equalSourceEntry(decoded, entry) {
				t.Fatalf("decode: %v", err)
			}
			clear(entry.Canonical)
			r.Origin.LastLive.AllowedGroupIDs[0] = "rag_changed"
			if decoded.Validate() != nil || !bytes.Equal(decoded.Canonical, want) {
				t.Fatal("decoded result retained caller-owned bytes or scope")
			}
		})
	}
}

func TestSourceRetirementRejectsInvalidRequestsAndWire(t *testing.T) {
	base := sourceTestEntry(t)
	for _, tc := range []struct {
		name   string
		change func(*SourceRetirementEntry)
	}{
		{"zero app oid", func(v *SourceRetirementEntry) { v.Request.Origin.Application.DatabaseOID = 0 }},
		{"app epoch overflow", func(v *SourceRetirementEntry) { v.Request.Origin.Application.Epoch = math.MaxUint64 }},
		{"wrong deployment", func(v *SourceRetirementEntry) { v.Request.Origin.Source.DeploymentID = "dp-INVALID" }},
		{"wrong kind ID", func(v *SourceRetirementEntry) { v.Request.Origin.Source.ID = "mi_0123456789abcdef" }},
		{"actor", func(v *SourceRetirementEntry) { v.Request.ActorUserID = "admin" }},
		{"operation", func(v *SourceRetirementEntry) { v.Request.OperationID = "bad operation" }},
		{"zero revision", func(v *SourceRetirementEntry) { v.Request.Origin.Revision = [32]byte{} }},
		{"zero token", func(v *SourceRetirementEntry) { v.Request.TokenCommitment = [32]byte{} }},
		{"zero fingerprint", func(v *SourceRetirementEntry) { v.Request.RequestFingerprint = [32]byte{} }},
		{"non UTC", func(v *SourceRetirementEntry) {
			v.Request.Origin.TombstoneRecordedAt = v.Request.Origin.TombstoneRecordedAt.In(time.FixedZone("other", 3600))
		}},
		{"submicrosecond", func(v *SourceRetirementEntry) {
			v.Request.Origin.TombstoneRecordedAt = v.Request.Origin.TombstoneRecordedAt.Add(time.Nanosecond)
		}},
		{"scope stale hash", func(v *SourceRetirementEntry) { v.Request.FinalFloor.CanonicalHash[0] ^= 1 }},
		{"scope duplicate", func(v *SourceRetirementEntry) {
			v.Request.Origin.LastLive.AllowedGroupIDs = append(v.Request.Origin.LastLive.AllowedGroupIDs, "rag_beta")
		}},
		{"scope widening", func(v *SourceRetirementEntry) { v.Request.FinalFloor = sourceTestScope(t, nil, []string{"rag_other"}) }},
		{"sequence overflow", func(v *SourceRetirementEntry) { v.Sequence = math.MaxUint64 }},
		{"chain previous", func(v *SourceRetirementEntry) { v.PreviousHash[0] = 1 }},
		{"confirmation submicrosecond", func(v *SourceRetirementEntry) { v.ConfirmedAt = v.ConfirmedAt.Add(time.Nanosecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := cloneSourceEntry(base)
			tc.change(&value)
			if _, err := EncodeSourceRetirementEntry(value); err == nil {
				t.Fatal("invalid projection encoded")
			}
		})
	}
	widening := cloneSourceRequest(base.Request)
	widening.FinalFloor = sourceTestScope(t, nil, []string{"rag_other"})
	if _, err := SourceRequestFingerprint(widening); err == nil {
		t.Fatal("widened floor could be fingerprinted")
	}
	if _, err := SourceTokenCommitment(base.Request.Origin.Source, [32]byte{}); err != nil {
		t.Fatal("valid 256-bit token rejected")
	}
	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"truncated", func(b []byte) []byte { return b[:len(b)-1] }},
		{"trailing", func(b []byte) []byte { return append(b, 0) }},
		{"oversize field", func(b []byte) []byte { binary.BigEndian.PutUint32(b[:4], math.MaxUint32); return b }},
		{"route", func(b []byte) []byte { i := bytes.Index(b, []byte(sourceRoute)); b[i] ^= 1; return b }},
		{"floor bytes", func(b []byte) []byte {
			i := bytes.Index(b, base.Request.FinalFloor.CanonicalBytes())
			b[i+4] ^= 1
			return b
		}},
		{"contract", func(b []byte) []byte { binary.BigEndian.PutUint64(b[len(b)-24:len(b)-16], 3); return b }},
		{"release", func(b []byte) []byte { binary.BigEndian.PutUint64(b[len(b)-16:len(b)-8], 1); return b }},
		{"time overflow", func(b []byte) []byte { binary.BigEndian.PutUint64(b[len(b)-8:], math.MaxUint64); return b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeSourceRetirementEntry(tc.change(bytes.Clone(base.Canonical))); err == nil {
				t.Fatal("malformed canonical accepted")
			}
		})
	}
	malformedScope := sourceTestWire("recordauth.visibility.v1")
	malformedScope = append(malformedScope, byte(1))
	malformedScope = append(malformedScope, sourceTestWire("restricted", "default")...)
	malformedScope = binary.BigEndian.AppendUint32(malformedScope, math.MaxUint32)
	if _, err := parseSourceScope(malformedScope); err == nil {
		t.Fatal("unbounded nested scope count accepted")
	}
}

func TestSourceLedgerRejectsEveryProjectedFieldDrift(t *testing.T) {
	entry := sourceTestEntry(t)
	row, err := sourceStoredRow(entry)
	if err != nil {
		t.Fatal(err)
	}
	client := client{ledgerDomain: entry.LedgerDomain}
	if _, err := client.sourceRow(row); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*storedRow)
	}{
		{"sequence", func(v *storedRow) { v.sequence++ }}, {"version", func(v *storedRow) { v.version++ }},
		{"type", func(v *storedRow) { v.entryType = "attempt_not_committed" }}, {"deployment", func(v *storedRow) { v.deployment += "a" }},
		{"project", func(v *storedRow) { v.project += "a" }}, {"operation", func(v *storedRow) { v.operation += "a" }},
		{"actor", func(v *storedRow) { v.actor += "a" }}, {"route", func(v *storedRow) { v.route = "record_permanent_delete" }},
		{"kind", func(v *storedRow) { v.kind = "target" }}, {"object", func(v *storedRow) { v.object += "a" }},
		{"origin", func(v *storedRow) { v.origin = append(bytes.Clone(v.origin), 0) }}, {"floor", func(v *storedRow) { v.floor = append(bytes.Clone(v.floor), 0) }},
		{"floor hash", func(v *storedRow) { v.floorHash = bytes.Repeat([]byte{1}, 32) }}, {"token", func(v *storedRow) { v.token = bytes.Repeat([]byte{1}, 32) }},
		{"fingerprint", func(v *storedRow) { v.fingerprint = bytes.Repeat([]byte{1}, 32) }}, {"contract", func(v *storedRow) { x := int64(3); v.contract = &x }},
		{"reason", func(v *storedRow) { v.reason = "source_removed" }}, {"release", func(v *storedRow) { x := int64(1); v.release = &x }},
		{"confirmed", func(v *storedRow) { v.confirmed = v.confirmed.Add(time.Microsecond) }}, {"previous", func(v *storedRow) { v.previous = bytes.Repeat([]byte{1}, 32) }},
		{"canonical", func(v *storedRow) { v.canonical = append(bytes.Clone(v.canonical), 0) }}, {"hash", func(v *storedRow) { v.hash = bytes.Repeat([]byte{1}, 32) }},
	}
	if len(cases) != 22 {
		t.Fatal("projection coverage incomplete")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := row
			tc.change(&changed)
			if _, err := client.sourceRow(changed); err == nil {
				t.Fatal("drift accepted")
			}
			if equalStoredRow(row, changed) {
				t.Fatal("all-field comparator accepted drift")
			}
		})
	}
	unknown := row
	unknown.route = sourceRoute
	unknown.canonical = []byte("opaque predecessor")
	if _, err := client.projectRow(unknown); err != nil {
		t.Fatalf("opaque source chain rejected: %v", err)
	}
	prefix := sourceTestWire("HOUFENG-SOURCE-RETIREMENT-LEDGER-V1")
	bad := row
	bad.canonical = append(bytes.Clone(prefix), 0)
	if _, err := client.projectRow(bad); err == nil {
		t.Fatal("malformed claimed source codec treated as opaque")
	}
	bad.canonical = bytes.Clone(prefix[:8])
	if _, err := client.projectRow(bad); err == nil {
		t.Fatal("truncated claimed source codec treated as opaque")
	}
}

type sourceTestFence struct {
	phase                uint64
	request              SourceRetirementRequest
	trust                recorddeletion.LedgerVerificationTrust
	projections, entries int
	closed, failAfter    bool
}

func (f *sourceTestFence) SourceRetirementPhase(ctx context.Context) (uint64, error) {
	if f.closed {
		return 0, errors.New("closed")
	}
	return f.phase, ctx.Err()
}
func (f *sourceTestFence) WithSourceRetirementFence(ctx context.Context, phase uint64, apply func(context.Context) error) error {
	if f.closed || phase == 0 || phase != f.phase {
		return errors.New("stale source phase")
	}
	f.entries++
	if err := apply(ctx); err != nil {
		return err
	}
	if f.closed || phase != f.phase || f.failAfter {
		return errors.New("unknown after effect")
	}
	return ctx.Err()
}
func (f *sourceTestFence) VerifySourceRetirementProjection(ctx context.Context, request SourceRetirementRequest) error {
	f.projections++
	if err := ctx.Err(); err != nil {
		return err
	}
	if !equalSourceRequest(request, f.request) {
		return errors.New("projection drift")
	}
	return nil
}
func (f *sourceTestFence) Trust() (recorddeletion.LedgerVerificationTrust, error) {
	return f.trust, nil
}
func (f *sourceTestFence) VerifyLedgerContinuity(ctx context.Context, _ recorddeletion.LedgerContinuityObservation) error {
	return ctx.Err()
}

func TestSourceCommitCapabilityRechecksPhaseAndProjection(t *testing.T) {
	request := sourceTestRequest(t, recordauth.SourceKindVPS, "vps_0123456789abcdef", "sro_fenced")
	fence := &sourceTestFence{phase: 1, request: cloneSourceRequest(request)}
	verifier, err := NewSourceRetirementCommitVerifier(fence)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := verifier.Verify(context.Background(), request)
	if err != nil || !commit.Valid() {
		t.Fatalf("verify = %v", err)
	}
	request.Origin.LastLive.AllowedGroupIDs[0] = "rag_mutated"
	copyRequest := commit.Request()
	copyRequest.FinalFloor.AllowedGroupIDs[0] = "rag_mutated"
	if !equalSourceRequest(commit.Request(), fence.request) {
		t.Fatal("commit did not defensively copy request")
	}
	applied := 0
	if err := commit.withProjection(context.Background(), func(context.Context) error { applied++; return nil }); err != nil || applied != 1 || fence.projections != 2 {
		t.Fatalf("effect recheck = %d, %d, %v", applied, fence.projections, err)
	}
	fence.request.Origin.Revision[0] ^= 1
	if err := commit.withProjection(context.Background(), func(context.Context) error { applied++; return nil }); err == nil || applied != 1 {
		t.Fatal("projection drift allowed effect")
	}
	fence.request = commit.Request()
	fence.phase++
	if err := commit.withProjection(context.Background(), func(context.Context) error { applied++; return nil }); err == nil || applied != 1 {
		t.Fatal("stale phase allowed effect")
	}
	if (SourceRetirementCommit{}).Valid() || (VerifiedSourceRetirement{}).Valid() {
		t.Fatal("zero private capability valid")
	}
	if _, err := NewSourceRetirementCommitVerifier(nil); err == nil {
		t.Fatal("nil fence accepted")
	}
}
