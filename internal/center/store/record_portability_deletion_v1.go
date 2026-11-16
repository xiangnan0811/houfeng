package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
)

// ConfigurePortabilityDeletionBlobStore binds the S3 route once for the
// lifetime of this repository. A matching route does not establish bucket
// incarnation or grant authority to delete an object.
func (repository *PostgresRecordDeletionRepository) ConfigurePortabilityDeletionBlobStore(backend attachments.BackendKind, deleter attachments.BlobGCDeleter) error {
	if repository == nil || backend != attachments.BackendKindS3 || nilAttachmentDeletionBlobStore(deleter) {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	identityProvider, ok := deleter.(attachments.BlobGCStorageIdentity)
	if !ok {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	identity, err := identityProvider.GCStorageIdentity()
	if err != nil || identity == ([sha256.Size]byte{}) {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	repository.portabilityBlobMu.Lock()
	defer repository.portabilityBlobMu.Unlock()
	if repository.portabilityBlob != nil && (!sameAttachmentDeletionBlobStore(repository.portabilityBlob, deleter) || repository.portabilityBlobIdentity != identity) {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	repository.portabilityBlob = deleter
	repository.portabilityBlobIdentity = identity
	return nil
}

// portabilityV1LocalCleanupRepository supplies a fresh caller-observed legacy
// authority to every transaction while leaving the physical GC wire unchanged.
type portabilityV1LocalCleanupRepository struct {
	repository *PostgresRecordDeletionRepository
	target     recorddeletion.PurgeTarget
	surface    [sha256.Size]byte
}

var _ attachments.LocalCleanupRepository = (*portabilityV1LocalCleanupRepository)(nil)

func (r *portabilityV1LocalCleanupRepository) command(ctx context.Context, statement string, request any) ([]byte, error) {
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var result []byte
	err = r.repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, tx *RecordPlatformTransaction) error {
		authority, err := legacyAdapterPurgeEnvelope(ctx, tx.tx, r.target, r.surface)
		if err != nil {
			return err
		}
		body, err := json.Marshal(struct {
			Authority json.RawMessage `json:"authority"`
			Request   json.RawMessage `json:"request"`
		}{authority, requestBytes})
		if err != nil {
			return err
		}
		return tx.tx.QueryRow(ctx, statement, body).Scan(&result)
	})
	if err != nil {
		return nil, fmt.Errorf("run V1 portability local cleanup: %w", err)
	}
	return result, nil
}

func (r *portabilityV1LocalCleanupRepository) PrepareLocalCleanup(ctx context.Context, request attachments.LocalCleanupPreparation) (attachments.LocalCleanupTask, error) {
	var result attachments.LocalCleanupTask
	data, err := r.command(ctx, `select public.record_portability_local_prepare_v1($1)`, request)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	return result, normalizeLocalCleanupDigests(&result)
}

func (r *portabilityV1LocalCleanupRepository) snapshot(ctx context.Context, statement string, request any) (attachments.LocalCleanupSnapshot, error) {
	var result attachments.LocalCleanupSnapshot
	data, err := r.command(ctx, statement, request)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	return result, normalizeLocalCleanupDigests(&result.Task)
}

func (r *portabilityV1LocalCleanupRepository) ClaimLocalCleanup(ctx context.Context, request attachments.LocalCleanupClaim) (attachments.LocalCleanupSnapshot, error) {
	return r.snapshot(ctx, `select public.record_portability_local_claim_v1($1)`, request)
}
func (r *portabilityV1LocalCleanupRepository) ResolveLocalCleanup(ctx context.Context, request attachments.LocalCleanupIdentity) (attachments.LocalCleanupSnapshot, error) {
	return r.snapshot(ctx, `select public.record_portability_local_resolve_v1($1)`, request)
}
func (r *portabilityV1LocalCleanupRepository) BindLocalCleanupFinal(ctx context.Context, request attachments.LocalCleanupFinalBinding) (attachments.LocalCleanupSnapshot, error) {
	return r.snapshot(ctx, `select public.record_portability_local_bind_final_v1($1)`, request)
}
func (r *portabilityV1LocalCleanupRepository) CompleteLocalCleanup(ctx context.Context, request attachments.LocalCleanupCompletion) (attachments.LocalCleanupSnapshot, error) {
	return r.snapshot(ctx, `select public.record_portability_local_complete_v1($1)`, request)
}

type portabilityV1S3Claim struct {
	attachmentPurgeV1Claim
	StorageBinding string `json:"storage_binding"`
}

type portabilityV1S3Stored struct {
	Claim          portabilityV1S3Claim `json:"claim"`
	State          string               `json:"deletion_state"`
	RetryAt        *string              `json:"retry_at"`
	PhysicalResult *string              `json:"physical_delete_result"`
	ReceiptDigest  *string              `json:"receipt_digest"`
	CompletedAt    *string              `json:"completed_at"`
}

type portabilityV1S3Receipt struct {
	Key            string `json:"key"`
	VersionID      string `json:"version_id"`
	SHA256         string `json:"sha256"`
	SizeBytes      int64  `json:"size_bytes"`
	Deleted        bool   `json:"deleted"`
	StorageBinding string `json:"storage_binding"`
}

func decodePortabilityV1S3Stored(raw []byte, target recorddeletion.PurgeTarget) (portabilityV1S3Stored, error) {
	var result portabilityV1S3Stored
	if err := attachmentPurgeV1ValidateJSON(raw); err != nil {
		return result, err
	}
	fields, err := attachmentPurgeV1Exact(raw, "claim", "deletion_state", "retry_at", "physical_delete_result", "receipt_digest", "completed_at")
	if err != nil {
		return result, err
	}
	claimFields, err := attachmentPurgeV1Exact(fields["claim"], "operation_id", "deletion_id", "project_id", "backend_kind", "key", "version_id", "sha256", "size_bytes", "blob_created_at", "cleanup_owner_id", "cleanup_owner_generation", "attempt", "cleanup_expires_at", "storage_binding")
	if err != nil {
		return result, err
	}
	for key := range claimFields {
		if err := attachmentPurgeV1NonNull(claimFields, key); err != nil {
			return result, err
		}
	}
	if err := attachmentPurgeV1NonNull(fields, "claim", "deletion_state"); err != nil || json.Unmarshal(raw, &result) != nil {
		return portabilityV1S3Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	claim, err := result.Claim.attachmentPurgeV1Claim.blobClaim(attachments.AttachmentDeletionCommand{PurgeTarget: target})
	if err != nil || result.Claim.VersionID == "null" {
		return portabilityV1S3Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	binding, err := attachmentPurgeV1Digest(result.Claim.StorageBinding)
	if err != nil {
		return portabilityV1S3Stored{}, err
	}
	switch result.State {
	case "claimed":
		if result.RetryAt != nil || result.PhysicalResult != nil || result.ReceiptDigest != nil || result.CompletedAt != nil {
			return portabilityV1S3Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
	case "retry_wait":
		if result.RetryAt == nil || result.PhysicalResult != nil || result.ReceiptDigest != nil || result.CompletedAt != nil {
			return portabilityV1S3Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		if _, err := attachmentPurgeV1Time(*result.RetryAt); err != nil {
			return portabilityV1S3Stored{}, err
		}
	case "completed":
		if result.RetryAt != nil || result.PhysicalResult == nil || result.ReceiptDigest == nil || result.CompletedAt == nil ||
			(*result.PhysicalResult != "deleted" && *result.PhysicalResult != "already_absent") {
			return portabilityV1S3Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		if _, err := attachmentPurgeV1Time(*result.CompletedAt); err != nil {
			return portabilityV1S3Stored{}, err
		}
		value, err := attachmentPurgeV1Digest(*result.ReceiptDigest)
		if err != nil {
			return portabilityV1S3Stored{}, err
		}
		want := digestRecordPurgeBytes("houfeng.record-portability.blob-gc-receipt.v1", []byte(claim.DeletionID), []byte(claim.ProjectID),
			[]byte("permanent"), []byte(claim.Candidate.Object.Key), claim.Candidate.Object.SHA256[:],
			[]byte(claim.Candidate.Object.ObjectVersion), recordPurgeUint64(uint64(claim.Candidate.Object.SizeBytes)),
			[]byte("s3"), binding, []byte(*result.PhysicalResult))
		if !bytes.Equal(value, want[:]) {
			return portabilityV1S3Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
	default:
		return portabilityV1S3Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return result, nil
}

func (repository *PostgresRecordDeletionRepository) portabilityV1Blob(expected string) (attachments.BlobGCDeleter, error) {
	repository.portabilityBlobMu.Lock()
	defer repository.portabilityBlobMu.Unlock()
	if nilAttachmentDeletionBlobStore(repository.portabilityBlob) {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	identityProvider, ok := repository.portabilityBlob.(attachments.BlobGCStorageIdentity)
	if !ok {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	identity, err := identityProvider.GCStorageIdentity()
	if err != nil || identity != repository.portabilityBlobIdentity || hex.EncodeToString(identity[:]) != expected {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return repository.portabilityBlob, nil
}

func (repository *PostgresRecordDeletionRepository) portabilityV1Command(ctx context.Context, target recorddeletion.PurgeTarget,
	surface [sha256.Size]byte, query string, extra any, beforeCommit func([]byte) error) ([]byte, error) {
	var result []byte
	candidate := false
	err := repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, tx *RecordPlatformTransaction) error {
		authority, err := legacyAdapterPurgeEnvelope(ctx, tx.tx, target, surface)
		if err != nil {
			return err
		}
		body := authority
		if extra != nil {
			encoded, err := json.Marshal(extra)
			if err != nil {
				return err
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil || fields == nil {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			fields["authority"] = authority
			body, err = json.Marshal(fields)
			if err != nil {
				return err
			}
		}
		if err := tx.tx.QueryRow(ctx, query, body).Scan(&result); err != nil {
			return err
		}
		if err := attachmentPurgeV1ValidateJSON(result); err != nil {
			return err
		}
		if beforeCommit != nil {
			if err := beforeCommit(result); err != nil {
				return err
			}
		}
		candidate = true
		return nil
	})
	if err != nil && !candidate {
		return nil, err
	}
	return result, err
}

func (repository *PostgresRecordDeletionRepository) portabilityV1S3Command(ctx context.Context, target recorddeletion.PurgeTarget,
	surface [sha256.Size]byte, query string, request any) (*portabilityV1S3Stored, error) {
	raw, err := repository.portabilityV1Command(ctx, target, surface, query, request, nil)
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, err
	}
	stored, decodeErr := decodePortabilityV1S3Stored(raw, target)
	if decodeErr != nil {
		return nil, errors.Join(err, decodeErr)
	}
	return &stored, err
}

func (repository *PostgresRecordDeletionRepository) executePortabilityV1S3(ctx context.Context, target recorddeletion.PurgeTarget,
	surface [sha256.Size]byte, deletionID string) error {
	repository.portabilityBlobMu.Lock()
	configured := hex.EncodeToString(repository.portabilityBlobIdentity[:])
	repository.portabilityBlobMu.Unlock()
	if _, err := repository.portabilityV1Blob(configured); err != nil {
		return err
	}
	request := struct {
		DeletionID     string `json:"deletion_id"`
		CleanupOwnerID string `json:"cleanup_owner_id"`
		CleanupLeaseMS int64  `json:"cleanup_lease_ms"`
	}{deletionID, target.OwnerClaim.Owner.OwnerID, attachments.DefaultBlobGCLeaseDuration.Milliseconds()}
	stored, err := repository.portabilityV1S3Command(ctx, target, surface, `select public.record_portability_gc_claim_v1($1)`, request)
	if err != nil && stored != nil {
		resolveRequest := struct {
			Claim portabilityV1S3Claim `json:"claim"`
		}{stored.Claim}
		resolved, resolveErr := repository.portabilityV1S3Command(ctx, target, surface, `select public.record_portability_gc_claim_resolve_v1($1)`, resolveRequest)
		if resolveErr != nil || resolved == nil || resolved.Claim != stored.Claim ||
			(resolved.State != "claimed" && resolved.State != "completed") {
			return errors.Join(err, resolveErr, recorddeletion.ErrDeletionSafetyUnavailable)
		}
		stored, err = resolved, nil
	}
	if err != nil {
		return err
	}
	if stored == nil || stored.Claim.DeletionID != deletionID || stored.Claim.StorageBinding != configured {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if stored.State == "completed" {
		return nil
	}
	if stored.State != "claimed" || stored.Claim.CleanupOwnerID != target.OwnerClaim.Owner.OwnerID {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	claim, err := stored.Claim.attachmentPurgeV1Claim.blobClaim(attachments.AttachmentDeletionCommand{PurgeTarget: target})
	if err != nil || !claim.LeaseExpiresAt.After(time.Now()) {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	deleter, err := repository.portabilityV1Blob(stored.Claim.StorageBinding)
	if err != nil {
		return err
	}
	version := storeObjectVersionFromBlob(claim.Candidate.Object)
	receipt, deleteErr := deleter.DeleteGCVersion(ctx, version)
	if _, routeErr := repository.portabilityV1Blob(stored.Claim.StorageBinding); routeErr != nil {
		return errors.Join(deleteErr, routeErr)
	}
	if deleteErr != nil {
		retry := struct {
			Claim   portabilityV1S3Claim `json:"claim"`
			RetryAt string               `json:"retry_at"`
		}{stored.Claim, time.Now().UTC().Add(attachments.DefaultBlobGCRetryDelay).Truncate(time.Microsecond).Format(time.RFC3339Nano)}
		_, retryErr := repository.portabilityV1S3Command(ctx, target, surface, `select public.record_portability_gc_retry_v1($1)`, retry)
		return errors.Join(fmt.Errorf("delete exact portability S3 version: %w", deleteErr), retryErr)
	}
	if receipt.Version != version {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	completion := struct {
		Claim   portabilityV1S3Claim   `json:"claim"`
		Receipt portabilityV1S3Receipt `json:"receipt"`
	}{stored.Claim, portabilityV1S3Receipt{Key: version.Key, VersionID: version.VersionID, SHA256: hex.EncodeToString(version.SHA256[:]),
		SizeBytes: version.SizeBytes, Deleted: receipt.Deleted, StorageBinding: stored.Claim.StorageBinding}}
	completed, err := repository.portabilityV1S3Command(ctx, target, surface, `select public.record_portability_gc_complete_v1($1)`, completion)
	if err != nil {
		resolved, resolveErr := repository.portabilityV1S3Command(ctx, target, surface, `select public.record_portability_gc_resolve_v1($1)`, completion)
		if resolveErr != nil || resolved == nil || resolved.Claim != stored.Claim || resolved.State != "completed" {
			return errors.Join(err, resolveErr, recorddeletion.ErrDeletionSafetyUnavailable)
		}
		completed = resolved
	}
	if completed == nil || completed.Claim != stored.Claim || completed.State != "completed" || completed.PhysicalResult == nil ||
		(*completed.PhysicalResult == "deleted") != receipt.Deleted {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

type portabilityV1LocalTarget struct {
	DeletionID      string `json:"deletion_id"`
	NamespaceID     string `json:"namespace_id"`
	IncarnationID   string `json:"incarnation_id"`
	Key             string `json:"key"`
	ExpectedVersion string `json:"expected_version"`
}

type portabilityV1Preparation struct {
	InventoryDigest  string                     `json:"inventory_digest"`
	UnresolvedCount  int64                      `json:"unresolved_count"`
	RequiredBackends []attachments.BackendKind  `json:"required_backends"`
	LocalTargets     []portabilityV1LocalTarget `json:"local_targets"`
	S3Targets        []string                   `json:"s3_targets"`
}

func decodePortabilityV1Preparation(raw []byte) (portabilityV1Preparation, error) {
	var result portabilityV1Preparation
	if err := attachmentPurgeV1ValidateJSON(raw); err != nil {
		return result, err
	}
	fields, err := attachmentPurgeV1Exact(raw, "inventory_digest", "unresolved_count", "required_backends", "local_targets", "s3_targets")
	if err != nil {
		return result, err
	}
	if err := attachmentPurgeV1NonNull(fields, "inventory_digest", "unresolved_count", "required_backends", "local_targets", "s3_targets"); err != nil {
		return result, err
	}
	var targets []json.RawMessage
	if err := json.Unmarshal(fields["local_targets"], &targets); err != nil || targets == nil {
		return result, recorddeletion.ErrDeletionSafetyUnavailable
	}
	for _, rawTarget := range targets {
		targetFields, err := attachmentPurgeV1Exact(rawTarget, "deletion_id", "namespace_id", "incarnation_id", "key", "expected_version")
		if err != nil {
			return result, err
		}
		for key := range targetFields {
			if err := attachmentPurgeV1NonNull(targetFields, key); err != nil {
				return result, err
			}
		}
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.UnresolvedCount < 0 || result.RequiredBackends == nil || result.S3Targets == nil {
		return portabilityV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if _, err := attachmentPurgeV1Digest(result.InventoryDigest); err != nil {
		return portabilityV1Preparation{}, err
	}
	if len(result.RequiredBackends) > 2 || len(result.RequiredBackends) == 2 &&
		(result.RequiredBackends[0] != attachments.BackendKindLocal || result.RequiredBackends[1] != attachments.BackendKindS3) ||
		len(result.RequiredBackends) == 1 && result.RequiredBackends[0] != attachments.BackendKindLocal && result.RequiredBackends[0] != attachments.BackendKindS3 {
		return portabilityV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if result.UnresolvedCount > 0 && (len(result.RequiredBackends) != 0 || len(result.LocalTargets) != 0 || len(result.S3Targets) != 0) {
		return portabilityV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if len(result.RequiredBackends) != 0 {
		wantLocal, wantS3 := len(result.LocalTargets) != 0, len(result.S3Targets) != 0
		gotLocal, gotS3 := false, false
		for _, backend := range result.RequiredBackends {
			gotLocal = gotLocal || backend == attachments.BackendKindLocal
			gotS3 = gotS3 || backend == attachments.BackendKindS3
		}
		if gotLocal != wantLocal || gotS3 != wantS3 {
			return portabilityV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
	}
	seen := make(map[string]bool, len(result.LocalTargets)+len(result.S3Targets))
	validID := func(id string) bool {
		if len(id) != 68 || id[:4] != "bgd_" {
			return false
		}
		_, err := attachmentPurgeV1Digest(id[4:])
		return err == nil
	}
	validUUID := func(value string) bool {
		if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
			return false
		}
		for i, c := range value {
			if i == 8 || i == 13 || i == 18 || i == 23 {
				continue
			}
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
	validExpectedVersion := func(value string) bool {
		if len(value) != len("local-v2-rwo_")+32 || value[:len("local-v2-rwo_")] != "local-v2-rwo_" {
			return false
		}
		for _, c := range value[len("local-v2-rwo_"):] {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
	for _, item := range result.LocalTargets {
		if !validID(item.DeletionID) || seen[item.DeletionID] || !validUUID(item.NamespaceID) || !validUUID(item.IncarnationID) ||
			len(item.Key) != 71 || item.Key[:7] != "sha256/" || !validExpectedVersion(item.ExpectedVersion) {
			return portabilityV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		if _, err := attachmentPurgeV1Digest(item.Key[7:]); err != nil {
			return portabilityV1Preparation{}, err
		}
		seen[item.DeletionID] = true
	}
	for index, id := range result.S3Targets {
		if !validID(id) || seen[id] || index > 0 && result.S3Targets[index-1] >= id {
			return portabilityV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		seen[id] = true
	}
	return result, nil
}

func (repository *PostgresRecordDeletionRepository) validatePortabilityV1Preparation(preparation portabilityV1Preparation) error {
	if preparation.UnresolvedCount > 0 {
		return nil
	}
	if len(preparation.LocalTargets) > 0 {
		coordinator := repository.portabilityCleanup.Load()
		if coordinator == nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		for _, item := range preparation.LocalTargets {
			if coordinator.NamespaceID() != item.NamespaceID || coordinator.IncarnationID() != item.IncarnationID {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
		}
	}
	if len(preparation.S3Targets) > 0 {
		repository.portabilityBlobMu.Lock()
		identity := hex.EncodeToString(repository.portabilityBlobIdentity[:])
		repository.portabilityBlobMu.Unlock()
		if _, err := repository.portabilityV1Blob(identity); err != nil {
			return err
		}
	}
	for _, backend := range preparation.RequiredBackends {
		if backend == attachments.BackendKindLocal && repository.portabilityCleanup.Load() == nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if backend == attachments.BackendKindS3 {
			repository.portabilityBlobMu.Lock()
			identity := hex.EncodeToString(repository.portabilityBlobIdentity[:])
			repository.portabilityBlobMu.Unlock()
			if _, err := repository.portabilityV1Blob(identity); err != nil {
				return err
			}
		}
	}
	return nil
}

func (repository *PostgresRecordDeletionRepository) preparePortabilityV1(ctx context.Context, target recorddeletion.PurgeTarget,
	surface [sha256.Size]byte) (portabilityV1Preparation, error) {
	var binding *string
	repository.portabilityBlobMu.Lock()
	configured := repository.portabilityBlob != nil
	identity := hex.EncodeToString(repository.portabilityBlobIdentity[:])
	repository.portabilityBlobMu.Unlock()
	if configured {
		if _, err := repository.portabilityV1Blob(identity); err != nil {
			return portabilityV1Preparation{}, err
		}
		binding = &identity
	}
	request := struct {
		StorageBinding *string `json:"storage_binding"`
	}{binding}
	validate := func(raw []byte) error {
		preparation, err := decodePortabilityV1Preparation(raw)
		if err != nil {
			return err
		}
		return repository.validatePortabilityV1Preparation(preparation)
	}
	raw, err := repository.portabilityV1Command(ctx, target, surface,
		`select public.record_portability_purge_prepare_v1($1)`, request, validate)
	if err != nil {
		if len(raw) == 0 {
			return portabilityV1Preparation{}, err
		}
		candidate, decodeErr := decodePortabilityV1Preparation(raw)
		if decodeErr != nil {
			return portabilityV1Preparation{}, errors.Join(err, decodeErr)
		}
		resolvedRaw, resolveErr := repository.portabilityV1Command(ctx, target, surface,
			`select public.record_portability_purge_prepare_resolve_v1($1)`, request, nil)
		if resolveErr != nil {
			return portabilityV1Preparation{}, errors.Join(err, resolveErr)
		}
		resolved, decodeErr := decodePortabilityV1Preparation(resolvedRaw)
		if decodeErr != nil || candidate.InventoryDigest != resolved.InventoryDigest ||
			candidate.UnresolvedCount != resolved.UnresolvedCount || !reflect.DeepEqual(candidate.LocalTargets, resolved.LocalTargets) ||
			!reflect.DeepEqual(candidate.S3Targets, resolved.S3Targets) || len(resolved.RequiredBackends) != 0 {
			return portabilityV1Preparation{}, errors.Join(err, decodeErr, recorddeletion.ErrDeletionSafetyUnavailable)
		}
		if validationErr := repository.validatePortabilityV1Preparation(resolved); validationErr != nil {
			return portabilityV1Preparation{}, errors.Join(err, validationErr)
		}
		return resolved, nil
	}
	return decodePortabilityV1Preparation(raw)
}

func (repository *PostgresRecordDeletionRepository) portabilityV1Finish(ctx context.Context, target recorddeletion.PurgeTarget,
	surface [sha256.Size]byte, verify bool) (recorddeletion.AdapterPurgeReceipt, error) {
	query := `select removed_surface_digest,receipt_digest,removed_row_count,verified_absent_at from public.record_portability_purge_v1($1)`
	if verify {
		query = `select removed_surface_digest,receipt_digest,removed_row_count,verified_absent_at from public.record_portability_purge_verify_v1($1)`
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	candidate := false
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, tx *RecordPlatformTransaction) error {
		body, err := legacyAdapterPurgeEnvelope(ctx, tx.tx, target, surface)
		if err != nil {
			return err
		}
		receipt, err = runLegacyAdapterPurge(ctx, tx.tx, query, body, recorddeletion.AdapterNameRecordPortability, target, surface)
		if err != nil {
			return err
		}
		if receipt.ReceiptDigest != portabilityDeletionReceiptDigest(target.Operation, surface, receipt.RemovedRowCount) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		candidate = true
		return nil
	})
	if err == nil {
		return receipt, nil
	}
	if !candidate || verify {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	resolved, resolveErr := repository.portabilityV1Finish(ctx, target, surface, true)
	if resolveErr != nil || resolved != receipt {
		return recorddeletion.AdapterPurgeReceipt{}, errors.Join(err, resolveErr, recorddeletion.ErrDeletionSafetyUnavailable)
	}
	return resolved, nil
}

func (repository *PostgresRecordDeletionRepository) purgePortabilityV1(ctx context.Context, target recorddeletion.PurgeTarget,
	surface [sha256.Size]byte) (recorddeletion.AdapterPurgeReceipt, error) {
	preparation, err := repository.preparePortabilityV1(ctx, target, surface)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	if preparation.UnresolvedCount != 0 {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if len(preparation.LocalTargets) > 0 {
		if target.OwnerClaim.Owner.Generation > math.MaxInt64 {
			return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		local := repository.portabilityCleanup.Load()
		if local == nil {
			return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		coordinator, err := local.WithRepository(&portabilityV1LocalCleanupRepository{repository: repository, target: target, surface: surface})
		if err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, err
		}
		owner := target.OwnerClaim.Owner
		generation := int64(owner.Generation)
		for _, item := range preparation.LocalTargets {
			version := item.ExpectedVersion
			_, err := coordinator.Cleanup(ctx, attachments.LocalCleanupPreparation{
				LocalCleanupOperation: attachments.LocalCleanupOperation{OperationID: &target.Operation.OperationID,
					OwnerID: &owner.OwnerID, Generation: &generation, ExpiresAt: &owner.ExpiresAt},
				DeletionID: item.DeletionID, NamespaceID: item.NamespaceID, IncarnationID: item.IncarnationID,
				Key: item.Key, ExpectedVersion: &version,
			}, owner.OwnerID)
			if err != nil {
				return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("clean portability local target: %w", err)
			}
		}
	}
	for _, id := range preparation.S3Targets {
		if err := repository.executePortabilityV1S3(ctx, target, surface, id); err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, err
		}
	}
	return repository.portabilityV1Finish(ctx, target, surface, false)
}
