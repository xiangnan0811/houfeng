//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/recorddeletion"
)

// Facts contain no token or source content. Heads describe the verification at
// installation; later calls prove current continuity without rewriting them.
type ledgerSourceHandoffFacts struct {
	DeploymentID        string `json:"deployment_id"`
	ProjectID           string `json:"project_id"`
	SourceKind          string `json:"source_kind"`
	SourceID            string `json:"source_id"`
	OperationID         string `json:"operation_id"`
	ActorID             string `json:"actor_id"`
	AppDomainID         string `json:"app_domain_id"`
	AppSystemIdentifier string `json:"app_system_identifier"`
	AppDatabaseOID      uint32 `json:"app_database_oid"`
	AppDatabaseName     string `json:"app_database_name"`
	AppEpoch            uint64 `json:"app_epoch"`
	SourceRevision      string `json:"source_revision"`
	LastLiveScope       string `json:"last_live_scope"`
	FinalFloorScope     string `json:"final_floor_scope"`
	TombstoneMicros     uint64 `json:"tombstone_unix_microseconds"`
	LedgerDomain        string `json:"ledger_domain"`
	LedgerSequence      uint64 `json:"ledger_sequence"`
	LedgerEntryHash     string `json:"ledger_entry_hash"`
	PreviousHash        string `json:"previous_hash"`
	CanonicalEntry      string `json:"canonical_entry"`
	TokenCommitment     string `json:"token_commitment"`
	RequestFingerprint  string `json:"request_fingerprint"`
	ConfirmedMicros     uint64 `json:"confirmed_at_unix_microseconds"`
	WitnessDomainID     string `json:"witness_domain_id"`
	WitnessProofDigest  string `json:"witness_proof_digest"`
	PrimaryHeadSequence uint64 `json:"primary_head_sequence"`
	PrimaryHeadHash     string `json:"primary_head_hash"`
	WitnessHeadSequence uint64 `json:"witness_head_sequence"`
	WitnessHeadHash     string `json:"witness_head_hash"`
}

type ledgerSourceHandoffCommand struct {
	ledgerSourceHandoffFacts
	MaintenanceRID        string `json:"maintenance_rid"`
	MaintenanceGeneration uint64 `json:"maintenance_generation"`
}

type ledgerSourceHandoffInstallation struct {
	ledgerSourceHandoffFacts
	FirstRID        string `json:"first_installation_rid"`
	FirstGeneration uint64 `json:"first_installation_generation"`
}

type ledgerSourceHandoffIntent struct {
	Version            int                             `json:"version"`
	Key                string                          `json:"key"`
	Application        RecordLedgerApplicationIdentity `json:"application"`
	Facts              ledgerSourceHandoffFacts        `json:"facts"`
	PreviewDigest      [32]byte                        `json:"preview_digest"`
	SourceIntentDigest [32]byte                        `json:"source_intent_digest"`
	WitnessedDigest    [32]byte                        `json:"witnessed_digest"`
	AnchoredDigest     [32]byte                        `json:"anchored_digest"`
}

type ledgerSourceHandoffAttempt struct {
	Version      int                        `json:"version"`
	Number       uint64                     `json:"number"`
	IntentDigest [32]byte                   `json:"intent_digest"`
	Command      ledgerSourceHandoffCommand `json:"command"`
	Backend      ledgerAppSession           `json:"backend"`
}

type ledgerSourceHandoffReceipt struct {
	Version      int                             `json:"version"`
	IntentDigest [32]byte                        `json:"intent_digest"`
	Attempt      uint64                          `json:"attempt"`
	Installation ledgerSourceHandoffInstallation `json:"installation"`
}

func (facts ledgerSourceHandoffFacts) withoutHeads() ledgerSourceHandoffFacts {
	facts.PrimaryHeadSequence, facts.WitnessHeadSequence = 0, 0
	facts.PrimaryHeadHash, facts.WitnessHeadHash = "", ""
	return facts
}

func ledgerSourceHandoffHex(digest [32]byte) string { return hex.EncodeToString(digest[:]) }

