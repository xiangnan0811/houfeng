package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/deploy"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/sourceretirement"
)

var (
	errLegacySourceInput     = errors.New("invalid legacy source retirement request; supply an exact private management request")
	errLegacySourceOperation = errors.New("legacy source retirement failed; keep maintenance closed and resolve the original request")
	errLegacySourceOutput    = errors.New("legacy source retirement output failed; keep the original private request and resolve its status")
)

type legacySourcePreviewManagement struct {
	ActorUserID         string                     `json:"actor_user_id"`
	Source              legacySourceKey            `json:"source"`
	FinalFloor          legacySourceScope          `json:"final_floor"`
	OutputRequestFile   string                     `json:"output_request_file"`
	AppAdminURL         string                     `json:"app_admin_url"`
	AppPreviousAdminURL string                     `json:"app_previous_admin_url"`
	Fence               recordDeletionFenceRequest `json:"fence"`
}

type legacySourceResumeManagement struct {
	OriginalRequestFile string                     `json:"original_request_file"`
	AppAdminURL         string                     `json:"app_admin_url"`
	AppPreviousAdminURL string                     `json:"app_previous_admin_url"`
	Fence               recordDeletionFenceRequest `json:"fence"`
}

var legacySourcePreviewSchema = sourceRequestSchema{
	"actor_user_id": nil, "source": legacySourceKeySchema, "final_floor": legacySourceScopeSchema,
	"output_request_file": nil, "app_admin_url": nil, "app_previous_admin_url": nil, "fence": recordDeletionRequestSchema["fence"],
}
var legacySourceResumeSchema = sourceRequestSchema{
	"original_request_file": nil, "app_admin_url": nil, "app_previous_admin_url": nil, "fence": recordDeletionRequestSchema["fence"],
}

type legacySourceFence interface {
	ProvisionRecordScopes(context.Context) error
	PreviewLegacySourceRetirement(context.Context, string, sourceretirement.SourceKey, recordauth.VisibilityScope) (deletionledger.SourceRetirementRequest, [32]byte, error)
	RetireLegacySourceRetirement(context.Context, deletionledger.SourceRetirementRequest, [32]byte) (deploy.LegacySourceRetirementResult, error)
	ReconcileLegacySourceRetirement(context.Context, deletionledger.SourceRetirementRequest) (deploy.LegacySourceRetirementResult, error)
}

type legacySourceMaintenance func(context.Context, deploy.RecordLedgerMaintenanceRequest, func(legacySourceFence) error) error

func withLegacySourceMaintenance(ctx context.Context, request deploy.RecordLedgerMaintenanceRequest, fn func(legacySourceFence) error) error {
	return deploy.WithLegacySourceLedgerMaintenance(ctx, request, func(fence *deploy.RecordLedgerFence) error {
		return fn(fence)
	})
}

func runSourceLegacyRetirement(ctx context.Context, args []string) error {
	return runSourceLegacyRetirementWith(ctx, args, os.Stdout, withLegacySourceMaintenance)
}

func runSourceLegacyRetirementWith(ctx context.Context, args []string, output io.Writer, maintenance legacySourceMaintenance) error {
	if ctx == nil || output == nil || maintenance == nil || len(args) != 4 || args[0] != "source-authority" ||
		args[2] != "--request-file" || args[3] == "" {
		return errLegacySourceInput
	}
	command := args[1]
	if command != "legacy-retirement-preview" && command != "legacy-retire" && command != "legacy-retirement-reconcile" {
		return errLegacySourceInput
	}
	body, err := readSourceEnrollmentRequest(args[3])
	if err != nil {
		return errLegacySourceInput
	}
	defer clear(body)
	if command == "legacy-retirement-preview" {
		return legacySourcePreview(ctx, body, output, maintenance)
	}
	return legacySourceResume(ctx, command, body, output, maintenance)
}

func legacySourceMaintenanceValid(adminURL, previousAdminURL string, fence recordDeletionFenceRequest) bool {
	app := fence.AppIdentity
	return (deletionledger.SourceApplicationIdentity{ID: app.ID, SystemIdentifier: app.SystemIdentifier, DatabaseName: app.DatabaseName,
		DatabaseOID: app.DatabaseOID, Epoch: app.Epoch}).Validate() == nil &&
		validateRecordLedgerMaintenance(adminURL, previousAdminURL, fence) == nil
}

