//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/sourceretirement"
	"houfeng/internal/center/store"
)

const ledgerSourceRetirementAction = "retire_source_authorization"

type ledgerSourcePreview struct {
	Version         int                                    `json:"version"`
	Action          string                                 `json:"action"`
	Key             string                                 `json:"key"`
	Request         deletionledger.SourceRetirementRequest `json:"request"`
	RecoveryID      string                                 `json:"recovery_id"`
	PhaseGeneration uint64                                 `json:"phase_generation"`
	PrimaryIdentity recorddeletion.LedgerPhysicalIdentity  `json:"primary_identity"`
	WitnessIdentity recorddeletion.LedgerPhysicalIdentity  `json:"witness_identity"`
	Anchor          ledgerAnchor                           `json:"anchor"`
}

type ledgerSourceBackend struct {
	Session       ledgerAppSession `json:"session"`
	TransactionID string           `json:"transaction_id"`
}

type ledgerSourceIntent struct {
	Version            int                 `json:"version"`
	Kind               string              `json:"kind"`
	Key                string              `json:"key"`
	PreviewDigest      [32]byte            `json:"preview_digest"`
	OriginalAbsent     bool                `json:"original_absent"`
	ExpectedFloor      [32]byte            `json:"expected_floor"`
	ExpectedRecordedAt time.Time           `json:"expected_recorded_at"`
	Backend            ledgerSourceBackend `json:"backend"`
}

type ledgerSourceWitnessed struct {
	Version      int                                  `json:"version"`
	Key          string                               `json:"key"`
	IntentDigest [32]byte                             `json:"intent_digest"`
	Entry        deletionledger.SourceRetirementEntry `json:"entry"`
	Phase        string                               `json:"phase"`
}

type ledgerSourceAnchored struct {
	Version         int      `json:"version"`
	Key             string   `json:"key"`
	WitnessedDigest [32]byte `json:"witnessed_digest"`
	AnchorIndex     uint64   `json:"anchor_index"`
}

func ledgerSourceKey(request deletionledger.SourceRetirementRequest) string {
	key := request.Origin.Source
	digest := sha256.Sum256([]byte(key.DeploymentID + "\x00" + string(key.ProjectID) + "\x00" + request.OperationID))
	return hex.EncodeToString(digest[:])
}

func ledgerSourceRequestEqual(a, b deletionledger.SourceRetirementRequest) bool {
	if a.Validate() != nil || b.Validate() != nil {
		return false
	}
	left, e1 := json.Marshal(a)
	right, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && bytes.Equal(left, right)
}

func ledgerSourceApplication(identity RecordLedgerApplicationIdentity) deletionledger.SourceApplicationIdentity {
	return deletionledger.SourceApplicationIdentity{ID: identity.ID, SystemIdentifier: identity.SystemIdentifier, DatabaseName: identity.DatabaseName, DatabaseOID: identity.DatabaseOID, Epoch: identity.Epoch}
}

func ledgerReadSourcePreviews(dir int) ([]ledgerSourcePreview, error) {
	names, err := ledgerNames(dir)
	if err != nil {
		return nil, err
	}
	var result []ledgerSourcePreview
	for _, name := range names {
		if !strings.HasPrefix(name, "source-preview-") {
			continue
		}
		data, exists, err := fenceReadFile(dir, name)
		var preview ledgerSourcePreview
		if err != nil || !exists || fenceDecode(data, &preview) != nil || preview.Version != 1 || preview.Action != ledgerSourceRetirementAction || preview.Request.Validate() != nil || preview.Key != ledgerSourceKey(preview.Request) || name != "source-preview-"+preview.Key+".json" || !fenceRID.MatchString(preview.RecoveryID) || preview.PhaseGeneration == 0 || preview.PrimaryIdentity.Validate() != nil || preview.WitnessIdentity.Validate() != nil || preview.PrimaryIdentity.Kind != "deletion_ledger" || preview.WitnessIdentity.Kind != "deletion_witness" || preview.Anchor.Primary != preview.Anchor.Witness {
			return nil, ErrRecordLedgerFence
		}
		result = append(result, preview)
	}
	return result, nil
}

