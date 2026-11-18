package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
)

// The operation association is deliberately private to the purge coordinator.
type attachmentPurgeV1Claim struct {
	OperationID            string                  `json:"operation_id"`
	DeletionID             string                  `json:"deletion_id"`
	ProjectID              string                  `json:"project_id"`
	BackendKind            attachments.BackendKind `json:"backend_kind"`
	Key                    string                  `json:"key"`
	VersionID              string                  `json:"version_id"`
	SHA256                 string                  `json:"sha256"`
	SizeBytes              int64                   `json:"size_bytes"`
	BlobCreatedAt          string                  `json:"blob_created_at"`
	CleanupOwnerID         string                  `json:"cleanup_owner_id"`
	CleanupOwnerGeneration int64                   `json:"cleanup_owner_generation"`
	Attempt                int64                   `json:"attempt"`
	CleanupExpiresAt       string                  `json:"cleanup_expires_at"`
}

type attachmentPurgeV1Stored struct {
	Claim          attachmentPurgeV1Claim `json:"claim"`
	State          string                 `json:"state"`
	RetryAt        *string                `json:"retry_at"`
	PhysicalResult *string                `json:"physical_delete_result"`
	ReceiptDigest  *string                `json:"receipt_digest"`
	CompletedAt    *string                `json:"completed_at"`
}

type attachmentPurgeV1Preparation struct {
	Counts struct {
		LogicalAttachment uint64 `json:"logical_attachment"`
		UploadPart        uint64 `json:"upload_part"`
		BlobObject        uint64 `json:"blob_object"`
	} `json:"counts"`
	VerifiedAt string                    `json:"verified_at"`
	Claims     []attachmentPurgeV1Stored `json:"claims"`
	// RequiredBackends describes only a fresh preparation's precommit checks;
	// committed replay and verification return an empty list.
	RequiredBackends []attachments.BackendKind `json:"required_backends"`
	counts           attachmentDeletionReceiptCounts
	claims           []attachmentPurgeV1Stored
}

