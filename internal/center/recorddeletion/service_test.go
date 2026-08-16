package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

func TestServicePreviewFailsClosedBeforeRecordOrPersistenceForIncompleteReadiness(t *testing.T) {
	t.Parallel()

	actor := deletionTestActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa")
	core := newDeletionServiceAdapterStub(t, AdapterNameRecordCore, RecordCoreSurfaceNames(), 1)
	registry, err := NewRegistry([]Adapter{core})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	records := &deletionRecordSnapshotSourceStub{snapshot: deletionTestRecordSnapshot(t)}
	witness := &deletionWitnessSourceStub{head: deletionTestWitnessHead(1)}
	repository := &deletionPreviewRepositoryStub{}
	service := deletionTestService(t, registry, records, witness, repository)

	_, err = service.Preview(context.Background(), PreviewRequest{Actor: actor, RecordID: "rec_01"})
	if !errors.Is(err, ErrDeletionSafetyUnavailable) {
		t.Fatalf("Preview() error = %v, want ErrDeletionSafetyUnavailable", err)
	}
	if records.calls != 0 || witness.currentCalls != 0 || repository.createCalls != 0 || core.previewCalls != 0 {
		t.Fatalf("failed readiness calls records=%d witness=%d create=%d adapter-preview=%d, want all zero", records.calls, witness.currentCalls, repository.createCalls, core.previewCalls)
	}
}

func TestServicePreviewBindsAuthorizationCASDependenciesAdaptersAndWitnessWithoutPersistingToken(t *testing.T) {
	t.Parallel()

	actor := deletionTestActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa")
	registry, adapters := deletionTestRegistry(t)
	adapters[1].preview.SurvivingCopies = []AdapterSurvivingCopy{
		{Kind: SurvivingCopyKindOtherRecord, CopyCount: 2},
	}
	snapshot := deletionTestRecordSnapshot(t)
	records := &deletionRecordSnapshotSourceStub{snapshot: snapshot}
	witness := &deletionWitnessSourceStub{head: deletionTestWitnessHead(7)}
	repository := &deletionPreviewRepositoryStub{}
	repository.create = func(command CreatePreviewCommand) (StoredPreview, error) {
		if command.Object != (recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"}) {
			t.Fatalf("preview object = %#v", command.Object)
		}
		if command.ActorScopeDigest != actor.CanonicalHash() || command.BindingDigest == ([sha256.Size]byte{}) {
			t.Fatalf("preview actor/binding digest = %x/%x", command.ActorScopeDigest, command.BindingDigest)
		}
		if command.Record.CurrentRevisionID != snapshot.CurrentRevisionID || command.Record.LockVersion != snapshot.LockVersion || command.Record.AuthorizationEpoch != snapshot.AuthorizationEpoch || command.Record.ContentDeliveryEpoch != snapshot.ContentDeliveryEpoch {
			t.Fatalf("preview record CAS = %#v, want %#v", command.Record, snapshot)
		}
		if command.WitnessHead != witness.head || command.AdapterReadinessDigest == ([sha256.Size]byte{}) || command.AdapterPreviewDigest == ([sha256.Size]byte{}) {
			t.Fatalf("preview safety snapshots witness=%#v readiness=%x adapters=%x", command.WitnessHead, command.AdapterReadinessDigest, command.AdapterPreviewDigest)
		}
		if command.TokenCommitment == ([sha256.Size]byte{}) || command.RequestFingerprint.Validate() != nil || command.TTL != DeletionPreviewTTL {
			t.Fatalf("preview persistence identity commitment=%x fingerprint=%v ttl=%s", command.TokenCommitment, command.RequestFingerprint.Validate(), command.TTL)
		}
		return deletionStoredPreviewFromCommand(t, command), nil
	}
	service := deletionTestService(t, registry, records, witness, repository)

	result, err := service.Preview(context.Background(), PreviewRequest{Actor: actor, RecordID: "rec_01"})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	transport := result.Token.Transport()
	parsed, err := recordplatform.ParseDeletionRequestTokenTransportV1(transport)
	if err != nil {
		t.Fatalf("preview token %q is not canonical: %v", transport, err)
	}
	if result.ReservationID != "drs_preview01" || result.ExpiresAt.IsZero() || !parsed.MatchesCommitment(deletionTestDeploymentID(), recordplatform.ProjectIDDefault, repository.lastCreate.TokenCommitment) {
		t.Fatalf("Preview() = %#v, token does not bind persisted commitment", result)
	}
	if err := result.Summary.Validate(); err != nil {
		t.Fatalf("Preview() summary = %#v: %v", result.Summary, err)
	}
	if !reflect.DeepEqual(result.Summary.OnlinePurgeScopes, RequiredAdapterNames()) ||
		!reflect.DeepEqual(result.Summary.SurvivingCopies, []SurvivingCopySummary{{
			Scope: AdapterNameRecordAttachments, Kind: SurvivingCopyKindOtherRecord, CopyCount: 2,
		}}) || result.Summary.ManagedBackup != snapshot.ManagedBackup ||
		result.Summary.LedgerHealth != LedgerHealthHealthy {
		t.Fatalf("Preview() safety summary = %#v", result.Summary)
	}
	if records.calls != 1 || witness.currentCalls != 1 || repository.createCalls != 1 {
		t.Fatalf("preview calls records=%d witness=%d create=%d, want 1/1/1", records.calls, witness.currentCalls, repository.createCalls)
	}
	for _, adapter := range adapters {
		if adapter.previewCalls != 1 {
			t.Fatalf("adapter %q preview calls = %d, want 1", adapter.name, adapter.previewCalls)
		}
	}
	if strings.Contains(fmt.Sprintf("%#v", repository.lastCreate), transport) {
		t.Fatal("CreatePreviewCommand contains the raw deletion token transport")
	}
}

