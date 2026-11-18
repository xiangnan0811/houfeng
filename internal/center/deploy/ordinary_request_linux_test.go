//go:build linux

package deploy

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestOrdinaryUpgradeRequestCanonicalBinding(t *testing.T) {
	request := ordinaryUpgradeRequest{
		Version: 1, OperationID: "upgrade_1", Project: "houfeng", WorkingDirectory: "/srv/houfeng",
		DatabaseContainerID: strings.Repeat("a", 64), TargetImageID: "sha256:" + strings.Repeat("b", 64),
		Secrets: ordinaryUpgradeSecrets{Bootstrap: "/run/private/bootstrap", Runtime: "/run/private/runtime", PlatformAdmin: "/run/private/admin", DeletionWorker: "/run/private/worker", Migrator: "/run/private/migrator", InitialPassword: "/run/private/initial", SessionHMACKey: "/run/private/session"},
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	if got, err := decodeOrdinaryUpgradeRequest(body); err != nil || got != request {
		t.Fatalf("canonical request rejected: %+v %v", got, err)
	}
	cases := [][]byte{
		bytes.Replace(body, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(body, []byte(`"version":1`), []byte(`"VERSION":1`), 1),
		bytes.TrimSuffix(body, []byte("\n")),
		bytes.Replace(body, []byte(`sha256:`), []byte(`latest:`), 1),
		bytes.Replace(body, []byte(`/srv/houfeng`), []byte(`/srv/../houfeng`), 1),
		bytes.Replace(body, []byte(`/run/private/worker`), []byte(`/run/private/admin`), 1),
	}
	for _, input := range cases {
		if _, err := decodeOrdinaryUpgradeRequest(input); err == nil {
			t.Fatalf("invalid request accepted: %s", input)
		}
	}
}

func ordinaryELFFixture(programType uint32, machine uint16) []byte {
	// Minimal ELF64 LE executable with one executable PT_LOAD (or a rejected
	// interpreter/dynamic header). It is only parsed, never executed.
	body := make([]byte, 120)
	copy(body, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(body[16:], 2)
	binary.LittleEndian.PutUint16(body[18:], machine)
	binary.LittleEndian.PutUint32(body[20:], 1)
	binary.LittleEndian.PutUint64(body[24:], 0x400040)
	binary.LittleEndian.PutUint64(body[32:], 64)
	binary.LittleEndian.PutUint16(body[52:], 64)
	binary.LittleEndian.PutUint16(body[54:], 56)
	binary.LittleEndian.PutUint16(body[56:], 1)
	binary.LittleEndian.PutUint32(body[64:], programType)
	binary.LittleEndian.PutUint32(body[68:], 5)
	binary.LittleEndian.PutUint64(body[80:], 0x400000)
	binary.LittleEndian.PutUint64(body[96:], uint64(len(body)))
	binary.LittleEndian.PutUint64(body[104:], uint64(len(body)))
	binary.LittleEndian.PutUint64(body[112:], 4096)
	return body
}

func TestOrdinaryStaticELFRejectsForeignAndDynamic(t *testing.T) {
	static := ordinaryELFFixture(1, 62)
	if err := ordinaryStaticELF(bytes.NewReader(static), int64(len(static)), "amd64"); err != nil {
		t.Fatalf("static ELF rejected: %v", err)
	}
	for _, body := range [][]byte{ordinaryELFFixture(3, 62), ordinaryELFFixture(2, 62), ordinaryELFFixture(1, 183), static[:80]} {
		if err := ordinaryStaticELF(bytes.NewReader(body), int64(len(body)), "amd64"); err == nil {
			t.Fatal("invalid, dynamic, or foreign ELF accepted")
		}
	}
	if err := ordinaryStaticELF(bytes.NewReader(static), int64(len(static)), "unknown"); err == nil {
		t.Fatal("unsupported host architecture accepted")
	}
}
