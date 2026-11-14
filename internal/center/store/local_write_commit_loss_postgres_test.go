//go:build linux

package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/store"
)

// commitLossProxy forwards the actual PostgreSQL protocol and drops one
// connection only after the server has emitted the selected transaction's
// COMMIT CommandComplete. It does not simulate a database commit or response.
// The runner's PostgreSQL connection must be unencrypted so frames remain visible.
type commitLossProxy struct {
	listener      net.Listener
	network       string
	address       string
	ctx           context.Context
	cancel        context.CancelFunc
	target        string
	rejectResolve bool

	mu            sync.Mutex
	connections   map[net.Conn]struct{}
	nextID        uint64
	dropped       bool
	droppedID     uint64
	queries       map[string][]uint64
	resolveDenied []uint64
	problems      []error
	wg            sync.WaitGroup
}

type commitLossConnection struct {
	id     uint64
	mu     sync.Mutex
	target bool
}

type commitLossSnapshot struct {
	dropped       bool
	droppedID     uint64
	queries       map[string][]uint64
	resolveDenied []uint64
}

func newCommitLossProxy(t *testing.T, ctx context.Context, network, address, target string, rejectResolve bool) *commitLossProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("open PostgreSQL fault proxy: %v", err)
	}
	proxyCtx, cancel := context.WithCancel(ctx)
	proxy := &commitLossProxy{
		listener: listener, network: network, address: address, ctx: proxyCtx, cancel: cancel,
		target: target, rejectResolve: rejectResolve, connections: make(map[net.Conn]struct{}),
		queries: make(map[string][]uint64),
	}
	proxy.wg.Add(1)
	go proxy.accept()
	t.Cleanup(func() {
		proxy.close()
		proxy.mu.Lock()
		defer proxy.mu.Unlock()
		for _, problem := range proxy.problems {
			t.Errorf("PostgreSQL fault proxy: %v", problem)
		}
	})
	return proxy
}

func (proxy *commitLossProxy) accept() {
	defer proxy.wg.Done()
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			if proxy.ctx.Err() == nil {
				proxy.addProblem(fmt.Errorf("accept: %w", err))
			}
			return
		}
		proxy.mu.Lock()
		if proxy.ctx.Err() != nil {
			proxy.mu.Unlock()
			_ = client.Close()
			return
		}
		proxy.nextID++
		state := &commitLossConnection{id: proxy.nextID}
		proxy.connections[client] = struct{}{}
		proxy.wg.Add(1)
		proxy.mu.Unlock()
		go proxy.serve(client, state)
	}
}

func (proxy *commitLossProxy) serve(client net.Conn, state *commitLossConnection) {
	defer proxy.wg.Done()
	defer proxy.removeConnection(client)
	dialCtx, cancel := context.WithTimeout(proxy.ctx, 15*time.Second)
	defer cancel()
	upstream, err := (&net.Dialer{}).DialContext(dialCtx, proxy.network, proxy.address)
	if err != nil {
		if proxy.ctx.Err() == nil {
			proxy.addProblem(fmt.Errorf("dial PostgreSQL: %w", err))
		}
		return
	}
	proxy.mu.Lock()
	if proxy.ctx.Err() != nil {
		proxy.mu.Unlock()
		_ = upstream.Close()
		return
	}
	proxy.connections[upstream] = struct{}{}
	proxy.mu.Unlock()
	defer proxy.removeConnection(upstream)

	startup, err := commitLossReadStartup(client)
	if err != nil {
		return
	}
	if err := commitLossWrite(upstream, startup); err != nil {
		return
	}

	finished := make(chan struct{}, 2)
	go func() {
		proxy.forwardClient(client, upstream, state)
		finished <- struct{}{}
	}()
	go func() {
		proxy.forwardServer(upstream, client, state)
		finished <- struct{}{}
	}()
	<-finished
	_ = client.Close()
	_ = upstream.Close()
	<-finished
}

func (proxy *commitLossProxy) removeConnection(conn net.Conn) {
	_ = conn.Close()
	proxy.mu.Lock()
	delete(proxy.connections, conn)
	proxy.mu.Unlock()
}

