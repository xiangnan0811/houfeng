//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/store/migrate"
)

// This test double only exercises Go admission; it is not root acceptance.
type migrationJournalTestCheckpointer struct {
	requests         []ledgerMaintenanceCheckpoint
	err              error
	decision         ledgerMaintenanceReadyDisposition
	decisionRequests []ledgerMaintenanceCheckpoint
	ack              *ledgerMaintenanceACK
}

func (c *migrationJournalTestCheckpointer) Checkpoint(_ context.Context, request ledgerMaintenanceCheckpoint) (ledgerMaintenanceACK, error) {
	c.requests = append(c.requests, request)
	if c.err != nil {
		return ledgerMaintenanceACK{}, c.err
	}
	if c.ack != nil {
		return *c.ack, nil
	}
	return ledgerMaintenanceACK{WitnessSequence: 1, WitnessDigest: strings.Repeat("f", 64)}, nil
}

func (c *migrationJournalTestCheckpointer) ReadyDisposition(_ context.Context, request ledgerMaintenanceCheckpoint) (ledgerMaintenanceReadyDisposition, error) {
	c.decisionRequests = append(c.decisionRequests, request)
	return c.decision, c.err
}

func (c *migrationJournalTestCheckpointer) Healthy() bool { return c.err == nil }

func migrationJournalOriginal(t *testing.T, app RecordLedgerApplicationIdentity) ledgerAppInventory {
	t.Helper()
	body, err := migrate.CanonicalPrivilegeSetBodyV1([]migrate.AppACLRoleBinding{
		{Subject: migrate.AppACLSubjectCenterRuntime, CatalogRole: "runtime"},
		{Subject: migrate.AppACLSubjectPlatformAdmin, CatalogRole: "admin"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldDigest := sha256.Sum256([]byte("c80 manifest"))
	manifest := migrate.AppACLManifestPersistedV1{MigratorCatalogRole: "migrator", CanonicalPrivilegeSet: body, ManifestDigest: oldDigest}
	original := ledgerAppInventory{Version: 2, RecoveryID: "rid-1", Application: app, Bootstrap: "postgres", LegacyC80: true, Admin: "admin", Previous: []string{"migrator", "runtime"}, ManifestDigest: oldDigest}
	original.Baseline = migrate.AppACLMaintenanceSnapshot{LegacyC80: true, Manifest: migrate.AppACLManifestRuntimeSnapshotV1{DatabaseName: app.DatabaseName, SessionUser: "postgres", CurrentUser: "postgres", Manifests: []migrate.AppACLManifestPersistedV1{manifest}}}
	original.RoleBaseline = ledgerAppRoleSnapshot{DatabaseOID: app.DatabaseOID, DatabaseOwnerOID: 1, Memberships: []ledgerAppRoleMembership{}, Settings: []ledgerAppRoleSetting{}}
	for i, name := range []string{"admin", "migrator", "postgres", "runtime"} {
		verifier := ledgerSCRAM("baseline-"+name, []byte("0123456789abcdef"), 4096)
		original.RoleBaseline.Roles = append(original.RoleBaseline.Roles, ledgerAppRole{Name: name, OID: uint32(i + 2), Login: true, Connect: true, ConnLimit: -1, Verifier: &verifier})
		if name != "postgres" {
			original.RoleBaseline.DatabaseACL = append(original.RoleBaseline.DatabaseACL, ledgerAppDatabaseACL{GrantorOID: 1, GranteeOID: uint32(i + 2), Privilege: "CONNECT"})
		}
	}
	for i, name := range []string{"admin", "migrator", "runtime"} {
		secret := strings.Repeat(string(rune('a'+i)), 32)
		original.Targets = append(original.Targets, ledgerAppRoleTarget{Role: name, Secret: secret, Verifier: ledgerSCRAM(secret, []byte("0123456789abcdef"), 4096)})
	}
	admin, _ := ledgerAppRoleByName(original.RoleBaseline, "admin")
	original.Credential = ledgerAdminCredential{Version: 1, RecoveryID: original.RecoveryID, Role: original.Admin, NewURL: "postgres://admin@localhost/houfeng", PreviousVerifier: *admin.Verifier, NewVerifier: original.Targets[0].Verifier}
	return original
}

func migrationJournalFixture(t *testing.T) (int, RecordLedgerApplicationIdentity, ledgerMigrationIntent, ledgerMaintenancePhase) {
	t.Helper()
	fd, err := unix.Open(t.TempDir(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	app := RecordLedgerApplicationIdentity{ID: "app", SystemIdentifier: "101", DatabaseName: "houfeng", DatabaseOID: 123, Epoch: 1}
	phase := ledgerMaintenancePhase{Version: 1, Generation: 1, RecoveryID: "rid-1", Application: app, Phase: ledgerPhaseQuiescent}
	if _, err := ledgerPublishExact(fd, "phase-00000000000000000001.json", phase); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	guard := ledgerPredecessorGuard{Dev: 1, Ino: 2}
	primary := recorddeletion.LedgerPhysicalIdentity{ID: "rd-" + strings.Repeat("1", 64), Kind: "deletion_ledger", SystemIdentifier: "102", DatabaseName: "ledger", DatabaseOID: 124, Epoch: 1}
	witness := recorddeletion.LedgerPhysicalIdentity{ID: "rd-" + strings.Repeat("2", 64), Kind: "deletion_witness", SystemIdentifier: "103", DatabaseName: "witness", DatabaseOID: 125, Epoch: 1}
	session, err := ledgerSessionDir(fd, phase.RecoveryID)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(session)
	original := migrationJournalOriginal(t, app)
	sessionRecord := ledgerFenceJournal{Version: 1, Compose: ComposeOfflineFenceRequest{RecoveryID: phase.RecoveryID}, Guard: MaintenanceGuardIdentity{FileDevice: guard.Dev, FileInode: guard.Ino}, Application: app, AppAdminRole: original.Admin, AppPreviousLogins: original.Previous, Primary: primary, Witness: witness}
	for name, value := range map[string]any{"session.json": sessionRecord, "app-inventory.json": original, "management-credential.json": original.Credential} {
		if _, err := ledgerPublishExact(session, name, value); err != nil {
			t.Fatal(err)
		}
	}
	sessionBytes, _ := json.Marshal(sessionRecord)
	objectBytes, _ := json.Marshal(original)
	credentialBytes, _ := json.Marshal(original.Credential)
	intent := ledgerMigrationIntent{Version: 1, PhaseGeneration: 2, PreviousPhaseGeneration: 1, RecoveryID: phase.RecoveryID, Application: app,
		OriginalInventoryDigest: ledgerPredecessorHash(objectBytes), Guard: guard, BootID: "00000000-0000-0000-0000-000000000001",
		Cgroup: ledgerPredecessorGroup{Path: ledgerMigrationCgroup, Dev: 3, Ino: 4}, Coordinator: ledgerPredecessorProcess{PID: 10, Starttime: 20, ExecutableSHA256: digest},
		ProgramSHA256: digest, RuntimeRole: "runtime", AdminRole: "admin", MigratorRole: "migrator", WorkerRole: composeDeletionWorkerRole, WorkerAbsent: true,
		Bootstrap: ledgerPredecessorBackend{PID: 30, Started: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Role: "postgres"}}
	begin := ledgerMigrationBegin{Version: 1, Family: "migration", RecoveryID: intent.RecoveryID, Application: app, PhaseGeneration: intent.PhaseGeneration,
		Phase: phase, InventoryDigest: digest, SessionDigest: ledgerPredecessorHash(sessionBytes), OriginalInventoryDigest: intent.OriginalInventoryDigest,
		ManagementCredentialDigest: ledgerPredecessorHash(credentialBytes), Primary: primary, Witness: witness, Coordinator: intent.Coordinator, Guard: guard, BootID: intent.BootID, Cgroup: intent.Cgroup}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("begin", intent.PhaseGeneration), begin); err != nil {
		t.Fatal(err)
	}
	return fd, app, intent, phase
}

func migrationJournalNextPhase(t *testing.T, fd int, previous ledgerMaintenancePhase, name string) ledgerMaintenancePhase {
	t.Helper()
	body, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	next := ledgerMaintenancePhase{Version: 1, Generation: previous.Generation + 1, Previous: sha256.Sum256(body), RecoveryID: previous.RecoveryID, Application: previous.Application, Phase: name}
	if _, err := ledgerPublishExact(fd, fmt.Sprintf("phase-%020d.json", next.Generation), next); err != nil {
		t.Fatal(err)
	}
	return next
}

func TestMigrationBeginBlocksAdmissionAndRetainsRecoveryBinding(t *testing.T) {
	fd, app, intent, _ := migrationJournalFixture(t)
	if _, err := ledgerMigrationLoadJournal(fd, app, false); err == nil {
		t.Fatal("generic admission ignored the durable begin")
	}
	journal, err := ledgerMigrationLoadJournal(fd, app, true)
	if err != nil || !journal.Pending || journal.Intent != nil || journal.Begin == nil || journal.Begin.RecoveryID != intent.RecoveryID || journal.Begin.PhaseGeneration != intent.PhaseGeneration {
		t.Fatalf("begin-only recovery binding was lost: %v", err)
	}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("intent", intent.PhaseGeneration), intent); err != nil {
		t.Fatal(err)
	}
	if err := unix.Unlinkat(fd, ledgerMigrationName("intent", intent.PhaseGeneration), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerMigrationLoadJournal(fd, app, false); err == nil {
		t.Fatal("deleted intent allowed admission despite the begin record")
	}
}

func TestMigrationIntentRequiresBeginAuthority(t *testing.T) {
	fd, app, intent, _ := migrationJournalFixture(t)
	if err := unix.Unlinkat(fd, ledgerMigrationName("begin", intent.PhaseGeneration), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("intent", intent.PhaseGeneration), intent); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerMigrationLoadJournal(fd, app, true); err == nil {
		t.Fatal("recovery accepted an intent without a begin record")
	}
}

func TestMigrationIntentBlocksGenericAdmissionBeforePhaseChange(t *testing.T) {
	fd, app, intent, _ := migrationJournalFixture(t)
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("intent", intent.PhaseGeneration), intent); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerMigrationLoadJournal(fd, app, false); err == nil {
		t.Fatal("generic admission ignored the durable intent")
	}
	journal, err := ledgerMigrationLoadJournal(fd, app, true)
	if err != nil || !journal.Pending || journal.Intent == nil || *journal.Intent != intent {
		t.Fatalf("recovery did not retain the original intent: %v", err)
	}
}

func TestMigrationCompensationNeedsTerminalPhase(t *testing.T) {
	fd, app, intent, phase := migrationJournalFixture(t)
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("intent", intent.PhaseGeneration), intent); err != nil {
		t.Fatal(err)
	}
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseMigration)
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseRefencing)
	intentDigest, err := ledgerMigrationDigest(intent)
	if err != nil {
		t.Fatal(err)
	}
	compensation := ledgerMigrationCompensation{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, TargetDigest: strings.Repeat("b", 64)}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("compensate", intent.PhaseGeneration), compensation); err != nil {
		t.Fatal(err)
	}
	compensationDigest, err := ledgerMigrationDigest(compensation)
	if err != nil {
		t.Fatal(err)
	}
	abort := ledgerMigrationAbort{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, CompensationDigest: compensationDigest, Outcome: "restored_c80"}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("abort", intent.PhaseGeneration), abort); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerMigrationLoadJournal(fd, app, false); err == nil {
		t.Fatal("unfinished compensation was admitted")
	}
	_ = migrationJournalNextPhase(t, fd, phase, ledgerPhaseQuiescent)
	journal, err := ledgerMigrationLoadJournal(fd, app, false)
	if err != nil || journal.Pending || journal.Activation != nil {
		t.Fatalf("complete compensation was not classified: %v", err)
	}
	if err := ledgerMigrationAuthorizeJournal(t.Context(), fd, journal, nil); err == nil {
		t.Fatal("complete compensation was admitted without a terminal witness")
	}
	if err := ledgerMigrationAuthorizeJournal(t.Context(), fd, journal, &migrationJournalTestCheckpointer{}); err != nil {
		t.Fatalf("witnessed compensation was rejected: %v", err)
	}
}

