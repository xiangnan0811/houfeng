//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func ledgerTestControl(t *testing.T) (int, string) {
	t.Helper()
	root := t.TempDir()
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd, root
}

func ledgerTestRequest(t *testing.T) recorddeletion.LedgerAppendRequest {
	t.Helper()
	fp, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var token [32]byte
	token[0] = 2
	return recorddeletion.LedgerAppendRequest{EntryType: recorddeletion.LedgerEntryDeleteCommit,
		DeploymentID: recordplatform.DeploymentID("dp-" + strings.Repeat("a", 64)), ProjectID: recordplatform.ProjectIDDefault,
		OperationID: "rpo_test", ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Object:          recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
		TokenCommitment: token, RequestFingerprint: fp, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, DeletionContractVersion: 1}
}

func TestRecordLedgerControlAnchorAndExactReplay(t *testing.T) {
	dir, root := ledgerTestControl(t)
	zero, err := ledgerAppendAnchor(dir, nil, recorddeletion.LedgerHead{}, recorddeletion.LedgerHead{})
	if err != nil {
		t.Fatal(err)
	}
	var hash [32]byte
	hash[0] = 4
	one := recorddeletion.LedgerHead{Sequence: 1, Hash: hash}
	if _, err := ledgerAppendAnchor(dir, []ledgerAnchor{zero}, one, one); err != nil {
		t.Fatal(err)
	}
	anchors, err := ledgerReadAnchors(dir)
	if err != nil || len(anchors) != 2 || anchors[1].Primary != one || anchors[1].Witness != one {
		t.Fatalf("read anchors: %#v, %v", anchors, err)
	}
	if _, err := ledgerAppendAnchor(dir, []ledgerAnchor{zero}, recorddeletion.LedgerHead{}, recorddeletion.LedgerHead{}); err == nil {
		t.Fatal("conflicting anchor replay accepted")
	}
	// A missing intermediate anchor must never be treated as a fresh chain.
	if err := os.Remove(filepath.Join(root, "anchor-00000000000000000000.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerReadAnchors(dir); err == nil {
		t.Fatal("truncated protected history accepted")
	}
}

func TestRecordLedgerResolutionRequiresExactOriginal(t *testing.T) {
	dir, _ := ledgerTestControl(t)
	request := ledgerTestRequest(t)
	domain, err := recorddeletion.LedgerDomainDigest("rd-" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	original, err := ledgerRequestCanonical(request, domain)
	if err != nil {
		t.Fatal(err)
	}
	key := ledgerIntentKey(request)
	intent := ledgerIntent{Version: 1, Key: key, Canonical: original}
	if _, err := ledgerPublishExact(dir, "intent-"+key+".json", intent); err != nil {
		t.Fatal(err)
	}
	outcome := request.AttemptNotCommitted(7)
	encodedOutcome, err := ledgerRequestCanonical(outcome, domain)
	if err != nil {
		t.Fatal(err)
	}
	intentBytes, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	resolution := ledgerResolution{Version: 1, Key: key, IntentDigest: sha256.Sum256(intentBytes), Original: original,
		Outcome: encodedOutcome, ReleaseEpoch: 7, ReservationID: "drs_test", FenceEpoch: 1}
	if _, err := ledgerPublishExact(dir, "resolution-"+key+".json", resolution); err != nil {
		t.Fatal(err)
	}
	entry := recorddeletion.CanonicalLedgerEntry{DeletionLedgerEntry: recorddeletion.DeletionLedgerEntry{Request: outcome, Sequence: 1},
		LedgerDomain: domain, ConfirmedAt: time.UnixMicro(1).UTC()}
	entry.Canonical, err = recorddeletion.EncodeCanonicalLedgerEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.EntryHash = sha256.Sum256(entry.Canonical)
	row := recorddeletion.LedgerChainEntry{Sequence: 1, EntryHash: entry.EntryHash, Canonical: entry.Canonical, Route: recorddeletion.LedgerRecordRoute, Record: &entry}
	snapshot := recorddeletion.LedgerSnapshot{Entries: []recorddeletion.LedgerChainEntry{row}}
	if _, found, err := ledgerFindIntentEntry(dir, snapshot, intent, domain); err != nil || !found {
		t.Fatalf("linked outcome rejected: %v", err)
	}
	conflict := intent
	conflict.Primary.Sequence = 1
	if _, _, err := ledgerFindIntentEntry(dir, snapshot, conflict, domain); err == nil {
		t.Fatal("outcome accepted for a different original intent")
	}
	if high, err := ledgerResolutionHighWater(dir); err != nil || high != 7 {
		t.Fatalf("release epoch = %d, %v", high, err)
	}
}

func TestRecordLedgerRejectsForgedOrInactiveFence(t *testing.T) {
	for _, token := range []*RecordLedgerFence{nil, {}, {ctx: context.Background()}} {
		called := false
		err := token.WithHeldLedgerFence(context.Background(), func(context.Context) error { called = true; return nil })
		if !errors.Is(err, ErrRecordLedgerFence) || called {
			t.Fatalf("forged token called=%v error=%v", called, err)
		}
		if _, _, err := token.RecoveryBinding(); !errors.Is(err, ErrRecordLedgerFence) {
			t.Fatalf("forged recovery binding: %v", err)
		}
	}
	if _, err := ledgerOpenCgroups([]string{"/tmp"}); err == nil {
		t.Fatal("unregistered non-cgroup path accepted")
	}
}

func TestRecordLedgerCgroupEventsAndSCRAM(t *testing.T) {
	frozen, populated, err := ledgerCgroupEvents([]byte("populated 0\nfrozen 1\n"))
	if err != nil || !frozen || populated {
		t.Fatalf("joined cgroup: %t %t %v", frozen, populated, err)
	}
	for _, value := range []string{"populated 0\n", "populated 0\npopulated 1\nfrozen 1\n", "populated 0\nfrozen 2\n"} {
		if _, _, err := ledgerCgroupEvents([]byte(value)); err == nil {
			t.Fatalf("invalid cgroup events accepted: %q", value)
		}
	}
	verifier := ledgerSCRAM("a sufficiently long management password", []byte("fixed test salt"), 4096)
	if !ledgerMatchesSCRAM(verifier, "a sufficiently long management password") || ledgerMatchesSCRAM(verifier, "previous password") {
		t.Fatal("SCRAM verifier did not distinguish credentials")
	}
}

func TestRecordLedgerPinsEveryAppEndpoint(t *testing.T) {
	config, err := pgxpool.ParseConfig("postgres://admin:password@old.example:6543/houfeng?sslmode=prefer")
	if err != nil {
		t.Fatal(err)
	}
	if err := ledgerPinApp(config, "172.30.0.7"); err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Host != "172.30.0.7" || config.ConnConfig.Port != 5432 {
		t.Fatal("primary endpoint was not isolated")
	}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if fallback.Host != "172.30.0.7" || fallback.Port != 5432 {
			t.Fatal("fallback retained the old endpoint")
		}
	}
	if err := ledgerPinApp(config, "not-an-ip"); err == nil {
		t.Fatal("invalid isolated address accepted")
	}
}
