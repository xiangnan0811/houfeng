//go:build linux

package deploy

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMigrationBeginSettlementPrefixes(t *testing.T) {
	fd, app, intent, phase := migrationJournalFixture(t)
	body, found, err := fenceReadFile(fd, ledgerMigrationName("begin", intent.PhaseGeneration))
	if err != nil || !found {
		t.Fatal("missing begin", err)
	}
	settled := ledgerMigrationBeginSettled{Version: 1, PhaseGeneration: intent.PhaseGeneration, RecoveryID: intent.RecoveryID, Application: app, BeginDigest: ledgerPredecessorHash(body), Coordinator: intent.Coordinator, Bootstrap: intent.Bootstrap, CatalogDigest: strings.Repeat("b", 64), RolesDigest: strings.Repeat("c", 64), Outcome: "no_effect_c80"}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("begin-settled", intent.PhaseGeneration), settled); err != nil {
		t.Fatal(err)
	}
	journal, err := ledgerMigrationLoadJournal(fd, app, true)
	if err != nil || !journal.Pending || journal.Begin == nil || journal.Intent != nil {
		t.Fatalf("settlement before phase must remain pending: %v", err)
	}
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseRefencing)
	journal, err = ledgerMigrationLoadJournal(fd, app, true)
	if err != nil || !journal.Pending || journal.Begin == nil || journal.Intent != nil {
		t.Fatalf("refencing before terminal must remain pending: %v", err)
	}
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseQuiescent)
	journal, err = ledgerMigrationLoadJournal(fd, app, false)
	if err != nil || journal.Pending || len(journal.Terminals) != 1 || journal.Terminals[0] != phase {
		t.Fatalf("terminal must require live root authorization: %v", err)
	}
	if ledgerMigrationAuthorizeJournal(t.Context(), fd, journal, nil) == nil {
		t.Fatal("terminal admitted without root")
	}
	if err := ledgerMigrationAuthorizeJournal(t.Context(), fd, journal, &migrationJournalTestCheckpointer{}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationCancelledGenerationRetainsTerminalBeforeNextBegin(t *testing.T) {
	fd, app, intent, phase := migrationJournalFixture(t)
	var begin ledgerMigrationBegin
	found, err := ledgerMigrationRead(fd, "begin", intent.PhaseGeneration, &begin)
	if err != nil || !found {
		t.Fatal("missing begin", err)
	}
	body, _, err := fenceReadFile(fd, ledgerMigrationName("begin", intent.PhaseGeneration))
	if err != nil {
		t.Fatal(err)
	}
	settled := ledgerMigrationBeginSettled{Version: 1, PhaseGeneration: intent.PhaseGeneration, RecoveryID: intent.RecoveryID, Application: app, BeginDigest: ledgerPredecessorHash(body), Coordinator: intent.Coordinator, Bootstrap: intent.Bootstrap, CatalogDigest: strings.Repeat("b", 64), RolesDigest: strings.Repeat("c", 64), Outcome: "no_effect_c80"}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("begin-settled", intent.PhaseGeneration), settled); err != nil {
		t.Fatal(err)
	}
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseRefencing)
	terminal := migrationJournalNextPhase(t, fd, phase, ledgerPhaseQuiescent)
	begin.Phase = terminal
	begin.PhaseGeneration = terminal.Generation + 1
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("begin", begin.PhaseGeneration), begin); err != nil {
		t.Fatal(err)
	}
	journal, err := ledgerMigrationLoadJournal(fd, app, true)
	if err != nil || !journal.Pending || journal.Begin == nil || journal.Begin.PhaseGeneration != begin.PhaseGeneration || len(journal.Terminals) != 1 || journal.Terminals[0] != terminal {
		t.Fatalf("lost cancelled generation or pending successor: %v", err)
	}
}

func TestMigrationBeginRefencingRequiresSettlement(t *testing.T) {
	fd, app, _, phase := migrationJournalFixture(t)
	migrationJournalNextPhase(t, fd, phase, ledgerPhaseRefencing)
	if _, err := ledgerMigrationLoadJournal(fd, app, true); err == nil {
		t.Fatal("accepted refencing without a begin settlement")
	}
}

