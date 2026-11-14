package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

type v1AuthorityFixture struct {
	ctx      context.Context
	db       recordPlatformPostgresFixture
	worker   *pgxpool.Pool
	client   *minio.Client
	bucket   string
	blob     *attachments.S3BlobStore
	object   attachments.ObjectVersion
	current  string
	neighbor string
	target   recorddeletion.PurgeTarget
}

func newV1AuthorityFixture(t *testing.T) *v1AuthorityFixture {
	t.Helper()
	ctx := context.Background()
	f := &v1AuthorityFixture{ctx: ctx}
	f.db = newAdmittedCoreRecordFixture(t, ctx)
	runtime := f.db.openDirectRuntimePool(t, ctx, "v1-authority-fixture", 2)
	records := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtime, Gate: f.db.admissionGate})
	record, err := records.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtime, Gate: f.db.admissionGate}, recordsPostgresCompleteRevisionInput(t, "V1 authority regression"), "v1-authority"))
	if err != nil {
		t.Fatal(err)
	}
	f.client, f.bucket = newAttachmentUploadWorkflowMinIO(t)
	f.blob, err = attachments.NewS3BlobStore(f.client, f.bucket)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("V1 exact historical object bytes\n")
	digest := sha256.Sum256(content)
	temp := sha256.Sum256([]byte(t.Name()))
	f.object, err = f.blob.Put(ctx, attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content)), TemporaryKey: fmt.Sprintf("temporary/%x", temp)}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.client.PutObject(ctx, f.bucket, f.object.Key, bytes.NewReader([]byte("new current")), 11, minio.PutObjectOptions{DisableMultipart: true})
	if err != nil || current.VersionID == "" || current.VersionID == f.object.VersionID {
		t.Fatalf("create current version: %v", err)
	}
	f.current = current.VersionID
	f.neighbor = "neighbor/v1-authority"
	if _, err = f.client.PutObject(ctx, f.bucket, f.neighbor, strings.NewReader("neighbor bytes"), 14, minio.PutObjectOptions{DisableMultipart: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.db.Exec(ctx, `insert into public.blob_objects (blob_key,sha256_digest,object_version,size_bytes,backend_kind) values ($1,$2,$3,$4,'s3')`, f.object.Key, f.object.SHA256[:], f.object.VersionID, f.object.SizeBytes); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.db.Exec(ctx, `insert into public.record_attachments (attachment_id,project_id,record_id,attachment_state,display_name,media_type,logical_size_bytes,blob_key,blob_object_version,created_by) values ('att_v1authority','default',$1,'available','v1.txt','text/plain',$2,$3,$4,'usr_records1')`, record.RecordID, f.object.SizeBytes, f.object.Key, f.object.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.db.Exec(ctx, `insert into public.record_revision_attachments (record_id,revision_id,ordinal,attachment_id) values ($1,$2,0,'att_v1authority')`, record.RecordID, record.RevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.db.Exec(ctx, `insert into public.attachment_quota_accounts (project_id,logical_bytes,reserved_bytes,physical_bytes) values ('default',$1,0,$1)`, f.object.SizeBytes); err != nil {
		t.Fatal(err)
	}
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_v1authority", ReservationID: "drs_v1authority", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12, LedgerSequence: 16, LedgerEntryHash: testStoreRecordPlatformDigest(0xa4)}
	f.target = seedVerifiedS3AttachmentDeletionOperation(t, ctx, f.db, record, op)
	f.worker = f.db.openDirectRolePool(t, ctx, f.db.deletionWorker, "v1-authority-worker", 2)
	return f
}

func (f *v1AuthorityFixture) adapter(t *testing.T, deleter attachments.BlobGCDeleter) (*PostgresAttachmentRepository, *attachments.DeletionAdapter) {
	t.Helper()
	repo := NewPostgresAttachmentRepository(f.worker)
	if err := repo.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindS3, deleter); err != nil {
		t.Fatal(err)
	}
	adapter, err := attachments.NewDeletionAdapter(repo)
	if err != nil {
		t.Fatal(err)
	}
	return repo, adapter
}

func (f *v1AuthorityFixture) survivors(t *testing.T, deleted bool) {
	t.Helper()
	head, err := f.client.StatObject(f.ctx, f.bucket, f.object.Key, minio.StatObjectOptions{VersionID: f.object.VersionID})
	if response := minio.ToErrorResponse(err); deleted && !(response.StatusCode == http.StatusNotFound && (response.Code == "NoSuchKey" || response.Code == "NoSuchVersion" || response.Code == "NotFound")) {
		t.Fatalf("historical version still present: %v", err)
	}
	if !deleted {
		if err != nil || head.VersionID != f.object.VersionID || head.Size != f.object.SizeBytes {
			t.Fatalf("historical exact version changed: version=%q size=%d error=%v", head.VersionID, head.Size, err)
		}
		object, openErr := f.client.GetObject(f.ctx, f.bucket, f.object.Key, minio.GetObjectOptions{VersionID: f.object.VersionID})
		if openErr != nil {
			t.Fatal(openErr)
		}
		content, readErr := io.ReadAll(object)
		closeErr := object.Close()
		if readErr != nil || closeErr != nil || int64(len(content)) != f.object.SizeBytes || sha256.Sum256(content) != f.object.SHA256 {
			t.Fatalf("historical exact bytes changed: read=%v close=%v", readErr, closeErr)
		}
	}
	for _, x := range []struct{ key, version, want string }{{f.object.Key, f.current, "new current"}, {f.neighbor, "", "neighbor bytes"}} {
		obj, err := f.client.GetObject(f.ctx, f.bucket, x.key, minio.GetObjectOptions{VersionID: x.version})
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(obj)
		_ = obj.Close()
		if err != nil || string(got) != x.want {
			t.Fatalf("survivor %s: %q %v", x.key, got, err)
		}
	}
}

type v1GCFacts struct {
	id, state, owner                     string
	expires                              time.Time
	generation, attempt, physical, quota int64
	result                               *string
	receipts, claims                     int
	proof                                bool
}

func (f *v1AuthorityFixture) facts(t *testing.T) v1GCFacts {
	t.Helper()
	var x v1GCFacts
	err := f.db.db.QueryRow(f.ctx, `select g.deletion_id,g.deletion_state,g.owner_id,g.lease_expires_at,g.owner_generation,g.attempt,q.physical_bytes,q.quota_version,g.physical_delete_result,(select count(*)::int from public.attachment_purge_receipts r where r.operation_id=g.operation_id),(select count(*)::int from public.blob_gc_deletions a where a.operation_id=g.operation_id),(g.receipt_digest is not null or g.completed_at is not null) from public.blob_gc_deletions g join public.attachment_quota_accounts q on q.project_id=g.project_id where g.operation_id=$1 and g.blob_key=$2 and g.object_version=$3`, f.target.Operation.OperationID, f.object.Key, f.object.VersionID).Scan(&x.id, &x.state, &x.owner, &x.expires, &x.generation, &x.attempt, &x.physical, &x.quota, &x.result, &x.receipts, &x.claims, &x.proof)
	if err != nil {
		t.Fatal(err)
	}
	if x.claims != 1 || (x.state != "completed" && x.proof) {
		t.Fatalf("invalid GC claims or premature receipt: %+v", x)
	}
	return x
}
func (f *v1AuthorityFixture) expireGC(t *testing.T) {
	t.Helper()
	tag, err := f.db.db.Exec(f.ctx, `update public.blob_gc_deletions set lease_expires_at=clock_timestamp()-interval '1 second',retry_at=case when retry_at is null then null else clock_timestamp()-interval '1 second' end where operation_id=$1`, f.target.Operation.OperationID)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("expire isolated GC fixture: %v rows=%d", err, tag.RowsAffected())
	}
}

type v1LostACK struct {
	pgx.Tx
	query               string
	selected, resolving bool
	lost                *bool
	resolutions         *int
	observe             func(bool)
}

func (tx *v1LostACK) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if query == tx.query && !*tx.lost {
		tx.selected = true
	} else if *tx.lost && ((strings.Contains(tx.query, "purge_prepare") && strings.Contains(query, "purge_verify")) || (strings.Contains(tx.query, "gc_claim") && query == tx.query)) {
		tx.resolving = true
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}
func (tx *v1LostACK) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	if tx.selected {
		tx.observe(false)
		*tx.lost = true
		return errors.New("lost committed acknowledgement")
	}
	if tx.resolving {
		tx.observe(true)
		*tx.resolutions++
	}
	return nil
}