func legacySourcePreview(ctx context.Context, body []byte, output io.Writer, maintenance legacySourceMaintenance) error {
	var input legacySourcePreviewManagement
	if decodeLegacySourceJSON(body, &input, legacySourcePreviewSchema) != nil ||
		!legacySourceMaintenanceValid(input.AppAdminURL, input.AppPreviousAdminURL, input.Fence) ||
		recordauth.ValidateActorUserID(input.ActorUserID) != nil {
		return errLegacySourceInput
	}
	key, err := input.Source.source()
	if err != nil {
		return errLegacySourceInput
	}
	floor, err := input.FinalFloor.scope()
	if err != nil || floor.ProjectID != key.ProjectID {
		return errLegacySourceInput
	}
	private, err := reserveLegacySourceOutput(input.OutputRequestFile)
	if err != nil {
		return err
	}
	defer private.close()
	var operationID string
	completed := false
	var outputToken [32]byte
	defer clear(outputToken[:])
	err = maintenance(ctx, recordLedgerMaintenance(input.AppAdminURL, input.AppPreviousAdminURL, input.Fence), func(fence legacySourceFence) error {
		if fence == nil || fence.ProvisionRecordScopes(ctx) != nil {
			return errLegacySourceOperation
		}
		request, token, previewErr := fence.PreviewLegacySourceRetirement(ctx, input.ActorUserID, key, floor)
		defer clear(token[:])
		if previewErr != nil || !legacySourcePreviewMatches(request, token, input, key, floor) {
			return errLegacySourceOperation
		}
		if private.publish(legacySourceTokenFileFrom(request, token)) != nil {
			return errLegacySourceOutput
		}
		operationID = request.OperationID
		outputToken = token
		completed = true
		return nil
	})
	if err != nil || !completed {
		if errors.Is(err, errLegacySourceOutput) {
			return errLegacySourceOutput
		}
		return errLegacySourceOperation
	}
	result := struct {
		Version                 uint64          `json:"version"`
		Action                  string          `json:"action"`
		DeletionContractVersion uint64          `json:"deletion_contract_version"`
		State                   string          `json:"state"`
		OperationID             string          `json:"operation_id"`
		Source                  legacySourceKey `json:"source"`
	}{legacySourceFileVersion, legacySourceAction, legacySourceContractVersion, "preview_recorded", operationID, input.Source}
	return writeLegacySourceResult(output, result, outputToken)
}

func legacySourcePreviewMatches(request deletionledger.SourceRetirementRequest, token [32]byte, input legacySourcePreviewManagement, key sourceretirement.SourceKey, floor recordauth.VisibilityScope) bool {
	app := input.Fence.AppIdentity
	expectedApp := deletionledger.SourceApplicationIdentity{ID: app.ID, SystemIdentifier: app.SystemIdentifier,
		DatabaseName: app.DatabaseName, DatabaseOID: app.DatabaseOID, Epoch: app.Epoch}
	commitment, err := deletionledger.SourceTokenCommitment(key, token)
	return err == nil && request.Validate() == nil && request.ActorUserID == input.ActorUserID && request.Origin.Source == key &&
		request.Origin.Application == expectedApp && request.TokenCommitment == commitment &&
		request.FinalFloor.CanonicalHash == floor.CanonicalHash && bytes.Equal(request.FinalFloor.CanonicalBytes(), floor.CanonicalBytes())
}

