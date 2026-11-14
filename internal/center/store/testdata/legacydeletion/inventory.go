package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/store"
)

var (
	errLegacyInventoryUnavailable = errors.New("legacy inventory unavailable")
	errLegacyAdmissionUnavailable = errors.New("legacy admission unavailable")
)

var (
	_ recorddeletion.DeletionRecordSnapshotSource = legacyRecordInventory{}
	_ recorddeletion.DeletionPreviewAdapter       = (*legacyEmptyClientAdapter)(nil)
	_ recorddeletion.DeletionPreviewRepository    = (*legacyObservedRepository)(nil)
)

// These digests come only from a fresh, credential-checked parent observation.
// The parent establishes the physical no-copy lifetime and SQL inventory.
type legacyInventoryObservation struct {
	realm      [sha256.Size]byte
	catalog    [sha256.Size]byte
	backup     [sha256.Size]byte
	client     [sha256.Size]byte
	comparison [sha256.Size]byte
	dependency [sha256.Size]byte
	processor  [sha256.Size]byte
}

func (parent *legacyParent) inventoryObserve(ctx context.Context) (legacyInventoryObservation, error) {
	var observation legacyInventoryObservation
	request := struct {
		Op              string `json:"op"`
		PhaseGeneration uint64 `json:"phase_generation"`
	}{"inventory_observe", parent.generation}
	digestFields := []string{
		"realm_digest", "catalog_digest", "backup_digest", "client_digest",
		"comparison_digest", "dependency_digest", "processor_digest",
	}
	fields, err := parent.exchangeNullable(ctx, request, digestFields,
		"op", "phase_generation", "available", "realm_digest", "catalog_digest",
		"backup_digest", "client_digest", "comparison_digest", "dependency_digest", "processor_digest")
	if err != nil {
		return observation, err
	}
	var available bool
	if !parent.responseIdentity(fields, "inventory_observe") || json.Unmarshal(fields["available"], &available) != nil {
		parent.invalid = true
		return observation, errors.New("invalid inventory response")
	}
	values := []*[sha256.Size]byte{
		&observation.realm, &observation.catalog, &observation.backup, &observation.client,
		&observation.comparison, &observation.dependency, &observation.processor,
	}
	for index, name := range digestFields {
		raw := bytes.TrimSpace(fields[name])
		if !available {
			if !bytes.Equal(raw, []byte("null")) {
				parent.invalid = true
				return legacyInventoryObservation{}, errors.New("invalid inventory response")
			}
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil || len(value) != 2*sha256.Size {
			parent.invalid = true
			return legacyInventoryObservation{}, errors.New("invalid inventory response")
		}
		decoded, decodeErr := hex.DecodeString(value)
		if decodeErr != nil || hex.EncodeToString(decoded) != value || len(decoded) != sha256.Size {
			parent.invalid = true
			return legacyInventoryObservation{}, errors.New("invalid inventory response")
		}
		copy(values[index][:], decoded)
		if *values[index] == [sha256.Size]byte{} {
			parent.invalid = true
			return legacyInventoryObservation{}, errors.New("invalid inventory response")
		}
	}
	if !available {
		return legacyInventoryObservation{}, errLegacyInventoryUnavailable
	}
	return observation, nil
}

// The encoding is private to this pinned caller: each domain and field is
// independently framed, and integers are fixed-width big-endian values.
func legacyInventoryDigest(domain string, fields ...[]byte) [sha256.Size]byte {
	payload := make([]byte, 0, 256)
	payload = legacyAppendFramed(payload, []byte(domain))
	for _, field := range fields {
		payload = legacyAppendFramed(payload, field)
	}
	return sha256.Sum256(payload)
}

func legacyAppendFramed(payload, field []byte) []byte {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(field)))
	payload = append(payload, size[:]...)
	return append(payload, field...)
}

func legacyUint64(value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return encoded[:]
}

type legacyRecordInventory struct {
	session *legacyServiceSession
}

