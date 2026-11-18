package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/sourceretirement"
)

const (
	sourceWitnessBootstrapDatabaseURLEnv = "HOUFENG_SOURCE_WITNESS_BOOTSTRAP_DATABASE_URL"
	sourceWitnessOwnerRoleEnv            = "HOUFENG_SOURCE_WITNESS_OWNER_ROLE"
	sourceWitnessReaderRoleEnv           = "HOUFENG_SOURCE_WITNESS_READER_ROLE"
	sourceWitnessProducerRoleEnv         = "HOUFENG_SOURCE_WITNESS_PRODUCER_ROLE"
	sourceWitnessProvisionerRoleEnv      = "HOUFENG_SOURCE_WITNESS_PROVISIONER_ROLE"
)

var (
	errInvalidSourceAuthorityInvocation    = errors.New("invalid source-authority invocation")
	errInvalidSourceAuthorityConfiguration = errors.New("source authority provisioning configuration is invalid")
	errOpenSourceAuthorityPool             = errors.New("open source authority bootstrap PostgreSQL connection failed")
	errProvisionSourceAuthority            = errors.New("source authority initial enrollment or exact repeat failed; resolve original enrollment before retry")
	errWriteSourceAuthorityIdentity        = errors.New("write source authority domain identity failed; resolve original enrollment before retry")
)

// runSourceAuthority performs initial W2 enrollment or an exact repeat, not
// post-recovery bootstrap. The caller owns dispatch and signal cancellation.
func runSourceAuthority(ctx context.Context, args []string) error {
	if len(args) > 1 && (args[1] == "retirement-preview" || args[1] == "retire" || args[1] == "deliver") {
		return runSourceRetirement(ctx, args)
	}
	if len(args) > 1 && (args[1] == "maintenance-provision" || args[1] == "enroll-offline" || args[1] == "enroll-compose") {
		return runSourceEnrollmentCommand(ctx, args)
	}
	config, roles, err := parseSourceAuthorityConfig(args, os.LookupEnv)
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return errOpenSourceAuthorityPool
	}
	defer pool.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = pool.Ping(pingCtx)
	cancel()
	if err != nil {
		return errOpenSourceAuthorityPool
	}

	identity, err := platformmigrate.ProvisionSourceAuthority(ctx, pool, roles)
	if err != nil {
		// Driver and database errors may contain credentials or arbitrary server
		// text. Never wrap them into the error exposed to the main logger.
		return errProvisionSourceAuthority
	}
	if identity.IdentityEpoch <= 0 {
		return errProvisionSourceAuthority
	}
	readerIdentity := sourceretirement.DomainIdentity{
		ID:               identity.ID,
		SystemIdentifier: identity.PostgresSystemIdentifier,
		DatabaseOID:      identity.DatabaseOID,
		DatabaseName:     identity.DatabaseName,
		Epoch:            uint64(identity.IdentityEpoch),
	}
	if err := json.NewEncoder(os.Stdout).Encode(readerIdentity); err != nil {
		return errWriteSourceAuthorityIdentity
	}
	return nil
}

func parseSourceAuthorityConfig(args []string, lookupEnv func(string) (string, bool)) (*pgxpool.Config, platformmigrate.SourceAuthorityRoles, error) {
	var roles platformmigrate.SourceAuthorityRoles
	// An exact invocation check precedes even environment lookup, including
	// help/unknown flags and positional values that might themselves be secrets.
	if len(args) != 4 || args[0] != "source-authority" || args[1] != "provision" || args[2] != "--scope" || args[3] != "witness" {
		return nil, roles, errInvalidSourceAuthorityInvocation
	}
	if lookupEnv == nil {
		return nil, roles, errInvalidSourceAuthorityConfiguration
	}
	roles.Owner, _ = lookupEnv(sourceWitnessOwnerRoleEnv)
	roles.Reader, _ = lookupEnv(sourceWitnessReaderRoleEnv)
	roles.Producer, _ = lookupEnv(sourceWitnessProducerRoleEnv)
	roles.Provisioner, _ = lookupEnv(sourceWitnessProvisionerRoleEnv)
	if err := roles.Validate(); err != nil {
		return nil, platformmigrate.SourceAuthorityRoles{}, errInvalidSourceAuthorityConfiguration
	}
	databaseURL, ok := lookupEnv(sourceWitnessBootstrapDatabaseURLEnv)
	if !ok || strings.TrimSpace(databaseURL) == "" {
		return nil, roles, errInvalidSourceAuthorityConfiguration
	}
	config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(strings.TrimSpace(databaseURL))
	if err != nil {
		return nil, roles, errInvalidSourceAuthorityConfiguration
	}
	return config, roles, nil
}
