//go:build linux

package main

import (
	"net"
	"net/url"

	"houfeng/internal/center/deploy"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/sourceretirement"
)

const (
	sourceMaintenancePath = "/var/lib/g2/control/maintenance/source-authority.json"
	sourceGuardDirectory  = "/var/lib/g2/control/maintenance/guard"
)

// The real source maintenance API verifies the direct reader against the
// provisioned witness, then creates its private guard and immutable config.
// This capability is not APP enrollment or a no-copy observation.
func provisionSourceMaintenance(actual platformmigrate.DomainIdentity, input privateInput) error {
	if actual.Kind != platformmigrate.DomainKindDeletionWitness || actual.Mode != "postgres_system" ||
		actual.IdentityEpoch != 1 || actual.DatabaseName != "g2_w2" ||
		input.w2.endpoint.database != "g2_w2" || input.w2.endpoint.port != 5432 {
		return errDomain
	}
	witness := sourceretirement.DomainIdentity{
		ID: actual.ID, SystemIdentifier: actual.PostgresSystemIdentifier,
		DatabaseOID: actual.DatabaseOID, DatabaseName: actual.DatabaseName,
		Epoch: uint64(actual.IdentityEpoch),
	}
	// The production source-authority URL parser deliberately permits only TLS
	// options; pgx's passfile, service and application-name options are refused.
	reader := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(w2SourceReaderRole, input.credentials.w2SourceReader),
		Host:     net.JoinHostPort(input.w2.endpoint.host, "5432"),
		Path:     "/g2_w2",
		RawQuery: "sslmode=disable",
	}
	readerURL := reader.String()
	provisioned, err := deploy.ProvisionSourceMaintenanceConfig(sourceMaintenancePath, witness, readerURL)
	if err != nil {
		return errDomain
	}
	loaded, err := deploy.LoadSourceMaintenanceConfig(sourceMaintenancePath)
	if err != nil || loaded != provisioned || loaded.Witness != witness ||
		loaded.ReaderDatabaseURL != readerURL || loaded.GuardDirectory != sourceGuardDirectory ||
		loaded.BirthInstalled || loaded.BirthLineageID != "" || loaded.BirthProtocolVersion != 0 ||
		loaded.BirthReaderDatabaseURL != "" {
		return errDomain
	}
	return nil
}