func ledgerReadSourceIntent(dir int, preview ledgerSourcePreview) (ledgerSourceIntent, bool, error) {
	var intent ledgerSourceIntent
	data, exists, err := fenceReadFile(dir, "source-intent-"+preview.Key+".json")
	if err != nil || !exists {
		return intent, exists, err
	}
	body, marshalErr := json.Marshal(preview)
	if marshalErr != nil || fenceDecode(data, &intent) != nil || intent.Version != 1 || intent.Kind != "source_app_commit_pending" || intent.Key != preview.Key || intent.PreviewDigest != sha256.Sum256(body) || !intent.OriginalAbsent || intent.ExpectedFloor != preview.Request.FinalFloor.CanonicalHashValue() || !intent.ExpectedRecordedAt.Equal(preview.Request.Origin.TombstoneRecordedAt) || intent.Backend.Session.PID <= 0 || intent.Backend.Session.Started.IsZero() || !ledgerSafeRole(intent.Backend.Session.Role) || intent.Backend.TransactionID == "" {
		return ledgerSourceIntent{}, false, ErrRecordLedgerFence
	}
	for _, c := range intent.Backend.TransactionID {
		if c < '0' || c > '9' {
			return ledgerSourceIntent{}, false, ErrRecordLedgerFence
		}
	}
	return intent, true, nil
}

func (f *RecordLedgerFence) sourcePreview(request deletionledger.SourceRetirementRequest) (ledgerSourcePreview, error) {
	f.work.Lock()
	defer f.work.Unlock()
	previews, err := ledgerReadSourcePreviews(f.control)
	if err != nil {
		return ledgerSourcePreview{}, err
	}
	for _, preview := range previews {
		if preview.Key != ledgerSourceKey(request) {
			continue
		}
		if !ledgerSourceRequestEqual(preview.Request, request) || preview.Request.Origin.Application != ledgerSourceApplication(f.request.AppIdentity) || preview.PrimaryIdentity != f.request.Primary.Identity || preview.WitnessIdentity != f.request.Witness.Identity || preview.Anchor.Index >= uint64(len(f.anchors)) || f.anchors[preview.Anchor.Index] != preview.Anchor {
			return ledgerSourcePreview{}, ErrRecordLedgerFence
		}
		return preview, nil
	}
	return ledgerSourcePreview{}, ErrRecordLedgerFence
}