func (proxy *commitLossProxy) close() {
	proxy.cancel()
	_ = proxy.listener.Close()
	proxy.mu.Lock()
	for conn := range proxy.connections {
		_ = conn.Close()
	}
	proxy.mu.Unlock()
	proxy.wg.Wait()
}

func (proxy *commitLossProxy) addProblem(err error) {
	proxy.mu.Lock()
	proxy.problems = append(proxy.problems, err)
	proxy.mu.Unlock()
}

// Authentication and all other messages are forwarded as received. Only the
// Parse, Query and CommandComplete bodies need decoding for the fault boundary.
func (proxy *commitLossProxy) forwardClient(client, upstream net.Conn, state *commitLossConnection) {
	for {
		frame, err := commitLossReadMessage(client)
		if err != nil {
			return
		}
		var sql string
		switch frame[0] {
		case 'P':
			var message pgproto3.Parse
			if err := message.Decode(frame[5:]); err != nil {
				proxy.addProblem(fmt.Errorf("decode Parse: %w", err))
				return
			}
			sql = message.Query
		case 'Q':
			var message pgproto3.Query
			if err := message.Decode(frame[5:]); err != nil {
				proxy.addProblem(fmt.Errorf("decode Query: %w", err))
				return
			}
			sql = message.String
		}
		kind := ""
		for _, candidate := range []string{"record_write_register", "record_write_dispatch", "record_write_temporary_created", "record_write_return", "record_write_resolve"} {
			if strings.Contains(sql, candidate) {
				kind = candidate
				break
			}
		}
		if kind != "" {
			proxy.mu.Lock()
			proxy.queries[kind] = append(proxy.queries[kind], state.id)
			deny := kind == "record_write_resolve" && proxy.rejectResolve && proxy.dropped
			if deny {
				proxy.resolveDenied = append(proxy.resolveDenied, state.id)
			}
			proxy.mu.Unlock()
			if deny {
				// No other operation is denied; this connection is closed before
				// the resolve statement is sent to PostgreSQL.
				return
			}
			if kind == proxy.target {
				state.mu.Lock()
				state.target = true
				state.mu.Unlock()
			}
		}
		if err := commitLossWrite(upstream, frame); err != nil {
			return
		}
	}
}

func (proxy *commitLossProxy) forwardServer(upstream, client net.Conn, state *commitLossConnection) {
	for {
		frame, err := commitLossReadMessage(upstream)
		if err != nil {
			return
		}
		if frame[0] == 'C' {
			var message pgproto3.CommandComplete
			if err := message.Decode(frame[5:]); err != nil {
				proxy.addProblem(fmt.Errorf("decode CommandComplete: %w", err))
				return
			}
			tag := string(message.CommandTag)
			if tag == "COMMIT" || tag == "ROLLBACK" {
				state.mu.Lock()
				selected := state.target
				state.target = false
				state.mu.Unlock()
				if tag == "COMMIT" && selected {
					proxy.mu.Lock()
					drop := !proxy.dropped
					if drop {
						proxy.dropped = true
						proxy.droppedID = state.id
					}
					proxy.mu.Unlock()
					if drop {
						// The real server committed; do not forward this ACK.
						return
					}
				}
			}
		}
		if err := commitLossWrite(client, frame); err != nil {
			return
		}
	}
}

func (proxy *commitLossProxy) snapshot() commitLossSnapshot {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	snapshot := commitLossSnapshot{
		dropped: proxy.dropped, droppedID: proxy.droppedID,
		queries:       make(map[string][]uint64, len(proxy.queries)),
		resolveDenied: append([]uint64(nil), proxy.resolveDenied...),
	}
	for kind, ids := range proxy.queries {
		snapshot.queries[kind] = append([]uint64(nil), ids...)
	}
	return snapshot
}

func commitLossReadStartup(reader io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 64<<20 {
		return nil, errors.New("invalid PostgreSQL startup frame length")
	}
	frame := make([]byte, int(size))
	copy(frame, header[:])
	_, err := io.ReadFull(reader, frame[4:])
	return frame, err
}

func commitLossReadMessage(reader io.Reader) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 64<<20 {
		return nil, errors.New("invalid PostgreSQL message frame length")
	}
	frame := make([]byte, int(size)+1)
	copy(frame, header[:])
	_, err := io.ReadFull(reader, frame[5:])
	return frame, err
}