func TestServicePreviewReauthorizationDenialStopsAdapterWitnessAndPersistence(t *testing.T) {
	t.Parallel()

	actor := deletionTestActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa")
	registry, adapters := deletionTestRegistry(t)
	records := &deletionRecordSnapshotSourceStub{snapshot: deletionDeniedRecordSnapshot(t)}
	witness := &deletionWitnessSourceStub{head: deletionTestWitnessHead(1)}
	repository := &deletionPreviewRepositoryStub{}
	service := deletionTestService(t, registry, records, witness, repository)

	_, err := service.Preview(context.Background(), PreviewRequest{Actor: actor, RecordID: "rec_01"})
	if !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("Preview() error = %v, want recordauth.ErrDenied", err)
	}
	if witness.currentCalls != 0 || repository.createCalls != 0 {
		t.Fatalf("denied preview calls witness=%d create=%d, want zero", witness.currentCalls, repository.createCalls)
	}
	for _, adapter := range adapters {
		if adapter.previewCalls != 0 {
			t.Fatalf("denied adapter %q preview calls = %d, want zero", adapter.name, adapter.previewCalls)
		}
	}
}

func TestServiceExecuteReauthorizesAndRejectsRevocationBeforeReservation(t *testing.T) {
	t.Parallel()

	fixture := newDeletionExecutionFixture(t)
	fixture.records.snapshot = deletionDeniedRecordSnapshot(t)

	_, err := fixture.service.Execute(context.Background(), ExecuteRequest{
		Actor:         fixture.actor,
		RecordID:      "rec_01",
		ReservationID: fixture.stored.ReservationID,
		Token:         fixture.token,
		ReasonCode:    DeletionReasonUserConfirmed,
	})
	if !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("Execute() error = %v, want recordauth.ErrDenied", err)
	}
	if fixture.repository.reserveCalls != 0 || fixture.witness.verifyCalls != 0 {
		t.Fatalf("revoked execute reserve=%d witness-verify=%d, want zero", fixture.repository.reserveCalls, fixture.witness.verifyCalls)
	}
	for _, adapter := range fixture.adapters {
		if adapter.previewCalls != 1 {
			t.Fatalf("adapter %q calls = %d, want only original preview", adapter.name, adapter.previewCalls)
		}
	}
}