func (inventory legacyRecordInventory) CurrentDeletionSnapshot(ctx context.Context, actor recordauth.ActorScope, recordID string) (recorddeletion.DeletionRecordSnapshot, error) {
	if ctx == nil || inventory.session == nil || inventory.session.parent == nil ||
		recordID != inventory.session.config.RecordID || actor.CanonicalHash() != inventory.session.actor.CanonicalHash() {
		return recorddeletion.DeletionRecordSnapshot{}, errLegacyInventoryUnavailable
	}
	before, err := inventory.session.parent.inventoryObserve(ctx)
	if err != nil {
		return recorddeletion.DeletionRecordSnapshot{}, err
	}
	facts, factsErr := legacyReadRecordFacts(ctx, inventory.session.pool, inventory.session.gate, inventory.session.records, actor, recordID)
	after, err := inventory.session.parent.inventoryObserve(ctx)
	if err != nil {
		return recorddeletion.DeletionRecordSnapshot{}, err
	}
	if before != after {
		return recorddeletion.DeletionRecordSnapshot{}, errLegacyInventoryUnavailable
	}
	if factsErr != nil {
		return recorddeletion.DeletionRecordSnapshot{}, factsErr
	}
	scope := recordauth.ResourceScope{
		Version:    recordauth.ResourceScopeVersionV1,
		ProjectID:  facts.Current.Evidence.ProjectID,
		Visibility: facts.Current.Evidence.Visibility,
		Sources:    append([]recordauth.SourceAuthorization(nil), facts.Current.Evidence.Sources...),
	}
	if err := recordauth.Authorize(actor, recordauth.CapabilityRecordPermanentDelete, scope); err != nil {
		return recorddeletion.DeletionRecordSnapshot{}, err
	}
	snapshot := recorddeletion.DeletionRecordSnapshot{
		RecordID:                 facts.Current.RecordID,
		CurrentRevisionID:        facts.Current.CurrentRevisionID,
		LockVersion:              facts.Current.LockVersion,
		AuthorizationEpoch:       facts.Current.AuthorizationEpoch,
		ContentDeliveryEpoch:     facts.DeliveryEpoch,
		Authorization:            scope,
		DependencyGraphDigest:    before.dependency,
		BackupInventoryDigest:    before.backup,
		ProcessorInventoryDigest: before.processor,
		ManagedBackup: recorddeletion.ManagedBackupSummary{
			RetainedCopyCount:    0,
			MaximumRetentionDays: 0,
			LatestExpiresAt:      time.Time{},
		},
	}
	if snapshot.Validate() != nil {
		return recorddeletion.DeletionRecordSnapshot{}, errLegacyInventoryUnavailable
	}
	return snapshot, nil
}

// These adapters cover only the explicitly admitted empty client domains. They
// deliberately do not implement the purge or receipt interfaces.
type legacyEmptyClientAdapter struct {
	name       recorddeletion.AdapterName
	parent     *legacyParent
	descriptor recorddeletion.AdapterDescriptor
}

func legacyNewEmptyClientAdapter(name recorddeletion.AdapterName, parent *legacyParent) (*legacyEmptyClientAdapter, error) {
	var surfaces []recorddeletion.SurfaceName
	switch name {
	case recorddeletion.AdapterNameRecordMarkdownClient:
		surfaces = []recorddeletion.SurfaceName{
			"c80_browser_indexeddb_record_drafts", "c80_browser_record_workspace_memory",
		}
	case recorddeletion.AdapterNameRecordComparison:
		surfaces = []recorddeletion.SurfaceName{
			"c80_comparison_result_dependencies", "c80_browser_comparison_workbench", "c80_browser_comparison_save_attempts",
		}
	default:
		return nil, recorddeletion.ErrInvalidAdapterDescriptor
	}
	if parent == nil {
		return nil, recorddeletion.ErrInvalidAdapterDescriptor
	}
	descriptor, err := recorddeletion.NewAdapterDescriptor(name, surfaces)
	if err != nil {
		return nil, err
	}
	return &legacyEmptyClientAdapter{name: name, parent: parent, descriptor: descriptor}, nil
}

