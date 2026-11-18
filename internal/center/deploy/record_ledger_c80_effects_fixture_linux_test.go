//go:build linux

package deploy

// This source is copied beside the existing request-file wrapper in an
// extracted C80 tree. It keeps one real runtime backend open so the preview and
// reserve records can be bound to the same observed PostgreSQL session. It
// calls the frozen repository directly; it is not a deletion Service fixture.
const c80EffectsFixtureSource = `package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
)

type c80EffectsBackend struct {
	PID int32 ` + "`json:\"pid\"`" + `
	Started time.Time ` + "`json:\"started\"`" + `
	Role string ` + "`json:\"role\"`" + `
}

type c80EffectsReply struct {
	Op string ` + "`json:\"op\"`" + `
	Backend c80EffectsBackend ` + "`json:\"backend\"`" + `
	ReservationID string ` + "`json:\"reservation_id\"`" + `
	ExpiresAt time.Time ` + "`json:\"expires_at\"`" + `
	Operation *recorddeletion.DeletionOperation ` + "`json:\"operation\"`" + `
}

func TestC80EffectsFixture(t *testing.T) {
	if os.Getenv("HOUFENG_C80_EFFECTS_CHILD") != "1" {
		t.Skip("private parent-driven C80 repository fixture")
	}
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("isolated PostgreSQL runner required")
	}
	input := os.NewFile(3, "fixture requests")
	output := os.NewFile(4, "fixture responses")
	if input == nil || output == nil { t.Fatal("missing fixture pipes") }
	defer input.Close()
	defer output.Close()
	decoder, encoder := json.NewDecoder(input), json.NewEncoder(output)
	var connect request
	if err := decoder.Decode(&connect); err != nil || connect.Action != "connect" { t.Fatal("invalid connect request") }
	config, err := pgxpool.ParseConfig(connect.DatabaseURL)
	if err != nil || !strings.HasPrefix(config.ConnConfig.Database, "houfeng_delivery_") || config.ConnConfig.User != connect.RuntimeRole {
		t.Fatal("invalid isolated runtime endpoint")
	}
	config.MaxConns = 1
	var connected atomic.Bool
	config.AfterConnect = func(context.Context, *pgx.Conn) error {
		if !connected.CompareAndSwap(false, true) { return errors.New("fixture backend reconnected") }
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil { t.Fatalf("open runtime: %v", err) }
	defer pool.Close()
	var backend c80EffectsBackend
	var current string
	if err := pool.QueryRow(ctx, "select pg_backend_pid(), backend_start, session_user::text, current_user::text from pg_catalog.pg_stat_activity where pid=pg_backend_pid()").Scan(&backend.PID, &backend.Started, &backend.Role, &current); err != nil || backend.PID <= 0 || backend.Role != connect.RuntimeRole || current != backend.Role {
		t.Fatalf("unexpected runtime backend: %v", err)
	}
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, pool); err != nil { t.Fatalf("admit frozen runtime: %v", err) }
	gate, err := store.NewDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID: "compose-center", DeploymentID: connect.DeploymentID, ProjectID: recordplatform.ProjectIDDefault,
		InstanceKind: "api", Capability: "records.runtime",
	})
	if err != nil { t.Fatalf("membership gate: %v", err) }
	repository := store.NewPostgresRecordDeletionRepository(pool, gate)
	if err := encoder.Encode(c80EffectsReply{Op: "ready", Backend: backend}); err != nil { t.Fatal(err) }
	var original request
	var preview recorddeletion.StoredPreview
	for {
		var next request
		if err := decoder.Decode(&next); err != nil { t.Fatalf("read fixture command: %v", err) }
		switch next.Action {
		case "preview":
			if original.Action != "" { t.Fatal("duplicate preview") }
			original = next
			fingerprint, err := recordplatform.FingerprintRequestV1(next.Fingerprint)
			if err != nil { t.Fatalf("fingerprint: %v", err) }
			original.Preview.RequestFingerprint = fingerprint
			preview, err = repository.CreatePreview(ctx, original.Preview)
			if err != nil { t.Fatalf("C80 CreatePreview: %v", err) }
			if err := encoder.Encode(c80EffectsReply{Op: "preview", Backend: backend, ReservationID: preview.ReservationID, ExpiresAt: preview.ExpiresAt}); err != nil { t.Fatal(err) }
		case "reserve":
			if original.Action != "preview" || preview.ReservationID == "" { t.Fatal("reserve without preview") }
			token, err := recordplatform.ParseDeletionRequestTokenTransportV1(original.TokenTransport)
			if err != nil || !token.MatchesCommitment(connect.DeploymentID, recordplatform.ProjectIDDefault, original.Preview.TokenCommitment) { t.Fatal("token commitment mismatch") }
			resolved, err := repository.ResolvePreview(ctx, recorddeletion.PreviewLookup{ReservationID: preview.ReservationID, Object: original.Preview.Object, Token: token})
			if err != nil { t.Fatalf("C80 ResolvePreview: %v", err) }
			operation, err := repository.ReservePreview(ctx, recorddeletion.ReservePreviewCommand{
				DeploymentID: connect.DeploymentID, ActorID: original.ActorID,
				DeletionContractVersion: recorddeletion.RecordDeletionContractVersionV1,
				Preview: resolved, Record: original.Preview.Record, ExpectedBindingDigest: original.Preview.BindingDigest,
				RequestFingerprint: original.Preview.RequestFingerprint, ObservedWitnessHead: original.ObservedWitness,
				OwnerID: original.OwnerID, OwnerLeaseDuration: original.OwnerLeaseDuration,
				ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
			})
			if err != nil { t.Fatalf("C80 ReservePreview: %v", err) }
			if err := encoder.Encode(c80EffectsReply{Op: "reserve", Backend: backend, ReservationID: preview.ReservationID, ExpiresAt: preview.ExpiresAt, Operation: &operation}); err != nil { t.Fatal(err) }
			original.Action = "reserved"
		case "quit":
			if err := encoder.Encode(c80EffectsReply{Op: "quit", Backend: backend}); err != nil { t.Fatal(err) }
			return
		default:
			t.Fatal("unknown fixture command")
		}
	}
}
`
