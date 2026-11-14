package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/deploy"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceretirement"
	"houfeng/internal/center/store"
)

const (
	sourceAppAdminDatabaseURLEnv        = "HOUFENG_SOURCE_APP_ADMIN_DATABASE_URL"
	sourceWitnessProducerDatabaseURLEnv = "HOUFENG_SOURCE_WITNESS_PRODUCER_DATABASE_URL"
	sourceWitnessIdentityFileEnv        = "HOUFENG_SOURCE_WITNESS_IDENTITY_FILE"
	sourceRetirementFileLimit           = 64 * 1024
)

var (
	errSourceRetirementInvocation    = errors.New("invalid source retirement invocation")
	errSourceRetirementConfiguration = errors.New("source retirement configuration is invalid")
	errSourceRetirementInput         = errors.New("source retirement input is invalid")
	errSourceRetirementConnection    = errors.New("source retirement database identity or connection is unavailable")
	errSourceRetirementOperation     = errors.New("source retirement failed; resolve uncertain results using the original retirement ID")
	errSourceRetirementOutput        = errors.New("source retirement output failed; resolve uncertain results using the original retirement ID")
)

type sourceRetirementPreviewRequest struct {
	ActorUserID string                     `json:"actor_user_id"`
	Source      sourceretirement.SourceKey `json:"source"`
}

type sourceRetirementCommandRequest struct {
	Source           sourceretirement.SourceKey `json:"source"`
	RetirementID     string                     `json:"retirement_id"`
	ActorUserID      string                     `json:"actor_user_id"`
	ExpectedRevision exactSourceRevision        `json:"expected_revision"`
	FinalFloor       recordauth.VisibilityScope `json:"final_floor"`
}

type exactSourceRevision [32]byte

func (revision *exactSourceRevision) UnmarshalJSON(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '[' {
		return errSourceRetirementInput
	}
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil || len(values) != len(revision) {
		return errSourceRetirementInput
	}
	for i, value := range values {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &revision[i]) != nil {
			return errSourceRetirementInput
		}
	}
	return nil
}

