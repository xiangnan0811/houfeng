package main

// This program is copied into the pinned pre-C81 source tree by the PostgreSQL
// integration fixture. It uses that tree's real migration and deletion APIs.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
)

type request struct {
	Action             string                                   `json:"action"`
	DatabaseURL        string                                   `json:"database_url"`
	RuntimeRole        string                                   `json:"runtime_role"`
	AdminRole          string                                   `json:"admin_role"`
	DeploymentID       recordplatform.DeploymentID              `json:"deployment_id"`
	Preview            recorddeletion.CreatePreviewCommand      `json:"preview"`
	Fingerprint        recordplatform.RequestFingerprintInputV1 `json:"fingerprint"`
	ReservationID      string                                   `json:"reservation_id"`
	TokenTransport     string                                   `json:"token_transport"`
	ObservedWitness    recorddeletion.WitnessHead               `json:"observed_witness"`
	ActorID            string                                   `json:"actor_id"`
	OwnerID            string                                   `json:"owner_id"`
	OwnerLeaseDuration time.Duration                            `json:"owner_lease_duration"`
}

type result struct {
	ReservationID string                            `json:"reservation_id,omitempty"`
	ExpiresAt     time.Time                         `json:"expires_at,omitempty"`
	Operation     *recorddeletion.DeletionOperation `json:"operation,omitempty"`
}

func readRequest(path string) (request, error) {
	var value request
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || (before.Mode().Perm() != 0o600 && before.Mode().Perm() != 0o400) ||
		before.Size() <= 0 || before.Size() > 1<<20 {
		return value, errors.New("invalid private fixture request")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return value, errors.New("invalid private fixture owner")
	}
	file, err := os.Open(path)
	if err != nil {
		return value, errors.New("open fixture request")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return value, errors.New("fixture request changed")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, errors.New("decode fixture request")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return value, errors.New("trailing fixture request")
	}
	return value, nil
}

func run(ctx context.Context, input request) (result, error) {
	var output result
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" ||
		os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" {
		return output, errors.New("fixture ownership is not configured")
	}
	config, err := pgxpool.ParseConfig(input.DatabaseURL)
	if err != nil || !strings.HasPrefix(config.ConnConfig.Database, "houfeng_delivery_") || config.ConnConfig.User == "" {
		return output, errors.New("invalid fixture database")
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return output, errors.New("open fixture database")
	}
	defer pool.Close()
	var session, current string
	if err := pool.QueryRow(ctx, `select session_user::text, current_user::text`).Scan(&session, &current); err != nil ||
		session != config.ConnConfig.User || current != session {
		return output, errors.New("fixture requires a direct role")
	}
	if input.Action == "converge" {
		if session == input.RuntimeRole || session == input.AdminRole || input.RuntimeRole == "" || input.AdminRole == "" {
			return output, errors.New("invalid predecessor migration role")
		}
		if _, err := storemigrate.ConvergeAppACLCurrent(ctx, pool, input.RuntimeRole, input.AdminRole); err != nil {
			return output, errors.New("predecessor convergence failed")
		}
		return output, nil
	}
	if session != input.RuntimeRole || storemigrate.AdmitAppACLCurrentRuntime(ctx, pool) != nil {
		return output, errors.New("predecessor runtime not admitted")
	}
	gate, err := store.NewDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID: "compose-center", DeploymentID: input.DeploymentID, ProjectID: recordplatform.ProjectIDDefault,
		InstanceKind: "api", Capability: "records.runtime",
	})
	if err != nil {
		return output, errors.New("predecessor deployment unavailable")
	}
	fingerprint, err := recordplatform.FingerprintRequestV1(input.Fingerprint)
	if err != nil {
		return output, errors.New("invalid predecessor fingerprint")
	}
	input.Preview.RequestFingerprint = fingerprint
	if input.Preview.Validate() != nil {
		return output, errors.New("invalid predecessor preview")
	}
	repository := store.NewPostgresRecordDeletionRepository(pool, gate)
	switch input.Action {
	case "preview":
		preview, err := repository.CreatePreview(ctx, input.Preview)
		if err != nil {
			return output, errors.New("predecessor preview failed")
		}
		return result{ReservationID: preview.ReservationID, ExpiresAt: preview.ExpiresAt}, nil
	case "reserve":
		token, err := recordplatform.ParseDeletionRequestTokenTransportV1(input.TokenTransport)
		if err != nil || !token.MatchesCommitment(input.DeploymentID, recordplatform.ProjectID(input.Preview.Object.ProjectID), input.Preview.TokenCommitment) {
			return output, errors.New("predecessor token mismatch")
		}
		preview, err := repository.ResolvePreview(ctx, recorddeletion.PreviewLookup{
			ReservationID: input.ReservationID, Object: input.Preview.Object, Token: token,
		})
		if err != nil {
			return output, errors.New("predecessor preview unavailable")
		}
		operation, err := repository.ReservePreview(ctx, recorddeletion.ReservePreviewCommand{
			DeploymentID: input.DeploymentID, ActorID: input.ActorID,
			DeletionContractVersion: recorddeletion.RecordDeletionContractVersionV1,
			Preview:                 preview, Record: input.Preview.Record, ExpectedBindingDigest: input.Preview.BindingDigest,
			RequestFingerprint: fingerprint, ObservedWitnessHead: input.ObservedWitness,
			OwnerID: input.OwnerID, OwnerLeaseDuration: input.OwnerLeaseDuration,
			ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		})
		if err != nil {
			return output, errors.New("predecessor reserve failed")
		}
		return result{ReservationID: preview.ReservationID, ExpiresAt: preview.ExpiresAt, Operation: &operation}, nil
	default:
		return output, errors.New("unknown fixture action")
	}
}

