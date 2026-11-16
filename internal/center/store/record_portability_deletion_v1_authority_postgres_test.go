package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
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
	"houfeng/internal/center/records"
)

// configuredTestS3Binding identifies the actual configured fixture store; it
// does not claim that a database-only test performed physical S3 I/O.
func configuredTestS3Binding(t *testing.T) string {
	t.Helper()
	client, err := minio.New("127.0.0.1:9000", &minio.Options{Creds: credentials.NewStaticV4("access", "secret", ""), Secure: false})
	if err != nil {
		t.Fatal(err)
	}
	store, err := attachments.NewS3BlobStore(client, "fixture-route")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.GCStorageIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(identity[:])
}

func TestPortabilityS3StorageRouteBinding(t *testing.T) {
	client := func(endpoint string, secure bool) *minio.Client {
		t.Helper()
		result, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4("access", "secret", ""), Secure: secure})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	newStore := func(c *minio.Client, bucket string) *attachments.S3BlobStore {
		t.Helper()
		result, err := attachments.NewS3BlobStore(c, bucket)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	originalClient := client("127.0.0.1:9000", false)
	original := newStore(originalClient, "route-one")
	identity, err := original.GCStorageIdentity()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	for _, field := range []string{"houfeng.s3-gc-storage-route.v1", originalClient.EndpointURL().String(), "route-one"} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(field))
	}
	var expected [sha256.Size]byte
	copy(expected[:], hash.Sum(nil))
	if identity != expected {
		t.Fatalf("route fingerprint = %x, want %x", identity, expected)
	}
	for _, other := range []*attachments.S3BlobStore{
		newStore(originalClient, "route-two"),
		newStore(client("127.0.0.1:9001", false), "route-one"),
		newStore(client("127.0.0.1:9000", true), "route-one"),
	} {
		got, err := other.GCStorageIdentity()
		if err != nil || got == identity {
			t.Fatalf("different S3 route fingerprint = %x, %v", got, err)
		}
	}
	repository := &PostgresRecordDeletionRepository{}
	if err := repository.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, original); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, original); err != nil {
		t.Fatalf("rebind same instance: %v", err)
	}
	if err := repository.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, newStore(originalClient, "route-one")); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("rebind another instance = %v", err)
	}
	var typedNil *attachments.S3BlobStore
	if err := repository.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, typedNil); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("bind typed nil = %v", err)
	}
	if err := repository.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindLocal, original); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("bind local backend = %v", err)
	}
}

func TestPostgresMinIOIntegrationManagedWriteSourceBinding(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("PostgreSQL and MinIO integration required")
	}
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	record := seedRecordWithoutSearchProjection(t, ctx, fixture, "S3 provenance")
	runtime := fixture.openDirectRuntimePool(t, ctx, "s3-source-binding", 2)
	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	blob, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatal(err)
	}
	route, err := blob.GCStorageIdentity()
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresLocalBlobRepository(runtime, localBlobFixtureWitness(t, runtime), nil)
	writer, err := attachments.NewManagedBlobWriter(blob, attachments.BackendKindS3, repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"artifact", "obligation"} {
		t.Run(suffix, func(t *testing.T) {
			body := []byte("S3 source " + suffix)
			digest := sha256.Sum256(body)
			jobID := "rej_source" + suffix
			var expiry time.Time
			if err := fixture.db.QueryRow(ctx, `insert into public.record_export_jobs
			(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,inventory_digest,
			authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
			values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'markdown','safe','staging',$2,$2,1,$3,$4,'source_worker',1,
			clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour') returning expires_at`, jobID, digest[:], record.RecordID, record.RevisionID).Scan(&expiry); err != nil {
				t.Fatal(err)
			}
			temporary, err := attachments.NewBlobTemporaryKey()
			if err != nil {
				t.Fatal(err)
			}
			written, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default", Origin: attachments.WriteOrigin{Kind: "export_artifact", ID: jobID, Generation: 1}, Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(body)), TemporaryKey: temporary}}, bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			var binding, registrationBinding, outcome string
			if err := fixture.db.QueryRow(ctx, `select encode(storage_binding,'hex'),convert_from(registration_command,'UTF8')::jsonb->>'storage_binding',outcome from public.record_write_obligations where obligation_id=$1`, written.ObligationID).Scan(&binding, &registrationBinding, &outcome); err != nil {
				t.Fatal(err)
			}
			if binding != hex.EncodeToString(route[:]) || registrationBinding != binding || outcome != "materialized" {
				t.Fatalf("source provenance %q/%q/%q", binding, registrationBinding, outcome)
			}
			var registrationBytes []byte
			var dispatchID string
			if err := fixture.db.QueryRow(ctx, `select registration_command,dispatch_id::text from public.record_write_obligations where obligation_id=$1`, written.ObligationID).Scan(&registrationBytes, &dispatchID); err != nil {
				t.Fatal(err)
			}
			var registration attachments.WriteRegistration
			if err := json.Unmarshal(registrationBytes, &registration); err != nil {
				t.Fatal(err)
			}
			identity := attachments.WriteIdentity{ObligationID: written.ObligationID, WriterInstance: registration.WriterInstance}
			for _, readback := range []func() (attachments.WriteObligation, error){
				func() (attachments.WriteObligation, error) { return repository.RegisterWrite(ctx, registration) },
				func() (attachments.WriteObligation, error) { return repository.ResolveWrite(ctx, identity) },
				func() (attachments.WriteObligation, error) {
					return repository.DispatchWrite(ctx, attachments.WriteDispatch{WriteIdentity: identity, DispatchID: dispatchID})
				},
				func() (attachments.WriteObligation, error) {
					return repository.RecordWriteReturn(ctx, attachments.WriteReturn{WriteIdentity: identity, DispatchID: dispatchID, Outcome: "materialized", Version: &written.Version.VersionID, Key: written.Version.Key, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(body))})
				},
			} {
				observed, err := readback()
				if err != nil || observed.StorageBinding == nil || *observed.StorageBinding != binding {
					t.Fatalf("replayed source obligation = %+v, %v", observed, err)
				}
			}
			conflicting := registration
			conflicting.StorageBinding = nil
			if _, err := repository.RegisterWrite(ctx, conflicting); err == nil {
				t.Fatal("unbound S3 registration replay accepted")
			}
			duplicate := append([]byte(`{"storage_binding":null,`), registrationBytes[1:]...)
			if _, err := runtime.Exec(ctx, `select public.record_write_register($1)`, duplicate); err == nil {
				t.Fatal("duplicate registration field accepted")
			}
			if _, err := fixture.db.Exec(ctx, `update public.record_write_obligations set storage_binding=decode($2,'hex') where obligation_id=$1`, written.ObligationID, hex.EncodeToString(digest[:])); err == nil {
				t.Fatal("immutable binding accepted a change")
			}
			if suffix == "artifact" {
				command, err := json.Marshal(map[string]any{"artifact_id": "rxa_sourceartifact", "job_id": jobID, "kind": "markdown", "content_type": "text/markdown", "backend_kind": "s3", "namespace_id": nil, "key": written.Version.Key, "version": written.Version.VersionID, "sha256": hex.EncodeToString(digest[:]), "size": len(body), "expires_at": expiry, "obligation_id": written.ObligationID})
				if err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if _, err := runtime.Exec(ctx, `select public.record_export_artifact_publish($1)`, command); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				// Simulate a historical dispatched responsibility with no recorded route.
				// Its late provider fact remains admissible, but cannot create provenance.
				const historicalID = "rwo_99999999999999999999999999999999"
				var writerID, dispatchID string
				if err := fixture.db.QueryRow(ctx, `insert into public.record_write_obligations
				(obligation_id,writer_instance,request_seq,writer_principal,project_id,origin_kind,origin_id,origin_generation,
				 namespace_id,incarnation_id,backend_kind,storage_binding,canonical_key,sha256_digest,size_bytes,record_id,record_epoch,
				 registration_command,dispatch_id,dispatched_at,quota_settlement_id)
				select $2,writer_instance,request_seq+10000,writer_principal,project_id,origin_kind,origin_id,origin_generation,
				 namespace_id,incarnation_id,backend_kind,null,canonical_key,sha256_digest,size_bytes,record_id,record_epoch,
				 convert_to((convert_from(registration_command,'UTF8')::jsonb || jsonb_build_object('obligation_id',$2::text,'request_seq',request_seq+10000,'storage_binding',null))::text,'UTF8'),
				 dispatch_id,dispatched_at,$2 from public.record_write_obligations where obligation_id=$1 returning writer_instance::text,dispatch_id::text`, written.ObligationID, historicalID).Scan(&writerID, &dispatchID); err != nil {
					t.Fatal(err)
				}
				const undispatchedID = "rwo_88888888888888888888888888888888"
				if _, err := fixture.db.Exec(ctx, `insert into public.record_write_obligations
				(obligation_id,writer_instance,request_seq,writer_principal,project_id,origin_kind,origin_id,origin_generation,
				 namespace_id,incarnation_id,backend_kind,storage_binding,canonical_key,sha256_digest,size_bytes,record_id,record_epoch,registration_command,quota_settlement_id)
				select $2,writer_instance,request_seq+10000,writer_principal,project_id,origin_kind,origin_id,origin_generation,
				 namespace_id,incarnation_id,backend_kind,null,canonical_key,sha256_digest,size_bytes,record_id,record_epoch,
				 convert_to((convert_from(registration_command,'UTF8')::jsonb || jsonb_build_object('obligation_id',$2::text,'request_seq',request_seq+10000))::text,'UTF8'),$2
				from public.record_write_obligations where obligation_id=$1`, historicalID, undispatchedID); err != nil {
					t.Fatal(err)
				}
				if _, err := repository.DispatchWrite(ctx, attachments.WriteDispatch{WriteIdentity: attachments.WriteIdentity{ObligationID: undispatchedID, WriterInstance: writerID}, DispatchID: localBlobFixtureUUID(t)}); err == nil {
					t.Fatal("unbound historical obligation authorized new dispatch")
				}
				returned, err := repository.RecordWriteReturn(ctx, attachments.WriteReturn{WriteIdentity: attachments.WriteIdentity{ObligationID: historicalID, WriterInstance: writerID}, DispatchID: dispatchID, Outcome: "materialized", Version: &written.Version.VersionID, Key: written.Version.Key, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(body))})
				if err != nil || returned.StorageBinding != nil || returned.Version == nil {
					t.Fatalf("historical return = %+v, %v", returned, err)
				}
				command, err := json.Marshal(map[string]any{"artifact_id": "rxa_historical", "job_id": jobID, "kind": "markdown", "content_type": "text/markdown", "backend_kind": "s3", "namespace_id": nil, "key": written.Version.Key, "version": written.Version.VersionID, "sha256": hex.EncodeToString(digest[:]), "size": len(body), "expires_at": expiry, "obligation_id": historicalID})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := runtime.Exec(ctx, `select public.record_export_artifact_publish($1)`, command); err == nil {
					t.Fatal("historical unbound responsibility published a new artifact")
				}
			}
		})
	}
}

// The database owner can frame captured identities, but application roles
// cannot invoke this private helper to assert ownership.
func TestPostgresIntegrationRecordDeletionV1OwnerDigestAuthority(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	const statement = `select record_platform_internal.record_portability_v1_owner_digest(
		$1, jsonb_build_array('rxa_owner'), jsonb_build_array('s3','sha256/key',$2::text,$3::text), jsonb_build_array('rej_owner'))`
	var first, repeated, changedVersion, changedCreation []byte
	for _, tc := range []struct {
		version string
		created string
		out     *[]byte
	}{
		{"version-a", "2025-01-01T00:00:00.000000Z", &first},
		{"version-a", "2025-01-01T00:00:00.000000Z", &repeated},
		{"version-b", "2025-01-01T00:00:00.000000Z", &changedVersion},
		{"version-a", "2025-01-01T00:00:01.000000Z", &changedCreation},
	} {
		if err := fixture.db.QueryRow(ctx, statement, "record_export_artifacts", tc.version, tc.created).Scan(tc.out); err != nil {
			t.Fatal(err)
		}
	}
	if len(first) != sha256.Size || !bytes.Equal(first, repeated) || bytes.Equal(first, changedVersion) || bytes.Equal(first, changedCreation) {
		t.Fatal("owner digest did not preserve physical identity")
	}
	if err := fixture.db.QueryRow(ctx, statement, "blob_gc_pins", "version-a", "2025-01-01T00:00:00.000000Z").Scan(&repeated); err == nil {
		t.Fatal("non-owner table accepted as retained evidence")
	}
	worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-owner-digest-worker", 1)
	err := worker.QueryRow(ctx, statement, "record_export_artifacts", "version-a", "2025-01-01T00:00:00.000000Z").Scan(&repeated)
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("worker private owner digest = %v, want permission denied", err)
	}
}