// This fixture exercises the protected journal loader; it does not stand in
// for the PostgreSQL catalog or root-witness acceptance tests.
func migrationJournalObservationFixture(t *testing.T) (int, RecordLedgerApplicationIdentity, ledgerMigrationIntent, ledgerMigrationActivation, ledgerAppInventory) {
	t.Helper()
	fd, app, intent, phase := migrationJournalFixture(t)
	original := migrationJournalOriginal(t, app)
	intentDigest, err := ledgerMigrationDigest(intent)
	if err != nil {
		t.Fatal(err)
	}
	workerSecret := strings.Repeat("w", 32)
	workerTarget := ledgerAppRoleTarget{Role: composeDeletionWorkerRole, Secret: workerSecret, Verifier: ledgerSCRAM(workerSecret, []byte("0123456789abcdef"), 4096)}
	prepared := original.RoleBaseline
	prepared.Roles = append([]ledgerAppRole(nil), prepared.Roles...)
	for i := range prepared.Roles {
		if prepared.Roles[i].Name == original.Bootstrap {
			continue
		}
		v := ledgerSCRAM("prepared-"+prepared.Roles[i].Name, []byte("0123456789abcdef"), 4096)
		prepared.Roles[i].Verifier = &v
	}
	workerVerifier := ledgerSCRAM("prepared-worker", []byte("0123456789abcdef"), 4096)
	prepared.Roles = append(prepared.Roles, ledgerAppRole{Name: composeDeletionWorkerRole, OID: 99, Login: true, ConnLimit: -1, Verifier: &workerVerifier})
	sort.Slice(prepared.Roles, func(i, j int) bool { return prepared.Roles[i].Name < prepared.Roles[j].Name })
	preparation := ledgerMigrationPreparation{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, From: ledgerAppExpectedRoles(original, ledgerAppTarget), Normal: prepared, WorkerTarget: workerTarget, Bootstrap: intent.Bootstrap, TransactionID: "42"}
	preparation.FinalTarget = ledgerAppExpectedRoles(ledgerMigrationRestrictionInventory(original, preparation, prepared), ledgerAppTarget)
	preparation.EmergencyTarget = ledgerMigrationEmergencyTarget(preparation.FinalTarget, original.Admin)
	if err := ledgerMigrationValidatePreparation(preparation, intent, original); err != nil {
		t.Fatalf("invalid preparation fixture: %v", err)
	}
	preparationDigest, err := ledgerMigrationDigest(preparation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("intent", intent.PhaseGeneration), intent); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("preparation", intent.PhaseGeneration), preparation); err != nil {
		t.Fatal(err)
	}
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseMigration)
	ready := ledgerMigrationReady{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, Child: ledgerPredecessorProcess{PID: 31, Starttime: 40, ExecutableSHA256: intent.ProgramSHA256}, Backend: ledgerPredecessorBackend{PID: 32, Started: intent.Bootstrap.Started, Role: intent.MigratorRole}}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("ready", intent.PhaseGeneration), ready); err != nil {
		t.Fatal(err)
	}
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseRefencing)
	observed := prepared
	observed.Roles = append([]ledgerAppRole(nil), prepared.Roles...)
	for i := range observed.Roles {
		if observed.Roles[i].Name == original.Bootstrap {
			continue
		}
		v := ledgerSCRAM("observation-"+observed.Roles[i].Name, []byte("0123456789abcdef"), 4096)
		observed.Roles[i].Verifier = &v
		if observed.Roles[i].Name == composeDeletionWorkerRole {
			observed.Roles[i].Connect = true
		}
	}
	observed.DatabaseACL = append(append([]ledgerAppDatabaseACL{}, prepared.DatabaseACL...), ledgerAppDatabaseACL{GrantorOID: prepared.DatabaseOwnerOID, GranteeOID: 99, Privilege: "CONNECT"})
	nextDigest := sha256.Sum256([]byte("c81 manifest"))
	inv := original
	inv.LegacyC80 = false
	inv.OriginalPrevious = append([]string{}, original.Previous...)
	inv.Previous = append(append([]string{}, original.Previous...), composeDeletionWorkerRole)
	sort.Strings(inv.Previous)
	inv.Targets = append(append([]ledgerAppRoleTarget{}, original.Targets...), workerTarget)
	sort.Slice(inv.Targets, func(i, j int) bool { return inv.Targets[i].Role < inv.Targets[j].Role })
	inv.ManifestDigest = nextDigest
	inv.Baseline.LegacyC80 = false
	inv.Baseline.Manifest.Manifests = append(append([]migrate.AppACLManifestPersistedV1{}, original.Baseline.Manifest.Manifests...), migrate.AppACLManifestPersistedV1{PreviousManifestDigest: original.ManifestDigest, ManifestDigest: nextDigest, MigratorCatalogRole: intent.MigratorRole})
	inv.Baseline.Manifest.Head = &migrate.AppACLManifestHeadV1{ManifestDigest: nextDigest}
	inv.RoleBaseline = observed
	catalogDigest, err := ledgerMigrationDigest(ledgerAppExpectedCatalog(inv, ledgerAppTarget))
	if err != nil {
		t.Fatal(err)
	}
	observation := ledgerMigrationObservationIntent{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: 1, RecoveryID: intent.RecoveryID, Application: app, IntentDigest: intentDigest, PreparationDigest: preparationDigest, Bootstrap: intent.Bootstrap, WorkerOID: 99, ManifestDigest: hexDigest(nextDigest), CatalogDigest: catalogDigest, From: preparation.FinalTarget, To: observed, FinalTarget: preparation.FinalTarget, EmergencyTarget: preparation.EmergencyTarget}
	observationDigest, err := ledgerMigrationDigest(observation)
	if err != nil {
		t.Fatal(err)
	}
	normal := ledgerMigrationObservationNormal{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: 1, IntentDigest: intentDigest, ObservationIntentDigest: observationDigest, Inventory: inv}
	if err := ledgerMigrationValidateObservationNormal(normal, observation, intent, preparation, original); err != nil {
		t.Fatalf("invalid observation fixture: %v", err)
	}
	normalDigest, err := ledgerMigrationDigest(normal)
	if err != nil {
		t.Fatal(err)
	}
	restriction := ledgerMigrationObservationRestriction{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: 1, IntentDigest: intentDigest, ObservationIntentDigest: observationDigest, From: observed, To: preparation.FinalTarget}
	restrictionDigest, err := ledgerMigrationDigest(restriction)
	if err != nil {
		t.Fatal(err)
	}
	settled := ledgerMigrationObservationSettled{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: 1, IntentDigest: intentDigest, ObservationIntentDigest: observationDigest, RestrictionDigest: restrictionDigest, Outcome: "normal_observed_refenced", NormalDigest: &normalDigest}
	for kind, value := range map[string]any{"intent": observation, "normal": normal, "restrict": restriction, "settled": settled} {
		if _, err := ledgerPublishExact(fd, ledgerMigrationObservationName(kind, intent.PhaseGeneration, 1), value); err != nil {
			t.Fatal(err)
		}
	}
	// A later complete observation exists, but activation below deliberately
	// selects the first one. Directory order must never select the successor.
	settledDigest, err := ledgerMigrationDigest(settled)
	if err != nil {
		t.Fatal(err)
	}
	second := observation
	second.Attempt = 2
	second.PreviousAttemptSettledDigest = &settledDigest
	second.To.Roles = append([]ledgerAppRole(nil), observed.Roles...)
	for i := range second.To.Roles {
		if second.To.Roles[i].Name == original.Bootstrap {
			continue
		}
		v := ledgerSCRAM("second-"+second.To.Roles[i].Name, []byte("0123456789abcdef"), 4096)
		second.To.Roles[i].Verifier = &v
	}
	secondDigest, err := ledgerMigrationDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	secondInventory := inv
	secondInventory.RoleBaseline = second.To
	secondNormal := ledgerMigrationObservationNormal{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: 2, IntentDigest: intentDigest, ObservationIntentDigest: secondDigest, Inventory: secondInventory}
	if err := ledgerMigrationValidateObservationNormal(secondNormal, second, intent, preparation, original); err != nil {
		t.Fatalf("invalid second observation fixture: %v", err)
	}
	secondNormalDigest, err := ledgerMigrationDigest(secondNormal)
	if err != nil {
		t.Fatal(err)
	}
	secondRestriction := ledgerMigrationObservationRestriction{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: 2, IntentDigest: intentDigest, ObservationIntentDigest: secondDigest, From: second.To, To: preparation.FinalTarget}
	secondRestrictionDigest, err := ledgerMigrationDigest(secondRestriction)
	if err != nil {
		t.Fatal(err)
	}
	secondSettled := ledgerMigrationObservationSettled{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: 2, IntentDigest: intentDigest, ObservationIntentDigest: secondDigest, RestrictionDigest: secondRestrictionDigest, Outcome: "normal_observed_refenced", NormalDigest: &secondNormalDigest}
	for kind, value := range map[string]any{"intent": second, "normal": secondNormal, "restrict": secondRestriction, "settled": secondSettled} {
		if _, err := ledgerPublishExact(fd, ledgerMigrationObservationName(kind, intent.PhaseGeneration, 2), value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("successor", intent.PhaseGeneration), inv); err != nil {
		t.Fatal(err)
	}
	successorDigest, err := ledgerMigrationDigest(inv)
	if err != nil {
		t.Fatal(err)
	}
	activation := ledgerMigrationActivation{Version: 1, PhaseGeneration: intent.PhaseGeneration, TerminalPhaseGeneration: intent.PhaseGeneration + 2, RecoveryID: intent.RecoveryID, Application: app, IntentDigest: intentDigest, OriginalInventoryDigest: intent.OriginalInventoryDigest, SuccessorDigest: successorDigest, NormalDigest: normalDigest, NormalSource: "observation", NormalAttempt: 1, ManifestDigest: hexDigest(nextDigest)}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("activation", intent.PhaseGeneration), activation); err != nil {
		t.Fatal(err)
	}
	_ = migrationJournalNextPhase(t, fd, phase, ledgerPhaseQuiescent)
	return fd, app, intent, activation, inv
}

