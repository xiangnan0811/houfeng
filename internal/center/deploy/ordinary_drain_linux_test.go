//go:build linux

package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOrdinaryDrainEvidenceIsPrivateAndContainsBackendIdentity(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	fd, err := unix.Open(work, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	want := ordinaryPendingBackend{PID: 123, BackendStart: time.Unix(123456, 0).UTC(), UserName: "postgres", Application: "fixture", ClientAddress: "127.0.1.1", State: "idle", WaitEventType: "Client"}
	if err := ordinaryPublishDrainEvidence(fd, []ordinaryPendingBackend{want}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(work)
	if err != nil || len(entries) != 1 || !ordinaryDrainEvidenceName.MatchString(entries[0].Name()) {
		t.Fatal("missing uniquely named drain evidence")
	}
	path := filepath.Join(work, entries[0].Name())
	var stat unix.Stat_t
	if unix.Lstat(path, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 {
		t.Fatal("drain evidence is not root-private")
	}
	body, err := os.ReadFile(path)
	var evidence ordinaryDrainEvidence
	if err != nil || json.Unmarshal(body, &evidence) != nil || evidence.Version != 1 || len(evidence.Backends) != 1 || evidence.Backends[0] != want {
		t.Fatal("drain evidence did not retain the backend identity")
	}
}
