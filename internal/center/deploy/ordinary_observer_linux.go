//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/store/migrate"
)

// The root controller uses this strictly read-only observer before fencing and
// after reconnecting to the isolated database. The validated parser rejects
// ambient PG/SSL overrides rather than changing process-wide environment.
func ordinaryOpenControlObserver(ctx context.Context, address, password, operationID string) (*pgx.Conn, ordinaryPhysicalDatabase, error) {
	var physical ordinaryPhysicalDatabase
	if ctx == nil || net.ParseIP(address) == nil || !validComposePassword(password) || !fenceRID.MatchString(operationID) {
		return nil, physical, errOrdinaryUpgrade
	}
	connectionURL := &url.URL{Scheme: "postgres", User: url.UserPassword(composeBootstrapRole, password), Host: net.JoinHostPort(address, strconv.Itoa(int(composeDatabasePort))), Path: "/" + composeDatabaseName, RawQuery: "sslmode=disable"}
	poolConfig, err := platformmigrate.ParseSourceAuthorityPostgresConfig(connectionURL.String())
	if err != nil {
		return nil, physical, errOrdinaryUpgrade
	}
	config := poolConfig.ConnConfig
	if config.RuntimeParams == nil {
		config.RuntimeParams = make(map[string]string)
	}
	digest := sha256.Sum256([]byte(operationID))
	application := "hf-cold-control-" + hex.EncodeToString(digest[:8])
	config.RuntimeParams["application_name"] = application
	config.RuntimeParams["default_transaction_read_only"] = "on"
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, physical, errOrdinaryUpgrade
	}
	var role, sessionRole, observedAddress, readOnly, actualApplication string
	var recovering bool
	err = conn.QueryRow(ctx, `select (select system_identifier::text from pg_catalog.pg_control_system()), (select oid from pg_catalog.pg_database where datname=current_database()), pg_postmaster_start_time(), current_user::text, session_user::text, host(inet_server_addr()), current_setting('transaction_read_only'), current_setting('application_name'), pg_is_in_recovery()`).Scan(&physical.SystemIdentifier, &physical.DatabaseOID, &physical.PostmasterStarted, &role, &sessionRole, &observedAddress, &readOnly, &actualApplication, &recovering)
	systemID, parseErr := strconv.ParseUint(physical.SystemIdentifier, 10, 64)
	if err != nil || parseErr != nil || systemID == 0 || physical.DatabaseOID == 0 || physical.PostmasterStarted.IsZero() || role != composeBootstrapRole || sessionRole != composeBootstrapRole || net.ParseIP(observedAddress) == nil || !net.ParseIP(observedAddress).Equal(net.ParseIP(address)) || readOnly != "on" || actualApplication != application || recovering {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = conn.Close(closeCtx)
		cancel()
		return nil, ordinaryPhysicalDatabase{}, errOrdinaryUpgrade
	}
	return conn, physical, nil
}

func ordinaryCloseControlObserver(conn *pgx.Conn) {
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = conn.Close(ctx)
}

const (
	ordinaryPredecessorC80 = "c80-exact"
	ordinaryPredecessorC81 = "c81-current"
)

// A missing source table is acceptable only for the registered exact C80
// migration ledger and catalog. The checks share one read-only snapshot.
func ordinaryObservePredecessorClass(ctx context.Context, conn *pgx.Conn) (string, error) {
	if ctx == nil || conn == nil {
		return "", errOrdinaryUpgrade
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return "", errOrdinaryUpgrade
	}
	defer tx.Rollback(ctx)
	var sourceExists, birthExists bool
	if err := tx.QueryRow(ctx, `select to_regclass('public.source_authority_admission') is not null,to_regclass('record_platform_internal.record_birth_lineage') is not null`).Scan(&sourceExists, &birthExists); err != nil {
		return "", errOrdinaryUpgrade
	}
	class := ordinaryPredecessorC81
	if sourceExists {
		var present bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from public.source_authority_admission)`).Scan(&present); err != nil || present {
			return "", errOrdinaryUpgrade
		}
		if birthExists {
			if err := tx.QueryRow(ctx, `select exists(select 1 from record_platform_internal.record_birth_lineage)`).Scan(&present); err != nil || present {
				return "", errOrdinaryUpgrade
			}
		}
	} else {
		var partial bool
		if birthExists || tx.QueryRow(ctx, `select exists(select 1 from unnest(array[
			'public.source_application_principals', 'public.source_retirement_barriers',
			'public.source_retirement_outbox', 'public.source_retirement_pins',
			'public.source_retirement_coverage_unknown',
			'record_platform_internal.record_birth_keys', 'record_platform_internal.record_birth_bindings',
			'record_platform_internal.record_birth_enabled', 'record_platform_internal.record_birth_launch_state',
			'record_platform_internal.record_birth_allocations', 'record_platform_internal.record_birth_checkpoint_head',
			'record_platform_internal.record_birth_checkpoints'
		]) as n(name) where to_regclass(n.name) is not null)`).Scan(&partial) != nil || partial {
			return "", errOrdinaryUpgrade
		}
		class = ordinaryPredecessorC80
	}
	snapshot, err := migrate.ReadAppACLMaintenanceSnapshotInTransaction(ctx, tx, nil)
	if err != nil || snapshot.LegacyC80 != (class == ordinaryPredecessorC80) {
		return "", errOrdinaryUpgrade
	}
	if err := tx.Commit(ctx); err != nil {
		return "", errOrdinaryUpgrade
	}
	return class, nil
}

// After C81 convergence both tables must exist and contain no installation.
func ordinaryObserverRequireCurrentSourceAbsent(ctx context.Context, conn *pgx.Conn) error {
	if ctx == nil || conn == nil {
		return errOrdinaryUpgrade
	}
	var source, birth, sourcePresent, birthPresent bool
	if err := conn.QueryRow(ctx, `select to_regclass('public.source_authority_admission') is not null,to_regclass('record_platform_internal.record_birth_lineage') is not null`).Scan(&source, &birth); err != nil || !source || !birth {
		return errOrdinaryUpgrade
	}
	if err := conn.QueryRow(ctx, `select exists(select 1 from public.source_authority_admission),exists(select 1 from record_platform_internal.record_birth_lineage)`).Scan(&sourcePresent, &birthPresent); err != nil || sourcePresent || birthPresent {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryObserverRequireIntentSourceAbsent(ctx context.Context, conn *pgx.Conn, intent ordinaryUpgradeIntent) error {
	class, err := ordinaryObservePredecessorClass(ctx, conn)
	if err != nil || (intent.Predecessor.CatalogClass == ordinaryPredecessorC81 && class != ordinaryPredecessorC81) {
		return errOrdinaryUpgrade
	}
	return nil
}