// PreviewLegacySourceRetirement fixes the original request before any APP or
// ledger effect. The raw token leaves only through the caller's private file.
func (f *RecordLedgerFence) PreviewLegacySourceRetirement(ctx context.Context, actor string, key sourceretirement.SourceKey, finalFloor recordauth.VisibilityScope) (deletionledger.SourceRetirementRequest, [32]byte, error) {
	var request deletionledger.SourceRetirementRequest
	var token [32]byte
	if err := f.enter(ctx); err != nil {
		return request, token, err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	f.sourceWork.Lock()
	defer f.sourceWork.Unlock()
	if err := f.verifyExternal(ctx); err != nil {
		return request, token, err
	}
	if !f.appInventory.LegacyC80 || f.binding.Generation != 0 {
		return request, token, ErrRecordLedgerFence
	}
	if err := f.sourceRequestsSettled(ctx, false); err != nil {
		return request, token, err
	}
	previews, err := ledgerReadSourcePreviews(f.control)
	if err != nil {
		return request, token, err
	}
	for _, previous := range previews {
		if previous.Request.Origin.Source == key {
			return request, token, ErrRecordLedgerFence
		}
	}
	floor, err := recordauth.NormalizeVisibilityScope(finalFloor)
	if err != nil {
		return request, token, ErrRecordLedgerFence
	}
	tx, err := f.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return request, token, ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	if err := ledgerAppIdentity(ctx, tx, f.request.AppIdentity); err != nil {
		return request, token, err
	}
	observed, err := store.ObserveLegacySourceRetirement(ctx, tx, actor, key)
	if err != nil {
		return request, token, err
	}
	var operation [16]byte
	if _, err := rand.Read(operation[:]); err != nil {
		return request, token, ErrRecordLedgerFence
	}
	if _, err := rand.Read(token[:]); err != nil {
		return request, [32]byte{}, ErrRecordLedgerFence
	}
	commitment, err := deletionledger.SourceTokenCommitment(key, token)
	if err != nil {
		return request, [32]byte{}, err
	}
	request = deletionledger.SourceRetirementRequest{OperationID: "source_ret_" + hex.EncodeToString(operation[:]), ActorUserID: actor,
		Origin: deletionledger.SourceOrigin{Application: ledgerSourceApplication(f.request.AppIdentity), Source: key, Revision: observed.Revision, LastLive: observed.LastLive, TombstoneRecordedAt: observed.RecordedAt}, FinalFloor: floor, TokenCommitment: commitment}
	request.RequestFingerprint, err = deletionledger.SourceRequestFingerprint(request)
	if err != nil || request.Validate() != nil {
		return deletionledger.SourceRetirementRequest{}, [32]byte{}, ErrRecordLedgerFence
	}
	if err := tx.Commit(ctx); err != nil {
		return deletionledger.SourceRetirementRequest{}, [32]byte{}, ErrRecordLedgerFence
	}
	if err := f.verifyExternal(ctx); err != nil {
		return deletionledger.SourceRetirementRequest{}, [32]byte{}, err
	}
	f.work.Lock()
	defer f.work.Unlock()
	if err := f.checkAnchorsLocked(); err != nil {
		return deletionledger.SourceRetirementRequest{}, [32]byte{}, err
	}
	anchor := f.anchors[len(f.anchors)-1]
	p, w, err := f.bootstrapReaders()
	if err != nil {
		return deletionledger.SourceRetirementRequest{}, [32]byte{}, err
	}
	primary, witness, err := f.pair(ctx, p, w, anchor)
	if err != nil || primary.Head != anchor.Primary || witness.Head != anchor.Witness || primary.Head != witness.Head {
		return deletionledger.SourceRetirementRequest{}, [32]byte{}, ErrRecordLedgerFence
	}
	preview := ledgerSourcePreview{Version: 1, Action: ledgerSourceRetirementAction, Key: ledgerSourceKey(request), Request: request, RecoveryID: f.request.Compose.RecoveryID, PhaseGeneration: f.phase.Generation, PrimaryIdentity: f.request.Primary.Identity, WitnessIdentity: f.request.Witness.Identity, Anchor: anchor}
	if _, err := ledgerPublishExact(f.control, "source-preview-"+preview.Key+".json", preview); err != nil {
		return deletionledger.SourceRetirementRequest{}, [32]byte{}, err
	}
	return request, token, nil
}

func (f *RecordLedgerFence) RetireLegacySourceRetirement(ctx context.Context, request deletionledger.SourceRetirementRequest, token [32]byte) (LegacySourceRetirementResult, error) {
	var result LegacySourceRetirementResult
	if err := f.enter(ctx); err != nil {
		return result, err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	f.sourceWork.Lock()
	defer f.sourceWork.Unlock()
	if err := f.verifyExternal(ctx); err != nil {
		return result, err
	}
	if !f.appInventory.LegacyC80 || f.binding.Generation != 0 || request.Validate() != nil {
		return result, ErrRecordLedgerFence
	}
	commitment, err := deletionledger.SourceTokenCommitment(request.Origin.Source, token)
	if err != nil || subtle.ConstantTimeCompare(commitment[:], request.TokenCommitment[:]) != 1 {
		return result, ErrRecordLedgerFence
	}
	preview, err := f.sourcePreview(request)
	if err != nil {
		return result, err
	}
	_, pending, err := ledgerReadSourceIntent(f.control, preview)
	if err != nil {
		return result, err
	}
	if !pending {
		if preview.PhaseGeneration != f.phase.Generation || preview.RecoveryID != f.request.Compose.RecoveryID || preview.Anchor != f.anchors[len(f.anchors)-1] {
			return result, ErrRecordLedgerFence
		}
		if err := f.sourceRequestsSettled(ctx, false); err != nil {
			return result, err
		}
		tx, err := f.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return result, ErrRecordLedgerFence
		}
		defer tx.Rollback(ctx)
		if err := ledgerAppIdentity(ctx, tx, f.request.AppIdentity); err != nil {
			return result, err
		}
		if err := store.InsertLegacySourceRetirement(ctx, tx, request); err != nil {
			return result, err
		}
		var backend ledgerSourceBackend
		if err := tx.QueryRow(ctx, `select pid,backend_start,usename::text,pg_catalog.pg_current_xact_id()::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&backend.Session.PID, &backend.Session.Started, &backend.Session.Role, &backend.TransactionID); err != nil || backend.Session.Role != f.appBootstrapRole {
			return result, ErrRecordLedgerFence
		}
		encoded, err := json.Marshal(preview)
		if err != nil {
			return result, ErrRecordLedgerFence
		}
		intent := ledgerSourceIntent{Version: 1, Kind: "source_app_commit_pending", Key: preview.Key, PreviewDigest: sha256.Sum256(encoded), OriginalAbsent: true, ExpectedFloor: request.FinalFloor.CanonicalHashValue(), ExpectedRecordedAt: request.Origin.TombstoneRecordedAt, Backend: backend}
		if _, err := ledgerPublishExact(f.control, "source-intent-"+preview.Key+".json", intent); err != nil {
			return result, err
		}
		// APP owns the one connection until COMMIT. Independently recheck the
		// retained host/ledger fence without recursively acquiring that pool.
		if ctx.Err() != nil || f.requireQuiescent() != nil || f.verifyCompose(ctx) != nil || ledgerVerifyCgroups(f.groups) != nil || f.primary.verifyFenced(ctx) != nil || f.witness.verifyFenced(ctx) != nil || !ledgerControlMatches(f.request.Compose.ControlConfigPath, f.control) {
			return result, ErrRecordLedgerFence
		}
		if err := tx.Commit(ctx); err != nil {
			return result, ErrRecordLedgerFence
		}
		if err := f.sourceCutpoint(ctx, "app-commit", request, ledgerSourceFaultBackend(backend.Session)); err != nil {
			return result, err
		}
	}
	return f.reconcileLegacySourceRetirement(ctx, request)
}

func (f *RecordLedgerFence) ReconcileLegacySourceRetirement(ctx context.Context, request deletionledger.SourceRetirementRequest) (LegacySourceRetirementResult, error) {
	if err := f.enter(ctx); err != nil {
		return LegacySourceRetirementResult{}, err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	f.sourceWork.Lock()
	defer f.sourceWork.Unlock()
	return f.reconcileLegacySourceRetirement(ctx, request)
}

func (f *RecordLedgerFence) VerifySourceRetirementProjection(ctx context.Context, request deletionledger.SourceRetirementRequest) error {
	if err := f.enter(ctx); err != nil {
		return err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	if err := f.verifyExternal(ctx); err != nil {
		return err
	}
	preview, err := f.sourcePreview(request)
	if err != nil {
		return err
	}
	intent, exists, err := ledgerReadSourceIntent(f.control, preview)
	if err != nil || !exists {
		return ErrRecordLedgerFence
	}
	var unresolved bool
	if err := f.app.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_stat_activity where pid=$1 and backend_start=$2 and usename=$3 and backend_xid is not null)`, intent.Backend.Session.PID, intent.Backend.Session.Started, intent.Backend.Session.Role).Scan(&unresolved); err != nil || unresolved {
		return ErrRecordLedgerFence
	}
	if err := ledgerAppIdentity(ctx, f.app, f.request.AppIdentity); err != nil {
		return err
	}
	return store.ReadLegacySourceRetirementProjection(ctx, f.app, request)
}

func (f *RecordLedgerFence) sourceWriterAttempt(ctx context.Context, key string, db *ledgerDatabase, kind string) error {
	names, err := ledgerNames(f.control)
	if err != nil {
		return err
	}
	prefix := "source-" + kind + "-writer-" + key + "-"
	attempt := 1
	for _, name := range names {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if name != fmt.Sprintf("%s%06d.json", prefix, attempt) {
			return ErrRecordLedgerFence
		}
		data, exists, err := fenceReadFile(f.control, name)
		var prior ledgerAppSession
		if err != nil || !exists || fenceDecode(data, &prior) != nil || prior.PID <= 0 || prior.Started.IsZero() || prior.Role != db.writerRole {
			return ErrRecordLedgerFence
		}
		var active bool
		if err := db.admin.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_stat_activity where pid=$1 and backend_start=$2 and usename=$3 and backend_xid is not null)`, prior.PID, prior.Started, prior.Role).Scan(&active); err != nil || active {
			return ErrRecordLedgerFence
		}
		attempt++
	}
	current, err := ledgerCurrentAppSession(ctx, db.writer)
	if err != nil || current.Role != db.writerRole {
		return ErrRecordLedgerFence
	}
	_, err = ledgerPublishExact(f.control, fmt.Sprintf("%s%06d.json", prefix, attempt), current)
	return err
}

func (f *RecordLedgerFence) reconcileLegacySourceRetirement(ctx context.Context, request deletionledger.SourceRetirementRequest) (LegacySourceRetirementResult, error) {
	var result LegacySourceRetirementResult
	if err := f.VerifySourceRetirementProjection(ctx, request); err != nil {
		return result, err
	}
	if err := f.Preflight(ctx); err != nil {
		return result, err
	}
	primary, witness, err := f.Clients()
	if err != nil {
		return result, err
	}
	verifier, err := deletionledger.NewSourceRetirementCommitVerifier(f)
	if err != nil {
		return result, err
	}
	commit, err := verifier.Verify(ctx, request)
	if err != nil {
		return result, err
	}
	key := ledgerSourceKey(request)
	if err := f.sourceWriterAttempt(ctx, key, f.primary, "primary"); err != nil {
		return result, err
	}
	resolved, err := primary.ResolveSourceRetirement(ctx, request)
	if err != nil {
		return result, err
	}
	entry := resolved.Entry
	if !resolved.Committed {
		if !f.appInventory.LegacyC80 {
			return result, ErrRecordLedgerFence
		}
		appendCtx := f.sourceCommitContext(ctx, request, "w1-commit", "primary", f.primary)
		entry, err = primary.AppendSourceRetirement(appendCtx, commit)
		if err != nil {
			return result, err
		}
	}
	if err := f.sourceWriterAttempt(ctx, key, f.witness, "witness"); err != nil {
		return result, err
	}
	witnessCtx := f.sourceCommitContext(ctx, request, "w2-commit", "witness", f.witness)
	verified, err := witness.ConfirmSourceRetirement(witnessCtx, commit, entry)
	if err != nil {
		return result, err
	}
	if !verified.Valid() || !ledgerSourceRequestEqual(verified.Entry().Request, request) {
		return result, ErrRecordLedgerFence
	}
	if err := f.Advance(ctx); err != nil {
		return result, err
	}
	result.Entry = verified.Entry()
	if !f.appInventory.LegacyC80 {
		if err := f.installSourceRetirementHandoff(ctx, verified); err != nil {
			return LegacySourceRetirementResult{}, err
		}
		result.HandoffInstalled = true
	}
	return result, nil
}
