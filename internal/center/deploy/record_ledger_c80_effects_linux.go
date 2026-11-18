//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

// These wire commands contain only the facts persisted by the C80 repository.
// The request token and record snapshot must never cross this boundary.
type ledgerC80CreateCommand struct {
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

type ledgerC80ReserveCommand struct {
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

type ledgerC80Preview struct {
	ReservationID string
	ExpiresAt     time.Time
	State         string
	FenceEpoch    uint64
}

func ledgerC80ID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) <= len(prefix) || len(value) > len(prefix)+64 {
		return false
	}
	for _, ch := range value[len(prefix):] {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

func ledgerC80DigestBytes(value string) ([]byte, bool) {
	if len(value) != 2*sha256.Size {
		return nil, false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return nil, false
	}
	var nonzero byte
	for _, b := range decoded {
		nonzero |= b
	}
	return decoded, nonzero != 0
}

func ledgerC80ValidCreate(command ledgerC80CreateCommand) bool {
	object := recordplatform.ObjectRef{ProjectID: command.ProjectID, ObjectKind: command.ObjectKind, ObjectID: command.ObjectID}
	if object.Validate() != nil || command.ObjectKind != "record" || !ledgerC80ID(command.ObjectID, "rec_") ||
		!ledgerC80ID(command.CurrentRevisionID, "rrv_") || command.LockVersion == 0 || command.LockVersion > math.MaxInt64 ||
		command.AuthorizationEpoch == 0 || command.AuthorizationEpoch > math.MaxInt64 ||
		command.ContentDeliveryEpoch > math.MaxInt64 || command.WitnessSequence == 0 || command.WitnessSequence > math.MaxInt64 ||
		command.TTLMicroseconds != uint64((10*time.Minute).Microseconds()) {
		return false
	}
	for _, digest := range []string{
		command.ActorScopeDigest, command.TokenCommitment, command.RequestFingerprint, command.BindingDigest,
		command.DependencyDigest, command.BackupDigest, command.ProcessorDigest, command.AdapterReadinessDigest,
		command.AdapterPreviewDigest, command.WitnessHash,
	} {
		if _, ok := ledgerC80DigestBytes(digest); !ok {
			return false
		}
	}
	return true
}

func ledgerC80ValidReserve(command ledgerC80ReserveCommand) bool {
	if !ledgerC80ID(command.ReservationID, "drs_") ||
		recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(command.DeploymentID)) != nil ||
		recordauth.ValidateActorUserID(command.ActorID) != nil || command.DeletionContractVersion != 1 ||
		command.ObservedWitnessSequence == 0 || command.ObservedWitnessSequence > math.MaxInt64 ||
		command.OwnerID != "c80_predecessor" || command.OwnerLeaseMicroseconds != uint64((30*time.Second).Microseconds()) ||
		command.ReasonCode != "user_confirmed" {
		return false
	}
	for _, digest := range []string{command.CreateCommandDigest, command.RequestFingerprint, command.ExpectedBindingDigest, command.ObservedWitnessHash} {
		if _, ok := ledgerC80DigestBytes(digest); !ok {
			return false
		}
	}
	return true
}

// Re-encode the typed command before sorting keys, matching the wrapper's
// canonical JSON even when the incoming JSON uses equivalent string escapes.
func ledgerC80CommandDigest(command any) (string, error) {
	body, err := json.Marshal(command)
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) == 0 {
		return "", ErrRecordLedgerFence
	}
	canonical, err := json.Marshal(fields)
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func ledgerC80StrictCommand(body []byte, names, numbers []string) error {
	if len(body) == 0 || len(body) > ledgerCallbackFrameLimit || !utf8.Valid(body) {
		return ErrRecordLedgerFence
	}
	fields, err := ledgerCallbackFields(body, names...)
	if err != nil {
		return ErrRecordLedgerFence
	}
	for _, raw := range fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return ErrRecordLedgerFence
		}
	}
	for _, name := range numbers {
		raw := bytes.TrimSpace(fields[name])
		if len(raw) == 0 {
			return ErrRecordLedgerFence
		}
		for _, b := range raw {
			if b < '0' || b > '9' {
				return ErrRecordLedgerFence
			}
		}
	}
	return nil
}