func TestMigrationActivatedInventorySelectsSettledObservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, fd int, intent ledgerMigrationIntent, activation *ledgerMigrationActivation)
	}{
		{name: "different settled attempt", change: func(t *testing.T, fd int, intent ledgerMigrationIntent, activation *ledgerMigrationActivation) {
			var normal ledgerMigrationObservationNormal
			if found, err := ledgerMigrationReadObservation(fd, "normal", intent.PhaseGeneration, 2, &normal); err != nil || !found {
				t.Fatalf("read second normal: %v", err)
			}
			digest, err := ledgerMigrationDigest(normal)
			if err != nil {
				t.Fatal(err)
			}
			activation.NormalAttempt, activation.NormalDigest = 2, digest
		}},
		{name: "observation attempt zero", change: func(_ *testing.T, _ int, _ ledgerMigrationIntent, activation *ledgerMigrationActivation) {
			activation.NormalAttempt = 0
		}},
		{name: "original source with observation attempt", change: func(_ *testing.T, _ int, _ ledgerMigrationIntent, activation *ledgerMigrationActivation) {
			activation.NormalSource = "original"
		}},
		{name: "different normal digest", change: func(_ *testing.T, _ int, _ ledgerMigrationIntent, activation *ledgerMigrationActivation) {
			activation.NormalDigest = strings.Repeat("b", 64)
		}},
		{name: "different successor digest", change: func(_ *testing.T, _ int, _ ledgerMigrationIntent, activation *ledgerMigrationActivation) {
			activation.SuccessorDigest = strings.Repeat("c", 64)
		}},
		{name: "missing settlement", change: func(t *testing.T, fd int, intent ledgerMigrationIntent, _ *ledgerMigrationActivation) {
			if err := unix.Unlinkat(fd, ledgerMigrationObservationName("settled", intent.PhaseGeneration, 1), 0); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing terminal phase", change: func(t *testing.T, fd int, intent ledgerMigrationIntent, _ *ledgerMigrationActivation) {
			if err := unix.Unlinkat(fd, fmt.Sprintf("phase-%020d.json", intent.PhaseGeneration+2), 0); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fd, app, intent, activation, successor := migrationJournalObservationFixture(t)
			request := RecordLedgerMaintenanceRequest{AppIdentity: app, Compose: ComposeOfflineFenceRequest{RecoveryID: intent.RecoveryID}}
			checkpointer := &migrationJournalTestCheckpointer{}
			got, active, err := ledgerMigrationActivatedInventory(t.Context(), fd, request, checkpointer)
			if err != nil || !active || got.ManifestDigest != successor.ManifestDigest {
				t.Fatalf("settled observation was not admitted: active=%v err=%v", active, err)
			}
			tc.change(t, fd, intent, &activation)
			name := ledgerMigrationName("activation", intent.PhaseGeneration)
			if err := unix.Unlinkat(fd, name, 0); err != nil {
				t.Fatal(err)
			}
			if _, err := ledgerPublishExact(fd, name, activation); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ledgerMigrationActivatedInventory(t.Context(), fd, request, checkpointer); err == nil {
				t.Fatal("admission accepted an invalid terminal normal selection")
			}
		})
	}
}