func main() {
	if len(os.Args) != 2 {
		os.Exit(2)
	}
	if os.Args[1] == "service-preview-execute" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := legacyRunService(ctx, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	input, err := readRequest(os.Args[1])
	if err != nil {
		_, _ = io.WriteString(os.Stderr, err.Error()+"\n")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	output, err := run(ctx, input)
	if err != nil {
		_, _ = io.WriteString(os.Stderr, err.Error()+"\n")
		os.Exit(1)
	}
	if json.NewEncoder(os.Stdout).Encode(output) != nil {
		os.Exit(1)
	}
}

// legacyActor reads the same server-side identity and group membership that
// the C80 session middleware uses to construct a record authorization actor.
func legacyActor(ctx context.Context, pool *pgxpool.Pool, projectID, userID string) (recordauth.ActorScope, error) {
	if recordauth.ProjectID(projectID) != recordauth.ProjectIDDefault || recordauth.ValidateActorUserID(userID) != nil {
		return recordauth.ActorScope{}, recordauth.ErrInvalidActorScope
	}
	var actualID, role string
	var disabledAt *time.Time
	err := pool.QueryRow(ctx, `select user_id, role, disabled_at from public.users where user_id=$1`, userID).Scan(&actualID, &role, &disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordauth.ActorScope{}, recordauth.ErrDenied
	}
	if err != nil {
		return recordauth.ActorScope{}, store.ErrRecordSubjectUnavailable
	}
	if actualID != userID || role != auth.RoleAdmin || disabledAt != nil {
		return recordauth.ActorScope{}, recordauth.ErrDenied
	}
	groups, err := store.NewPostgresRecordAuthorizationRepository(pool).ListActorGroupIDs(ctx, recordauth.ProjectID(projectID), userID)
	if err != nil {
		return recordauth.ActorScope{}, store.ErrRecordSubjectUnavailable
	}
	return recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: userID, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectID(projectID), GroupIDs: groups,
	})
}

type legacySourceKey struct {
	DeploymentID string `json:"deployment_id"`
	ProjectID    string `json:"project_id"`
	Kind         string `json:"kind"`
	ID           string `json:"id"`
}

// legacySourceGuard wraps, rather than reimplements, the C80 live adapter.
// The caller must configure the single-connection pool to reject reconnects
// after its backend identity is reported to the supervising parent.
type legacySourceGuard struct {
	pool         *pgxpool.Pool
	live         records.SubjectSourceAdapter
	deploymentID string
	projectID    string
	parent       *legacyParent
	check        func(context.Context, legacySourceKey) error
}

func (guard *legacySourceGuard) Kind() records.SubjectKind { return guard.live.Kind() }

func (guard *legacySourceGuard) Resolve(ctx context.Context, actor recordauth.ActorScope, ref records.SubjectReference) (resolved records.ResolvedSubject, resultErr error) {
	if ctx == nil || guard == nil || guard.pool == nil || guard.live == nil || guard.check == nil ||
		ref.Kind != guard.live.Kind() || records.ValidateSubjectReference(ref) != nil ||
		string(actor.ProjectID) != guard.projectID {
		return records.ResolvedSubject{}, store.ErrRecordSubjectUnavailable
	}

	// C80 graph writers use the same advisory key. Retain its session-level
	// lock while the original adapter borrows this pool's only connection.
	conn, err := guard.pool.Acquire(ctx)
	if err != nil {
		return records.ResolvedSubject{}, store.ErrRecordSubjectUnavailable
	}
	if _, err = conn.Exec(ctx, `select pg_catalog.pg_advisory_lock(1213154899, 1)`); err != nil {
		// If cancellation races with acquisition, closing the physical session
		// is the only reliable way to release a possibly acquired lock.
		physical := conn.Hijack()
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = physical.Close(closeCtx)
		cancel()
		return records.ResolvedSubject{}, store.ErrRecordSubjectUnavailable
	}
	conn.Release()
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var unlocked bool
		if err := guard.pool.QueryRow(closeCtx, `select pg_catalog.pg_advisory_unlock(1213154899, 1)`).Scan(&unlocked); err != nil || !unlocked {
			if guard.parent != nil {
				guard.parent.invalid = true
			}
			guard.pool.Close()
			resolved = records.ResolvedSubject{}
			resultErr = store.ErrRecordSubjectUnavailable
		}
	}()

	tx, err := guard.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return records.ResolvedSubject{}, store.ErrRecordSubjectUnavailable
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := tx.Rollback(rollbackCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) && guard.parent != nil {
			guard.parent.invalid = true
		}
	}()
	keys, err := legacySourceClosure(ctx, tx, guard.deploymentID, guard.projectID, string(ref.Kind), ref.SourceID)
	if err != nil {
		return records.ResolvedSubject{}, store.ErrRecordSubjectUnavailable
	}
	for _, key := range keys {
		if err := guard.check(ctx, key); err != nil {
			// This is deliberately not ErrRecordSubjectNotFound: the frozen
			// live-first resolver must not fall back after denial or RPC failure.
			return records.ResolvedSubject{}, store.ErrRecordSubjectUnavailable
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return records.ResolvedSubject{}, store.ErrRecordSubjectUnavailable
	}
	return guard.live.Resolve(ctx, actor, ref)
}