func TestMigrationBeginSettlementRejectsWrongPredecessor(t *testing.T) {
	fd, app, intent, _ := migrationJournalFixture(t)
	settled := ledgerMigrationBeginSettled{Version: 1, PhaseGeneration: intent.PhaseGeneration, RecoveryID: intent.RecoveryID, Application: app, BeginDigest: strings.Repeat("d", 64), Coordinator: intent.Coordinator, Bootstrap: intent.Bootstrap, CatalogDigest: strings.Repeat("b", 64), RolesDigest: strings.Repeat("c", 64), Outcome: "no_effect_c80"}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("begin-settled", intent.PhaseGeneration), settled); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerMigrationLoadJournal(fd, app, true); err == nil {
		t.Fatal("accepted settlement for different begin bytes")
	}
}

func TestMigrationReadyAbandonmentRequiresRetainedReady(t *testing.T) {
	fd, app, intent, phase := migrationJournalFixture(t)
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("intent", intent.PhaseGeneration), intent); err != nil {
		t.Fatal(err)
	}
	migrationJournalNextPhase(t, fd, phase, ledgerPhaseMigration)
	abandoned := ledgerMigrationReadyAbandoned{Version: 1, PhaseGeneration: intent.PhaseGeneration, RecoveryID: intent.RecoveryID, Application: app, Outcome: "unwitnessed_ready_c80"}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("ready-abandoned", intent.PhaseGeneration), abandoned); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerMigrationLoadJournal(fd, app, true); err == nil {
		t.Fatal("accepted abandonment without retained preparation and ready")
	}
}

func TestMigrationBeginCheckpointUsesRetainedBytes(t *testing.T) {
	fd, _, intent, _ := migrationJournalFixture(t)
	var begin ledgerMigrationBegin
	found, err := ledgerMigrationRead(fd, "begin", intent.PhaseGeneration, &begin)
	if err != nil || !found {
		t.Fatal("missing begin", err)
	}
	body, found, err := fenceReadFile(fd, ledgerMigrationName("begin", intent.PhaseGeneration))
	if err != nil || !found {
		t.Fatal("missing begin bytes", err)
	}
	checkpointer := &migrationJournalTestCheckpointer{}
	fence := &RecordLedgerFence{control: fd, maintenanceCheckpointer: checkpointer}
	if err := ledgerMigrationBeginCheckpoint(t.Context(), fence, begin, "begin", begin); err != nil {
		t.Fatal(err)
	}
	if len(checkpointer.requests) != 1 || checkpointer.requests[0].Kind != "begin" || checkpointer.requests[0].RecordSHA256 != ledgerPredecessorHash(body) || checkpointer.requests[0].CurrentBackend != nil {
		t.Fatal("begin replay did not bind the original bytes")
	}
}