func (adapter *legacyEmptyClientAdapter) Descriptor() recorddeletion.AdapterDescriptor {
	if adapter == nil {
		return recorddeletion.AdapterDescriptor{}
	}
	return adapter.descriptor
}

func (adapter *legacyEmptyClientAdapter) inventoryFields(observation legacyInventoryObservation) [][]byte {
	fields := [][]byte{observation.realm[:], observation.catalog[:], observation.client[:]}
	if adapter.name == recorddeletion.AdapterNameRecordComparison {
		fields = append(fields, observation.comparison[:])
	}
	return fields
}

func (adapter *legacyEmptyClientAdapter) HealthSnapshot(ctx context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	if adapter == nil || adapter.parent == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	observation, err := adapter.parent.inventoryObserve(ctx)
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	surfaces := adapter.descriptor.Surfaces()
	encoded := append([]byte(nil), legacyUint64(uint64(len(surfaces)))...)
	for _, surface := range surfaces {
		encoded = legacyAppendFramed(encoded, []byte(surface))
	}
	fields := [][]byte{[]byte(adapter.name), encoded}
	fields = append(fields, adapter.inventoryFields(observation)...)
	proof := legacyInventoryDigest("HOUFENG-C80-ADAPTER-HEALTH-V1", fields...)
	return recorddeletion.NewAdapterHealthSnapshot(true, adapter.parent.generation, proof)
}

func (adapter *legacyEmptyClientAdapter) PreviewDeletion(ctx context.Context, target recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	if adapter == nil || adapter.parent == nil || target.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	observation, err := adapter.parent.inventoryObserve(ctx)
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	if target.DependencyGraphDigest != observation.dependency {
		return recorddeletion.AdapterPreviewSnapshot{}, errLegacyInventoryUnavailable
	}
	fields := [][]byte{
		[]byte(adapter.name), []byte(target.Object.ProjectID), []byte(target.Object.ObjectKind), []byte(target.Object.ObjectID),
		[]byte(target.CurrentRevisionID), legacyUint64(target.LockVersion), legacyUint64(target.AuthorizationEpoch),
		legacyUint64(uint64(target.ContentDeliveryEpoch)), target.DependencyGraphDigest[:],
	}
	fields = append(fields, adapter.inventoryFields(observation)...)
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: legacyInventoryDigest("HOUFENG-C80-ADAPTER-DEPENDENCY-V1", fields...),
		ImpactDigest:     legacyInventoryDigest("HOUFENG-C80-ADAPTER-IMPACT-V1", fields...),
		SurvivingCopies:  []recorddeletion.AdapterSurvivingCopy{},
	}
	if preview.Validate() != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return preview, nil
}

// The command structs contain only content-free persisted facts. The raw
// deletion token and the authorization snapshot never enter this protocol.
type legacyCreateCommand struct {
	ProjectID              string `json:"project_id"`
	ObjectKind             string `json:"object_kind"`
	ObjectID               string `json:"object_id"`
	ActorScopeDigest       string `json:"actor_scope_digest"`
	TokenCommitment        string `json:"token_commitment"`
	RequestFingerprint     string `json:"request_fingerprint"`
	BindingDigest          string `json:"binding_digest"`
	CurrentRevisionID      string `json:"current_revision_id"`
	LockVersion            uint64 `json:"lock_version"`
	AuthorizationEpoch     uint64 `json:"authorization_epoch"`
	ContentDeliveryEpoch   uint64 `json:"content_delivery_epoch"`
	DependencyDigest       string `json:"dependency_digest"`
	BackupDigest           string `json:"backup_digest"`
	ProcessorDigest        string `json:"processor_digest"`
	AdapterReadinessDigest string `json:"adapter_readiness_digest"`
	AdapterPreviewDigest   string `json:"adapter_preview_digest"`
	WitnessSequence        uint64 `json:"witness_sequence"`
	WitnessHash            string `json:"witness_hash"`
	TTLMicroseconds        uint64 `json:"ttl_microseconds"`
}