func runSourceRetirement(ctx context.Context, args []string) error {
	if len(args) != 4 || args[0] != "source-authority" || args[3] == "" {
		return errSourceRetirementInvocation
	}
	command, argument := args[1], args[3]
	if !((command == "retirement-preview" || command == "retire") && args[2] == "--request-file" || command == "deliver" && args[2] == "--retirement-id") {
		return errSourceRetirementInvocation
	}

	var preview sourceRetirementPreviewRequest
	var request sourceRetirementCommandRequest
	if command != "deliver" {
		body, err := readSourceRetirementFile(argument)
		if err != nil {
			return errSourceRetirementInput
		}
		if command == "retirement-preview" {
			if err := decodeSourceRetirementJSON(body, &preview, "actor_user_id", "source"); err != nil {
				return errSourceRetirementInput
			}
		} else if err := decodeSourceRetirementJSON(body, &request, "source", "retirement_id", "actor_user_id", "expected_revision", "final_floor"); err != nil {
			return errSourceRetirementInput
		}
	}

	appURL, appOK := os.LookupEnv(sourceAppAdminDatabaseURLEnv)
	witnessURL, witnessOK := os.LookupEnv(sourceWitnessProducerDatabaseURLEnv)
	identityPath, identityOK := os.LookupEnv(sourceWitnessIdentityFileEnv)
	instanceID, instanceOK := os.LookupEnv("HOUFENG_RECORD_INSTANCE_ID")
	deploymentID, deploymentOK := os.LookupEnv("HOUFENG_RECORD_DEPLOYMENT_ID")
	kind, kindOK := os.LookupEnv("HOUFENG_RECORD_INSTANCE_KIND")
	capability, capabilityOK := os.LookupEnv("HOUFENG_RECORD_INSTANCE_CAPABILITY")
	if !appOK || !witnessOK || !identityOK || !instanceOK || !deploymentOK || !kindOK || !capabilityOK ||
		strings.TrimSpace(appURL) == "" || strings.TrimSpace(witnessURL) == "" || identityPath == "" ||
		instanceID == "" || deploymentID == "" || kind == "" || capability == "" {
		return errSourceRetirementConfiguration
	}
	if command == "retirement-preview" && preview.Source.DeploymentID != deploymentID ||
		command == "retire" && request.Source.DeploymentID != deploymentID {
		return errSourceRetirementInput
	}
	appConfig, err := platformmigrate.ParseSourceAuthorityPostgresConfig(strings.TrimSpace(appURL))
	if err != nil {
		return errSourceRetirementConfiguration
	}
	witnessConfig, err := platformmigrate.ParseSourceAuthorityPostgresConfig(strings.TrimSpace(witnessURL))
	if err != nil {
		return errSourceRetirementConfiguration
	}
	identityBytes, err := readSourceRetirementFile(identityPath)
	if err != nil {
		return errSourceRetirementConfiguration
	}
	var identity sourceretirement.DomainIdentity
	if err := decodeSourceRetirementJSON(identityBytes, &identity, "ID", "SystemIdentifier", "DatabaseOID", "DatabaseName", "Epoch"); err != nil {
		return errSourceRetirementConfiguration
	}
	gate, err := store.NewDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID: instanceID, DeploymentID: recordplatform.DeploymentID(deploymentID),
		ProjectID: recordplatform.ProjectIDDefault, InstanceKind: kind, Capability: capability,
	})
	if err != nil {
		return errSourceRetirementConfiguration
	}
	if identity.SystemIdentifier == "" {
		return errSourceRetirementConfiguration
	}
	// W2 is independently checked on every connection and by Authority on each
	// operation. Read the protected, activated APP binding before opening APP.
	witnessConfig.AfterConnect = sourceRetirementWitnessConnectionCheck(witnessConfig.ConnConfig.User, identity)
	witness, err := pgxpool.NewWithConfig(ctx, witnessConfig)
	if err != nil {
		return errSourceRetirementConnection
	}
	defer witness.Close()
	if err := witness.Ping(ctx); err != nil {
		return errSourceRetirementConnection
	}
	authority, err := sourceretirement.NewAuthority(witness, identity)
	if err != nil {
		return errSourceRetirementConfiguration
	}
	enrollment, err := authority.ReadApplicationEnrollment(ctx, deploymentID)
	if err != nil || !sourceRetirementEnrollmentActivated(enrollment, deploymentID) {
		return errSourceRetirementConnection
	}
	// The trusted enrollment is the expected identity, not the APP's own
	// self-description. Physical APP continuity and endpoint isolation belong
	// to the trusted bootstrap/recovery coordinator; this is not clone detection.
	appConfig.AfterConnect = sourceRetirementApplicationConnectionCheck(
		appConfig.ConnConfig.User, enrollment, identity)
	app, err := pgxpool.NewWithConfig(ctx, appConfig)
	if err != nil {
		return errSourceRetirementConnection
	}
	defer app.Close()
	if err := app.Ping(ctx); err != nil {
		return errSourceRetirementConnection
	}
	configPath := sourceMaintenanceConfigPath()
	var output any
	err = deploy.WithSourceMaintenance(ctx, app, configPath, false, func(config *deploy.SourceMaintenanceConfig) error {
		if config == nil || config.Witness != identity {
			return errSourceRetirementOperation
		}
		service, err := store.NewSourceRetirementService(app, gate, authority)
		if err != nil {
			return errSourceRetirementOperation
		}
		switch command {
		case "retirement-preview":
			output, err = service.Preview(ctx, preview.ActorUserID, preview.Source)
		case "retire":
			prepared, prepareErr := service.Prepare(ctx, store.SourceRetirementRequest{
				Source: request.Source, RetirementID: request.RetirementID, ActorUserID: request.ActorUserID,
				ExpectedRevision: [32]byte(request.ExpectedRevision), FinalFloor: request.FinalFloor,
			})
			if prepareErr != nil {
				return errSourceRetirementOperation
			}
			var digest [32]byte
			digest, err = service.CommitBarrier(ctx, prepared)
			output = struct {
				RetirementID  string   `json:"retirement_id"`
				BarrierDigest [32]byte `json:"barrier_digest"`
			}{request.RetirementID, digest}
		case "deliver":
			err = service.Deliver(ctx, argument)
			output = struct {
				RetirementID string `json:"retirement_id"`
				Delivered    bool   `json:"delivered"`
			}{argument, true}
		}
		return err
	})
	if err != nil {
		return errSourceRetirementOperation
	}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		return errSourceRetirementOutput
	}
	return nil
}

func sourceRetirementEnrollmentActivated(enrollment sourceretirement.ApplicationEnrollment, deploymentID string) bool {
	return deploymentID != "" && enrollment.DeploymentID == deploymentID &&
		enrollment.Generation > 0 && enrollment.ActivationDigest != nil &&
		*enrollment.ActivationDigest != ([32]byte{})
}

func sourceRetirementWitnessConnectionCheck(user string, expected sourceretirement.DomainIdentity) func(context.Context, *pgx.Conn) error {
	return func(ctx context.Context, conn *pgx.Conn) error {
		var sessionUser, currentUser string
		var actual sourceretirement.DomainIdentity
		var epoch int64
		// The restricted W2 snapshot checks pg_control_system under its owner,
		// not under the producer login, and rejects a nonphysical W2 identity.
		err := conn.QueryRow(ctx, `select session_user::text, current_user::text,
			domain_id, system_identifier, database_oid, database_name, identity_epoch
			from public.source_authority_snapshot()`).Scan(
			&sessionUser, &currentUser, &actual.ID, &actual.SystemIdentifier,
			&actual.DatabaseOID, &actual.DatabaseName, &epoch)
		if err != nil || sessionUser != user || currentUser != user || epoch <= 0 {
			return errSourceRetirementConnection
		}
		actual.Epoch = uint64(epoch)
		if actual != expected {
			return errSourceRetirementConnection
		}
		return nil
	}
}

