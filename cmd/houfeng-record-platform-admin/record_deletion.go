package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/deploy"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/store"
)

var errRecordDeletionRequest = errors.New("record deletion request is invalid; supply a private exact management request")

type recordDeletionPhysicalIdentity struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	SystemIdentifier string `json:"system_identifier"`
	DatabaseName     string `json:"database_name"`
	DatabaseOID      uint32 `json:"database_oid"`
	Epoch            uint64 `json:"epoch"`
}

func (identity recordDeletionPhysicalIdentity) ledger() recorddeletion.LedgerPhysicalIdentity {
	return recorddeletion.LedgerPhysicalIdentity{ID: identity.ID, Kind: identity.Kind, SystemIdentifier: identity.SystemIdentifier,
		DatabaseName: identity.DatabaseName, DatabaseOID: identity.DatabaseOID, Epoch: identity.Epoch}
}

type recordDeletionDomainRequest struct {
	Identity        recordDeletionPhysicalIdentity `json:"identity"`
	BootstrapURL    string                         `json:"bootstrap_url"`
	ReaderURL       string                         `json:"reader_url"`
	WriterURL       string                         `json:"writer_url"`
	PreviousWriters []string                       `json:"previous_writers"`
}

func (domain recordDeletionDomainRequest) maintenance() deploy.RecordLedgerDomainRequest {
	return deploy.RecordLedgerDomainRequest{Identity: domain.Identity.ledger(), BootstrapURL: domain.BootstrapURL,
		ReaderURL: domain.ReaderURL, WriterURL: domain.WriterURL, PreviousWriters: append([]string(nil), domain.PreviousWriters...)}
}

type recordDeletionComposeRequest struct {
	RecoveryID          string `json:"recovery_id"`
	Project             string `json:"project"`
	WorkingDirectory    string `json:"working_directory"`
	DatabaseContainerID string `json:"database_container_id"`
	ControlConfigPath   string `json:"control_config_path"`
}
type recordDeletionApplicationIdentity struct {
	ID               string `json:"id"`
	SystemIdentifier string `json:"system_identifier"`
	DatabaseName     string `json:"database_name"`
	DatabaseOID      uint32 `json:"database_oid"`
	Epoch            uint64 `json:"epoch"`
}
type recordDeletionFenceRequest struct {
	Compose             recordDeletionComposeRequest      `json:"compose"`
	AppBootstrapURL     string                            `json:"app_bootstrap_url"`
	AppIdentity         recordDeletionApplicationIdentity `json:"app_identity"`
	AppAdminRole        string                            `json:"app_admin_role"`
	AppPreviousLogins   []string                          `json:"app_previous_logins"`
	Primary             recordDeletionDomainRequest       `json:"primary"`
	Witness             recordDeletionDomainRequest       `json:"witness"`
	ComposeContainerIDs []string                          `json:"compose_container_ids"`
	HostCgroups         []string                          `json:"host_cgroups"`
}
type recordDeletionRequest struct {
	DeploymentID        string                     `json:"deployment_id"`
	ProjectID           string                     `json:"project_id"`
	OperationID         string                     `json:"operation_id"`
	LedgerSequence      uint64                     `json:"ledger_sequence"`
	LedgerEntryHash     string                     `json:"ledger_entry_hash"`
	PreviousSequence    uint64                     `json:"previous_sequence"`
	PreviousHash        string                     `json:"previous_hash"`
	AppAdminURL         string                     `json:"app_admin_url"`
	AppPreviousAdminURL string                     `json:"app_previous_admin_url"`
	Fence               recordDeletionFenceRequest `json:"fence"`
}

var recordDeletionPhysicalSchema = sourceRequestSchema{
	"id": nil, "kind": nil, "system_identifier": nil, "database_name": nil, "database_oid": nil, "epoch": nil,
}
var recordDeletionDomainSchema = sourceRequestSchema{
	"identity": recordDeletionPhysicalSchema, "bootstrap_url": nil, "reader_url": nil, "writer_url": nil, "previous_writers": nil,
}
var recordDeletionRequestSchema = sourceRequestSchema{
	"deployment_id": nil, "project_id": nil, "operation_id": nil, "ledger_sequence": nil, "ledger_entry_hash": nil,
	"previous_sequence": nil, "previous_hash": nil, "app_admin_url": nil, "app_previous_admin_url": nil,
	"fence": {
		"compose":           {"recovery_id": nil, "project": nil, "working_directory": nil, "database_container_id": nil, "control_config_path": nil},
		"app_bootstrap_url": nil, "app_identity": {"id": nil, "system_identifier": nil, "database_name": nil, "database_oid": nil, "epoch": nil},
		"app_admin_role": nil, "app_previous_logins": nil,
		"primary": recordDeletionDomainSchema, "witness": recordDeletionDomainSchema,
		"compose_container_ids": nil, "host_cgroups": nil,
	},
}