func commitLossWrite(writer io.Writer, data []byte) error {
	for len(data) != 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

type commitLossQuota struct {
	exists   bool
	logical  int64
	reserved int64
	physical int64
	version  int64
}

func readCommitLossQuota(t *testing.T, ctx context.Context, runtime *pgxpool.Pool) commitLossQuota {
	t.Helper()
	var quota commitLossQuota
	if err := runtime.QueryRow(ctx, `select
		exists(select 1 from public.attachment_quota_accounts where project_id='default'),
		coalesce((select logical_bytes from public.attachment_quota_accounts where project_id='default'),0),
		coalesce((select reserved_bytes from public.attachment_quota_accounts where project_id='default'),0),
		coalesce((select physical_bytes from public.attachment_quota_accounts where project_id='default'),0),
		coalesce((select quota_version from public.attachment_quota_accounts where project_id='default'),0)`).Scan(
		&quota.exists, &quota.logical, &quota.reserved, &quota.physical, &quota.version); err != nil {
		t.Fatalf("read attachment quota: %v", err)
	}
	return quota
}

func TestPostgresIntegrationLocalWriteCommitLoss(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	scenarios := []struct {
		name          string
		target        string
		rejectResolve bool
	}{
		{"dispatch_ack_lost_resolved", "record_write_dispatch", false},
		{"dispatch_ack_lost_resolve_unavailable", "record_write_dispatch", true},
		{"return_ack_lost_resolved", "record_write_return", false},
		{"return_ack_lost_resolve_unavailable", "record_write_return", true},
	}
	for index, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			scenarioCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			fixture := store.NewImportDestinationFixture(t, scenarioCtx)
			provisioned, ok := fixture.Blobs.(*attachments.LocalBlobStore)
			if !ok {
				t.Fatal("import fixture did not provision a local Blob store")
			}
			root := provisioned.Namespace().RootPath
			var fs unix.Statfs_t
			if err := unix.Statfs(root, &fs); err != nil {
				t.Fatalf("stat Blob root filesystem: %v", err)
			}
			if fs.Type != unix.BTRFS_SUPER_MAGIC {
				t.Fatalf("actual Blob root filesystem type=%x, want btrfs", fs.Type)
			}
			var expectedRole, directCurrent, expectedDatabase string
			var constrained bool
			if err := fixture.Runtime.QueryRow(scenarioCtx, `select session_user,current_user,current_database(),
				rolcanlogin and not rolsuper and not rolcreaterole and not rolcreatedb and not rolreplication and not rolbypassrls and not rolinherit
				from pg_catalog.pg_roles where rolname=session_user`).Scan(&expectedRole, &directCurrent, &expectedDatabase, &constrained); err != nil {
				t.Fatalf("read fixture runtime identity: %v", err)
			}
			if expectedRole != directCurrent || !constrained {
				t.Fatal("fixture is not a constrained direct runtime login")
			}
			// The exported fixture's direct runtime pool comes from directRoleConfig.
			directConfig := fixture.Runtime.Config().Copy()
			config := directConfig.Copy()
			if config.ConnConfig.TLSConfig != nil {
				t.Fatal("commit-loss wire proxy requires the integration runner's unencrypted PostgreSQL connection")
			}
			network, address := pgconn.NetworkAddress(config.ConnConfig.Host, config.ConnConfig.Port)
			proxy := newCommitLossProxy(t, scenarioCtx, network, address, scenario.target, scenario.rejectResolve)
			port := proxy.listener.Addr().(*net.TCPAddr).Port
			config.ConnConfig.Host = "127.0.0.1"
			config.ConnConfig.Port = uint16(port)
			config.ConnConfig.TLSConfig = nil
			config.ConnConfig.Fallbacks = nil
			config.MaxConns = 1
			config.MinConns = 0
			pool, err := pgxpool.NewWithConfig(scenarioCtx, config)
			if err != nil {
				t.Fatalf("open proxied direct runtime pool: %v", err)
			}
			t.Cleanup(pool.Close)
			var sessionUser, currentUser, database string
			if err := pool.QueryRow(scenarioCtx, `select session_user, current_user, current_database()`).Scan(&sessionUser, &currentUser, &database); err != nil {
				t.Fatalf("read proxied runtime identity: %v", err)
			}
			if sessionUser != expectedRole || currentUser != expectedRole || database != expectedDatabase {
				t.Fatal("proxied connection did not use the fixture database and direct runtime role")
			}
			witness, err := store.NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, pool)
			if err != nil {
				t.Fatal(err)
			}
			repository := store.NewPostgresLocalBlobRepository(pool, witness, nil)
			blob, err := attachments.NewLocalBlobStore(scenarioCtx, root, repository)
			if err != nil {
				t.Fatalf("open admitted local Blob store through proxy: %v", err)
			}
			t.Cleanup(func() {
				if err := blob.Close(); err != nil {
					t.Errorf("close proxied local Blob store: %v", err)
				}
			})
			writer, err := attachments.NewManagedBlobWriter(blob, attachments.BackendKindLocal, repository)
			if err != nil {
				t.Fatalf("create managed local writer: %v", err)
			}

			// Use the same real import-archive origin as the local Blob fixture,
			// keeping the job alive so its lifecycle can be checked after Put.
			content := []byte("real local commit loss " + scenario.name)
			digest := sha256.Sum256(content)
			digestText := hex.EncodeToString(digest[:])
			key := "sha256/" + digestText
			jobID := fmt.Sprintf("rij_commitloss%d", index)
			if _, err := fixture.Runtime.Exec(scenarioCtx, `insert into public.record_import_jobs(import_job_id,actor_id,idempotency_key,job_state,
				identity_classification,archive_digest,destination_subject_kind,destination_subject_source_id,expires_at)
				values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'uploaded','unknown',$2,'vps','vps_0123456789abcdef',clock_timestamp()+interval '1 hour')`, jobID, digest[:]); err != nil {
				t.Fatalf("create import archive origin: %v", err)
			}
			// Ranked admission ensures this account before an uncharged write.
			if _, err := fixture.Runtime.Exec(scenarioCtx, `
				insert into public.attachment_quota_accounts (project_id) values ('default')
				on conflict (project_id) do nothing`); err != nil {
				t.Fatalf("establish attachment quota baseline: %v", err)
			}
			quotaBefore := readCommitLossQuota(t, scenarioCtx, fixture.Runtime)
			if !quotaBefore.exists {
				t.Fatal("attachment quota baseline is missing")
			}
			staging := portability.NewLeasedBlobStore(blob, writer)
			written, putErr := staging.StageImport(scenarioCtx, attachments.WriteRequest{
				ProjectID: "default",
				Origin:    attachments.WriteOrigin{Kind: "import_archive", ID: jobID, Generation: 1, ArchiveDigest: &digestText},
			}, content)
			if scenario.rejectResolve {
				if !errors.Is(putErr, attachments.ErrWriteOutcomeUnknown) {
					t.Fatalf("Put error = %v, want ErrWriteOutcomeUnknown", putErr)
				}
				if written != (attachments.WrittenBlob{}) {
					t.Fatal("unknown write exposed a publishable result")
				}
			} else if putErr != nil {
				t.Fatalf("Put with successful readback: %v", putErr)
			}
			lease, leasedVersion, leaseErr := staging.OpenLeased(scenarioCtx, jobID)
			if scenario.rejectResolve {
				if !errors.Is(leaseErr, portability.ErrExportNotFound) || lease != nil {
					t.Fatalf("unknown write exposed an import staging lease: %v", leaseErr)
				}
			} else {
				if leaseErr != nil {
					t.Fatalf("open successfully staged archive: %v", leaseErr)
				}
				leaseBytes, readErr := io.ReadAll(lease)
				closeErr := lease.Close()
				if readErr != nil || closeErr != nil || !bytes.Equal(leaseBytes, content) || leasedVersion != written.Version {
					t.Fatalf("staged archive differs from returned object: read=%v close=%v", readErr, closeErr)
				}
			}

			observed := proxy.snapshot()
			if !observed.dropped || observed.droppedID == 0 {
				t.Fatal("proxy did not observe and suppress a committed transaction ACK")
			}
			if ids := observed.queries[scenario.target]; len(ids) != 1 || ids[0] != observed.droppedID {
				t.Fatalf("selected transaction attempts = %v, dropped connection = %d", ids, observed.droppedID)
			}
			resolveIDs := observed.queries["record_write_resolve"]
			if len(resolveIDs) != 1 || resolveIDs[0] == observed.droppedID {
				t.Fatalf("resolve connections = %v, committed connection = %d; want one fresh connection", resolveIDs, observed.droppedID)
			}
			if scenario.rejectResolve {
				if len(observed.resolveDenied) != 1 || observed.resolveDenied[0] != resolveIDs[0] {
					t.Fatal("the fresh resolve was not the specifically rejected operation")
				}
			} else if len(observed.resolveDenied) != 0 {
				t.Fatal("successful readback was rejected by the proxy")
			}
			// The armed fault must leave other direct-runtime statements usable.
			if err := pool.QueryRow(scenarioCtx, `select session_user, current_user, current_database()`).Scan(&sessionUser, &currentUser, &database); err != nil {
				t.Fatalf("unrelated query through armed proxy: %v", err)
			}
			if sessionUser != expectedRole || currentUser != expectedRole || database != expectedDatabase {
				t.Fatal("post-fault proxied connection changed database or runtime identity")
			}

			var obligationID, writerInstance, originKind, originID, principal string
			var requestSeq, originGeneration int64
			var obligationCount, keyCount int
			if err := fixture.Runtime.QueryRow(scenarioCtx, `select
				(select count(*) from public.record_write_obligations where origin_id=$1),
				(select count(*) from public.record_write_obligations where canonical_key=$2)`, jobID, key).Scan(&obligationCount, &keyCount); err != nil {
				t.Fatalf("count durable write attempts: %v", err)
			}
			if obligationCount != 1 || keyCount != 1 {
				t.Fatalf("write attempts by origin/key = %d/%d, want one", obligationCount, keyCount)
			}
			if err := fixture.Runtime.QueryRow(scenarioCtx, `select obligation_id,writer_instance::text,request_seq,origin_kind,origin_id,origin_generation,writer_principal::text
				from public.record_write_obligations where origin_id=$1`, jobID).Scan(
				&obligationID, &writerInstance, &requestSeq, &originKind, &originID, &originGeneration, &principal); err != nil {
				t.Fatalf("read durable write identity: %v", err)
			}
			if obligationID == "" || writerInstance == "" || requestSeq != 1 || originKind != "import_archive" || originID != jobID || originGeneration != 1 || principal != expectedRole {
				t.Fatal("durable obligation has an unexpected writer or origin identity")
			}
			// This independently opened direct-role pool bypasses the test fault.
			directConfig.MaxConns = 1
			directConfig.MinConns = 0
			direct, err := pgxpool.NewWithConfig(scenarioCtx, directConfig)
			if err != nil {
				t.Fatalf("open fresh direct runtime readback pool: %v", err)
			}
			t.Cleanup(direct.Close)
			resolved, err := store.NewPostgresLocalBlobRepository(direct, nil, nil).ResolveWrite(scenarioCtx, attachments.WriteIdentity{
				ObligationID: obligationID, WriterInstance: writerInstance,
			})
			if err != nil {
				t.Fatalf("authoritative fresh direct-role readback: %v", err)
			}
			namespace := blob.Namespace()
			if resolved.ID != obligationID || resolved.WriterInstance != writerInstance || resolved.RequestSeq != 1 || resolved.ProjectID != "default" ||
				resolved.BackendKind != attachments.BackendKindLocal || resolved.NamespaceID == nil || *resolved.NamespaceID != namespace.NamespaceID ||
				resolved.IncarnationID == nil || *resolved.IncarnationID != namespace.IncarnationID || resolved.Key != key ||
				resolved.SHA256 != digestText || resolved.Size != int64(len(content)) || resolved.TemporaryKey == nil ||
				*resolved.TemporaryKey != "sha256/.blob-tmp-"+obligationID || resolved.DispatchID == nil || *resolved.DispatchID == "" || resolved.DispatchedAt == nil ||
				resolved.HandedOffAt != nil || resolved.CleanupOwnerID != nil || resolved.CleanupCompletedAt != nil {
				t.Fatal("readback did not preserve the exact registered and dispatched identity")
			}
			if quotaAfter := readCommitLossQuota(t, scenarioCtx, fixture.Runtime); quotaAfter != quotaBefore {
				t.Fatalf("write changed quota: before=%+v after=%+v", quotaBefore, quotaAfter)
			}
			var jobState string
			var jobGeneration int64
			var jobDigest []byte
			var artifacts, metadata int
			var unpublished bool
			if err := fixture.Runtime.QueryRow(scenarioCtx, `select j.job_state,j.lock_version,j.archive_digest,
				(select count(*) from public.record_import_artifacts a where a.import_job_id=j.import_job_id),
				(select count(*) from public.blob_objects b where b.blob_key=$2),
				o.published_at is null
				from public.record_import_jobs j join public.record_write_obligations o on o.origin_id=j.import_job_id
				where j.import_job_id=$1 and o.obligation_id=$3`, jobID, key, obligationID).Scan(
				&jobState, &jobGeneration, &jobDigest, &artifacts, &metadata, &unpublished); err != nil {
				t.Fatalf("read origin and publication state: %v", err)
			}
			if jobState != "uploaded" || jobGeneration != 1 || !bytes.Equal(jobDigest, digest[:]) || artifacts != 0 || metadata != 0 || !unpublished {
				t.Fatal("write changed the archive origin or published an artifact/metadata")
			}

			materialized := scenario.target == "record_write_return" || !scenario.rejectResolve
			wantTemp := scenario.target == "record_write_return" && scenario.rejectResolve
			var obligationArchive []byte
			var standalone, registrationMatches, temporaryRecorded, returnRecorded, returnMatches bool
			if err := fixture.Runtime.QueryRow(scenarioCtx, `select archive_digest,
				record_id is null and record_epoch is null and publication_id is null and operation_id is null and quota_settled_at is null,
				coalesce((convert_from(registration_command,'UTF8')::jsonb->>'obligation_id')=obligation_id
					and (convert_from(registration_command,'UTF8')::jsonb->>'writer_instance')=writer_instance::text
					and (convert_from(registration_command,'UTF8')::jsonb->>'request_seq')=request_seq::text
					and (convert_from(registration_command,'UTF8')::jsonb->>'origin_id')=origin_id
					and (convert_from(registration_command,'UTF8')::jsonb->>'origin_kind')=origin_kind
					and (convert_from(registration_command,'UTF8')::jsonb->>'origin_generation')=origin_generation::text
					and (convert_from(registration_command,'UTF8')::jsonb->>'namespace_id')=namespace_id::text
					and (convert_from(registration_command,'UTF8')::jsonb->>'incarnation_id')=incarnation_id::text
					and (convert_from(registration_command,'UTF8')::jsonb->>'archive_digest')=encode(archive_digest,'hex')
					and (convert_from(registration_command,'UTF8')::jsonb->>'key')=canonical_key
					and (convert_from(registration_command,'UTF8')::jsonb->>'sha256')=encode(sha256_digest,'hex')
					and (convert_from(registration_command,'UTF8')::jsonb->>'size')=size_bytes::text,false),
				temporary_created_at is not null,return_command is not null,
				coalesce((convert_from(return_command,'UTF8')::jsonb->>'obligation_id')=obligation_id
					and (convert_from(return_command,'UTF8')::jsonb->>'writer_instance')=writer_instance::text
					and (convert_from(return_command,'UTF8')::jsonb->>'dispatch_id')=dispatch_id::text
					and (convert_from(return_command,'UTF8')::jsonb->>'outcome')=outcome
					and (convert_from(return_command,'UTF8')::jsonb->>'version')=object_version
					and (convert_from(return_command,'UTF8')::jsonb->>'key')=canonical_key
					and (convert_from(return_command,'UTF8')::jsonb->>'sha256')=encode(sha256_digest,'hex')
					and (convert_from(return_command,'UTF8')::jsonb->>'size')=size_bytes::text,false)
				from public.record_write_obligations where obligation_id=$1`, obligationID).Scan(
				&obligationArchive, &standalone, &registrationMatches, &temporaryRecorded, &returnRecorded, &returnMatches); err != nil {
				t.Fatalf("read exact registration and returned facts: %v", err)
			}
			if !bytes.Equal(obligationArchive, digest[:]) || !standalone || !registrationMatches ||
				temporaryRecorded != materialized || returnRecorded != materialized || returnMatches != materialized {
				t.Fatal("durable registration, temporary creation, or return facts have an unexpected identity")
			}
			if len(observed.queries["record_write_register"]) != 1 || len(observed.queries["record_write_dispatch"]) != 1 {
				t.Fatal("write repeated registration or dispatch")
			}
			wantPhysicalCalls := 0
			if materialized {
				wantPhysicalCalls = 1
			}
			if len(observed.queries["record_write_temporary_created"]) != wantPhysicalCalls || len(observed.queries["record_write_return"]) != wantPhysicalCalls {
				t.Fatal("write performed an unexpected number of physical creation/return operations")
			}
			digestDir := filepath.Join(root, "sha256")
			entries, err := os.ReadDir(digestDir)
			if err != nil {
				t.Fatalf("inspect actual Blob directory: %v", err)
			}
			wantNames := make(map[string]bool)
			if materialized {
				wantNames[digestText] = true
			}
			if wantTemp {
				wantNames[".blob-tmp-"+obligationID] = true
			}
			if len(entries) != len(wantNames) {
				t.Fatalf("Blob directory has %d entries, want %d", len(entries), len(wantNames))
			}
			for _, entry := range entries {
				if !wantNames[entry.Name()] || !entry.Type().IsRegular() {
					t.Fatalf("unexpected physical Blob entry %q", entry.Name())
				}
			}
			finalPath := filepath.Join(root, key)
			tempPath := filepath.Join(root, *resolved.TemporaryKey)
			if !materialized {
				if resolved.Outcome != "registered" || resolved.ReturnedAt != nil || resolved.Version != nil || resolved.TemporaryDevice != nil || resolved.TemporaryInode != nil {
					t.Fatal("unresolved dispatch recorded a physical return")
				}
				for _, path := range []string{tempPath, finalPath} {
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("unconfirmed dispatch left a named physical object: %v", err)
					}
				}
				return
			}

			if resolved.Outcome != "materialized" || resolved.ReturnedAt == nil || resolved.Version == nil ||
				*resolved.Version != "local-v2-"+obligationID || resolved.TemporaryDevice == nil || resolved.TemporaryInode == nil {
				t.Fatal("materialized write lost its exact returned fact or physical identity")
			}
			actual, err := os.ReadFile(finalPath)
			if err != nil || !bytes.Equal(actual, content) {
				t.Fatalf("final Blob bytes differ: %v", err)
			}
			var stat unix.Stat_t
			if err := unix.Lstat(finalPath, &stat); err != nil || uint64(stat.Dev) != *resolved.TemporaryDevice || stat.Ino != *resolved.TemporaryInode {
				t.Fatalf("final Blob is not the recorded temporary inode: %v", err)
			}
			version := attachments.ObjectVersion{Key: key, VersionID: *resolved.Version, SHA256: digest, SizeBytes: int64(len(content))}
			if _, err := blob.Stat(scenarioCtx, version); err != nil {
				t.Fatalf("verify exact materialized Blob version: %v", err)
			}
			if wantTemp {
				tempBytes, err := os.ReadFile(tempPath)
				if err != nil || !bytes.Equal(tempBytes, content) {
					t.Fatalf("unknown return did not retain the exact temporary bytes: %v", err)
				}
				tempInfo, tempErr := os.Stat(tempPath)
				finalInfo, finalErr := os.Stat(finalPath)
				if tempErr != nil || finalErr != nil || !os.SameFile(tempInfo, finalInfo) {
					t.Fatalf("unknown return did not retain both names for one inode: %v / %v", tempErr, finalErr)
				}
			} else {
				if _, err := os.Lstat(tempPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("successful write retained its temporary name: %v", err)
				}
				if written.ObligationID != obligationID || written.NamespaceID != namespace.NamespaceID || written.Version != version {
					t.Fatal("successful Put returned a different object, version, namespace, or obligation")
				}
			}
		})
	}
}