func ledgerSourceHandoffMaterial(verified deletionledger.VerifiedSourceRetirement, witnessID string) (ledgerSourceHandoffFacts, error) {
	if !verified.Valid() {
		return ledgerSourceHandoffFacts{}, ErrRecordLedgerFence
	}
	entry := verified.Entry()
	request, origin := entry.Request, entry.Request.Origin
	if entry.Validate() != nil {
		return ledgerSourceHandoffFacts{}, ErrRecordLedgerFence
	}
	proof, err := recorddeletion.LedgerWitnessProofDigest(entry.LedgerDomain, entry.Sequence, entry.EntryHash, witnessID)
	if err != nil || proof != verified.ProofDigest() {
		return ledgerSourceHandoffFacts{}, ErrRecordLedgerFence
	}
	primary, witness := verified.PrimaryHead(), verified.WitnessHead()
	return ledgerSourceHandoffFacts{
		DeploymentID: origin.Source.DeploymentID, ProjectID: string(origin.Source.ProjectID),
		SourceKind: string(origin.Source.Kind), SourceID: origin.Source.ID, OperationID: request.OperationID, ActorID: request.ActorUserID,
		AppDomainID: origin.Application.ID, AppSystemIdentifier: origin.Application.SystemIdentifier,
		AppDatabaseOID: origin.Application.DatabaseOID, AppDatabaseName: origin.Application.DatabaseName, AppEpoch: origin.Application.Epoch,
		SourceRevision: ledgerSourceHandoffHex(origin.Revision), LastLiveScope: hex.EncodeToString(origin.LastLive.CanonicalBytes()),
		FinalFloorScope: hex.EncodeToString(request.FinalFloor.CanonicalBytes()), TombstoneMicros: uint64(origin.TombstoneRecordedAt.UnixMicro()),
		LedgerDomain: ledgerSourceHandoffHex(entry.LedgerDomain), LedgerSequence: entry.Sequence,
		LedgerEntryHash: ledgerSourceHandoffHex(entry.EntryHash), PreviousHash: ledgerSourceHandoffHex(entry.PreviousHash),
		CanonicalEntry: hex.EncodeToString(entry.Canonical), TokenCommitment: ledgerSourceHandoffHex(request.TokenCommitment),
		RequestFingerprint: ledgerSourceHandoffHex(request.RequestFingerprint), ConfirmedMicros: uint64(entry.ConfirmedAt.UnixMicro()),
		WitnessDomainID: witnessID, WitnessProofDigest: ledgerSourceHandoffHex(proof),
		PrimaryHeadSequence: primary.Sequence, PrimaryHeadHash: ledgerSourceHandoffHex(primary.Hash),
		WitnessHeadSequence: witness.Sequence, WitnessHeadHash: ledgerSourceHandoffHex(witness.Hash),
	}, nil
}

// Independently verifies the complete current pair even when a caller already
// has an entry. Decoding a canonical row does not grant installation authority.
func (f *RecordLedgerFence) sourceHandoffVerified(ctx context.Context, entry deletionledger.SourceRetirementEntry) (deletionledger.VerifiedSourceRetirement, error) {
	if f.appInventory.LegacyC80 || entry.Validate() != nil || entry.Request.Origin.Application != ledgerSourceApplication(f.request.AppIdentity) {
		return deletionledger.VerifiedSourceRetirement{}, ErrRecordLedgerFence
	}
	if err := f.VerifySourceRetirementProjection(ctx, entry.Request); err != nil {
		return deletionledger.VerifiedSourceRetirement{}, err
	}
	trust, err := f.Trust()
	if err != nil {
		return deletionledger.VerifiedSourceRetirement{}, err
	}
	primary, witness, err := f.bootstrapReaders()
	if err != nil {
		return deletionledger.VerifiedSourceRetirement{}, err
	}
	verifier, err := deletionledger.NewSourceRetirementVerifier(primary, witness, trust)
	if err != nil {
		return deletionledger.VerifiedSourceRetirement{}, err
	}
	verified, err := verifier.Verify(ctx, recorddeletion.LedgerLocator{LedgerDomain: entry.LedgerDomain, Sequence: entry.Sequence, EntryHash: entry.EntryHash})
	if err != nil || !verified.Valid() || !bytes.Equal(verified.Entry().Canonical, entry.Canonical) {
		return deletionledger.VerifiedSourceRetirement{}, ErrRecordLedgerFence
	}
	return verified, nil
}