type legacyReserveCommand struct {
	CreateCommandDigest     string `json:"create_command_digest"`
	ReservationID           string `json:"reservation_id"`
	DeploymentID            string `json:"deployment_id"`
	ActorID                 string `json:"actor_id"`
	DeletionContractVersion uint64 `json:"deletion_contract_version"`
	RequestFingerprint      string `json:"request_fingerprint"`
	ExpectedBindingDigest   string `json:"expected_binding_digest"`
	ObservedWitnessSequence uint64 `json:"observed_witness_sequence"`
	ObservedWitnessHash     string `json:"observed_witness_hash"`
	OwnerID                 string `json:"owner_id"`
	OwnerLeaseMicroseconds  uint64 `json:"owner_lease_microseconds"`
	ReasonCode              string `json:"reason_code"`
}

func legacyHex(digest [sha256.Size]byte) string {
	return hex.EncodeToString(digest[:])
}

// Re-marshal object keys through encoding/json's sorted map encoding so the
// digest does not depend on the Go declaration order of a command struct.
func legacyCommandDigest(command any) (string, error) {
	body, err := json.Marshal(command)
	if err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || len(fields) == 0 {
		return "", errors.New("invalid effect command")
	}
	canonical, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return legacyHex(digest), nil
}

type legacyObservedRepository struct {
	repository          *store.PostgresRecordDeletionRepository
	parent              *legacyParent
	config              legacyServiceConfig
	actorDigest         [sha256.Size]byte
	createAttempted     bool
	previewAcknowledged bool
	reserveAttempted    bool
	reserveAcknowledged bool
	create              legacyCreateCommand
	createDigest        string
	stored              recorddeletion.StoredPreview
}