func TestPostgresMinIOIntegrationAttachmentV1CommitUnknown(t *testing.T) {
	for _, stage := range []string{"prepare", "claim"} {
		t.Run(stage, func(t *testing.T) {
			f := newV1AuthorityFixture(t)
			lost := false
			resolved := 0
			calls := 0
			var original, readback v1GCFacts
			observer := &attachmentDeletionObservingBlobStore{BlobGCDeleter: f.blob, beforeDelete: func(version attachments.ObjectVersion) error {
				calls++
				if !lost || resolved == 0 || version != f.object || original.id != readback.id || original.owner != readback.owner || original.generation != readback.generation || original.attempt != readback.attempt || !original.expires.Equal(readback.expires) {
					return errors.New("delete preceded exact committed identity resolution")
				}
				var n int
				if err := f.db.db.QueryRow(f.ctx, `select count(*)::int from public.blob_gc_deletions where operation_id=$1 and blob_key=$2 and object_version=$3 and deletion_state='claimed'`, f.target.Operation.OperationID, version.Key, version.VersionID).Scan(&n); err != nil || n != 1 {
					return fmt.Errorf("missing durable claim: %d %v", n, err)
				}
				return nil
			}}
			repo, adapter := f.adapter(t, observer)
			query := `select public.record_attachment_purge_prepare_v1($1)`
			if stage == "claim" {
				query = `select public.record_attachment_gc_claim_v1($1)`
			}
			repo.beginTx = func(ctx context.Context, options pgx.TxOptions) (attachmentTx, error) {
				tx, err := f.worker.BeginTx(ctx, options)
				if err != nil {
					return nil, err
				}
				return &v1LostACK{Tx: tx, query: query, lost: &lost, resolutions: &resolved, observe: func(resolution bool) {
					if resolution {
						readback = f.facts(t)
					} else {
						original = f.facts(t)
					}
				}}, nil
			}
			receipt, err := adapter.PurgeDeletion(f.ctx, f.target)
			if err != nil {
				t.Fatal(err)
			}
			if !lost || resolved == 0 || calls != 1 {
				t.Fatalf("lost=%t resolved=%d physical calls=%d", lost, resolved, calls)
			}
			if replay, err := adapter.PurgeDeletion(f.ctx, f.target); err != nil || replay != receipt {
				t.Fatalf("replay: %v", err)
			}
			facts := f.facts(t)
			if calls != 1 || facts.state != "completed" || facts.physical != 0 || facts.quota != 2 || facts.receipts != 3 {
				t.Fatalf("replay facts %+v calls=%d", facts, calls)
			}
			f.survivors(t, true)
		})
	}
}

