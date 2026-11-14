package deletionledger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	ledgerMigrations "houfeng/db/deletionledger/migrations"
	witnessMigrations "houfeng/db/deletionwitness/migrations"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
)

const sourceObserverPrimaryStateSQL = `select
 (select count(*) from public.deletion_ledger_entries where operation_id = $1),
 (select count(*) from public.deletion_ledger_entries),
 (select sequence from public.deletion_ledger_head where singleton)`

const sourceObserverWitnessStateSQL = `select
 (select count(*) from public.deletion_witness_entries where operation_id = $1),
 (select count(*) from public.deletion_witness_entries),
 (select sequence from public.deletion_witness_head where singleton)`

type sourceObserverState struct {
	operationRows int64
	totalRows     int64
	headSequence  int64
}

func readSourceObserverState(ctx context.Context, admin *pgxpool.Pool, query, operation string) (sourceObserverState, error) {
	var state sourceObserverState
	err := admin.QueryRow(ctx, query, operation).Scan(&state.operationRows, &state.totalRows, &state.headSequence)
	return state, err
}

func assertSourceObserverState(t *testing.T, ctx context.Context, admin *pgxpool.Pool, query, operation string, want sourceObserverState) {
	t.Helper()
	got, err := readSourceObserverState(ctx, admin, query, operation)
	if err != nil || got != want {
		t.Fatalf("persisted source state = %+v, %v; want %+v", got, err, want)
	}
}

type sourceCommitProbe struct {
	admin           *pgxpool.Pool
	writerRole      string
	entriesRelation string
	stateSQL        string
	operation       string
	boundary        string
	rejectCommitted bool
	rejection       error
	events          []SourceCommitObservation
}

func (probe *sourceCommitProbe) observe(ctx context.Context, event SourceCommitObservation) error {
	if event.Boundary != probe.boundary {
		return fmt.Errorf("boundary = %q, want %q", event.Boundary, probe.boundary)
	}
	wantState := sourceObserverState{}
	if !event.Committed {
		if len(probe.events) != 0 {
			return errors.New("duplicate pre-commit observation")
		}
		// Independently identify the sole restricted writer holding the real
		// entries-table write lock, rather than looking up the reported PID.
		var pid int32
		var started time.Time
		var role string
		var matches int64
		err := probe.admin.QueryRow(ctx, `select activity.pid, activity.backend_start, activity.usename::text, count(*) over ()
 from pg_catalog.pg_stat_activity as activity
 join pg_catalog.pg_locks as held_lock on held_lock.pid = activity.pid
 where activity.datname = pg_catalog.current_database() and activity.usename = $1
   and activity.backend_xid is not null and held_lock.relation = pg_catalog.to_regclass($2)
   and held_lock.mode = 'RowExclusiveLock' and held_lock.granted`, probe.writerRole, probe.entriesRelation).Scan(&pid, &started, &role, &matches)
		if err != nil {
			return fmt.Errorf("find physical writer: %w", err)
		}
		if matches != 1 || event.BackendPID != pid || !event.BackendStarted.Equal(started) || event.BackendRole != role {
			return fmt.Errorf("observed backend = %d/%s/%s, physical writer = %d/%s/%s (%d matches)",
				event.BackendPID, event.BackendStarted, event.BackendRole, pid, started, role, matches)
		}
	} else {
		if len(probe.events) != 1 || probe.events[0].Committed ||
			event.BackendPID != probe.events[0].BackendPID ||
			!event.BackendStarted.Equal(probe.events[0].BackendStarted) ||
			event.BackendRole != probe.events[0].BackendRole {
			return errors.New("post-commit observation does not match the original writer")
		}
		wantState = sourceObserverState{operationRows: 1, totalRows: 1, headSequence: 1}
	}
	state, err := readSourceObserverState(ctx, probe.admin, probe.stateSQL, probe.operation)
	if err != nil {
		return fmt.Errorf("read independently visible source state: %w", err)
	}
	if state != wantState {
		return fmt.Errorf("source state at committed=%t is %+v, want %+v", event.Committed, state, wantState)
	}
	probe.events = append(probe.events, event)
	if event.Committed == probe.rejectCommitted {
		return probe.rejection
	}
	return nil
}

