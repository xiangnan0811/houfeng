//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

const c80EffectsRevision = "ec38a48d3538735ee343698d7ed5b4ecfeb4ad12"
const c80EffectsActor = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"

type c80EffectsRequest struct {
	Action             string                                   `json:"action"`
	DatabaseURL        string                                   `json:"database_url"`
	RuntimeRole        string                                   `json:"runtime_role"`
	AdminRole          string                                   `json:"admin_role"`
	DeploymentID       recordplatform.DeploymentID              `json:"deployment_id"`
	Preview            recorddeletion.CreatePreviewCommand      `json:"preview"`
	Fingerprint        recordplatform.RequestFingerprintInputV1 `json:"fingerprint"`
	TokenTransport     string                                   `json:"token_transport"`
	ObservedWitness    recorddeletion.WitnessHead               `json:"observed_witness"`
	ActorID            string                                   `json:"actor_id"`
	OwnerID            string                                   `json:"owner_id"`
	OwnerLeaseDuration time.Duration                            `json:"owner_lease_duration"`
}

type c80EffectsReply struct {
	Op            string                            `json:"op"`
	Backend       ledgerPredecessorBackend          `json:"backend"`
	ReservationID string                            `json:"reservation_id"`
	ExpiresAt     time.Time                         `json:"expires_at"`
	Operation     *recorddeletion.DeletionOperation `json:"operation"`
}

type c80EffectsDatabase struct {
	app         *pgxpool.Pool
	runtimeURL  string
	runtimeRole string
	adminRole   string
	deployment  recordplatform.DeploymentID
	identity    RecordLedgerApplicationIdentity
	state       recordauthority.VerifiedComposeState
}

type c80EffectsChild struct {
	cmd     *exec.Cmd
	input   *os.File
	output  *os.File
	encoder *json.Encoder
	decoder *json.Decoder
	logs    bytes.Buffer
	backend ledgerPredecessorBackend
	stopped bool
}

type c80EffectsJournal struct {
	dir     string
	fd      int
	binding ledgerPredecessorEffectBinding
	intent  ledgerPredecessorIntent
	fence   *RecordLedgerFence
}

