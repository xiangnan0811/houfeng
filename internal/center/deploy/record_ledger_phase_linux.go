//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"houfeng/internal/center/deletionledger"
)

const (
	ledgerPhaseQuiescent   = "quiescent"
	ledgerPhasePredecessor = "predecessor_callback"
	ledgerPhaseMigration   = "migration_callback"
	ledgerPhaseRefencing   = "refencing"
)

type ledgerMaintenancePhase struct {
	Version     int                             `json:"version"`
	Generation  uint64                          `json:"generation"`
	Previous    [32]byte                        `json:"previous"`
	RecoveryID  string                          `json:"recovery_id"`
	Application RecordLedgerApplicationIdentity `json:"application"`
	Phase       string                          `json:"phase"`
}

func ledgerPhaseTransition(previous, next string) bool {
	switch previous {
	case "":
		return next == ledgerPhaseQuiescent
	case ledgerPhaseQuiescent:
		return next == ledgerPhasePredecessor || next == ledgerPhaseMigration || next == ledgerPhaseRefencing
	case ledgerPhasePredecessor, ledgerPhaseMigration:
		return next == ledgerPhaseRefencing
	case ledgerPhaseRefencing:
		return next == ledgerPhaseQuiescent
	default:
		return false
	}
}

func ledgerReadMaintenancePhases(dir int) ([]ledgerMaintenancePhase, error) {
	names, err := ledgerNames(dir)
	if err != nil {
		return nil, err
	}
	var phases []ledgerMaintenancePhase
	var previous [32]byte
	var previousPhase string
	for _, name := range names {
		if !strings.HasPrefix(name, "phase-") {
			continue
		}
		generation := uint64(len(phases)) + 1
		if name != fmt.Sprintf("phase-%020d.json", generation) {
			return nil, ErrRecordLedgerFence
		}
		data, exists, err := fenceReadFile(dir, name)
		var phase ledgerMaintenancePhase
		if err != nil || !exists || fenceDecode(data, &phase) != nil || phase.Version != 1 || phase.Generation != generation || generation > math.MaxInt64 || phase.Previous != previous || !fenceRID.MatchString(phase.RecoveryID) || !ledgerPhaseTransition(previousPhase, phase.Phase) {
			return nil, ErrRecordLedgerFence
		}
		if len(phases) != 0 && phase.Application != phases[0].Application {
			return nil, ErrRecordLedgerFence
		}
		phases = append(phases, phase)
		previous, previousPhase = sha256.Sum256(data), phase.Phase
	}
	return phases, nil
}

func (f *RecordLedgerFence) loadMaintenancePhase() error {
	phases, err := ledgerReadMaintenancePhases(f.control)
	if err != nil {
		return err
	}
	if len(phases) == 0 {
		phase := ledgerMaintenancePhase{Version: 1, Generation: 1, RecoveryID: f.request.Compose.RecoveryID, Application: f.request.AppIdentity, Phase: ledgerPhaseQuiescent}
		if _, err := ledgerPublishExact(f.control, "phase-00000000000000000001.json", phase); err != nil {
			return err
		}
		f.phase = phase
		return nil
	}
	phase := phases[len(phases)-1]
	if phase.Application != f.request.AppIdentity || phase.Phase != ledgerPhaseQuiescent {
		return ErrRecordLedgerFence
	}
	f.phase = phase
	return nil
}

func (f *RecordLedgerFence) requireQuiescent() error {
	f.mu.Lock()
	phase := f.phase
	closed := f.closed
	f.mu.Unlock()
	if closed || phase.Generation == 0 || phase.Phase != ledgerPhaseQuiescent {
		return ErrRecordLedgerFence
	}
	phases, err := ledgerReadMaintenancePhases(f.control)
	if err != nil || len(phases) == 0 || phases[len(phases)-1] != phase {
		return ErrRecordLedgerFence
	}
	return nil
}

// transitionMaintenancePhase is called only by the registered coordinator
// participant. An entered source/record operation prevents a callback launch.
func (f *RecordLedgerFence) transitionMaintenancePhase(next string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || f.uses != 1 || f.phase.Generation == 0 || f.phase.Generation >= math.MaxInt64 || !ledgerPhaseTransition(f.phase.Phase, next) {
		return ErrRecordLedgerFence
	}
	phases, err := ledgerReadMaintenancePhases(f.control)
	if err != nil || len(phases) == 0 || phases[len(phases)-1] != f.phase {
		return ErrRecordLedgerFence
	}
	body, err := json.Marshal(f.phase)
	if err != nil {
		return ErrRecordLedgerFence
	}
	phase := ledgerMaintenancePhase{Version: 1, Generation: f.phase.Generation + 1, Previous: sha256.Sum256(body), RecoveryID: f.request.Compose.RecoveryID, Application: f.request.AppIdentity, Phase: next}
	if _, err := ledgerPublishExact(f.control, fmt.Sprintf("phase-%020d.json", phase.Generation), phase); err != nil {
		return err
	}
	f.phase = phase
	if err := f.checkpointMaintenancePhase(f.ctx, phase); err != nil {
		return err
	}
	return nil
}

var _ deletionledger.SourceRetirementFence = (*RecordLedgerFence)(nil)

func (f *RecordLedgerFence) SourceRetirementPhase(ctx context.Context) (uint64, error) {
	if err := f.enter(ctx); err != nil {
		return 0, err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	if err := f.verifyExternal(ctx); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.phase.Generation, nil
}

func (f *RecordLedgerFence) WithSourceRetirementFence(ctx context.Context, generation uint64, fn func(context.Context) error) error {
	if fn == nil || generation == 0 {
		return ErrRecordLedgerFence
	}
	if err := f.enter(ctx); err != nil {
		return err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	f.sourceEffect.Lock()
	defer f.sourceEffect.Unlock()
	f.mu.Lock()
	matches := f.phase.Generation == generation && f.phase.Phase == ledgerPhaseQuiescent
	f.mu.Unlock()
	if !matches {
		return ErrRecordLedgerFence
	}
	if err := f.Preflight(ctx); err != nil {
		return err
	}
	return f.WithHeldLedgerFence(ctx, fn)
}
