//go:build linux

package deploy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recorddeletion"
)

type ledgerRoleInventory struct {
	Bootstrap string   `json:"bootstrap"`
	Reader    string   `json:"reader"`
	Writer    string   `json:"writer"`
	Previous  []string `json:"previous"`
}

type ledgerFenceJournal struct {
	Version           int                                   `json:"version"`
	Compose           ComposeOfflineFenceRequest            `json:"compose"`
	Guard             MaintenanceGuardIdentity              `json:"guard"`
	Application       RecordLedgerApplicationIdentity       `json:"application"`
	AppAdminRole      string                                `json:"app_admin_role"`
	AppPreviousLogins []string                              `json:"app_previous_logins"`
	Primary           recorddeletion.LedgerPhysicalIdentity `json:"primary"`
	Witness           recorddeletion.LedgerPhysicalIdentity `json:"witness"`
	PrimaryRoles      ledgerRoleInventory                   `json:"primary_roles"`
	WitnessRoles      ledgerRoleInventory                   `json:"witness_roles"`
	Containers        []fenceContainer                      `json:"containers"`
	Cgroups           []ledgerCgroupIdentity                `json:"cgroups"`
}

type ledgerDomainBinding struct {
	Version int                                   `json:"version"`
	Primary recorddeletion.LedgerPhysicalIdentity `json:"primary"`
	Witness recorddeletion.LedgerPhysicalIdentity `json:"witness"`
}

type ledgerBinding struct {
	Version    int    `json:"version"`
	RecoveryID string `json:"recovery_id"`
	Generation uint64 `json:"generation"`
}

type ledgerAnchor struct {
	Version  int                       `json:"version"`
	Index    uint64                    `json:"index"`
	Previous [32]byte                  `json:"previous"`
	Primary  recorddeletion.LedgerHead `json:"primary"`
	Witness  recorddeletion.LedgerHead `json:"witness"`
}

type ledgerIntent struct {
	Version   int                       `json:"version"`
	Key       string                    `json:"key"`
	Canonical []byte                    `json:"canonical"`
	Primary   recorddeletion.LedgerHead `json:"primary"`
	Witness   recorddeletion.LedgerHead `json:"witness"`
}

type ledgerIntentDone struct {
	Version      int      `json:"version"`
	Key          string   `json:"key"`
	IntentDigest [32]byte `json:"intent_digest"`
	AnchorIndex  uint64   `json:"anchor_index"`
	Sequence     uint64   `json:"sequence"`
	Hash         [32]byte `json:"hash"`
}