// This checks the lower-level C80 repository effects and recovery readbacks.
// The ordinary record rows are fixture data; the C80 repository, not the test,
// creates every preview, reservation, operation, fence lease, and audit. The
// journal records are published around those actual calls, with the actual
// runtime backend identity. This is not Service, realm, or offline-fence
// acceptance and does not assert that a live supervisor issued an ACK.
func TestPostgresIntegrationC80EffectsReadback(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_POSTGRES_INTEGRATION=1 through the strict PostgreSQL runner")
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" || os.Getenv("HOUFENG_DATABASE_URL") == "" || os.Getenv("HOUFENG_RECOVERY_CONTROL_DATABASE_URL") == "" {
		t.Fatal("C80 effects require the ownership-checked isolated PostgreSQL runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	binary := buildC80EffectsFixture(t, ctx)
	db := newC80EffectsDatabase(t, ctx, binary)

	t.Run("original create intent without a preview", func(t *testing.T) {
		req, create := db.newRequest(t, ctx, "rec_c80absent", "rrv_c80absent")
		child := db.startChild(t, ctx, binary)
		journal := newC80EffectsJournal(t, db, child.backend, create.ObjectID)
		createDigest := c80EffectsCommandDigest(t, create)
		journal.publish(t, "create-intent", ledgerPredecessorCreateIntent{
			ledgerPredecessorEffectBinding: journal.binding, Command: create, CommandDigest: createDigest,
		})
		if _, found, err := ledgerC80ReadPreview(ctx, db.app, create); err != nil || found {
			t.Fatalf("unissued commitment: found=%t error=%v", found, err)
		}
		child.stop(t, ctx, db.app)
		resolution, err := ledgerResolvePredecessor(ctx, journal.fence, journal.intent, journal.binding)
		if err != nil || resolution.Outcome != "preview_not_created" || resolution.CreateCommandDigest != createDigest || resolution.ReservationID != "" || resolution.OperationID != "" {
			t.Fatalf("resolve untouched original intent: %+v, error=%v", resolution, err)
		}
		journal.assertNoToken(t, req.TokenTransport)
	})

	t.Run("preview without result ACK is abandoned", func(t *testing.T) {
		req, create := db.newRequest(t, ctx, "rec_c80preview", "rrv_c80preview")
		child := db.startChild(t, ctx, binary)
		journal := newC80EffectsJournal(t, db, child.backend, create.ObjectID)
		createDigest := c80EffectsCommandDigest(t, create)
		original := ledgerPredecessorCreateIntent{ledgerPredecessorEffectBinding: journal.binding, Command: create, CommandDigest: createDigest}
		journal.publish(t, "create-intent", original)
		before := journal.read(t, "create-intent")
		if fresh, err := ledgerPublishExact(journal.fd, ledgerPredecessorName("create-intent", journal.intent.PhaseGeneration), original); err != nil || fresh {
			t.Fatalf("exact original intent replay: fresh=%t error=%v", fresh, err)
		}
		conflict := original
		conflict.Command.BindingDigest = c80EffectsHex("conflicting intent")
		if _, err := ledgerPublishExact(journal.fd, ledgerPredecessorName("create-intent", journal.intent.PhaseGeneration), conflict); !errors.Is(err, ErrRecordLedgerFence) {
			t.Fatalf("conflicting immutable intent: %v", err)
		}
		if !bytes.Equal(before, journal.read(t, "create-intent")) {
			t.Fatal("conflicting replay changed the original create intent")
		}
		actual := child.preview(t, req)
		child.stop(t, ctx, db.app)
		preview := assertC80EffectsPreview(t, ctx, db.app, create, actual)
		if preview.State != "previewed" || preview.FenceEpoch != 0 {
			t.Fatalf("unexpected unfenced preview: %+v", preview)
		}
		if err := ledgerC80PreviewAbandonable(ctx, db.app, create, preview); err != nil {
			t.Fatalf("actual preview is not abandonable: %v", err)
		}
		resolution, err := ledgerResolvePredecessor(ctx, journal.fence, journal.intent, journal.binding)
		if err != nil || resolution.Outcome != "preview_abandoned" || resolution.ReservationID != actual.ReservationID || resolution.OperationID != "" {
			t.Fatalf("resolve preview with lost result ACK: %+v, error=%v", resolution, err)
		}
		wrongCommitment := create
		wrongCommitment.TokenCommitment = c80EffectsHex("another token commitment")
		if _, found, err := ledgerC80ReadPreview(ctx, db.app, wrongCommitment); err != nil || found {
			t.Fatalf("unrelated commitment: found=%t error=%v", found, err)
		}
		wrongBinding := create
		wrongBinding.BindingDigest = c80EffectsHex("another binding")
		if _, _, err := ledgerC80ReadPreview(ctx, db.app, wrongBinding); !errors.Is(err, ErrRecordLedgerFence) {
			t.Fatalf("same commitment with conflicting binding: %v", err)
		}
		c80EffectsWithRollback(t, ctx, db.app, func(tx pgx.Tx) {
			if _, err := tx.Exec(ctx, `update public.deletion_reservations set preview_binding_digest=$2 where reservation_id=$1`, actual.ReservationID, c80EffectsDigest("changed persisted binding")); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ledgerC80ReadPreview(ctx, tx, create); !errors.Is(err, ErrRecordLedgerFence) {
				t.Fatalf("changed persisted binding: %v", err)
			}
		})
		c80EffectsWithRollback(t, ctx, db.app, func(tx pgx.Tx) {
			if _, err := tx.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=delivery_epoch+1 where project_id='default' and object_kind='record' and object_id=$1`, create.ObjectID); err != nil {
				t.Fatal(err)
			}
			if err := ledgerC80PreviewAbandonable(ctx, tx, create, preview); !errors.Is(err, ErrRecordLedgerFence) {
				t.Fatalf("changed delivery epoch cannot be abandoned: %v", err)
			}
		})
		// An absent create-result is allowed above. It is not allowed once a
		// reserve intent claims that the preview result was acknowledged.
		reserve := c80EffectsReserve(create, createDigest, req, actual.ReservationID)
		journal.publish(t, "reserve-intent", ledgerPredecessorReserveIntent{
			ledgerPredecessorEffectBinding: journal.binding, Command: reserve, CommandDigest: c80EffectsCommandDigest(t, reserve),
		})
		if _, err := ledgerResolvePredecessor(ctx, journal.fence, journal.intent, journal.binding); !errors.Is(err, ErrRecordLedgerFence) {
			t.Fatalf("reserve intent without preview ACK: %v", err)
		}
		if !bytes.Equal(before, journal.read(t, "create-intent")) {
			t.Fatal("resolution changed the original create intent")
		}
		journal.assertNoToken(t, req.TokenTransport)
	})

	t.Run("reserve intent without commit abandons the same preview", func(t *testing.T) {
		req, create := db.newRequest(t, ctx, "rec_c80unreserved", "rrv_c80unreserved")
		child := db.startChild(t, ctx, binary)
		journal := newC80EffectsJournal(t, db, child.backend, create.ObjectID)
		createDigest := c80EffectsCommandDigest(t, create)
		journal.publish(t, "create-intent", ledgerPredecessorCreateIntent{ledgerPredecessorEffectBinding: journal.binding, Command: create, CommandDigest: createDigest})
		actual := child.preview(t, req)
		journal.publishPreview(t, createDigest, actual)
		reserve := c80EffectsReserve(create, createDigest, req, actual.ReservationID)
		reserveDigest := c80EffectsCommandDigest(t, reserve)
		journal.publish(t, "reserve-intent", ledgerPredecessorReserveIntent{ledgerPredecessorEffectBinding: journal.binding, Command: reserve, CommandDigest: reserveDigest})
		child.stop(t, ctx, db.app)
		operation, found, err := ledgerC80ReadOperation(ctx, db.app, create, reserve)
		if err != nil || found || operation != "" {
			t.Fatalf("uncommitted reserve readback: operation=%q found=%t error=%v", operation, found, err)
		}
		resolution, err := ledgerResolvePredecessor(ctx, journal.fence, journal.intent, journal.binding)
		if err != nil || resolution.Outcome != "reserve_not_committed_preview_abandoned" || resolution.ReservationID != actual.ReservationID || resolution.OperationID != "" || resolution.ReserveCommandDigest != reserveDigest {
			t.Fatalf("resolve uncommitted reserve: %+v, error=%v", resolution, err)
		}
		journal.assertNoToken(t, req.TokenTransport)
	})

	t.Run("committed reserve survives lost result ACK", func(t *testing.T) {
		req, create := db.newRequest(t, ctx, "rec_c80committed", "rrv_c80committed")
		child := db.startChild(t, ctx, binary)
		journal := newC80EffectsJournal(t, db, child.backend, create.ObjectID)
		createDigest := c80EffectsCommandDigest(t, create)
		journal.publish(t, "create-intent", ledgerPredecessorCreateIntent{ledgerPredecessorEffectBinding: journal.binding, Command: create, CommandDigest: createDigest})
		actual := child.preview(t, req)
		journal.publishPreview(t, createDigest, actual)
		reserve := c80EffectsReserve(create, createDigest, req, actual.ReservationID)
		reserveDigest := c80EffectsCommandDigest(t, reserve)
		journal.publish(t, "reserve-intent", ledgerPredecessorReserveIntent{ledgerPredecessorEffectBinding: journal.binding, Command: reserve, CommandDigest: reserveDigest})
		intentBytes := journal.read(t, "reserve-intent")
		committed := child.reserve(t)
		child.stop(t, ctx, db.app)
		if committed.Operation == nil || committed.ReservationID != actual.ReservationID || committed.Operation.ReservationID != actual.ReservationID || committed.Operation.Object != req.Preview.Object || committed.Operation.State != recorddeletion.DeletionStateProvisionalFenced || committed.Operation.FenceEpoch != 1 {
			t.Fatalf("unexpected real C80 reservation: %+v", committed)
		}
		committed.Operation.ContractVersion = recorddeletion.DeletionContractV1
		if err := committed.Operation.Validate(); err != nil {
			t.Fatalf("invalid real C80 operation: %v", err)
		}
		preview := assertC80EffectsPreview(t, ctx, db.app, create, actual)
		if preview.State != "fenced" || preview.FenceEpoch != 1 {
			t.Fatalf("unexpected fenced preview: %+v", preview)
		}
		if err := ledgerC80PreviewAbandonable(ctx, db.app, create, preview); !errors.Is(err, ErrRecordLedgerFence) {
			t.Fatalf("fenced preview cannot be abandoned: %v", err)
		}
		// Exercise the effect handler directly: this is not a live IPC or Service
		// acceptance test. The already-fenced reservation must still replay its
		// exact, previously persisted preview-result acknowledgement.
		var savedCreate ledgerPredecessorCreateIntent
		if found, err := ledgerPredecessorRead(journal.fd, "create-intent", journal.intent.PhaseGeneration, &savedCreate); err != nil || !found {
			t.Fatalf("read original create intent: found=%t error=%v", found, err)
		}
		var savedPreview ledgerPredecessorCreateResult
		if found, err := ledgerPredecessorRead(journal.fd, "create-result", journal.intent.PhaseGeneration, &savedPreview); err != nil || !found {
			t.Fatalf("read original preview result: found=%t error=%v", found, err)
		}
		call := &ledgerPredecessorCall{fence: journal.fence, intent: journal.intent, backend: journal.binding.Backend, create: &savedCreate, preview: &savedPreview}
		frame := struct {
			Op              string `json:"op"`
			PhaseGeneration uint64 `json:"phase_generation"`
			CommandDigest   string `json:"command_digest"`
			ReservationID   string `json:"reservation_id"`
			ExpiresAt       string `json:"expires_at"`
		}{"preview_result", journal.intent.PhaseGeneration, createDigest, actual.ReservationID, actual.ExpiresAt.UTC().Format(time.RFC3339Nano)}
		body, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		previewResultBytes := journal.read(t, "create-result")
		reply, err := call.effect(ctx, "preview_result", body)
		ack, ok := reply.(map[string]any)
		if err != nil || !ok || len(ack) != 4 || ack["op"] != "preview_result_ack" || ack["phase_generation"] != journal.intent.PhaseGeneration || ack["command_digest"] != createDigest || ack["reservation_id"] != actual.ReservationID {
			t.Fatalf("replay original preview result after reserve: reply=%v error=%v", reply, err)
		}
		frame.ReservationID = "drs_conflicttuple"
		body, err = json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := call.effect(ctx, "preview_result", body); !errors.Is(err, ErrRecordLedgerFence) {
			t.Fatalf("conflicting preview result replay: %v", err)
		}
		if !bytes.Equal(previewResultBytes, journal.read(t, "create-result")) {
			t.Fatal("preview ACK replay changed the original result receipt")
		}

		operation, found, err := ledgerC80ReadOperation(ctx, db.app, create, reserve)
		if err != nil || !found || operation != committed.Operation.OperationID {
			t.Fatalf("exact original operation readback: operation=%q found=%t error=%v", operation, found, err)
		}
		var reservationID, operationID string
		var count int
		if err := db.app.QueryRow(ctx, `select r.reservation_id, o.operation_id, (select count(*) from public.record_purge_operations where reservation_id=r.reservation_id)::integer from public.deletion_reservations r join public.record_purge_operations o on o.reservation_id=r.reservation_id where r.deletion_token_commitment=$1`, req.Preview.TokenCommitment[:]).Scan(&reservationID, &operationID, &count); err != nil || reservationID != actual.ReservationID || operationID != committed.Operation.OperationID || count != 1 {
			t.Fatalf("unique commitment to original reservation/operation: reservation=%q operation=%q count=%d error=%v", reservationID, operationID, count, err)
		}
		resolution, err := ledgerResolvePredecessor(ctx, journal.fence, journal.intent, journal.binding)
		if err != nil || resolution.Outcome != "reserve_committed" || resolution.ReservationID != actual.ReservationID || resolution.OperationID != committed.Operation.OperationID || resolution.ReserveCommandDigest != reserveDigest {
			t.Fatalf("resolve committed reserve without result receipt: %+v, error=%v", resolution, err)
		}
		journal.publish(t, "reserve-result", ledgerPredecessorReserveResult{
			ledgerPredecessorEffectBinding: journal.binding, CreateCommandDigest: createDigest, CommandDigest: reserveDigest,
			ReservationID: actual.ReservationID, OperationID: committed.Operation.OperationID,
		})
		repeated, err := ledgerResolvePredecessor(ctx, journal.fence, journal.intent, journal.binding)
		if err != nil || repeated != resolution {
			t.Fatalf("resolve exact committed receipt: %+v, error=%v", repeated, err)
		}
		if !bytes.Equal(intentBytes, journal.read(t, "reserve-intent")) {
			t.Fatal("resolution changed the original reserve intent")
		}
		wrong := reserve
		wrong.ExpectedBindingDigest = c80EffectsHex("wrong reserve binding")
		if _, _, err := ledgerC80ReadOperation(ctx, db.app, create, wrong); !errors.Is(err, ErrRecordLedgerFence) {
			t.Fatalf("conflicting reserve binding: %v", err)
		}
		c80EffectsWithRollback(t, ctx, db.app, func(tx pgx.Tx) {
			if _, err := tx.Exec(ctx, `update public.record_purge_operations set owner_generation=owner_generation+1 where operation_id=$1`, operation); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ledgerC80ReadOperation(ctx, tx, create, reserve); !errors.Is(err, ErrRecordLedgerFence) {
				t.Fatalf("conflicting persisted owner generation: %v", err)
			}
		})
		c80EffectsWithRollback(t, ctx, db.app, func(tx pgx.Tx) {
			if _, err := tx.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=delivery_epoch+1 where project_id='default' and object_kind='record' and object_id=$1`, create.ObjectID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ledgerC80ReadOperation(ctx, tx, create, reserve); !errors.Is(err, ErrRecordLedgerFence) {
				t.Fatalf("conflicting committed delivery epoch: %v", err)
			}
		})
		journal.assertNoToken(t, req.TokenTransport)
	})

	t.Run("acknowledged preview missing from original APP is unresolved", func(t *testing.T) {
		req, create := db.newRequest(t, ctx, "rec_c80missing", "rrv_c80missing")
		child := db.startChild(t, ctx, binary)
		journal := newC80EffectsJournal(t, db, child.backend, create.ObjectID)
		createDigest := c80EffectsCommandDigest(t, create)
		journal.publish(t, "create-intent", ledgerPredecessorCreateIntent{ledgerPredecessorEffectBinding: journal.binding, Command: create, CommandDigest: createDigest})
		actual := child.preview(t, req)
		journal.publishPreview(t, createDigest, actual)
		child.stop(t, ctx, db.app)
		resultBytes := journal.read(t, "create-result")
		command, err := db.app.Exec(ctx, `delete from public.deletion_reservations where reservation_id=$1`, actual.ReservationID)
		if err != nil || command.RowsAffected() != 1 {
			t.Fatalf("remove fixture preview to model lost APP row: rows=%d error=%v", command.RowsAffected(), err)
		}
		if _, found, err := ledgerC80ReadPreview(ctx, db.app, create); err != nil || found {
			t.Fatalf("deleted original commitment: found=%t error=%v", found, err)
		}
		if _, err := ledgerResolvePredecessor(ctx, journal.fence, journal.intent, journal.binding); !errors.Is(err, ErrRecordLedgerFence) {
			t.Fatalf("missing acknowledged preview was treated as absent: %v", err)
		}
		if !bytes.Equal(resultBytes, journal.read(t, "create-result")) {
			t.Fatal("resolution changed the acknowledged original preview receipt")
		}
		journal.assertNoToken(t, req.TokenTransport)
	})
}

func buildC80EffectsFixture(t *testing.T, ctx context.Context) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate C80 fixture sources")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "predecessor")
	if err := os.Mkdir(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := exec.CommandContext(ctx, "git", "archive", c80EffectsRevision)
	archive.Dir = repository
	stream, err := archive.StdoutPipe()
	if err != nil {
		t.Fatalf("open pinned predecessor archive: %v", err)
	}
	extract := exec.CommandContext(ctx, "tar", "-x", "-C", snapshot)
	extract.Stdin = stream
	var archiveErrors, extractErrors bytes.Buffer
	archive.Stderr, extract.Stderr = &archiveErrors, &extractErrors
	if err := archive.Start(); err != nil {
		t.Fatalf("start pinned predecessor archive: %v", err)
	}
	extractErr := extract.Run()
	if extractErr != nil {
		_ = stream.Close()
	}
	archiveErr := archive.Wait()
	if archiveErr != nil || extractErr != nil {
		t.Fatalf("extract pinned predecessor: archive=%v %s extract=%v %s", archiveErr, archiveErrors.String(), extractErr, extractErrors.String())
	}
	commandDir := filepath.Join(snapshot, "cmd", "record-deletion-predecessor-fixture")
	if err := os.MkdirAll(commandDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.go", "inventory.go"} {
		source, err := os.ReadFile(filepath.Join(repository, "internal", "center", "store", "testdata", "legacydeletion", name))
		if err != nil {
			t.Fatalf("read existing C80 wrapper %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(commandDir, name), source, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("C80 wrapper %s sha256=%x", name, sha256.Sum256(source))
	}
	if err := os.WriteFile(filepath.Join(commandDir, "effects_fixture_test.go"), []byte(c80EffectsFixtureSource), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "c80-effects-fixture")
	build := exec.CommandContext(ctx, "go", "test", "-c", "-o", binary, "./cmd/record-deletion-predecessor-fixture")
	build.Dir = snapshot
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build pinned C80 repository fixture: %v: %s", err, output)
	}
	return binary
}

func newC80EffectsDatabase(t *testing.T, ctx context.Context, binary string) c80EffectsDatabase {
	t.Helper()
	// The runner's separate recovery-control server keeps its cluster-global
	// role catalog independent of APP fixtures in other Go test packages. This
	// test creates and drops its own C80 APP database on that isolated server.
	baseURL, err := url.Parse(os.Getenv("HOUFENG_RECOVERY_CONTROL_DATABASE_URL"))
	if err != nil || (baseURL.Scheme != "postgres" && baseURL.Scheme != "postgresql") || baseURL.User == nil || baseURL.User.Username() != "postgres" || baseURL.Path != "/postgres" {
		t.Fatal("isolated runner requires a direct postgres bootstrap URL")
	}
	host := net.ParseIP(baseURL.Hostname())
	if host == nil || !host.IsLoopback() || baseURL.Port() == "" {
		t.Fatal("isolated runner requires a loopback PostgreSQL endpoint")
	}
	appURL, err := url.Parse(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil || appURL.Host == baseURL.Host {
		t.Fatal("C80 readback fixture requires the runner's separate recovery-control endpoint")
	}
	base, err := pgxpool.New(ctx, baseURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(base.Close)
	var suffixBytes [8]byte
	if _, err := rand.Read(suffixBytes[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(suffixBytes[:])
	database := "houfeng_delivery_c80_" + suffix
	runtimeRole, adminRole, migratorRole := "hf_c80_runtime_"+suffix, "hf_c80_admin_"+suffix, "hf_c80_migrator_"+suffix
	roles := []string{runtimeRole, adminRole, migratorRole}
	if _, err := base.Exec(ctx, `create database `+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatalf("create isolated C80 database: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := base.Exec(cleanup, `drop database if exists `+pgx.Identifier{database}.Sanitize()+` with (force)`); err != nil {
			t.Errorf("drop isolated C80 database: %v", err)
		}
		for _, role := range roles {
			if _, err := base.Exec(cleanup, `drop role if exists `+pgx.Identifier{role}.Sanitize()); err != nil {
				t.Errorf("drop isolated C80 role %s: %v", role, err)
			}
		}
	})
	passwords := make(map[string]string, len(roles))
	for _, role := range roles {
		var random [24]byte
		if _, err := rand.Read(random[:]); err != nil {
			t.Fatal(err)
		}
		passwords[role] = hex.EncodeToString(random[:])
		if _, err := base.Exec(ctx, `create role `+pgx.Identifier{role}.Sanitize()+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '`+passwords[role]+`'`); err != nil {
			t.Fatalf("create isolated C80 direct role: %v", err)
		}
	}
	if _, err := base.Exec(ctx, `alter database `+pgx.Identifier{database}.Sanitize()+` owner to `+pgx.Identifier{migratorRole}.Sanitize()); err != nil {
		t.Fatalf("assign isolated C80 database owner: %v", err)
	}
	roleURL := func(role string) string {
		copy := *baseURL
		copy.Path = "/" + database
		if role != "postgres" {
			copy.User = url.UserPassword(role, passwords[role])
		}
		return copy.String()
	}
	app, err := pgxpool.New(ctx, roleURL("postgres"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	// Match the existing Compose bootstrap's pre-R1 control-function ACL in
	// this temporary database before the direct migrator converges C80.
	if _, err := app.Exec(ctx, `revoke execute on function pg_catalog.pg_control_system() from public`); err != nil {
		t.Fatalf("prepare isolated C80 bootstrap ACL: %v", err)
	}
	// Use the existing wrapper's request-file converge action, which executes
	// the pinned direct-login C80 migrations and ACL implementation.
	request := c80EffectsRequest{Action: "converge", DatabaseURL: roleURL(migratorRole), RuntimeRole: runtimeRole, AdminRole: adminRole}
	runC80EffectsConverge(t, ctx, binary, request)
	var predecessor, successor bool
	if err := app.QueryRow(ctx, `select exists(select 1 from public.schema_migrations where name='0080_bound_agent_sync_replay.sql'), exists(select 1 from public.schema_migrations where name='0081_record_deletion_successor.sql')`).Scan(&predecessor, &successor); err != nil || !predecessor || successor {
		t.Fatalf("expected exact C80 migration boundary: c80=%t c81=%t error=%v", predecessor, successor, err)
	}
	identity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, app, platformmigrate.DomainKindApplication)
	if err != nil {
		t.Fatalf("provision real isolated APP identity: %v", err)
	}
	root := filepath.Join(t.TempDir(), "authority")
	state, err := recordauthority.CreateComposeState(root)
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := pgxpool.New(ctx, roleURL(migratorRole))
	if err != nil {
		t.Fatal(err)
	}
	if err := activateComposeAuthorityState(ctx, migrator, root, state); err != nil {
		migrator.Close()
		t.Fatalf("activate isolated C80 deployment: %v", err)
	}
	migrator.Close()
	return c80EffectsDatabase{
		app: app, runtimeURL: roleURL(runtimeRole), runtimeRole: runtimeRole, adminRole: adminRole,
		deployment: state.DeploymentID, state: state,
		identity: RecordLedgerApplicationIdentity{ID: identity.ID, SystemIdentifier: identity.PostgresSystemIdentifier, DatabaseName: identity.DatabaseName, DatabaseOID: identity.DatabaseOID, Epoch: uint64(identity.IdentityEpoch)},
	}
}

func runC80EffectsConverge(t *testing.T, ctx context.Context, binary string, request c80EffectsRequest) {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "converge-request.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	// The frozen wrapper's main is used by a separate binary because the
	// Go test binary runs the persistent-backend fixture entry point instead.
	// See buildC80EffectsConverge for the unchanged request-file mode.
	converge := buildC80EffectsConverge(t, ctx, binary)
	command := exec.CommandContext(ctx, converge, path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("converge pinned C80 schema: %v: %s", err, output)
	}
}

// Build the unchanged request-file entry point from the same archived tree.
func buildC80EffectsConverge(t *testing.T, ctx context.Context, fixtureBinary string) string {
	t.Helper()
	directory := filepath.Dir(fixtureBinary)
	binary := filepath.Join(directory, "c80-request-file")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/record-deletion-predecessor-fixture")
	build.Dir = filepath.Join(directory, "predecessor")
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build pinned C80 request-file wrapper: %v: %s", err, output)
	}
	return binary
}

func (db c80EffectsDatabase) newRequest(t *testing.T, ctx context.Context, recordID, revisionID string) (c80EffectsRequest, ledgerC80CreateCommand) {
	t.Helper()
	// Ordinary, non-deletion fixture data only. These rows are not claimed to
	// prove record authorization or a successful Service invocation.
	sourceHash := sha256.Sum256([]byte(recordID))
	sourceID := "vps_" + hex.EncodeToString(sourceHash[:8])
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("normalize fixture visibility: %v", err)
	}
	visibilityJSON, err := json.Marshal(visibility)
	if err != nil {
		t.Fatalf("encode fixture visibility: %v", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindVPS,
		SourceID: sourceID, State: recordauth.SourceStateLive, CaptureScope: visibility, CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("normalize fixture source authorization: %v", err)
	}
	authorizationJSON, err := json.Marshal(authorization)
	if err != nil {
		t.Fatalf("encode fixture source authorization: %v", err)
	}
	c80EffectsWithTransaction(t, ctx, db.app, func(tx pgx.Tx) {
		if _, err := tx.Exec(ctx, `insert into public.vps_assets (vps_id,display_name,lifecycle_status,usage_status) values ($1,'C80 readback VPS','active','in_use')`, sourceID); err != nil {
			t.Fatalf("seed fixture VPS: %v", err)
		}
		if _, err := tx.Exec(ctx, `insert into public.records (record_id) values ($1)`, recordID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `insert into public.record_revisions (revision_id,record_id,revision_no,title,body_markdown,markdown_dialect_version,record_type,impact_level,visibility_scope,visibility_digest,author_id,canonical_hash) values ($1,$2,1,'C80 readback fixture','fixture content',1,'note','informational',$3::jsonb,$4,$5,$6)`, revisionID, recordID, visibilityJSON, visibility.CanonicalHash[:], c80EffectsActor, c80EffectsDigest("revision")); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `insert into public.record_revision_subjects (revision_id,ordinal,registry_version,subject_kind,relation_role,source_id,is_primary,identity_snapshot,capture_authorization,capture_authorization_digest) values ($1,0,1,'vps','affected',$2,true,'{"display_name":"C80 readback VPS"}'::jsonb,$3::jsonb,$4)`, revisionID, sourceID, authorizationJSON, authorization.Digest[:]); err != nil {
			t.Fatalf("seed fixture primary subject: %v", err)
		}
		if _, err := tx.Exec(ctx, `update public.records set current_revision_id=$2,current_title='C80 readback fixture',current_record_type='note',current_impact_level='informational',current_visibility_scope=$5::jsonb,current_visibility_digest=$3,current_owner_id=$4,lock_version=1,authorization_epoch=1 where record_id=$1`, recordID, revisionID, visibility.CanonicalHash[:], c80EffectsActor, visibilityJSON); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `insert into public.content_delivery_epochs (project_id,object_kind,object_id,delivery_epoch) values ('default','record',$1,0)`, recordID); err != nil {
			t.Fatal(err)
		}
	})
	issued, err := recordplatform.NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatal(err)
	}
	commitment, err := issued.Commitment(db.deployment, recordplatform.ProjectIDDefault)
	if err != nil {
		t.Fatal(err)
	}
	actor, binding := sha256.Sum256([]byte("actor scope")), sha256.Sum256([]byte("preview binding"))
	fingerprintInput := recordplatform.RequestFingerprintInputV1{
		Version: recordplatform.RequestFingerprintVersionV1, OperationKind: recordplatform.OperationKindRecordPermanentDelete,
		ProjectID: recordplatform.ProjectIDDefault, ActorScopeDigest: actor,
		RequestScopeDigest: sha256.Sum256([]byte("request scope")), PayloadDigest: binding,
	}
	fingerprint, err := recordplatform.FingerprintRequestV1(fingerprintInput)
	if err != nil {
		t.Fatal(err)
	}
	fingerprintBytes, err := fingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	witness := recorddeletion.WitnessHead{Sequence: 7, EntryHash: sha256.Sum256([]byte("fixture witness"))}
	preview := recorddeletion.CreatePreviewCommand{
		Object:           recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: recordID},
		ActorScopeDigest: actor, TokenCommitment: commitment, RequestFingerprint: fingerprint, BindingDigest: binding,
		Record: recorddeletion.DeletionRecordSnapshot{
			RecordID: recordID, CurrentRevisionID: revisionID, LockVersion: 1, AuthorizationEpoch: 1,
			Authorization:         recordauth.ResourceScope{Version: recordauth.ResourceScopeVersionV1, ProjectID: recordauth.ProjectIDDefault},
			DependencyGraphDigest: sha256.Sum256([]byte("fixture dependencies")), BackupInventoryDigest: sha256.Sum256([]byte("fixture backup")),
			ProcessorInventoryDigest: sha256.Sum256([]byte("fixture processor")),
		},
		WitnessHead: witness, AdapterReadinessDigest: sha256.Sum256([]byte("fixture readiness")),
		AdapterPreviewDigest: sha256.Sum256([]byte("fixture impact")), TTL: recorddeletion.DeletionPreviewTTL,
	}
	if err := preview.Validate(); err != nil {
		t.Fatalf("invalid fixture preview command: %v", err)
	}
	request := c80EffectsRequest{
		Action: "preview", DeploymentID: db.deployment, Preview: preview, Fingerprint: fingerprintInput, TokenTransport: issued.Transport(),
		ObservedWitness: witness, ActorID: c80EffectsActor, OwnerID: "c80_predecessor", OwnerLeaseDuration: 30 * time.Second,
	}
	create := ledgerC80CreateCommand{
		ProjectID: "default", ObjectKind: "record", ObjectID: recordID,
		ActorScopeDigest: hex.EncodeToString(actor[:]), TokenCommitment: hex.EncodeToString(commitment[:]),
		RequestFingerprint: hex.EncodeToString(fingerprintBytes[:]), BindingDigest: hex.EncodeToString(binding[:]),
		CurrentRevisionID: revisionID, LockVersion: 1, AuthorizationEpoch: 1, ContentDeliveryEpoch: 0,
		DependencyDigest: hex.EncodeToString(preview.Record.DependencyGraphDigest[:]), BackupDigest: hex.EncodeToString(preview.Record.BackupInventoryDigest[:]),
		ProcessorDigest: hex.EncodeToString(preview.Record.ProcessorInventoryDigest[:]), AdapterReadinessDigest: hex.EncodeToString(preview.AdapterReadinessDigest[:]),
		AdapterPreviewDigest: hex.EncodeToString(preview.AdapterPreviewDigest[:]), WitnessSequence: witness.Sequence,
		WitnessHash: hex.EncodeToString(witness.EntryHash[:]), TTLMicroseconds: uint64(preview.TTL.Microseconds()),
	}
	return request, create
}

func (db c80EffectsDatabase) startChild(t *testing.T, ctx context.Context, binary string) *c80EffectsChild {
	t.Helper()
	// The lower-level fixture uses a real signed membership command, invoked by
	// the isolated bootstrap connection so it does not create the globally
	// fixed Compose authority role used by other parallel package tests.
	if err := heartbeatComposeAuthority(ctx, db.app, db.state); err != nil {
		t.Fatalf("renew signed C80 fixture membership: %v", err)
	}
	readRequest, writeRequest, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	readReply, writeReply, err := os.Pipe()
	if err != nil {
		readRequest.Close()
		writeRequest.Close()
		t.Fatal(err)
	}
	child := &c80EffectsChild{input: writeRequest, output: readReply}
	child.cmd = exec.CommandContext(ctx, binary, "-test.run=^TestC80EffectsFixture$")
	child.cmd.Env = append(os.Environ(), "HOUFENG_C80_EFFECTS_CHILD=1")
	child.cmd.ExtraFiles = []*os.File{readRequest, writeReply}
	child.cmd.Stdout, child.cmd.Stderr = &child.logs, &child.logs
	if err := child.cmd.Start(); err != nil {
		readRequest.Close()
		writeRequest.Close()
		readReply.Close()
		writeReply.Close()
		t.Fatalf("start pinned C80 repository fixture: %v", err)
	}
	readRequest.Close()
	writeReply.Close()
	child.encoder, child.decoder = json.NewEncoder(writeRequest), json.NewDecoder(readReply)
	t.Cleanup(func() {
		if !child.stopped {
			_ = child.input.Close()
			_ = child.output.Close()
			_ = child.cmd.Process.Kill()
			_ = child.cmd.Wait()
		}
	})
	if err := child.encoder.Encode(c80EffectsRequest{Action: "connect", DatabaseURL: db.runtimeURL, RuntimeRole: db.runtimeRole, DeploymentID: db.deployment}); err != nil {
		t.Fatalf("connect C80 fixture: %v", err)
	}
	reply := child.receive(t, "ready")
	child.backend = reply.Backend
	child.backend.Started = child.backend.Started.UTC()
	if child.backend.PID <= 0 || child.backend.Started.IsZero() || child.backend.Role != db.runtimeRole {
		t.Fatalf("invalid observed C80 backend: %+v", child.backend)
	}
	var live bool
	if err := db.app.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_stat_activity where pid=$1 and backend_start=$2 and usename=$3 and datname=current_database())`, child.backend.PID, child.backend.Started, child.backend.Role).Scan(&live); err != nil || !live {
		t.Fatalf("fixture runtime backend was not observed: live=%t error=%v", live, err)
	}
	return child
}

func (child *c80EffectsChild) receive(t *testing.T, op string) c80EffectsReply {
	t.Helper()
	var reply c80EffectsReply
	if err := child.decoder.Decode(&reply); err != nil {
		// Wait before reading logs; the child can still be writing them.
		_ = child.input.Close()
		_ = child.output.Close()
		_ = child.cmd.Wait()
		child.stopped = true
		t.Fatalf("read C80 fixture %s: %v: %s", op, err, child.logs.String())
	}
	reply.Backend.Started = reply.Backend.Started.UTC()
	if reply.Op != op || (child.backend.PID != 0 && reply.Backend != child.backend) {
		t.Fatalf("unexpected C80 fixture response for %s: %+v", op, reply)
	}
	return reply
}

func (child *c80EffectsChild) preview(t *testing.T, request c80EffectsRequest) c80EffectsReply {
	t.Helper()
	if err := child.encoder.Encode(request); err != nil {
		t.Fatal(err)
	}
	reply := child.receive(t, "preview")
	if !ledgerC80ID(reply.ReservationID, "drs_") || reply.ExpiresAt.IsZero() || reply.Operation != nil {
		t.Fatalf("invalid repository preview: %+v", reply)
	}
	return reply
}

func (child *c80EffectsChild) reserve(t *testing.T) c80EffectsReply {
	t.Helper()
	if err := child.encoder.Encode(c80EffectsRequest{Action: "reserve"}); err != nil {
		t.Fatal(err)
	}
	return child.receive(t, "reserve")
}

func (child *c80EffectsChild) stop(t *testing.T, ctx context.Context, app *pgxpool.Pool) {
	t.Helper()
	if err := child.encoder.Encode(c80EffectsRequest{Action: "quit"}); err != nil {
		t.Fatal(err)
	}
	child.receive(t, "quit")
	_ = child.input.Close()
	_ = child.output.Close()
	if err := child.cmd.Wait(); err != nil {
		child.stopped = true
		t.Fatalf("wait for C80 fixture backend: %v: %s", err, child.logs.String())
	}
	child.stopped = true
	settle, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var live bool
		if err := app.QueryRow(settle, `select exists(select 1 from pg_catalog.pg_stat_activity where pid=$1 and backend_start=$2)`, child.backend.PID, child.backend.Started).Scan(&live); err != nil {
			t.Fatalf("observe original C80 backend settlement: %v", err)
		}
		if !live {
			return
		}
		select {
		case <-settle.Done():
			t.Fatalf("original C80 backend did not settle: %v", settle.Err())
		case <-ticker.C:
		}
	}
}

func newC80EffectsJournal(t *testing.T, db c80EffectsDatabase, backend ledgerPredecessorBackend, recordID string) c80EffectsJournal {
	t.Helper()
	dir := t.TempDir()
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	binding := ledgerPredecessorEffectBinding{Version: 1, PhaseGeneration: 1, RecoveryID: "rid_c80readback", Application: db.identity, RealmDigest: c80EffectsHex("fixture realm"), Backend: backend}
	intent := ledgerPredecessorIntent{Version: 1, PhaseGeneration: 1, RecoveryID: binding.RecoveryID, Application: db.identity, RealmDigest: binding.RealmDigest, ActorUserID: c80EffectsActor, RecordID: recordID}
	return c80EffectsJournal{dir: dir, fd: fd, binding: binding, intent: intent, fence: &RecordLedgerFence{app: db.app, control: fd, request: RecordLedgerMaintenanceRequest{AppIdentity: db.identity}}}
}

func (journal c80EffectsJournal) publish(t *testing.T, kind string, value any) {
	t.Helper()
	if fresh, err := ledgerPublishExact(journal.fd, ledgerPredecessorName(kind, journal.intent.PhaseGeneration), value); err != nil || !fresh {
		t.Fatalf("publish %s: fresh=%t error=%v", kind, fresh, err)
	}
}

func (journal c80EffectsJournal) publishPreview(t *testing.T, digest string, actual c80EffectsReply) {
	t.Helper()
	journal.publish(t, "create-result", ledgerPredecessorCreateResult{ledgerPredecessorEffectBinding: journal.binding, CommandDigest: digest, ReservationID: actual.ReservationID, ExpiresAt: actual.ExpiresAt.UTC()})
}

func (journal c80EffectsJournal) read(t *testing.T, kind string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(journal.dir, ledgerPredecessorName(kind, journal.intent.PhaseGeneration)))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func (journal c80EffectsJournal) assertNoToken(t *testing.T, token string) {
	t.Helper()
	entries, err := os.ReadDir(journal.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(journal.dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte(token)) {
			t.Fatalf("raw token persisted in %s", entry.Name())
		}
	}
}

func assertC80EffectsPreview(t *testing.T, ctx context.Context, q ledgerAppQueryer, create ledgerC80CreateCommand, actual c80EffectsReply) ledgerC80Preview {
	t.Helper()
	preview, found, err := ledgerC80ReadPreview(ctx, q, create)
	if err != nil || !found || preview.ReservationID != actual.ReservationID || !preview.ExpiresAt.Equal(actual.ExpiresAt) {
		t.Fatalf("exact original preview readback: %+v found=%t error=%v", preview, found, err)
	}
	return preview
}

func c80EffectsReserve(create ledgerC80CreateCommand, digest string, request c80EffectsRequest, reservationID string) ledgerC80ReserveCommand {
	return ledgerC80ReserveCommand{
		CreateCommandDigest: digest, ReservationID: reservationID, DeploymentID: string(request.DeploymentID), ActorID: request.ActorID,
		DeletionContractVersion: 1, RequestFingerprint: create.RequestFingerprint, ExpectedBindingDigest: create.BindingDigest,
		ObservedWitnessSequence: request.ObservedWitness.Sequence, ObservedWitnessHash: hex.EncodeToString(request.ObservedWitness.EntryHash[:]),
		OwnerID: request.OwnerID, OwnerLeaseMicroseconds: uint64(request.OwnerLeaseDuration.Microseconds()), ReasonCode: "user_confirmed",
	}
}

func c80EffectsCommandDigest(t *testing.T, command any) string {
	t.Helper()
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(fields) // Go's JSON encoder orders map keys.
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func c80EffectsDigest(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}

func c80EffectsHex(value string) string { return hex.EncodeToString(c80EffectsDigest(value)) }

func c80EffectsWithTransaction(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx)) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	fn(tx)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func c80EffectsWithRollback(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx)) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	fn(tx)
}