type attachmentPurgeV1Receipt struct {
	Key       string `json:"key"`
	VersionID string `json:"version_id"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Deleted   bool   `json:"deleted"`
}

func attachmentPurgeV1Time(text string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, text)
	if err != nil || t.IsZero() || t.Nanosecond()%1000 != 0 {
		return time.Time{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return t.UTC(), nil
}

func attachmentPurgeV1Digest(text string) ([]byte, error) {
	if len(text) != 64 {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	value, err := hex.DecodeString(text)
	if err != nil || hex.EncodeToString(value) != text {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return value, nil
}

// JSONB normally has unique keys, but reject malformed or duplicate-key responses
// before decoding so that the protocol remains exact even with another provider.
func attachmentPurgeV1JSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			seen[name] = true
			if err := attachmentPurgeV1JSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := attachmentPurgeV1JSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	_, err = decoder.Token()
	return err
}

func attachmentPurgeV1Exact(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil || len(fields) != len(keys) {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return nil, recorddeletion.ErrDeletionSafetyUnavailable
		}
	}
	return fields, nil
}

func attachmentPurgeV1NonNull(fields map[string]json.RawMessage, keys ...string) error {
	for _, key := range keys {
		if bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
	}
	return nil
}

func attachmentPurgeV1ValidateJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := attachmentPurgeV1JSONValue(decoder); err != nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if _, err := decoder.Token(); err != io.EOF {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}

func (claim attachmentPurgeV1Claim) blobClaim(command attachments.AttachmentDeletionCommand) (attachments.BlobGCClaim, error) {
	digest, err := attachmentPurgeV1Digest(claim.SHA256)
	if err != nil {
		return attachments.BlobGCClaim{}, err
	}
	created, err := attachmentPurgeV1Time(claim.BlobCreatedAt)
	if err != nil {
		return attachments.BlobGCClaim{}, err
	}
	expires, err := attachmentPurgeV1Time(claim.CleanupExpiresAt)
	if err != nil {
		return attachments.BlobGCClaim{}, err
	}
	var result attachments.BlobGCClaim
	result.DeletionID, result.ProjectID, result.Mode = claim.DeletionID, claim.ProjectID, attachments.BlobGCPurgeModePermanent
	result.Candidate.Object.Key, result.Candidate.Object.ObjectVersion = claim.Key, claim.VersionID
	result.Candidate.Object.SizeBytes, result.Candidate.Object.BackendKind = claim.SizeBytes, claim.BackendKind
	copy(result.Candidate.Object.SHA256[:], digest)
	result.Candidate.CreatedAt = created
	result.OwnerID, result.OwnerGeneration, result.Attempt, result.LeaseExpiresAt = claim.CleanupOwnerID, claim.CleanupOwnerGeneration, claim.Attempt, expires
	if claim.OperationID != command.Operation.OperationID || claim.ProjectID != command.Operation.Object.ProjectID ||
		claim.BackendKind != attachments.BackendKindS3 || result.Validate() != nil {
		return attachments.BlobGCClaim{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return result, nil
}

func attachmentPurgeV1DecodeStored(raw []byte, command attachments.AttachmentDeletionCommand) (attachmentPurgeV1Stored, error) {
	fields, err := attachmentPurgeV1Exact(raw, "claim", "state", "retry_at", "physical_delete_result", "receipt_digest", "completed_at")
	if err != nil {
		return attachmentPurgeV1Stored{}, err
	}
	claimFields, err := attachmentPurgeV1Exact(fields["claim"], "operation_id", "deletion_id", "project_id", "backend_kind", "key", "version_id", "sha256", "size_bytes", "blob_created_at", "cleanup_owner_id", "cleanup_owner_generation", "attempt", "cleanup_expires_at")
	if err != nil {
		return attachmentPurgeV1Stored{}, err
	}
	if err := attachmentPurgeV1NonNull(fields, "claim", "state"); err != nil {
		return attachmentPurgeV1Stored{}, err
	}
	for key := range claimFields {
		if err := attachmentPurgeV1NonNull(claimFields, key); err != nil {
			return attachmentPurgeV1Stored{}, err
		}
	}
	var stored attachmentPurgeV1Stored
	if err := json.Unmarshal(raw, &stored); err != nil {
		return attachmentPurgeV1Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	claim, err := stored.Claim.blobClaim(command)
	if err != nil {
		return attachmentPurgeV1Stored{}, err
	}
	state := blobGCStoredDeletion{claim: claim, state: stored.State}
	if stored.RetryAt != nil {
		t, err := attachmentPurgeV1Time(*stored.RetryAt)
		if err != nil {
			return attachmentPurgeV1Stored{}, err
		}
		state.retryAt = &t
	}
	if stored.CompletedAt != nil {
		t, err := attachmentPurgeV1Time(*stored.CompletedAt)
		if err != nil {
			return attachmentPurgeV1Stored{}, err
		}
		state.completedAt = &t
	}
	state.physicalResult = stored.PhysicalResult
	if stored.ReceiptDigest != nil {
		state.receiptDigest, err = attachmentPurgeV1Digest(*stored.ReceiptDigest)
		if err != nil {
			return attachmentPurgeV1Stored{}, err
		}
	}
	if !validBlobGCStoredState(state) {
		return attachmentPurgeV1Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if state.state == "completed" {
		receipt := attachments.DeletionReceipt{Version: storeObjectVersionFromBlob(claim.Candidate.Object), Deleted: *state.physicalResult == "deleted"}
		if !blobGCStoredReceiptMatches(state, receipt) {
			return attachmentPurgeV1Stored{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
	}
	return stored, nil
}

func attachmentPurgeV1DecodePreparation(raw []byte, command attachments.AttachmentDeletionCommand) (attachmentPurgeV1Preparation, error) {
	if err := attachmentPurgeV1ValidateJSON(raw); err != nil {
		return attachmentPurgeV1Preparation{}, err
	}
	fields, err := attachmentPurgeV1Exact(raw, "counts", "verified_at", "claims", "required_backends")
	if err != nil {
		return attachmentPurgeV1Preparation{}, err
	}
	countFields, err := attachmentPurgeV1Exact(fields["counts"], "logical_attachment", "upload_part", "blob_object")
	if err != nil {
		return attachmentPurgeV1Preparation{}, err
	}
	if err := attachmentPurgeV1NonNull(fields, "counts", "verified_at", "claims", "required_backends"); err != nil {
		return attachmentPurgeV1Preparation{}, err
	}
	if err := attachmentPurgeV1NonNull(countFields, "logical_attachment", "upload_part", "blob_object"); err != nil {
		return attachmentPurgeV1Preparation{}, err
	}
	var elements []json.RawMessage
	if bytes.Equal(bytes.TrimSpace(fields["claims"]), []byte("null")) || json.Unmarshal(fields["claims"], &elements) != nil {
		return attachmentPurgeV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var result attachmentPurgeV1Preparation
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if len(result.RequiredBackends) > 1 || len(result.RequiredBackends) == 1 && result.RequiredBackends[0] != attachments.BackendKindS3 {
		return attachmentPurgeV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	verified, err := attachmentPurgeV1Time(result.VerifiedAt)
	if err != nil || result.Counts.LogicalAttachment > math.MaxInt64 || result.Counts.UploadPart > math.MaxInt64 || result.Counts.BlobObject > math.MaxInt64 || result.Counts.BlobObject != uint64(len(elements)) {
		return attachmentPurgeV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	result.counts = attachmentDeletionReceiptCounts{verifiedAt: verified, counts: map[string]uint64{
		attachmentDeletionSurfaceLogicalAttachment: result.Counts.LogicalAttachment,
		attachmentDeletionSurfaceUploadPart:        result.Counts.UploadPart,
		attachmentDeletionSurfaceBlobObject:        result.Counts.BlobObject,
	}}
	result.claims = make([]attachmentPurgeV1Stored, 0, len(elements))
	seen := make(map[string]bool, len(elements))
	for _, element := range elements {
		stored, err := attachmentPurgeV1DecodeStored(element, command)
		if err != nil {
			return attachmentPurgeV1Preparation{}, err
		}
		if seen[stored.Claim.DeletionID] {
			return attachmentPurgeV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		seen[stored.Claim.DeletionID] = true
		result.claims = append(result.claims, stored)
	}
	if !sort.SliceIsSorted(result.claims, func(i, j int) bool {
		a, b := result.claims[i].Claim, result.claims[j].Claim
		if a.BackendKind != b.BackendKind {
			return a.BackendKind < b.BackendKind
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		if a.VersionID != b.VersionID {
			return a.VersionID < b.VersionID
		}
		return a.DeletionID < b.DeletionID
	}) {
		return attachmentPurgeV1Preparation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return result, nil
}

// Each command runs in its own READ COMMITTED transaction. A non-nil result
// accompanied by a commit error is only a candidate, never dispatch authority.
func (repository *PostgresAttachmentRepository) attachmentPurgeV1Command(ctx context.Context, command attachments.AttachmentDeletionCommand, query string, extra any, beforeCommit func([]byte) error) ([]byte, error) {
	tx, err := repository.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin attachment V1 command: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	actual, ok := tx.(pgx.Tx)
	if !ok {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	authority, err := legacyAdapterPurgeEnvelope(ctx, actual, command.PurgeTarget, command.SurfaceDigest)
	if err != nil {
		return nil, err
	}
	body := authority
	if extra != nil {
		fields, err := json.Marshal(extra)
		if err != nil {
			return nil, err
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(fields, &object); err != nil {
			return nil, err
		}
		object["authority"] = json.RawMessage(authority)
		body, err = json.Marshal(object)
		if err != nil {
			return nil, err
		}
	}
	var raw []byte
	if err := tx.QueryRow(ctx, query, body).Scan(&raw); err != nil {
		return nil, fmt.Errorf("run attachment V1 command: %w", err)
	}
	if err := attachmentPurgeV1ValidateJSON(raw); err != nil {
		return nil, err
	}
	if beforeCommit != nil {
		if err := beforeCommit(raw); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return raw, fmt.Errorf("commit attachment V1 command: %w", err)
	}
	return raw, nil
}

func (repository *PostgresAttachmentRepository) prepareAttachmentDeletionV1(ctx context.Context, command attachments.AttachmentDeletionCommand) (attachmentPurgeV1Preparation, error) {
	validate := func(raw []byte) error {
		preparation, err := attachmentPurgeV1DecodePreparation(raw, command)
		if err != nil {
			return err
		}
		for _, backend := range preparation.RequiredBackends {
			if repository.attachmentDeletionBlobStore(backend) == nil {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
		}
		for _, stored := range preparation.claims {
			if stored.State != "completed" {
				if repository.attachmentDeletionBlobStore(attachments.BackendKindS3) == nil {
					return recorddeletion.ErrDeletionSafetyUnavailable
				}
				break
			}
		}
		return nil
	}
	raw, err := repository.attachmentPurgeV1Command(ctx, command, `select public.record_attachment_purge_prepare_v1($1)`, nil, validate)
	if err != nil {
		if len(raw) == 0 {
			return attachmentPurgeV1Preparation{}, err
		}
		candidate, decodeErr := attachmentPurgeV1DecodePreparation(raw, command)
		if decodeErr != nil {
			return attachmentPurgeV1Preparation{}, errors.Join(err, decodeErr)
		}
		resolvedRaw, resolveErr := repository.attachmentPurgeV1Command(ctx, command, `select public.record_attachment_purge_verify_v1($1)`, nil, nil)
		if resolveErr != nil {
			return attachmentPurgeV1Preparation{}, errors.Join(err, resolveErr)
		}
		resolved, resolveErr := attachmentPurgeV1DecodePreparation(resolvedRaw, command)
		if resolveErr != nil || !reflect.DeepEqual(candidate.counts, resolved.counts) || !reflect.DeepEqual(candidate.claims, resolved.claims) {
			return attachmentPurgeV1Preparation{}, errors.Join(err, resolveErr, recorddeletion.ErrDeletionSafetyUnavailable)
		}
		if validationErr := validate(resolvedRaw); validationErr != nil {
			return attachmentPurgeV1Preparation{}, errors.Join(err, validationErr)
		}
		return resolved, nil
	}
	return attachmentPurgeV1DecodePreparation(raw, command)
}

func (repository *PostgresAttachmentRepository) verifyAttachmentDeletionV1(ctx context.Context, command attachments.AttachmentDeletionCommand, receipt recorddeletion.AdapterPurgeReceipt) error {
	raw, err := repository.attachmentPurgeV1Command(ctx, command, `select public.record_attachment_purge_verify_v1($1)`, nil, nil)
	if err != nil {
		return err
	}
	preparation, err := attachmentPurgeV1DecodePreparation(raw, command)
	if err != nil {
		return err
	}
	if attachmentDeletionAdapterReceipt(command, preparation.counts) != receipt {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	for _, stored := range preparation.claims {
		if stored.State != "completed" {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
	}
	return nil
}

func (repository *PostgresAttachmentRepository) attachmentPurgeV1GC(ctx context.Context, command attachments.AttachmentDeletionCommand, query string, request any) (*attachmentPurgeV1Stored, error) {
	raw, err := repository.attachmentPurgeV1Command(ctx, command, query, request, nil)
	if len(raw) == 0 {
		return nil, err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, err
	}
	stored, decodeErr := attachmentPurgeV1DecodeStored(raw, command)
	if decodeErr != nil {
		return nil, errors.Join(err, decodeErr)
	}
	return &stored, err
}

func (repository *PostgresAttachmentRepository) executeAttachmentDeletionClaimsV1(ctx context.Context, command attachments.AttachmentDeletionCommand, claims []attachmentPurgeV1Stored) error {
	for _, original := range claims {
		if original.State == "completed" {
			continue
		}
		request := struct {
			DeletionID     string `json:"deletion_id"`
			CleanupOwnerID string `json:"cleanup_owner_id"`
			CleanupLeaseMS int64  `json:"cleanup_lease_ms"`
		}{original.Claim.DeletionID, command.OwnerClaim.Owner.OwnerID, attachments.DefaultBlobGCLeaseDuration.Milliseconds()}
		stored, err := repository.attachmentPurgeV1GC(ctx, command, `select public.record_attachment_gc_claim_v1($1)`, request)
		if err != nil && stored != nil {
			// Resolve an unknown COMMIT against the same ID and the exact tuple.
			observed, readErr := repository.attachmentPurgeV1GC(ctx, command, `select public.record_attachment_gc_claim_v1($1)`, request)
			if readErr != nil || observed == nil || observed.Claim != stored.Claim || observed.State != stored.State {
				return errors.Join(err, readErr, recorddeletion.ErrDeletionSafetyUnavailable)
			}
			stored, err = observed, nil
		}
		if err != nil {
			return err
		}
		if stored == nil {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		before, beforeErr := original.Claim.blobClaim(command)
		after, afterErr := stored.Claim.blobClaim(command)
		if beforeErr != nil || afterErr != nil || before.DeletionID != after.DeletionID || before.ProjectID != after.ProjectID || before.Candidate != after.Candidate {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if stored.State == "completed" {
			continue
		}
		if stored.State != "claimed" || stored.Claim.CleanupOwnerID != command.OwnerClaim.Owner.OwnerID {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if err := repository.executeAttachmentDeletionClaimV1(ctx, command, stored.Claim); err != nil {
			return err
		}
	}
	return nil
}

func (repository *PostgresAttachmentRepository) executeAttachmentDeletionClaimV1(ctx context.Context, command attachments.AttachmentDeletionCommand, claim attachmentPurgeV1Claim) error {
	blobClaim, err := claim.blobClaim(command)
	if err != nil {
		return err
	}
	deleter := repository.attachmentDeletionBlobStore(attachments.BackendKindS3)
	if deleter == nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	version := storeObjectVersionFromBlob(blobClaim.Candidate.Object)
	receipt, err := deleter.DeleteGCVersion(ctx, version)
	if err != nil {
		request := struct {
			Claim   attachmentPurgeV1Claim `json:"claim"`
			RetryAt string                 `json:"retry_at"`
		}{claim, time.Now().UTC().Add(attachments.DefaultBlobGCRetryDelay).Truncate(time.Microsecond).Format(time.RFC3339Nano)}
		_, retryErr := repository.attachmentPurgeV1GC(ctx, command, `select public.record_attachment_gc_retry_v1($1)`, request)
		return errors.Join(fmt.Errorf("delete exact attachment purge Blob: %w", err), retryErr)
	}
	if receipt.Version != version {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	request := struct {
		Claim   attachmentPurgeV1Claim   `json:"claim"`
		Receipt attachmentPurgeV1Receipt `json:"receipt"`
	}{claim, attachmentPurgeV1Receipt{version.Key, version.VersionID, hex.EncodeToString(version.SHA256[:]), version.SizeBytes, receipt.Deleted}}
	stored, err := repository.attachmentPurgeV1GC(ctx, command, `select public.record_attachment_gc_complete_v1($1)`, request)
	if err != nil {
		resolved, resolveErr := repository.attachmentPurgeV1GC(ctx, command, `select public.record_attachment_gc_resolve_v1($1)`, request)
		if resolveErr != nil || resolved == nil || resolved.Claim != claim || resolved.State != "completed" {
			return errors.Join(err, resolveErr)
		}
		stored = resolved
	}
	if stored == nil || stored.Claim != claim || stored.State != "completed" || stored.PhysicalResult == nil || (*stored.PhysicalResult == "deleted") != receipt.Deleted {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return nil
}
