package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
)

type request struct {
	Phase           string `json:"phase"`
	RunID           string `json:"run_id"`
	Owner           string `json:"owner"`
	RuntimeURL      string `json:"runtime_url"`
	WorkerURL       string `json:"worker_url"`
	DriverSHA256    string `json:"driver_binary_sha256"`
	CenterSHA256    string `json:"center_binary_sha256"`
	ActorID         string `json:"actor_id"`
	RecordID        string `json:"record_id"`
	AttachmentID    string `json:"attachment_id"`
	SharedSHA256    string `json:"shared_sha256"`
	ExclusiveSHA256 string `json:"exclusive_sha256"`
	PreviewSHA256   string `json:"preview_sha256"`
	TemporaryKey    string `json:"temporary_key"`
	ObligationID    string `json:"obligation_id"`
	TemporaryDevice uint64 `json:"temporary_device"`
	TemporaryInode  uint64 `json:"temporary_inode"`
	TemporarySize   int64  `json:"temporary_size"`
	TemporarySHA256 string `json:"temporary_sha256"`
}

type replayState struct {
	RunID         string `json:"run_id"`
	Owner         string `json:"owner"`
	RecordID      string `json:"record_id"`
	ReservationID string `json:"reservation_id"`
	Token         string `json:"token"`
	OperationID   string `json:"operation_id"`
	Receipt       string `json:"receipt"`
	Quota         int64  `json:"quota"`
	GCDigest      string `json:"gc_digest"`
}

var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var obligationRE = regexp.MustCompile(`^rwo_[0-9a-f]{32}$`)

func privateJSON(path string, value any) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "private input")
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 || info.Size() <= 0 || info.Size() > 65536 {
		return errors.New("unsafe private input")
	}
	dec := json.NewDecoder(io.LimitReader(f, 65537))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing private input")
	}
	return nil
}