func TestServiceExecuteRejectsRecordDependencyAndAdapterDriftWithoutReservation(t *testing.T) {
	t.Parallel()

	t.Run("record dependency drift", func(t *testing.T) {
		fixture := newDeletionExecutionFixture(t)
		fixture.records.snapshot.DependencyGraphDigest[0] ^= 0xff

		_, err := fixture.service.Execute(context.Background(), ExecuteRequest{Actor: fixture.actor, RecordID: "rec_01", ReservationID: fixture.stored.ReservationID, Token: fixture.token, ReasonCode: DeletionReasonUserConfirmed})
		if !errors.Is(err, ErrDeletionPreviewStale) {
			t.Fatalf("Execute() error = %v, want ErrDeletionPreviewStale", err)
		}
		if fixture.repository.reserveCalls != 0 {
			t.Fatalf("ReservePreview() calls = %d, want zero", fixture.repository.reserveCalls)
		}
	})

	t.Run("adapter dependency drift", func(t *testing.T) {
		fixture := newDeletionExecutionFixture(t)
		fixture.adapters[3].preview.DependencyDigest[0] ^= 0xff

		_, err := fixture.service.Execute(context.Background(), ExecuteRequest{Actor: fixture.actor, RecordID: "rec_01", ReservationID: fixture.stored.ReservationID, Token: fixture.token, ReasonCode: DeletionReasonUserConfirmed})
		if !errors.Is(err, ErrDeletionPreviewStale) {
			t.Fatalf("Execute() error = %v, want ErrDeletionPreviewStale", err)
		}
		if fixture.repository.reserveCalls != 0 {
			t.Fatalf("ReservePreview() calls = %d, want zero", fixture.repository.reserveCalls)
		}
	})

	t.Run("surviving copy drift", func(t *testing.T) {
		fixture := newDeletionExecutionFixture(t)
		fixture.adapters[1].preview.SurvivingCopies = []AdapterSurvivingCopy{
			{Kind: SurvivingCopyKindOtherRecord, CopyCount: 1},
		}

		_, err := fixture.service.Execute(context.Background(), ExecuteRequest{Actor: fixture.actor, RecordID: "rec_01", ReservationID: fixture.stored.ReservationID, Token: fixture.token, ReasonCode: DeletionReasonUserConfirmed})
		if !errors.Is(err, ErrDeletionPreviewStale) {
			t.Fatalf("Execute() error = %v, want ErrDeletionPreviewStale", err)
		}
		if fixture.repository.reserveCalls != 0 {
			t.Fatalf("ReservePreview() calls = %d, want zero", fixture.repository.reserveCalls)
		}
	})

	t.Run("managed backup disclosure drift", func(t *testing.T) {
		fixture := newDeletionExecutionFixture(t)
		fixture.records.snapshot.ManagedBackup.MaximumRetentionDays++

		_, err := fixture.service.Execute(context.Background(), ExecuteRequest{Actor: fixture.actor, RecordID: "rec_01", ReservationID: fixture.stored.ReservationID, Token: fixture.token, ReasonCode: DeletionReasonUserConfirmed})
		if !errors.Is(err, ErrDeletionPreviewStale) {
			t.Fatalf("Execute() error = %v, want ErrDeletionPreviewStale", err)
		}
		if fixture.repository.reserveCalls != 0 {
			t.Fatalf("ReservePreview() calls = %d, want zero", fixture.repository.reserveCalls)
		}
	})
}

func TestServiceExecuteRequiresWitnessExtensionBeforeReservation(t *testing.T) {
	t.Parallel()

	fixture := newDeletionExecutionFixture(t)
	fixture.witness.verifyErr = errors.New("witness tail unavailable")

	_, err := fixture.service.Execute(context.Background(), ExecuteRequest{Actor: fixture.actor, RecordID: "rec_01", ReservationID: fixture.stored.ReservationID, Token: fixture.token, ReasonCode: DeletionReasonUserConfirmed})
	if !errors.Is(err, ErrDeletionSafetyUnavailable) {
		t.Fatalf("Execute() error = %v, want ErrDeletionSafetyUnavailable", err)
	}
	if fixture.repository.reserveCalls != 0 {
		t.Fatalf("ReservePreview() calls = %d, want zero", fixture.repository.reserveCalls)
	}
}

func TestServiceExecuteReservesExactPreviewCASAndReturnsProvisionalFence(t *testing.T) {
	t.Parallel()

	fixture := newDeletionExecutionFixture(t)
	fixture.repository.reserve = func(command ReservePreviewCommand) (DeletionOperation, error) {
		if command.DeploymentID != deletionTestDeploymentID() || command.ActorID != fixture.actor.UserID ||
			command.DeletionContractVersion != RecordDeletionContractVersionV1 {
			t.Fatalf("reserve durable ledger identity = %q/%q/v%d", command.DeploymentID, command.ActorID, command.DeletionContractVersion)
		}
		if command.Preview.ReservationID != fixture.stored.ReservationID || command.ExpectedBindingDigest != fixture.stored.BindingDigest {
			t.Fatalf("reserve preview/binding = %#v/%x", command.Preview, command.ExpectedBindingDigest)
		}
		if command.RequestFingerprint.Validate() != nil || !command.RequestFingerprint.MatchesPersisted(fixture.stored.RequestFingerprint) {
			t.Fatalf("reserve request fingerprint does not match preview")
		}
		if command.Record.CurrentRevisionID != fixture.records.snapshot.CurrentRevisionID || command.Record.LockVersion != fixture.records.snapshot.LockVersion || command.Record.AuthorizationEpoch != fixture.records.snapshot.AuthorizationEpoch {
			t.Fatalf("reserve record CAS = %#v", command.Record)
		}
		if command.ObservedWitnessHead != fixture.witness.verifiedHead || command.OwnerID != "deletion_worker_01" || command.OwnerLeaseDuration != 2*time.Minute || command.ReasonCode != DeletionReasonUserConfirmed {
			t.Fatalf("reserve witness/owner/reason = %#v/%q/%s/%q", command.ObservedWitnessHead, command.OwnerID, command.OwnerLeaseDuration, command.ReasonCode)
		}
		return deletionTestOperation(DeletionStateProvisionalFenced), nil
	}

	operation, err := fixture.service.Execute(context.Background(), ExecuteRequest{Actor: fixture.actor, RecordID: "rec_01", ReservationID: fixture.stored.ReservationID, Token: fixture.token, ReasonCode: DeletionReasonUserConfirmed})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if operation.State != DeletionStateProvisionalFenced || operation.OperationID != "rpo_operation01" || operation.FenceEpoch == 0 {
		t.Fatalf("Execute() = %#v, want provisional fenced operation", operation)
	}
	if fixture.repository.resolveCalls != 1 || fixture.repository.reserveCalls != 1 || fixture.witness.verifyCalls != 1 {
		t.Fatalf("execute calls resolve=%d reserve=%d witness=%d, want 1/1/1", fixture.repository.resolveCalls, fixture.repository.reserveCalls, fixture.witness.verifyCalls)
	}
	if fixture.repository.lastLookup.ReservationID != fixture.stored.ReservationID {
		t.Fatalf("execute preview locator = %q, want %q", fixture.repository.lastLookup.ReservationID, fixture.stored.ReservationID)
	}
}

