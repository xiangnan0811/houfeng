package deletionledger

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestSourceRetirementRepresentationBounds(t *testing.T) {
	entry := sourceTestEntry(t)
	entry.Request.OperationID = strings.Repeat("a", 128)
	entry.Request.Origin.Application.SystemIdentifier = "18446744073709551615"
	entry.Request.Origin.Application.DatabaseOID = math.MaxUint32
	entry.Request.Origin.Application.Epoch = math.MaxInt64
	var err error
	entry.Request.RequestFingerprint, err = SourceRequestFingerprint(entry.Request)
	if err != nil {
		t.Fatalf("boundary fingerprint: %v", err)
	}
	entry.Sequence = math.MaxInt64
	entry.PreviousHash[0] = 1
	wire, err := EncodeSourceRetirementEntry(entry)
	if err != nil {
		t.Fatalf("valid SQL boundaries: %v", err)
	}
	decoded, err := DecodeSourceRetirementEntry(wire)
	if err != nil || decoded.Sequence != math.MaxInt64 || decoded.Request.Origin.Application.DatabaseOID != math.MaxUint32 {
		t.Fatalf("boundary round trip: %v", err)
	}
	epoch := cloneSourceEntry(entry)
	epoch.Request.Origin.TombstoneRecordedAt = time.UnixMicro(0).UTC()
	epoch.ConfirmedAt = time.UnixMicro(0).UTC()
	epoch.Request.RequestFingerprint, err = SourceRequestFingerprint(epoch.Request)
	if err != nil {
		t.Fatal(err)
	}
	epochWire, err := EncodeSourceRetirementEntry(epoch)
	if err != nil {
		t.Fatalf("epoch timestamp: %v", err)
	}
	if _, err := DecodeSourceRetirementEntry(epochWire); err != nil {
		t.Fatalf("epoch roundtrip: %v", err)
	}
	tooLong := cloneSourceRequest(entry.Request)
	tooLong.OperationID += "a"
	if _, err := SourceRequestFingerprint(tooLong); err == nil {
		t.Fatal("oversized operation accepted")
	}
	origin, err := EncodeSourceOrigin(entry.Request.Origin)
	if err != nil {
		t.Fatal(err)
	}
	oidPosition := bytes.Index(origin, []byte(entry.Request.Origin.Application.SystemIdentifier)) + len(entry.Request.Origin.Application.SystemIdentifier)
	binary.BigEndian.PutUint64(origin[oidPosition:oidPosition+8], uint64(math.MaxUint32)+1)
	if _, err := decodeSourceOrigin(origin); err == nil {
		t.Fatal("unrepresentable APP OID accepted")
	}
	if _, err := DecodeSourceRetirementEntry(bytes.Repeat([]byte{1}, sourceEntryLimit+1)); err == nil {
		t.Fatal("oversized entry accepted")
	}

	groups := make([]string, 70)
	for i := range groups {
		groups[i] = fmt.Sprintf("rag_%064d", i)
	}
	oversized := sourceTestScope(t, nil, groups)
	if len(oversized.CanonicalBytes()) <= sourceScopeLimit {
		t.Fatal("fixture did not exceed source scope limit")
	}
	entry.Request.Origin.LastLive = oversized
	if _, err := EncodeSourceOrigin(entry.Request.Origin); err == nil {
		t.Fatal("oversized origin scope accepted")
	}
	entry.Request.Origin.LastLive = sourceTestScope(t, []recordauth.Role{recordauth.RoleViewer}, []string{"rag_alpha", "rag_beta"})
	entry.Request.FinalFloor = oversized
	if _, err := SourceRequestFingerprint(entry.Request); err == nil {
		t.Fatal("oversized floor accepted")
	}
}
