//go:build linux

package deploy

import (
	"context"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
)

type ordinaryPendingBackend struct {
	PID           int32     `json:"pid"`
	BackendStart  time.Time `json:"backend_start"`
	UserName      string    `json:"usename"`
	Application   string    `json:"application_name"`
	ClientAddress string    `json:"client_addr"`
	State         string    `json:"state"`
	WaitEventType string    `json:"wait_event_type"`
}

const ordinaryDrainTimeout = 2 * time.Minute

func ordinaryWaitForBackendSettlement(ctx context.Context, observer *pgx.Conn, physical ordinaryPhysicalDatabase, records []ordinaryBackendRecord, drainAllClients bool) error {
	_, err := ordinaryWaitForBackendSettlementSnapshot(ctx, observer, physical, records, drainAllClients)
	return err
}

// ordinaryWaitForBackendSettlement never signals a numeric PID. A reused PID
// is distinct when its backend_start differs. All client sessions can also be
// required absent for the pre-fence drain, except this exact control observer.
func ordinaryWaitForBackendSettlementSnapshot(ctx context.Context, observer *pgx.Conn, physical ordinaryPhysicalDatabase, records []ordinaryBackendRecord, drainAllClients bool) ([]ordinaryPendingBackend, error) {
	if ctx == nil || observer == nil || physical.SystemIdentifier == "" || physical.DatabaseOID == 0 || physical.PostmasterStarted.IsZero() {
		return nil, errOrdinaryUpgrade
	}
	for _, record := range records {
		if record.Version != 1 || !fenceRID.MatchString(record.OperationID) || record.PID <= 0 || record.BackendStarted.IsZero() || record.Role == "" || record.ApplicationName == "" || net.ParseIP(record.ServerAddress) == nil || record.Physical.SystemIdentifier != physical.SystemIdentifier || record.Physical.DatabaseOID != physical.DatabaseOID || !record.Physical.PostmasterStarted.Equal(physical.PostmasterStarted) {
			return nil, errOrdinaryUpgrade
		}
	}
	var last []ordinaryPendingBackend
	for {
		var systemID, address string
		var databaseOID uint32
		var postmaster time.Time
		err := observer.QueryRow(ctx, `select (select system_identifier::text from pg_catalog.pg_control_system()), (select oid from pg_catalog.pg_database where datname=current_database()), pg_postmaster_start_time(), host(inet_server_addr())`).Scan(&systemID, &databaseOID, &postmaster, &address)
		if ctx.Err() != nil {
			return last, ctx.Err()
		}
		if err != nil || systemID != physical.SystemIdentifier || databaseOID != physical.DatabaseOID || !postmaster.Equal(physical.PostmasterStarted) || net.ParseIP(address) == nil {
			return last, errOrdinaryUpgrade
		}
		rows, err := observer.Query(ctx, `select pid,backend_start,usename::text,application_name,coalesce(host(client_addr),''),coalesce(state,''),coalesce(wait_event_type,'') from pg_catalog.pg_stat_activity where datid=$1 and backend_type='client backend' and pid<>pg_backend_pid() order by pid`, physical.DatabaseOID)
		if err != nil {
			if ctx.Err() != nil {
				return last, ctx.Err()
			}
			return last, errOrdinaryUpgrade
		}
		var pending []ordinaryPendingBackend
		for rows.Next() {
			var backend ordinaryPendingBackend
			if err := rows.Scan(&backend.PID, &backend.BackendStart, &backend.UserName, &backend.Application, &backend.ClientAddress, &backend.State, &backend.WaitEventType); err != nil {
				rows.Close()
				return last, errOrdinaryUpgrade
			}
			matches := drainAllClients
			for _, record := range records {
				if record.PID == backend.PID && record.BackendStarted.Equal(backend.BackendStart) {
					if record.Role != backend.UserName || record.ApplicationName != backend.Application {
						rows.Close()
						return last, errOrdinaryUpgrade
					}
					matches = true
				}
			}
			if matches {
				pending = append(pending, backend)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			if ctx.Err() != nil {
				return last, ctx.Err()
			}
			return last, errOrdinaryUpgrade
		}
		if len(pending) == 0 {
			return nil, nil
		}
		last = pending
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, ctx.Err()
		case <-timer.C:
		}
	}
}