func ledgerC80DecodeCreate(body []byte) (ledgerC80CreateCommand, string, error) {
	var command ledgerC80CreateCommand
	names := []string{
		"project_id", "object_kind", "object_id", "actor_scope_digest", "token_commitment", "request_fingerprint",
		"binding_digest", "current_revision_id", "lock_version", "authorization_epoch", "content_delivery_epoch",
		"dependency_digest", "backup_digest", "processor_digest", "adapter_readiness_digest", "adapter_preview_digest",
		"witness_sequence", "witness_hash", "ttl_microseconds",
	}
	numbers := []string{"lock_version", "authorization_epoch", "content_delivery_epoch", "witness_sequence", "ttl_microseconds"}
	if ledgerC80StrictCommand(body, names, numbers) != nil || json.Unmarshal(body, &command) != nil || !ledgerC80ValidCreate(command) {
		return ledgerC80CreateCommand{}, "", ErrRecordLedgerFence
	}
	digest, err := ledgerC80CommandDigest(command)
	if err != nil {
		return ledgerC80CreateCommand{}, "", ErrRecordLedgerFence
	}
	return command, digest, nil
}

func ledgerC80DecodeReserve(body []byte) (ledgerC80ReserveCommand, string, error) {
	var command ledgerC80ReserveCommand
	names := []string{
		"create_command_digest", "reservation_id", "deployment_id", "actor_id", "deletion_contract_version",
		"request_fingerprint", "expected_binding_digest", "observed_witness_sequence", "observed_witness_hash",
		"owner_id", "owner_lease_microseconds", "reason_code",
	}
	numbers := []string{"deletion_contract_version", "observed_witness_sequence", "owner_lease_microseconds"}
	if ledgerC80StrictCommand(body, names, numbers) != nil || json.Unmarshal(body, &command) != nil || !ledgerC80ValidReserve(command) {
		return ledgerC80ReserveCommand{}, "", ErrRecordLedgerFence
	}
	digest, err := ledgerC80CommandDigest(command)
	if err != nil {
		return ledgerC80ReserveCommand{}, "", ErrRecordLedgerFence
	}
	return command, digest, nil
}

// Only the unique token commitment is used to find a row. Every other field is
// then checked against the actual INSERT, including its transaction-time TTL.
func ledgerC80ReadPreview(ctx context.Context, q ledgerAppQueryer, command ledgerC80CreateCommand) (ledgerC80Preview, bool, error) {
	if ctx == nil || ctx.Err() != nil || q == nil || !ledgerC80ValidCreate(command) {
		return ledgerC80Preview{}, false, ErrRecordLedgerFence
	}
	token, _ := ledgerC80DigestBytes(command.TokenCommitment)
	fingerprint, _ := ledgerC80DigestBytes(command.RequestFingerprint)
	actor, _ := ledgerC80DigestBytes(command.ActorScopeDigest)
	binding, _ := ledgerC80DigestBytes(command.BindingDigest)
	dependency, _ := ledgerC80DigestBytes(command.DependencyDigest)
	backup, _ := ledgerC80DigestBytes(command.BackupDigest)
	processor, _ := ledgerC80DigestBytes(command.ProcessorDigest)
	readiness, _ := ledgerC80DigestBytes(command.AdapterReadinessDigest)
	impact, _ := ledgerC80DigestBytes(command.AdapterPreviewDigest)
	witness, _ := ledgerC80DigestBytes(command.WitnessHash)

	var preview ledgerC80Preview
	var fenceEpoch int64
	var exact bool
	err := q.QueryRow(ctx, `
		select r.reservation_id, r.expires_at, r.state, r.fence_epoch,
		       coalesce((r.project_id=$2 and r.object_kind=$3 and r.object_id=$4
		         and r.request_fingerprint=$5 and r.actor_scope_digest=$6
		         and r.preview_binding_digest=$7 and r.preview_current_revision_id=$8
		         and r.preview_lock_version=$9 and r.preview_authorization_epoch=$10
		         and r.preview_content_delivery_epoch=$11 and r.preview_dependency_graph_digest=$12
		         and r.preview_backup_inventory_digest=$13 and r.preview_processor_inventory_digest=$14
		         and r.adapter_readiness_digest=$15 and r.adapter_preview_digest=$16
		         and r.preview_witness_sequence=$17 and r.preview_witness_entry_hash=$18
		         and pg_catalog.isfinite(r.created_at) and pg_catalog.isfinite(r.expires_at)
		         and r.expires_at=r.created_at + ($19::bigint * interval '1 microsecond')
		         and not r.recovery_replayed and r.release_epoch=0 and r.completed_at is null
		         and ((r.state='previewed' and r.fence_epoch=0 and r.owner_id=''
		               and r.owner_generation=0 and r.owner_expires_at is null)
		           or (r.state='fenced' and r.fence_epoch>0 and r.fence_epoch-1=$11
		               and r.owner_id<>'' and r.owner_generation>0 and r.owner_expires_at is not null
		               and pg_catalog.isfinite(r.owner_expires_at)))), false)
		from public.deletion_reservations r
		where r.deletion_token_commitment=$1`,
		token, command.ProjectID, command.ObjectKind, command.ObjectID, fingerprint, actor, binding,
		command.CurrentRevisionID, int64(command.LockVersion), int64(command.AuthorizationEpoch),
		int64(command.ContentDeliveryEpoch), dependency, backup, processor, readiness, impact,
		int64(command.WitnessSequence), witness, int64(command.TTLMicroseconds),
	).Scan(&preview.ReservationID, &preview.ExpiresAt, &preview.State, &fenceEpoch, &exact)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledgerC80Preview{}, false, nil
	}
	if err != nil || !exact || !ledgerC80ID(preview.ReservationID, "drs_") || fenceEpoch < 0 ||
		preview.ExpiresAt.IsZero() || preview.ExpiresAt.Nanosecond()%1000 != 0 {
		return ledgerC80Preview{}, false, ErrRecordLedgerFence
	}
	preview.ExpiresAt = preview.ExpiresAt.UTC()
	preview.FenceEpoch = uint64(fenceEpoch)
	return preview, true, nil
}