func saveState(value replayState) error {
	fd, err := unix.Open("/request/state.json", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "private replay state")
	defer f.Close()
	if err := json.NewEncoder(f).Encode(value); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	dir, err := os.Open("/request")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func validateURL(raw, role string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User == nil || u.User.Username() != role || u.Path != "/houfeng" || u.RawQuery != "sslmode=disable" || u.Fragment != "" {
		return errors.New("invalid restricted database connection")
	}
	if password, ok := u.User.Password(); !ok || password == "" {
		return errors.New("missing database credential")
	}
	return nil
}

func checkExecutable(path, expected string) error {
	if !digestRE.MatchString(expected) {
		return errors.New("invalid executable measurement")
	}
	actual, err := recordbirth.DigestExecutable(path)
	if err != nil || hex.EncodeToString(actual[:]) != expected {
		return errors.New("executable measurement mismatch")
	}
	return nil
}

type observedAdapter struct {
	recorddeletion.DeletionPurgeAdapter
	order *[]string
}

func (adapter observedAdapter) PurgeDeletion(ctx context.Context, target recorddeletion.PurgeTarget) (recorddeletion.AdapterPurgeReceipt, error) {
	receipt, err := adapter.DeletionPurgeAdapter.PurgeDeletion(ctx, target)
	if err == nil {
		*adapter.order = append(*adapter.order, string(adapter.Descriptor().Name()))
	}
	return receipt, err
}

type streamBarrier struct {
	ctx      context.Context
	expected []byte
	ready    chan []byte
	release  chan struct{}
	content  bytes.Buffer
	started  bool
}

func (writer *streamBarrier) Write(payload []byte) (int, error) {
	if !writer.started {
		if len(payload) != 16 || !bytes.Equal(payload, writer.expected) {
			return 0, errors.New("stream first chunk differs")
		}
		writer.started = true
		n, err := writer.content.Write(payload)
		if err != nil {
			return n, err
		}
		writer.ready <- append([]byte(nil), payload...)
		select {
		case <-writer.release:
			return n, nil
		case <-writer.ctx.Done():
			return n, writer.ctx.Err()
		}
	}
	return writer.content.Write(payload)
}

type dependencies struct {
	runtime, worker *pgxpool.Pool
	blob            *attachments.LocalBlobStore
	issuer          *store.PostgresRecordDeletionIssuer
	platform        *store.PostgresRecordPlatformRepository
	download        *attachments.DownloadService
	workerService   *recorddeletion.V2DeletionWorker
	order           []string
}

func openDependencies(ctx context.Context, req request) (*dependencies, error) {
	if err := checkExecutable("/proc/self/exe", req.DriverSHA256); err != nil {
		return nil, err
	}
	binding, digest, err := recordbirth.ReadPublishedLaunchBinding("/run/houfeng/records-authority/birth-launch.json", "center")
	if err != nil {
		return nil, err
	}
	if err := checkExecutable("/usr/local/bin/houfeng-center", req.CenterSHA256); err != nil {
		return nil, err
	}
	if hex.EncodeToString(binding.BinaryDigest[:]) != req.CenterSHA256 || binding.BirthInstalled || binding.SourceGeneration != 0 {
		return nil, errors.New("unexpected birth launch binding")
	}
	if err := validateURL(req.RuntimeURL, "houfeng_runtime"); err != nil {
		return nil, err
	}
	if err := validateURL(req.WorkerURL, "houfeng_record_deletion_worker"); err != nil {
		return nil, err
	}
	runtimeEndpoint, _ := url.Parse(req.RuntimeURL)
	workerEndpoint, _ := url.Parse(req.WorkerURL)
	if runtimeEndpoint.Host != workerEndpoint.Host {
		return nil, errors.New("database endpoints differ")
	}
	runtimeURL, err := recordbirth.WithRuntimeLaunch(req.RuntimeURL, digest)
	if err != nil {
		return nil, err
	}
	workerURL, err := recordbirth.WithRuntimeLaunch(req.WorkerURL, digest)
	if err != nil {
		return nil, err
	}
	d := &dependencies{}
	fail := func(err error) (*dependencies, error) { d.close(); return nil, err }
	d.runtime, err = pgxpool.New(ctx, runtimeURL)
	if err != nil {
		return fail(err)
	}
	d.worker, err = pgxpool.New(ctx, workerURL)
	if err != nil {
		return fail(err)
	}
	for _, role := range []struct {
		pool     *pgxpool.Pool
		expected string
	}{{d.runtime, "houfeng_runtime"}, {d.worker, "houfeng_record_deletion_worker"}} {
		var session, current, database string
		var superuser bool
		if err := role.pool.QueryRow(ctx, `select session_user,current_user,current_database(),(select rolsuper from pg_roles where rolname=current_user)`).Scan(&session, &current, &database, &superuser); err != nil || session != role.expected || current != role.expected || database != "houfeng" || superuser {
			return fail(errors.New("restricted database identity mismatch"))
		}
	}
	if err = storemigrate.AdmitAppACLCurrentRuntime(ctx, d.runtime); err != nil {
		return fail(err)
	}
	if err = storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, d.worker); err != nil {
		return fail(err)
	}
	if err = recordbirth.VerifyRuntimeLaunch(ctx, d.runtime, nil, binding); err != nil {
		return fail(err)
	}
	gate, err := store.NewDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{InstanceID: "compose-center", DeploymentID: recordplatform.DeploymentID(binding.DeploymentID), ProjectID: recordplatform.ProjectIDDefault, InstanceKind: "api", Capability: "records.runtime"})
	if err != nil {
		return fail(err)
	}
	birth, err := recordbirth.NewCoordinator(store.NewPostgresRecordBirthRepository(d.runtime, gate, nil), nil, nil)
	if err != nil {
		return fail(err)
	}
	d.platform = store.NewPostgresRecordPlatformRepository(d.runtime, gate)
	runtimeRepo := store.NewPostgresRecordDeletionRepository(d.runtime, gate)
	d.issuer, err = store.NewPostgresRecordDeletionIssuer(runtimeRepo, recordplatform.DeploymentID(binding.DeploymentID), birth, recorddeletion.UnavailableDeletionBackupInventorySource{}, recorddeletion.ServiceOptions{OwnerID: "g2arc_issuer", OwnerLeaseDuration: time.Second})
	if err != nil {
		return fail(err)
	}
	localRuntime := store.NewPostgresLocalBlobRepository(d.runtime, nil, birth)
	d.blob, err = attachments.NewLocalBlobStore(ctx, "/var/lib/houfeng/attachments", localRuntime)
	if err != nil {
		return fail(err)
	}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(store.NewPostgresVPSAssetRepository(d.runtime)),
		store.NewMonitoringInstanceRecordSubjectAdapter(store.NewPostgresMonitoringInstanceRepository(d.runtime)),
		store.NewTargetRecordSubjectAdapter(store.NewPostgresTargetRepository(d.runtime)),
	})
	if err != nil {
		return fail(err)
	}
	tombstones, err := store.NewWitnessedRecordSubjectTombstoneReader(recordplatform.DeploymentID(binding.DeploymentID), recordauth.ProjectIDDefault, nil, d.runtime)
	if err != nil {
		return fail(err)
	}
	authorizer := store.NewPostgresCurrentRecordAuthorizationSource(d.runtime, store.NewRecordSubjectReadResolver(subjects, tombstones), gate)
	d.download, err = attachments.NewDownloadService(store.NewPostgresAttachmentRepository(d.runtime), authorizer, d.platform, d.blob, attachments.DownloadServiceOptions{
		ChunkBytes: 16, LeaseDuration: time.Second,
		NewLeaseOwnerID: func() (string, error) { return "g2arc_stream", nil },
	})
	if err != nil {
		return fail(err)
	}
	cleanup, err := attachments.NewLocalCleanupCoordinator(d.blob, store.NewPostgresLocalBlobRepository(d.worker, nil, nil))
	if err != nil {
		return fail(err)
	}
	workerRepo := store.NewPostgresRecordDeletionRepository(d.worker, gate)
	if err = workerRepo.ConfigurePortabilityDeletionLocalCleanup(cleanup); err != nil {
		return fail(err)
	}
	attachmentRepo := store.NewPostgresAttachmentRepository(d.worker)
	if err = attachmentRepo.ConfigureAttachmentDeletionLocalCleanup(cleanup); err != nil {
		return fail(err)
	}
	workerEvidence := store.NewPostgresEvidenceRepository(d.worker, gate, nil)
	runtimeEvidence := store.NewPostgresEvidenceRepository(d.runtime, gate, birth)
	comparison, err := store.NewPostgresComparisonDeletionStore(runtimeEvidence, workerEvidence)
	if err != nil {
		return fail(err)
	}
	var adapters []recorddeletion.Adapter
	add := func(adapter recorddeletion.DeletionPurgeAdapter, err error) error {
		if err != nil {
			return err
		}
		adapters = append(adapters, observedAdapter{DeletionPurgeAdapter: adapter, order: &d.order})
		return nil
	}
	if err = add(recorddeletion.NewCoreAdapter(workerRepo)); err != nil {
		return fail(err)
	}
	if err = add(attachments.NewDeletionAdapter(attachmentRepo)); err != nil {
		return fail(err)
	}
	if err = add(evidence.NewDeletionAdapter(workerEvidence)); err != nil {
		return fail(err)
	}
	if err = add(recorddeletion.NewMarkdownClientAdapter(workerRepo)); err != nil {
		return fail(err)
	}
	if err = add(recordsearch.NewDeletionAdapter(workerRepo)); err != nil {
		return fail(err)
	}
	if err = add(activity.NewDeletionAdapter(workerRepo)); err != nil {
		return fail(err)
	}
	if err = add(recorddeletion.NewComparisonAdapter(comparison)); err != nil {
		return fail(err)
	}
	if err = add(recordcollaboration.NewDeletionAdapter(workerRepo)); err != nil {
		return fail(err)
	}
	if err = add(portability.NewDeletionAdapter(workerRepo)); err != nil {
		return fail(err)
	}
	registry, err := recorddeletion.NewRegistry(adapters)
	if err != nil {
		return fail(err)
	}
	d.workerService = recorddeletion.NewV2DeletionWorker(workerRepo, recorddeletion.NewRegistryPurger(registry), recorddeletion.DeletionWorkerOptions{OwnerID: "g2arc_worker", OwnerLeaseDuration: 5 * time.Minute})
	return d, nil
}
func (d *dependencies) close() {
	if d.blob != nil {
		_ = d.blob.Close()
	}
	if d.worker != nil {
		d.worker.Close()
	}
	if d.runtime != nil {
		d.runtime.Close()
	}
}