func TestMaintenanceSettlementBackendStrictSchema(t *testing.T) {
	valid := `{"pid":42,"started":"2026-01-01T00:00:00Z","role":"postgres"}`
	if _, err := ledgerMaintenanceDecodeBackend([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{
		`{"pid":42,"pid":43,"started":"2026-01-01T00:00:00Z","role":"postgres"}`,
		`{"pid":42,"started":"2026-01-01T00:00:00Z","role":"postgres","extra":1}`,
		`{"pid":42,"started":"2026-01-01T00:00:00.000000001Z","role":"postgres"}`,
		`{"pid":42,"started":"2026-01-01T00:00:00Z","role":null}`,
	} {
		if _, err := ledgerMaintenanceDecodeBackend([]byte(malformed)); err == nil {
			t.Fatalf("accepted malformed backend %s", malformed)
		}
	}
}

func TestMigrationReadyDispositionBindsRetainedBytesAndWitness(t *testing.T) {
	fd, _, intent, _ := migrationJournalFixture(t)
	intentDigest, err := ledgerMigrationDigest(intent)
	if err != nil {
		t.Fatal(err)
	}
	ready := ledgerMigrationReady{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest,
		Child:   ledgerPredecessorProcess{PID: 31, Starttime: 40, ExecutableSHA256: intent.ProgramSHA256},
		Backend: ledgerPredecessorBackend{PID: 32, Started: intent.Bootstrap.Started, Role: intent.MigratorRole}}
	// Deliberately preserve whitespace that json.Marshal would not emit.
	body, err := json.MarshalIndent(ready, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	file, err := unix.Openat(fd, ledgerMigrationName("ready", intent.PhaseGeneration), unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = unix.Write(file, body); err != nil {
		unix.Close(file)
		t.Fatal(err)
	}
	if err = unix.Close(file); err != nil {
		t.Fatal(err)
	}
	witnessSequence := uint64(1)
	witnessDigest := strings.Repeat("f", 64)
	decision := ledgerMaintenanceReadyDisposition{Disposition: "witnessed", DecisionSequence: 2, DecisionDigest: strings.Repeat("a", 64), ReadyWitnessSequence: &witnessSequence, ReadyWitnessDigest: &witnessDigest}
	checkpointer := &migrationJournalTestCheckpointer{decision: decision}
	fence := &RecordLedgerFence{control: fd, maintenanceCheckpointer: checkpointer}
	got, err := ledgerMigrationReadyDisposition(t.Context(), fence, intent, body, false)
	if err != nil || got != "witnessed" || len(checkpointer.decisionRequests) != 1 || len(checkpointer.requests) != 1 {
		t.Fatalf("witnessed ready was not replayed: %q %v", got, err)
	}
	request := checkpointer.decisionRequests[0]
	if request.RecordSHA256 != ledgerPredecessorHash(body) || request.Kind != "ready" || request.Attempt != 0 || request.CurrentBackend != nil || checkpointer.requests[0] != request {
		t.Fatal("decision or witness replay lost the original record binding")
	}
	checkpointer.requests = nil
	if _, err := ledgerMigrationReadyDisposition(t.Context(), fence, intent, body, true); err == nil || len(checkpointer.requests) != 0 {
		t.Fatal("accepted a witnessed decision with an abandonment record")
	}
	checkpointer.ack = &ledgerMaintenanceACK{WitnessSequence: 3, WitnessDigest: witnessDigest}
	if _, err := ledgerMigrationReadyDisposition(t.Context(), fence, intent, body, false); err == nil {
		t.Fatal("accepted a ready ACK referring to a different witness")
	}
	checkpointer.ack = nil
	checkpointer.requests = nil
	checkpointer.decision = ledgerMaintenanceReadyDisposition{Disposition: "unwitnessed_retired", DecisionSequence: 2, DecisionDigest: strings.Repeat("a", 64)}
	got, err = ledgerMigrationReadyDisposition(t.Context(), fence, intent, body, false)
	if err != nil || got != "unwitnessed_retired" || len(checkpointer.requests) != 0 {
		t.Fatalf("unwitnessed ready incorrectly received a ready ACK: %q %v", got, err)
	}
	checkpointer.decision.ReadyWitnessSequence = &witnessSequence
	if _, err := ledgerMigrationReadyDisposition(t.Context(), fence, intent, body, false); err == nil {
		t.Fatal("accepted an unwitnessed decision with a witness reference")
	}
	checkpointer.decision = ledgerMaintenanceReadyDisposition{Disposition: "unknown", DecisionSequence: 2, DecisionDigest: strings.Repeat("a", 64)}
	if _, err := ledgerMigrationReadyDisposition(t.Context(), fence, intent, body, false); err == nil {
		t.Fatal("accepted an unknown decision")
	}
	checkpointer.requests = nil
	checkpointer.err = errors.New("transport closed")
	if _, err := ledgerMigrationReadyDisposition(t.Context(), fence, intent, body, false); err == nil || len(checkpointer.requests) != 0 {
		t.Fatal("transport failure selected a ready branch")
	}
}