func legacySourceResume(ctx context.Context, command string, body []byte, output io.Writer, maintenance legacySourceMaintenance) error {
	var input legacySourceResumeManagement
	if decodeLegacySourceJSON(body, &input, legacySourceResumeSchema) != nil ||
		!legacySourceMaintenanceValid(input.AppAdminURL, input.AppPreviousAdminURL, input.Fence) {
		return errLegacySourceInput
	}
	privateBody, err := readLegacySourceTokenFile(input.OriginalRequestFile)
	if err != nil {
		return errLegacySourceInput
	}
	defer clear(privateBody)
	var wire legacySourceTokenFile
	if decodeLegacySourceJSON(privateBody, &wire, legacySourceTokenSchema) != nil {
		return errLegacySourceInput
	}
	request, token, err := wire.request()
	if err != nil {
		return errLegacySourceInput
	}
	defer clear(token[:])
	// Reconcile can use a separately authorized current APP after restore. The
	// held fence, not a self-reported identity in this file, proves continuity.
	if command == "legacy-retire" {
		app := input.Fence.AppIdentity
		if request.Origin.Application != (deletionledger.SourceApplicationIdentity{ID: app.ID, SystemIdentifier: app.SystemIdentifier,
			DatabaseName: app.DatabaseName, DatabaseOID: app.DatabaseOID, Epoch: app.Epoch}) {
			return errLegacySourceInput
		}
	}
	var result deploy.LegacySourceRetirementResult
	completed := false
	err = maintenance(ctx, recordLedgerMaintenance(input.AppAdminURL, input.AppPreviousAdminURL, input.Fence), func(fence legacySourceFence) error {
		if fence == nil || fence.ProvisionRecordScopes(ctx) != nil {
			return errLegacySourceOperation
		}
		var callErr error
		if command == "legacy-retire" {
			result, callErr = fence.RetireLegacySourceRetirement(ctx, request, token)
		} else {
			result, callErr = fence.ReconcileLegacySourceRetirement(ctx, request)
		}
		if callErr != nil || result.Entry.Validate() != nil || result.Entry.Request.RequestFingerprint != request.RequestFingerprint {
			return errLegacySourceOperation
		}
		completed = true
		return nil
	})
	if err != nil || !completed {
		return errLegacySourceOperation
	}
	state := "witnessed_handoff_pending"
	if result.HandoffInstalled {
		state = "handoff_installed"
	}
	response := struct {
		Version                               uint64          `json:"version"`
		Action                                string          `json:"action"`
		DeletionContractVersion               uint64          `json:"deletion_contract_version"`
		State                                 string          `json:"state"`
		OperationID                           string          `json:"operation_id"`
		Source                                legacySourceKey `json:"source"`
		LedgerDomain                          string          `json:"ledger_domain"`
		LedgerSequence                        uint64          `json:"ledger_sequence"`
		LedgerEntryHash                       string          `json:"ledger_entry_hash"`
		SourceAuthorizationPermanentlyRetired bool            `json:"source_authorization_permanently_retired"`
		SourceAssetContentRetained            bool            `json:"source_asset_content_retained"`
		W2LegacyBlockerUnchanged              bool            `json:"w2_legacy_blocker_unchanged"`
	}{legacySourceFileVersion, legacySourceAction, legacySourceContractVersion, state, request.OperationID,
		legacySourceKeyFrom(request.Origin.Source), hex.EncodeToString(result.Entry.LedgerDomain[:]), result.Entry.Sequence,
		hex.EncodeToString(result.Entry.EntryHash[:]), true, true, true}
	return writeLegacySourceResult(output, response, token)
}

// Even an operator-supplied identity or a ledger digest must not cause the
// original private token to be echoed as part of an otherwise public result.
func writeLegacySourceResult(output io.Writer, value any, token [32]byte) error {
	body, err := json.Marshal(value)
	if err != nil {
		return errLegacySourceOutput
	}
	body = append(body, '\n')
	defer clear(body)
	encoded := hex.EncodeToString(token[:])
	for _, secret := range [][]byte{token[:], []byte(encoded), []byte(strings.ToUpper(encoded)),
		[]byte(base64.StdEncoding.EncodeToString(token[:])), []byte(base64.RawStdEncoding.EncodeToString(token[:])),
		[]byte(base64.URLEncoding.EncodeToString(token[:])), []byte(base64.RawURLEncoding.EncodeToString(token[:]))} {
		if bytes.Contains(body, secret) {
			return errLegacySourceOutput
		}
	}
	if n, err := output.Write(body); err != nil || n != len(body) {
		return errLegacySourceOutput
	}
	return nil
}