func readBlob(digest string) ([]byte, error) {
	if !digestRE.MatchString(digest) {
		return nil, errors.New("invalid blob digest")
	}
	path := filepath.Join("/var/lib/houfeng/attachments/sha256", digest)
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "blob")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16<<20 {
		return nil, errors.New("invalid blob")
	}
	body, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if err != nil {
		return nil, err
	}
	actual := sha256.Sum256(body)
	if hex.EncodeToString(actual[:]) != digest {
		return nil, errors.New("blob digest mismatch")
	}
	return body, nil
}
func absent(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return errors.New("expected blob absence")
}
func snapshot(ctx context.Context, pool *pgxpool.Pool, operation string) (int64, string, string, error) {
	var quota int64
	var receipt string
	if err := pool.QueryRow(ctx, `select q.physical_bytes,encode(o.receipt_digest,'hex') from public.record_purge_operations o cross join public.attachment_quota_accounts q where o.operation_id=$1 and o.operation_state='online_purged' and q.project_id='default'`, operation).Scan(&quota, &receipt); err != nil {
		return 0, "", "", err
	}
	hash := sha256.New()
	for _, table := range []string{"record_core_purge_receipts", "attachment_purge_receipts", "evidence_purge_receipts", "record_markdown_client_purge_receipts", "record_search_purge_receipts", "record_activity_purge_receipts", "record_comparison_purge_receipts", "record_collaboration_purge_receipts", "record_portability_purge_receipts", "blob_gc_deletions", "record_write_obligations", "browser_record_cleanup_events"} {
		var rows string
		query := `select coalesce(jsonb_agg(to_jsonb(r) order by to_jsonb(r)::text), '[]'::jsonb)::text from public.` + table + ` r where operation_id=$1`
		if err := pool.QueryRow(ctx, query, operation).Scan(&rows); err != nil {
			return 0, "", "", err
		}
		_, _ = fmt.Fprintf(hash, "%d:%s%d:%s", len(table), table, len(rows), rows)
	}
	return quota, receipt, hex.EncodeToString(hash.Sum(nil)), nil
}