func TestMigrationTerminalRequiresRootAcknowledgementOnReentry(t *testing.T) {
	fd, app, intent, _, successor := migrationJournalObservationFixture(t)
	request := RecordLedgerMaintenanceRequest{AppIdentity: app, Compose: ComposeOfflineFenceRequest{RecoveryID: intent.RecoveryID}}

	// The terminal file is already durable, but its ACK was never received.
	if _, _, err := ledgerMigrationActivatedInventory(t.Context(), fd, request, nil); err == nil {
		t.Fatal("admitted a durable terminal without a root channel")
	}
	lost := &migrationJournalTestCheckpointer{err: ErrRecordLedgerFence}
	if _, _, err := ledgerMigrationActivatedInventory(t.Context(), fd, request, lost); err == nil {
		t.Fatal("admitted a terminal whose acknowledgement was lost")
	}
	if len(lost.requests) != 1 {
		t.Fatalf("got %d terminal requests, want one", len(lost.requests))
	}
	accepted := &migrationJournalTestCheckpointer{}
	got, active, err := ledgerMigrationActivatedInventory(t.Context(), fd, request, accepted)
	if err != nil || !active || got.ManifestDigest != successor.ManifestDigest {
		t.Fatalf("exact retry was not admitted: active=%v err=%v", active, err)
	}
	if len(accepted.requests) != 1 || accepted.requests[0] != lost.requests[0] {
		t.Fatal("reentry did not retry the exact terminal binding")
	}
	session, err := ledgerSessionDir(fd, intent.RecoveryID)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(session)
	if err := ledgerMigrationVerifyInventory(t.Context(), fd, session, request, successor, nil); err == nil {
		t.Fatal("inventory verification admitted a terminal without root")
	}
	if err := ledgerMigrationVerifyInventory(t.Context(), fd, session, request, successor, &migrationJournalTestCheckpointer{}); err != nil {
		t.Fatalf("witnessed inventory verification failed: %v", err)
	}
	terminal, found, err := fenceReadFile(fd, fmt.Sprintf("phase-%020d.json", intent.PhaseGeneration+2))
	if err != nil || !found {
		t.Fatalf("read terminal: %v", err)
	}
	checkpoint := accepted.requests[0]
	if checkpoint.Family != "migration" || checkpoint.Kind != "terminal" || checkpoint.PhaseGeneration != intent.PhaseGeneration+2 || checkpoint.Attempt != 0 || checkpoint.RecoveryID != intent.RecoveryID || checkpoint.RecordSHA256 != ledgerPredecessorHash(terminal) {
		t.Fatalf("wrong terminal request: %+v", checkpoint)
	}
}

