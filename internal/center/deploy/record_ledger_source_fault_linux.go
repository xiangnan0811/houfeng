//go:build linux

package deploy

import (
	"context"
	"fmt"
	"strings"

	"houfeng/internal/center/deletionledger"
)

func ledgerSourceFaultBackend(session ledgerAppSession) *ledgerPredecessorBackend {
	return &ledgerPredecessorBackend{PID: session.PID, Started: session.Started, Role: session.Role}
}

func (f *RecordLedgerFence) sourceCutpoint(ctx context.Context, boundary string, request deletionledger.SourceRetirementRequest, backend *ledgerPredecessorBackend) error {
	if f.maintenanceFault == nil || f.maintenanceFault.boundary != boundary {
		return nil
	}
	preview, err := f.sourcePreview(request)
	if err != nil {
		return err
	}
	kind := "source-intent-"
	switch boundary {
	case "app-commit", "w1-commit", "w2-commit":
	case "handoff-install":
		kind = "source-handoff-intent-"
	case "handoff-return":
		kind = "source-handoff-installed-"
	default:
		return ErrRecordLedgerFence
	}
	return f.maintenanceFault.pause(ctx, f, boundary, request.OperationID, kind+preview.Key+".json", nil, -1, backend)
}

// The selected transaction must be the exact backend recorded before the
// append/confirmation call, including when a pool could replace a connection.
func (f *RecordLedgerFence) sourceCommitContext(ctx context.Context, request deletionledger.SourceRetirementRequest, boundary, kind string, db *ledgerDatabase) context.Context {
	if f.maintenanceFault == nil || f.maintenanceFault.boundary != boundary {
		return ctx
	}
	return deletionledger.WithSourceCommitObserver(ctx, func(held context.Context, observation deletionledger.SourceCommitObservation) error {
		if observation.Boundary != boundary || observation.BackendPID <= 0 || observation.BackendStarted.IsZero() || observation.BackendRole != db.writerRole {
			return ErrRecordLedgerFence
		}
		prefix := "source-" + kind + "-writer-" + ledgerSourceKey(request) + "-"
		names, err := ledgerNames(f.control)
		if err != nil {
			return err
		}
		attempt := 0
		var original ledgerAppSession
		for _, name := range names {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			attempt++
			if name != fmt.Sprintf("%s%06d.json", prefix, attempt) {
				return ErrRecordLedgerFence
			}
			raw, exists, err := fenceReadFile(f.control, name)
			if err != nil || !exists || fenceDecode(raw, &original) != nil {
				return ErrRecordLedgerFence
			}
		}
		if attempt == 0 || original.PID != observation.BackendPID || !original.Started.Equal(observation.BackendStarted) || original.Role != observation.BackendRole {
			return ErrRecordLedgerFence
		}
		if !observation.Committed {
			return nil
		}
		return f.sourceCutpoint(held, boundary, request, ledgerSourceFaultBackend(original))
	})
}

// Advance holds work and has published next, but has not yet appended its
// in-memory anchor or the per-source anchored receipt. This is the fsync gap.
func (f *RecordLedgerFence) sourceAnchorCutpoint(ctx context.Context, items []ledgerSourcePending, next ledgerAnchor) error {
	if f.maintenanceFault == nil || f.maintenanceFault.boundary != "anchor-fsync" {
		return nil
	}
	if len(f.anchors) == 0 || next.Index != uint64(len(f.anchors)) || next.Primary != next.Witness {
		return ErrRecordLedgerFence
	}
	previous := f.anchors[len(f.anchors)-1]
	operation := ""
	for _, item := range items {
		if !item.found || item.entry.Sequence <= previous.Primary.Sequence || item.entry.Sequence > next.Primary.Sequence {
			continue
		}
		if operation != "" {
			return ErrRecordLedgerFence
		}
		operation = item.preview.Request.OperationID
	}
	if operation == "" {
		return ErrRecordLedgerFence
	}
	return f.maintenanceFault.pause(ctx, f, "anchor-fsync", operation, fmt.Sprintf("anchor-%020d.json", next.Index), nil, -1, nil)
}