func (request recordDeletionRequest) validate() error {
	if recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(request.DeploymentID)) != nil ||
		recordplatform.ValidateProjectID(recordplatform.ProjectID(request.ProjectID)) != nil ||
		request.LedgerSequence > math.MaxInt64 || request.PreviousSequence > math.MaxInt64 ||
		validateRecordLedgerMaintenance(request.AppAdminURL, request.AppPreviousAdminURL, request.Fence) != nil {
		return errRecordDeletionRequest
	}
	if request.OperationID != "" {
		if !validLegacyOperationID(request.OperationID) {
			return errRecordDeletionRequest
		}
		if request.LedgerSequence != 0 || request.LedgerEntryHash != "" || request.PreviousSequence != 0 || request.PreviousHash != "" {
			return errRecordDeletionRequest
		}
	} else {
		if request.LedgerSequence == 0 || request.PreviousSequence != request.LedgerSequence-1 {
			return errRecordDeletionRequest
		}
		if _, err := decodeRecordDeletionDigest(request.LedgerEntryHash, false); err != nil {
			return errRecordDeletionRequest
		}
		previous, err := decodeRecordDeletionCursorHash(request.PreviousHash)
		if err != nil || (request.PreviousSequence == 0) != (previous == ([32]byte{})) {
			return errRecordDeletionRequest
		}
	}
	return nil
}

// validateRecordLedgerMaintenance applies the same endpoint and physical
// identity checks to record and source maintenance requests.
func validateRecordLedgerMaintenance(appAdminURL, appPreviousAdminURL string, fence recordDeletionFenceRequest) error {
	if fence.Primary.Identity.ledger().Validate() != nil || fence.Witness.Identity.ledger().Validate() != nil ||
		fence.Primary.Identity.Kind != "deletion_ledger" || fence.Witness.Identity.Kind != "deletion_witness" ||
		fence.Primary.Identity.SystemIdentifier == fence.Witness.Identity.SystemIdentifier || len(fence.HostCgroups) == 0 ||
		fence.AppIdentity.SystemIdentifier == fence.Primary.Identity.SystemIdentifier ||
		fence.AppIdentity.SystemIdentifier == fence.Witness.Identity.SystemIdentifier {
		return errRecordDeletionRequest
	}
	for _, group := range fence.HostCgroups {
		if !filepath.IsAbs(group) || filepath.Clean(group) != group || !strings.HasPrefix(group, "/sys/fs/cgroup/") {
			return errRecordDeletionRequest
		}
	}
	// Parse every secret-bearing URL before invoking the maintenance fence.
	adminConfig, err := platformmigrate.ParseSourceAuthorityPostgresConfig(appAdminURL)
	if err != nil || adminConfig.ConnConfig.User != fence.AppAdminRole || len(adminConfig.ConnConfig.Password) < 32 ||
		adminConfig.ConnConfig.Database != fence.AppIdentity.DatabaseName {
		return errRecordDeletionRequest
	}
	previousAdminConfig, err := platformmigrate.ParseSourceAuthorityPostgresConfig(appPreviousAdminURL)
	if err != nil || previousAdminConfig.ConnConfig.User != adminConfig.ConnConfig.User ||
		previousAdminConfig.ConnConfig.Database != adminConfig.ConnConfig.Database ||
		previousAdminConfig.ConnConfig.Host != adminConfig.ConnConfig.Host || previousAdminConfig.ConnConfig.Port != adminConfig.ConnConfig.Port ||
		previousAdminConfig.ConnConfig.Password == adminConfig.ConnConfig.Password {
		return errRecordDeletionRequest
	}
	bootstrapConfig, err := platformmigrate.ParseSourceAuthorityPostgresConfig(fence.AppBootstrapURL)
	if err != nil || bootstrapConfig.ConnConfig.Host != adminConfig.ConnConfig.Host || bootstrapConfig.ConnConfig.Port != adminConfig.ConnConfig.Port ||
		bootstrapConfig.ConnConfig.Database != adminConfig.ConnConfig.Database || bootstrapConfig.ConnConfig.Password == adminConfig.ConnConfig.Password {
		return errRecordDeletionRequest
	}
	urls := []string{fence.Primary.BootstrapURL, fence.Primary.ReaderURL, fence.Primary.WriterURL,
		fence.Witness.BootstrapURL, fence.Witness.ReaderURL, fence.Witness.WriterURL}
	for _, value := range urls {
		config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(value)
		if err != nil || config.ConnConfig.Password == adminConfig.ConnConfig.Password {
			return errRecordDeletionRequest
		}
	}
	return nil
}

