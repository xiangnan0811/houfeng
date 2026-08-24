package recordauthority

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestMarshalMembershipHeartbeatCommandV1IsExactClosedAndCredentialFree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "records-authority")
	random := append(bytes.Repeat([]byte{0x11}, 32), bytes.Repeat([]byte{0x22}, 32)...)
	random = append(random, bytes.Repeat([]byte{0x33}, 32)...)
	state, err := createComposeState(root, bytes.NewReader(random))
	if err != nil {
		t.Fatalf("createComposeState(): %v", err)
	}
	issuedAt := time.Unix(1_724_500_000, 987_654_321).UTC()

	command, expiresAt, err := MarshalMembershipHeartbeatCommandV1(state, issuedAt)
	if err != nil {
		t.Fatalf("MarshalMembershipHeartbeatCommandV1() error = %v", err)
	}
	if len(command) != 177 {
		t.Fatalf("heartbeat command length = %d, want 177", len(command))
	}
	if got := string(command[:33]); got != "HOUFENG-APP-PROJECTION-COMMAND-V1" {
		t.Fatalf("heartbeat command magic = %q", got)
	}
	if !bytes.Equal(command[33:37], []byte{0, 1, 3, 10}) {
		t.Fatalf("heartbeat command header = %x, want version 1 operation 3 field count 10", command[33:37])
	}
	assertHeartbeatField := func(offset int, want string) {
		t.Helper()
		if got := string(command[offset : offset+len(want)]); got != want {
			t.Fatalf("heartbeat command field at %d = %q, want %q", offset, got, want)
		}
	}
	assertHeartbeatField(37, "dp-"+string(bytes.Repeat([]byte("11"), 32)))
	assertHeartbeatField(104, "default")
	assertHeartbeatField(111, "compose-center")
	assertHeartbeatField(125, "api")
	assertHeartbeatField(128, "records.runtime")
	if got := binary.BigEndian.Uint64(command[143:151]); got != 1 {
		t.Fatalf("deployment epoch = %d, want 1", got)
	}
	if got := binary.BigEndian.Uint64(command[151:159]); got != 1 {
		t.Fatalf("fence contract version = %d, want 1", got)
	}
	if !bytes.Equal(command[159:161], []byte{1, 0}) {
		t.Fatalf("admission flags = %x, want load-balancer only", command[159:161])
	}
	if got := binary.BigEndian.Uint64(command[161:169]); got != uint64(issuedAt.Unix()) {
		t.Fatalf("issued-at epoch = %d, want %d", got, issuedAt.Unix())
	}
	if got := binary.BigEndian.Uint64(command[169:177]); got != uint64(issuedAt.Unix()+90) {
		t.Fatalf("expires-at epoch = %d, want %d", got, issuedAt.Unix()+90)
	}
	if !expiresAt.Equal(time.Unix(issuedAt.Unix()+90, 0).UTC()) {
		t.Fatalf("expiresAt = %s, want exact 90-second lease", expiresAt)
	}
	if bytes.Contains(command, []byte(state.DatabasePassword())) {
		t.Fatal("heartbeat command contains the authority database credential")
	}
}

func TestMarshalMembershipHeartbeatCommandV1RejectsStateDriftAndUnrepresentableTime(t *testing.T) {
	root := filepath.Join(t.TempDir(), "records-authority")
	random := append(bytes.Repeat([]byte{0x11}, 32), bytes.Repeat([]byte{0x22}, 32)...)
	random = append(random, bytes.Repeat([]byte{0x33}, 32)...)
	state, err := createComposeState(root, bytes.NewReader(random))
	if err != nil {
		t.Fatalf("createComposeState(): %v", err)
	}

	drifted := state
	drifted.Memberships = append([]ComposeMembership(nil), state.Memberships...)
	drifted.Memberships[0].QueueAdmitted = true
	if _, _, err := MarshalMembershipHeartbeatCommandV1(drifted, time.Unix(1_724_500_000, 0)); !errors.Is(err, ErrComposeHeartbeatInvalid) {
		t.Fatalf("drifted state error = %v, want ErrComposeHeartbeatInvalid", err)
	}
	if _, _, err := MarshalMembershipHeartbeatCommandV1(state, time.Unix(math.MaxInt64, 0)); !errors.Is(err, ErrComposeHeartbeatInvalid) {
		t.Fatalf("unrepresentable time error = %v, want ErrComposeHeartbeatInvalid", err)
	}
}