// The client signs requests for the original endpoint. Only the TCP dial is
// redirected; the proxy forwards the signed Host, path and query unchanged.
func TestPostgresMinIOIntegrationAttachmentV1NetworkFailureRetry(t *testing.T) {
	f := newV1AuthorityFixture(t)
	if secure := os.Getenv("HOUFENG_MINIO_SECURE"); secure != "" {
		value, err := strconv.ParseBool(secure)
		if err != nil {
			t.Fatal(err)
		}
		if value {
			t.Fatal("this isolated HTTP proxy requires the integration profile's HTTP MinIO endpoint")
		}
	}
	var dropped atomic.Int32
	upstream := http.DefaultTransport.(*http.Transport).Clone()
	upstream.Proxy = nil
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := r.Clone(r.Context())
		req.RequestURI = ""
		req.URL.Scheme = "http"
		req.URL.Host = os.Getenv("HOUFENG_MINIO_ENDPOINT")
		resp, err := upstream.RoundTrip(req)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if r.Method == http.MethodDelete && r.URL.Path == "/"+f.bucket+"/"+f.object.Key && r.URL.Query().Get("versionId") == f.object.VersionID {
			_, _ = io.Copy(io.Discard, resp.Body)
			dropped.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer server.Close()
	defer upstream.CloseIdleConnections()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
	}
	defer transport.CloseIdleConnections()
	client, err := minio.New(os.Getenv("HOUFENG_MINIO_ENDPOINT"), &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("HOUFENG_MINIO_ACCESS_KEY"), os.Getenv("HOUFENG_MINIO_SECRET_KEY"), ""), Secure: false, Transport: transport, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	faultStore, err := attachments.NewS3BlobStore(client, f.bucket)
	if err != nil {
		t.Fatal(err)
	}
	_, adapter := f.adapter(t, faultStore)
	ctx, cancel := context.WithTimeout(f.ctx, 20*time.Second)
	defer cancel()
	if _, err = adapter.PurgeDeletion(ctx, f.target); err == nil {
		t.Fatal("lost real DELETE response accepted as a completed purge")
	}
	if dropped.Load() == 0 {
		t.Fatal("proxy did not observe an exact version DELETE")
	}
	before := f.facts(t)
	if before.state != "retry_wait" || before.physical != f.object.SizeBytes || before.result != nil || before.quota != 1 {
		t.Fatalf("failed network deletion settled quota: %+v", before)
	}
	f.survivors(t, true)
	f.expireGC(t)
	_, adapter = f.adapter(t, f.blob)
	receipt, err := adapter.PurgeDeletion(f.ctx, f.target)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := adapter.PurgeDeletion(f.ctx, f.target); err != nil || replay != receipt {
		t.Fatalf("retry replay: %v", err)
	}
	after := f.facts(t)
	if after.id != before.id || after.generation != before.generation+1 || after.attempt != before.attempt+1 || after.state != "completed" || after.physical != 0 || after.quota != 2 || after.result == nil || *after.result != "already_absent" || after.receipts != 3 {
		t.Fatalf("retry did not settle same claim exactly once: before=%+v after=%+v", before, after)
	}
	f.survivors(t, true)
}

