//go:build linux

package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sys/unix"
)

type ordinaryDrainEvidence struct {
	Version  int                      `json:"version"`
	Backends []ordinaryPendingBackend `json:"backends"`
}

// The controller lock and ordinary EX are held; this file is diagnostic only
// and never authorizes a transition. It excludes SQL text and credentials.
func ordinaryPublishDrainEvidence(dir int, backends []ordinaryPendingBackend) error {
	body, err := json.Marshal(ordinaryDrainEvidence{Version: 1, Backends: backends})
	if err != nil || len(body) > fenceOutputLimit {
		return errOrdinaryUpgrade
	}
	temp, err := ordinaryLaunchTempName()
	if err != nil {
		return errOrdinaryUpgrade
	}
	name := "drain-" + strings.TrimPrefix(temp, ".ordinary-launch-") + ".json"
	fd, err := unix.Openat2(dir, temp, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary drain evidence")
	defer func() { _ = file.Close(); _ = unix.Unlinkat(dir, temp, 0) }()
	if unix.Fchown(fd, 0, 0) != nil || unix.Fchmod(fd, 0o600) != nil {
		return errOrdinaryUpgrade
	}
	if n, err := file.Write(body); err != nil || n != len(body) || file.Sync() != nil || unix.Renameat2(dir, temp, dir, name, unix.RENAME_NOREPLACE) != nil || unix.Fsync(dir) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryBoundedDrain(ctx context.Context, observer *pgx.Conn, physical ordinaryPhysicalDatabase, records []ordinaryBackendRecord, operationDir int) error {
	drainCtx, cancel := context.WithTimeout(ctx, ordinaryDrainTimeout)
	defer cancel()
	pending, err := ordinaryWaitForBackendSettlementSnapshot(drainCtx, observer, physical, records, true)
	if errors.Is(err, context.DeadlineExceeded) {
		if writeErr := ordinaryPublishDrainEvidence(operationDir, pending); writeErr != nil {
			return errors.Join(err, writeErr)
		}
	}
	return err
}