func TestMigrationInventoryWithoutJournalNeedsNoRootChannel(t *testing.T) {
	fd, err := unix.Open(t.TempDir(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	request := RecordLedgerMaintenanceRequest{AppIdentity: RecordLedgerApplicationIdentity{ID: "app"}}
	_, active, err := ledgerMigrationActivatedInventory(t.Context(), fd, request, nil)
	if err != nil || active {
		t.Fatalf("ordinary inventory unexpectedly required root: active=%v err=%v", active, err)
	}
}

func TestMigrationAbandonmentCompensationPrefixes(t *testing.T) {
	fd, app, intent, phase := migrationJournalFixture(t)
	original := migrationJournalOriginal(t, app)
	intentDigest, err := ledgerMigrationDigest(intent)
	if err != nil {
		t.Fatal(err)
	}
	workerSecret := strings.Repeat("w", 32)
	workerTarget := ledgerAppRoleTarget{Role: composeDeletionWorkerRole, Secret: workerSecret, Verifier: ledgerSCRAM(workerSecret, []byte("0123456789abcdef"), 4096)}
	prepared := original.RoleBaseline
	prepared.Roles = append([]ledgerAppRole(nil), prepared.Roles...)
	for i := range prepared.Roles {
		if prepared.Roles[i].Name != original.Bootstrap {
			verifier := ledgerSCRAM("prepared-"+prepared.Roles[i].Name, []byte("0123456789abcdef"), 4096)
			prepared.Roles[i].Verifier = &verifier
		}
	}
	workerVerifier := ledgerSCRAM("prepared-worker", []byte("0123456789abcdef"), 4096)
	prepared.Roles = append(prepared.Roles, ledgerAppRole{Name: composeDeletionWorkerRole, OID: 99, Login: true, ConnLimit: -1, Verifier: &workerVerifier})
	sort.Slice(prepared.Roles, func(i, j int) bool { return prepared.Roles[i].Name < prepared.Roles[j].Name })
	preparation := ledgerMigrationPreparation{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, From: ledgerAppExpectedRoles(original, ledgerAppTarget), Normal: prepared, WorkerTarget: workerTarget, Bootstrap: intent.Bootstrap, TransactionID: "42"}
	preparation.FinalTarget = ledgerAppExpectedRoles(ledgerMigrationRestrictionInventory(original, preparation, prepared), ledgerAppTarget)
	preparation.EmergencyTarget = ledgerMigrationEmergencyTarget(preparation.FinalTarget, original.Admin)
	if err := ledgerMigrationValidatePreparation(preparation, intent, original); err != nil {
		t.Fatal(err)
	}
	ready := ledgerMigrationReady{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, Child: ledgerPredecessorProcess{PID: 31, Starttime: 40, ExecutableSHA256: intent.ProgramSHA256}, Backend: ledgerPredecessorBackend{PID: 32, Started: intent.Bootstrap.Started, Role: intent.MigratorRole}}
	for kind, value := range map[string]any{"intent": intent, "preparation": preparation, "ready": ready} {
		if _, err := ledgerPublishExact(fd, ledgerMigrationName(kind, intent.PhaseGeneration), value); err != nil {
			t.Fatal(err)
		}
	}
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseMigration)
	preparationBody, _, _ := fenceReadFile(fd, ledgerMigrationName("preparation", intent.PhaseGeneration))
	readyBody, _, _ := fenceReadFile(fd, ledgerMigrationName("ready", intent.PhaseGeneration))
	abandoned := ledgerMigrationReadyAbandoned{Version: 1, PhaseGeneration: intent.PhaseGeneration, RecoveryID: intent.RecoveryID, Application: app, IntentDigest: intentDigest,
		PreparationDigest: ledgerPredecessorHash(preparationBody), ReadyDigest: ledgerPredecessorHash(readyBody), Coordinator: intent.Coordinator, Bootstrap: intent.Bootstrap,
		Restricted: preparation.EmergencyTarget, CatalogDigest: strings.Repeat("c", 64), Outcome: "unwitnessed_ready_c80"}
	abandoned.Coordinator.PID++
	abandoned.Bootstrap.PID++
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("ready-abandoned", intent.PhaseGeneration), abandoned); err != nil {
		t.Fatal(err)
	}
	if journal, err := ledgerMigrationLoadJournal(fd, app, true); err != nil || !journal.Pending {
		t.Fatalf("abandonment before refencing was not pending: %v", err)
	}
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseRefencing)
	preparationDigest, _ := ledgerMigrationDigest(preparation)
	restriction := ledgerMigrationRestriction{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, PreparationDigest: preparationDigest, From: preparation.Normal, To: preparation.FinalTarget}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("restrict", intent.PhaseGeneration), restriction); err != nil {
		t.Fatal(err)
	}
	if journal, err := ledgerMigrationLoadJournal(fd, app, true); err != nil || !journal.Pending {
		t.Fatalf("abandonment followed by restriction was not pending: %v", err)
	}
	targetDigest, _ := ledgerMigrationDigest(ledgerAppExpectedRoles(original, ledgerAppTarget))
	compensation := ledgerMigrationCompensation{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, PreparationDigest: preparationDigest, TargetDigest: targetDigest, WorkerOID: 99}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("compensate", intent.PhaseGeneration), compensation); err != nil {
		t.Fatal(err)
	}
	compensationDigest, _ := ledgerMigrationDigest(compensation)
	abort := ledgerMigrationAbort{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, CompensationDigest: compensationDigest, Outcome: "restored_c80"}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("abort", intent.PhaseGeneration), abort); err != nil {
		t.Fatal(err)
	}
	if journal, err := ledgerMigrationLoadJournal(fd, app, true); err != nil || !journal.Pending {
		t.Fatalf("abort before terminal was not pending: %v", err)
	}
	phase = migrationJournalNextPhase(t, fd, phase, ledgerPhaseQuiescent)
	journal, err := ledgerMigrationLoadJournal(fd, app, false)
	if err != nil || journal.Pending || len(journal.Terminals) != 1 || journal.Terminals[0] != phase {
		t.Fatalf("completed compensation was not retained: %v", err)
	}
	if ledgerMigrationAuthorizeJournal(t.Context(), fd, journal, nil) == nil {
		t.Fatal("terminal was accepted without root authorization")
	}
	if _, err := ledgerPublishExact(fd, ledgerMigrationName("normal", intent.PhaseGeneration), ledgerMigrationNormal{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerMigrationLoadJournal(fd, app, true); err == nil {
		t.Fatal("abandoned ready accepted a normal successor")
	}
	if err := unix.Unlinkat(fd, ledgerMigrationName("normal", intent.PhaseGeneration), 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.Unlinkat(fd, ledgerMigrationName("ready", intent.PhaseGeneration), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerMigrationLoadJournal(fd, app, true); err == nil {
		t.Fatal("abandonment survived loss of its original ready bytes")
	}
}

func TestMigrationReplayRetainedNormalBeforeRefencing(t *testing.T) {
	for _, test := range []struct {
		name    string
		invalid bool
		refuse  bool
	}{
		{name: "valid normal"},
		{name: "wrong preparation lineage", invalid: true},
		{name: "root refuses normal", refuse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fd, app, intent, _, inventory := migrationJournalObservationFixture(t)
			original := migrationJournalOriginal(t, app)
			var preparation ledgerMigrationPreparation
			found, err := ledgerMigrationRead(fd, "preparation", intent.PhaseGeneration, &preparation)
			if err != nil || !found {
				t.Fatal("missing preparation", err)
			}
			inventory.RoleBaseline = preparation.Normal
			inventory.RoleBaseline.Roles = append([]ledgerAppRole(nil), preparation.Normal.Roles...)
			var workerOID uint32
			for i := range inventory.RoleBaseline.Roles {
				if inventory.RoleBaseline.Roles[i].Name == composeDeletionWorkerRole {
					inventory.RoleBaseline.Roles[i].Connect = true
					workerOID = inventory.RoleBaseline.Roles[i].OID
				}
			}
			inventory.RoleBaseline.DatabaseACL = append(append([]ledgerAppDatabaseACL{}, preparation.Normal.DatabaseACL...), ledgerAppDatabaseACL{GrantorOID: preparation.Normal.DatabaseOwnerOID, GranteeOID: workerOID, Privilege: "CONNECT"})
			intentDigest, _ := ledgerMigrationDigest(intent)
			preparationDigest, _ := ledgerMigrationDigest(preparation)
			normal := ledgerMigrationNormal{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest, PreparationDigest: preparationDigest, Inventory: inventory}
			if test.invalid {
				normal.PreparationDigest = strings.Repeat("d", 64)
			}
			if _, err := ledgerPublishExact(fd, ledgerMigrationName("normal", intent.PhaseGeneration), normal); err != nil {
				t.Fatal(err)
			}
			checkpointer := &migrationJournalTestCheckpointer{}
			if test.refuse {
				checkpointer.err = ErrRecordLedgerFence
			}
			fence := &RecordLedgerFence{control: fd, maintenanceCheckpointer: checkpointer}
			err = ledgerMigrationReplayRetainedNormal(t.Context(), fence, intent, original)
			if test.invalid || test.refuse {
				if err == nil {
					t.Fatal("invalid or unacknowledged normal allowed recovery to advance")
				}
				if test.invalid && len(checkpointer.requests) != 0 {
					t.Fatal("invalid normal was sent to root")
				}
				return
			}
			body, found, readErr := fenceReadFile(fd, ledgerMigrationName("normal", intent.PhaseGeneration))
			if err != nil || readErr != nil || !found || len(checkpointer.requests) != 1 || checkpointer.requests[0].Kind != "normal" || checkpointer.requests[0].RecordSHA256 != ledgerPredecessorHash(body) {
				t.Fatalf("valid retained normal was not acknowledged exactly: %v", err)
			}
		})
	}
}