func TestPostgresIntegrationSourceCommitObserver(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required")
	}
	ctx := context.Background()
	primaryAdmin := sourceIsolatedPool(t, ctx, "HOUFENG_DELETION_LEDGER_DATABASE_URL", "w1")
	witnessAdmin := sourceIsolatedPool(t, ctx, "HOUFENG_DELETION_WITNESS_DATABASE_URL", "w2")
	if err := platformmigrate.Apply(ctx, primaryAdmin, ledgerMigrations.FS); err != nil {
		t.Fatal(err)
	}
	witnessSQL, err := witnessMigrations.FS.ReadFile("0001_create_full_witness.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := platformmigrate.Apply(ctx, witnessAdmin, fstest.MapFS{"0001_create_full_witness.sql": &fstest.MapFile{Data: witnessSQL}}); err != nil {
		t.Fatal(err)
	}
	primaryIdentity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, primaryAdmin, platformmigrate.DomainKindDeletionLedger)
	if err != nil {
		t.Fatal(err)
	}
	witnessIdentity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, witnessAdmin, platformmigrate.DomainKindDeletionWitness)
	if err != nil {
		t.Fatal(err)
	}
	primaryID, witnessID := ledgerIntegrationIdentity(primaryIdentity), ledgerIntegrationIdentity(witnessIdentity)
	primaryRead, primaryWrite := ledgerIntegrationRoles(t, ctx, primaryAdmin, "HOUFENG_DELETION_LEDGER_DATABASE_URL", false)
	witnessRead, witnessWrite := ledgerIntegrationRoles(t, ctx, witnessAdmin, "HOUFENG_DELETION_WITNESS_DATABASE_URL", true)
	primary, err := NewPrimary(primaryWrite, primaryID)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewPrimary(primaryRead, primaryID)
	if err != nil {
		t.Fatal(err)
	}
	witness, err := NewWitness(witnessWrite, primary, witnessID)
	if err != nil {
		t.Fatal(err)
	}
	witnessReader, err := NewWitness(witnessRead, reader, witnessID)
	if err != nil {
		t.Fatal(err)
	}
	request := sourceTestRequest(t, recordauth.SourceKindVPS, "vps_0123456789abcdef", "sro_commit_observer")
	fence := &sourceTestFence{phase: 1, request: cloneSourceRequest(request),
		trust: recorddeletion.LedgerVerificationTrust{Primary: primaryID, Witness: witnessID}}
	fence.trust.Continuity = fence
	maker, err := NewSourceRetirementCommitVerifier(fence)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := maker.Verify(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewSourceRetirementVerifier(reader, witnessReader, fence.trust)
	if err != nil {
		t.Fatal(err)
	}

	preRejected := errors.New("pre-commit observer rejected")
	postRejected := errors.New("post-commit observer acknowledgement lost")
	primaryProbe := func(rejectCommitted bool) *sourceCommitProbe {
		return &sourceCommitProbe{admin: primaryAdmin, writerRole: primaryWrite.Config().ConnConfig.User,
			entriesRelation: "public.deletion_ledger_entries", stateSQL: sourceObserverPrimaryStateSQL,
			operation: request.OperationID, boundary: "w1-commit", rejectCommitted: rejectCommitted,
			rejection: map[bool]error{false: preRejected, true: postRejected}[rejectCommitted]}
	}
	witnessProbe := func(rejectCommitted bool) *sourceCommitProbe {
		return &sourceCommitProbe{admin: witnessAdmin, writerRole: witnessWrite.Config().ConnConfig.User,
			entriesRelation: "public.deletion_witness_entries", stateSQL: sourceObserverWitnessStateSQL,
			operation: request.OperationID, boundary: "w2-commit", rejectCommitted: rejectCommitted,
			rejection: map[bool]error{false: preRejected, true: postRejected}[rejectCommitted]}
	}

	// The pre-commit callback sees the physical W1 writer, then rejects it.
	w1Before := primaryProbe(false)
	if _, err := primary.AppendSourceRetirement(WithSourceCommitObserver(ctx, w1Before.observe), commit); !errors.Is(err, preRejected) {
		t.Fatalf("W1 pre-commit rejection = %v", err)
	}
	if len(w1Before.events) != 1 || w1Before.events[0].Committed {
		t.Fatalf("W1 rejected observations = %+v", w1Before.events)
	}
	assertSourceObserverState(t, ctx, primaryAdmin, sourceObserverPrimaryStateSQL, request.OperationID, sourceObserverState{})
	resolved, err := reader.ResolveSourceRetirement(ctx, request)
	if err != nil || resolved.Committed {
		t.Fatalf("rejected W1 append resolved = %+v, %v", resolved, err)
	}

	// The post-commit callback fails only after a separate connection sees the
	// actual W1 row and head. Resolve the original operation after the error.
	w1After := primaryProbe(true)
	if _, err := primary.AppendSourceRetirement(WithSourceCommitObserver(ctx, w1After.observe), commit); !errors.Is(err, postRejected) {
		t.Fatalf("W1 post-commit rejection = %v", err)
	}
	if len(w1After.events) != 2 || w1After.events[0].Committed || !w1After.events[1].Committed {
		t.Fatalf("W1 committed observations = %+v", w1After.events)
	}
	oneRow := sourceObserverState{operationRows: 1, totalRows: 1, headSequence: 1}
	assertSourceObserverState(t, ctx, primaryAdmin, sourceObserverPrimaryStateSQL, request.OperationID, oneRow)
	resolved, err = reader.ResolveSourceRetirement(ctx, request)
	if err != nil || !resolved.Committed || resolved.Entry.Sequence != 1 || resolved.Entry.Request.OperationID != request.OperationID {
		t.Fatalf("committed original W1 operation = %+v, %v", resolved, err)
	}
	entry := resolved.Entry
	unexpected := 0
	replayCtx := WithSourceCommitObserver(ctx, func(context.Context, SourceCommitObservation) error {
		unexpected++
		return errors.New("unexpected observation on replay")
	})
	replayed, err := primary.AppendSourceRetirement(replayCtx, commit)
	if err != nil || unexpected != 0 || !equalSourceEntry(replayed, entry) {
		t.Fatalf("W1 exact replay = %+v, observations=%d, err=%v", replayed, unexpected, err)
	}
	assertSourceObserverState(t, ctx, primaryAdmin, sourceObserverPrimaryStateSQL, request.OperationID, oneRow)

	locator := recorddeletion.LedgerLocator{LedgerDomain: entry.LedgerDomain, Sequence: entry.Sequence, EntryHash: entry.EntryHash}
	w2Before := witnessProbe(false)
	if _, err := witness.ConfirmSourceRetirement(WithSourceCommitObserver(ctx, w2Before.observe), commit, entry); !errors.Is(err, preRejected) {
		t.Fatalf("W2 pre-commit rejection = %v", err)
	}
	if len(w2Before.events) != 1 || w2Before.events[0].Committed {
		t.Fatalf("W2 rejected observations = %+v", w2Before.events)
	}
	assertSourceObserverState(t, ctx, witnessAdmin, sourceObserverWitnessStateSQL, request.OperationID, sourceObserverState{})
	if _, err := verifier.Verify(ctx, locator); !errors.Is(err, recorddeletion.ErrLedgerPending) {
		t.Fatalf("rejected W2 prefix verification = %v", err)
	}

	w2After := witnessProbe(true)
	if _, err := witness.ConfirmSourceRetirement(WithSourceCommitObserver(ctx, w2After.observe), commit, entry); !errors.Is(err, postRejected) {
		t.Fatalf("W2 post-commit rejection = %v", err)
	}
	if len(w2After.events) != 2 || w2After.events[0].Committed || !w2After.events[1].Committed {
		t.Fatalf("W2 committed observations = %+v", w2After.events)
	}
	assertSourceObserverState(t, ctx, witnessAdmin, sourceObserverWitnessStateSQL, request.OperationID, oneRow)
	witnessed, err := witnessReader.ReadSourceRetirements(ctx)
	if err != nil || len(witnessed) != 1 || !equalSourceEntry(witnessed[0], entry) {
		t.Fatalf("committed original W2 prefix = %+v, %v", witnessed, err)
	}
	verified, err := verifier.Verify(ctx, locator)
	if err != nil || !verified.Valid() || !equalSourceEntry(verified.Entry(), entry) {
		t.Fatalf("independent verification of original prefix = %+v, %v", verified, err)
	}
	unexpected = 0
	verified, err = witness.ConfirmSourceRetirement(replayCtx, commit, entry)
	if err != nil || unexpected != 0 || !verified.Valid() || !equalSourceEntry(verified.Entry(), entry) {
		t.Fatalf("W2 exact replay = %+v, observations=%d, err=%v", verified, unexpected, err)
	}
	assertSourceObserverState(t, ctx, witnessAdmin, sourceObserverWitnessStateSQL, request.OperationID, oneRow)
}