type v1ChildConfig struct {
	DatabaseConfig string
	Database       string
	Worker         string
	Password       string
	Endpoint       string
	AccessKey      string
	SecretKey      string
	Secure         bool
	Bucket         string
	Target         recorddeletion.PurgeTarget
}
type v1AfterDeleteBarrier struct {
	attachments.BlobGCDeleter
	event   *os.File
	control *os.File
}

func (b *v1AfterDeleteBarrier) DeleteGCVersion(ctx context.Context, version attachments.ObjectVersion) (attachments.DeletionReceipt, error) {
	receipt, err := b.BlobGCDeleter.DeleteGCVersion(ctx, version)
	if err != nil {
		return receipt, err
	}
	if !receipt.Deleted || receipt.Version != version {
		return receipt, errors.New("real exact deletion did not return a deleted receipt")
	}
	if _, err = b.event.Write([]byte{1}); err != nil {
		return receipt, err
	}
	var signal [1]byte
	_, err = io.ReadFull(b.control, signal[:])
	return receipt, err
}

func TestPostgresMinIOIntegrationAttachmentV1ProcessKillRecovery(t *testing.T) {
	if os.Getenv("HOUFENG_V1_PROCESS_HELPER") == "1" {
		input := os.NewFile(3, "private-config")
		event := os.NewFile(4, "event")
		control := os.NewFile(5, "control")
		defer input.Close()
		defer event.Close()
		defer control.Close()
		var c v1ChildConfig
		if err := json.NewDecoder(io.LimitReader(input, 65536)).Decode(&c); err != nil {
			t.Fatal("invalid private child config")
		}
		config, err := pgxpool.ParseConfig(c.DatabaseConfig)
		if err != nil {
			t.Fatal("invalid child database config")
		}
		config.ConnConfig.User = c.Worker
		config.ConnConfig.Password = c.Password
		config.ConnConfig.Database = c.Database
		config.AfterConnect = nil
		pool, err := pgxpool.NewWithConfig(context.Background(), config)
		if err != nil {
			t.Fatal("child worker connection failed")
		}
		defer pool.Close()
		var sessionUser, currentUser string
		if err = pool.QueryRow(context.Background(), `select session_user,current_user`).Scan(&sessionUser, &currentUser); err != nil || sessionUser != c.Worker || currentUser != c.Worker {
			t.Fatal("child is not the original direct deletion worker")
		}
		client, err := minio.New(c.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""), Secure: c.Secure})
		if err != nil {
			t.Fatal("child MinIO client failed")
		}
		blob, err := attachments.NewS3BlobStore(client, c.Bucket)
		if err != nil {
			t.Fatal("child blob configuration failed")
		}
		repo := NewPostgresAttachmentRepository(pool)
		if err = repo.ConfigureAttachmentDeletionBlobStore(attachments.BackendKindS3, &v1AfterDeleteBarrier{BlobGCDeleter: blob, event: event, control: control}); err != nil {
			t.Fatal("child deleter configuration failed")
		}
		adapter, err := attachments.NewDeletionAdapter(repo)
		if err != nil {
			t.Fatal("child adapter configuration failed")
		}
		if _, err = adapter.PurgeDeletion(context.Background(), c.Target); err != nil {
			t.Fatal("child purge stopped before barrier")
		}
		t.Fatal("child continued past process-death barrier")
	}
	f := newV1AuthorityFixture(t)
	configRead, configWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	eventRead, eventWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer configWrite.Close()
	defer eventRead.Close()
	defer controlWrite.Close()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestPostgresMinIOIntegrationAttachmentV1ProcessKillRecovery$", "-test.count=1")
	cmd.Env = []string{"HOUFENG_V1_PROCESS_HELPER=1"}
	cmd.ExtraFiles = []*os.File{configRead, eventWrite, controlRead}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = configRead.Close()
	_ = eventWrite.Close()
	_ = controlRead.Close()
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	pidfd, err := unix.PidfdOpen(cmd.Process.Pid, 0)
	if err != nil {
		t.Fatalf("hold original child pidfd: %v", err)
	}
	defer unix.Close(pidfd)
	secure, err := strconv.ParseBool(os.Getenv("HOUFENG_MINIO_SECURE"))
	if os.Getenv("HOUFENG_MINIO_SECURE") == "" {
		secure = false
		err = nil
	}
	if err != nil {
		t.Fatal(err)
	}
	workerURL, err := url.Parse(f.db.db.Config().ConnString())
	if err != nil || (workerURL.Scheme != "postgres" && workerURL.Scheme != "postgresql") {
		t.Fatal("child requires the integration runner's PostgreSQL URL")
	}
	workerURL.User = url.UserPassword(f.db.deletionWorker, f.db.passwords[f.db.deletionWorker])
	workerURL.Path = "/" + f.db.databaseName
	child := v1ChildConfig{DatabaseConfig: workerURL.String(), Database: f.db.databaseName, Worker: f.db.deletionWorker, Password: f.db.passwords[f.db.deletionWorker], Endpoint: os.Getenv("HOUFENG_MINIO_ENDPOINT"), AccessKey: os.Getenv("HOUFENG_MINIO_ACCESS_KEY"), SecretKey: os.Getenv("HOUFENG_MINIO_SECRET_KEY"), Secure: secure, Bucket: f.bucket, Target: f.target}
	if err = json.NewEncoder(configWrite).Encode(child); err != nil {
		t.Fatal("sending private child config failed")
	}
	_ = configWrite.Close()
	barrier := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := io.ReadFull(eventRead, b[:])
		if err == nil && b[0] != 1 {
			err = errors.New("invalid barrier")
		}
		barrier <- err
	}()
	select {
	case err := <-barrier:
		if err != nil {
			t.Fatalf("child failed before delete barrier: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("child did not reach post-delete barrier")
	}
	if err = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	waited = true
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("child exit was not SIGKILL: %v", err)
	}
	terminal := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	if count, pollErr := unix.Poll(terminal, 1000); pollErr != nil || count != 1 || terminal[0].Revents&unix.POLLIN == 0 {
		t.Fatalf("original child pidfd not terminal: count=%d events=%d error=%v", count, terminal[0].Revents, pollErr)
	}
	var extra [1]byte
	if n, err := eventRead.Read(extra[:]); n != 0 || err != io.EOF {
		t.Fatalf("child event channel remained open: %d %v", n, err)
	}
	before := f.facts(t)
	if before.state != "claimed" || before.physical != f.object.SizeBytes || before.result != nil || before.quota != 1 {
		t.Fatalf("killed child settled GC: %+v", before)
	}
	f.survivors(t, true)
	f.expireGC(t)
	_, adapter := f.adapter(t, f.blob)
	receipt, err := adapter.PurgeDeletion(f.ctx, f.target)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := adapter.PurgeDeletion(f.ctx, f.target); err != nil || replay != receipt {
		t.Fatalf("kill recovery replay: %v", err)
	}
	after := f.facts(t)
	if after.id != before.id || after.generation != before.generation+1 || after.attempt != before.attempt+1 || after.state != "completed" || after.physical != 0 || after.quota != 2 || after.result == nil || *after.result != "already_absent" || after.receipts != 3 {
		t.Fatalf("process recovery changed identity or settled twice: before=%+v after=%+v", before, after)
	}
	f.survivors(t, true)
}