// portabilityV1TestFixture produces the physical source through the same
// managed writer that records the endpoint and bucket on the obligation.
type portabilityV1TestFixture struct {
	ctx                 context.Context
	db                  recordPlatformPostgresFixture
	worker              *pgxpool.Pool
	client              *minio.Client
	bucket              string
	blob                *attachments.S3BlobStore
	written             attachments.WrittenBlob
	content             []byte
	current             string
	neighbor            string
	target              recorddeletion.PurgeTarget
	expectedArtifacts   string
	expectedObligations string
	expectedOwners      string
}

var portabilityV1TestLostPut = errors.New("test: S3 Put response lost after physical write")

type portabilityV1TestAmbiguousPut struct {
	*attachments.S3BlobStore
	version attachments.ObjectVersion
}

func (b *portabilityV1TestAmbiguousPut) Put(ctx context.Context, request attachments.PutRequest, body io.Reader) (attachments.ObjectVersion, error) {
	version, err := b.S3BlobStore.Put(ctx, request, body)
	if err != nil {
		return version, err
	}
	b.version = version
	return version, portabilityV1TestLostPut
}

func newPortabilityV1TestFixture(t *testing.T, publish bool) *portabilityV1TestFixture {
	return newPortabilityV1TestFixtureMode(t, publish, false)
}