func (repository *legacyObservedRepository) CreatePreview(ctx context.Context, command recorddeletion.CreatePreviewCommand) (recorddeletion.StoredPreview, error) {
	if repository == nil || repository.repository == nil || repository.parent == nil || repository.createAttempted ||
		command.Validate() != nil || command.Object.ProjectID != repository.config.ProjectID ||
		command.Object.ObjectID != repository.config.RecordID || command.ActorScopeDigest != repository.actorDigest {
		return recorddeletion.StoredPreview{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	repository.createAttempted = true
	fingerprint, err := command.RequestFingerprint.PersistedBytes()
	if err != nil || command.TTL.Microseconds() <= 0 {
		return recorddeletion.StoredPreview{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	intent := legacyCreateCommand{
		ProjectID: command.Object.ProjectID, ObjectKind: command.Object.ObjectKind, ObjectID: command.Object.ObjectID,
		ActorScopeDigest: legacyHex(command.ActorScopeDigest), TokenCommitment: legacyHex(command.TokenCommitment),
		RequestFingerprint: legacyHex(fingerprint), BindingDigest: legacyHex(command.BindingDigest),
		CurrentRevisionID: command.Record.CurrentRevisionID, LockVersion: command.Record.LockVersion,
		AuthorizationEpoch: command.Record.AuthorizationEpoch, ContentDeliveryEpoch: uint64(command.Record.ContentDeliveryEpoch),
		DependencyDigest: legacyHex(command.Record.DependencyGraphDigest), BackupDigest: legacyHex(command.Record.BackupInventoryDigest),
		ProcessorDigest: legacyHex(command.Record.ProcessorInventoryDigest), AdapterReadinessDigest: legacyHex(command.AdapterReadinessDigest),
		AdapterPreviewDigest: legacyHex(command.AdapterPreviewDigest), WitnessSequence: command.WitnessHead.Sequence,
		WitnessHash: legacyHex(command.WitnessHead.EntryHash), TTLMicroseconds: uint64(command.TTL.Microseconds()),
	}
	digest, err := legacyCommandDigest(intent)
	if err != nil {
		return recorddeletion.StoredPreview{}, err
	}
	request := struct {
		Op              string              `json:"op"`
		PhaseGeneration uint64              `json:"phase_generation"`
		Command         legacyCreateCommand `json:"command"`
	}{"preview_intent", repository.parent.generation, intent}
	fields, err := repository.parent.exchange(ctx, request, "op", "phase_generation", "command_digest")
	var acknowledged string
	if err != nil || !repository.parent.responseIdentity(fields, "preview_intent_ack") ||
		json.Unmarshal(fields["command_digest"], &acknowledged) != nil || acknowledged != digest {
		repository.parent.invalid = true
		return recorddeletion.StoredPreview{}, errors.New("preview intent unconfirmed")
	}
	repository.create, repository.createDigest = intent, digest
	stored, err := repository.repository.CreatePreview(ctx, command)
	if err != nil {
		return recorddeletion.StoredPreview{}, err
	}
	if stored.Validate() != nil || stored.ExpiresAt.IsZero() || stored.ExpiresAt.Nanosecond()%1000 != 0 {
		return recorddeletion.StoredPreview{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	result := struct {
		Op              string `json:"op"`
		PhaseGeneration uint64 `json:"phase_generation"`
		CommandDigest   string `json:"command_digest"`
		ReservationID   string `json:"reservation_id"`
		ExpiresAt       string `json:"expires_at"`
	}{"preview_result", repository.parent.generation, digest, stored.ReservationID, stored.ExpiresAt.UTC().Format(time.RFC3339Nano)}
	fields, err = repository.parent.exchange(ctx, result, "op", "phase_generation", "command_digest", "reservation_id")
	var returnedDigest, reservationID string
	if err != nil || !repository.parent.responseIdentity(fields, "preview_result_ack") ||
		json.Unmarshal(fields["command_digest"], &returnedDigest) != nil || returnedDigest != digest ||
		json.Unmarshal(fields["reservation_id"], &reservationID) != nil || reservationID != stored.ReservationID {
		repository.parent.invalid = true
		return recorddeletion.StoredPreview{}, errors.New("preview result unconfirmed")
	}
	repository.stored = stored
	repository.previewAcknowledged = true
	return stored, nil
}

func (repository *legacyObservedRepository) ResolvePreview(ctx context.Context, lookup recorddeletion.PreviewLookup) (recorddeletion.StoredPreview, error) {
	return repository.repository.ResolvePreview(ctx, lookup)
}

func (repository *legacyObservedRepository) ResolveOperationStatus(ctx context.Context, projectID recordplatform.ProjectID, operationID string) (recorddeletion.DeletionOperationStatus, error) {
	return repository.repository.ResolveOperationStatus(ctx, projectID, operationID)
}

func (repository *legacyObservedRepository) ReservePreview(ctx context.Context, command recorddeletion.ReservePreviewCommand) (recorddeletion.DeletionOperation, error) {
	if repository == nil || repository.repository == nil || repository.parent == nil || !repository.previewAcknowledged ||
		repository.reserveAttempted || command.Validate() != nil || command.Preview.ReservationID != repository.stored.ReservationID ||
		command.Preview.Object != repository.stored.Object || command.Preview.ActorScopeDigest != repository.stored.ActorScopeDigest ||
		command.Preview.TokenCommitment != repository.stored.TokenCommitment || command.Preview.BindingDigest != repository.stored.BindingDigest ||
		command.Preview.WitnessHead != repository.stored.WitnessHead || !command.Preview.ExpiresAt.Equal(repository.stored.ExpiresAt) ||
		command.Record.CurrentRevisionID != repository.create.CurrentRevisionID || command.Record.LockVersion != repository.create.LockVersion ||
		command.Record.AuthorizationEpoch != repository.create.AuthorizationEpoch || uint64(command.Record.ContentDeliveryEpoch) != repository.create.ContentDeliveryEpoch ||
		legacyHex(command.Record.DependencyGraphDigest) != repository.create.DependencyDigest || legacyHex(command.Record.BackupInventoryDigest) != repository.create.BackupDigest ||
		legacyHex(command.Record.ProcessorInventoryDigest) != repository.create.ProcessorDigest ||
		string(command.DeploymentID) != repository.config.DeploymentID || command.ActorID != repository.config.ActorUserID ||
		command.OwnerLeaseDuration.Microseconds() <= 0 {
		return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	repository.reserveAttempted = true
	fingerprint, err := command.RequestFingerprint.PersistedBytes()
	if err != nil || legacyHex(fingerprint) != repository.create.RequestFingerprint {
		return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	intent := legacyReserveCommand{
		CreateCommandDigest: repository.createDigest, ReservationID: command.Preview.ReservationID,
		DeploymentID: string(command.DeploymentID), ActorID: command.ActorID, DeletionContractVersion: command.DeletionContractVersion,
		RequestFingerprint: legacyHex(fingerprint), ExpectedBindingDigest: legacyHex(command.ExpectedBindingDigest),
		ObservedWitnessSequence: command.ObservedWitnessHead.Sequence, ObservedWitnessHash: legacyHex(command.ObservedWitnessHead.EntryHash),
		OwnerID: command.OwnerID, OwnerLeaseMicroseconds: uint64(command.OwnerLeaseDuration.Microseconds()), ReasonCode: string(command.ReasonCode),
	}
	digest, err := legacyCommandDigest(intent)
	if err != nil {
		return recorddeletion.DeletionOperation{}, err
	}
	request := struct {
		Op              string               `json:"op"`
		PhaseGeneration uint64               `json:"phase_generation"`
		Command         legacyReserveCommand `json:"command"`
	}{"reserve_intent", repository.parent.generation, intent}
	fields, err := repository.parent.exchange(ctx, request, "op", "phase_generation", "command_digest")
	var acknowledged string
	if err != nil || !repository.parent.responseIdentity(fields, "reserve_intent_ack") ||
		json.Unmarshal(fields["command_digest"], &acknowledged) != nil || acknowledged != digest {
		repository.parent.invalid = true
		return recorddeletion.DeletionOperation{}, errors.New("reserve intent unconfirmed")
	}
	operation, err := repository.repository.ReservePreview(ctx, command)
	if err != nil {
		return recorddeletion.DeletionOperation{}, err
	}
	if operation.Validate() != nil || operation.State != recorddeletion.DeletionStateProvisionalFenced ||
		operation.ReservationID != command.Preview.ReservationID || operation.Object != command.Preview.Object ||
		operation.ReasonCode != command.ReasonCode {
		return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := repository.parent.reserveResult(ctx, digest, operation.OperationID); err != nil {
		return recorddeletion.DeletionOperation{}, err
	}
	repository.reserveAcknowledged = true
	return operation, nil
}

func (parent *legacyParent) reserveResult(ctx context.Context, commandDigest, operationID string) error {
	if ctx == nil || ctx.Err() != nil || parent == nil || parent.conn == nil || parent.invalid ||
		!parent.readyAcknowledged || parent.generation == 0 {
		return errors.New("service supervision unavailable")
	}
	result := struct {
		Op              string `json:"op"`
		PhaseGeneration uint64 `json:"phase_generation"`
		CommandDigest   string `json:"command_digest"`
		OperationID     string `json:"operation_id"`
	}{"reserve_result", parent.generation, commandDigest, operationID}
	fields, err := parent.exchange(ctx, result, "op", "phase_generation", "command_digest", "operation_id")
	var returnedDigest, returnedOperation string
	if err != nil || !parent.responseIdentity(fields, "reserve_result_ack") ||
		json.Unmarshal(fields["command_digest"], &returnedDigest) != nil || returnedDigest != commandDigest ||
		json.Unmarshal(fields["operation_id"], &returnedOperation) != nil || returnedOperation != operationID {
		parent.invalid = true
		return errors.New("reserve result unconfirmed")
	}
	if ctx.Err() != nil {
		return errors.New("service supervision unavailable")
	}
	return nil
}

type legacyServiceResult struct {
	Version         uint64 `json:"version"`
	PhaseGeneration uint64 `json:"phase_generation"`
	Status          string `json:"status"`
	OperationID     string `json:"operation_id"`
	Reason          string `json:"reason"`
}

// legacyWriteServiceResult writes the exact bytes that the parent must verify
// and keeps the authenticated channel open until their durable acknowledgement.
func legacyWriteServiceResult(ctx context.Context, output io.Writer, parent *legacyParent, result legacyServiceResult) error {
	if ctx == nil || ctx.Err() != nil || output == nil || parent == nil || parent.conn == nil ||
		parent.invalid || !parent.readyAcknowledged || parent.generation == 0 ||
		result.Version != 1 || result.PhaseGeneration != parent.generation {
		return errors.New("service supervision unavailable")
	}
	_, ok := ctx.Deadline()
	if !ok {
		return errors.New("service deadline unavailable")
	}
	switch result.Status {
	case "committed":
		if result.Reason != "app_reserved" || len(result.OperationID) == 0 || len(result.OperationID) > 68 {
			return errors.New("invalid service result")
		}
	case "rejected":
		if result.Reason != "source_denied" || result.OperationID != "" {
			return errors.New("invalid service result")
		}
	case "unavailable":
		if result.OperationID != "" || (result.Reason != "inventory_unavailable" &&
			result.Reason != "admission_unavailable" && result.Reason != "service_unavailable") {
			return errors.New("invalid service result")
		}
	default:
		return errors.New("invalid service result")
	}
	body, err := json.Marshal(result)
	if err != nil || len(body) == 0 || len(body)+1 > legacyFrameLimit {
		return errors.New("invalid service result")
	}
	body = append(body, '\n')

	// The launcher supplies a pipe. Use a single nonblocking kernel write so
	// a short write, interrupted write, or full pipe is never retried.
	var n int
	if file, ok := output.(*os.File); ok {
		info, statErr := file.Stat()
		if statErr != nil || info.Mode()&os.ModeNamedPipe == 0 {
			return errors.New("service output unavailable")
		}
		fd := int(file.Fd())
		if unix.SetNonblock(fd, true) != nil {
			return errors.New("service output unavailable")
		}
		if ctx.Err() != nil {
			return errors.New("service supervision unavailable")
		}
		n, err = unix.Write(fd, body)
		runtime.KeepAlive(file)
	} else {
		if ctx.Err() != nil {
			return errors.New("service supervision unavailable")
		}
		n, err = output.Write(body)
	}
	if err != nil || n != len(body) {
		return errors.New("service output incomplete")
	}
	digest := legacyHex(sha256.Sum256(body))
	request := struct {
		Op              string `json:"op"`
		PhaseGeneration uint64 `json:"phase_generation"`
		ResultDigest    string `json:"result_digest"`
	}{"result_written", parent.generation, digest}
	fields, err := parent.exchange(ctx, request, "op", "phase_generation", "result_digest")
	var acknowledged string
	if err != nil || !parent.responseIdentity(fields, "result_written_ack") ||
		json.Unmarshal(fields["result_digest"], &acknowledged) != nil || acknowledged != digest {
		parent.invalid = true
		return errors.New("service result unconfirmed")
	}
	if ctx.Err() != nil {
		return errors.New("service supervision unavailable")
	}
	return nil
}

// legacyRunService owns the partial or complete session through the result ACK.
func legacyRunService(ctx context.Context, output io.Writer) error {
	if ctx == nil || ctx.Err() != nil || output == nil {
		return errors.New("service supervision unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	session, generation, setupErr := legacyOpenServiceSession(ctx)
	if session == nil {
		return errors.New("service supervision unavailable")
	}
	defer session.Close()
	if session.parent == nil || !session.parent.readyAcknowledged || session.parent.invalid ||
		generation == 0 || generation != session.parent.generation || ctx.Err() != nil {
		return errors.New("service supervision unavailable")
	}
	result, err := legacyServiceOutcome(ctx, session, generation, setupErr)
	if err != nil {
		return err
	}
	return legacyWriteServiceResult(ctx, output, session.parent, result)
}

func legacyServiceOutcome(ctx context.Context, session *legacyServiceSession, generation uint64, setupErr error) (legacyServiceResult, error) {
	result := legacyServiceResult{Version: 1, PhaseGeneration: generation, Status: "unavailable", Reason: "service_unavailable"}
	if setupErr != nil {
		if session.parent.invalid || ctx.Err() != nil {
			return legacyServiceResult{}, errors.New("service supervision unavailable")
		}
		if errors.Is(setupErr, errLegacyAdmissionUnavailable) {
			result.Reason = "admission_unavailable"
		} else if errors.Is(setupErr, errLegacyInventoryUnavailable) {
			result.Reason = "inventory_unavailable"
		}
		return result, nil
	}
	unavailable := func(cause error) (legacyServiceResult, error) {
		if session.parent.invalid || ctx.Err() != nil {
			return legacyServiceResult{}, errors.New("service supervision unavailable")
		}
		if errors.Is(cause, errLegacyInventoryUnavailable) {
			result.Reason = "inventory_unavailable"
		}
		return result, nil
	}
	markdown, err := legacyNewEmptyClientAdapter(recorddeletion.AdapterNameRecordMarkdownClient, session.parent)
	if err != nil {
		return unavailable(err)
	}
	comparison, err := legacyNewEmptyClientAdapter(recorddeletion.AdapterNameRecordComparison, session.parent)
	if err != nil {
		return unavailable(err)
	}
	adapters := append(session.adapters, markdown, comparison)
	registry, err := recorddeletion.NewRegistry(adapters)
	if err != nil {
		return unavailable(err)
	}
	repository := &legacyObservedRepository{
		repository: store.NewPostgresRecordDeletionRepository(session.pool, session.gate),
		parent:     session.parent, config: session.config, actorDigest: session.actor.CanonicalHash(),
	}
	service, err := recorddeletion.NewService(recordplatform.DeploymentID(session.config.DeploymentID), registry,
		legacyRecordInventory{session: session}, session.witness, repository,
		recorddeletion.ServiceOptions{OwnerID: "c80_predecessor", OwnerLeaseDuration: 30 * time.Second})
	if err != nil {
		return unavailable(err)
	}
	preview, err := service.Preview(ctx, recorddeletion.PreviewRequest{Actor: session.actor, RecordID: session.config.RecordID})
	if err != nil {
		if !session.parent.invalid && ctx.Err() == nil && !errors.Is(err, errLegacyInventoryUnavailable) &&
			session.parent.denied && !repository.createAttempted {
			result.Status, result.Reason = "rejected", "source_denied"
			return result, nil
		}
		return unavailable(err)
	}
	if session.parent.denied {
		return legacyServiceResult{}, errors.New("inconsistent source decision")
	}
	token, err := recordplatform.ParseDeletionRequestTokenTransportV1(preview.Token.Transport())
	if err != nil {
		return unavailable(err)
	}
	request := recorddeletion.ExecuteRequest{
		Actor: session.actor, RecordID: session.config.RecordID, ReservationID: preview.ReservationID,
		Token: token, ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
	}
	operation, err := service.Execute(ctx, request)
	if err != nil {
		return unavailable(err)
	}
	if operation.Validate() != nil || operation.State != recorddeletion.DeletionStateProvisionalFenced ||
		operation.ReservationID != preview.ReservationID || operation.ReasonCode != recorddeletion.DeletionReasonUserConfirmed || !repository.reserveAcknowledged {
		return unavailable(recorddeletion.ErrDeletionSafetyUnavailable)
	}
	replayed, err := service.Execute(ctx, request)
	if err != nil || replayed != operation {
		return unavailable(recorddeletion.ErrDeletionSafetyUnavailable)
	}
	result.Status, result.OperationID, result.Reason = "committed", operation.OperationID, "app_reserved"
	return result, nil
}
