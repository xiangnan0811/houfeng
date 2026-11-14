//go:build linux

package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// This binding is installed by the root supervisor before the coordinator is
// admitted to its dedicated cgroup. The coordinator never accepts a path,
// digest, role or cgroup identity from the private management request.
type ledgerMigrationInstallation struct {
	Version           uint64                 `json:"version"`
	BootID            string                 `json:"boot_id"`
	ProgramSHA256     string                 `json:"program_sha256"`
	CoordinatorSHA256 string                 `json:"coordinator_sha256"`
	Cgroup            ledgerPredecessorGroup `json:"cgroup"`
}

func ledgerMigrationInstall() (ledgerMigrationInstallation, error) {
	var result ledgerMigrationInstallation
	for _, path := range []string{"/opt", "/opt/g2"} {
		var st unix.Stat_t
		if unix.Lstat(path, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0022 != 0 {
			return result, ErrRecordLedgerFence
		}
	}
	fd, err := unix.Open("/opt/g2/c81-migration.json", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return result, ErrRecordLedgerFence
	}
	file := os.NewFile(uintptr(fd), "c81-migration-installation")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != 0440 ||
		before.Uid != 0 || before.Gid != 10002 || before.Nlink != 1 || before.Size <= 0 || before.Size > 4096 {
		return result, ErrRecordLedgerFence
	}
	body, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || int64(len(body)) != before.Size || unix.Fstat(fd, &after) != nil ||
		unix.Lstat("/opt/g2/c81-migration.json", &named) != nil || !sameSourceFile(before, after) || !sameSourceFile(before, named) {
		return result, ErrRecordLedgerFence
	}
	fields, err := ledgerCallbackFields(body, "version", "boot_id", "program_sha256", "coordinator_sha256", "cgroup")
	if err != nil {
		return result, ErrRecordLedgerFence
	}
	group, err := ledgerCallbackFields(fields["cgroup"], "path", "dev", "ino")
	if err != nil || group == nil || json.Unmarshal(body, &result) != nil || result.Version != 1 ||
		result.Cgroup.Path != ledgerMigrationCgroup || result.Cgroup.Dev == 0 || result.Cgroup.Ino == 0 {
		return ledgerMigrationInstallation{}, ErrRecordLedgerFence
	}
	if _, err := ledgerCallbackDigest(fields, "program_sha256"); err != nil {
		return ledgerMigrationInstallation{}, err
	}
	if _, err := ledgerCallbackDigest(fields, "coordinator_sha256"); err != nil {
		return ledgerMigrationInstallation{}, err
	}
	boot, err := ledgerMigrationBootID()
	if err != nil || boot != result.BootID || ledgerMigrationScope(result.Cgroup, os.Getpid()) != nil {
		return ledgerMigrationInstallation{}, ErrRecordLedgerFence
	}
	self, err := ledgerPredecessorSelf()
	if err != nil || self.ExecutableSHA256 != result.CoordinatorSHA256 {
		return ledgerMigrationInstallation{}, ErrRecordLedgerFence
	}
	program, err := ledgerPredecessorProgram(ledgerMigrationProgram, 0550)
	if err != nil || program != result.ProgramSHA256 {
		return ledgerMigrationInstallation{}, ErrRecordLedgerFence
	}
	return result, nil
}

type ledgerMigrationIntent struct {
	Version                 uint64                          `json:"version"`
	PhaseGeneration         uint64                          `json:"phase_generation"`
	PreviousPhaseGeneration uint64                          `json:"previous_phase_generation"`
	RecoveryID              string                          `json:"recovery_id"`
	Application             RecordLedgerApplicationIdentity `json:"application"`
	OriginalInventoryDigest string                          `json:"original_inventory_digest"`
	Guard                   ledgerPredecessorGuard          `json:"guard"`
	BootID                  string                          `json:"boot_id"`
	Cgroup                  ledgerPredecessorGroup          `json:"cgroup"`
	Coordinator             ledgerPredecessorProcess        `json:"coordinator"`
	ProgramSHA256           string                          `json:"program_sha256"`
	RuntimeRole             string                          `json:"runtime_role"`
	AdminRole               string                          `json:"admin_role"`
	MigratorRole            string                          `json:"migrator_role"`
	WorkerRole              string                          `json:"worker_role"`
	WorkerAbsent            bool                            `json:"worker_absent"`
	Bootstrap               ledgerPredecessorBackend        `json:"bootstrap"`
}

type ledgerMigrationPreparation struct {
	Version         uint64                   `json:"version"`
	PhaseGeneration uint64                   `json:"phase_generation"`
	IntentDigest    string                   `json:"intent_digest"`
	From            ledgerAppRoleSnapshot    `json:"from"`
	Normal          ledgerAppRoleSnapshot    `json:"normal"`
	FinalTarget     ledgerAppRoleSnapshot    `json:"final_target"`
	EmergencyTarget ledgerAppRoleSnapshot    `json:"emergency_target"`
	Bootstrap       ledgerPredecessorBackend `json:"bootstrap"`
	TransactionID   string                   `json:"transaction_id"`
	WorkerTarget    ledgerAppRoleTarget      `json:"worker_target"`
}

type ledgerMigrationReady struct {
	Version         uint64                   `json:"version"`
	PhaseGeneration uint64                   `json:"phase_generation"`
	IntentDigest    string                   `json:"intent_digest"`
	Child           ledgerPredecessorProcess `json:"child"`
	Backend         ledgerPredecessorBackend `json:"backend"`
}

type ledgerMigrationReadyAbandoned struct {
	Version           uint64                          `json:"version"`
	PhaseGeneration   uint64                          `json:"phase_generation"`
	RecoveryID        string                          `json:"recovery_id"`
	Application       RecordLedgerApplicationIdentity `json:"application"`
	IntentDigest      string                          `json:"intent_digest"`
	PreparationDigest string                          `json:"preparation_digest"`
	ReadyDigest       string                          `json:"ready_digest"`
	Coordinator       ledgerPredecessorProcess        `json:"coordinator"`
	Bootstrap         ledgerPredecessorBackend        `json:"bootstrap"`
	Restricted        ledgerAppRoleSnapshot           `json:"restricted"`
	CatalogDigest     string                          `json:"catalog_digest"`
	Outcome           string                          `json:"outcome"`
}

type ledgerMigrationNormal struct {
	Version           uint64             `json:"version"`
	PhaseGeneration   uint64             `json:"phase_generation"`
	IntentDigest      string             `json:"intent_digest"`
	PreparationDigest string             `json:"preparation_digest"`
	Inventory         ledgerAppInventory `json:"inventory"`
}

type ledgerMigrationActivation struct {
	Version                 uint64                          `json:"version"`
	PhaseGeneration         uint64                          `json:"phase_generation"`
	TerminalPhaseGeneration uint64                          `json:"terminal_phase_generation"`
	RecoveryID              string                          `json:"recovery_id"`
	Application             RecordLedgerApplicationIdentity `json:"application"`
	IntentDigest            string                          `json:"intent_digest"`
	OriginalInventoryDigest string                          `json:"original_inventory_digest"`
	SuccessorDigest         string                          `json:"successor_digest"`
	NormalDigest            string                          `json:"normal_digest"`
	NormalSource            string                          `json:"normal_source"`
	NormalAttempt           uint64                          `json:"normal_attempt"`
	ManifestDigest          string                          `json:"manifest_digest"`
}

type ledgerMigrationAbort struct {
	Version            uint64 `json:"version"`
	PhaseGeneration    uint64 `json:"phase_generation"`
	IntentDigest       string `json:"intent_digest"`
	CompensationDigest string `json:"compensation_digest"`
	Outcome            string `json:"outcome"`
}

type ledgerMigrationCompensation struct {
	Version           uint64 `json:"version"`
	PhaseGeneration   uint64 `json:"phase_generation"`
	IntentDigest      string `json:"intent_digest"`
	PreparationDigest string `json:"preparation_digest"`
	WorkerOID         uint32 `json:"worker_oid"`
	TargetDigest      string `json:"target_digest"`
}

func ledgerMigrationName(kind string, generation uint64) string {
	return fmt.Sprintf("migration-%s-%020d.json", kind, generation)
}

func ledgerMigrationDigest(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func ledgerMigrationNameGeneration(kind, name string) (uint64, bool) {
	prefix := "migration-" + kind + "-"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
	generation, err := strconv.ParseUint(value, 10, 64)
	return generation, err == nil && generation != 0 && name == ledgerMigrationName(kind, generation)
}

func ledgerMigrationRead(dir int, kind string, generation uint64, target any) (bool, error) {
	body, found, err := fenceReadFile(dir, ledgerMigrationName(kind, generation))
	if err != nil || !found {
		return found, err
	}
	if fenceDecode(body, target) != nil {
		return false, ErrRecordLedgerFence
	}
	return true, nil
}

// The two fixed-width numbers identify the migration and its observation attempt.
func ledgerMigrationObservationNameParts(name string) (string, uint64, uint64, bool) {
	for _, kind := range []string{"intent", "normal", "restrict", "settled"} {
		prefix := "migration-observe-" + kind + "-"
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json"), "-")
		if len(parts) != 2 {
			return "", 0, 0, false
		}
		generation, e1 := strconv.ParseUint(parts[0], 10, 64)
		attempt, e2 := strconv.ParseUint(parts[1], 10, 64)
		if e1 != nil || e2 != nil || generation == 0 || attempt == 0 || name != ledgerMigrationObservationName(kind, generation, attempt) {
			return "", 0, 0, false
		}
		return kind, generation, attempt, true
	}
	return "", 0, 0, false
}

func ledgerMigrationValidateBeginSession(control int, begin ledgerMigrationBegin) error {
	parent, err := unix.Openat2(control, "sessions", &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer unix.Close(parent)
	session, err := unix.Openat2(parent, begin.RecoveryID, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer unix.Close(session)
	if !ledgerSessionMatches(control, session, begin.RecoveryID) {
		return ErrRecordLedgerFence
	}
	for name, digest := range map[string]string{"session.json": begin.SessionDigest, "app-inventory.json": begin.OriginalInventoryDigest, "management-credential.json": begin.ManagementCredentialDigest} {
		body, found, err := fenceReadFile(session, name)
		if err != nil || !found || ledgerPredecessorHash(body) != digest {
			return ErrRecordLedgerFence
		}
		if name == "session.json" {
			var original ledgerFenceJournal
			if fenceDecode(body, &original) != nil || original.Version != 1 || original.Compose.RecoveryID != begin.RecoveryID || original.Application != begin.Application || original.Primary != begin.Primary || original.Witness != begin.Witness || original.Guard.FileDevice != begin.Guard.Dev || original.Guard.FileInode != begin.Guard.Ino {
				return ErrRecordLedgerFence
			}
		}
	}
	return nil
}

func ledgerMigrationOriginalInventory(control int, intent ledgerMigrationIntent) (ledgerAppInventory, error) {
	var original ledgerAppInventory
	parent, err := unix.Openat2(control, "sessions", &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return original, ErrRecordLedgerFence
	}
	defer unix.Close(parent)
	session, err := unix.Openat2(parent, intent.RecoveryID, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: maintenanceGuardResolve})
	if err != nil {
		return original, ErrRecordLedgerFence
	}
	defer unix.Close(session)
	body, found, err := fenceReadFile(session, "app-inventory.json")
	if err != nil || !found || !ledgerSessionMatches(control, session, intent.RecoveryID) || fenceDecode(body, &original) != nil || ledgerPredecessorHash(body) != intent.OriginalInventoryDigest || original.RecoveryID != intent.RecoveryID || original.Application != intent.Application || !original.LegacyC80 || ledgerValidateAppJournal(original, original.Credential) != nil || ledgerReadExact(session, "management-credential.json", original.Credential) != nil {
		return ledgerAppInventory{}, ErrRecordLedgerFence
	}
	return original, nil
}

// A final incomplete attempt remains pending and may only be resumed exactly.
func ledgerMigrationValidateObservationPrefix(control int, intent ledgerMigrationIntent, attempts map[uint64]map[string]bool) (bool, error) {
	if len(attempts) == 0 {
		return false, nil
	}
	var preparation ledgerMigrationPreparation
	if found, err := ledgerMigrationRead(control, "preparation", intent.PhaseGeneration, &preparation); err != nil || !found {
		return false, ErrRecordLedgerFence
	}
	original, err := ledgerMigrationOriginalInventory(control, intent)
	if err != nil {
		return false, err
	}
	var previous *string
	seen := make(map[string]bool)
	for attempt := uint64(1); attempt <= uint64(len(attempts)); attempt++ {
		kinds, ok := attempts[attempt]
		if !ok || !kinds["intent"] || attempt < uint64(len(attempts)) && !kinds["settled"] || kinds["settled"] && !kinds["restrict"] {
			return false, ErrRecordLedgerFence
		}
		var observation ledgerMigrationObservationIntent
		if found, err := ledgerMigrationReadObservation(control, "intent", intent.PhaseGeneration, attempt, &observation); err != nil || !found || observation.Attempt != attempt || ledgerMigrationValidateObservationIntent(observation, intent, preparation, original) != nil || !reflect.DeepEqual(observation.PreviousAttemptSettledDigest, previous) {
			return false, ErrRecordLedgerFence
		}
		for _, role := range observation.To.Roles {
			if role.Name != original.Bootstrap {
				if role.Verifier == nil || seen[*role.Verifier] {
					return false, ErrRecordLedgerFence
				}
				seen[*role.Verifier] = true
			}
		}
		observationDigest, err := ledgerMigrationDigest(observation)
		if err != nil {
			return false, ErrRecordLedgerFence
		}
		var normal ledgerMigrationObservationNormal
		if kinds["normal"] {
			if found, err := ledgerMigrationReadObservation(control, "normal", intent.PhaseGeneration, attempt, &normal); err != nil || !found || ledgerMigrationValidateObservationNormal(normal, observation, intent, preparation, original) != nil {
				return false, ErrRecordLedgerFence
			}
		}
		var restriction ledgerMigrationObservationRestriction
		if kinds["restrict"] {
			if found, err := ledgerMigrationReadObservation(control, "restrict", intent.PhaseGeneration, attempt, &restriction); err != nil || !found || restriction.Version != 1 || restriction.PhaseGeneration != intent.PhaseGeneration || restriction.Attempt != attempt || restriction.IntentDigest != observation.IntentDigest || restriction.ObservationIntentDigest != observationDigest || !reflect.DeepEqual(restriction.From, observation.To) || (!reflect.DeepEqual(restriction.To, observation.FinalTarget) && !reflect.DeepEqual(restriction.To, observation.EmergencyTarget)) {
				return false, ErrRecordLedgerFence
			}
		}
		if !kinds["settled"] {
			return true, nil
		}
		var settled ledgerMigrationObservationSettled
		restrictionDigest, err := ledgerMigrationDigest(restriction)
		if found, readErr := ledgerMigrationReadObservation(control, "settled", intent.PhaseGeneration, attempt, &settled); err != nil || readErr != nil || !found || settled.Version != 1 || settled.PhaseGeneration != intent.PhaseGeneration || settled.Attempt != attempt || settled.IntentDigest != observation.IntentDigest || settled.ObservationIntentDigest != observationDigest || settled.RestrictionDigest != restrictionDigest {
			return false, ErrRecordLedgerFence
		}
		switch settled.Outcome {
		case "grant_not_committed", "refenced_without_normal":
			if kinds["normal"] || settled.NormalDigest != nil {
				return false, ErrRecordLedgerFence
			}
		case "normal_observed_refenced":
			normalDigest, err := ledgerMigrationDigest(normal)
			if err != nil || !kinds["normal"] || settled.NormalDigest == nil || *settled.NormalDigest != normalDigest {
				return false, ErrRecordLedgerFence
			}
		default:
			return false, ErrRecordLedgerFence
		}
		digest, err := ledgerMigrationDigest(settled)
		if err != nil {
			return false, ErrRecordLedgerFence
		}
		previous = &digest
	}
	return false, nil
}

// Selection is bound to the activation, never inferred from directory order.
func ledgerMigrationSelectedInventory(control int, intent ledgerMigrationIntent, activation ledgerMigrationActivation, original ledgerAppInventory) (ledgerAppInventory, error) {
	var empty ledgerAppInventory
	var preparation ledgerMigrationPreparation
	if found, err := ledgerMigrationRead(control, "preparation", intent.PhaseGeneration, &preparation); err != nil || !found || ledgerMigrationValidatePreparation(preparation, intent, original) != nil {
		return empty, ErrRecordLedgerFence
	}
	intentDigest, err := ledgerMigrationDigest(intent)
	var ready ledgerMigrationReady
	if found, readErr := ledgerMigrationRead(control, "ready", intent.PhaseGeneration, &ready); err != nil || readErr != nil || !found || ready.Version != 1 || ready.PhaseGeneration != intent.PhaseGeneration || ready.IntentDigest != intentDigest || ready.Child.PID <= 0 || ready.Child.Starttime == 0 || ready.Child.ExecutableSHA256 != intent.ProgramSHA256 || ready.Backend.PID <= 0 || ready.Backend.Started.IsZero() || ready.Backend.Started.Nanosecond()%1000 != 0 || ready.Backend.Role != intent.MigratorRole {
		return empty, ErrRecordLedgerFence
	}
	var selected ledgerAppInventory
	var normalDigest string
	switch activation.NormalSource {
	case "original":
		var normal ledgerMigrationNormal
		if activation.NormalAttempt != 0 {
			return empty, ErrRecordLedgerFence
		}
		if found, err := ledgerMigrationRead(control, "normal", intent.PhaseGeneration, &normal); err != nil || !found || ledgerMigrationValidateNormal(normal, intent, preparation, original) != nil {
			return empty, ErrRecordLedgerFence
		}
		var restriction ledgerMigrationRestriction
		if found, err := ledgerMigrationRead(control, "restrict", intent.PhaseGeneration, &restriction); err != nil || !found || ledgerMigrationValidateRestriction(restriction, intent, preparation, original) != nil || !reflect.DeepEqual(restriction.To, ledgerAppExpectedRoles(normal.Inventory, ledgerAppTarget)) {
			return empty, ErrRecordLedgerFence
		}
		selected = normal.Inventory
		normalDigest, err = ledgerMigrationDigest(normal)
	case "observation":
		if activation.NormalAttempt == 0 {
			return empty, ErrRecordLedgerFence
		}
		normal, selectErr := ledgerMigrationSelectObservation(control, intent, preparation, original, activation.NormalAttempt)
		if selectErr != nil {
			return empty, ErrRecordLedgerFence
		}
		selected = normal.Inventory
		normalDigest, err = ledgerMigrationDigest(normal)
	default:
		return empty, ErrRecordLedgerFence
	}
	if err != nil || normalDigest != activation.NormalDigest {
		return empty, ErrRecordLedgerFence
	}
	var successor ledgerAppInventory
	if found, err := ledgerMigrationRead(control, "successor", intent.PhaseGeneration, &successor); err != nil || !found || !reflect.DeepEqual(successor, selected) {
		return empty, ErrRecordLedgerFence
	}
	digest, err := ledgerMigrationDigest(successor)
	if err != nil || digest != activation.SuccessorDigest || hexDigest(successor.ManifestDigest) != activation.ManifestDigest {
		return empty, ErrRecordLedgerFence
	}
	return successor, nil
}

// A terminal file is only a candidate. Admission additionally requires the
// live root to acknowledge every completed terminal and its retained prefix.
// An intent published before the phase transition remains pending.
type ledgerMigrationJournal struct {
	Begin      *ledgerMigrationBegin
	Intent     *ledgerMigrationIntent
	Activation *ledgerMigrationActivation
	Terminals  []ledgerMaintenancePhase
	Pending    bool
}

func ledgerMigrationLoadJournal(control int, application RecordLedgerApplicationIdentity, allowPending bool) (ledgerMigrationJournal, error) {
	var result ledgerMigrationJournal
	names, err := ledgerNames(control)
	if err != nil {
		return result, err
	}
	phases, err := ledgerReadMaintenancePhases(control)
	if err != nil {
		return result, err
	}
	begins := make(map[uint64]ledgerMigrationBegin)
	observations := make(map[uint64]map[uint64]map[string]bool)
	for _, name := range names {
		if strings.HasPrefix(name, "migration-begin-") && !strings.HasPrefix(name, "migration-begin-settled-") {
			generation, ok := ledgerMigrationNameGeneration("begin", name)
			var begin ledgerMigrationBegin
			found, err := ledgerMigrationRead(control, "begin", generation, &begin)
			if !ok || err != nil || !found || generation < 2 || generation > math.MaxInt64-2 || generation-1 > uint64(len(phases)) ||
				begin.Version != 1 || begin.Family != "migration" || begin.PhaseGeneration != generation || begin.Application != application || !fenceRID.MatchString(begin.RecoveryID) ||
				begin.Phase != phases[generation-2] || begin.Phase.Phase != ledgerPhaseQuiescent || begin.Phase.RecoveryID != begin.RecoveryID ||
				begin.Coordinator.PID <= 0 || begin.Coordinator.Starttime == 0 || begin.Guard.Dev == 0 || begin.Guard.Ino == 0 || begin.BootID == "" ||
				begin.Cgroup.Path != ledgerMigrationCgroup || begin.Cgroup.Dev == 0 || begin.Cgroup.Ino == 0 || begin.Primary.Validate() != nil || begin.Witness.Validate() != nil {
				return result, ErrRecordLedgerFence
			}
			for _, digest := range []string{begin.InventoryDigest, begin.SessionDigest, begin.OriginalInventoryDigest, begin.ManagementCredentialDigest, begin.Coordinator.ExecutableSHA256} {
				if _, ok := ledgerC80DigestBytes(digest); !ok {
					return result, ErrRecordLedgerFence
				}
			}
			if ledgerMigrationValidateBeginSession(control, begin) != nil {
				return result, ErrRecordLedgerFence
			}
			begins[generation] = begin
		}
		if strings.HasPrefix(name, "migration-observe-") {
			kind, generation, attempt, ok := ledgerMigrationObservationNameParts(name)
			if !ok {
				return result, ErrRecordLedgerFence
			}
			if observations[generation] == nil {
				observations[generation] = make(map[uint64]map[string]bool)
			}
			if observations[generation][attempt] == nil {
				observations[generation][attempt] = make(map[string]bool)
			}
			observations[generation][attempt][kind] = true
		}
	}
	known := map[uint64]bool{}
	for _, name := range names {
		if !strings.HasPrefix(name, "migration-intent-") {
			continue
		}
		generation, valid := ledgerMigrationNameGeneration("intent", name)
		if !valid || known[generation] || result.Pending || result.Activation != nil {
			return result, ErrRecordLedgerFence
		}
		known[generation] = true
		var intent ledgerMigrationIntent
		found, err := ledgerMigrationRead(control, "intent", generation, &intent)
		if err != nil || !found || intent.Version != 1 || generation < 2 || generation > math.MaxInt64-2 || intent.PhaseGeneration != generation || intent.PreviousPhaseGeneration+1 != generation ||
			intent.Application != application || !fenceRID.MatchString(intent.RecoveryID) || !intent.WorkerAbsent || intent.WorkerRole != composeDeletionWorkerRole ||
			intent.Cgroup.Path != ledgerMigrationCgroup || intent.Cgroup.Dev == 0 || intent.Cgroup.Ino == 0 || intent.Guard.Dev == 0 || intent.Guard.Ino == 0 ||
			intent.Coordinator.PID <= 0 || intent.Coordinator.Starttime == 0 || intent.Bootstrap.PID <= 0 || intent.Bootstrap.Started.IsZero() || intent.Bootstrap.Started.Nanosecond()%1000 != 0 || intent.Bootstrap.Role == "" ||
			intent.PreviousPhaseGeneration > uint64(len(phases)) || phases[intent.PreviousPhaseGeneration-1].Phase != ledgerPhaseQuiescent {
			return result, ErrRecordLedgerFence
		}
		begin, ok := begins[generation]
		if !ok || begin.RecoveryID != intent.RecoveryID || begin.Application != intent.Application || begin.Phase.Generation != intent.PreviousPhaseGeneration || begin.OriginalInventoryDigest != intent.OriginalInventoryDigest || begin.Coordinator != intent.Coordinator || begin.Guard != intent.Guard || begin.BootID != intent.BootID || begin.Cgroup != intent.Cgroup {
			return result, ErrRecordLedgerFence
		}
		beginCopy := begin
		result.Begin = &beginCopy
		if generation <= uint64(len(phases)) {
			phase := phases[generation-1]
			if phase.Phase != ledgerPhaseMigration || phase.RecoveryID != intent.RecoveryID || phase.Application != intent.Application {
				return result, ErrRecordLedgerFence
			}
		}
		if generation+1 <= uint64(len(phases)) {
			phase := phases[generation]
			if phase.Phase != ledgerPhaseRefencing || phase.RecoveryID != intent.RecoveryID || phase.Application != intent.Application {
				return result, ErrRecordLedgerFence
			}
		}
		for _, digest := range []string{intent.OriginalInventoryDigest, intent.Coordinator.ExecutableSHA256, intent.ProgramSHA256} {
			if _, ok := ledgerC80DigestBytes(digest); !ok {
				return result, ErrRecordLedgerFence
			}
		}
		for _, role := range []string{intent.RuntimeRole, intent.AdminRole, intent.MigratorRole, intent.WorkerRole, intent.Bootstrap.Role} {
			if !ledgerSafeRole(role) {
				return result, ErrRecordLedgerFence
			}
		}
		intentDigest, err := ledgerMigrationDigest(intent)
		if err != nil {
			return result, err
		}
		var activation ledgerMigrationActivation
		activated, err := ledgerMigrationRead(control, "activation", generation, &activation)
		if err != nil {
			return result, err
		}
		var abort ledgerMigrationAbort
		aborted, err := ledgerMigrationRead(control, "abort", generation, &abort)
		if err != nil || activated && aborted {
			return result, ErrRecordLedgerFence
		}
		_, compensationExists, err := fenceReadFile(control, ledgerMigrationName("compensate", generation))
		if err != nil || activated && compensationExists {
			return result, ErrRecordLedgerFence
		}
		present := make(map[string]bool, 6)
		for _, kind := range []string{"preparation", "ready", "normal", "successor", "restrict", "revocation-unresolved"} {
			_, exists, e := fenceReadFile(control, ledgerMigrationName(kind, generation))
			if e != nil {
				return result, ErrRecordLedgerFence
			}
			present[kind] = exists
		}
		attempts := observations[generation]
		observationPending, err := ledgerMigrationValidateObservationPrefix(control, intent, attempts)
		if err != nil || len(attempts) != 0 && (!present["ready"] || present["normal"] || generation+1 > uint64(len(phases))) ||
			present["ready"] && (!present["preparation"] || generation > uint64(len(phases))) ||
			present["normal"] && !present["ready"] || present["successor"] && !present["normal"] && len(attempts) == 0 ||
			present["successor"] && observationPending || activated && !present["successor"] || (compensationExists || present["successor"] || present["restrict"] || activated || aborted) && generation+1 > uint64(len(phases)) ||
			compensationExists && (present["normal"] || len(attempts) != 0) {
			return result, ErrRecordLedgerFence
		}
		var abandoned ledgerMigrationReadyAbandoned
		abandonedFound, err := ledgerMigrationRead(control, "ready-abandoned", generation, &abandoned)
		if err != nil {
			return result, ErrRecordLedgerFence
		}
		if abandonedFound {
			if !present["ready"] || !present["preparation"] || present["normal"] || present["successor"] || len(attempts) != 0 || activated {
				return result, ErrRecordLedgerFence
			}
			var preparation ledgerMigrationPreparation
			var ready ledgerMigrationReady
			preparationBody, prepFound, prepErr := fenceReadFile(control, ledgerMigrationName("preparation", generation))
			readyBody, readyFound, readyErr := fenceReadFile(control, ledgerMigrationName("ready", generation))
			original, originalErr := ledgerMigrationOriginalInventory(control, intent)
			if prepErr != nil || readyErr != nil || !prepFound || !readyFound || originalErr != nil || fenceDecode(preparationBody, &preparation) != nil || fenceDecode(readyBody, &ready) != nil || ledgerMigrationValidatePreparation(preparation, intent, original) != nil || ready.Version != 1 || ready.PhaseGeneration != generation || ready.IntentDigest != intentDigest || abandoned.Version != 1 || abandoned.PhaseGeneration != generation || abandoned.RecoveryID != intent.RecoveryID || abandoned.Application != intent.Application || abandoned.IntentDigest != intentDigest || abandoned.PreparationDigest != ledgerPredecessorHash(preparationBody) || abandoned.ReadyDigest != ledgerPredecessorHash(readyBody) || abandoned.Outcome != "unwitnessed_ready_c80" || abandoned.Coordinator.PID <= 0 || abandoned.Coordinator.Starttime == 0 || !ledgerMaintenanceBackendValid(abandoned.Bootstrap) || !reflect.DeepEqual(abandoned.Restricted, preparation.EmergencyTarget) {
				return result, ErrRecordLedgerFence
			}
			if abandoned.Coordinator.ExecutableSHA256 != intent.Coordinator.ExecutableSHA256 || abandoned.Bootstrap.Role != intent.Bootstrap.Role || abandoned.Coordinator.PID == intent.Coordinator.PID && abandoned.Coordinator.Starttime == intent.Coordinator.Starttime || abandoned.Bootstrap.PID == intent.Bootstrap.PID && abandoned.Bootstrap.Started.Equal(intent.Bootstrap.Started) {
				return result, ErrRecordLedgerFence
			}
			if ready.Child.PID <= 0 || ready.Child.Starttime == 0 || ready.Child.ExecutableSHA256 != intent.ProgramSHA256 || !ledgerMaintenanceBackendValid(ready.Backend) || ready.Backend.Role != intent.MigratorRole {
				return result, ErrRecordLedgerFence
			}
			if _, ok := ledgerC80DigestBytes(abandoned.CatalogDigest); !ok {
				return result, ErrRecordLedgerFence
			}
			if _, ok := ledgerC80DigestBytes(abandoned.Coordinator.ExecutableSHA256); !ok {
				return result, ErrRecordLedgerFence
			}
		}
		if present["successor"] && len(attempts) != 0 {
			var successor ledgerAppInventory
			if found, err := ledgerMigrationRead(control, "successor", generation, &successor); err != nil || !found {
				return result, ErrRecordLedgerFence
			}
			matches := 0
			for attempt := uint64(1); attempt <= uint64(len(attempts)); attempt++ {
				if !attempts[attempt]["normal"] {
					continue
				}
				var normal ledgerMigrationObservationNormal
				if found, err := ledgerMigrationReadObservation(control, "normal", generation, attempt, &normal); err != nil || !found {
					return result, ErrRecordLedgerFence
				}
				if reflect.DeepEqual(successor, normal.Inventory) {
					matches++
				}
			}
			if matches != 1 {
				return result, ErrRecordLedgerFence
			}
		}
		if aborted {
			if abort.Version != 1 || abort.PhaseGeneration != generation || abort.IntentDigest != intentDigest || abort.Outcome != "restored_c80" {
				return result, ErrRecordLedgerFence
			}
			var compensation ledgerMigrationCompensation
			found, err := ledgerMigrationRead(control, "compensate", generation, &compensation)
			compensationDigest, digestErr := ledgerMigrationDigest(compensation)
			if err != nil || !found || digestErr != nil || compensation.Version != 1 || compensation.PhaseGeneration != generation || compensation.IntentDigest != intentDigest || compensationDigest != abort.CompensationDigest {
				return result, ErrRecordLedgerFence
			}
			if _, ok := ledgerC80DigestBytes(compensation.TargetDigest); !ok {
				return result, ErrRecordLedgerFence
			}
			var preparation ledgerMigrationPreparation
			prepared, err := ledgerMigrationRead(control, "preparation", generation, &preparation)
			if err != nil {
				return result, err
			}
			if prepared {
				digest, e := ledgerMigrationDigest(preparation)
				worker, found := ledgerAppRoleByName(preparation.Normal, composeDeletionWorkerRole)
				if e != nil || !found || worker.OID == 0 || worker.OID != compensation.WorkerOID || digest != compensation.PreparationDigest {
					return result, ErrRecordLedgerFence
				}
			} else if compensation.WorkerOID != 0 || compensation.PreparationDigest != "" {
				return result, ErrRecordLedgerFence
			}
			for _, kind := range []string{"normal", "successor"} {
				_, exists, e := fenceReadFile(control, ledgerMigrationName(kind, generation))
				if e != nil || exists {
					return result, ErrRecordLedgerFence
				}
			}
			if generation+2 > uint64(len(phases)) {
				if !allowPending {
					return result, ErrRecordLedgerFence
				}
				copy := intent
				result.Intent, result.Pending = &copy, true
			} else {
				terminal := phases[generation+1]
				if terminal.Phase != ledgerPhaseQuiescent || terminal.RecoveryID != intent.RecoveryID || terminal.Application != intent.Application {
					return result, ErrRecordLedgerFence
				}
				result.Terminals = append(result.Terminals, terminal)
			}
			continue
		}
		copy := intent
		result.Intent = &copy
		if activated {
			if activation.Version != 1 || activation.PhaseGeneration != generation || activation.IntentDigest != intentDigest ||
				activation.Application != intent.Application || activation.RecoveryID != intent.RecoveryID || activation.OriginalInventoryDigest != intent.OriginalInventoryDigest ||
				activation.TerminalPhaseGeneration != generation+2 ||
				activation.NormalSource == "original" && (activation.NormalAttempt != 0 || !present["normal"] || len(attempts) != 0) ||
				activation.NormalSource == "observation" && (activation.NormalAttempt == 0 || !attempts[activation.NormalAttempt]["settled"]) ||
				activation.NormalSource != "original" && activation.NormalSource != "observation" {
				return result, ErrRecordLedgerFence
			}
			original, err := ledgerMigrationOriginalInventory(control, intent)
			if err != nil {
				return result, ErrRecordLedgerFence
			}
			if _, err := ledgerMigrationSelectedInventory(control, intent, activation, original); err != nil {
				return result, ErrRecordLedgerFence
			}
			if activation.TerminalPhaseGeneration > uint64(len(phases)) {
				if !allowPending {
					return result, ErrRecordLedgerFence
				}
				result.Pending = true
			} else {
				terminal := phases[activation.TerminalPhaseGeneration-1]
				if terminal.Phase != ledgerPhaseQuiescent || terminal.RecoveryID != intent.RecoveryID || terminal.Application != application {
					return result, ErrRecordLedgerFence
				}
				if _, ok := ledgerC80DigestBytes(activation.SuccessorDigest); !ok {
					return result, ErrRecordLedgerFence
				}
				if _, ok := ledgerC80DigestBytes(activation.NormalDigest); !ok {
					return result, ErrRecordLedgerFence
				}
				if _, ok := ledgerC80DigestBytes(activation.ManifestDigest); !ok {
					return result, ErrRecordLedgerFence
				}
				if result.Activation != nil {
					return result, ErrRecordLedgerFence
				}
				copy := activation
				result.Terminals = append(result.Terminals, terminal)
				result.Activation = &copy
			}
		} else {
			if result.Pending || result.Activation != nil {
				return result, ErrRecordLedgerFence
			}
			result.Pending = true
		}
	}
	for generation, begin := range begins {
		if known[generation] {
			continue
		}
		if result.Activation != nil && generation > result.Activation.PhaseGeneration {
			return result, ErrRecordLedgerFence
		}
		var settled ledgerMigrationBeginSettled
		found, err := ledgerMigrationRead(control, "begin-settled", generation, &settled)
		body, beginFound, readErr := fenceReadFile(control, ledgerMigrationName("begin", generation))
		if err != nil || readErr != nil || !beginFound || len(observations[generation]) != 0 {
			return result, ErrRecordLedgerFence
		}
		if found {
			if settled.Version != 1 || settled.PhaseGeneration != generation || settled.RecoveryID != begin.RecoveryID || settled.Application != begin.Application || settled.BeginDigest != ledgerPredecessorHash(body) || settled.Outcome != "no_effect_c80" || settled.Coordinator.PID <= 0 || settled.Coordinator.Starttime == 0 || !ledgerMaintenanceBackendValid(settled.Bootstrap) {
				return result, ErrRecordLedgerFence
			}
			for _, digest := range []string{settled.Coordinator.ExecutableSHA256, settled.CatalogDigest, settled.RolesDigest} {
				if _, ok := ledgerC80DigestBytes(digest); !ok {
					return result, ErrRecordLedgerFence
				}
			}
		}
		if generation <= uint64(len(phases)) {
			phase := phases[generation-1]
			if !found || phase.Phase != ledgerPhaseRefencing || phase.RecoveryID != begin.RecoveryID || phase.Application != begin.Application {
				return result, ErrRecordLedgerFence
			}
		}
		if generation+1 <= uint64(len(phases)) {
			terminal := phases[generation]
			if terminal.Phase != ledgerPhaseQuiescent || terminal.RecoveryID != begin.RecoveryID || terminal.Application != begin.Application {
				return result, ErrRecordLedgerFence
			}
			result.Terminals = append(result.Terminals, terminal)
			continue
		}
		if result.Pending || result.Activation != nil || generation+1 < uint64(len(phases)) {
			return result, ErrRecordLedgerFence
		}
		beginCopy := begin
		result.Begin, result.Intent, result.Pending = &beginCopy, nil, true
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "migration-") {
			continue
		}
		if kind, generation, _, ok := ledgerMigrationObservationNameParts(name); ok && kind != "" && known[generation] {
			continue
		}
		if generation, ok := ledgerMigrationNameGeneration("begin", name); ok {
			if _, exists := begins[generation]; exists {
				continue
			}
		}
		if generation, ok := ledgerMigrationNameGeneration("begin-settled", name); ok {
			if _, exists := begins[generation]; exists && !known[generation] {
				continue
			}
		}
		valid := false
		for _, kind := range []string{"intent", "preparation", "ready", "ready-abandoned", "normal", "successor", "activation", "restrict", "revocation-unresolved", "compensate", "abort"} {
			if generation, ok := ledgerMigrationNameGeneration(kind, name); ok && known[generation] {
				valid = true
				break
			}
		}
		if !valid {
			return result, ErrRecordLedgerFence
		}
	}
	sort.Slice(result.Terminals, func(i, j int) bool { return result.Terminals[i].Generation < result.Terminals[j].Generation })
	if result.Pending && !allowPending {
		return result, ErrRecordLedgerFence
	}
	return result, nil
}

func PrepareLegacyC81MigrationCoordinator() error {
	if os.Geteuid() != 10002 || os.Getegid() != 10002 || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}