func execute(ctx context.Context, req request, d *dependencies) (map[string]any, error) {
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: req.ActorID, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
	if err != nil {
		return nil, err
	}
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: req.RecordID}
	if err := object.Validate(); err != nil {
		return nil, err
	}
	if req.Phase == "replay" {
		var state replayState
		if err := privateJSON("/request/state.json", &state); err != nil {
			return nil, err
		}
		if state.RunID != req.RunID || state.Owner != req.Owner || state.RecordID != req.RecordID {
			return nil, errors.New("replay identity mismatch")
		}
		token, err := recordplatform.ParseDeletionRequestTokenTransportV1(state.Token)
		if err != nil {
			return nil, err
		}
		op, err := d.issuer.Execute(ctx, recorddeletion.ExecuteRequest{Actor: actor, RecordID: req.RecordID, ReservationID: state.ReservationID, Token: token, ReasonCode: recorddeletion.DeletionReasonUserConfirmed})
		if err != nil || op.OperationID != state.OperationID || op.State != recorddeletion.DeletionStateOnlinePurged || hex.EncodeToString(op.ReceiptDigest[:]) != state.Receipt {
			return nil, errors.New("terminal execute replay failed")
		}
		if err := d.workerService.RunOnce(ctx); err != nil || len(d.order) != 0 {
			return nil, errors.New("terminal worker replay failed")
		}
		quota, receipt, gc, err := snapshot(ctx, d.runtime, state.OperationID)
		if err != nil || quota != state.Quota || receipt != state.Receipt || gc != state.GCDigest {
			return nil, errors.New("terminal replay changed effects")
		}
		if _, err := readBlob(req.SharedSHA256); err != nil {
			return nil, err
		}
		for _, name := range []string{req.ExclusiveSHA256, req.PreviewSHA256, strings.TrimPrefix(req.TemporaryKey, "sha256/")} {
			if err := absent(filepath.Join("/var/lib/houfeng/attachments/sha256", name)); err != nil {
				return nil, err
			}
		}
		return map[string]any{"status": "PASS", "phase": "replay", "operation_id": state.OperationID, "receipt": receipt, "physical_bytes": quota, "duplicate_effects": false}, nil
	}
	shared, err := readBlob(req.SharedSHA256)
	if err != nil {
		return nil, err
	}
	exclusive, err := readBlob(req.ExclusiveSHA256)
	if err != nil {
		return nil, err
	}
	preview, err := readBlob(req.PreviewSHA256)
	if err != nil || req.ExclusiveSHA256 == req.PreviewSHA256 || req.SharedSHA256 == req.ExclusiveSHA256 || req.SharedSHA256 == req.PreviewSHA256 {
		return nil, errors.New("invalid exclusive blob identities")
	}
	temporary := filepath.Join("/var/lib/houfeng/attachments", req.TemporaryKey)
	info, err := os.Lstat(temporary)
	if err != nil || !info.Mode().IsRegular() || req.TemporarySize <= 0 || req.TemporarySize > 16<<20 || info.Size() != req.TemporarySize || !digestRE.MatchString(req.TemporarySHA256) {
		return nil, errors.New("temporary file unavailable")
	}
	physical, ok := info.Sys().(*syscall.Stat_t)
	if !ok || physical.Dev != req.TemporaryDevice || physical.Ino != req.TemporaryInode {
		return nil, errors.New("temporary file identity changed")
	}
	fd, err := unix.Open(temporary, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	tempFile := os.NewFile(uintptr(fd), "interrupted temporary")
	tempInfo, err := tempFile.Stat()
	if err != nil || !os.SameFile(info, tempInfo) {
		_ = tempFile.Close()
		return nil, errors.New("temporary file changed")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(tempFile, req.TemporarySize+1))
	afterInfo, statErr := tempFile.Stat()
	_ = tempFile.Close()
	if err != nil || statErr != nil || n != req.TemporarySize || afterInfo.Size() != info.Size() || !afterInfo.ModTime().Equal(info.ModTime()) || hex.EncodeToString(hash.Sum(nil)) != req.TemporarySHA256 {
		return nil, errors.New("temporary bytes changed")
	}
	var epoch int64
	var gates int
	if err := d.runtime.QueryRow(ctx, `select delivery_epoch,(select count(*) from public.record_deletion_gates where project_id='default' and record_id=$1) from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1`, req.RecordID).Scan(&epoch, &gates); err != nil {
		return nil, err
	}
	if gates != 0 {
		return nil, errors.New("record was already fenced")
	}
	lease, err := d.platform.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "g2arc_serving", LeaseDuration: time.Minute})
	if err != nil {
		return nil, err
	}
	if err := d.platform.AssertServingLease(ctx, lease); err != nil {
		return nil, err
	}
	if err := d.platform.ReleaseObjectContentLease(ctx, lease.Object, lease.Owner); err != nil {
		return nil, err
	}
	if len(shared) < 16 {
		return nil, errors.New("shared attachment is too short for stream barrier")
	}
	streamCtx, cancelStream := context.WithTimeout(ctx, 3*time.Minute)
	stream, err := d.download.Open(streamCtx, attachments.DownloadRequest{Actor: actor, AttachmentID: req.AttachmentID, Variant: attachments.ContentVariantOriginal})
	if err != nil {
		cancelStream()
		return nil, err
	}
	metadata := stream.Metadata()
	if metadata.AttachmentID != req.AttachmentID || metadata.Range.Length != int64(len(shared)) || hex.EncodeToString(metadata.Object.SHA256[:]) != req.SharedSHA256 {
		cancelStream()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = stream.Close(cleanupCtx)
		cleanupCancel()
		return nil, errors.New("stream metadata differs from target attachment")
	}
	barrier := &streamBarrier{ctx: streamCtx, expected: shared[:16], ready: make(chan []byte, 1), release: make(chan struct{})}
	done := make(chan struct{})
	var streamBytes int64
	var streamErr error
	go func() {
		streamBytes, streamErr = stream.WriteTo(streamCtx, barrier)
		close(done)
	}()
	released, streamClosed := false, false
	defer func() {
		cancelStream()
		if !released {
			close(barrier.release)
		}
		<-done
		if !streamClosed {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = stream.Close(cleanupCtx)
			cleanupCancel()
		}
	}()
	var firstChunk []byte
	select {
	case firstChunk = <-barrier.ready:
	case <-done:
		return nil, errors.New("content stream ended before reaching barrier")
	case <-streamCtx.Done():
		return nil, streamCtx.Err()
	}
	if len(firstChunk) != 16 || !bytes.Equal(firstChunk, shared[:16]) {
		return nil, errors.New("content stream first chunk differs")
	}
	checkStreamLease := func() error {
		select {
		case <-done:
			return errors.New("content stream is not held at the barrier")
		default:
		}
		var live bool
		if err := d.runtime.QueryRow(ctx, `select exists(select 1 from public.object_content_leases where project_id='default' and object_kind='record' and object_id=$1 and owner_id='g2arc_stream' and expires_at>clock_timestamp())`, req.RecordID).Scan(&live); err != nil || !live {
			return errors.New("stream serving lease is not live")
		}
		return nil
	}
	if err := checkStreamLease(); err != nil {
		return nil, err
	}
	issued, err := d.issuer.Preview(ctx, recorddeletion.PreviewRequest{Actor: actor, RecordID: req.RecordID})
	if err != nil {
		return nil, err
	}
	if issued.Summary.Validate() != nil || issued.Summary.BackupInventory.Availability != recorddeletion.BackupInventoryUnavailable {
		return nil, errors.New("invalid online-only preview disclosure")
	}
	token, err := recordplatform.ParseDeletionRequestTokenTransportV1(issued.Token.Transport())
	if err != nil {
		return nil, err
	}
	executeRequest := recorddeletion.ExecuteRequest{Actor: actor, RecordID: req.RecordID, ReservationID: issued.ReservationID, Token: token, ReasonCode: recorddeletion.DeletionReasonUserConfirmed}
	if _, err := d.issuer.Execute(ctx, executeRequest); !errors.Is(err, recordplatform.ErrObjectContentLeaseLive) {
		return nil, errors.New("live serving lease did not block issuance")
	}
	var epochAfter int64
	var gatesAfter int
	if err := d.runtime.QueryRow(ctx, `select delivery_epoch,(select count(*) from public.record_deletion_gates where project_id='default' and record_id=$1) from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1`, req.RecordID).Scan(&epochAfter, &gatesAfter); err != nil || epochAfter != epoch || gatesAfter != 0 {
		return nil, errors.New("failed issuance changed fence")
	}
	if err := checkStreamLease(); err != nil {
		return nil, err
	}
	close(barrier.release)
	released = true
	select {
	case <-done:
	case <-streamCtx.Done():
		return nil, streamCtx.Err()
	}
	if streamErr != nil || streamBytes != int64(len(shared)) || !bytes.Equal(barrier.content.Bytes(), shared) {
		return nil, errors.New("content stream failed to drain exact bytes")
	}
	if err := stream.Close(ctx); err != nil {
		return nil, err
	}
	streamClosed = true
	var liveAfterClose bool
	if err := d.runtime.QueryRow(ctx, `select exists(select 1 from public.object_content_leases where project_id='default' and object_kind='record' and object_id=$1 and owner_id='g2arc_stream' and expires_at>clock_timestamp())`, req.RecordID).Scan(&liveAfterClose); err != nil || liveAfterClose {
		return nil, errors.New("stream serving lease was not released")
	}
	op, err := d.issuer.Execute(ctx, executeRequest)
	if errors.Is(err, recorddeletion.ErrDeletionPreviewStale) {
		issued, err = d.issuer.Preview(ctx, recorddeletion.PreviewRequest{Actor: actor, RecordID: req.RecordID})
		if err != nil {
			return nil, err
		}
		if issued.Summary.Validate() != nil || issued.Summary.BackupInventory.Availability != recorddeletion.BackupInventoryUnavailable {
			return nil, errors.New("invalid refreshed preview disclosure")
		}
		token, err = recordplatform.ParseDeletionRequestTokenTransportV1(issued.Token.Transport())
		if err != nil {
			return nil, err
		}
		executeRequest.ReservationID, executeRequest.Token = issued.ReservationID, token
		op, err = d.issuer.Execute(ctx, executeRequest)
	}
	if err != nil || op.State != recorddeletion.DeletionStateOnlinePurging {
		return nil, errors.New("issuance failed")
	}
	if _, err := d.platform.RenewServingLease(ctx, lease, time.Minute); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		return nil, errors.New("old lease renewed")
	}
	if err := d.platform.AssertServingLease(ctx, lease); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		return nil, errors.New("old lease asserted")
	}
	if _, err := d.platform.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "g2arc_after_fence", LeaseDuration: time.Minute}); !errors.Is(err, recordplatform.ErrDeletionFenceLeaseLive) {
		return nil, errors.New("new lease crossed fence")
	}
	var eventCount int
	if err := d.runtime.QueryRow(ctx, `select e.delivery_epoch,(select count(*) from public.record_deletion_gates where operation_id=$2 and record_id=$1),(select count(*) from public.browser_record_cleanup_events where operation_id=$2 and record_id=$1) from public.content_delivery_epochs e where e.project_id='default' and e.object_kind='record' and e.object_id=$1`, req.RecordID, op.OperationID).Scan(&epochAfter, &gatesAfter, &eventCount); err != nil || epochAfter <= epoch || gatesAfter != 1 || eventCount < 1 {
		return nil, errors.New("fence or cleanup event missing")
	}
	var beforeQuota int64
	if err := d.runtime.QueryRow(ctx, `select physical_bytes from public.attachment_quota_accounts where project_id='default'`).Scan(&beforeQuota); err != nil {
		return nil, err
	}
	if _, err := d.runtime.Exec(ctx, `select pg_sleep(greatest(0,extract(epoch from expires_at-clock_timestamp())+0.01)) from public.deletion_fence_leases where project_id='default' and object_kind='record' and object_id=$1`, req.RecordID); err != nil {
		return nil, err
	}
	if err := d.workerService.RunOnce(ctx); err != nil {
		return nil, err
	}
	expected := []string{"record_portability", "record_collaboration", "record_comparison", "record_activity_projection", "record_search", "record_markdown_client", "record_evidence", "record_attachments", "record_core"}
	if !equalStrings(d.order, expected) {
		return nil, errors.New("purge order mismatch")
	}
	if _, err := d.platform.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "g2arc_after_core", LeaseDuration: time.Minute}); !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) {
		return nil, errors.New("core purge retained delivery epoch")
	}
	if current, err := readBlob(req.SharedSHA256); err != nil || !bytes.Equal(current, shared) {
		return nil, errors.New("shared blob changed")
	}
	for _, name := range []string{req.ExclusiveSHA256, req.PreviewSHA256, strings.TrimPrefix(req.TemporaryKey, "sha256/")} {
		if err := absent(filepath.Join("/var/lib/houfeng/attachments/sha256", name)); err != nil {
			return nil, err
		}
	}
	dir, err := os.Open("/var/lib/houfeng/attachments/sha256")
	if err != nil {
		return nil, err
	}
	err = dir.Sync()
	_ = dir.Close()
	if err != nil {
		return nil, err
	}
	quota, receipt, gc, err := snapshot(ctx, d.runtime, op.OperationID)
	if err != nil || len(receipt) != 64 || beforeQuota-quota < int64(len(exclusive)+len(preview)) {
		return nil, errors.New("terminal receipt or quota settlement missing")
	}
	var retained, deleted int
	if err := d.runtime.QueryRow(ctx, `select count(*) filter(where blob_key=$2 and deletion_state='completed' and physical_delete_result='retained_referenced' and receipt_digest is not null),count(*) filter(where blob_key in ($3,$4) and deletion_state='completed' and physical_delete_result='deleted' and receipt_digest is not null) from public.blob_gc_deletions where operation_id=$1`, op.OperationID, "sha256/"+req.SharedSHA256, "sha256/"+req.ExclusiveSHA256, "sha256/"+req.PreviewSHA256).Scan(&retained, &deleted); err != nil || retained != 1 || deleted != 2 {
		return nil, errors.New("blob cleanup receipts do not establish shared and exclusive outcomes")
	}
	var complete bool
	if err := d.runtime.QueryRow(ctx, `select cleanup_completed_at is not null and cleanup_receipt is not null and cleanup_outcome in ('deleted','already_absent','retained_referenced') from public.record_write_obligations where obligation_id=$1 and record_id=$2 and operation_id=$3`, req.ObligationID, req.RecordID, op.OperationID).Scan(&complete); err != nil || !complete {
		return nil, errors.New("interrupted obligation unresolved")
	}
	state := replayState{RunID: req.RunID, Owner: req.Owner, RecordID: req.RecordID, ReservationID: executeRequest.ReservationID, Token: token.Transport(), OperationID: op.OperationID, Receipt: receipt, Quota: quota, GCDigest: gc}
	if err := saveState(state); err != nil {
		return nil, err
	}
	return map[string]any{"status": "PASS", "phase": "issue", "operation_id": op.OperationID, "receipt": receipt, "purge_order": d.order, "physical_bytes_before": beforeQuota, "physical_bytes_after": quota, "cleanup_events": eventCount, "directory_fsync": true, "serving_asserted": true, "stream_attachment_id": req.AttachmentID, "stream_first_chunk_bytes": len(firstChunk), "stream_total_bytes": streamBytes, "stream_barrier_held": true, "stream_drained": true, "live_lease_rejection": "object_content_lease_live", "epoch_before": epoch, "epoch_after": epochAfter, "gate_count": gatesAfter, "old_lease_renew_and_assert": "lost_owner_lease", "new_lease_before_purge": "deletion_fence_lease_live", "new_lease_after_purge": "content_delivery_epoch_missing", "backup_inventory": "unavailable", "shared_cleanup": "retained_referenced", "exclusive_and_preview_cleanup": "deleted", "interrupted_obligation_completed": true}, nil
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func run() error {
	var path string
	flag.StringVar(&path, "request-file", "", "private smoke request")
	flag.Parse()
	if flag.NArg() != 0 || path != "/request/request.json" || os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" || os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" {
		return errors.New("isolated smoke authorization required")
	}
	var req request
	if err := privateJSON(path, &req); err != nil {
		return errors.New("invalid private smoke request")
	}
	if req.RunID != os.Getenv("HOUFENG_RECORDS_RUN_ID") || req.Owner != os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") || (req.Phase != "issue" && req.Phase != "replay") || !obligationRE.MatchString(req.ObligationID) || req.TemporaryKey != "sha256/.blob-tmp-"+req.ObligationID {
		return errors.New("invalid owned smoke identity")
	}
	redact := func(err error) error {
		message := regexp.MustCompile(`drt1_[A-Za-z0-9_-]{43}`).ReplaceAllString(err.Error(), "[redacted]")
		for _, raw := range []string{req.RuntimeURL, req.WorkerURL} {
			if raw == "" {
				continue
			}
			message = strings.ReplaceAll(message, raw, "[redacted]")
			if parsed, parseErr := url.Parse(raw); parseErr == nil && parsed.User != nil {
				if password, ok := parsed.User.Password(); ok && password != "" {
					message = strings.ReplaceAll(message, password, "[redacted]")
				}
			}
		}
		return errors.New(message)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	d, err := openDependencies(ctx, req)
	if err != nil {
		return fmt.Errorf("restricted smoke dependency admission: %w", redact(err))
	}
	defer d.close()
	result, err := execute(ctx, req, d)
	if err != nil {
		return fmt.Errorf("restricted deletion smoke: %w", redact(err))
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