type sourceRetirementApplicationConnection struct {
	sessionUser, currentUser     string
	domainKind, identityMode     string
	domain                       sourceretirement.DomainIdentity
	epoch                        int64
	databaseOID                  uint32
	databaseName                 string
	singleIdentity, currentAdmin bool
}

func (observed sourceRetirementApplicationConnection) matches(user string, expected sourceretirement.DomainIdentity) bool {
	return user != "" && observed.sessionUser == user && observed.currentUser == user &&
		observed.singleIdentity && observed.currentAdmin && observed.domainKind == "application" &&
		observed.identityMode == "postgres_system" && observed.epoch > 0 &&
		uint64(observed.epoch) == expected.Epoch &&
		observed.domain.ID == expected.ID && observed.domain.SystemIdentifier == expected.SystemIdentifier &&
		observed.domain.DatabaseOID == expected.DatabaseOID && observed.domain.DatabaseName == expected.DatabaseName &&
		observed.databaseOID == expected.DatabaseOID && observed.databaseName == expected.DatabaseName
}

func sourceRetirementApplicationConnectionCheck(user string, enrollment sourceretirement.ApplicationEnrollment, witness sourceretirement.DomainIdentity) func(context.Context, *pgx.Conn) error {
	return func(ctx context.Context, conn *pgx.Conn) error {
		var observed sourceRetirementApplicationConnection
		err := conn.QueryRow(ctx, `select session_user::text, current_user::text,
			d.domain_id, d.domain_kind, d.identity_mode, d.postgres_system_identifier,
			d.database_oid, d.database_name, d.identity_epoch,
			(select oid from pg_catalog.pg_database where datname = current_database()),
			current_database()::text,
			(select count(*) = 1 from public.record_platform_domain_identity),
			exists (select 1 from public.source_authority_admission a
				join public.source_application_principals p
				  on p.generation = a.generation and p.principal = session_user and p.purpose = 'admin'
				where a.singleton and a.mode = 'active' and a.generation = $1
				  and a.deployment_id = $2 and a.witness_domain_id = $3
				  and a.witness_system_identifier = $4 and a.witness_database_oid = $5
				  and a.witness_database_name = $6 and a.witness_identity_epoch = $7)
			from public.record_platform_domain_identity d`,
			enrollment.Generation, enrollment.DeploymentID, witness.ID, witness.SystemIdentifier,
			witness.DatabaseOID, witness.DatabaseName, witness.Epoch).Scan(
			&observed.sessionUser, &observed.currentUser, &observed.domain.ID,
			&observed.domainKind, &observed.identityMode, &observed.domain.SystemIdentifier,
			&observed.domain.DatabaseOID, &observed.domain.DatabaseName, &observed.epoch,
			&observed.databaseOID, &observed.databaseName, &observed.singleIdentity, &observed.currentAdmin)
		if err != nil || !observed.matches(user, enrollment.Application) {
			return errSourceRetirementConnection
		}
		return nil
	}
}

// Read only a bounded, stable regular file and never follow a final symlink.
func readSourceRetirementFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o022 != 0 || before.Size() > sourceRetirementFileLimit {
		return nil, errSourceRetirementInput
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errSourceRetirementInput
	}
	file := os.NewFile(uintptr(fd), "source-retirement-input")
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || after.Mode().Perm()&0o022 != 0 || !os.SameFile(before, after) || after.Size() > sourceRetirementFileLimit {
		return nil, errSourceRetirementInput
	}
	body, err := io.ReadAll(io.LimitReader(file, sourceRetirementFileLimit+1))
	if err != nil || len(body) > sourceRetirementFileLimit {
		return nil, errSourceRetirementInput
	}
	return body, nil
}

// Duplicate members (including case-folded aliases) are rejected at every
// level before Go's case-insensitive struct matching can hide a conflicting value.
func decodeSourceRetirementJSON(body []byte, destination any, required ...string) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := scanSourceRetirementJSONValue(decoder, 0); err != nil {
		return errSourceRetirementInput
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errSourceRetirementInput
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil || len(fields) != len(required) {
		return errSourceRetirementInput
	}
	for _, name := range required {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errSourceRetirementInput
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errSourceRetirementInput
	}
	return nil
}

func scanSourceRetirementJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return errSourceRetirementInput
	}
	token, err := decoder.Token()
	if err != nil {
		return errSourceRetirementInput
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok {
				return errSourceRetirementInput
			}
			folded := strings.ToLower(name)
			if _, exists := seen[folded]; exists {
				return errSourceRetirementInput
			}
			seen[folded] = struct{}{}
			if err := scanSourceRetirementJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errSourceRetirementInput
		}
	case '[':
		for decoder.More() {
			if err := scanSourceRetirementJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errSourceRetirementInput
		}
	default:
		return errSourceRetirementInput
	}
	return nil
}