func (f *RecordLedgerFence) sourceHandoffIntent(entry deletionledger.SourceRetirementEntry, facts ledgerSourceHandoffFacts) (ledgerSourceHandoffIntent, error) {
	preview, err := f.sourcePreview(entry.Request)
	if err != nil {
		return ledgerSourceHandoffIntent{}, err
	}
	pending, exists, err := ledgerReadSourceIntent(f.control, preview)
	if err != nil || !exists {
		return ledgerSourceHandoffIntent{}, ErrRecordLedgerFence
	}
	anchored, err := f.sourceAnchored(ledgerSourcePending{preview: preview, intent: pending, entry: entry, found: true})
	if err != nil || !anchored {
		return ledgerSourceHandoffIntent{}, ErrRecordLedgerFence
	}
	intent := ledgerSourceHandoffIntent{Version: 1, Key: preview.Key, Application: f.request.AppIdentity, Facts: facts.withoutHeads()}
	for _, file := range []struct {
		prefix string
		digest *[32]byte
	}{
		{"source-preview-", &intent.PreviewDigest}, {"source-intent-", &intent.SourceIntentDigest},
		{"source-witnessed-", &intent.WitnessedDigest}, {"source-anchored-", &intent.AnchoredDigest},
	} {
		body, exists, err := fenceReadFile(f.control, file.prefix+preview.Key+".json")
		if err != nil || !exists {
			return ledgerSourceHandoffIntent{}, ErrRecordLedgerFence
		}
		*file.digest = sha256.Sum256(body)
	}
	return intent, nil
}

