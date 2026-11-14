//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const ledgerPredecessorPath = "/opt/g2/c80-predecessor"
const ledgerPredecessorLauncher = "/opt/g2/c80-launcher"
const ledgerPredecessorCgroup = "/sys/fs/cgroup/g2-c80-predecessor"

type ledgerPredecessorProcess struct {
	PID              int    `json:"pid"`
	Starttime        uint64 `json:"starttime"`
	ExecutableSHA256 string `json:"executable_sha256"`
}

type ledgerPredecessorBackend struct {
	PID     int32     `json:"pid"`
	Started time.Time `json:"started"`
	Role    string    `json:"role"`
}

func (b ledgerPredecessorBackend) session() ledgerAppSession {
	return ledgerAppSession{PID: b.PID, Started: b.Started, Role: b.Role}
}

type ledgerPredecessorGuard struct {
	Dev uint64 `json:"dev"`
	Ino uint64 `json:"ino"`
}
type ledgerPredecessorGroup struct {
	Path string `json:"path"`
	Dev  uint64 `json:"dev"`
	Ino  uint64 `json:"ino"`
}

type ledgerPredecessorIntent struct {
	Version           uint64                          `json:"version"`
	PhaseGeneration   uint64                          `json:"phase_generation"`
	RecoveryID        string                          `json:"recovery_id"`
	Application       RecordLedgerApplicationIdentity `json:"application"`
	RealmDigest       string                          `json:"realm_digest"`
	Coordinator       ledgerPredecessorProcess        `json:"coordinator"`
	Guard             ledgerPredecessorGuard          `json:"guard"`
	ConfigFD          int                             `json:"config_fd"`
	ConfigDigest      string                          `json:"config_digest"`
	ActorUserID       string                          `json:"actor_user_id"`
	RecordID          string                          `json:"record_id"`
	ChildCgroup       ledgerPredecessorGroup          `json:"child_cgroup"`
	LauncherSHA256    string                          `json:"launcher_sha256"`
	PredecessorSHA256 string                          `json:"predecessor_sha256"`
}

type ledgerPredecessorConfig struct {
	Version         uint64 `json:"version"`
	PhaseGeneration uint64 `json:"phase_generation"`
	RuntimeURL      string `json:"runtime_url"`
	DeploymentID    string `json:"deployment_id"`
	ProjectID       string `json:"project_id"`
	ActorUserID     string `json:"actor_user_id"`
	RecordID        string `json:"record_id"`
}

type ledgerPredecessorEffectBinding struct {
	Version         uint64                          `json:"version"`
	PhaseGeneration uint64                          `json:"phase_generation"`
	RecoveryID      string                          `json:"recovery_id"`
	Application     RecordLedgerApplicationIdentity `json:"application"`
	RealmDigest     string                          `json:"realm_digest"`
	Backend         ledgerPredecessorBackend        `json:"backend"`
}

type ledgerPredecessorCreateIntent struct {
	ledgerPredecessorEffectBinding
	Command       ledgerC80CreateCommand `json:"command"`
	CommandDigest string                 `json:"command_digest"`
}