func ledgerControlDir(config string) (int, error) {
	parent, err := fenceJournalDir(config)
	if err != nil {
		return -1, ErrRecordLedgerFence
	}
	defer unix.Close(parent)
	if err := unix.Mkdirat(parent, "record-ledger", 0o700); err != nil && err != unix.EEXIST {
		return -1, ErrRecordLedgerFence
	}
	if unix.Fsync(parent) != nil {
		return -1, ErrRecordLedgerFence
	}
	fd, err := unix.Openat2(parent, "record-ledger", &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return -1, ErrRecordLedgerFence
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o7777 != 0o700 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink == 0 {
		unix.Close(fd)
		return -1, ErrRecordLedgerFence
	}
	return fd, nil
}

func ledgerControlMatches(config string, fd int) bool {
	parent, err := sourceConfigParent(config)
	if err != nil {
		return false
	}
	defer unix.Close(parent)
	if !privateSourceConfigParent(parent) {
		return false
	}
	fences, err := unix.Openat2(parent, "fences", &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return false
	}
	defer unix.Close(fences)
	var dir, dirNamed, opened, named unix.Stat_t
	return unix.Fstat(fences, &dir) == nil && unix.Fstatat(parent, "fences", &dirNamed, unix.AT_SYMLINK_NOFOLLOW) == nil &&
		dir.Dev == dirNamed.Dev && dir.Ino == dirNamed.Ino && dir.Mode&unix.S_IFMT == unix.S_IFDIR && dir.Mode&0o7777 == 0o700 && dir.Uid == uint32(os.Geteuid()) && dir.Nlink != 0 &&
		unix.Fstat(fd, &opened) == nil && unix.Fstatat(fences, "record-ledger", &named, unix.AT_SYMLINK_NOFOLLOW) == nil &&
		opened.Dev == named.Dev && opened.Ino == named.Ino && opened.Mode&unix.S_IFMT == unix.S_IFDIR && opened.Mode&0o7777 == 0o700 && opened.Uid == uint32(os.Geteuid()) && opened.Nlink != 0
}

func ledgerPrivateSubdir(parent int, name string) (int, error) {
	if err := unix.Mkdirat(parent, name, 0o700); err != nil && err != unix.EEXIST {
		return -1, ErrRecordLedgerFence
	}
	if unix.Fsync(parent) != nil {
		return -1, ErrRecordLedgerFence
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return -1, ErrRecordLedgerFence
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o7777 != 0o700 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink == 0 {
		unix.Close(fd)
		return -1, ErrRecordLedgerFence
	}
	return fd, nil
}

func ledgerSessionDir(control int, rid string) (int, error) {
	if !fenceRID.MatchString(rid) {
		return -1, ErrRecordLedgerFence
	}
	parent, err := ledgerPrivateSubdir(control, "sessions")
	if err != nil {
		return -1, err
	}
	defer unix.Close(parent)
	return ledgerPrivateSubdir(parent, rid)
}

func ledgerSessionMatches(control, session int, rid string) bool {
	if !fenceRID.MatchString(rid) {
		return false
	}
	parent, err := unix.Openat2(control, "sessions", &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return false
	}
	defer unix.Close(parent)
	var parentStat, opened, named unix.Stat_t
	return unix.Fstat(parent, &parentStat) == nil && parentStat.Mode&unix.S_IFMT == unix.S_IFDIR && parentStat.Mode&0o7777 == 0o700 && parentStat.Uid == uint32(os.Geteuid()) && parentStat.Nlink != 0 &&
		unix.Fstat(session, &opened) == nil && unix.Fstatat(parent, rid, &named, unix.AT_SYMLINK_NOFOLLOW) == nil && opened.Dev == named.Dev && opened.Ino == named.Ino && opened.Mode&unix.S_IFMT == unix.S_IFDIR && opened.Mode&0o7777 == 0o700 && opened.Uid == uint32(os.Geteuid()) && opened.Nlink != 0
}

func ledgerRoles(db *ledgerDatabase) ledgerRoleInventory {
	return ledgerRoleInventory{Bootstrap: db.adminRole, Reader: db.readerRole, Writer: db.writerRole, Previous: append([]string{}, db.previous...)}
}

func ledgerJournalRequest(r RecordLedgerMaintenanceRequest, cfg SourceMaintenanceConfig, project []fenceObservation, groups []ledgerCgroup, primary, witness *ledgerDatabase) ledgerFenceJournal {
	previous := append([]string{}, r.AppPreviousLogins...)
	sort.Strings(previous)
	journal := ledgerFenceJournal{Version: 1, Compose: r.Compose, Guard: cfg.GuardIdentity, Application: r.AppIdentity, AppAdminRole: r.AppAdminRole, AppPreviousLogins: previous,
		Primary: r.Primary.Identity, Witness: r.Witness.Identity, PrimaryRoles: ledgerRoles(primary), WitnessRoles: ledgerRoles(witness),
		Containers: make([]fenceContainer, 0, len(project)), Cgroups: make([]ledgerCgroupIdentity, 0, len(groups))}
	for _, item := range project {
		journal.Containers = append(journal.Containers, item.anchor)
	}
	for _, item := range groups {
		journal.Cgroups = append(journal.Cgroups, item.identity)
	}
	return journal
}

func ledgerSameJournal(stored, observed ledgerFenceJournal) bool {
	if len(stored.Containers) != len(observed.Containers) {
		return false
	}
	for i := range stored.Containers {
		if !fenceSameAnchors(stored.Containers[i], observed.Containers[i]) {
			return false
		}
	}
	stored.Containers = nil
	observed.Containers = nil
	a, err1 := json.Marshal(stored)
	b, err2 := json.Marshal(observed)
	return err1 == nil && err2 == nil && bytes.Equal(a, b)
}

func ledgerPublishExact(dir int, name string, v any) (bool, error) {
	data, exists, err := fenceReadFile(dir, name)
	if err != nil {
		return false, ErrRecordLedgerFence
	}
	if exists {
		want, e := json.Marshal(v)
		if e != nil || !bytes.Equal(data, want) || unix.Fsync(dir) != nil {
			return false, ErrRecordLedgerFence
		}
		return false, nil
	}
	if fencePublish(dir, name, v) != nil {
		return false, ErrRecordLedgerFence
	}
	return true, nil
}

func ledgerReadExact(dir int, name string, expected any) error {
	data, exists, err := fenceReadFile(dir, name)
	want, marshalErr := json.Marshal(expected)
	if err != nil || !exists || marshalErr != nil || !bytes.Equal(data, want) {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerEnsureJournal(dir int, observed ledgerFenceJournal) (ledgerFenceJournal, error) {
	data, exists, err := fenceReadFile(dir, "session.json")
	if err != nil {
		return ledgerFenceJournal{}, ErrRecordLedgerFence
	}
	if !exists {
		if _, err := ledgerPublishExact(dir, "session.json", observed); err != nil {
			return ledgerFenceJournal{}, err
		}
		return observed, nil
	}
	var stored ledgerFenceJournal
	if fenceDecode(data, &stored) != nil || stored.Version != 1 || !ledgerSameJournal(stored, observed) || unix.Fsync(dir) != nil {
		return ledgerFenceJournal{}, ErrRecordLedgerFence
	}
	return stored, nil
}

func ledgerNames(dir int) ([]string, error) {
	fd, err := unix.Openat(dir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	file := os.NewFile(uintptr(fd), "ledger control directory")
	names, err := file.Readdirnames(-1)
	file.Close()
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	sort.Strings(names)
	return names, nil
}

func ledgerReadAnchors(dir int) ([]ledgerAnchor, error) {
	names, err := ledgerNames(dir)
	if err != nil {
		return nil, err
	}
	var result []ledgerAnchor
	var previous [32]byte
	for _, name := range names {
		if !strings.HasPrefix(name, "anchor-") {
			continue
		}
		if name != fmt.Sprintf("anchor-%020d.json", len(result)) {
			return nil, ErrRecordLedgerFence
		}
		data, exists, e := fenceReadFile(dir, name)
		var anchor ledgerAnchor
		if e != nil || !exists || fenceDecode(data, &anchor) != nil || anchor.Version != 1 || anchor.Index != uint64(len(result)) || anchor.Previous != previous {
			return nil, ErrRecordLedgerFence
		}
		if anchor.Index == 0 && (anchor.Primary != (recorddeletion.LedgerHead{}) || anchor.Witness != (recorddeletion.LedgerHead{})) {
			return nil, ErrRecordLedgerFence
		}
		if len(result) > 0 && (anchor.Primary.Sequence < result[len(result)-1].Primary.Sequence || anchor.Witness.Sequence < result[len(result)-1].Witness.Sequence) {
			return nil, ErrRecordLedgerFence
		}
		previous = sha256.Sum256(data)
		result = append(result, anchor)
	}
	if len(result) > 0 && unix.Fsync(dir) != nil {
		return nil, ErrRecordLedgerFence
	}
	return result, nil
}

func ledgerAppendAnchor(dir int, anchors []ledgerAnchor, primary, witness recorddeletion.LedgerHead) (ledgerAnchor, error) {
	anchor := ledgerAnchor{Version: 1, Index: uint64(len(anchors)), Primary: primary, Witness: witness}
	if len(anchors) > 0 {
		previous, err := json.Marshal(anchors[len(anchors)-1])
		if err != nil {
			return ledgerAnchor{}, ErrRecordLedgerFence
		}
		anchor.Previous = sha256.Sum256(previous)
	}
	if _, err := ledgerPublishExact(dir, fmt.Sprintf("anchor-%020d.json", anchor.Index), anchor); err != nil {
		return ledgerAnchor{}, err
	}
	return anchor, nil
}

func ledgerRequestCanonical(request recorddeletion.LedgerAppendRequest, domain [32]byte) ([]byte, error) {
	entry := recorddeletion.CanonicalLedgerEntry{DeletionLedgerEntry: recorddeletion.DeletionLedgerEntry{Request: request, Sequence: 1}, LedgerDomain: domain, ConfirmedAt: time.Unix(1, 0).UTC()}
	return recorddeletion.EncodeCanonicalLedgerEntry(entry)
}

func ledgerIntentKey(request recorddeletion.LedgerAppendRequest) string {
	// All three identities are independently validated and cannot contain NUL.
	sum := sha256.Sum256([]byte(string(request.DeploymentID) + "\x00" + string(request.ProjectID) + "\x00" + request.OperationID))
	return hex.EncodeToString(sum[:])
}

func ledgerReadIntents(dir int) ([]ledgerIntent, error) {
	names, err := ledgerNames(dir)
	if err != nil {
		return nil, err
	}
	var intents []ledgerIntent
	for _, name := range names {
		if !strings.HasPrefix(name, "intent-") {
			continue
		}
		data, exists, e := fenceReadFile(dir, name)
		var intent ledgerIntent
		if e != nil || !exists || fenceDecode(data, &intent) != nil || intent.Version != 1 || len(intent.Key) != 64 || name != "intent-"+intent.Key+".json" {
			return nil, ErrRecordLedgerFence
		}
		decoded, e := hex.DecodeString(intent.Key)
		if e != nil || len(decoded) != 32 {
			return nil, ErrRecordLedgerFence
		}
		entry, e := recorddeletion.DecodeCanonicalLedgerEntry(intent.Canonical)
		if e != nil || ledgerIntentKey(entry.Request) != intent.Key {
			return nil, ErrRecordLedgerFence
		}
		intents = append(intents, intent)
	}
	return intents, nil
}

func ledgerDone(dir int, intent ledgerIntent) (ledgerIntentDone, bool, error) {
	var done ledgerIntentDone
	data, exists, err := fenceReadFile(dir, "done-"+intent.Key+".json")
	if err != nil || !exists {
		return done, exists, err
	}
	encoded, err := json.Marshal(intent)
	if err != nil || fenceDecode(data, &done) != nil || done.Version != 1 || done.Key != intent.Key || done.IntentDigest != sha256.Sum256(encoded) || done.Sequence == 0 {
		return ledgerIntentDone{}, false, ErrRecordLedgerFence
	}
	return done, true, nil
}