func (f *RecordLedgerFence) sourceHandoffAttempts(ctx context.Context, key string, intentDigest [32]byte, facts ledgerSourceHandoffFacts) ([]ledgerSourceHandoffAttempt, error) {
	names, err := ledgerNames(f.control)
	if err != nil {
		return nil, err
	}
	prefix := "source-handoff-attempt-" + key + "-"
	var attempts []ledgerSourceHandoffAttempt
	for _, name := range names {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		number := uint64(len(attempts) + 1)
		if name != fmt.Sprintf("%s%06d.json", prefix, number) {
			return nil, ErrRecordLedgerFence
		}
		body, exists, err := fenceReadFile(f.control, name)
		var attempt ledgerSourceHandoffAttempt
		if err != nil || !exists || fenceDecode(body, &attempt) != nil || attempt.Version != 1 || attempt.Number != number ||
			attempt.IntentDigest != intentDigest || attempt.Command.withoutHeads() != facts.withoutHeads() ||
			!fenceRID.MatchString(attempt.Command.MaintenanceRID) || attempt.Backend.PID <= 0 || attempt.Backend.Started.IsZero() ||
			attempt.Backend.Role != f.appInventory.Admin {
			return nil, ErrRecordLedgerFence
		}
		var unresolved bool
		if err := f.app.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_stat_activity
			where pid=$1 and backend_start=$2 and usename=$3 and backend_xid is not null)`,
			attempt.Backend.PID, attempt.Backend.Started, attempt.Backend.Role).Scan(&unresolved); err != nil || unresolved {
			return nil, ErrRecordLedgerFence
		}
		attempts = append(attempts, attempt)
	}
	return attempts, nil
}

func ledgerSourceHandoffMatch(installation ledgerSourceHandoffInstallation, attempts []ledgerSourceHandoffAttempt, current ledgerSourceHandoffFacts) (uint64, error) {
	if installation.withoutHeads() != current.withoutHeads() || !fenceRID.MatchString(installation.FirstRID) ||
		installation.PrimaryHeadSequence > current.PrimaryHeadSequence || installation.WitnessHeadSequence > current.WitnessHeadSequence ||
		(installation.PrimaryHeadSequence == current.PrimaryHeadSequence && installation.PrimaryHeadHash != current.PrimaryHeadHash) ||
		(installation.WitnessHeadSequence == current.WitnessHeadSequence && installation.WitnessHeadHash != current.WitnessHeadHash) {
		return 0, ErrRecordLedgerFence
	}
	for _, attempt := range attempts {
		if installation.ledgerSourceHandoffFacts == attempt.Command.ledgerSourceHandoffFacts &&
			installation.FirstRID == attempt.Command.MaintenanceRID && installation.FirstGeneration == attempt.Command.MaintenanceGeneration {
			return attempt.Number, nil
		}
	}
	return 0, ErrRecordLedgerFence
}

func (f *RecordLedgerFence) installSourceRetirementHandoff(ctx context.Context, supplied deletionledger.VerifiedSourceRetirement) error {
	if !supplied.Valid() {
		return ErrRecordLedgerFence
	}
	return f.sourceRetirementHandoff(ctx, supplied.Entry(), true)
}

func (f *RecordLedgerFence) verifySourceRetirementHandoff(ctx context.Context, entry deletionledger.SourceRetirementEntry) error {
	return f.sourceRetirementHandoff(ctx, entry, false)
}

func (f *RecordLedgerFence) sourceRetirementHandoff(ctx context.Context, entry deletionledger.SourceRetirementEntry, install bool) error {
	if err := f.enter(ctx); err != nil {
		return err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	f.sourceEffect.Lock()
	defer f.sourceEffect.Unlock()
	verified, err := f.sourceHandoffVerified(ctx, entry)
	if err != nil {
		return err
	}
	facts, err := ledgerSourceHandoffMaterial(verified, f.request.Witness.Identity.ID)
	if err != nil {
		return err
	}
	intent, err := f.sourceHandoffIntent(entry, facts)
	if err != nil {
		return err
	}
	intentName := "source-handoff-intent-" + intent.Key + ".json"
	if install {
		if _, err = ledgerPublishExact(f.control, intentName, intent); err != nil {
			return err
		}
	} else if ledgerReadExact(f.control, intentName, intent) != nil {
		return ErrRecordLedgerFence
	}
	intentBytes, err := json.Marshal(intent)
	if err != nil {
		return ErrRecordLedgerFence
	}
	intentDigest := sha256.Sum256(intentBytes)
	attempts, err := f.sourceHandoffAttempts(ctx, intent.Key, intentDigest, facts)
	if err != nil {
		return err
	}
	receiptName := "source-handoff-installed-" + intent.Key + ".json"
	receiptBytes, completed, err := fenceReadFile(f.control, receiptName)
	if err != nil {
		return err
	}
	var receipt ledgerSourceHandoffReceipt
	if completed && (fenceDecode(receiptBytes, &receipt) != nil || receipt.Version != 1 || receipt.IntentDigest != intentDigest) {
		return ErrRecordLedgerFence
	}
	if !install && !completed {
		return ErrRecordLedgerFence
	}
	command := ledgerSourceHandoffCommand{ledgerSourceHandoffFacts: facts, MaintenanceRID: f.binding.RecoveryID, MaintenanceGeneration: f.binding.Generation}
	body, err := json.Marshal(command)
	if err != nil {
		return ErrRecordLedgerFence
	}
	pool, err := f.OpenAdmin(ctx, f.appCredential.NewURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer connection.Release()
	query := `select public.source_legacy_retirement_verify($1)`
	var effectBackend ledgerAppSession
	if install && !completed {
		if err := connection.QueryRow(ctx, `select pid,backend_start,usename from pg_catalog.pg_stat_activity where pid=pg_backend_pid()`).Scan(
			&effectBackend.PID, &effectBackend.Started, &effectBackend.Role); err != nil || effectBackend.Role != f.appInventory.Admin {
			return ErrRecordLedgerFence
		}
		attempt := ledgerSourceHandoffAttempt{Version: 1, Number: uint64(len(attempts) + 1), IntentDigest: intentDigest, Command: command, Backend: effectBackend}
		if _, err := ledgerPublishExact(f.control, fmt.Sprintf("source-handoff-attempt-%s-%06d.json", intent.Key, attempt.Number), attempt); err != nil {
			return err
		}
		attempts = append(attempts, attempt)
		query = `select public.source_legacy_retirement_install($1)`
	}
	if err := f.verifyExternal(ctx); err != nil {
		return err
	}
	var result []byte
	if err := connection.QueryRow(ctx, query, body).Scan(&result); err != nil {
		return ErrRecordLedgerFence
	}
	if install && !completed {
		if err := f.sourceCutpoint(ctx, "handoff-install", entry.Request, ledgerSourceFaultBackend(effectBackend)); err != nil {
			return err
		}
	}
	var installation ledgerSourceHandoffInstallation
	if fenceDecode(result, &installation) != nil {
		return ErrRecordLedgerFence
	}
	attempt, err := ledgerSourceHandoffMatch(installation, attempts, facts)
	if err != nil {
		return err
	}
	// A fresh query after the autocommitted command is the committed readback.
	var readback []byte
	if err := connection.QueryRow(ctx, `select public.source_legacy_retirement_verify($1)`, body).Scan(&readback); err != nil || !bytes.Equal(result, readback) {
		return ErrRecordLedgerFence
	}
	if err := f.verifyExternal(ctx); err != nil {
		return err
	}
	want := ledgerSourceHandoffReceipt{Version: 1, IntentDigest: intentDigest, Attempt: attempt, Installation: installation}
	if completed {
		if receipt != want {
			return ErrRecordLedgerFence
		}
		return nil
	}
	if _, err := ledgerPublishExact(f.control, receiptName, want); err != nil {
		return err
	}
	return f.sourceCutpoint(ctx, "handoff-return", entry.Request, ledgerSourceFaultBackend(effectBackend))
}