func validLegacyOperationID(value string) bool {
	if len(value) < len("rpo_")+1 || len(value) > len("rpo_")+64 || value[:len("rpo_")] != "rpo_" {
		return false
	}
	for _, ch := range value[len("rpo_"):] {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

func decodeRecordDeletionCursorHash(value string) ([32]byte, error) {
	var result [32]byte
	if len(value) != 64 {
		return result, errRecordDeletionRequest
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return result, errRecordDeletionRequest
	}
	copy(result[:], decoded)
	return result, nil
}

func (request recordDeletionRequest) maintenance() deploy.RecordLedgerMaintenanceRequest {
	return recordLedgerMaintenance(request.AppAdminURL, request.AppPreviousAdminURL, request.Fence)
}

func recordLedgerMaintenance(appAdminURL, appPreviousAdminURL string, fence recordDeletionFenceRequest) deploy.RecordLedgerMaintenanceRequest {
	c, app := fence.Compose, fence.AppIdentity
	return deploy.RecordLedgerMaintenanceRequest{
		Compose: deploy.ComposeOfflineFenceRequest{RecoveryID: c.RecoveryID, Project: c.Project,
			WorkingDirectory: c.WorkingDirectory, DatabaseContainerID: c.DatabaseContainerID, ControlConfigPath: c.ControlConfigPath},
		AppBootstrapURL:     fence.AppBootstrapURL,
		AppPreviousAdminURL: appPreviousAdminURL, AppAdminURL: appAdminURL,
		AppIdentity: deploy.RecordLedgerApplicationIdentity{ID: app.ID, SystemIdentifier: app.SystemIdentifier,
			DatabaseName: app.DatabaseName, DatabaseOID: app.DatabaseOID, Epoch: app.Epoch},
		AppAdminRole: fence.AppAdminRole, AppPreviousLogins: append([]string(nil), fence.AppPreviousLogins...),
		Primary: fence.Primary.maintenance(), Witness: fence.Witness.maintenance(),
		ComposeContainerIDs: append([]string(nil), fence.ComposeContainerIDs...),
		HostCgroups:         append([]string(nil), fence.HostCgroups...),
	}
}

type legacyC80ServiceManagement struct {
	ActorUserID         string                     `json:"actor_user_id"`
	RecordID            string                     `json:"record_id"`
	AppAdminURL         string                     `json:"app_admin_url"`
	AppPreviousAdminURL string                     `json:"app_previous_admin_url"`
	Fence               recordDeletionFenceRequest `json:"fence"`
}

var legacyC80ServiceSchema = sourceRequestSchema{
	"actor_user_id": nil, "record_id": nil, "app_admin_url": nil, "app_previous_admin_url": nil,
	"fence": recordDeletionRequestSchema["fence"],
}

type legacyC80ServiceResponse struct {
	Version     int    `json:"version"`
	Status      string `json:"status"`
	OperationID string `json:"operation_id"`
	Reason      string `json:"reason"`
}

// legacyC80HandledResultError is returned only after the complete controlled
// failure frame has been written. The caller must still exit unsuccessfully.
type legacyC80HandledResultError struct{}

func (*legacyC80HandledResultError) Error() string {
	return "legacy C80 service returned a controlled failure"
}

type legacyC80Maintenance func(context.Context, deploy.RecordLedgerMaintenanceRequest, deploy.LegacyC80ServiceRequest) (deploy.LegacyC80ServiceResult, error)

func runLegacyC80Service(ctx context.Context, args []string) error {
	return runLegacyC80ServiceWith(ctx, args, os.Stdout, deploy.PrepareLegacyC80ServiceCoordinator, deploy.RunLegacyC80ServiceMaintenance)
}

func runLegacyC80ServiceWith(ctx context.Context, args []string, output io.Writer, prepare func() error, maintenance legacyC80Maintenance) error {
	if output == nil {
		return errRecordDeletionAdmin
	}
	unavailable := func(reason string) error {
		return writeLegacyC80ServiceResult(output, legacyC80ServiceResponse{Version: 1, Status: "unavailable", Reason: reason}, true)
	}
	if ctx == nil || prepare == nil || maintenance == nil || len(args) != 4 || args[0] != "record-deletion" ||
		args[1] != "legacy-c80-service" || args[2] != "--request-file" || args[3] == "" {
		return unavailable("admission_unavailable")
	}
	// The child shares the coordinator's UID. Restrict access to the parent
	// before opening or decoding any credential-bearing request.
	if prepare() != nil {
		return unavailable("admission_unavailable")
	}
	body, err := readSourceEnrollmentRequest(args[3])
	if err != nil {
		return unavailable("admission_unavailable")
	}
	defer clear(body)
	var request legacyC80ServiceManagement
	if decodeLegacySourceJSON(body, &request, legacyC80ServiceSchema) != nil ||
		!legacySourceMaintenanceValid(request.AppAdminURL, request.AppPreviousAdminURL, request.Fence) ||
		recordauth.ValidateActorUserID(request.ActorUserID) != nil || recordidentity.Classify(request.RecordID) != recordidentity.Legacy {
		return unavailable("admission_unavailable")
	}
	result, err := maintenance(ctx, recordLedgerMaintenance(request.AppAdminURL, request.AppPreviousAdminURL, request.Fence),
		deploy.LegacyC80ServiceRequest{ActorUserID: request.ActorUserID, RecordID: request.RecordID, RealmFD: 5})
	if err != nil {
		if result.Status == "unavailable" && result.OperationID == "" && validLegacyC80UnavailableReason(result.Reason) {
			return unavailable(result.Reason)
		}
		return unavailable("service_unavailable")
	}
	response := legacyC80ServiceResponse{Version: 1, Status: result.Status, OperationID: result.OperationID, Reason: result.Reason}
	switch {
	case result.Status == "committed" && result.Reason == "app_reserved" && validLegacyOperationID(result.OperationID):
		return writeLegacyC80ServiceResult(output, response, false)
	case result.Status == "rejected" && result.Reason == "source_denied" && result.OperationID == "":
		return writeLegacyC80ServiceResult(output, response, false)
	case result.Status == "recovered_committed" && result.Reason == "reserve_committed" && validLegacyOperationID(result.OperationID):
		return writeLegacyC80ServiceResult(output, response, false)
	case result.Status == "unavailable" && result.OperationID == "" && validLegacyC80ChildUnavailableReason(result.Reason):
		return writeLegacyC80ServiceResult(output, response, true)
	default:
		return unavailable("service_unavailable")
	}
}

func validLegacyC80UnavailableReason(reason string) bool {
	return validLegacyC80ChildUnavailableReason(reason) || reason == "realm_unavailable" || reason == "unexpected_session"
}

func validLegacyC80ChildUnavailableReason(reason string) bool {
	switch reason {
	case "inventory_unavailable", "admission_unavailable", "service_unavailable":
		return true
	default:
		return false
	}
}

func writeLegacyC80ServiceResult(output io.Writer, result legacyC80ServiceResponse, failed bool) error {
	body, err := json.Marshal(result)
	if err != nil {
		return errRecordDeletionAdmin
	}
	body = append(body, '\n')
	n, err := output.Write(body)
	if err != nil || n != len(body) {
		return errRecordDeletionAdmin
	}
	if failed {
		return &legacyC80HandledResultError{}
	}
	return nil
}

// runRecordDeletion accepts only an existing V1 operation or a single current
// recovery cursor. Credential-bearing inputs are read from a private file;
// no DSN or provider credential is included in its result or error messages.
func runRecordDeletion(ctx context.Context, args []string) error {
	if ctx == nil || len(args) != 4 || args[0] != "record-deletion" || args[1] != "reconcile-legacy" || args[2] != "--request-file" || args[3] == "" {
		return errRecordDeletionRequest
	}
	body, err := readSourceEnrollmentRequest(args[3])
	if err != nil {
		return errRecordDeletionRequest
	}
	var request recordDeletionRequest
	if decodeSourceEnrollmentRequest(body, &request, recordDeletionRequestSchema) != nil || request.validate() != nil {
		return errRecordDeletionRequest
	}
	var result struct {
		LedgerDomain    string `json:"ledger_domain"`
		LedgerSequence  uint64 `json:"ledger_sequence"`
		LedgerEntryHash string `json:"ledger_entry_hash"`
		ContentPurged   bool   `json:"content_purged"`
		ReplayApplied   bool   `json:"replay_applied"`
	}
	err = deploy.WithRecordLedgerMaintenance(ctx, request.maintenance(), func(fence *deploy.RecordLedgerFence) error {
		return fence.WithHeldLedgerFence(ctx, func(ctx context.Context) error {
			if err := fence.ProvisionRecordScopes(ctx); err != nil {
				return err
			}
			admin, err := fence.OpenAdmin(ctx, request.AppAdminURL)
			if err != nil {
				return err
			}
			defer admin.Close()
			primary, witness, err := fence.Clients()
			if err != nil {
				return err
			}
			primaryReader, witnessReader, err := fence.Readers()
			if err != nil {
				return err
			}
			trust, err := fence.Trust()
			if err != nil {
				return err
			}
			domain, err := recorddeletion.LedgerDomainDigest(trust.Primary.ID)
			if err != nil {
				return err
			}
			adminStore := &recordDeletionAdminStore{pool: admin, deployment: recordplatform.DeploymentID(request.DeploymentID),
				project: recordplatform.ProjectID(request.ProjectID), fence: fence}
			coordinator, err := recorddeletion.NewLegacyLedgerCoordinator(adminStore, primary, witness, primaryReader, witnessReader, fence, domain)
			if err != nil {
				return err
			}
			var locator recorddeletion.LedgerLocator
			if request.OperationID != "" {
				locator, err = coordinator.ReconcileOperation(ctx, request.OperationID)
				if err != nil {
					return err
				}
			} else {
				hash, decodeErr := decodeRecordDeletionDigest(request.LedgerEntryHash, false)
				if decodeErr != nil {
					return decodeErr
				}
				previousHash, decodeErr := decodeRecordDeletionCursorHash(request.PreviousHash)
				if decodeErr != nil {
					return decodeErr
				}
				locator = recorddeletion.LedgerLocator{LedgerDomain: domain, Sequence: request.LedgerSequence, EntryHash: hash}
				if err := coordinator.InstallRecoveryEntry(ctx, locator); err != nil {
					return err
				}
				// The installed prefix advanced the protected anchors; build the replay
				// verifier from the fresh held trust, not the previous snapshot.
				trust, err = fence.Trust()
				if err != nil {
					return err
				}
				verifier, err := recorddeletion.NewLedgerVerifier(primaryReader, witnessReader, trust)
				if err != nil {
					return err
				}
				adapter, err := recorddeletion.NewRecoveryAdapter(store.NewPostgresRecordDeletionRepository(admin, nil), verifier, fence)
				if err != nil {
					return err
				}
				receipt, err := adapter.Replay(ctx, recorddeletion.RecoveryReplayRequest{Locator: locator,
					Cursor: recorddeletion.RecoveryReplayCursor{Sequence: request.PreviousSequence, EntryHash: previousHash}})
				if err != nil {
					return err
				}
				result.ReplayApplied = true
				result.ContentPurged = receipt.ContentPurged
			}
			result.LedgerDomain = hex.EncodeToString(locator.LedgerDomain[:])
			result.LedgerSequence = locator.Sequence
			result.LedgerEntryHash = hex.EncodeToString(locator.EntryHash[:])
			return nil
		})
	})
	if err != nil {
		if errors.Is(err, recorddeletion.ErrLegacyLedgerUnresolved) {
			return recorddeletion.ErrLegacyLedgerUnresolved
		}
		return safeRecordDeletionAdminError(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return errRecordDeletionAdmin
	}
	return nil
}

// Only bounded catalog identifiers are exposed; PostgreSQL messages, details,
// connection parameters and credentials never enter CLI output.
func safeRecordDeletionAdminError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr == nil || len(pgErr.Code) != 5 {
		return errRecordDeletionAdmin
	}
	for _, ch := range pgErr.Code {
		if (ch < '0' || ch > '9') && (ch < 'A' || ch > 'Z') {
			return errRecordDeletionAdmin
		}
	}
	constraint := pgErr.ConstraintName
	if len(constraint) > 63 {
		constraint = ""
	}
	for i, ch := range constraint {
		if (ch < 'a' || ch > 'z') && ch != '_' && (i == 0 || ch < '0' || ch > '9') {
			constraint = ""
			break
		}
	}
	if constraint != "" {
		return fmt.Errorf("%w (SQLSTATE %s, constraint %s)", errRecordDeletionAdmin, pgErr.Code, constraint)
	}
	return fmt.Errorf("%w (SQLSTATE %s)", errRecordDeletionAdmin, pgErr.Code)
}