type ledgerPredecessorCreateResult struct {
	ledgerPredecessorEffectBinding
	CommandDigest string    `json:"command_digest"`
	ReservationID string    `json:"reservation_id"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type ledgerPredecessorReserveIntent struct {
	ledgerPredecessorEffectBinding
	Command       ledgerC80ReserveCommand `json:"command"`
	CommandDigest string                  `json:"command_digest"`
}

type ledgerPredecessorReserveResult struct {
	ledgerPredecessorEffectBinding
	CreateCommandDigest string `json:"create_command_digest"`
	CommandDigest       string `json:"command_digest"`
	ReservationID       string `json:"reservation_id"`
	OperationID         string `json:"operation_id"`
}

type ledgerPredecessorOutput struct {
	Version         uint64 `json:"version"`
	PhaseGeneration uint64 `json:"phase_generation"`
	Status          string `json:"status"`
	OperationID     string `json:"operation_id"`
	Reason          string `json:"reason"`
}

type ledgerPredecessorWrittenResult struct {
	Version         uint64                          `json:"version"`
	PhaseGeneration uint64                          `json:"phase_generation"`
	RecoveryID      string                          `json:"recovery_id"`
	Application     RecordLedgerApplicationIdentity `json:"application"`
	RealmDigest     string                          `json:"realm_digest"`
	ResultDigest    string                          `json:"result_digest"`
	Result          ledgerPredecessorOutput         `json:"result"`
}

type ledgerPredecessorResolution struct {
	ledgerPredecessorEffectBinding
	CreateCommandDigest  string `json:"create_command_digest"`
	ReserveCommandDigest string `json:"reserve_command_digest"`
	ReservationID        string `json:"reservation_id"`
	OperationID          string `json:"operation_id"`
	Outcome              string `json:"outcome"`
}

func ledgerPredecessorName(kind string, phase uint64) string {
	return fmt.Sprintf("predecessor-%s-%020d.json", kind, phase)
}

func ledgerPredecessorHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Decode the original bytes, including duplicate-field rejection at every
// object depth. Durable records are bounded by the existing private-file layer.
func ledgerPredecessorDecode(body []byte, target any) error {
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || object == nil {
		return ErrRecordLedgerFence
	}
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	if _, err := ledgerCallbackFields(body, names...); err != nil {
		return err
	}
	return fenceDecode(body, target)
}

func ledgerPredecessorRead(dir int, kind string, phase uint64, target any) (bool, error) {
	body, found, err := fenceReadFile(dir, ledgerPredecessorName(kind, phase))
	if err != nil || !found {
		return found, err
	}
	if ledgerPredecessorDecode(body, target) != nil {
		return false, ErrRecordLedgerFence
	}
	return true, nil
}

func ledgerPredecessorReadonly(ctx context.Context, f *RecordLedgerFence, fn func(pgx.Tx) error) error {
	tx, err := f.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	if ledgerAppIdentity(ctx, tx, f.request.AppIdentity) != nil {
		return ErrRecordLedgerFence
	}
	if err := fn(tx); err != nil {
		return err
	}
	if tx.Commit(ctx) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

type ledgerPredecessorReady struct {
	ledgerPredecessorEffectBinding
	Child ledgerPredecessorProcess `json:"child"`
}

type ledgerPredecessorCompletion struct {
	Version              uint64                          `json:"version"`
	PhaseGeneration      uint64                          `json:"phase_generation"`
	FinalPhaseGeneration uint64                          `json:"final_phase_generation"`
	Application          RecordLedgerApplicationIdentity `json:"application"`
	RecoveryID           string                          `json:"recovery_id"`
	ActorUserID          string                          `json:"actor_user_id"`
	RecordID             string                          `json:"record_id"`
	Coordinator          ledgerPredecessorProcess        `json:"coordinator"`
	Child                ledgerPredecessorProcess        `json:"child"`
	Backend              ledgerPredecessorBackend        `json:"backend"`
	Events               []ledgerPredecessorEvent        `json:"events"`
	ChildResult          ledgerPredecessorOutput         `json:"child_result"`
	Exit                 struct {
		PidfdExited bool `json:"pidfd_exited"`
		WaitStatus  int  `json:"wait_status"`
	} `json:"exit"`
}

func ledgerPredecessorOutputValid(result ledgerPredecessorOutput, phase uint64) bool {
	if result.Version != 1 || result.PhaseGeneration != phase || phase == 0 {
		return false
	}
	switch result.Status {
	case "committed":
		return result.Reason == "app_reserved" && ledgerC80ID(result.OperationID, "rpo_")
	case "rejected":
		return result.Reason == "source_denied" && result.OperationID == ""
	case "unavailable":
		return result.OperationID == "" && (result.Reason == "inventory_unavailable" || result.Reason == "admission_unavailable" || result.Reason == "service_unavailable")
	default:
		return false
	}
}