// No observed witness from ReservePreview is persisted by C80. Its actual
// extension is authenticated separately by the callback's witness protocol.
func ledgerC80ReadOperation(ctx context.Context, q ledgerAppQueryer, create ledgerC80CreateCommand, reserve ledgerC80ReserveCommand) (string, bool, error) {
	if ctx == nil || ctx.Err() != nil || q == nil || !ledgerC80ValidCreate(create) || !ledgerC80ValidReserve(reserve) {
		return "", false, ErrRecordLedgerFence
	}
	digest, err := ledgerC80CommandDigest(create)
	if err != nil || reserve.CreateCommandDigest != digest || reserve.RequestFingerprint != create.RequestFingerprint ||
		reserve.ExpectedBindingDigest != create.BindingDigest {
		return "", false, ErrRecordLedgerFence
	}
	preview, found, err := ledgerC80ReadPreview(ctx, q, create)
	if err != nil || !found || preview.ReservationID != reserve.ReservationID {
		return "", false, ErrRecordLedgerFence
	}

	var operationID string
	var present, exact bool
	err = q.QueryRow(ctx, `
		select o.operation_id is not null, coalesce(o.operation_id, ''),
		       coalesce((r.state='fenced' and r.fence_epoch>0 and r.fence_epoch-1=$13
		         and o.reservation_id=r.reservation_id and o.project_id=r.project_id and o.project_id=$2
		         and o.operation_state='provisional_fenced' and o.actor_id=$3 and o.deployment_id=$4
		         and o.reason_code=$5 and o.deletion_contract_version=$6
		         and o.owner_id=$7 and o.owner_generation>0 and o.owner_expires_at is not null
		         and r.owner_id=o.owner_id and r.owner_generation=o.owner_generation
		         and r.owner_expires_at=o.owner_expires_at
		         and f.owner_id=o.owner_id and f.owner_generation=o.owner_generation and f.expires_at=o.owner_expires_at
		         and e.delivery_epoch=r.fence_epoch
		         and root.current_revision_id=$10 and root.lock_version=$11 and root.authorization_epoch=$12
		         and pg_catalog.isfinite(o.started_at) and pg_catalog.isfinite(o.owner_expires_at)
		         and o.started_at>=r.created_at and o.started_at<r.expires_at and o.updated_at=o.started_at
		         and o.owner_expires_at=o.started_at + ($8::bigint * interval '1 microsecond')
		         and o.ledger_entry_type is null and o.ledger_sequence is null and o.ledger_entry_hash is null
		         and o.witness_proof_digest is null and o.release_epoch=0 and o.receipt_digest is null
		         and o.retry_from is null and o.completed_at is null and o.details_delete_after is null
		         and (select count(*) from public.record_deletion_audits a where a.operation_id=o.operation_id)=1
		         and exists (select 1 from public.record_deletion_audits a
		                     where a.operation_id=o.operation_id and a.project_id=o.project_id
		                       and a.event_kind='fenced' and a.reason_code=o.reason_code
		                       and a.occurred_at=o.started_at and a.audit_id ~ '^rda_[a-z0-9]{1,64}$')), false)
		from public.deletion_reservations r
		left join public.record_purge_operations o on o.reservation_id=r.reservation_id
		left join public.deletion_fence_leases f on f.project_id=r.project_id and f.object_kind=r.object_kind and f.object_id=r.object_id
		left join public.content_delivery_epochs e on e.project_id=r.project_id and e.object_kind=r.object_kind and e.object_id=r.object_id
		left join public.records root on root.project_id=r.project_id and root.record_id=r.object_id
		where r.reservation_id=$1 and r.object_id=$9`,
		reserve.ReservationID, create.ProjectID, reserve.ActorID, reserve.DeploymentID, reserve.ReasonCode,
		int64(reserve.DeletionContractVersion), reserve.OwnerID, int64(reserve.OwnerLeaseMicroseconds), create.ObjectID,
		create.CurrentRevisionID, int64(create.LockVersion), int64(create.AuthorizationEpoch), int64(create.ContentDeliveryEpoch),
	).Scan(&present, &operationID, &exact)
	if err != nil {
		return "", false, ErrRecordLedgerFence
	}
	if !present {
		if operationID != "" || ledgerC80PreviewAbandonable(ctx, q, create, preview) != nil {
			return "", false, ErrRecordLedgerFence
		}
		return "", false, nil
	}
	if !exact || !ledgerC80ID(operationID, "rpo_") || preview.State != "fenced" ||
		preview.FenceEpoch == 0 || preview.FenceEpoch-1 != create.ContentDeliveryEpoch {
		return "", false, ErrRecordLedgerFence
	}
	return operationID, true, nil
}

