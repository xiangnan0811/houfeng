//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"reflect"
	"testing"
)

// These are the native STATUS wire offsets, not a serialization of guardStatus.
func statusWireFixture(state, phase uint32, sealed bool) []byte {
	b := make([]byte, 240)
	binary.LittleEndian.PutUint32(b[0:4], 5)
	binary.LittleEndian.PutUint32(b[24:28], state)
	binary.LittleEndian.PutUint32(b[32:36], phase)
	binary.LittleEndian.PutUint64(b[40:48], 19)
	binary.LittleEndian.PutUint64(b[48:56], 15)
	binary.LittleEndian.PutUint64(b[56:64], 14)
	binary.LittleEndian.PutUint64(b[80:88], 31)
	binary.LittleEndian.PutUint64(b[88:96], 32)
	binary.LittleEndian.PutUint64(b[104:112], 41)
	binary.LittleEndian.PutUint64(b[112:120], 42)
	binary.LittleEndian.PutUint64(b[120:128], 43)
	binary.LittleEndian.PutUint64(b[128:136], 44)
	b[208] = 0x44
	if state == 2 {
		binary.LittleEndian.PutUint64(b[96:104], 18)
	}
	if sealed {
		b[8] = 0x11
		b[176] = 0x22
		for i := range 3 {
			binary.LittleEndian.PutUint32(b[136+i*4:], uint32(51+i))
			binary.LittleEndian.PutUint32(b[148+i*4:], uint32(61+i))
			binary.LittleEndian.PutUint32(b[160+i*4:], 1)
		}
	}
	return b
}