func TestServiceExecutePreservesFinalReservationCASConflict(t *testing.T) {
	t.Parallel()

	fixture := newDeletionExecutionFixture(t)
	fixture.repository.reserveErr = ErrDeletionPreviewStale

	_, err := fixture.service.Execute(context.Background(), ExecuteRequest{Actor: fixture.actor, RecordID: "rec_01", ReservationID: fixture.stored.ReservationID, Token: fixture.token, ReasonCode: DeletionReasonUserConfirmed})
	if !errors.Is(err, ErrDeletionPreviewStale) {
		t.Fatalf("Execute() error = %v, want ErrDeletionPreviewStale", err)
	}
}

func TestServiceExecuteReplaysExistingOperationBeforeExpiryReadinessOrDependencyChecks(t *testing.T) {
	t.Parallel()

	fixture := newDeletionExecutionFixture(t)
	existing := deletionTestOperation(DeletionStateWitnessPending)
	fixture.stored.Operation = &existing
	fixture.repository.stored = fixture.stored
	for _, adapter := range fixture.adapters {
		adapter.healthErr = errors.New("adapter now unavailable")
	}
	recordCallsBefore := fixture.records.calls

	operation, err := fixture.service.Execute(context.Background(), ExecuteRequest{Actor: fixture.actor, RecordID: "rec_01", ReservationID: fixture.stored.ReservationID, Token: fixture.token, ReasonCode: DeletionReasonUserConfirmed})
	if err != nil {
		t.Fatalf("Execute() replay error = %v", err)
	}
	if operation != existing {
		t.Fatalf("Execute() replay = %#v, want %#v", operation, existing)
	}
	if fixture.records.calls != recordCallsBefore || fixture.repository.reserveCalls != 0 || fixture.witness.verifyCalls != 0 {
		t.Fatalf("replay performed fresh work records=%d(before %d) reserve=%d witness=%d", fixture.records.calls, recordCallsBefore, fixture.repository.reserveCalls, fixture.witness.verifyCalls)
	}
}

func TestServiceExecuteRejectsSameTokenForDifferentActorAsReuseBeforeMutation(t *testing.T) {
	t.Parallel()

	fixture := newDeletionExecutionFixture(t)
	otherActor := deletionTestActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb")

	_, err := fixture.service.Execute(context.Background(), ExecuteRequest{Actor: otherActor, RecordID: "rec_01", ReservationID: fixture.stored.ReservationID, Token: fixture.token, ReasonCode: DeletionReasonUserConfirmed})
	if !errors.Is(err, ErrDeletionRequestTokenReused) {
		t.Fatalf("Execute() error = %v, want ErrDeletionRequestTokenReused", err)
	}
	if fixture.repository.reserveCalls != 0 || fixture.witness.verifyCalls != 0 {
		t.Fatalf("token reuse reserve=%d witness=%d, want zero", fixture.repository.reserveCalls, fixture.witness.verifyCalls)
	}
}

func TestServiceStatusAllowsInitiatorAndProjectAdminWithoutPreviewDependencies(t *testing.T) {
	t.Parallel()

	registry, adapters := deletionTestRegistry(t)
	records := &deletionRecordSnapshotSourceStub{snapshot: deletionTestRecordSnapshot(t)}
	witness := &deletionWitnessSourceStub{head: deletionTestWitnessHead(1)}
	repository := &deletionPreviewRepositoryStub{
		status: DeletionOperationStatus{
			Operation:        deletionTestOperation(DeletionStateWitnessPending),
			InitiatorActorID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}
	service := deletionTestService(t, registry, records, witness, repository)

	tests := []struct {
		name  string
		actor recordauth.ActorScope
	}{
		{name: "initiator", actor: deletionTestActorWithRole(t, repository.status.InitiatorActorID, recordauth.RoleViewer)},
		{name: "project admin", actor: deletionTestActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation, err := service.Status(context.Background(), StatusRequest{
				Actor:       tt.actor,
				OperationID: repository.status.Operation.OperationID,
			})
			if err != nil {
				t.Fatalf("Status() error = %v", err)
			}
			if operation != repository.status.Operation {
				t.Fatalf("Status() = %#v, want %#v", operation, repository.status.Operation)
			}
		})
	}
	if repository.statusCalls != len(tests) || repository.lastStatusProjectID != recordplatform.ProjectIDDefault ||
		repository.lastStatusOperationID != repository.status.Operation.OperationID {
		t.Fatalf(
			"status lookup calls=%d project=%q operation=%q",
			repository.statusCalls,
			repository.lastStatusProjectID,
			repository.lastStatusOperationID,
		)
	}
	if records.calls != 0 || witness.currentCalls != 0 || witness.verifyCalls != 0 {
		t.Fatalf("status touched preview dependencies records=%d witness-current=%d witness-verify=%d", records.calls, witness.currentCalls, witness.verifyCalls)
	}
	for _, adapter := range adapters {
		if adapter.previewCalls != 0 {
			t.Fatalf("status called adapter %q preview %d times", adapter.name, adapter.previewCalls)
		}
	}
}

