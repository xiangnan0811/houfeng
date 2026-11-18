//go:build linux

package deploy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/platformmigrate"
)

type ordinaryPhysicalDatabase struct {
	SystemIdentifier  string    `json:"system_identifier"`
	DatabaseOID       uint32    `json:"database_oid"`
	PostmasterStarted time.Time `json:"postmaster_started"`
}

type ordinaryBackendRecord struct {
	Version         int                      `json:"version"`
	OperationID     string                   `json:"operation_id"`
	ApplicationName string                   `json:"application_name"`
	PID             int32                    `json:"pid"`
	BackendStarted  time.Time                `json:"backend_started"`
	Role            string                   `json:"role"`
	ServerAddress   string                   `json:"server_address"`
	Physical        ordinaryPhysicalDatabase `json:"physical"`
}

type ordinaryTrackedPool struct {
	pool *pgxpool.Pool
	dirs []composeLaunchDirectory
}

func (p *ordinaryTrackedPool) Close() {
	p.pool.Close()
	for _, dir := range p.dirs {
		_ = unix.Close(dir.fd)
	}
}

// Every newly connected effect-capable backend is durably registered in the
// operation directory before pgxpool can hand it to SQL callers. The physical
// tuple must first be observed and pinned by a separate bootstrap observer.
func ordinaryOpenTrackedPool(ctx context.Context, endpoint composePostgresEndpoint, physical ordinaryPhysicalDatabase, operationID, journalDirectory string) (*ordinaryTrackedPool, error) {
	if ctx == nil || !fenceRID.MatchString(operationID) || endpoint.Database != composeDatabaseName || endpoint.Port != composeDatabasePort || net.ParseIP(endpoint.Host) == nil || physical.SystemIdentifier == "" || physical.DatabaseOID == 0 || physical.PostmasterStarted.IsZero() || !validComposePassword(endpoint.Password) {
		return nil, errOrdinaryUpgrade
	}
	switch endpoint.Role {
	case composeBootstrapRole, composeMigratorRole, composeAdminRole, composeRuntimeRole, composeDeletionWorkerRole, composeAuthorityRole:
	default:
		return nil, errOrdinaryUpgrade
	}
	if endpoint.LaunchDigest != ([32]byte{}) && endpoint.Role != composeRuntimeRole && endpoint.Role != composeDeletionWorkerRole {
		return nil, errOrdinaryUpgrade
	}
	dirs, err := ordinaryOpenPrivateDirectory(journalDirectory, composeLaunchRuntimeGID)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	closeDirs := func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}
	directory := dirs[len(dirs)-1]
	if directory.before.Uid != composeLaunchPublisherUID || directory.before.Mode&0o7777 != 0o700 {
		closeDirs()
		return nil, errOrdinaryUpgrade
	}
	connectionURL := &url.URL{Scheme: "postgres", User: url.UserPassword(endpoint.Role, endpoint.Password), Host: net.JoinHostPort(endpoint.Host, strconv.Itoa(int(endpoint.Port))), Path: "/" + endpoint.Database, RawQuery: "sslmode=disable"}
	config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(connectionURL.String())
	if err != nil {
		closeDirs()
		return nil, errOrdinaryUpgrade
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		closeDirs()
		return nil, errOrdinaryUpgrade
	}
	hash := sha256.Sum256([]byte(operationID))
	application := "hf-cold-" + hex.EncodeToString(hash[:8]) + "-" + hex.EncodeToString(nonce[:])
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["application_name"] = application
	if endpoint.LaunchDigest != ([32]byte{}) {
		config.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"] = hex.EncodeToString(endpoint.LaunchDigest[:])
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		var record ordinaryBackendRecord
		var databaseOID uint32
		var serverAddress string
		var serverPort int
		var postmaster time.Time
		var actualApplication string
		err := conn.QueryRow(ctx, `select a.pid,a.backend_start,a.datid,current_user::text,pg_postmaster_start_time(),host(inet_server_addr()),inet_server_port(),current_setting('application_name') from pg_catalog.pg_stat_activity a where a.pid=pg_backend_pid()`).Scan(&record.PID, &record.BackendStarted, &databaseOID, &record.Role, &postmaster, &serverAddress, &serverPort, &actualApplication)
		if err != nil || actualApplication != application || record.PID <= 0 || record.BackendStarted.IsZero() || record.Role != endpoint.Role || databaseOID != physical.DatabaseOID || !postmaster.Equal(physical.PostmasterStarted) || net.ParseIP(serverAddress) == nil || !net.ParseIP(serverAddress).Equal(net.ParseIP(endpoint.Host)) || serverPort != int(endpoint.Port) || !ordinaryPrivateDirectoryUnchanged(dirs) {
			return errOrdinaryUpgrade
		}
		if endpoint.Role == composeBootstrapRole {
			var systemID string
			if err := conn.QueryRow(ctx, `select system_identifier::text from pg_catalog.pg_control_system()`).Scan(&systemID); err != nil || systemID != physical.SystemIdentifier {
				return errOrdinaryUpgrade
			}
		}
		if endpoint.LaunchDigest != ([32]byte{}) {
			var launch string
			if err := conn.QueryRow(ctx, `select current_setting('houfeng.birth_launch_digest',true)`).Scan(&launch); err != nil || launch != hex.EncodeToString(endpoint.LaunchDigest[:]) {
				return errOrdinaryUpgrade
			}
		}
		record.Version, record.OperationID, record.ApplicationName, record.Physical = 1, operationID, application, physical
		record.ServerAddress = serverAddress
		var registrationNonce [16]byte
		if _, err := rand.Read(registrationNonce[:]); err != nil {
			return errOrdinaryUpgrade
		}
		name := fmt.Sprintf("backend-%d-%s.json", record.PID, hex.EncodeToString(registrationNonce[:]))
		if fencePublish(directory.fd, name, record) != nil || !ordinaryPrivateDirectoryUnchanged(dirs) {
			return errOrdinaryUpgrade
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		closeDirs()
		return nil, errOrdinaryUpgrade
	}
	return &ordinaryTrackedPool{pool: pool, dirs: dirs}, nil
}
