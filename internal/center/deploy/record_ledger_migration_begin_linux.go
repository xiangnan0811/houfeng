//go:build linux

package deploy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
	"houfeng/internal/center/recorddeletion"
)

type ledgerMigrationBegin struct {
	Version                    uint64                                `json:"version"`
	Family                     string                                `json:"family"`
	RecoveryID                 string                                `json:"recovery_id"`
	Application                RecordLedgerApplicationIdentity       `json:"application"`
	PhaseGeneration            uint64                                `json:"phase_generation"`
	Phase                      ledgerMaintenancePhase                `json:"phase"`
	InventoryDigest            string                                `json:"inventory_digest"`
	SessionDigest              string                                `json:"session_digest"`
	OriginalInventoryDigest    string                                `json:"original_inventory_digest"`
	ManagementCredentialDigest string                                `json:"management_credential_digest"`
	Primary                    recorddeletion.LedgerPhysicalIdentity `json:"primary"`
	Witness                    recorddeletion.LedgerPhysicalIdentity `json:"witness"`
	Coordinator                ledgerPredecessorProcess              `json:"coordinator"`
	Guard                      ledgerPredecessorGuard                `json:"guard"`
	BootID                     string                                `json:"boot_id"`
	Cgroup                     ledgerPredecessorGroup                `json:"cgroup"`
}

type ledgerMigrationBeginSettled struct {
	Version         uint64                          `json:"version"`
	PhaseGeneration uint64                          `json:"phase_generation"`
	RecoveryID      string                          `json:"recovery_id"`
	Application     RecordLedgerApplicationIdentity `json:"application"`
	BeginDigest     string                          `json:"begin_digest"`
	Coordinator     ledgerPredecessorProcess        `json:"coordinator"`
	Bootstrap       ledgerPredecessorBackend        `json:"bootstrap"`
	CatalogDigest   string                          `json:"catalog_digest"`
	RolesDigest     string                          `json:"roles_digest"`
	Outcome         string                          `json:"outcome"`
}

type ledgerMaintenanceInventoryEntry struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func ledgerMaintenanceInventory(dir int, prefix, excluded string, entries *[]ledgerMaintenanceInventoryEntry) error {
	names, err := ledgerNames(dir)
	if err != nil {
		return err
	}
	for _, name := range names {
		relative := prefix + name
		if relative == excluded {
			continue
		}
		// The normal file reader settles its exact two-link publication gap.
		// Delay temp validation until all final names have been read.
		if strings.HasPrefix(name, ".tmp-") {
			continue
		}
		var st unix.Stat_t
		if unix.Fstatat(dir, name, &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
			return ErrRecordLedgerFence
		}
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			if prefix != "" && prefix != "sessions/" || prefix == "" && name != "sessions" {
				return ErrRecordLedgerFence
			}
			fd, err := unix.Openat2(dir, name, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
			if err != nil {
				return ErrRecordLedgerFence
			}
			err = ledgerMaintenanceInventory(fd, relative+"/", excluded, entries)
			unix.Close(fd)
			if err != nil {
				return err
			}
			continue
		}
		body, found, err := fenceReadFile(dir, name)
		if err != nil || !found {
			return ErrRecordLedgerFence
		}
		*entries = append(*entries, ledgerMaintenanceInventoryEntry{Name: relative, SHA256: ledgerPredecessorHash(body)})
	}
	for _, name := range names {
		if !strings.HasPrefix(name, ".tmp-") {
			continue
		}
		var st unix.Stat_t
		if err := unix.Fstatat(dir, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != unix.ENOENT {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

func ledgerPublishMigrationBegin(ctx context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent) error {
	name := ledgerMigrationName("begin", intent.PhaseGeneration)
	entries := make([]ledgerMaintenanceInventoryEntry, 0)
	if err := ledgerMaintenanceInventory(f.control, "", name, &entries); err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	inventoryDigest, err := ledgerMigrationDigest(entries)
	if err != nil {
		return err
	}
	rawDigest := func(name string) (string, error) {
		body, found, err := fenceReadFile(f.session, name)
		if err != nil || !found {
			return "", ErrRecordLedgerFence
		}
		return ledgerPredecessorHash(body), nil
	}
	sessionDigest, err := rawDigest("session.json")
	if err != nil {
		return err
	}
	credentialDigest, err := rawDigest("management-credential.json")
	if err != nil {
		return err
	}
	begin := ledgerMigrationBegin{Version: 1, Family: "migration", RecoveryID: intent.RecoveryID, Application: intent.Application,
		PhaseGeneration: intent.PhaseGeneration, Phase: f.phase, InventoryDigest: inventoryDigest, SessionDigest: sessionDigest,
		OriginalInventoryDigest: intent.OriginalInventoryDigest, ManagementCredentialDigest: credentialDigest,
		Primary: f.journal.Primary, Witness: f.journal.Witness, Coordinator: intent.Coordinator, Guard: intent.Guard, BootID: intent.BootID, Cgroup: intent.Cgroup}
	if f.phase.Phase != ledgerPhaseQuiescent || f.phase.Generation+1 != intent.PhaseGeneration {
		return ErrRecordLedgerFence
	}
	if _, err := ledgerPublishExact(f.control, fmt.Sprintf("migration-begin-%020d.json", intent.PhaseGeneration), begin); err != nil {
		return err
	}
	return ledgerMigrationBeginCheckpoint(ctx, f, begin, "begin", begin)
}
