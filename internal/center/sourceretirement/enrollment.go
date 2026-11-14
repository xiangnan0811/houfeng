package sourceretirement

import (
	"context"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// ApplicationEnrollment is the immutable APP identity bound to one W2
// generation. A nil activation is an enrolled but not yet activated binding;
// it is useful for trusted initial provisioning, not normal source admission.
type ApplicationEnrollment struct {
	Generation       uint64
	DeploymentID     string
	Application      DomainIdentity
	ActivationDigest *[32]byte
}

// EnrollApplication records or exactly replays the initial APP binding. The
// provisioner must independently establish the actual APP physical identity.
func (authority *Authority) EnrollApplication(ctx context.Context, deploymentID string, application DomainIdentity) error {
	if err := authority.checkContext(ctx); err != nil {
		return err
	}
	if !validIdentity(deploymentID, "dp-") {
		return invalidTerminal("application deployment identity")
	}
	if err := validateApplicationIdentity(application); err != nil {
		return err
	}
	if application.SystemIdentifier == authority.expected.SystemIdentifier {
		return invalidTerminal("application and witness share a PostgreSQL system")
	}
	return authority.recoveryMutation(ctx, "enroll application", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `select public.source_application_enroll($1, $2, $3, $4::oid, $5, $6)`,
			deploymentID, application.ID, application.SystemIdentifier, application.DatabaseOID,
			application.DatabaseName, int64(application.Epoch))
		return err
	})
}

// ActivateApplication asserts the trusted provisioner's confirmed APP commit.
// An uncertain commit must be resolved by reading or exactly replaying the same
// digest, never by inventing another activation.
func (authority *Authority) ActivateApplication(ctx context.Context, activation [32]byte) error {
	if activation == ([32]byte{}) {
		return invalidTerminal("application activation digest")
	}
	return authority.recoveryMutation(ctx, "activate application", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `select public.source_application_activate($1)`, activation[:])
		return err
	})
}

// ReadApplicationEnrollment reads the exact current generation and deployment
// in one authenticated snapshot. The caller must require activation before
// granting normal source authority.
func (authority *Authority) ReadApplicationEnrollment(ctx context.Context, deploymentID string) (ApplicationEnrollment, error) {
	if err := authority.checkContext(ctx); err != nil {
		return ApplicationEnrollment{}, err
	}
	if !validIdentity(deploymentID, "dp-") {
		return ApplicationEnrollment{}, invalidTerminal("application deployment identity")
	}
	tx, err := authority.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ApplicationEnrollment{}, authorityFailure("begin read application enrollment", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := authority.snapshot(ctx, tx)
	if err != nil {
		return ApplicationEnrollment{}, authorityFailure("read application enrollment identity", err)
	}
	if state.Mode != "active" || !state.HandoffCompleted {
		return ApplicationEnrollment{}, invalidTerminal("application enrollment authority not active")
	}
	enrollment, err := authority.readApplicationEnrollment(ctx, tx, state, deploymentID)
	if err != nil {
		return ApplicationEnrollment{}, authorityFailure("read application enrollment", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplicationEnrollment{}, authorityFailure("commit read application enrollment", err)
	}
	return enrollment, nil
}

// readApplicationEnrollment uses the caller's authenticated W2 snapshot. It
// never starts another transaction, so Observe can bind source evidence and
// enrollment to the same repeatable-read view.
func (authority *Authority) readApplicationEnrollment(ctx context.Context, tx pgx.Tx, state Observation, deploymentID string) (ApplicationEnrollment, error) {
	if !validIdentity(deploymentID, "dp-") || state.Generation == 0 || state.Generation > math.MaxInt64 ||
		state.Domain != authority.expected {
		return ApplicationEnrollment{}, invalidTerminal("application enrollment snapshot identity")
	}
	var enrollment ApplicationEnrollment
	var generation, epoch int64
	var activation []byte
	err := tx.QueryRow(ctx, `select e.generation, e.deployment_id, e.application_domain_id,
		e.application_system_identifier, e.application_database_oid, e.application_database_name,
		e.application_identity_epoch, a.activation_digest
		from public.source_application_enrollments e
		left join public.source_application_activations a on a.generation = e.generation
		where e.generation = $1 and e.deployment_id = $2`, int64(state.Generation), deploymentID).Scan(
		&generation, &enrollment.DeploymentID, &enrollment.Application.ID,
		&enrollment.Application.SystemIdentifier, &enrollment.Application.DatabaseOID,
		&enrollment.Application.DatabaseName, &epoch, &activation,
	)
	if err != nil {
		return ApplicationEnrollment{}, authorityScanError(err)
	}
	if generation <= 0 || uint64(generation) != state.Generation || enrollment.DeploymentID != deploymentID || epoch <= 0 {
		return ApplicationEnrollment{}, invalidTerminal("application enrollment projection")
	}
	enrollment.Generation = uint64(generation)
	enrollment.Application.Epoch = uint64(epoch)
	if err := validateApplicationIdentity(enrollment.Application); err != nil {
		return ApplicationEnrollment{}, err
	}
	if enrollment.Application.SystemIdentifier == state.Domain.SystemIdentifier {
		return ApplicationEnrollment{}, invalidTerminal("application and witness share a PostgreSQL system")
	}
	if activation != nil {
		if len(activation) != 32 {
			return ApplicationEnrollment{}, invalidTerminal("application activation digest projection")
		}
		var digest [32]byte
		copy(digest[:], activation)
		if digest == ([32]byte{}) {
			return ApplicationEnrollment{}, invalidTerminal("application activation digest projection")
		}
		enrollment.ActivationDigest = &digest
	}
	return enrollment, nil
}

func validateApplicationIdentity(application DomainIdentity) error {
	if !validIdentity(application.ID, "rd-") || application.DatabaseOID == 0 ||
		application.Epoch == 0 || application.Epoch > math.MaxInt64 {
		return invalidTerminal("application domain identity")
	}
	identifier := application.SystemIdentifier
	if len(identifier) == 0 || len(identifier) > 20 || identifier[0] < '1' || identifier[0] > '9' {
		return invalidTerminal("application system identifier")
	}
	for index := range identifier {
		if identifier[index] < '0' || identifier[index] > '9' {
			return invalidTerminal("application system identifier")
		}
	}
	if _, err := strconv.ParseUint(identifier, 10, 64); err != nil {
		return invalidTerminal("application system identifier")
	}
	name := application.DatabaseName
	if len(name) == 0 || len(name) > 63 || !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 {
		return invalidTerminal("application database name")
	}
	return nil
}