func newPortabilityV1TestFixtureMode(t *testing.T, publish, ambiguous bool) *portabilityV1TestFixture {
	t.Helper()
	f := &portabilityV1TestFixture{ctx: context.Background(), content: []byte("physical V1 portability source\n")}
	f.db = newAdmittedCoreRecordFixture(t, f.ctx)
	record := seedRecordWithoutSearchProjection(t, f.ctx, f.db, "V1 S3 portability")
	runtime := f.db.openDirectRuntimePool(t, f.ctx, "v1-portability-source", 2)
	f.client, f.bucket = newAttachmentUploadWorkflowMinIO(t)
	var err error
	f.blob, err = attachments.NewS3BlobStore(f.client, f.bucket)
	if err != nil {
		t.Fatal(err)
	}
	var source attachments.BlobStore = f.blob
	fault := &portabilityV1TestAmbiguousPut{S3BlobStore: f.blob}
	if ambiguous {
		source = fault
	}
	writer, err := attachments.NewManagedBlobWriter(source, attachments.BackendKindS3,
		NewPostgresLocalBlobRepository(runtime, localBlobFixtureWitness(t, runtime), nil))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(f.content)
	const job = "rej_portabilitys3"
	var expiry time.Time
	if err := f.db.db.QueryRow(f.ctx, `insert into public.record_export_jobs
		(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,inventory_digest,
		 authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
		values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'markdown','safe','staging',$2,$2,1,$3,$4,'source_worker',1,
		 clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour') returning expires_at`, job, digest[:], record.RecordID, record.RevisionID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	temporary, err := attachments.NewBlobTemporaryKey()
	if err != nil {
		t.Fatal(err)
	}
	f.written, err = writer.Put(f.ctx, attachments.WriteRequest{ProjectID: "default", Origin: attachments.WriteOrigin{Kind: "export_artifact", ID: job, Generation: 1}, Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(f.content)), TemporaryKey: temporary}}, bytes.NewReader(f.content))
	if ambiguous {
		if !errors.Is(err, portabilityV1TestLostPut) || fault.version.Validate() != nil {
			t.Fatalf("ambiguous managed write: %v", err)
		}
		f.written.Version = fault.version
		var binding string
		if err := f.db.db.QueryRow(f.ctx, `select obligation_id,encode(storage_binding,'hex') from public.record_write_obligations where origin_kind='export_artifact' and origin_id=$1 and outcome='remote_unknown' and returned_at is not null and object_version=$2`, job, fault.version.VersionID).Scan(&f.written.ObligationID, &binding); err != nil {
			t.Fatal(err)
		}
		route, err := f.blob.GCStorageIdentity()
		if err != nil || binding != hex.EncodeToString(route[:]) {
			t.Fatalf("ambiguous write lost original route: %v", err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if publish {
		command, err := json.Marshal(map[string]any{"artifact_id": "rxa_portabilitys3", "job_id": job, "kind": "markdown", "content_type": "text/markdown", "backend_kind": "s3", "namespace_id": nil, "key": f.written.Version.Key, "version": f.written.Version.VersionID, "sha256": hex.EncodeToString(digest[:]), "size": len(f.content), "expires_at": expiry, "obligation_id": f.written.ObligationID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Exec(f.ctx, `select public.record_export_artifact_publish($1)`, command); err != nil {
			t.Fatal(err)
		}
	}
	currentBytes := []byte("new current version")
	current, err := f.client.PutObject(f.ctx, f.bucket, f.written.Version.Key, bytes.NewReader(currentBytes), int64(len(currentBytes)), minio.PutObjectOptions{DisableMultipart: true})
	if err != nil || current.VersionID == "" || current.VersionID == f.written.Version.VersionID {
		t.Fatalf("new current version: %v", err)
	}
	f.current = current.VersionID
	f.neighbor = "neighbor/portability-v1"
	if _, err := f.client.PutObject(f.ctx, f.bucket, f.neighbor, bytes.NewReader([]byte("neighbor bytes")), 14, minio.PutObjectOptions{DisableMultipart: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.db.Exec(f.ctx, `insert into public.attachment_quota_accounts (project_id,physical_bytes,quota_version) values ('default',$1,9) on conflict (project_id) do update set physical_bytes=excluded.physical_bytes,quota_version=excluded.quota_version`, f.written.Version.SizeBytes); err != nil {
		t.Fatal(err)
	}
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_portabilitys3", ReservationID: "drs_portabilitys3", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12, LedgerSequence: 16, LedgerEntryHash: testStoreRecordPlatformDigest(0xa4)}
	f.target = seedVerifiedS3AttachmentDeletionOperation(t, f.ctx, f.db, record, op)
	f.worker = f.db.openDirectRolePool(t, f.ctx, f.db.deletionWorker, "v1-portability-worker", 2)
	f.captureSources(t)
	return f
}

func (f *portabilityV1TestFixture) repository(t *testing.T, blob attachments.BlobGCDeleter) *PostgresRecordDeletionRepository {
	t.Helper()
	r := NewPostgresRecordDeletionRepository(f.worker, allowRecordPlatformAdmissionGate)
	if blob != nil {
		if err := r.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, blob); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (f *portabilityV1TestFixture) survivors(t *testing.T, deleted bool) {
	t.Helper()
	_, err := f.client.StatObject(f.ctx, f.bucket, f.written.Version.Key, minio.StatObjectOptions{VersionID: f.written.Version.VersionID})
	if deleted {
		response := minio.ToErrorResponse(err)
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("historical version still accessible: %v", err)
		}
	} else if err != nil {
		t.Fatalf("historical version unavailable: %v", err)
	}
	for _, item := range []struct{ key, version, value string }{{f.written.Version.Key, f.current, "new current version"}, {f.neighbor, "", "neighbor bytes"}} {
		object, err := f.client.GetObject(f.ctx, f.bucket, item.key, minio.GetObjectOptions{VersionID: item.version})
		if err != nil {
			t.Fatal(err)
		}
		content, readErr := io.ReadAll(object)
		closeErr := object.Close()
		if readErr != nil || closeErr != nil || string(content) != item.value {
			t.Fatalf("survivor %q: %q, %v, %v", item.key, content, readErr, closeErr)
		}
	}
}

// Capture the complete job source set before prepare removes any metadata.
func (f *portabilityV1TestFixture) captureSources(t *testing.T) {
	t.Helper()
	err := f.db.db.QueryRow(f.ctx, `select
		coalesce((select jsonb_agg(jsonb_build_array(export_artifact_id,export_job_id,artifact_kind,backend_kind,blob_key,object_version_id,encode(sha256,'hex'),byte_size,to_jsonb(created_at at time zone 'UTC'),namespace_id,obligation_id) order by export_artifact_id collate "C") from public.record_export_artifacts where export_job_id='rej_portabilitys3'),'[]'::jsonb)::text,
		coalesce((select jsonb_agg(to_jsonb(o) order by obligation_id collate "C") from public.record_write_obligations o where origin_kind='export_artifact' and origin_id='rej_portabilitys3'),'[]'::jsonb)::text`).Scan(&f.expectedArtifacts, &f.expectedObligations)
	if err != nil {
		t.Fatal(err)
	}
	f.captureOwners(t)
}

func (f *portabilityV1TestFixture) captureOwners(t *testing.T) {
	t.Helper()
	err := f.db.db.QueryRow(f.ctx, `with plans as (select distinct p.import_plan_id,p.import_job_id from public.record_import_plans p join public.record_import_entity_mappings m on m.import_plan_id=p.import_plan_id where m.entity_kind='record' and m.target_id=$1)
		select jsonb_build_object(
		'export_jobs',coalesce((select jsonb_agg(jsonb_build_array(j.export_job_id,j.project_id,j.record_id,j.export_kind) order by j.export_job_id collate "C") from public.record_export_jobs j where j.record_id=$1),'[]'::jsonb),
		'import_jobs',coalesce((select jsonb_agg(jsonb_build_array(j.import_job_id,j.project_id) order by j.import_job_id collate "C") from public.record_import_jobs j where exists(select 1 from plans p where p.import_job_id=j.import_job_id)),'[]'::jsonb),
		'import_plans',coalesce((select jsonb_agg(jsonb_build_array(p.import_plan_id,p.import_job_id) order by p.import_plan_id collate "C") from plans p),'[]'::jsonb),
		'mappings',coalesce((select jsonb_agg(jsonb_build_array(m.import_plan_id,m.entity_kind,encode(m.source_identity_digest,'hex'),m.source_id,m.target_id) order by m.import_plan_id collate "C",m.entity_kind collate "C",encode(m.source_identity_digest,'hex') collate "C",m.source_id collate "C",m.target_id collate "C") from public.record_import_entity_mappings m where exists(select 1 from plans p where p.import_plan_id=m.import_plan_id)),'[]'::jsonb),
		'origins',coalesce((select jsonb_agg(jsonb_build_array(o.origin_id,o.project_id,o.origin_kind,encode(o.origin_digest,'hex'),o.source_record_id) order by o.origin_id collate "C") from public.record_origins o where o.source_record_id=$1),'[]'::jsonb))::text`, f.target.Operation.Object.ObjectID).Scan(&f.expectedOwners)
	if err != nil {
		t.Fatal(err)
	}
}

func (f *portabilityV1TestFixture) checkSources(t *testing.T) {
	t.Helper()
	var artifacts, obligations, owners string
	var invalid int
	err := f.db.db.QueryRow(f.ctx, `select
		coalesce((select jsonb_agg(jsonb_build_array(a.artifact_id,a.job_id,a.artifact_role,a.backend_kind,a.blob_key,a.object_version_id,encode(a.sha256,'hex'),a.byte_size,to_jsonb(a.created_at at time zone 'UTC'),a.namespace_id,a.obligation_id) order by a.artifact_id collate "C") from public.record_portability_v1_artifacts a where a.operation_id=$1 and a.source_kind='export'),'[]'::jsonb)::text,
		coalesce((select jsonb_agg(o.snapshot order by o.obligation_id collate "C") from public.record_portability_v1_obligations o where o.operation_id=$1),'[]'::jsonb)::text,
		(select count(*) from public.record_portability_v1_obligations o join public.record_portability_v1_targets t using(operation_id,target_id) where o.operation_id=$1 and (o.obligation_id is distinct from o.snapshot->>'obligation_id' or t.backend_kind is distinct from o.snapshot->>'backend_kind' or t.blob_key is distinct from o.snapshot->>'canonical_key' or (t.disposition='s3' and (t.object_version_id is distinct from o.snapshot->>'object_version' or encode(t.sha256,'hex') is distinct from substring(o.snapshot->>'sha256_digest' from 3) or t.byte_size is distinct from (o.snapshot->>'size_bytes')::bigint or encode(t.storage_binding,'hex') is distinct from substring(o.snapshot->>'storage_binding' from 3))))) +
		(select count(*) from public.record_portability_v1_artifacts a join public.record_portability_v1_targets t using(operation_id,target_id) where a.operation_id=$1 and (a.backend_kind is distinct from t.backend_kind or a.blob_key is distinct from t.blob_key or (t.disposition='s3' and (a.object_version_id is distinct from t.object_version_id or a.sha256 is distinct from t.sha256 or a.byte_size is distinct from t.byte_size)))),
		(select p.owners::text from public.record_portability_v1_preparations p where p.operation_id=$1)`, f.target.Operation.OperationID).Scan(&artifacts, &obligations, &invalid, &owners)
	if err != nil || artifacts != f.expectedArtifacts || obligations != f.expectedObligations || owners != f.expectedOwners || invalid != 0 {
		t.Fatalf("manifest differs from captured source sets or membership: artifact=%t obligation=%t owners=%t invalid=%d error=%v", artifacts == f.expectedArtifacts, obligations == f.expectedObligations, owners == f.expectedOwners, invalid, err)
	}
	rows, err := f.db.db.Query(f.ctx, `select target_id,jsonb_build_array('HOUFENG-PORTABILITY-TARGET-V1',operation_id,backend_kind,blob_key,object_version_id,namespace_id,incarnation_id,encode(storage_binding,'hex'))::text from public.record_portability_v1_targets where operation_id=$1 order by target_id collate "C"`, f.target.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, frame string
		if err := rows.Scan(&id, &frame); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(frame))
		if id != "bgd_"+hex.EncodeToString(digest[:]) {
			t.Fatalf("target identifier does not match captured physical tuple: %q", id)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// The database only renders jsonb's canonical text here; Go hashes the
// independently assembled saved columns rather than calling the product digest.
func (f *portabilityV1TestFixture) checkDigest(t *testing.T, prep portabilityV1Preparation) {
	t.Helper()
	var frame string
	var stored []byte
	err := f.db.db.QueryRow(f.ctx, `select jsonb_build_array('HOUFENG-PORTABILITY-INVENTORY-V1',p.operation_id,p.project_id,p.record_id,
		encode(p.surface_digest,'hex'),encode(p.ledger_domain,'hex'),p.ledger_sequence,encode(p.ledger_entry_hash,'hex'),encode(p.witness_proof_digest,'hex'),
		p.owners,p.removed_rows,p.pdf_pending,
		coalesce((select jsonb_agg(jsonb_build_array(a.operation_id,a.source_kind,a.artifact_id,a.job_id,a.artifact_role,a.backend_kind,a.blob_key,a.object_version_id,encode(a.sha256,'hex'),a.byte_size,to_jsonb(a.created_at at time zone 'UTC'),a.namespace_id,a.incarnation_id,a.obligation_id,a.target_id) order by a.source_kind collate "C",a.artifact_id collate "C") from public.record_portability_v1_artifacts a where a.operation_id=p.operation_id),'[]'::jsonb),
		coalesce((select jsonb_agg(jsonb_build_array(o.operation_id,o.obligation_id,o.snapshot,o.target_id) order by o.obligation_id collate "C") from public.record_portability_v1_obligations o where o.operation_id=p.operation_id),'[]'::jsonb),
		coalesce((select jsonb_agg(jsonb_build_array(t.operation_id,t.target_id,t.backend_kind,t.blob_key,t.object_version_id,encode(t.sha256,'hex'),t.byte_size,to_jsonb(t.blob_created_at at time zone 'UTC'),t.namespace_id,t.incarnation_id,t.obligation_id,encode(t.storage_binding,'hex'),t.disposition,t.retained_evidence,t.local_deletion_id) order by t.target_id collate "C") from public.record_portability_v1_targets t where t.operation_id=p.operation_id),'[]'::jsonb))::text,p.inventory_digest
		from public.record_portability_v1_preparations p where p.operation_id=$1`, f.target.Operation.OperationID).Scan(&frame, &stored)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(frame))
	if !bytes.Equal(digest[:], stored) || hex.EncodeToString(digest[:]) != prep.InventoryDigest {
		t.Fatal("saved inventory framing or digest differs")
	}
	f.checkSources(t)
}

func (f *portabilityV1TestFixture) checkInventory(t *testing.T, prep portabilityV1Preparation, artifacts int) {
	t.Helper()
	f.checkDigest(t, prep)
	var sourceCount, artifactCount, targetCount int
	identity, err := f.blob.GCStorageIdentity()
	if err != nil {
		t.Fatal(err)
	}
	err = f.db.db.QueryRow(f.ctx, `select
		(select count(*) from public.record_portability_v1_obligations o join public.record_portability_v1_targets t using(operation_id,target_id)
		 where o.operation_id=$1 and o.obligation_id=$2 and o.snapshot->>'obligation_id'=$2
		 and o.snapshot->>'canonical_key'=$3 and o.snapshot->>'object_version'=$4
		 and t.blob_key=$3 and t.object_version_id=$4 and t.storage_binding=$5 and t.disposition='s3'),
		(select count(*) from public.record_portability_v1_artifacts a join public.record_portability_v1_targets t using(operation_id,target_id)
		 where a.operation_id=$1 and a.obligation_id=$2 and a.blob_key=$3 and a.object_version_id=$4 and t.blob_key=a.blob_key and t.object_version_id=a.object_version_id),
		(select count(*) from public.record_portability_v1_targets where operation_id=$1 and disposition='s3')`, f.target.Operation.OperationID, f.written.ObligationID, f.written.Version.Key, f.written.Version.VersionID, identity[:]).Scan(&sourceCount, &artifactCount, &targetCount)
	if err != nil || sourceCount != 1 || artifactCount != artifacts || targetCount != 1 || len(prep.S3Targets) != 1 {
		t.Fatalf("inventory membership source=%d artifact=%d target=%d projection=%v: %v", sourceCount, artifactCount, targetCount, prep.S3Targets, err)
	}
}

type portabilityV1TestObserver struct {
	store  *attachments.S3BlobStore
	calls  int
	before func(attachments.ObjectVersion) error
}

func (b *portabilityV1TestObserver) GCStorageIdentity() ([32]byte, error) {
	return b.store.GCStorageIdentity()
}
func (b *portabilityV1TestObserver) DeleteGCVersion(ctx context.Context, version attachments.ObjectVersion) (attachments.DeletionReceipt, error) {
	b.calls++
	if b.before != nil {
		if err := b.before(version); err != nil {
			return attachments.DeletionReceipt{}, err
		}
	}
	return b.store.DeleteGCVersion(ctx, version)
}

type portabilityV1TestGCObservedTx struct {
	attachmentTx
	state *string
}
type portabilityV1TestGCObservedRow struct {
	pgx.Row
	state *string
}

func (tx *portabilityV1TestGCObservedTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return &portabilityV1TestGCObservedRow{Row: tx.attachmentTx.QueryRow(ctx, query, args...), state: tx.state}
}
func (row *portabilityV1TestGCObservedRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		*row.state = pgErr.Code
	}
	return err
}

type portabilityV1TestRouteDrift struct {
	original, alternate *attachments.S3BlobStore
	afterDelete         bool
	calls               int
}

func (b *portabilityV1TestRouteDrift) GCStorageIdentity() ([32]byte, error) {
	if b.afterDelete {
		return b.alternate.GCStorageIdentity()
	}
	return b.original.GCStorageIdentity()
}
func (b *portabilityV1TestRouteDrift) DeleteGCVersion(ctx context.Context, version attachments.ObjectVersion) (attachments.DeletionReceipt, error) {
	b.calls++
	receipt, err := b.original.DeleteGCVersion(ctx, version)
	b.afterDelete = true
	return receipt, err
}

type portabilityV1TestLostCommit struct {
	pgx.Tx
	query         string
	lost          *bool
	selected      bool
	resolved      *int
	rejectResolve bool
}

func (tx *portabilityV1TestLostCommit) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if query == tx.query && !*tx.lost {
		tx.selected = true
	}
	if *tx.lost && (query == `select public.record_portability_purge_prepare_resolve_v1($1)` || query == `select public.record_portability_gc_claim_resolve_v1($1)`) {
		*tx.resolved++
		if tx.rejectResolve {
			return portabilityV1TestErrorRow{}
		}
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}
func (tx *portabilityV1TestLostCommit) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	if tx.selected {
		*tx.lost = true
		return errors.New("test: committed response lost")
	}
	return nil
}

type portabilityV1TestErrorRow struct{}

func (portabilityV1TestErrorRow) Scan(...any) error {
	return errors.New("test: committed readback unavailable")
}

func (f *portabilityV1TestFixture) assertNoAttachmentGC(t *testing.T) {
	t.Helper()
	var claims, receipts int
	var physical, version int64
	if err := f.db.db.QueryRow(f.ctx, `select (select count(*) from public.blob_gc_deletions where operation_id=$1 or (blob_key=$2 and object_version=$3)),(select count(*) from public.attachment_purge_receipts where operation_id=$1),physical_bytes,quota_version from public.attachment_quota_accounts where project_id='default'`, f.target.Operation.OperationID, f.written.Version.Key, f.written.Version.VersionID).Scan(&claims, &receipts, &physical, &version); err != nil {
		t.Fatal(err)
	}
	if claims != 0 || receipts != 0 || physical != f.written.Version.SizeBytes || version != 9 {
		t.Fatalf("private S3 portability entered attachment GC/accounting: claims=%d receipts=%d physical=%d version=%d", claims, receipts, physical, version)
	}
}

// Private S3 completion is proved by the exact manifest target. It must not
// manufacture completion or quota facts on the original write obligation.
func (f *portabilityV1TestFixture) obligationCleanupFields(t *testing.T) string {
	t.Helper()
	var fields string
	if err := f.db.db.QueryRow(f.ctx, `select jsonb_build_array(cleanup_owner_id,cleanup_generation,
		to_jsonb(cleanup_expires_at at time zone 'UTC'),cleanup_dispatch_id,cleanup_outcome,encode(cleanup_receipt,'hex'),
		to_jsonb(cleanup_completed_at at time zone 'UTC'),quota_settlement_id,to_jsonb(quota_settled_at at time zone 'UTC'))::text
		from public.record_write_obligations where obligation_id=$1`, f.written.ObligationID).Scan(&fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func (f *portabilityV1TestFixture) assertPrivateCompletion(t *testing.T, targetID, before string) {
	t.Helper()
	if after := f.obligationCleanupFields(t); after != before {
		t.Fatalf("private S3 completion changed original cleanup/quota fields: before=%s after=%s", before, after)
	}
	route, err := f.blob.GCStorageIdentity()
	if err != nil {
		t.Fatal(err)
	}
	var proof bool
	if err := f.db.db.QueryRow(f.ctx, `select exists(select 1 from public.record_portability_v1_targets t
		join public.record_portability_v1_obligations o on (o.operation_id,o.target_id)=(t.operation_id,t.target_id)
		where t.operation_id=$1 and t.target_id=$2 and o.obligation_id=$3
		and t.backend_kind='s3' and t.blob_key=$4 and t.object_version_id=$5 and t.sha256=$6 and t.byte_size=$7 and t.storage_binding=$8
		and t.deletion_state='completed' and t.physical_delete_result in ('deleted','already_absent')
		and octet_length(t.receipt_digest)=32 and t.completed_at is not null)`, f.target.Operation.OperationID, targetID,
		f.written.ObligationID, f.written.Version.Key, f.written.Version.VersionID, f.written.Version.SHA256[:], f.written.Version.SizeBytes, route[:]).Scan(&proof); err != nil || !proof {
		t.Fatalf("missing exact source-linked private S3 completion proof: %v %v", proof, err)
	}
	f.assertNoAttachmentGC(t)
}

type portabilityV1TestBarrier struct {
	*v1AfterDeleteBarrier
	store *attachments.S3BlobStore
}

func (b *portabilityV1TestBarrier) GCStorageIdentity() ([32]byte, error) {
	return b.store.GCStorageIdentity()
}

func TestPostgresMinIOIntegrationRecordDeletionV1Portability(t *testing.T) {
	if os.Getenv("HOUFENG_PORTABILITY_V1_CHILD") == "1" {
		portabilityV1TestChild(t)
		return
	}
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("PostgreSQL and MinIO integration required")
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	t.Run("historical_version_preserves_current", func(t *testing.T) {
		for _, published := range []bool{true, false} {
			name := "artifact"
			if !published {
				name = "obligation_only"
			}
			t.Run(name, func(t *testing.T) {
				f := newPortabilityV1TestFixture(t, published)
				observer := &portabilityV1TestObserver{store: f.blob}
				repo := f.repository(t, observer)
				prep, err := repo.preparePortabilityV1(f.ctx, f.target, surface)
				if err != nil || prep.UnresolvedCount != 0 {
					t.Fatalf("prepare: %+v %v", prep, err)
				}
				artifacts := 0
				if published {
					artifacts = 1
				}
				f.checkInventory(t, prep, artifacts)
				beforeCleanup := f.obligationCleanupFields(t)
				f.survivors(t, false)
				receipt, err := repo.PurgeRecordPortability(f.ctx, f.target, surface)
				if err != nil {
					t.Fatal(err)
				}
				if observer.calls != 1 {
					t.Fatalf("deletes = %d", observer.calls)
				}
				f.survivors(t, true)
				f.assertNoAttachmentGC(t)
				f.assertPrivateCompletion(t, prep.S3Targets[0], beforeCleanup)
				replay, err := repo.PurgeRecordPortability(f.ctx, f.target, surface)
				if err != nil || replay != receipt || observer.calls != 1 {
					t.Fatalf("replay: %v calls=%d", err, observer.calls)
				}
				f.assertPrivateCompletion(t, prep.S3Targets[0], beforeCleanup)
			})
		}
	})
	t.Run("same_object_artifact_dedup", func(t *testing.T) {
		f := newPortabilityV1TestFixture(t, true)
		// A second historical artifact row points at the already produced and
		// witnessed physical version; it does not synthesize physical evidence.
		if _, err := f.db.db.Exec(f.ctx, `insert into public.record_export_artifacts
			(export_artifact_id,export_job_id,artifact_kind,content_type,backend_kind,blob_key,sha256,byte_size,expires_at,created_at,object_version_id,namespace_id,obligation_id)
			select 'rxa_portabilitycopy',export_job_id,'comparison_json','application/json',backend_kind,blob_key,sha256,byte_size,expires_at,created_at,object_version_id,namespace_id,obligation_id
			from public.record_export_artifacts where export_artifact_id='rxa_portabilitys3'`); err != nil {
			t.Fatal(err)
		}
		f.captureSources(t)
		observer := &portabilityV1TestObserver{store: f.blob}
		repo := f.repository(t, observer)
		prep, err := repo.preparePortabilityV1(f.ctx, f.target, surface)
		if err != nil || prep.UnresolvedCount != 0 {
			t.Fatalf("dedup prepare: %+v %v", prep, err)
		}
		f.checkInventory(t, prep, 2)
		if _, err := repo.PurgeRecordPortability(f.ctx, f.target, surface); err != nil {
			t.Fatal(err)
		}
		if observer.calls != 1 {
			t.Fatalf("deduplicated source issued %d deletes", observer.calls)
		}
		f.assertNoAttachmentGC(t)
		f.survivors(t, true)
	})
	t.Run("shared_archive_final_handoff", func(t *testing.T) { portabilityV1TestSharedArchive(t) })
	t.Run("independent_same_version_owners", runPortabilityV1IndependentSameVersionOwners)
	t.Run("prepare_commit_unknown", func(t *testing.T) {
		for _, stage := range []string{"prepare", "claim"} {
			t.Run(stage, func(t *testing.T) {
				f := newPortabilityV1TestFixture(t, true)
				missing := f.repository(t, nil)
				if _, err := missing.preparePortabilityV1(f.ctx, f.target, surface); err == nil {
					t.Fatal("fresh missing S3 configuration accepted")
				}
				var preps, sources int
				if err := f.db.db.QueryRow(f.ctx, `select (select count(*) from public.record_portability_v1_preparations where operation_id=$1),(select count(*) from public.record_export_artifacts where export_artifact_id='rxa_portabilitys3')`, f.target.Operation.OperationID).Scan(&preps, &sources); err != nil || preps != 0 || sources != 1 {
					t.Fatalf("missing configuration changed source: prep=%d source=%d %v", preps, sources, err)
				}
				lost, resolved := false, 0
				observer := &portabilityV1TestObserver{store: f.blob, before: func(v attachments.ObjectVersion) error {
					if !lost || resolved == 0 || v != f.written.Version {
						return errors.New("delete before committed readback or wrong identity")
					}
					return nil
				}}
				repo := f.repository(t, observer)
				query := `select public.record_portability_purge_prepare_v1($1)`
				if stage == "claim" {
					query = `select public.record_portability_gc_claim_v1($1)`
				}
				repo.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
					tx, err := f.worker.BeginTx(ctx, options)
					if err != nil {
						return nil, err
					}
					return &portabilityV1TestLostCommit{Tx: tx, query: query, lost: &lost, resolved: &resolved}, nil
				}
				if _, err := repo.PurgeRecordPortability(f.ctx, f.target, surface); err != nil {
					t.Fatal(err)
				}
				if !lost || resolved != 1 || observer.calls != 1 {
					t.Fatalf("unknown %s: lost=%v resolved=%d calls=%d", stage, lost, resolved, observer.calls)
				}
				f.survivors(t, true)
				f.assertNoAttachmentGC(t)
			})
		}
	})
	t.Run("unconfirmed_candidate_no_io", func(t *testing.T) {
		for _, stage := range []string{"prepare", "claim"} {
			t.Run(stage, func(t *testing.T) {
				f := newPortabilityV1TestFixture(t, true)
				observer := &portabilityV1TestObserver{store: f.blob}
				repo := f.repository(t, observer)
				lost, resolved := false, 0
				query := `select public.record_portability_purge_prepare_v1($1)`
				if stage == "claim" {
					query = `select public.record_portability_gc_claim_v1($1)`
				}
				repo.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
					tx, err := f.worker.BeginTx(ctx, options)
					if err != nil {
						return nil, err
					}
					return &portabilityV1TestLostCommit{Tx: tx, query: query, lost: &lost, resolved: &resolved, rejectResolve: true}, nil
				}
				if _, err := repo.PurgeRecordPortability(f.ctx, f.target, surface); err == nil || !lost || resolved != 1 || observer.calls != 0 {
					t.Fatalf("unconfirmed %s: lost=%v readback=%d calls=%d error=%v", stage, lost, resolved, observer.calls, err)
				}
				f.survivors(t, false)
				f.assertNoAttachmentGC(t)
				restarted := f.repository(t, observer)
				if _, err := restarted.PurgeRecordPortability(f.ctx, f.target, surface); err != nil || observer.calls != 1 {
					t.Fatalf("confirmed restart: calls=%d error=%v", observer.calls, err)
				}
				f.survivors(t, true)
			})
		}
	})
	t.Run("unresolved_sources_no_dispatch", func(t *testing.T) {
		for _, legacy := range []bool{true, false} {
			name := "remote_unknown"
			if legacy {
				name = "legacy_null_route_and_version"
			}
			t.Run(name, func(t *testing.T) {
				f := newPortabilityV1TestFixtureMode(t, false, !legacy)
				unknownID := f.written.ObligationID
				if legacy {
					unknownID = "rwo_77777777777777777777777777777777"
					if _, err := f.db.db.Exec(f.ctx, `insert into public.record_write_obligations
					(obligation_id,writer_instance,request_seq,writer_principal,project_id,origin_kind,origin_id,origin_generation,
					 namespace_id,incarnation_id,backend_kind,storage_binding,canonical_key,sha256_digest,size_bytes,record_id,record_epoch,
					 registration_command,dispatch_id,dispatched_at,outcome,quota_settlement_id)
					select $2,writer_instance,request_seq+10000,writer_principal,project_id,origin_kind,origin_id,origin_generation,
					 namespace_id,incarnation_id,backend_kind,case when $3 then null else storage_binding end,canonical_key,sha256_digest,size_bytes,record_id,record_epoch,
					 convert_to((convert_from(registration_command,'UTF8')::jsonb || jsonb_build_object('obligation_id',$2::text,'request_seq',request_seq+10000,'storage_binding',case when $3 then null else encode(storage_binding,'hex') end))::text,'UTF8'),
					 dispatch_id,dispatched_at,'remote_unknown',$2 from public.record_write_obligations where obligation_id=$1`, f.written.ObligationID, unknownID, legacy); err != nil {
						t.Fatal(err)
					}
				}
				v := f.written.Version
				if _, err := f.db.db.Exec(f.ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,created_at) values($1,$2,$3,$4,'s3',clock_timestamp()-interval '25 hours')`, v.Key, v.SHA256[:], v.VersionID, v.SizeBytes); err != nil {
					t.Fatal(err)
				}
				f.captureSources(t)
				observer := &portabilityV1TestObserver{store: f.blob}
				repo := f.repository(t, observer)
				prep, err := repo.preparePortabilityV1(f.ctx, f.target, surface)
				if err != nil || prep.UnresolvedCount == 0 || len(prep.S3Targets) != 0 {
					t.Fatalf("unknown prepare: %+v %v", prep, err)
				}
				f.checkDigest(t, prep)
				var count, sourceJobs int
				if err := f.db.db.QueryRow(f.ctx, `select (select count(*) from public.record_portability_v1_obligations o join public.record_portability_v1_targets t using(operation_id,target_id) join public.record_write_obligations source on source.obligation_id=o.obligation_id where o.operation_id=$1 and o.obligation_id=$2 and o.snapshot=to_jsonb(source) and o.snapshot->>'outcome'='remote_unknown' and (($3 and o.snapshot->'object_version'='null'::jsonb and o.snapshot->'storage_binding'='null'::jsonb) or (not $3 and o.snapshot->>'object_version'=$4 and o.snapshot->>'storage_binding' is not null)) and t.blob_key=source.canonical_key and t.disposition='unresolved'),(select count(*) from public.record_export_jobs where export_job_id='rej_portabilitys3')`, f.target.Operation.OperationID, unknownID, legacy, f.written.Version.VersionID).Scan(&count, &sourceJobs); err != nil || count != 1 || sourceJobs != 1 {
					t.Fatalf("unknown source not preserved: snapshot=%d job=%d %v", count, sourceJobs, err)
				}
				var id string
				if err := f.db.db.QueryRow(f.ctx, `select target_id from public.record_portability_v1_obligations where operation_id=$1 and obligation_id=$2`, f.target.Operation.OperationID, unknownID).Scan(&id); err != nil {
					t.Fatal(err)
				}
				claimRequest := struct {
					DeletionID     string `json:"deletion_id"`
					CleanupOwnerID string `json:"cleanup_owner_id"`
					CleanupLeaseMS int64  `json:"cleanup_lease_ms"`
				}{id, f.target.OwnerClaim.Owner.OwnerID, 60000}
				if _, err := repo.portabilityV1S3Command(f.ctx, f.target, surface, `select public.record_portability_gc_claim_v1($1)`, claimRequest); err == nil {
					t.Fatal("unresolved manifest authorized direct S3 claim")
				}
				runtime := f.db.openDirectRuntimePool(t, f.ctx, "portability-ordinary-s3-gc", 2)
				ordinary := NewPostgresAttachmentRepository(runtime)
				var observedSQLState string
				ordinary.beginTx = func(ctx context.Context, options pgx.TxOptions) (attachmentTx, error) {
					tx, err := runtime.BeginTx(ctx, options)
					if err != nil {
						return nil, err
					}
					return &portabilityV1TestGCObservedTx{attachmentTx: tx, state: &observedSQLState}, nil
				}
				object := attachmentBlobObjectFromVersion(f.written.Version, attachments.BackendKindS3)
				for _, request := range []attachments.BlobGCClaimRequest{
					attachmentBlobGCPermanentClaimRequest(object, "ordinary_exact"),
					{ProjectID: "default", BackendKind: attachments.BackendKindS3, Mode: attachments.BlobGCPurgeModeOrdinary, OrphanedBefore: time.Now().UTC().Add(-24 * time.Hour), OwnerID: "ordinary_aged", OwnerLeaseDuration: attachments.DefaultBlobGCLeaseDuration},
				} {
					observedSQLState = ""
					claim, err := ordinary.ClaimBlobGC(f.ctx, request)
					if claim != nil || !errors.Is(err, attachments.ErrAttachmentConflict) || observedSQLState != "55000" {
						t.Fatalf("ineligible manifest allowed alternate S3 claim: %+v SQLSTATE=%q %v", claim, observedSQLState, err)
					}
				}
				var present bool
				if err := f.db.db.QueryRow(f.ctx, `select exists(select 1 from public.blob_objects where blob_key=$1 and object_version=$2)`, v.Key, v.VersionID).Scan(&present); err != nil || !present {
					t.Fatalf("alternate S3 claim changed source: %v %v", present, err)
				}
				if _, err := repo.PurgeRecordPortability(f.ctx, f.target, surface); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
					t.Fatalf("unknown purge: %v", err)
				}
				if observer.calls != 0 {
					t.Fatalf("unknown source dispatched %d deletes", observer.calls)
				}
				f.assertNoAttachmentGC(t)
				var changed bool
				if err := f.db.db.QueryRow(f.ctx, `select exists(select 1 from public.record_portability_v1_targets where operation_id=$1 and (deletion_state in ('claimed','retry_wait','completed') or cleanup_owner_id is not null or receipt_digest is not null or completed_at is not null))`, f.target.Operation.OperationID).Scan(&changed); err != nil || changed {
					t.Fatalf("ineligible manifest gained claim or receipt: %v %v", changed, err)
				}
				f.checkDigest(t, prep)
				f.survivors(t, false)
			})
		}
	})
	t.Run("claim_readback_and_tamper", func(t *testing.T) {
		f := newPortabilityV1TestFixture(t, true)
		repo := f.repository(t, f.blob)
		prep, err := repo.preparePortabilityV1(f.ctx, f.target, surface)
		if err != nil || len(prep.S3Targets) != 1 {
			t.Fatalf("prepare: %+v %v", prep, err)
		}
		f.checkInventory(t, prep, 1)
		for _, role := range []string{f.db.runtime, f.db.admin} {
			pool := f.db.openDirectRolePool(t, f.ctx, role, "portability-s3-denied", 1)
			_, err := pool.Exec(f.ctx, `select public.record_portability_gc_claim_v1($1)`, []byte(`{}`))
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("role %s invoked worker claim: %v", role, err)
			}
		}
		if _, err := f.worker.Exec(f.ctx, `select * from public.record_portability_v1_targets`); err == nil {
			t.Fatal("worker read raw portability targets")
		}
		var publicGrant bool
		if err := f.db.db.QueryRow(f.ctx, `select exists(select 1 from pg_proc p cross join lateral aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a where p.oid='public.record_portability_gc_claim_v1(bytea)'::regprocedure and a.grantee=0 and a.privilege_type='EXECUTE')`).Scan(&publicGrant); err != nil || publicGrant {
			t.Fatalf("PUBLIC claim grant: %v %v", publicGrant, err)
		}
		request := struct {
			DeletionID     string `json:"deletion_id"`
			CleanupOwnerID string `json:"cleanup_owner_id"`
			CleanupLeaseMS int64  `json:"cleanup_lease_ms"`
		}{prep.S3Targets[0], f.target.OwnerClaim.Owner.OwnerID, 60000}
		claim, err := repo.portabilityV1S3Command(f.ctx, f.target, surface, `select public.record_portability_gc_claim_v1($1)`, request)
		if err != nil || claim == nil || claim.State != "claimed" {
			t.Fatalf("claim: %+v %v", claim, err)
		}
		readback := struct {
			Claim portabilityV1S3Claim `json:"claim"`
		}{claim.Claim}
		resolved, err := repo.portabilityV1S3Command(f.ctx, f.target, surface, `select public.record_portability_gc_claim_resolve_v1($1)`, readback)
		if err != nil || resolved == nil || resolved.Claim != claim.Claim {
			t.Fatalf("claim readback changed the fourteen-field tuple: %+v %v", resolved, err)
		}
		reused, err := repo.portabilityV1S3Command(f.ctx, f.target, surface, `select public.record_portability_gc_claim_v1($1)`, request)
		if err != nil || reused == nil || reused.Claim != claim.Claim {
			t.Fatalf("live owner refreshed claim: %+v %v", reused, err)
		}
		for _, mutation := range []func(*portabilityV1S3Claim){
			func(c *portabilityV1S3Claim) { c.SHA256 = hex.EncodeToString(make([]byte, 32)) },
			func(c *portabilityV1S3Claim) { c.VersionID = "other-version" },
			func(c *portabilityV1S3Claim) { c.StorageBinding = hex.EncodeToString(make([]byte, 32)) },
		} {
			bad := claim.Claim
			mutation(&bad)
			if _, err := repo.portabilityV1S3Command(f.ctx, f.target, surface, `select public.record_portability_gc_claim_resolve_v1($1)`, struct {
				Claim portabilityV1S3Claim `json:"claim"`
			}{bad}); err == nil {
				t.Fatal("tampered claim resolved")
			}
		}
		if _, err := f.db.db.Exec(f.ctx, `update public.record_portability_v1_obligations set snapshot=snapshot || '{"origin_id":"forged"}'::jsonb where operation_id=$1`, f.target.Operation.OperationID); err == nil {
			t.Fatal("immutable obligation snapshot accepted forgery")
		}
		f.checkInventory(t, prep, 1)
		receipt := portabilityV1S3Receipt{Key: claim.Claim.Key, VersionID: claim.Claim.VersionID, SHA256: claim.Claim.SHA256, SizeBytes: claim.Claim.SizeBytes, Deleted: true, StorageBinding: claim.Claim.StorageBinding}
		for _, mutation := range []func(*portabilityV1S3Receipt){
			func(r *portabilityV1S3Receipt) { r.SHA256 = hex.EncodeToString(make([]byte, 32)) },
			func(r *portabilityV1S3Receipt) { r.VersionID = "other-version" },
			func(r *portabilityV1S3Receipt) { r.StorageBinding = hex.EncodeToString(make([]byte, 32)) },
		} {
			bad := receipt
			mutation(&bad)
			if _, err := repo.portabilityV1S3Command(f.ctx, f.target, surface, `select public.record_portability_gc_complete_v1($1)`, struct {
				Claim   portabilityV1S3Claim   `json:"claim"`
				Receipt portabilityV1S3Receipt `json:"receipt"`
			}{claim.Claim, bad}); err == nil {
				t.Fatal("tampered receipt accepted")
			}
		}
		newTarget := f.target
		if err := f.db.db.QueryRow(f.ctx, `update public.record_purge_operations set owner_generation=owner_generation+1 where operation_id=$1 returning owner_generation,owner_expires_at`, f.target.Operation.OperationID).Scan(&newTarget.OwnerClaim.Owner.Generation, &newTarget.OwnerClaim.Owner.ExpiresAt); err != nil {
			t.Fatal(err)
		}
		reused, err = repo.portabilityV1S3Command(f.ctx, newTarget, surface, `select public.record_portability_gc_claim_v1($1)`, request)
		if err != nil || reused == nil || reused.Claim != claim.Claim {
			t.Fatalf("operation takeover changed cleanup tuple: %+v %v", reused, err)
		}
		if _, err := repo.portabilityV1S3Command(f.ctx, f.target, surface, `select public.record_portability_gc_complete_v1($1)`, struct {
			Claim   portabilityV1S3Claim   `json:"claim"`
			Receipt portabilityV1S3Receipt `json:"receipt"`
		}{claim.Claim, receipt}); err == nil {
			t.Fatal("stale operation owner completed claim")
		}
		if _, err := repo.portabilityV1S3Command(f.ctx, f.target, surface, `select public.record_portability_gc_retry_v1($1)`, struct {
			Claim   portabilityV1S3Claim `json:"claim"`
			RetryAt string               `json:"retry_at"`
		}{claim.Claim, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)}); err == nil {
			t.Fatal("stale operation owner retried claim")
		}
		f.survivors(t, false)
		f.assertNoAttachmentGC(t)
		if err := repo.executePortabilityV1S3(f.ctx, newTarget, surface, prep.S3Targets[0]); err != nil {
			t.Fatal(err)
		}
		f.survivors(t, true)
	})
	t.Run("delete_ack_loss_process_restart", func(t *testing.T) { portabilityV1TestProcessRecovery(t) })
	t.Run("route_binding_mismatch", func(t *testing.T) {
		f := newPortabilityV1TestFixture(t, true)
		original := f.repository(t, f.blob)
		prep, err := original.preparePortabilityV1(f.ctx, f.target, surface)
		if err != nil {
			t.Fatal(err)
		}
		f.checkInventory(t, prep, 1)
		if _, err := f.repository(t, nil).PurgeRecordPortability(f.ctx, f.target, surface); err == nil {
			t.Fatal("committed target accepted missing configuration")
		}
		_, alternateBucket := newAttachmentUploadWorkflowMinIO(t)
		other, err := attachments.NewS3BlobStore(f.client, alternateBucket)
		if err != nil {
			t.Fatal(err)
		}
		observer := &portabilityV1TestObserver{store: other}
		restarted := f.repository(t, observer)
		if _, err := restarted.PurgeRecordPortability(f.ctx, f.target, surface); err == nil {
			t.Fatal("different bucket authorized portability cleanup")
		}
		if observer.calls != 0 {
			t.Fatalf("different route dispatched %d deletes", observer.calls)
		}
		var state string
		if err := f.db.db.QueryRow(f.ctx, `select deletion_state from public.record_portability_v1_targets where operation_id=$1 and target_id=$2`, f.target.Operation.OperationID, prep.S3Targets[0]).Scan(&state); err != nil || state != "pending" {
			t.Fatalf("mismatched route changed target: %q %v", state, err)
		}
		f.checkInventory(t, prep, 1)
		f.survivors(t, false)
	})
	t.Run("route_post_io_identity_mismatch", func(t *testing.T) {
		f := newPortabilityV1TestFixture(t, true)
		_, alternateBucket := newAttachmentUploadWorkflowMinIO(t)
		other, err := attachments.NewS3BlobStore(f.client, alternateBucket)
		if err != nil {
			t.Fatal(err)
		}
		drift := &portabilityV1TestRouteDrift{original: f.blob, alternate: other}
		repo := f.repository(t, drift)
		if _, err := repo.PurgeRecordPortability(f.ctx, f.target, surface); err == nil || drift.calls != 1 {
			t.Fatalf("post-I/O route change: calls=%d err=%v", drift.calls, err)
		}
		var state string
		var result *string
		if err := f.db.db.QueryRow(f.ctx, `select deletion_state,physical_delete_result from public.record_portability_v1_targets where operation_id=$1 and disposition='s3'`, f.target.Operation.OperationID).Scan(&state, &result); err != nil || state != "claimed" || result != nil {
			t.Fatalf("route drift published receipt: %q %v %v", state, result, err)
		}
		f.survivors(t, true)
		f.assertNoAttachmentGC(t)
	})
}

func portabilityV1TestChild(t *testing.T) {
	t.Helper()
	input, event, control := os.NewFile(3, "private-config"), os.NewFile(4, "event"), os.NewFile(5, "control")
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
	config.ConnConfig.User, config.ConnConfig.Password, config.ConnConfig.Database = c.Worker, c.Password, c.Database
	config.AfterConnect = nil
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal("child worker connection failed")
	}
	defer pool.Close()
	var sessionUser, currentUser string
	if err := pool.QueryRow(context.Background(), `select session_user,current_user`).Scan(&sessionUser, &currentUser); err != nil || sessionUser != c.Worker || currentUser != c.Worker {
		t.Fatal("child is not direct deletion worker")
	}
	client, err := minio.New(c.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""), Secure: c.Secure})
	if err != nil {
		t.Fatal("child MinIO client failed")
	}
	blob, err := attachments.NewS3BlobStore(client, c.Bucket)
	if err != nil {
		t.Fatal("child blob configuration failed")
	}
	repo := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	barrier := &portabilityV1TestBarrier{v1AfterDeleteBarrier: &v1AfterDeleteBarrier{BlobGCDeleter: blob, event: event, control: control}, store: blob}
	if err := repo.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, barrier); err != nil {
		t.Fatal("child deleter configuration failed")
	}
	if _, err := repo.PurgeRecordPortability(context.Background(), c.Target, recorddeletion.RecordPortabilitySurfaceDigest()); err != nil {
		t.Fatal("child purge stopped before barrier")
	}
	t.Fatal("child continued past process-death barrier")
}

func portabilityV1TestProcessRecovery(t *testing.T) {
	t.Helper()
	f := newPortabilityV1TestFixture(t, true)
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
	cmd := exec.Command(binary, "-test.run=^TestPostgresMinIOIntegrationRecordDeletionV1Portability$", "-test.count=1")
	cmd.Env = []string{"HOUFENG_PORTABILITY_V1_CHILD=1"}
	cmd.ExtraFiles = []*os.File{configRead, eventWrite, controlRead}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
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
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	secure := false
	if value := os.Getenv("HOUFENG_MINIO_SECURE"); value != "" {
		secure, err = strconv.ParseBool(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	workerURL, err := url.Parse(f.db.db.Config().ConnString())
	if err != nil || (workerURL.Scheme != "postgres" && workerURL.Scheme != "postgresql") {
		t.Fatal("child requires PostgreSQL URL")
	}
	workerURL.User = url.UserPassword(f.db.deletionWorker, f.db.passwords[f.db.deletionWorker])
	workerURL.Path = "/" + f.db.databaseName
	child := v1ChildConfig{DatabaseConfig: workerURL.String(), Database: f.db.databaseName, Worker: f.db.deletionWorker, Password: f.db.passwords[f.db.deletionWorker], Endpoint: os.Getenv("HOUFENG_MINIO_ENDPOINT"), AccessKey: os.Getenv("HOUFENG_MINIO_ACCESS_KEY"), SecretKey: os.Getenv("HOUFENG_MINIO_SECRET_KEY"), Secure: secure, Bucket: f.bucket, Target: f.target}
	if err := json.NewEncoder(configWrite).Encode(child); err != nil {
		t.Fatal("sending private child config failed")
	}
	_ = configWrite.Close()
	barrier := make(chan error, 1)
	go func() {
		var b [1]byte
		_, e := io.ReadFull(eventRead, b[:])
		if e == nil && b[0] != 1 {
			e = errors.New("invalid barrier")
		}
		barrier <- e
	}()
	select {
	case err := <-barrier:
		if err != nil {
			t.Fatalf("child failed before delete barrier: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("child did not reach post-delete barrier")
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	waited = true
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("child exit was not SIGKILL: %v", err)
	}
	terminal := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	if n, err := unix.Poll(terminal, 1000); err != nil || n != 1 || terminal[0].Revents&unix.POLLIN == 0 {
		t.Fatalf("child pidfd not terminal: %d %v", n, err)
	}
	var extra [1]byte
	if n, err := eventRead.Read(extra[:]); n != 0 || err != io.EOF {
		t.Fatalf("child event pipe remained open: %d %v", n, err)
	}
	var id, state string
	var generation, attempt int64
	var receipt *string
	var proof bool
	if err := f.db.db.QueryRow(f.ctx, `select target_id,deletion_state,cleanup_owner_generation,attempt,physical_delete_result,(receipt_digest is not null or completed_at is not null) from public.record_portability_v1_targets where operation_id=$1 and disposition='s3'`, f.target.Operation.OperationID).Scan(&id, &state, &generation, &attempt, &receipt, &proof); err != nil || state != "claimed" || receipt != nil || proof {
		t.Fatalf("killed child state=%q result=%v proof=%v: %v", state, receipt, proof, err)
	}
	f.survivors(t, true)
	f.assertNoAttachmentGC(t)
	repo := f.repository(t, f.blob)
	prep, err := repo.preparePortabilityV1(f.ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil {
		t.Fatalf("crash inventory readback: %v", err)
	}
	f.checkInventory(t, prep, 1)
	if _, err := f.db.db.Exec(f.ctx, `update public.record_portability_v1_targets set cleanup_expires_at=clock_timestamp()-interval '1 second' where operation_id=$1 and target_id=$2`, f.target.Operation.OperationID, id); err != nil {
		t.Fatal(err)
	}
	result, err := repo.PurgeRecordPortability(f.ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := repo.PurgeRecordPortability(f.ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest()); err != nil || replay != result {
		t.Fatalf("restart replay: %v", err)
	}
	var nextGeneration, nextAttempt int64
	if err := f.db.db.QueryRow(f.ctx, `select deletion_state,cleanup_owner_generation,attempt,physical_delete_result from public.record_portability_v1_targets where operation_id=$1 and target_id=$2`, f.target.Operation.OperationID, id).Scan(&state, &nextGeneration, &nextAttempt, &receipt); err != nil || state != "completed" || receipt == nil || *receipt != "already_absent" || nextGeneration != generation+1 || nextAttempt != attempt+1 {
		t.Fatalf("restart state=%q generation=%d attempt=%d result=%v: %v", state, nextGeneration, nextAttempt, receipt, err)
	}
	f.assertNoAttachmentGC(t)
	f.survivors(t, true)
}

func portabilityV1TestSharedArchive(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	db := newAdmittedCoreRecordFixture(t, ctx)
	first := seedRecordWithoutSearchProjection(t, ctx, db, "shared S3 first owner")
	last := seedRecordWithoutSearchProjection(t, ctx, db, "shared S3 last owner")
	runtime := db.openDirectRuntimePool(t, ctx, "shared-s3-source", 2)
	client, bucket := newAttachmentUploadWorkflowMinIO(t)
	blob, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := attachments.NewManagedBlobWriter(blob, attachments.BackendKindS3, NewPostgresLocalBlobRepository(runtime, localBlobFixtureWitness(t, runtime), nil))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("shared S3 import archive original bytes")
	hash := sha256.Sum256(body)
	hashText := hex.EncodeToString(hash[:])
	if _, err := db.db.Exec(ctx, `insert into public.record_import_jobs(import_job_id,actor_id,idempotency_key,job_state,identity_classification,archive_digest,destination_subject_kind,destination_subject_source_id,expires_at) values('rij_s3shared','usr_aaaaaaaaaaaaaaaaaaaaaaaa','s3shared','uploaded','unknown',$1,'vps',$2,clock_timestamp()+interval '1 hour')`, hash[:], testStoreRecordVPSID); err != nil {
		t.Fatal(err)
	}
	temporary, err := attachments.NewBlobTemporaryKey()
	if err != nil {
		t.Fatal(err)
	}
	written, err := writer.Put(ctx, attachments.WriteRequest{ProjectID: "default", Content: attachments.PutRequest{ExpectedSHA256: hash, ExpectedSizeBytes: int64(len(body)), TemporaryKey: temporary}, Origin: attachments.WriteOrigin{Kind: "import_archive", ID: "rij_s3shared", Generation: 1, ArchiveDigest: &hashText}}, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var expiry time.Time
	if err := db.db.QueryRow(ctx, `select expires_at from public.record_import_jobs where import_job_id='rij_s3shared'`).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	publication, err := json.Marshal(map[string]any{"artifact_id": "ria_s3shared", "job_id": "rij_s3shared", "kind": "archive", "backend_kind": "s3", "namespace_id": nil, "key": written.Version.Key, "version": written.Version.VersionID, "sha256": hashText, "size": len(body), "expires_at": expiry, "obligation_id": written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Exec(ctx, `select public.record_import_artifact_publish($1)`, publication); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(ctx, `insert into public.record_import_plans(import_plan_id,import_job_id,plan_digest,object_count,remap_count,expires_at) values('rip_s3shared','rij_s3shared',$1,2,0,clock_timestamp()+interval '1 hour')`, hash[:]); err != nil {
		t.Fatal(err)
	}
	for _, recordID := range []string{first.RecordID, last.RecordID} {
		identity := sha256.Sum256([]byte(recordID))
		if _, err := db.db.Exec(ctx, `insert into public.record_import_entity_mappings(import_plan_id,entity_kind,source_id,source_identity_digest,target_id) values('rip_s3shared','record',$1,$2,$1)`, recordID, identity[:]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id='rij_s3shared'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(ctx, `insert into public.attachment_quota_accounts(project_id,physical_bytes,quota_version) values('default',$1,9) on conflict(project_id) do update set physical_bytes=excluded.physical_bytes,quota_version=excluded.quota_version`, len(body)); err != nil {
		t.Fatal(err)
	}
	opFirst := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_shareds3first", ReservationID: "drs_shareds3first", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	opLast := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_shareds3last", ReservationID: "drs_shareds3last", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: last.RecordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	firstTarget := seedVerifiedS3AttachmentDeletionOperation(t, ctx, db, first, opFirst)
	lastTarget := seedVerifiedS3AttachmentDeletionOperation(t, ctx, db, last, opLast)
	worker := db.openDirectRolePool(t, ctx, db.deletionWorker, "shared-s3-worker", 2)
	observer := &portabilityV1TestObserver{store: blob}
	repo := NewPostgresRecordDeletionRepository(worker, allowRecordPlatformAdmissionGate)
	if err := repo.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, observer); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	// Capture actual source rows before each independently authenticated prepare.
	capture := func(target recorddeletion.PurgeTarget) (*portabilityV1TestFixture, string) {
		f := &portabilityV1TestFixture{ctx: ctx, db: db, target: target, expectedArtifacts: "[]"}
		var artifact string
		f.captureOwners(t)
		if err := db.db.QueryRow(ctx, `select coalesce((select jsonb_agg(to_jsonb(o) order by obligation_id collate "C") from public.record_write_obligations o where origin_kind='import_archive' and origin_id='rij_s3shared'),'[]'::jsonb)::text,coalesce((select jsonb_agg(jsonb_build_array(import_artifact_id,import_job_id,artifact_role,backend_kind,blob_key,object_version_id,encode(sha256,'hex'),byte_size,to_jsonb(created_at at time zone 'UTC'),namespace_id,obligation_id) order by import_artifact_id collate "C") from public.record_import_artifacts where import_job_id='rij_s3shared'),'[]'::jsonb)::text`).Scan(&f.expectedObligations, &artifact); err != nil {
			t.Fatal(err)
		}
		return f, artifact
	}
	check := func(f *portabilityV1TestFixture, prep portabilityV1Preparation, expectedArtifact string) {
		f.checkDigest(t, prep)
		var actual string
		var links int
		route, err := blob.GCStorageIdentity()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.db.QueryRow(ctx, `select coalesce((select jsonb_agg(jsonb_build_array(a.artifact_id,a.job_id,a.artifact_role,a.backend_kind,a.blob_key,a.object_version_id,encode(a.sha256,'hex'),a.byte_size,to_jsonb(a.created_at at time zone 'UTC'),a.namespace_id,a.obligation_id) order by a.artifact_id collate "C") from public.record_portability_v1_artifacts a where a.operation_id=$1 and a.source_kind='import'),'[]'::jsonb)::text,(select count(*) from public.record_portability_v1_artifacts a join public.record_portability_v1_obligations o on o.operation_id=a.operation_id and o.obligation_id=a.obligation_id and o.target_id=a.target_id join public.record_portability_v1_targets x on x.operation_id=a.operation_id and x.target_id=a.target_id where a.operation_id=$1 and a.artifact_id='ria_s3shared' and a.blob_key=$2 and a.object_version_id=$3 and a.sha256=$4 and a.byte_size=$5 and x.blob_key=a.blob_key and x.object_version_id=a.object_version_id and x.storage_binding=$6 and o.snapshot->>'origin_id'='rij_s3shared')`, f.target.Operation.OperationID, written.Version.Key, written.Version.VersionID, hash[:], len(body), route[:]).Scan(&actual, &links); err != nil || actual != expectedArtifact || links != 1 {
			t.Fatalf("shared source set/membership: match=%t links=%d error=%v", actual == expectedArtifact, links, err)
		}
	}
	firstSnapshot, firstArtifact := capture(firstTarget)
	firstPrep, err := repo.preparePortabilityV1(ctx, firstTarget, surface)
	if err != nil || firstPrep.UnresolvedCount != 0 || len(firstPrep.S3Targets) != 0 {
		t.Fatalf("shared first prepare: %+v %v", firstPrep, err)
	}
	check(firstSnapshot, firstPrep, firstArtifact)
	firstReceipt, err := repo.PurgeRecordPortability(ctx, firstTarget, surface)
	if err != nil {
		t.Fatal(err)
	}
	var retained bool
	if err := db.db.QueryRow(ctx, `select exists(select 1 from public.record_portability_v1_targets where operation_id=$1 and disposition='retained_shared' and jsonb_array_length(retained_evidence)>0) and exists(select 1 from public.record_import_entity_mappings where target_id=$2) and exists(select 1 from public.record_import_artifacts where import_artifact_id='ria_s3shared')`, firstTarget.Operation.OperationID, last.RecordID).Scan(&retained); err != nil || !retained || observer.calls != 0 {
		t.Fatalf("shared owner retained=%v deletes=%d error=%v", retained, observer.calls, err)
	}
	object, err := client.GetObject(ctx, bucket, written.Version.Key, minio.GetObjectOptions{VersionID: written.Version.VersionID})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(object)
	_ = object.Close()
	if err != nil || !bytes.Equal(contents, body) {
		t.Fatalf("shared archive changed: %v", err)
	}
	lastSnapshot, lastArtifact := capture(lastTarget)
	lastPrep, err := repo.preparePortabilityV1(ctx, lastTarget, surface)
	if err != nil || lastPrep.UnresolvedCount != 0 || len(lastPrep.S3Targets) != 1 {
		t.Fatalf("last owner prepare: %+v %v", lastPrep, err)
	}
	check(lastSnapshot, lastPrep, lastArtifact)
	if replay, err := repo.PurgeRecordPortability(ctx, firstTarget, surface); err != nil || replay != firstReceipt {
		t.Fatalf("shared first receipt lost successor: %v", err)
	}
	lastFixture := &portabilityV1TestFixture{ctx: ctx, db: db, target: lastTarget, written: written, blob: blob}
	beforeCleanup := lastFixture.obligationCleanupFields(t)
	lastReceipt, err := repo.PurgeRecordPortability(ctx, lastTarget, surface)
	if err != nil {
		t.Fatal(err)
	}
	if observer.calls != 1 {
		t.Fatalf("shared final delete count=%d", observer.calls)
	}
	_, err = client.StatObject(ctx, bucket, written.Version.Key, minio.StatObjectOptions{VersionID: written.Version.VersionID})
	if minio.ToErrorResponse(err).StatusCode != http.StatusNotFound {
		t.Fatalf("shared exact version survived: %v", err)
	}
	var completed, quota int
	if err := db.db.QueryRow(ctx, `select (select count(*) from public.record_portability_v1_targets where operation_id=$1 and target_id=$2 and deletion_state='completed' and physical_delete_result in ('deleted','already_absent') and receipt_digest is not null),(select count(*) from public.attachment_quota_accounts where project_id='default' and physical_bytes=$3 and quota_version=9)`, lastTarget.Operation.OperationID, lastPrep.S3Targets[0], len(body)).Scan(&completed, &quota); err != nil || completed != 1 || quota != 1 {
		t.Fatalf("shared final receipt=%d quota=%d error=%v", completed, quota, err)
	}
	lastFixture.assertPrivateCompletion(t, lastPrep.S3Targets[0], beforeCleanup)
	for _, replay := range []struct {
		target  recorddeletion.PurgeTarget
		receipt recorddeletion.AdapterPurgeReceipt
	}{{firstTarget, firstReceipt}, {lastTarget, lastReceipt}} {
		if got, err := repo.PurgeRecordPortability(ctx, replay.target, surface); err != nil || got != replay.receipt || observer.calls != 1 {
			t.Fatalf("shared replay: %v calls=%d", err, observer.calls)
		}
	}
	lastFixture.assertPrivateCompletion(t, lastPrep.S3Targets[0], beforeCleanup)
}

type portabilityV1IndependentFixture struct {
	ctx        context.Context
	db         recordPlatformPostgresFixture
	worker     *pgxpool.Pool
	client     *minio.Client
	bucket     string
	blob       *attachments.S3BlobStore
	observer   *portabilityV1TestObserver
	repository *PostgresRecordDeletionRepository
	records    [2]records.RevisionCommitResult
	targets    [2]recorddeletion.PurgeTarget
	writes     [2]attachments.WrittenBlob
	body       []byte
}

func newPortabilityV1IndependentFixture(t *testing.T) *portabilityV1IndependentFixture {
	t.Helper()
	f := &portabilityV1IndependentFixture{ctx: context.Background(), body: []byte("independent owners share the exact same S3 version\n")}
	f.db = newAdmittedCoreRecordFixture(t, f.ctx)
	f.records[0] = seedRecordWithoutSearchProjection(t, f.ctx, f.db, "independent S3 owner A")
	f.records[1] = seedRecordWithoutSearchProjection(t, f.ctx, f.db, "independent S3 owner B")
	runtime := f.db.openDirectRuntimePool(t, f.ctx, "independent-s3-writers", 2)
	f.client, f.bucket = newAttachmentUploadWorkflowMinIO(t)
	var err error
	f.blob, err = attachments.NewS3BlobStore(f.client, f.bucket)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(f.body)
	for index, suffix := range []string{"a", "b"} {
		jobID, artifactID := "rej_independents3"+suffix, "rxa_independents3"+suffix
		var expiry time.Time
		if err := f.db.db.QueryRow(f.ctx, `insert into public.record_export_jobs
			(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,inventory_digest,
			 authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
			values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'markdown','safe','staging',$2,$2,1,$3,$4,'source_worker',1,
			 clock_timestamp()+interval '30 minutes',clock_timestamp()+interval '1 hour') returning expires_at`,
			jobID, digest[:], f.records[index].RecordID, f.records[index].RevisionID).Scan(&expiry); err != nil {
			t.Fatal(err)
		}
		writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindS3, NewPostgresLocalBlobRepository(runtime, localBlobFixtureWitness(t, runtime), nil))
		if err != nil {
			t.Fatal(err)
		}
		temporary, err := attachments.NewBlobTemporaryKey()
		if err != nil {
			t.Fatal(err)
		}
		f.writes[index], err = writer.Put(f.ctx, attachments.WriteRequest{ProjectID: "default", Origin: attachments.WriteOrigin{Kind: "export_artifact", ID: jobID, Generation: 1}, Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(f.body)), TemporaryKey: temporary}}, bytes.NewReader(f.body))
		if err != nil {
			t.Fatal(err)
		}
		publication, err := json.Marshal(map[string]any{"artifact_id": artifactID, "job_id": jobID, "kind": "markdown", "content_type": "text/markdown", "backend_kind": "s3", "namespace_id": nil, "key": f.writes[index].Version.Key, "version": f.writes[index].Version.VersionID, "sha256": hex.EncodeToString(digest[:]), "size": len(f.body), "expires_at": expiry, "obligation_id": f.writes[index].ObligationID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Exec(f.ctx, `select public.record_export_artifact_publish($1)`, publication); err != nil {
			t.Fatal(err)
		}
	}
	if f.writes[0].ObligationID == f.writes[1].ObligationID || f.writes[0].Version != f.writes[1].Version || f.writes[0].Version.Validate() != nil {
		t.Fatal("independent managed exports did not return distinct obligations and the same exact S3 version")
	}
	route, err := f.blob.GCStorageIdentity()
	if err != nil {
		t.Fatal(err)
	}
	var writers, sources int
	if err := f.db.db.QueryRow(f.ctx, `select count(distinct writer_instance),count(*) from public.record_write_obligations
		where obligation_id=any($1::text[]) and backend_kind='s3' and storage_binding=$2 and canonical_key=$3
		and object_version=$4 and sha256_digest=$5 and size_bytes=$6 and outcome='materialized'
		and dispatch_id is not null and dispatched_at is not null and returned_at is not null and return_command is not null`,
		[]string{f.writes[0].ObligationID, f.writes[1].ObligationID}, route[:], f.writes[0].Version.Key,
		f.writes[0].Version.VersionID, digest[:], len(f.body)).Scan(&writers, &sources); err != nil || writers != 2 || sources != 2 {
		t.Fatalf("independent source provenance: writers=%d sources=%d error=%v", writers, sources, err)
	}
	if _, err := f.db.db.Exec(f.ctx, `insert into public.attachment_quota_accounts(project_id,physical_bytes,quota_version) values('default',$1,9) on conflict(project_id) do update set physical_bytes=excluded.physical_bytes,quota_version=excluded.quota_version`, len(f.body)); err != nil {
		t.Fatal(err)
	}
	for index, suffix := range []string{"a", "b"} {
		op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_independents3" + suffix, ReservationID: "drs_independents3" + suffix, Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: f.records[index].RecordID}, ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
		f.targets[index] = seedVerifiedS3AttachmentDeletionOperation(t, f.ctx, f.db, f.records[index], op)
	}
	f.worker = f.db.openDirectRolePool(t, f.ctx, f.db.deletionWorker, "independent-s3-worker", 4)
	f.observer = &portabilityV1TestObserver{store: f.blob}
	f.repository = NewPostgresRecordDeletionRepository(f.worker, allowRecordPlatformAdmissionGate)
	if err := f.repository.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, f.observer); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *portabilityV1IndependentFixture) sourceFixture(index int) *portabilityV1TestFixture {
	return &portabilityV1TestFixture{ctx: f.ctx, db: f.db, worker: f.worker, client: f.client, bucket: f.bucket, blob: f.blob, target: f.targets[index], written: f.writes[index]}
}

func (f *portabilityV1IndependentFixture) snapshot(t *testing.T, index int) *portabilityV1TestFixture {
	t.Helper()
	x := f.sourceFixture(index)
	x.captureOwners(t)
	ids := []string{f.writes[index].ObligationID}
	if index == 1 {
		ids = []string{f.writes[0].ObligationID, f.writes[1].ObligationID}
	}
	if err := f.db.db.QueryRow(f.ctx, `select
		coalesce((select jsonb_agg(jsonb_build_array(export_artifact_id,export_job_id,artifact_kind,backend_kind,blob_key,object_version_id,encode(sha256,'hex'),byte_size,to_jsonb(created_at at time zone 'UTC'),namespace_id,obligation_id) order by export_artifact_id collate "C") from public.record_export_artifacts where export_job_id=$1),'[]'::jsonb)::text,
		coalesce((select jsonb_agg(to_jsonb(o) order by obligation_id collate "C") from public.record_write_obligations o where obligation_id=any($2::text[])),'[]'::jsonb)::text`,
		[]string{"rej_independents3a", "rej_independents3b"}[index], ids).Scan(&x.expectedArtifacts, &x.expectedObligations); err != nil {
		t.Fatal(err)
	}
	return x
}

func (f *portabilityV1IndependentFixture) assertObject(t *testing.T, present bool) {
	t.Helper()
	info, err := f.client.StatObject(f.ctx, f.bucket, f.writes[0].Version.Key, minio.StatObjectOptions{VersionID: f.writes[0].Version.VersionID})
	if !present {
		if minio.ToErrorResponse(err).StatusCode != http.StatusNotFound {
			t.Fatalf("exact version remains: %v", err)
		}
		return
	}
	if err != nil || info.VersionID != f.writes[0].Version.VersionID {
		t.Fatalf("exact retained version: %v", err)
	}
	object, err := f.client.GetObject(f.ctx, f.bucket, f.writes[0].Version.Key, minio.GetObjectOptions{VersionID: f.writes[0].Version.VersionID})
	if err != nil {
		t.Fatal(err)
	}
	actual, readErr := io.ReadAll(object)
	closeErr := object.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, f.body) {
		t.Fatalf("retained physical bytes: %v %v", readErr, closeErr)
	}
}

func (f *portabilityV1IndependentFixture) retainFirst(t *testing.T) recorddeletion.AdapterPurgeReceipt {
	t.Helper()
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	first, second := f.sourceFixture(0), f.sourceFixture(1)
	firstCleanup := first.obligationCleanupFields(t)
	var survivorBefore, survivorAfter string
	if err := f.db.db.QueryRow(f.ctx, `select to_jsonb(o)::text from public.record_write_obligations o where obligation_id=$1`, f.writes[1].ObligationID).Scan(&survivorBefore); err != nil {
		t.Fatal(err)
	}
	snapshot := f.snapshot(t, 0)
	prep, err := f.repository.preparePortabilityV1(f.ctx, f.targets[0], surface)
	if err != nil || prep.UnresolvedCount != 0 || len(prep.S3Targets) != 0 {
		t.Fatalf("first owner prepare: %+v %v", prep, err)
	}
	snapshot.checkDigest(t, prep)
	receipt, err := f.repository.PurgeRecordPortability(f.ctx, f.targets[0], surface)
	if err != nil {
		t.Fatal(err)
	}
	var retained bool
	err = f.db.db.QueryRow(f.ctx, `select exists(select 1 from public.record_portability_v1_targets t
		join public.record_portability_v1_obligations o using(operation_id,target_id)
		where t.operation_id=$1 and o.obligation_id=$2 and t.disposition='retained_shared'
		and exists(select 1 from jsonb_array_elements(t.retained_evidence) e(value) where e.value->>0='export_artifact' and e.value->>1='rxa_independents3b'))
		and exists(select 1 from public.record_write_obligations where obligation_id=$2 and operation_id is null and handed_off_at is not null and cleanup_completed_at is null)
		and not exists(select 1 from public.record_export_jobs where export_job_id='rej_independents3a')`, f.targets[0].Operation.OperationID, f.writes[0].ObligationID).Scan(&retained)
	if err != nil || !retained || f.observer.calls != 0 {
		t.Fatalf("first owner did not retain second: retained=%v deletes=%d error=%v", retained, f.observer.calls, err)
	}
	if err := f.db.db.QueryRow(f.ctx, `select to_jsonb(o)::text from public.record_write_obligations o where obligation_id=$1`, f.writes[1].ObligationID).Scan(&survivorAfter); err != nil {
		t.Fatal(err)
	}
	if survivorBefore != survivorAfter || first.obligationCleanupFields(t) != firstCleanup {
		t.Fatal("first owner changed survivor or settled original cleanup")
	}
	first.assertNoAttachmentGC(t)
	second.assertNoAttachmentGC(t)
	f.assertObject(t, true)
	return receipt
}

func (f *portabilityV1IndependentFixture) checkSuccessor(t *testing.T, prep portabilityV1Preparation, snapshot *portabilityV1TestFixture) {
	t.Helper()
	if prep.UnresolvedCount != 0 || len(prep.S3Targets) != 1 {
		t.Fatalf("last owner preparation: %+v", prep)
	}
	snapshot.checkDigest(t, prep)
	var linked, assigned int
	err := f.db.db.QueryRow(f.ctx, `select
		(select count(*) from public.record_portability_v1_obligations o join public.record_portability_v1_targets t using(operation_id,target_id)
		 where o.operation_id=$1 and o.obligation_id=any($2::text[]) and o.target_id=$3 and t.disposition='s3'
		 and t.blob_key=$4 and t.object_version_id=$5 and t.storage_binding=$6),
		(select count(*) from public.record_write_obligations where obligation_id=any($2::text[]) and operation_id=$1)`,
		f.targets[1].Operation.OperationID, []string{f.writes[0].ObligationID, f.writes[1].ObligationID}, prep.S3Targets[0],
		f.writes[0].Version.Key, f.writes[0].Version.VersionID, func() []byte {
			b, err := f.blob.GCStorageIdentity()
			if err != nil {
				t.Fatal(err)
			}
			return b[:]
		}()).Scan(&linked, &assigned)
	if err != nil || linked != 2 || assigned != 2 {
		t.Fatalf("carried sources not linked and assigned: linked=%d assigned=%d error=%v", linked, assigned, err)
	}
}

func (f *portabilityV1IndependentFixture) finishLast(t *testing.T, firstReceipt recorddeletion.AdapterPurgeReceipt, prep portabilityV1Preparation) {
	t.Helper()
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	before := [2]string{f.sourceFixture(0).obligationCleanupFields(t), f.sourceFixture(1).obligationCleanupFields(t)}
	lastReceipt, err := f.repository.PurgeRecordPortability(f.ctx, f.targets[1], surface)
	if err != nil || f.observer.calls != 1 {
		t.Fatalf("last owner completion: deletes=%d error=%v", f.observer.calls, err)
	}
	for i := range f.writes {
		x := f.sourceFixture(i)
		x.target = f.targets[1]
		x.assertPrivateCompletion(t, prep.S3Targets[0], before[i])
	}
	f.assertObject(t, false)
	for _, pair := range []struct {
		target  recorddeletion.PurgeTarget
		receipt recorddeletion.AdapterPurgeReceipt
	}{{f.targets[0], firstReceipt}, {f.targets[1], lastReceipt}} {
		got, err := f.repository.PurgeRecordPortability(f.ctx, pair.target, surface)
		if err != nil || got != pair.receipt || f.observer.calls != 1 {
			t.Fatalf("independent owner replay: deletes=%d error=%v", f.observer.calls, err)
		}
	}
	for i := range f.writes {
		x := f.sourceFixture(i)
		x.target = f.targets[1]
		x.assertPrivateCompletion(t, prep.S3Targets[0], before[i])
	}
}

func runPortabilityV1IndependentSameVersionOwners(t *testing.T) {
	t.Run("carry_and_delete", func(t *testing.T) {
		f := newPortabilityV1IndependentFixture(t)
		firstReceipt := f.retainFirst(t)
		snapshot := f.snapshot(t, 1)
		prep, err := f.repository.preparePortabilityV1(f.ctx, f.targets[1], recorddeletion.RecordPortabilitySurfaceDigest())
		if err != nil {
			t.Fatal(err)
		}
		f.checkSuccessor(t, prep, snapshot)
		f.finishLast(t, firstReceipt, prep)
	})
	t.Run("prepare_commit_unknown", func(t *testing.T) {
		f := newPortabilityV1IndependentFixture(t)
		firstReceipt := f.retainFirst(t)
		snapshot := f.snapshot(t, 1)
		lost, resolved := false, 0
		f.repository.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			tx, err := f.worker.BeginTx(ctx, options)
			if err != nil {
				return nil, err
			}
			return &portabilityV1TestLostCommit{Tx: tx, query: `select public.record_portability_purge_prepare_v1($1)`, lost: &lost, resolved: &resolved}, nil
		}
		prep, err := f.repository.preparePortabilityV1(f.ctx, f.targets[1], recorddeletion.RecordPortabilitySurfaceDigest())
		if err != nil || !lost || resolved != 1 || f.observer.calls != 0 {
			t.Fatalf("successor commit readback lost=%v resolved=%d deletes=%d error=%v", lost, resolved, f.observer.calls, err)
		}
		f.checkSuccessor(t, prep, snapshot)
		f.finishLast(t, firstReceipt, prep)
	})
	t.Run("guarded_negative_inputs", runPortabilityV1IndependentNegativeInputs)
	t.Run("concurrent_successor_prepares", runPortabilityV1IndependentConcurrentPrepares)
}

func (f *portabilityV1IndependentFixture) assertNoLastEffects(t *testing.T) {
	t.Helper()
	var clean bool
	err := f.db.db.QueryRow(f.ctx, `select
		exists(select 1 from public.record_export_jobs where export_job_id='rej_independents3b')
		and exists(select 1 from public.record_export_artifacts where export_artifact_id='rxa_independents3b')
		and not exists(select 1 from public.record_portability_v1_preparations where operation_id=$1 and removed_rows<>0)
		and not exists(select 1 from public.record_portability_v1_targets where operation_id=$1 and (deletion_state in ('claimed','retry_wait','completed') or receipt_digest is not null))
		and not exists(select 1 from public.record_portability_purge_receipts where operation_id=$1)`, f.targets[1].Operation.OperationID).Scan(&clean)
	if err != nil || !clean || f.observer.calls != 0 {
		t.Fatalf("rejected successor changed effects: clean=%v deletes=%d error=%v", clean, f.observer.calls, err)
	}
	f.sourceFixture(1).assertNoAttachmentGC(t)
	f.assertObject(t, true)
}

func runPortabilityV1IndependentNegativeInputs(t *testing.T) {
	t.Run("forged_predecessor_evidence", func(t *testing.T) {
		f := newPortabilityV1IndependentFixture(t)
		f.retainFirst(t)
		_, err := f.db.db.Exec(f.ctx, `update public.record_portability_v1_targets set retained_evidence='[["export_artifact","rxa_independents3b","ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"]]'::jsonb where operation_id=$1`, f.targets[0].Operation.OperationID)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "55000" {
			t.Fatalf("forged retained evidence was not rejected: %v", err)
		}
		f.assertNoLastEffects(t)
	})
	t.Run("altered_binding_or_version", func(t *testing.T) {
		f := newPortabilityV1IndependentFixture(t)
		f.retainFirst(t)
		for _, statement := range []string{
			`update public.record_write_obligations set storage_binding=decode(repeat('ff',32),'hex') where obligation_id=$1`,
			`update public.record_write_obligations set object_version='forged-version' where obligation_id=$1`,
		} {
			_, err := f.db.db.Exec(f.ctx, statement, f.writes[0].ObligationID)
			var denied *pgconn.PgError
			if !errors.As(err, &denied) || denied.Code != "55000" {
				t.Fatalf("physical identity forgery was not rejected: %v", err)
			}
		}
		f.assertNoLastEffects(t)
	})
	t.Run("assigned_elsewhere", func(t *testing.T) {
		f := newPortabilityV1IndependentFixture(t)
		f.retainFirst(t)
		if _, err := f.db.db.Exec(f.ctx, `update public.record_write_obligations set operation_id=$2 where obligation_id=$1 and operation_id is null`, f.writes[0].ObligationID, f.targets[0].Operation.OperationID); err != nil {
			t.Fatalf("arrange previously assigned responsibility: %v", err)
		}
		var assigned bool
		if err := f.db.db.QueryRow(f.ctx, `select operation_id=$2 from public.record_write_obligations where obligation_id=$1`, f.writes[0].ObligationID, f.targets[0].Operation.OperationID).Scan(&assigned); err != nil || !assigned {
			t.Fatalf("prior assignment unavailable: %v %v", assigned, err)
		}
		prep, err := f.repository.preparePortabilityV1(f.ctx, f.targets[1], recorddeletion.RecordPortabilitySurfaceDigest())
		if err == nil && prep.UnresolvedCount == 0 {
			t.Fatalf("previously assigned source authorized cleanup: %+v", prep)
		}
		f.assertNoLastEffects(t)
	})
	t.Run("absent_carried_source", func(t *testing.T) {
		f := newPortabilityV1IndependentFixture(t)
		f.retainFirst(t)
		_, err := f.db.db.Exec(f.ctx, `delete from public.record_write_obligations where obligation_id=$1`, f.writes[0].ObligationID)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "55000" {
			t.Fatalf("absent carried source was not rejected: %v", err)
		}
		f.assertNoLastEffects(t)
	})
}

type portabilityV1IndependentCommitGate struct {
	pgx.Tx
	selected bool
	pid      int32
	entered  chan<- int32
	release  <-chan struct{}
}

func (tx *portabilityV1IndependentCommitGate) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if query == `select public.record_portability_purge_prepare_v1($1)` {
		tx.selected = true
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}
func (tx *portabilityV1IndependentCommitGate) Commit(ctx context.Context) error {
	if tx.selected {
		tx.entered <- tx.pid
		select {
		case <-tx.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return tx.Tx.Commit(ctx)
}

func runPortabilityV1IndependentConcurrentPrepares(t *testing.T) {
	f := newPortabilityV1IndependentFixture(t)
	firstReceipt := f.retainFirst(t)
	snapshot := f.snapshot(t, 1)
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	firstRepo := NewPostgresRecordDeletionRepository(f.worker, allowRecordPlatformAdmissionGate)
	secondRepo := NewPostgresRecordDeletionRepository(f.worker, allowRecordPlatformAdmissionGate)
	if err := firstRepo.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, f.observer); err != nil {
		t.Fatal(err)
	}
	if err := secondRepo.ConfigurePortabilityDeletionBlobStore(attachments.BackendKindS3, f.observer); err != nil {
		t.Fatal(err)
	}
	hold := make(chan int32, 1)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	firstRepo.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := f.worker.BeginTx(ctx, options)
		if err != nil {
			return nil, err
		}
		var pid int32
		if err := tx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		return &portabilityV1IndependentCommitGate{Tx: tx, pid: pid, entered: hold, release: release}, nil
	}
	secondPID := make(chan int32, 1)
	secondRepo.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := f.worker.BeginTx(ctx, options)
		if err != nil {
			return nil, err
		}
		var pid int32
		if err := tx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		select {
		case secondPID <- pid:
		default:
		}
		return tx, nil
	}
	type outcome struct {
		preparation portabilityV1Preparation
		err         error
	}
	firstResult, secondResult := make(chan outcome, 1), make(chan outcome, 1)
	go func() {
		p, err := firstRepo.preparePortabilityV1(ctx, f.targets[1], surface)
		firstResult <- outcome{p, err}
	}()
	var firstPID int32
	select {
	case firstPID = <-hold:
	case result := <-firstResult:
		t.Fatalf("first prepare did not reach commit gate: %v", result.err)
	case <-ctx.Done():
		t.Fatal("first prepare commit gate timed out")
	}
	go func() {
		p, err := secondRepo.preparePortabilityV1(ctx, f.targets[1], surface)
		secondResult <- outcome{p, err}
	}()
	var pid int32
	select {
	case pid = <-secondPID:
	case result := <-secondResult:
		t.Fatalf("second prepare did not start: %v", result.err)
	case <-ctx.Done():
		t.Fatal("second prepare did not start")
	}
	var second outcome
	secondFinished := false
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	observedContention := false
	for !observedContention && !secondFinished {
		select {
		case second = <-secondResult:
			secondFinished = true
			var pgErr *pgconn.PgError
			if second.err == nil || !errors.As(second.err, &pgErr) || pgErr.Code != "40001" {
				t.Fatalf("second prepare escaped live competitor: %v", second.err)
			}
		case <-ticker.C:
			if err := f.db.db.QueryRow(ctx, `select $2::integer=any(pg_blocking_pids($1::integer))`, pid, firstPID).Scan(&observedContention); err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("successor prepare contention was not observed")
		}
	}
	close(release)
	released = true
	var first outcome
	select {
	case first = <-firstResult:
	case <-ctx.Done():
		t.Fatal("winning prepare did not commit")
	}
	if first.err != nil {
		t.Fatalf("winning prepare failed: %v", first.err)
	}
	if !secondFinished {
		select {
		case second = <-secondResult:
		case <-ctx.Done():
			t.Fatal("competing prepare did not resolve")
		}
	}
	if second.err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(second.err, &pgErr) || pgErr.Code != "40001" {
			t.Fatalf("competing prepare failed unexpectedly: %v", second.err)
		}
	} else if second.preparation.InventoryDigest != first.preparation.InventoryDigest || len(second.preparation.RequiredBackends) != 0 || len(second.preparation.S3Targets) != 1 || second.preparation.S3Targets[0] != first.preparation.S3Targets[0] {
		t.Fatalf("competing prepare was not immutable replay: %+v", second.preparation)
	}
	var preparations, targetCount int
	if err := f.db.db.QueryRow(ctx, `select (select count(*) from public.record_portability_v1_preparations where operation_id=$1),(select count(*) from public.record_portability_v1_targets where operation_id=$1 and disposition='s3')`, f.targets[1].Operation.OperationID).Scan(&preparations, &targetCount); err != nil || preparations != 1 || targetCount != 1 || f.observer.calls != 0 {
		t.Fatalf("competing prepares count=%d targets=%d deletes=%d error=%v", preparations, targetCount, f.observer.calls, err)
	}
	f.checkSuccessor(t, first.preparation, snapshot)
	f.finishLast(t, firstReceipt, first.preparation)
}