func TestServiceStatusReturnsOpaqueNotFoundForUnauthorizedOrMissingOperation(t *testing.T) {
	t.Parallel()

	t.Run("unauthorized", func(t *testing.T) {
		registry, _ := deletionTestRegistry(t)
		repository := &deletionPreviewRepositoryStub{
			status: DeletionOperationStatus{
				Operation:        deletionTestOperation(DeletionStateOnlinePurged),
				InitiatorActorID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
			},
		}
		service := deletionTestService(
			t,
			registry,
			&deletionRecordSnapshotSourceStub{snapshot: deletionTestRecordSnapshot(t)},
			&deletionWitnessSourceStub{head: deletionTestWitnessHead(1)},
			repository,
		)
		actor := deletionTestActorWithRole(t, "usr_cccccccccccccccccccccccc", recordauth.RoleViewer)

		_, err := service.Status(context.Background(), StatusRequest{Actor: actor, OperationID: "rpo_operation01"})
		if !errors.Is(err, ErrDeletionOperationNotFound) {
			t.Fatalf("Status() error = %v, want ErrDeletionOperationNotFound", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		registry, _ := deletionTestRegistry(t)
		repository := &deletionPreviewRepositoryStub{statusErr: ErrDeletionOperationNotFound}
		service := deletionTestService(
			t,
			registry,
			&deletionRecordSnapshotSourceStub{snapshot: deletionTestRecordSnapshot(t)},
			&deletionWitnessSourceStub{head: deletionTestWitnessHead(1)},
			repository,
		)

		_, err := service.Status(context.Background(), StatusRequest{Actor: deletionTestActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa"), OperationID: "rpo_missing01"})
		if !errors.Is(err, ErrDeletionOperationNotFound) {
			t.Fatalf("Status() error = %v, want ErrDeletionOperationNotFound", err)
		}
	})
}

func TestServiceStatusFailsClosedWhenProjectionCannotProveOperation(t *testing.T) {
	t.Parallel()

	registry, _ := deletionTestRegistry(t)
	repository := &deletionPreviewRepositoryStub{statusErr: errors.New("projection read failed with secret detail")}
	service := deletionTestService(
		t,
		registry,
		&deletionRecordSnapshotSourceStub{snapshot: deletionTestRecordSnapshot(t)},
		&deletionWitnessSourceStub{head: deletionTestWitnessHead(1)},
		repository,
	)

	_, err := service.Status(context.Background(), StatusRequest{
		Actor:       deletionTestActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa"),
		OperationID: "rpo_operation01",
	})
	if !errors.Is(err, ErrDeletionStatusUnavailable) {
		t.Fatalf("Status() error = %v, want ErrDeletionStatusUnavailable", err)
	}
}

func TestNewServiceRejectsInvalidScopeOwnerAndTypedNilDependencies(t *testing.T) {
	t.Parallel()

	registry, _ := deletionTestRegistry(t)
	records := &deletionRecordSnapshotSourceStub{snapshot: deletionTestRecordSnapshot(t)}
	witness := &deletionWitnessSourceStub{head: deletionTestWitnessHead(1)}
	repository := &deletionPreviewRepositoryStub{}
	var typedNilRecords *deletionRecordSnapshotSourceStub
	var typedNilWitness *deletionWitnessSourceStub
	var typedNilRepository *deletionPreviewRepositoryStub

	tests := []struct {
		name       string
		deployment recordplatform.DeploymentID
		records    DeletionRecordSnapshotSource
		witness    DeletionWitnessSource
		repository DeletionPreviewRepository
		options    ServiceOptions
	}{
		{name: "invalid deployment", deployment: "dp_invalid", records: records, witness: witness, repository: repository, options: deletionTestServiceOptions()},
		{name: "typed nil records", deployment: deletionTestDeploymentID(), records: typedNilRecords, witness: witness, repository: repository, options: deletionTestServiceOptions()},
		{name: "typed nil witness", deployment: deletionTestDeploymentID(), records: records, witness: typedNilWitness, repository: repository, options: deletionTestServiceOptions()},
		{name: "typed nil repository", deployment: deletionTestDeploymentID(), records: records, witness: witness, repository: typedNilRepository, options: deletionTestServiceOptions()},
		{name: "invalid owner", deployment: deletionTestDeploymentID(), records: records, witness: witness, repository: repository, options: ServiceOptions{OwnerID: "INVALID", OwnerLeaseDuration: 2 * time.Minute}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewService(tt.deployment, registry, tt.records, tt.witness, tt.repository, tt.options); !errors.Is(err, ErrInvalidDeletionService) {
				t.Fatalf("NewService() error = %v, want ErrInvalidDeletionService", err)
			}
		})
	}
}

type deletionExecutionFixture struct {
	actor      recordauth.ActorScope
	token      recordplatform.DeletionRequestTokenTransportV1
	stored     StoredPreview
	service    *Service
	adapters   []*deletionServiceAdapterStub
	records    *deletionRecordSnapshotSourceStub
	witness    *deletionWitnessSourceStub
	repository *deletionPreviewRepositoryStub
}

func newDeletionExecutionFixture(t *testing.T) deletionExecutionFixture {
	t.Helper()
	actor := deletionTestActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa")
	registry, adapters := deletionTestRegistry(t)
	records := &deletionRecordSnapshotSourceStub{snapshot: deletionTestRecordSnapshot(t)}
	witness := &deletionWitnessSourceStub{head: deletionTestWitnessHead(7), verifiedHead: deletionTestWitnessHead(9)}
	repository := &deletionPreviewRepositoryStub{}
	service := deletionTestService(t, registry, records, witness, repository)
	preview, err := service.Preview(context.Background(), PreviewRequest{Actor: actor, RecordID: "rec_01"})
	if err != nil {
		t.Fatalf("Preview() fixture error = %v", err)
	}
	token, err := recordplatform.ParseDeletionRequestTokenTransportV1(preview.Token.Transport())
	if err != nil {
		t.Fatalf("ParseDeletionRequestTokenTransportV1() error = %v", err)
	}
	return deletionExecutionFixture{
		actor:      actor,
		token:      token,
		stored:     repository.stored,
		service:    service,
		adapters:   adapters,
		records:    records,
		witness:    witness,
		repository: repository,
	}
}

func deletionTestService(t *testing.T, registry Registry, records DeletionRecordSnapshotSource, witness DeletionWitnessSource, repository DeletionPreviewRepository) *Service {
	t.Helper()
	service, err := NewService(deletionTestDeploymentID(), registry, records, witness, repository, deletionTestServiceOptions())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func deletionTestServiceOptions() ServiceOptions {
	return ServiceOptions{OwnerID: "deletion_worker_01", OwnerLeaseDuration: 2 * time.Minute}
}

func deletionTestDeploymentID() recordplatform.DeploymentID {
	return recordplatform.DeploymentID("dp-" + strings.Repeat("a", 64))
}

func deletionTestActor(t *testing.T, userID string) recordauth.ActorScope {
	return deletionTestActorWithRole(t, userID, recordauth.RoleProjectAdmin)
}

func deletionTestActorWithRole(t *testing.T, userID string, role recordauth.Role) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    userID,
		Role:      role,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func deletionTestRecordSnapshot(t *testing.T) DeletionRecordSnapshot {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      recordauth.ProjectIDDefault,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	current := visibility
	source, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindVPS,
		SourceID:     "vps_cccccccccccccccc",
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &current,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return DeletionRecordSnapshot{
		RecordID:             "rec_01",
		CurrentRevisionID:    "rrv_01",
		LockVersion:          4,
		AuthorizationEpoch:   3,
		ContentDeliveryEpoch: 6,
		Authorization: recordauth.ResourceScope{
			Version:    recordauth.ResourceScopeVersionV1,
			ProjectID:  recordauth.ProjectIDDefault,
			Visibility: visibility,
			Sources:    []recordauth.SourceAuthorization{source},
		},
		DependencyGraphDigest:    deletionTestDigest(11),
		BackupInventoryDigest:    deletionTestDigest(12),
		ProcessorInventoryDigest: deletionTestDigest(13),
		ManagedBackup: ManagedBackupSummary{
			RetainedCopyCount:    1,
			MaximumRetentionDays: 30,
			LatestExpiresAt:      time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC),
		},
	}
}

func deletionDeniedRecordSnapshot(t *testing.T) DeletionRecordSnapshot {
	t.Helper()
	snapshot := deletionTestRecordSnapshot(t)
	restricted, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindRestricted,
		ProjectID:      recordauth.ProjectIDDefault,
		AllowedRoles:   []recordauth.Role{recordauth.RoleViewer},
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 2,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope(restricted) error = %v", err)
	}
	snapshot.Authorization.Visibility = restricted
	return snapshot
}

func deletionTestWitnessHead(seed byte) WitnessHead {
	return WitnessHead{Sequence: uint64(seed), EntryHash: deletionTestDigest(seed)}
}

func deletionTestDigest(seed byte) [sha256.Size]byte {
	var digest [sha256.Size]byte
	for index := range digest {
		digest[index] = seed + byte(index)
	}
	return digest
}

func deletionTestRegistry(t *testing.T) (Registry, []*deletionServiceAdapterStub) {
	t.Helper()
	adapters := make([]*deletionServiceAdapterStub, 0, len(RequiredAdapterNames()))
	registered := make([]Adapter, 0, len(RequiredAdapterNames()))
	for index, name := range RequiredAdapterNames() {
		surfaces := []SurfaceName{SurfaceName("fixture." + string(name))}
		switch name {
		case AdapterNameRecordCore:
			surfaces = RecordCoreSurfaceNames()
		case AdapterNameRecordAttachments:
			surfaces = RecordAttachmentsSurfaceNames()
		case AdapterNameRecordEvidence:
			surfaces = RecordEvidenceSurfaceNames()
		}
		adapter := newDeletionServiceAdapterStub(t, name, surfaces, byte(index+1))
		adapters = append(adapters, adapter)
		registered = append(registered, adapter)
	}
	registry, err := NewRegistry(registered)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry, adapters
}

func newDeletionServiceAdapterStub(t *testing.T, name AdapterName, surfaces []SurfaceName, seed byte) *deletionServiceAdapterStub {
	t.Helper()
	descriptor, err := NewAdapterDescriptor(name, surfaces)
	if err != nil {
		t.Fatalf("NewAdapterDescriptor(%q) error = %v", name, err)
	}
	health, err := NewAdapterHealthSnapshot(true, uint64(seed), deletionTestDigest(seed))
	if err != nil {
		t.Fatalf("NewAdapterHealthSnapshot(%q) error = %v", name, err)
	}
	return &deletionServiceAdapterStub{
		name:       name,
		descriptor: descriptor,
		health:     health,
		preview: AdapterPreviewSnapshot{
			DependencyDigest: deletionTestDigest(seed + 32),
			ImpactDigest:     deletionTestDigest(seed + 64),
			SurvivingCopies:  []AdapterSurvivingCopy{},
		},
	}
}

type deletionServiceAdapterStub struct {
	name         AdapterName
	descriptor   AdapterDescriptor
	health       AdapterHealthSnapshot
	healthErr    error
	preview      AdapterPreviewSnapshot
	previewErr   error
	purge        AdapterPurgeReceipt
	purgeErr     error
	verifyErr    error
	purgeHook    func(AdapterName)
	verifyHook   func(AdapterName)
	previewCalls int
	purgeCalls   int
	verifyCalls  int
}

func (adapter *deletionServiceAdapterStub) Descriptor() AdapterDescriptor {
	return adapter.descriptor
}

func (adapter *deletionServiceAdapterStub) HealthSnapshot(context.Context) (AdapterHealthSnapshot, error) {
	return adapter.health, adapter.healthErr
}

func (adapter *deletionServiceAdapterStub) PreviewDeletion(context.Context, PreviewTarget) (AdapterPreviewSnapshot, error) {
	adapter.previewCalls++
	return adapter.preview, adapter.previewErr
}

func (adapter *deletionServiceAdapterStub) PurgeDeletion(context.Context, PurgeTarget) (AdapterPurgeReceipt, error) {
	adapter.purgeCalls++
	if adapter.purgeHook != nil {
		adapter.purgeHook(adapter.name)
	}
	return adapter.purge, adapter.purgeErr
}

func (adapter *deletionServiceAdapterStub) VerifyDeletion(context.Context, PurgeTarget, AdapterPurgeReceipt) error {
	adapter.verifyCalls++
	if adapter.verifyHook != nil {
		adapter.verifyHook(adapter.name)
	}
	return adapter.verifyErr
}

type deletionRecordSnapshotSourceStub struct {
	snapshot DeletionRecordSnapshot
	err      error
	calls    int
}

func (source *deletionRecordSnapshotSourceStub) CurrentDeletionSnapshot(context.Context, recordauth.ActorScope, string) (DeletionRecordSnapshot, error) {
	source.calls++
	return source.snapshot, source.err
}

type deletionWitnessSourceStub struct {
	head         WitnessHead
	currentErr   error
	verifiedHead WitnessHead
	verifyErr    error
	currentCalls int
	verifyCalls  int
}

func (source *deletionWitnessSourceStub) CurrentWitnessHead(context.Context) (WitnessHead, error) {
	source.currentCalls++
	return source.head, source.currentErr
}

func (source *deletionWitnessSourceStub) VerifyWitnessExtension(_ context.Context, prior WitnessHead) (WitnessHead, error) {
	source.verifyCalls++
	if source.verifyErr != nil {
		return WitnessHead{}, source.verifyErr
	}
	if source.verifiedHead.Sequence == 0 {
		return prior, nil
	}
	return source.verifiedHead, nil
}

type deletionPreviewRepositoryStub struct {
	stored                StoredPreview
	create                func(CreatePreviewCommand) (StoredPreview, error)
	createErr             error
	reserve               func(ReservePreviewCommand) (DeletionOperation, error)
	reserveErr            error
	resolveErr            error
	status                DeletionOperationStatus
	statusErr             error
	lastCreate            CreatePreviewCommand
	lastLookup            PreviewLookup
	lastReserve           ReservePreviewCommand
	lastStatusProjectID   recordplatform.ProjectID
	lastStatusOperationID string
	createCalls           int
	resolveCalls          int
	reserveCalls          int
	statusCalls           int
}

func (repository *deletionPreviewRepositoryStub) CreatePreview(_ context.Context, command CreatePreviewCommand) (StoredPreview, error) {
	repository.createCalls++
	repository.lastCreate = command
	if repository.createErr != nil {
		return StoredPreview{}, repository.createErr
	}
	var stored StoredPreview
	var err error
	if repository.create != nil {
		stored, err = repository.create(command)
	} else {
		stored = deletionStoredPreviewFromCommand(nil, command)
	}
	if err == nil {
		repository.stored = stored
	}
	return stored, err
}

func (repository *deletionPreviewRepositoryStub) ResolvePreview(_ context.Context, lookup PreviewLookup) (StoredPreview, error) {
	repository.resolveCalls++
	repository.lastLookup = lookup
	if repository.resolveErr != nil {
		return StoredPreview{}, repository.resolveErr
	}
	return repository.stored, nil
}

func (repository *deletionPreviewRepositoryStub) ReservePreview(_ context.Context, command ReservePreviewCommand) (DeletionOperation, error) {
	repository.reserveCalls++
	repository.lastReserve = command
	if repository.reserveErr != nil {
		return DeletionOperation{}, repository.reserveErr
	}
	if repository.reserve != nil {
		return repository.reserve(command)
	}
	return deletionTestOperation(DeletionStateProvisionalFenced), nil
}

func (repository *deletionPreviewRepositoryStub) ResolveOperationStatus(
	_ context.Context,
	projectID recordplatform.ProjectID,
	operationID string,
) (DeletionOperationStatus, error) {
	repository.statusCalls++
	repository.lastStatusProjectID = projectID
	repository.lastStatusOperationID = operationID
	if repository.statusErr != nil {
		return DeletionOperationStatus{}, repository.statusErr
	}
	return repository.status, nil
}

func deletionStoredPreviewFromCommand(t *testing.T, command CreatePreviewCommand) StoredPreview {
	bytes, err := command.RequestFingerprint.PersistedBytes()
	if err != nil {
		if t != nil {
			t.Fatalf("PersistedBytes() error = %v", err)
		}
		return StoredPreview{}
	}
	persisted, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(bytes[:])
	if err != nil {
		if t != nil {
			t.Fatalf("ParseTrustedPersistedRequestFingerprintV1() error = %v", err)
		}
		return StoredPreview{}
	}
	return StoredPreview{
		ReservationID:      "drs_preview01",
		Object:             command.Object,
		ActorScopeDigest:   command.ActorScopeDigest,
		TokenCommitment:    command.TokenCommitment,
		RequestFingerprint: persisted,
		BindingDigest:      command.BindingDigest,
		WitnessHead:        command.WitnessHead,
		ExpiresAt:          time.Date(2026, time.August, 3, 12, 10, 0, 0, time.UTC),
	}
}

func deletionTestOperation(state DeletionState) DeletionOperation {
	operation := DeletionOperation{
		OperationID:   "rpo_operation01",
		ReservationID: "drs_preview01",
		Object: recordplatform.ObjectRef{
			ProjectID:  "default",
			ObjectKind: "record",
			ObjectID:   "rec_01",
		},
		ReasonCode: DeletionReasonUserConfirmed,
		State:      state,
		FenceEpoch: 7,
	}
	switch state {
	case DeletionStateWitnessPending, DeletionStateDeleteRequested, DeletionStateFencePropagating,
		DeletionStateReadFenced, DeletionStateOnlinePurging, DeletionStateRetryRequired:
		operation.LedgerSequence = 11
		operation.LedgerEntryHash = deletionTestDigest(21)
	case DeletionStateOnlinePurged:
		operation.LedgerSequence = 11
		operation.LedgerEntryHash = deletionTestDigest(21)
		operation.ReceiptDigest = deletionTestDigest(22)
	case DeletionStateReleasePending, DeletionStateNotCommitted:
		operation.LedgerSequence = 12
		operation.LedgerEntryHash = deletionTestDigest(23)
		operation.ReleaseEpoch = 3
	}
	return operation
}