func TestGuardStatusWireLifecycle(t *testing.T) {
	base := guardStatus{
		Sequence: 19, AckCursor: 15, ObserverAckCursor: 14,
		BrokerCookie: 31, ObserverCookie: 32,
		ModuleInstance: 41, ModuleDev: 42, ModuleIno: 43, ModuleSize: 44,
		ObserverBindingsSHA256: [32]byte{0x44},
	}
	sealed := base
	sealed.Epoch = [16]byte{0x11}
	sealed.BootstrapDigest = [32]byte{0x22}
	sealed.LinkIDs = [3]uint32{51, 52, 53}
	sealed.ProgramIDs = [3]uint32{61, 62, 63}
	sealed.LinkAttached = [3]uint32{1, 1, 1}

	unsealed := base
	unsealed.State, unsealed.AuthorityPhase, unsealed.ObserverCookie = 0, 1, 0
	unsealedWire := statusWireFixture(0, 1, false)
	binary.LittleEndian.PutUint64(unsealedWire[88:96], 0)

	presealDead := base
	presealDead.State, presealDead.AuthorityPhase, presealDead.DeathSequence = 2, 0, 18

	live := sealed
	live.State, live.AuthorityPhase = 1, 1
	revoked := sealed
	revoked.State, revoked.AuthorityPhase = 1, 2

	sealedDead := sealed
	sealedDead.State, sealedDead.AuthorityPhase, sealedDead.DeathSequence = 2, 0, 18
	sealedDead.LinkAttached = [3]uint32{0, 1, 0}
	sealedDead.Sticky, sealedDead.Lost, sealedDead.ProbeMissed = 1, 2, 3
	sealedDeadWire := statusWireFixture(2, 0, true)
	binary.LittleEndian.PutUint32(sealedDeadWire[28:32], 1)
	binary.LittleEndian.PutUint64(sealedDeadWire[64:72], 2)
	binary.LittleEndian.PutUint64(sealedDeadWire[72:80], 3)
	binary.LittleEndian.PutUint32(sealedDeadWire[160:164], 0)
	binary.LittleEndian.PutUint32(sealedDeadWire[168:172], 0)

	// Release writes DEAD before acquiring the ring lock to lower the phase.
	presealDeadInFlight := presealDead
	presealDeadInFlight.AuthorityPhase = 1

	for _, tc := range []struct {
		name string
		wire []byte
		want guardStatus
	}{
		{"unsealed before observer binding", unsealedWire, unsealed},
		{"dead before seal", statusWireFixture(2, 0, false), presealDead},
		{"dead before release completes", statusWireFixture(2, 1, false), presealDeadInFlight},
		{"live sealed", statusWireFixture(1, 1, true), live},
		{"live revoked", statusWireFixture(1, 2, true), revoked},
		{"dead after seal with loss", sealedDeadWire, sealedDead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeGuardStatus(tc.wire)
			if err != nil || got != tc.want {
				t.Fatalf("decode STATUS = %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}

	got, err := decodeGuardStatus(statusWireFixture(2, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	projected := projectGuardStatus(got)
	if projected["epoch"] != nil || projected["bootstrap_digest"] != nil ||
		!reflect.DeepEqual(projected["link_ids"], []any{nil, nil, nil}) ||
		!reflect.DeepEqual(projected["program_ids"], []any{nil, nil, nil}) ||
		!reflect.DeepEqual(projected["link_attached"], []any{false, false, false}) {
		t.Fatalf("preseal death fabricated identity in projection: %#v", projected)
	}
}

func TestGuardStatusWireRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		wire []byte
	}{}
	add := func(name string, wire []byte, change func([]byte)) {
		change(wire)
		tests = append(tests, struct {
			name string
			wire []byte
		}{name, wire})
	}
	deadSealed := func() []byte { return statusWireFixture(2, 0, true) }
	deadPreseal := func() []byte { return statusWireFixture(2, 0, false) }
	liveSealed := func() []byte { return statusWireFixture(1, 1, true) }

	add("partial preseal epoch", deadPreseal(), func(b []byte) { b[8] = 1 })
	add("partial preseal digest", deadPreseal(), func(b []byte) { b[176] = 1 })
	add("missing sealed epoch", deadSealed(), func(b []byte) { b[8] = 0 })
	add("missing sealed digest", deadSealed(), func(b []byte) { b[176] = 0 })
	for i, name := range []string{"first", "second", "third"} {
		offLink, offProgram, offAttached := 136+i*4, 148+i*4, 160+i*4
		add("partial preseal "+name+" link", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint32(b[offLink:], 1) })
		add("partial preseal "+name+" program", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint32(b[offProgram:], 1) })
		add("missing sealed "+name+" link", deadSealed(), func(b []byte) { binary.LittleEndian.PutUint32(b[offLink:], 0) })
		add("missing sealed "+name+" program", deadSealed(), func(b []byte) { binary.LittleEndian.PutUint32(b[offProgram:], 0) })
		add("attached without seal "+name, deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint32(b[offAttached:], 1) })
		add("unattached live "+name, liveSealed(), func(b []byte) { binary.LittleEndian.PutUint32(b[offAttached:], 0) })
	}
	add("unsealed with sealed identity", statusWireFixture(0, 1, true), func([]byte) {})
	add("sealed without identity", statusWireFixture(1, 1, false), func([]byte) {})
	add("dead without broker cookie", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint64(b[80:88], 0) })
	add("dead without observer cookie", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint64(b[88:96], 0) })
	add("invalid state", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint32(b[24:28], 3) })
	add("invalid phase", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint32(b[32:36], 3) })
	add("invalid attached flag", deadSealed(), func(b []byte) { binary.LittleEndian.PutUint32(b[160:164], 2) })
	add("broker cursor past sequence", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint64(b[48:56], 20) })
	add("observer cursor past sequence", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint64(b[56:64], 20) })
	add("missing module instance", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint64(b[104:112], 0) })
	add("missing module inode", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint64(b[120:128], 0) })
	add("missing module size", deadPreseal(), func(b []byte) { binary.LittleEndian.PutUint64(b[128:136], 0) })
	add("missing observer bindings digest", deadPreseal(), func(b []byte) { b[208] = 0 })
	add("reserved status field", deadPreseal(), func(b []byte) { b[36] = 1 })
	add("reserved status tail", deadPreseal(), func(b []byte) { b[172] = 1 })
	add("wrong version", deadPreseal(), func(b []byte) { b[0] = 2 })
	add("reserved header field", deadPreseal(), func(b []byte) { b[4] = 1 })
	tests = append(tests, struct {
		name string
		wire []byte
	}{"truncated response", deadPreseal()[:239]})
	tests = append(tests, struct {
		name string
		wire []byte
	}{"oversized response", append(deadPreseal(), 0)})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeGuardStatus(tc.wire)
			if err != errGuardProtocol || got != (guardStatus{}) {
				t.Fatalf("decode STATUS = %#v, %v; want empty status and protocol error", got, err)
			}
		})
	}
}