// The connected source set includes historical ownership and association
// edges, as well as maintenance effects. It is read in one database snapshot;
// a missing root is still checked directly before C80 can try tombstone lookup.
func legacySourceClosure(ctx context.Context, tx pgx.Tx, deploymentID, projectID, kind, id string) ([]legacySourceKey, error) {
	if tx == nil || recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(deploymentID)) != nil ||
		recordauth.ProjectID(projectID) != recordauth.ProjectIDDefault || !records.ValidSubjectSourceID(records.SubjectKind(kind), id) {
		return nil, store.ErrRecordSubjectUnavailable
	}
	rows, err := tx.Query(ctx, `
		with recursive edges(parent_kind, parent_id, child_kind, child_id) as materialized (
		  select 'vps'::text, vps_id, 'monitoring_instance'::text, monitoring_instance_id from public.monitoring_instances
		  union select 'vps', vps_id, 'monitoring_instance', monitoring_instance_id from public.vps_monitoring_instance_links
		  union select 'vps', vps_id, 'target', target_id from public.asset_service_associations where target_id is not null
		  union select 'vps', vps_id, 'target', target_id from public.asset_domain_associations where target_id is not null
		  union select 'vps', action.vps_id, effect.resource_kind, effect.resource_id
		    from public.vps_maintenance_effects effect join public.vps_maintenance_actions action on action.action_id=effect.action_id
		), adjacency(from_kind, from_id, to_kind, to_id) as (
		  select parent_kind, parent_id, child_kind, child_id from edges
		  union all select child_kind, child_id, parent_kind, parent_id from edges
		), component(kind, id) as (
		  select $1::text, $2::text
		  union select adjacency.to_kind, adjacency.to_id from component
		    join adjacency on adjacency.from_kind=component.kind and adjacency.from_id=component.id
		)
		select kind, id from component order by kind collate "C", id collate "C" limit 1025`, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []legacySourceKey{{DeploymentID: deploymentID, ProjectID: projectID, Kind: kind, ID: id}}
	for rows.Next() {
		var key legacySourceKey
		if err := rows.Scan(&key.Kind, &key.ID); err != nil {
			return nil, err
		}
		if !records.ValidSubjectSourceID(records.SubjectKind(key.Kind), key.ID) {
			return nil, store.ErrRecordSubjectUnavailable
		}
		if key.Kind == kind && key.ID == id {
			continue
		}
		key.DeploymentID, key.ProjectID = deploymentID, projectID
		keys = append(keys, key)
		if len(keys) > 1024 {
			return nil, store.ErrRecordSubjectUnavailable
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

const legacyFrameLimit = 16 << 10

type legacyServiceConfig struct {
	Version         uint64
	PhaseGeneration uint64
	RuntimeURL      string
	DeploymentID    string
	ProjectID       string
	ActorUserID     string
	RecordID        string
}

// legacyFields rejects missing, duplicate, extra and null fields, including
// JSON values with a valid prefix followed by another document.
func legacyFields(body []byte, names ...string) (map[string]json.RawMessage, error) {
	return legacyFieldsNullable(body, nil, names...)
}

// Only the inventory response opts in to nullable fields. All other frames
// retain the original null rejection.
func legacyFieldsNullable(body []byte, nullable []string, names ...string) (map[string]json.RawMessage, error) {
	if len(body) == 0 || len(body) > legacyFrameLimit {
		return nil, errors.New("invalid private frame")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("invalid private frame")
	}
	expected := make(map[string]bool, len(names))
	for _, name := range names {
		expected[name] = true
	}
	allowNull := make(map[string]bool, len(nullable))
	for _, name := range nullable {
		if !expected[name] || allowNull[name] {
			return nil, errors.New("invalid private frame")
		}
		allowNull[name] = true
	}
	fields := make(map[string]json.RawMessage, len(names))
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || !expected[key] || fields[key] != nil {
			return nil, errors.New("invalid private frame")
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || (bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !allowNull[key]) {
			return nil, errors.New("invalid private frame")
		}
		fields[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != len(expected) {
		return nil, errors.New("invalid private frame")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, errors.New("invalid private frame")
	}
	return fields, nil
}

func legacyReadConfig(ctx context.Context) (legacyServiceConfig, error) {
	var config legacyServiceConfig
	if ctx == nil || ctx.Err() != nil {
		return config, errors.New("invalid private config")
	}
	file := os.NewFile(4, "legacy-private-config")
	if file == nil {
		return config, errors.New("invalid private config")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || (!info.Mode().IsRegular() && info.Mode()&os.ModeNamedPipe == 0) ||
		(info.Mode().IsRegular() && (info.Size() <= 0 || info.Size() > legacyFrameLimit)) {
		return config, errors.New("invalid private config")
	}
	if info.Mode()&os.ModeNamedPipe != 0 {
		deadline, ok := ctx.Deadline()
		if !ok || file.SetReadDeadline(deadline) != nil {
			return config, errors.New("invalid private config")
		}
		done := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { _ = file.SetReadDeadline(time.Now()); close(done) })
		defer func() {
			if !stop() {
				<-done
			}
		}()
	}
	body, err := io.ReadAll(io.LimitReader(file, legacyFrameLimit+1))
	if err != nil {
		return config, errors.New("invalid private config")
	}
	fields, err := legacyFields(body, "version", "phase_generation", "runtime_url", "deployment_id", "project_id", "actor_user_id", "record_id")
	if err != nil || json.Unmarshal(fields["version"], &config.Version) != nil ||
		json.Unmarshal(fields["phase_generation"], &config.PhaseGeneration) != nil ||
		json.Unmarshal(fields["runtime_url"], &config.RuntimeURL) != nil ||
		json.Unmarshal(fields["deployment_id"], &config.DeploymentID) != nil ||
		json.Unmarshal(fields["project_id"], &config.ProjectID) != nil ||
		json.Unmarshal(fields["actor_user_id"], &config.ActorUserID) != nil ||
		json.Unmarshal(fields["record_id"], &config.RecordID) != nil || config.Version != 1 ||
		config.PhaseGeneration == 0 || config.RuntimeURL == "" || len(config.RuntimeURL) > 8192 ||
		recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(config.DeploymentID)) != nil ||
		recordauth.ProjectID(config.ProjectID) != recordauth.ProjectIDDefault ||
		recordauth.ValidateActorUserID(config.ActorUserID) != nil || config.RecordID == "" || len(config.RecordID) > 68 {
		return legacyServiceConfig{}, errors.New("invalid private config")
	}
	return config, nil
}

// legacyParent is the exclusive, credential-checked SOCK_SEQPACKET control
// channel. Only fixed observation requests are sent over it.
type legacyParent struct {
	conn              *net.UnixConn
	peer              unix.Ucred
	generation        uint64
	readyAcknowledged bool
	denied            bool
	invalid           bool
}

func legacyOpenParent(generation uint64) (*legacyParent, error) {
	// Linux ties this setting to the calling thread, so keep that thread until
	// the one-shot process exits. The second parent check closes the prctl race.
	runtime.LockOSThread()
	parentPID := os.Getppid()
	if parentPID <= 1 || unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0) != nil || os.Getppid() != parentPID {
		return nil, errors.New("parent unavailable")
	}
	unix.CloseOnExec(3)
	unix.CloseOnExec(4)
	unix.CloseOnExec(6)
	var guard unix.Stat_t
	if unix.Fstat(3, &guard) != nil || guard.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errors.New("guard unavailable")
	}
	typ, err := unix.GetsockoptInt(6, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || typ != unix.SOCK_SEQPACKET || unix.SetsockoptInt(6, unix.SOL_SOCKET, unix.SO_PASSCRED, 1) != nil {
		return nil, errors.New("parent channel unavailable")
	}
	peer, err := unix.GetsockoptUcred(6, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer == nil || int(peer.Pid) != parentPID || peer.Uid != 10002 || peer.Gid != 10002 ||
		os.Geteuid() != 10002 || os.Getegid() != 10002 {
		return nil, errors.New("parent identity unavailable")
	}
	file := os.NewFile(6, "legacy-parent")
	if file == nil {
		return nil, errors.New("parent channel unavailable")
	}
	converted, err := net.FileConn(file)
	_ = file.Close()
	if err != nil {
		return nil, errors.New("parent channel unavailable")
	}
	connection, ok := converted.(*net.UnixConn)
	if !ok {
		_ = converted.Close()
		return nil, errors.New("parent channel unavailable")
	}
	return &legacyParent{conn: connection, peer: *peer, generation: generation}, nil
}

func (parent *legacyParent) exchange(ctx context.Context, request any, responseFields ...string) (map[string]json.RawMessage, error) {
	return parent.exchangeNullable(ctx, request, nil, responseFields...)
}

func (parent *legacyParent) exchangeNullable(ctx context.Context, request any, nullable []string, responseFields ...string) (_ map[string]json.RawMessage, resultErr error) {
	defer func() {
		if parent != nil && resultErr != nil {
			parent.invalid = true
		}
	}()
	if parent == nil || parent.conn == nil || parent.invalid || ctx == nil || ctx.Err() != nil {
		return nil, errors.New("parent channel unavailable")
	}
	deadline, ok := ctx.Deadline()
	if !ok || parent.conn.SetDeadline(deadline) != nil {
		return nil, errors.New("parent deadline unavailable")
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = parent.conn.SetDeadline(time.Now()); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
		_ = parent.conn.SetDeadline(time.Time{})
	}()
	body, err := json.Marshal(request)
	if err != nil || len(body) == 0 || len(body) > legacyFrameLimit {
		return nil, errors.New("invalid parent request")
	}
	n, oobn, err := parent.conn.WriteMsgUnix(body, nil, nil)
	if err != nil || n != len(body) || oobn != 0 {
		return nil, errors.New("parent channel unavailable")
	}
	message := make([]byte, legacyFrameLimit+1)
	control := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(4*256))
	n, oobn, flags, _, err := parent.conn.ReadMsgUnix(message, control)
	if err != nil {
		return nil, errors.New("parent channel unavailable")
	}
	controls, controlErr := unix.ParseSocketControlMessage(control[:oobn])
	credentials := 0
	badControl := controlErr != nil
	for _, item := range controls {
		if item.Header.Level == unix.SOL_SOCKET && item.Header.Type == unix.SCM_RIGHTS {
			fds, parseErr := unix.ParseUnixRights(&item)
			for _, fd := range fds {
				_ = unix.Close(fd)
			}
			_ = parseErr
			badControl = true
			continue
		}
		if item.Header.Level != unix.SOL_SOCKET || item.Header.Type != unix.SCM_CREDENTIALS {
			badControl = true
			continue
		}
		credential, parseErr := unix.ParseUnixCredentials(&item)
		if parseErr != nil || credential == nil || *credential != parent.peer {
			badControl = true
		}
		credentials++
	}
	if n == 0 || n > legacyFrameLimit || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || badControl || credentials != 1 || ctx.Err() != nil {
		return nil, errors.New("invalid parent response")
	}
	fields, err := legacyFieldsNullable(message[:n], nullable, responseFields...)
	if err != nil {
		return nil, errors.New("invalid parent response")
	}
	return fields, nil
}

func (parent *legacyParent) responseIdentity(fields map[string]json.RawMessage, expected string) bool {
	var op string
	var generation uint64
	valid := json.Unmarshal(fields["op"], &op) == nil && json.Unmarshal(fields["phase_generation"], &generation) == nil &&
		op == expected && generation == parent.generation
	if !valid {
		parent.invalid = true
	}
	return valid
}

func (parent *legacyParent) ready(ctx context.Context, pid int32, started time.Time, role string) error {
	request := struct {
		Op               string    `json:"op"`
		PhaseGeneration  uint64    `json:"phase_generation"`
		BackendPID       int32     `json:"backend_pid"`
		BackendStartedAt time.Time `json:"backend_started_at"`
		Role             string    `json:"role"`
	}{"ready", parent.generation, pid, started.UTC(), role}
	fields, err := parent.exchange(ctx, request, "op", "phase_generation")
	if err != nil || !parent.responseIdentity(fields, "ready_ack") {
		return errors.New("parent readiness denied")
	}
	parent.readyAcknowledged = true
	return nil
}

func (parent *legacyParent) sourceCheck(ctx context.Context, key legacySourceKey) error {
	request := struct {
		Op              string          `json:"op"`
		PhaseGeneration uint64          `json:"phase_generation"`
		Source          legacySourceKey `json:"source"`
	}{"source_check", parent.generation, key}
	fields, err := parent.exchange(ctx, request, "op", "phase_generation", "allowed")
	var allowed bool
	if err != nil || !parent.responseIdentity(fields, "source_check") || json.Unmarshal(fields["allowed"], &allowed) != nil {
		parent.invalid = true
		return errors.New("source authority unavailable")
	}
	if !allowed {
		parent.denied = true
		return recordauth.ErrDenied
	}
	return nil
}

type legacyWitness struct{ parent *legacyParent }

func (source legacyWitness) CurrentWitnessHead(ctx context.Context) (recorddeletion.WitnessHead, error) {
	request := struct {
		Op              string `json:"op"`
		PhaseGeneration uint64 `json:"phase_generation"`
	}{"witness_head", source.parent.generation}
	fields, err := source.parent.exchange(ctx, request, "op", "phase_generation", "sequence", "hash", "healthy")
	return legacyWitnessResponse(source.parent, fields, err, "witness_head", false)
}

func (source legacyWitness) VerifyWitnessExtension(ctx context.Context, prior recorddeletion.WitnessHead) (recorddeletion.WitnessHead, error) {
	if prior.Validate() != nil {
		return recorddeletion.WitnessHead{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	request := struct {
		Op              string `json:"op"`
		PhaseGeneration uint64 `json:"phase_generation"`
		Sequence        uint64 `json:"sequence"`
		Hash            string `json:"hash"`
	}{"witness_extension", source.parent.generation, prior.Sequence, hex.EncodeToString(prior.EntryHash[:])}
	fields, err := source.parent.exchange(ctx, request, "op", "phase_generation", "accepted", "healthy", "sequence", "hash")
	return legacyWitnessResponse(source.parent, fields, err, "witness_extension", true)
}

func legacyWitnessResponse(parent *legacyParent, fields map[string]json.RawMessage, responseErr error, op string, extension bool) (recorddeletion.WitnessHead, error) {
	var head recorddeletion.WitnessHead
	var healthy, accepted bool
	var hash string
	if responseErr != nil || !parent.responseIdentity(fields, op) ||
		json.Unmarshal(fields["healthy"], &healthy) != nil ||
		(extension && json.Unmarshal(fields["accepted"], &accepted) != nil) ||
		json.Unmarshal(fields["sequence"], &head.Sequence) != nil || json.Unmarshal(fields["hash"], &hash) != nil ||
		len(hash) != 64 || strings.ToLower(hash) != hash {
		parent.invalid = true
		return recorddeletion.WitnessHead{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	raw, err := hex.DecodeString(hash)
	if err != nil || len(raw) != len(head.EntryHash) {
		parent.invalid = true
		return recorddeletion.WitnessHead{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if !healthy || (extension && !accepted) {
		return recorddeletion.WitnessHead{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	copy(head.EntryHash[:], raw)
	if head.Validate() != nil {
		parent.invalid = true
		return recorddeletion.WitnessHead{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return head, nil
}

type legacyRuntimeIdentity struct {
	PID     int32
	Started time.Time
	Role    string
}

// legacyRuntime opens a direct runtime role with a maximum of one physical
// connection. Once authenticated, a replacement backend is never admitted.
func legacyOpenRuntime(ctx context.Context, runtimeURL string) (*pgxpool.Pool, legacyRuntimeIdentity, error) {
	var identity legacyRuntimeIdentity
	parsed, err := url.Parse(runtimeURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.User == nil ||
		parsed.User.Username() == "" || parsed.Hostname() == "" || parsed.Port() == "" ||
		parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, "/") || len(parsed.Path) < 2 || strings.Contains(parsed.Path[1:], "/") {
		return nil, identity, errors.New("invalid runtime endpoint")
	}
	password, hasPassword := parsed.User.Password()
	port, portErr := strconv.ParseUint(parsed.Port(), 10, 16)
	parameters, queryErr := url.ParseQuery(parsed.RawQuery)
	if !hasPassword || password == "" || portErr != nil || port == 0 || queryErr != nil || len(parameters) != 1 || len(parameters["sslmode"]) != 1 {
		return nil, identity, errors.New("invalid runtime endpoint")
	}
	switch parameters["sslmode"][0] {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return nil, identity, errors.New("invalid runtime endpoint")
	}
	// pgx otherwise reads service, password and client TLS files from the
	// process environment or the OS user's home even with an explicit URL.
	// This one-shot process has exactly one supplied credential.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "PG") {
			if os.Unsetenv(name) != nil {
				return nil, identity, errors.New("runtime environment unavailable")
			}
		}
	}
	parameters.Set("passfile", "/dev/null")
	parameters.Set("sslcert", "")
	parameters.Set("sslkey", "")
	parameters.Set("sslrootcert", "")
	parsed.RawQuery = parameters.Encode()
	config, err := pgxpool.ParseConfig(parsed.String())
	if err != nil || config.ConnConfig.User != parsed.User.Username() || config.ConnConfig.Password != password ||
		config.ConnConfig.Database != parsed.Path[1:] || config.ConnConfig.Host != parsed.Hostname() ||
		config.ConnConfig.Port != uint16(port) || len(config.ConnConfig.Fallbacks) != 0 {
		return nil, identity, errors.New("invalid runtime endpoint")
	}
	deadline, ok := ctx.Deadline()
	if !ok || ctx.Err() != nil {
		return nil, identity, errors.New("runtime deadline unavailable")
	}
	connectTimeout := time.Until(deadline)
	if connectTimeout <= 0 {
		return nil, identity, errors.New("runtime deadline unavailable")
	}
	if connectTimeout > 10*time.Second {
		connectTimeout = 10 * time.Second
	}
	config.ConnConfig.ConnectTimeout = connectTimeout
	config.MaxConns = 1
	config.MinConns = 0
	config.MinIdleConns = 0
	config.ConnConfig.RuntimeParams = map[string]string{"application_name": "legacy_c80_service"}
	var attempted atomic.Bool
	var backend atomic.Uint32
	config.BeforeConnect = func(_ context.Context, _ *pgx.ConnConfig) error {
		if !attempted.CompareAndSwap(false, true) {
			return errors.New("runtime backend changed")
		}
		return nil
	}
	config.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		backend.Store(conn.PgConn().PID())
		return nil
	}
	config.BeforeAcquire = func(_ context.Context, conn *pgx.Conn) bool {
		return backend.Load() != 0 && conn.PgConn().PID() == backend.Load()
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, identity, errors.New("runtime connection unavailable")
	}
	var session, current, roleSetting string
	var login, super, inherit, createRole, createDB, replication, bypass, member bool
	err = pool.QueryRow(ctx, `select activity.pid, activity.backend_start, activity.usename::text,
		session_user::text, current_user::text, pg_catalog.current_setting('role'), role.rolcanlogin,
		role.rolsuper, role.rolinherit, role.rolcreaterole, role.rolcreatedb, role.rolreplication, role.rolbypassrls,
		exists(select 1 from pg_catalog.pg_auth_members membership where membership.roleid=role.oid or membership.member=role.oid)
		from pg_catalog.pg_stat_activity activity join pg_catalog.pg_roles role on role.rolname=session_user
		where activity.pid=pg_catalog.pg_backend_pid()`).Scan(&identity.PID, &identity.Started, &identity.Role,
		&session, &current, &roleSetting, &login, &super, &inherit, &createRole, &createDB, &replication, &bypass, &member)
	if err != nil || identity.PID <= 0 || uint32(identity.PID) != backend.Load() || identity.Started.IsZero() ||
		identity.Role != config.ConnConfig.User || session != identity.Role || current != session || roleSetting != "none" ||
		!login || super || inherit || createRole || createDB || replication || bypass || member {
		pool.Close()
		return nil, legacyRuntimeIdentity{}, errors.New("runtime identity unavailable")
	}
	identity.Started = identity.Started.UTC()
	return pool, identity, nil
}

// legacyRecordAuthorization uses C80's real record snapshot loader and its
// real PostgreSQL subject adapters. An absent live source cannot obtain a
// fabricated witnessed tombstone through this one-shot maintenance channel.
func legacyRecordAuthorization(pool *pgxpool.Pool, gate store.AdmissionGate, parent *legacyParent, config legacyServiceConfig) (*store.PostgresCurrentRecordAuthorizationSource, error) {
	if pool == nil || gate == nil || parent == nil {
		return nil, store.ErrRecordSubjectUnavailable
	}
	live := []records.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(store.NewPostgresVPSAssetRepository(pool)),
		store.NewMonitoringInstanceRecordSubjectAdapter(store.NewPostgresMonitoringInstanceRepository(pool)),
		store.NewTargetRecordSubjectAdapter(store.NewPostgresTargetRepository(pool)),
	}
	guarded := make([]records.SubjectSourceAdapter, 0, len(live))
	for _, adapter := range live {
		guarded = append(guarded, &legacySourceGuard{pool: pool, live: adapter, deploymentID: config.DeploymentID, projectID: config.ProjectID, parent: parent, check: parent.sourceCheck})
	}
	registry, err := records.NewSubjectAdapterRegistry(guarded)
	if err != nil {
		return nil, store.ErrRecordSubjectUnavailable
	}
	resolver := store.NewRecordSubjectReadResolver(registry, nil)
	return store.NewPostgresCurrentRecordAuthorizationSource(pool, resolver, gate), nil
}

// These are the seven adapters implemented in the frozen source. Their health
// and preview observations are produced by their own PostgreSQL repositories.
func legacyInstalledAdapters(pool *pgxpool.Pool, gate store.AdmissionGate) ([]recorddeletion.Adapter, error) {
	repository := store.NewPostgresRecordDeletionRepository(pool, gate)
	core, err := recorddeletion.NewCoreAdapter(repository)
	if err != nil {
		return nil, err
	}
	attachment, err := attachments.NewDeletionAdapter(store.NewPostgresAttachmentRepository(pool))
	if err != nil {
		return nil, err
	}
	evidenceAdapter, err := evidence.NewDeletionAdapter(store.NewPostgresEvidenceRepository(pool, gate))
	if err != nil {
		return nil, err
	}
	search, err := recordsearch.NewDeletionAdapter(repository)
	if err != nil {
		return nil, err
	}
	activityAdapter, err := activity.NewDeletionAdapter(repository)
	if err != nil {
		return nil, err
	}
	collaboration, err := recordcollaboration.NewDeletionAdapter(repository)
	if err != nil {
		return nil, err
	}
	portabilityAdapter, err := portability.NewDeletionAdapter(repository)
	if err != nil {
		return nil, err
	}
	return []recorddeletion.Adapter{core, attachment, evidenceAdapter, search, activityAdapter, collaboration, portabilityAdapter}, nil
}

type legacyRecordFacts struct {
	Current       records.CurrentRecordAuthorization
	DeliveryEpoch recordplatform.ContentEpoch
}

// legacyReadRecordFacts combines C80's actual current authorization loader
// with an admitted read of the matching record and delivery-epoch CAS. It
// returns only observed APP facts; it does not manufacture an inventory.
func legacyReadRecordFacts(ctx context.Context, pool *pgxpool.Pool, gate store.AdmissionGate, source *store.PostgresCurrentRecordAuthorizationSource, actor recordauth.ActorScope, recordID string) (legacyRecordFacts, error) {
	var facts legacyRecordFacts
	if ctx == nil || pool == nil || gate == nil || source == nil {
		return facts, store.ErrRecordSubjectUnavailable
	}
	current, err := source.ResolveCurrentRecordAuthorization(ctx, actor, recordID)
	if err != nil {
		return facts, err
	}
	if current.RecordID != recordID || current.Evidence.ProjectID != actor.ProjectID ||
		current.LockVersion == 0 || current.AuthorizationEpoch == 0 || records.ValidateLifecycle(current.Lifecycle) != nil {
		return facts, store.ErrRecordSubjectUnavailable
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return facts, store.ErrRecordSubjectUnavailable
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if err := gate.Admit(ctx, tx); err != nil {
		return facts, store.ErrRecordSubjectUnavailable
	}
	var epoch int64
	err = tx.QueryRow(ctx, `select epoch.delivery_epoch
		from public.records record
		join public.content_delivery_epochs epoch
		  on epoch.project_id=record.project_id and epoch.object_kind='record' and epoch.object_id=record.record_id
		where record.project_id=$1 and record.record_id=$2 and record.current_revision_id=$3
		  and record.lock_version=$4 and record.authorization_epoch=$5 and record.lifecycle=$6
		  and not exists(select 1 from public.deletion_reservations reservation
		    where reservation.project_id=record.project_id and reservation.object_kind='record'
		      and reservation.object_id=record.record_id and reservation.state in ('fenced','committed'))`,
		string(actor.ProjectID), recordID, current.CurrentRevisionID, int64(current.LockVersion), int64(current.AuthorizationEpoch), string(current.Lifecycle)).Scan(&epoch)
	if err != nil || epoch < 0 {
		return facts, store.ErrRecordSubjectUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return facts, store.ErrRecordSubjectUnavailable
	}
	facts.Current, facts.DeliveryEpoch = current, recordplatform.ContentEpoch(epoch)
	return facts, nil
}

type legacyServiceSession struct {
	config   legacyServiceConfig
	parent   *legacyParent
	pool     *pgxpool.Pool
	gate     store.AdmissionGate
	actor    recordauth.ActorScope
	records  *store.PostgresCurrentRecordAuthorizationSource
	adapters []recorddeletion.Adapter
	witness  legacyWitness
}

// legacyOpenServiceSession establishes the fixed parent handshake and the
// actual C80 runtime admission before composing the service dependencies.
func legacyOpenServiceSession(ctx context.Context) (*legacyServiceSession, uint64, error) {
	parent, err := legacyOpenParent(0)
	if err != nil {
		return nil, 0, err
	}
	closeParent := true
	defer func() {
		if closeParent {
			_ = parent.conn.Close()
		}
	}()
	config, err := legacyReadConfig(ctx)
	if err != nil {
		return nil, 0, err
	}
	parent.generation = config.PhaseGeneration
	pool, identity, err := legacyOpenRuntime(ctx, config.RuntimeURL)
	config.RuntimeURL = ""
	if err != nil {
		return nil, 0, err
	}
	closePool := true
	defer func() {
		if closePool {
			pool.Close()
		}
	}()
	if err := parent.ready(ctx, identity.PID, identity.Started, identity.Role); err != nil {
		return nil, 0, err
	}
	// Once readiness is authenticated the caller owns the partial session,
	// including failures that must still report a supervised result.
	session := &legacyServiceSession{config: config, parent: parent, pool: pool}
	closeParent, closePool = false, false
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, pool); err != nil {
		return session, config.PhaseGeneration, errLegacyAdmissionUnavailable
	}
	gate, err := store.NewDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID: "compose-center", DeploymentID: recordplatform.DeploymentID(config.DeploymentID),
		ProjectID: recordplatform.ProjectID(config.ProjectID), InstanceKind: "api", Capability: "records.runtime",
	})
	if err != nil {
		return session, config.PhaseGeneration, errLegacyAdmissionUnavailable
	}
	actor, err := legacyActor(ctx, pool, config.ProjectID, config.ActorUserID)
	if err != nil {
		return session, config.PhaseGeneration, errLegacyAdmissionUnavailable
	}
	recordSource, err := legacyRecordAuthorization(pool, gate, parent, config)
	if err != nil {
		return session, config.PhaseGeneration, err
	}
	adapters, err := legacyInstalledAdapters(pool, gate)
	if err != nil {
		return session, config.PhaseGeneration, errors.New("deletion adapters unavailable")
	}
	session.gate, session.actor, session.records = gate, actor, recordSource
	session.adapters, session.witness = adapters, legacyWitness{parent: parent}
	return session, config.PhaseGeneration, nil
}

func (session *legacyServiceSession) Close() {
	if session == nil {
		return
	}
	if session.pool != nil {
		session.pool.Close()
	}
	if session.parent != nil && session.parent.conn != nil {
		_ = session.parent.conn.Close()
	}
}