func v1JSONFields(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}
func v1JSONSet(t *testing.T, raw []byte, key, value string) []byte {
	t.Helper()
	fields := v1JSONFields(t, raw)
	fields[key] = json.RawMessage(value)
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func v1JSONDuplicate(raw []byte, key, value string) []byte {
	return append(append(append([]byte{}, raw[:len(raw)-1]...), []byte(",\""+key+"\":"+value)...), '}')
}
func v1JSONString(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func v1ExpectSQLRejection(t *testing.T, f *v1AuthorityFixture, query string, body []byte, codes ...string) {
	t.Helper()
	var result []byte
	err := f.worker.QueryRow(f.ctx, query, body).Scan(&result)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("command accepted or returned non-PostgreSQL error: %v", err)
	}
	for _, code := range codes {
		if pgErr.Code == code {
			return
		}
	}
	t.Fatalf("unexpected rejection SQLSTATE %s, expected %v", pgErr.Code, codes)
}

func TestPostgresMinIOIntegrationAttachmentV1AuthorityBoundaryMatrix(t *testing.T) {
	f := newV1AuthorityFixture(t)
	tx, err := f.worker.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := legacyAdapterPurgeEnvelope(f.ctx, tx, f.target, recorddeletion.RecordAttachmentsSurfaceDigest())
	if err != nil {
		_ = tx.Rollback(f.ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	original := v1JSONFields(t, authority)
	otherDigest := v1JSONString(t, strings.Repeat("a", 64))
	identityCases := []struct {
		name, key, value string
		duplicate        bool
		codes            []string
	}{
		{"duplicate", "owner_id", string(original["owner_id"]), true, []string{"22023"}},
		{"unknown", "unexpected", "1", false, []string{"22023"}},
		{"null", "owner_id", "null", false, []string{"22023"}},
		{"boolean_integer", "owner_generation", "true", false, []string{"22023"}},
		{"overflow", "ledger_sequence", "9223372036854775808", false, []string{"22023", "22003"}},
		{"submicrosecond", "owner_expires_at", `"2026-01-01T00:00:00.0000001Z"`, false, []string{"22023"}},
		{"surface", "surface_digest", otherDigest, false, []string{"22023"}},
		{"ledger_domain", "ledger_domain", otherDigest, false, []string{"P0002", "42501"}},
		{"ledger_sequence", "ledger_sequence", strconv.FormatUint(f.target.Operation.LedgerSequence+1, 10), false, []string{"P0002", "42501"}},
		{"ledger_hash", "ledger_entry_hash", otherDigest, false, []string{"P0002", "42501"}},
		{"witness", "witness_proof_digest", otherDigest, false, []string{"P0002", "42501"}},
		{"fence_epoch", "fence_epoch", "13", false, []string{"42501"}},
		{"owner", "owner_id", `"different_worker"`, false, []string{"42501"}},
		{"owner_generation", "owner_generation", "999", false, []string{"42501"}},
		{"owner_expiry", "owner_expires_at", `"2026-01-01T00:00:00Z"`, false, []string{"42501"}},
	}
	for _, tc := range identityCases {
		t.Run("prepare/"+tc.name, func(t *testing.T) {
			body := v1JSONSet(t, authority, tc.key, tc.value)
			if tc.duplicate {
				body = v1JSONDuplicate(authority, tc.key, tc.value)
			}
			v1ExpectSQLRejection(t, f, `select public.record_attachment_purge_prepare_v1($1)`, body, tc.codes...)
		})
	}
	var attachmentsCount, claimsCount, receiptCount int
	var logical, physical int64
	if err = f.db.db.QueryRow(f.ctx, `select (select count(*)::int from public.record_attachments where attachment_id='att_v1authority'),(select count(*)::int from public.blob_gc_deletions where operation_id=$1),(select count(*)::int from public.attachment_purge_receipts where operation_id=$1),logical_bytes,physical_bytes from public.attachment_quota_accounts where project_id='default'`, f.target.Operation.OperationID).Scan(&attachmentsCount, &claimsCount, &receiptCount, &logical, &physical); err != nil {
		t.Fatal(err)
	}
	if attachmentsCount != 1 || claimsCount != 0 || receiptCount != 0 || logical != f.object.SizeBytes || physical != f.object.SizeBytes {
		t.Fatalf("rejected prepare changed state: %d %d %d %d %d", attachmentsCount, claimsCount, receiptCount, logical, physical)
	}
	f.survivors(t, false)
	var prepared []byte
	if err = f.worker.QueryRow(f.ctx, `select public.record_attachment_purge_prepare_v1($1)`, authority).Scan(&prepared); err != nil {
		t.Fatal(err)
	}
	var preparation attachmentPurgeV1Preparation
	if err = json.Unmarshal(prepared, &preparation); err != nil || len(preparation.Claims) != 1 {
		t.Fatalf("prepare did not return one durable claim: %v", err)
	}
	claim := preparation.Claims[0].Claim
	claimRaw, err := json.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	receiptRaw, err := json.Marshal(attachmentPurgeV1Receipt{Key: claim.Key, VersionID: claim.VersionID, SHA256: claim.SHA256, SizeBytes: claim.SizeBytes, Deleted: true})
	if err != nil {
		t.Fatal(err)
	}
	command := func(auth, tuple, receipt []byte) []byte {
		raw, err := json.Marshal(map[string]json.RawMessage{"authority": auth, "claim": tuple, "receipt": receipt})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	complete := `select public.record_attachment_gc_complete_v1($1)`
	before := f.facts(t)
	claimRequest, err := json.Marshal(map[string]any{"authority": json.RawMessage(authority), "deletion_id": claim.DeletionID, "cleanup_owner_id": claim.CleanupOwnerID, "cleanup_lease_ms": 60000})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, key, value string
		duplicate        bool
	}{
		{"duplicate", "cleanup_lease_ms", "60000", true}, {"unknown", "extra", "1", false},
		{"null", "deletion_id", "null", false}, {"boolean", "cleanup_lease_ms", "true", false},
		{"overflow", "cleanup_lease_ms", "9223372036854775808", false},
	} {
		t.Run("gc_claim_request/"+tc.name, func(t *testing.T) {
			raw := v1JSONSet(t, claimRequest, tc.key, tc.value)
			if tc.duplicate {
				raw = v1JSONDuplicate(claimRequest, tc.key, tc.value)
			}
			v1ExpectSQLRejection(t, f, `select public.record_attachment_gc_claim_v1($1)`, raw, "22023")
		})
	}
	for _, key := range []string{"authority", "claim", "receipt"} {
		t.Run("gc_null_"+key, func(t *testing.T) {
			v1ExpectSQLRejection(t, f, complete, v1JSONSet(t, command(authority, claimRaw, receiptRaw), key, "null"), "22023")
		})
	}
	retry, err := json.Marshal(map[string]json.RawMessage{"authority": authority, "claim": claimRaw, "retry_at": json.RawMessage(`"2026-01-01T00:00:00.0000001Z"`)})
	if err != nil {
		t.Fatal(err)
	}
	v1ExpectSQLRejection(t, f, `select public.record_attachment_gc_retry_v1($1)`, retry, "22023")
	for _, tc := range identityCases {
		t.Run("gc_authority/"+tc.name, func(t *testing.T) {
			auth := v1JSONSet(t, authority, tc.key, tc.value)
			if tc.duplicate {
				auth = v1JSONDuplicate(authority, tc.key, tc.value)
			}
			v1ExpectSQLRejection(t, f, complete, command(auth, claimRaw, receiptRaw), tc.codes...)
		})
	}
	tupleCases := []struct {
		name, key, value string
		duplicate        bool
		codes            []string
	}{
		{"duplicate", "attempt", string(v1JSONFields(t, claimRaw)["attempt"]), true, []string{"22023"}},
		{"unknown", "extra", "1", false, []string{"22023"}},
		{"null", "version_id", "null", false, []string{"22023"}},
		{"bool_integer", "attempt", "true", false, []string{"22023"}},
		{"overflow", "attempt", "9223372036854775808", false, []string{"22023"}},
		{"submicrosecond", "cleanup_expires_at", `"2026-01-01T00:00:00.0000001Z"`, false, []string{"22023"}},
		{"version", "version_id", `"wrongversion"`, false, []string{"42501"}},
		{"size", "size_bytes", strconv.FormatInt(claim.SizeBytes+1, 10), false, []string{"42501"}},
		{"digest", "sha256", otherDigest, false, []string{"42501"}},
		{"blob_created_at", "blob_created_at", `"2026-01-01T00:00:00Z"`, false, []string{"42501"}},
		{"cleanup_owner", "cleanup_owner_id", `"different_worker"`, false, []string{"42501"}},
		{"cleanup_generation", "cleanup_owner_generation", strconv.FormatInt(claim.CleanupOwnerGeneration+1, 10), false, []string{"42501"}},
		{"attempt", "attempt", strconv.FormatInt(claim.Attempt+1, 10), false, []string{"42501"}},
		{"cleanup_expiry", "cleanup_expires_at", `"2026-01-01T00:00:00Z"`, false, []string{"42501"}},
	}
	for _, tc := range tupleCases {
		t.Run("claim/"+tc.name, func(t *testing.T) {
			tuple := v1JSONSet(t, claimRaw, tc.key, tc.value)
			if tc.duplicate {
				tuple = v1JSONDuplicate(claimRaw, tc.key, tc.value)
			}
			// Keep receipt internally consistent when probing a wrong but syntactically valid object tuple.
			receipt := receiptRaw
			if tc.key == "version_id" || tc.key == "size_bytes" {
				receipt = v1JSONSet(t, receiptRaw, tc.key, tc.value)
			}
			if tc.key == "sha256" {
				tuple = v1JSONSet(t, tuple, "key", v1JSONString(t, "sha256/"+strings.Repeat("a", 64)))
				receipt = v1JSONSet(t, receipt, "sha256", tc.value)
				receipt = v1JSONSet(t, receipt, "key", v1JSONString(t, "sha256/"+strings.Repeat("a", 64)))
			}
			v1ExpectSQLRejection(t, f, complete, command(authority, tuple, receipt), tc.codes...)
		})
	}
	receiptCases := []struct {
		name, key, value string
		duplicate        bool
	}{
		{"duplicate", "deleted", "true", true}, {"unknown", "extra", "1", false}, {"null", "deleted", "null", false},
		{"bool_type", "deleted", `"true"`, false}, {"boolean_integer", "size_bytes", "true", false},
		{"overflow", "size_bytes", "9223372036854775808", false}, {"wrong_version", "version_id", `"wrongversion"`, false},
		{"wrong_key", "key", `"sha256/wrong"`, false},
		{"wrong_digest", "sha256", otherDigest, false}, {"wrong_size", "size_bytes", strconv.FormatInt(claim.SizeBytes+1, 10), false},
	}
	for _, tc := range receiptCases {
		t.Run("receipt/"+tc.name, func(t *testing.T) {
			receipt := v1JSONSet(t, receiptRaw, tc.key, tc.value)
			if tc.duplicate {
				receipt = v1JSONDuplicate(receiptRaw, tc.key, tc.value)
			}
			v1ExpectSQLRejection(t, f, complete, command(authority, claimRaw, receipt), "22023")
		})
	}
	after := f.facts(t)
	if before != after || after.state != "claimed" || after.result != nil || after.physical != f.object.SizeBytes {
		t.Fatalf("rejected GC changed durable state: before=%+v after=%+v", before, after)
	}
	f.survivors(t, false)
	// A completed receipt must reject the opposite actual deletion result.
	realReceipt, err := f.blob.DeleteGCVersion(f.ctx, f.object)
	if err != nil || !realReceipt.Deleted {
		t.Fatalf("real exact delete: %v", err)
	}
	var completed []byte
	if err = f.worker.QueryRow(f.ctx, complete, command(authority, claimRaw, receiptRaw)).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	v1ExpectSQLRejection(t, f, complete, command(authority, claimRaw, v1JSONSet(t, receiptRaw, "deleted", "false")), "55000")
	final := f.facts(t)
	if final.state != "completed" || final.physical != 0 || final.quota != 2 {
		t.Fatalf("receipt conflict changed settlement: %+v", final)
	}
	f.survivors(t, true)
}
