//go:build linux

package deploy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/secure/precis"

	"houfeng/internal/center/recorddeletion"
)

func ledgerCheckAppLoginInventory(ctx context.Context, app *pgxpool.Pool, bootstrap, admin string, previous []string, fenced bool) error {
	allowed := map[string]bool{bootstrap: true, admin: true}
	if !fenced {
		for _, role := range previous {
			allowed[role] = true
		}
	}
	rows, err := app.Query(ctx, `select rolname from pg_catalog.pg_roles r where r.rolcanlogin and
		(r.rolsuper or r.rolcreaterole or r.rolreplication or pg_catalog.has_database_privilege(r.oid,pg_catalog.current_database(),'CONNECT'))`)
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		if rows.Scan(&role) != nil || !allowed[role] {
			return ErrRecordLedgerFence
		}
	}
	if rows.Err() != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerSCRAM(password string, salt []byte, iterations int) string {
	prepared, err := precis.OpaqueString.String(password)
	if err != nil {
		prepared = password
	}
	salted := pbkdf2.Key([]byte(prepared), salt, iterations, sha256.Size, sha256.New)
	client := hmac.New(sha256.New, salted)
	_, _ = client.Write([]byte("Client Key"))
	stored := sha256.Sum256(client.Sum(nil))
	server := hmac.New(sha256.New, salted)
	_, _ = server.Write([]byte("Server Key"))
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations, base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(stored[:]), base64.StdEncoding.EncodeToString(server.Sum(nil)))
}

func ledgerMatchesSCRAM(verifier, password string) bool {
	parts := strings.Split(verifier, "$")
	if len(parts) != 3 || parts[0] != "SCRAM-SHA-256" {
		return false
	}
	args := strings.Split(parts[1], ":")
	if len(args) != 2 {
		return false
	}
	iterations, err := strconv.Atoi(args[0])
	if err != nil || iterations < 1 || iterations > 1000000 {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(args[1])
	if err != nil || len(salt) == 0 {
		return false
	}
	actual := ledgerSCRAM(password, salt, iterations)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(verifier)) == 1
}

func ledgerDisableAdmin(ctx context.Context, app *pgxpool.Pool, role string) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrRecordLedgerFence
	}
	if _, err := app.Exec(ctx, "alter role "+pgx.Identifier{role}.Sanitize()+" nologin"); err != nil {
		return ErrRecordLedgerFence
	}
	rows, err := app.Query(ctx, `select pg_catalog.pg_terminate_backend(pid) from pg_catalog.pg_stat_activity where usename=$1 and pid<>pg_catalog.pg_backend_pid()`, role)
	if err != nil {
		return ErrRecordLedgerFence
	}
	for rows.Next() {
		var stopped bool
		if rows.Scan(&stopped) != nil || !stopped {
			rows.Close()
			return ErrRecordLedgerFence
		}
	}
	if rows.Err() != nil {
		rows.Close()
		return ErrRecordLedgerFence
	}
	rows.Close()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var sessions, prepared int
		if app.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity where usename=$1`, role).Scan(&sessions) != nil || app.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts where owner=$1`, role).Scan(&prepared) != nil || prepared != 0 {
			return ErrRecordLedgerFence
		}
		if sessions == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrRecordLedgerFence
		case <-ticker.C:
		}
	}
}

func ledgerCheckAppFenced(ctx context.Context, app *pgxpool.Pool, bootstrap string, inventory ledgerAppInventory, credential ledgerAdminCredential) error {
	if bootstrap != inventory.Bootstrap {
		return ErrRecordLedgerFence
	}
	if err := ledgerCheckAppLoginInventory(ctx, app, bootstrap, inventory.Admin, inventory.Previous, true); err != nil {
		return err
	}
	if err := ledgerVerifyAppRoleState(ctx, app, inventory, credential, ledgerAppTarget); err != nil {
		return err
	}
	var unsafe, prepared int
	if app.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity where usename=any($1::name[])`, inventory.Previous).Scan(&unsafe) != nil || unsafe != 0 {
		return ErrRecordLedgerFence
	}
	roles := append(append([]string(nil), inventory.Previous...), inventory.Admin)
	if app.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts where database=pg_catalog.current_database() or owner=any($1::name[])`, roles).Scan(&prepared) != nil || prepared != 0 {
		return ErrRecordLedgerFence
	}
	return nil
}

type ledgerAppSession struct {
	PID     int32
	Started time.Time
	Role    string
}

func ledgerCurrentAppSession(ctx context.Context, pool *pgxpool.Pool) (ledgerAppSession, error) {
	var session ledgerAppSession
	err := pool.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&session.PID, &session.Started, &session.Role)
	if err != nil || session.PID == 0 || session.Started.IsZero() || session.Role == "" {
		return ledgerAppSession{}, ErrRecordLedgerFence
	}
	return session, nil
}

func ledgerCheckAppSessions(ctx context.Context, app ledgerAppQueryer, inventory ledgerAppInventory, allowed map[int32]ledgerAppSession) error {
	rows, err := app.Query(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity
		where backend_type='client backend' and (datname=pg_catalog.current_database() or usename=any($1::name[]))`, ledgerAppRoleNames(inventory))
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer rows.Close()
	for rows.Next() {
		var actual ledgerAppSession
		if rows.Scan(&actual.PID, &actual.Started, &actual.Role) != nil {
			return ErrRecordLedgerFence
		}
		expected, ok := allowed[actual.PID]
		if !ok || expected.Role != actual.Role || !expected.Started.Equal(actual.Started) {
			return ErrRecordLedgerUnexpectedSession
		}
	}
	if rows.Err() != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerCheckProvisionalOperation(ctx context.Context, app *pgxpool.Pool, request recorddeletion.LedgerAppendRequest) error {
	if request.Validate() != nil || request.EntryType != recorddeletion.LedgerEntryDeleteCommit || request.DeletionContractVersion != 1 {
		return ErrRecordLedgerFence
	}
	fingerprint, err := request.RequestFingerprint.PersistedBytes()
	if err != nil {
		return ErrRecordLedgerFence
	}
	var exact bool
	err = app.QueryRow(ctx, `select exists(select 1 from public.record_purge_operations o
		join public.deletion_reservations r on r.reservation_id=o.reservation_id and r.project_id=o.project_id
		where o.operation_id=$1 and o.project_id=$2 and o.deployment_id=$3 and o.actor_id=$4 and o.reason_code=$5
		and o.deletion_contract_version=1 and o.operation_state='provisional_fenced' and o.ledger_sequence is null and o.ledger_entry_hash is null
		and o.release_epoch=0 and r.state='fenced' and r.object_kind=$6 and r.object_id=$7
		and r.deletion_token_commitment=$8 and r.request_fingerprint=$9)`, request.OperationID, string(request.ProjectID), string(request.DeploymentID), request.ActorID, string(request.ReasonCode), request.Object.ObjectKind, request.Object.ObjectID, request.TokenCommitment[:], fingerprint[:]).Scan(&exact)
	if err != nil || !exact {
		return ErrRecordLedgerFence
	}
	return nil
}
