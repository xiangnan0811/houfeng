//go:build linux

package deploy

import (
	"context"
	"reflect"

	"golang.org/x/sys/unix"
)

// Activated successor selection never rewrites or substitutes the original
// per-RID inventory. The activation is usable only with its terminal phase,
// the exact predecessor inventory, and the independently captured normal
// successor and preparation records.
func ledgerMigrationActivatedInventory(ctx context.Context, control int, request RecordLedgerMaintenanceRequest, checkpointer ledgerMaintenanceCheckpointer) (ledgerAppInventory, bool, error) {
	var empty ledgerAppInventory
	journal, err := ledgerMigrationLoadJournal(control, request.AppIdentity, false)
	if err != nil {
		return empty, false, err
	}
	if err := ledgerMigrationAuthorizeJournal(ctx, control, journal, checkpointer); err != nil {
		return empty, false, err
	}
	if journal.Activation == nil {
		return empty, false, nil
	}
	if journal.Intent == nil {
		return empty, false, ErrRecordLedgerFence
	}
	intent, activation := *journal.Intent, *journal.Activation
	parent, err := unix.Openat2(control, "sessions", &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return empty, false, ErrRecordLedgerFence
	}
	defer unix.Close(parent)
	oldSession, err := unix.Openat2(parent, intent.RecoveryID, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return empty, false, ErrRecordLedgerFence
	}
	defer unix.Close(oldSession)
	if !ledgerSessionMatches(control, oldSession, intent.RecoveryID) {
		return empty, false, ErrRecordLedgerFence
	}
	body, found, err := fenceReadFile(oldSession, "app-inventory.json")
	var original ledgerAppInventory
	if err != nil || !found || fenceDecode(body, &original) != nil || ledgerPredecessorHash(body) != intent.OriginalInventoryDigest ||
		original.RecoveryID != intent.RecoveryID || original.Application != request.AppIdentity || !original.LegacyC80 ||
		ledgerValidateAppJournal(original, original.Credential) != nil || ledgerReadExact(oldSession, "management-credential.json", original.Credential) != nil {
		return empty, false, ErrRecordLedgerFence
	}
	journalBody, journalFound, err := fenceReadFile(oldSession, "session.json")
	var originalJournal ledgerFenceJournal
	if err != nil || !journalFound || fenceDecode(journalBody, &originalJournal) != nil || originalJournal.Version != 1 ||
		originalJournal.Compose.RecoveryID != intent.RecoveryID || originalJournal.Application != intent.Application || originalJournal.AppAdminRole != original.Admin ||
		!reflect.DeepEqual(originalJournal.AppPreviousLogins, original.Previous) || originalJournal.Guard.FileDevice != intent.Guard.Dev || originalJournal.Guard.FileInode != intent.Guard.Ino {
		return empty, false, ErrRecordLedgerFence
	}
	successor, err := ledgerMigrationSelectedInventory(control, intent, activation, original)
	if err != nil {
		return empty, false, err
	}
	return successor, true, nil
}

func ledgerMigrationVerifyInventory(ctx context.Context, control, session int, request RecordLedgerMaintenanceRequest, expected ledgerAppInventory, checkpointer ledgerMaintenanceCheckpointer) error {
	source, active, err := ledgerMigrationActivatedInventory(ctx, control, request, checkpointer)
	if err != nil {
		return err
	}
	if !active {
		if expected.OriginalPrevious != nil {
			return ErrRecordLedgerFence
		}
		return ledgerReadExact(session, "app-inventory.json", expected)
	}
	if source.RecoveryID != request.Compose.RecoveryID || !reflect.DeepEqual(source, expected) {
		return ErrRecordLedgerFence
	}
	// ActivatedInventory independently read and verified the original C80 file.
	return nil
}
