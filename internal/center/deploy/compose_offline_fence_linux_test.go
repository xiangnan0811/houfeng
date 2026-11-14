//go:build linux

package deploy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestComposeOfflineFenceRejectsNilCallback(t *testing.T) {
	if err := FenceComposeSource(context.Background(), ComposeOfflineFenceRequest{}, nil); err == nil {
		t.Fatal("accepted nil callback")
	}
}

func TestComposeOfflineFenceJournalExactReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := fenceJournalDir(filepath.Join(root, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dir)
	r := ComposeOfflineFenceRequest{RecoveryID: "rid_1", Project: "project", WorkingDirectory: "/work", DatabaseContainerID: strings.Repeat("a", 64), ControlConfigPath: filepath.Join(root, "source.json")}
	j := fenceJournal{Version: 1, Request: r, Containers: []fenceContainer{{ID: r.DatabaseContainerID, Service: "db"}}}
	if err = fencePublish(dir, r.RecoveryID+".json", j); err != nil {
		t.Fatal(err)
	}
	got, ok, err := fenceReadJournal(dir, r)
	if err != nil || !ok || got.Request != r {
		t.Fatalf("exact replay: %v %v", ok, err)
	}
	changed := r
	changed.WorkingDirectory = "/other"
	if _, _, err = fenceReadJournal(dir, changed); err == nil {
		t.Fatal("accepted different request")
	}
	changed = r
	changed.RecoveryID = "other"
	if _, _, err = fenceReadJournal(dir, changed); err == nil {
		t.Fatal("accepted different recovery ID")
	}
	if err = fencePublish(dir, r.RecoveryID+".json", j); err == nil {
		t.Fatal("replaced immutable journal")
	}
}

func TestComposeOfflineFenceJournalLinkCutpoint(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := fenceJournalDir(filepath.Join(root, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dir)
	r := ComposeOfflineFenceRequest{RecoveryID: "recovery", Project: "project", DatabaseContainerID: strings.Repeat("a", 64)}
	data, err := json.Marshal(fenceJournal{Version: 1, Request: r})
	if err != nil {
		t.Fatal(err)
	}
	tmp := ".tmp-" + strings.Repeat("a", 32)
	if err = os.WriteFile(filepath.Join(root, "fences", tmp), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = unix.Linkat(dir, tmp, dir, "recovery.json", 0); err != nil {
		t.Fatal(err)
	}
	_, ok, err := fenceReadJournal(dir, r)
	if err != nil || !ok {
		t.Fatalf("resume link cutpoint: %v %v", ok, err)
	}
	if _, err = os.Lstat(filepath.Join(root, "fences", tmp)); !os.IsNotExist(err) {
		t.Fatal("temporary link not settled")
	}
}

func TestComposeOfflineFenceRejectsJournalSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := fenceJournalDir(filepath.Join(root, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dir)
	if err = os.Symlink("/etc/passwd", filepath.Join(root, "fences", "rid.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = fenceReadJournal(dir, ComposeOfflineFenceRequest{RecoveryID: "rid"}); err == nil {
		t.Fatal("accepted journal symlink")
	}
}

func TestComposeOfflineFenceNetworkAnchors(t *testing.T) {
	a := fenceContainer{Networks: []fenceNetworkAnchor{{ID: "original", Aliases: []string{"db"}}}}
	original := fenceObservation{anchor: a}
	if !fenceAllowedNetworks(a, original, "maintenance", false) {
		t.Fatal("original network rejected")
	}
	changed := fenceObservation{anchor: fenceContainer{Networks: []fenceNetworkAnchor{{ID: "maintenance"}}}}
	if !fenceAllowedNetworks(a, changed, "maintenance", true) {
		t.Fatal("database isolation rejected")
	}
	if fenceAllowedNetworks(a, changed, "maintenance", false) {
		t.Fatal("accepted changed service network")
	}
	changed.anchor.Networks[0].ID = "foreign"
	if fenceAllowedNetworks(a, changed, "maintenance", true) {
		t.Fatal("accepted foreign database network")
	}
}

func TestComposeOfflineFenceCanonicalJournalAndBounds(t *testing.T) {
	var receipt fenceNetworkReceipt
	if fenceDecode([]byte(`{"version":1,"recovery_id":"r","network_id":"x","extra":0}`), &receipt) == nil {
		t.Fatal("accepted unknown field")
	}
	if fenceDecode([]byte(`{"version":1,"version":1,"recovery_id":"r","network_id":"x"}`), &receipt) == nil {
		t.Fatal("accepted duplicate field")
	}
	var output fenceLimitedBuffer
	p := make([]byte, fenceOutputLimit+1)
	if n, err := output.Write(p); err != nil || n != len(p) || !output.overflow || output.Len() != fenceOutputLimit {
		t.Fatal("output not bounded")
	}
}

func TestComposeOfflineFenceRequestRecoveryTree(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	control := filepath.Join(root, "control")
	if err := os.MkdirAll(filepath.Join(work, "data", "control"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(control, 0700); err != nil {
		t.Fatal(err)
	}
	r := ComposeOfflineFenceRequest{RecoveryID: "valid_1", Project: "project", WorkingDirectory: work, DatabaseContainerID: strings.Repeat("a", 64), ControlConfigPath: filepath.Join(control, "source.json")}
	if !fenceRequestValid(r) {
		t.Fatal("valid request rejected")
	}
	r.ControlConfigPath = filepath.Join(work, "data", "control", "source.json")
	if fenceRequestValid(r) {
		t.Fatal("control inside recovery tree accepted")
	}
	r.ControlConfigPath = filepath.Join(control, "source.json")
	r.RecoveryID = "../bad"
	if fenceRequestValid(r) {
		t.Fatal("invalid recovery ID accepted")
	}
}