// The caller must additionally establish that the original backend has settled
// and that the fenced APP and realm have continuous identity before resolving.
func ledgerC80PreviewAbandonable(ctx context.Context, q ledgerAppQueryer, create ledgerC80CreateCommand, preview ledgerC80Preview) error {
	if ctx == nil || ctx.Err() != nil || q == nil || !ledgerC80ValidCreate(create) ||
		preview.State != "previewed" || preview.FenceEpoch != 0 {
		return ErrRecordLedgerFence
	}
	actual, found, err := ledgerC80ReadPreview(ctx, q, create)
	if err != nil || !found || actual.ReservationID != preview.ReservationID || !actual.ExpiresAt.Equal(preview.ExpiresAt) ||
		actual.State != preview.State || actual.FenceEpoch != preview.FenceEpoch {
		return ErrRecordLedgerFence
	}
	var exact bool
	err = q.QueryRow(ctx, `
		select coalesce((root.current_revision_id=$4 and root.lock_version=$5 and root.authorization_epoch=$6
		  and epoch.delivery_epoch=$7
		  and not exists (select 1 from public.deletion_fence_leases f
		                  where f.project_id=$1 and f.object_kind=$2 and f.object_id=$3)
		  and not exists (select 1 from public.deletion_reservations other
		                  where other.project_id=$1 and other.object_kind=$2 and other.object_id=$3
		                    and other.state in ('fenced','committed'))
		  and not exists (select 1 from public.record_purge_operations op
		                  where op.reservation_id=$8)
		  and not exists (select 1 from public.record_purge_operations op
		                  join public.deletion_reservations other on other.reservation_id=op.reservation_id
		                  where other.project_id=$1 and other.object_kind=$2 and other.object_id=$3)), false)
		from public.records root
		join public.content_delivery_epochs epoch
		  on epoch.project_id=root.project_id and epoch.object_kind=$2 and epoch.object_id=root.record_id
		where root.project_id=$1 and root.record_id=$3`,
		create.ProjectID, create.ObjectKind, create.ObjectID, create.CurrentRevisionID,
		int64(create.LockVersion), int64(create.AuthorizationEpoch), int64(create.ContentDeliveryEpoch), preview.ReservationID,
	).Scan(&exact)
	if err != nil || !exact {
		return ErrRecordLedgerFence
	}
	return nil
}
