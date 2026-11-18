package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
)

// sourceRetirementLegacyCoverage uses one independently provisioned witness
// domain. Its W1 entries are deliberately undecodable, but satisfy the frozen
// W1 projection constraints; no W1 entry is promoted to a W2 terminal.
func sourceRetirementLegacyCoverage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	f := newSourceRetirementPGFixture(t, ctx)
	floor := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, nil, 1)

	type witnessState struct {
		prepares  int64
		terminals int64
		sequence  int64
		hash      string
	}
	state := func(t *testing.T) witnessState {
		t.Helper()
		var s witnessState
		err := f.witnessDB.QueryRow(ctx, `select
			(select count(*) from public.source_retirement_prepares),
			(select count(*) from public.source_retirement_terminals),
			sequence, encode(entry_hash, 'hex') from public.source_retirement_head where singleton`).
			Scan(&s.prepares, &s.terminals, &s.sequence, &s.hash)
		if err != nil {
			t.Fatalf("read W2 state: %v", err)
		}
		return s
	}
	assertBlocked := func(t *testing.T, operation string, err error) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
			t.Errorf("%s: expected legacy blocker SQLSTATE 55000, got %v", operation, err)
		}
	}

	// Cleanup removes only the exact test-owned entry. The trigger is disabled
	// and restored in one transaction, including on any failure via rollback.
	insertW1 := func(t *testing.T, operation, deployment, route, kind, id, entryType string) {
		t.Helper()
		var sequence int64
		canonical := []byte("not a decodable W1 entry: " + operation)
		err := f.witnessDB.QueryRow(ctx, `insert into public.deletion_witness_entries (
			sequence, entry_version, entry_type, deployment_id, project_id, operation_id,
			actor_id, route, object_kind, object_id, origin_identity, authorization_floor,
			authorization_floor_hash, deletion_request_token_commitment, request_fingerprint,
			deletion_contract_version, reason_code, release_epoch, confirmed_at,
			previous_hash, canonical_entry, entry_hash)
			values ((select coalesce(max(sequence), 0) + 1 from public.deletion_witness_entries),
			1, $1, $2, 'default', $3, 'legacy_test_actor', $4, $5, $6,
			case when $1 = 'delete_commit' and $5 <> 'record' then decode('01', 'hex') end,
			case when $1 = 'delete_commit' and $5 <> 'record' then decode('02', 'hex') end,
			case when $1 = 'delete_commit' and $5 <> 'record' then record_platform_internal.digest(decode('02', 'hex'), 'sha256') end,
			decode(repeat('11', 32), 'hex'), decode(repeat('22', 32), 'hex'),
			case when $1 = 'delete_commit' then 1::bigint end, 'source_removed',
			case when $1 = 'attempt_not_committed' then 1::bigint end, now(),
			decode(repeat('00', 32), 'hex'), $7::bytea, record_platform_internal.digest($7::bytea, 'sha256'))
			returning sequence`, entryType, deployment, operation, route, kind, id, canonical).Scan(&sequence)
		if err != nil {
			t.Fatalf("insert test-owned W1 evidence: %v", err)
		}
		t.Cleanup(func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tx, err := f.witnessDB.Begin(cleanup)
			if err != nil {
				t.Errorf("begin W1 cleanup: %v", err)
				return
			}
			defer func() { _ = tx.Rollback(cleanup) }()
			if _, err := tx.Exec(cleanup, `alter table public.deletion_witness_entries disable trigger user`); err != nil {
				t.Errorf("disable W1 trigger for cleanup: %v", err)
				return
			}
			tag, err := tx.Exec(cleanup, `delete from public.deletion_witness_entries where sequence=$1 and operation_id=$2`, sequence, operation)
			if err != nil || tag.RowsAffected() != 1 {
				t.Errorf("delete exact test-owned W1 entry: rows=%d error=%v", tag.RowsAffected(), err)
				return
			}
			if _, err := tx.Exec(cleanup, `alter table public.deletion_witness_entries enable trigger user`); err != nil {
				t.Errorf("restore W1 trigger: %v", err)
				return
			}
			if err := tx.Commit(cleanup); err != nil {
				t.Errorf("commit W1 cleanup: %v", err)
			}
		})
	}

	tests := []struct {
		name       string
		route      string
		kind       string
		id         string // "same" and "other" refer to the live VPS source.
		entryType  string
		otherDepot bool
		blocked    bool
	}{
		{"record_route_and_kind_excluded", "record_permanent_delete", "record", "same", "delete_commit", false, false},
		{"same_source_delete_commit", "source_permanent_delete", "vps", "same", "delete_commit", false, true},
		{"same_source_uncommitted_attempt", "source_permanent_delete", "vps", "same", "attempt_not_committed", false, true},
		{"unrelated_canonical_source", "source_permanent_delete", "vps", "other", "delete_commit", false, false},
		{"unrelated_canonical_kind", "source_permanent_delete", "target", "tg_0123456789abcdef", "attempt_not_committed", false, false},
		{"malformed_vps_id", "source_permanent_delete", "vps", "vps_123", "attempt_not_committed", false, true},
		{"nonhex_vps_id", "source_permanent_delete", "vps", "vps_gggggggggggggggg", "attempt_not_committed", false, true},
		{"wrong_kind_prefix", "source_permanent_delete", "target", "vps_0123456789abcdef", "attempt_not_committed", false, true},
		{"malformed_monitoring_id", "source_permanent_delete", "monitoring_instance", "mi_123", "attempt_not_committed", false, true},
		{"malformed_target_id", "source_permanent_delete", "target", "tg_123", "attempt_not_committed", false, true},
		{"source_route_record_kind", "source_permanent_delete", "record", "same", "attempt_not_committed", false, true},
		{"record_route_source_kind", "record_permanent_delete", "vps", "same", "attempt_not_committed", false, true},
		{"malformed_other_deployment", "source_permanent_delete", "vps", "vps_123", "attempt_not_committed", true, false},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, key := f.newVPS(t, ctx, "legacy coverage "+test.name)
			retirementID := fmt.Sprintf("legacy_pending_%d", i)
			preview, prepared := f.prepare(t, ctx, key, retirementID, floor)
			barrier, err := f.service.CommitBarrier(ctx, prepared)
			if err != nil {
				t.Fatalf("commit real APP barrier: %v", err)
			}
			id := test.id
			switch id {
			case "same":
				id = key.ID
			case "other":
				id = "vps_0000000000000000"
				if id == key.ID {
					id = "vps_0000000000000001"
				}
			}
			deployment := key.DeploymentID
			if test.otherDepot {
				deployment = "dp-" + string(bytes.Repeat([]byte{'0'}, 64))
				if deployment == key.DeploymentID {
					deployment = "dp-" + string(bytes.Repeat([]byte{'1'}, 64))
				}
			}
			insertW1(t, fmt.Sprintf("legacy_entry_%d", i), deployment, test.route, test.kind, id, test.entryType)
			before := state(t)
			observation, err := f.reader.Observe(ctx, key)
			if err != nil {
				t.Fatalf("restricted reader observe: %v", err)
			}
			if observation.LegacyBlocked != test.blocked || observation.Terminal != nil {
				t.Errorf("observe legacy blocked=%v terminal=%v; want blocked=%v and no terminal", observation.LegacyBlocked, observation.Terminal != nil, test.blocked)
			}

			_, prepareErr := f.producer.Prepare(ctx, key, fmt.Sprintf("legacy_new_%d", i), [32]byte{1})
			terminal, appendErr := f.producer.Append(ctx, retirementID, barrier, preview.LastLive, floor)
			if test.blocked {
				assertBlocked(t, "new prepare", prepareErr)
				assertBlocked(t, "append committed barrier", appendErr)
				if after := state(t); after != before {
					t.Errorf("blocked operations changed W2 state: before=%+v after=%+v", before, after)
				}
				return
			}
			if prepareErr != nil || appendErr != nil {
				t.Fatalf("unblocked prepare=%v append=%v", prepareErr, appendErr)
			}
			after := state(t)
			if after.prepares != before.prepares+1 || after.terminals != before.terminals+1 || after.sequence != before.sequence+1 || after.hash == before.hash {
				t.Errorf("unblocked operations did not advance W2 exactly once: before=%+v after=%+v", before, after)
			}
			replay, err := f.producer.Append(ctx, retirementID, barrier, preview.LastLive, floor)
			if err != nil || replay.Sequence != terminal.Sequence || replay.BarrierDigest != terminal.BarrierDigest {
				t.Errorf("exact terminal replay: terminal=%+v replay=%+v error=%v", terminal, replay, err)
			}
			if replayState := state(t); replayState != after {
				t.Errorf("exact replay changed W2 state: before=%+v after=%+v", after, replayState)
			}
		})
	}

	t.Run("existing_terminal_does_not_override_legacy", func(t *testing.T) {
		_, key := f.newVPS(t, ctx, "legacy after terminal")
		const retirementID = "legacy_existing_terminal"
		preview, prepared := f.prepare(t, ctx, key, retirementID, floor)
		barrier, err := f.service.CommitBarrier(ctx, prepared)
		if err != nil {
			t.Fatalf("commit real APP barrier: %v", err)
		}
		if err := f.service.Deliver(ctx, retirementID); err != nil {
			t.Fatalf("deliver real terminal: %v", err)
		}
		insertW1(t, "legacy_entry_existing_terminal", key.DeploymentID, "source_permanent_delete", "vps", "vps_123", "attempt_not_committed")
		before := state(t)
		observation, err := f.reader.Observe(ctx, key)
		if err != nil || observation.Terminal == nil || !observation.LegacyBlocked {
			t.Errorf("terminal must not override W1 blocker: terminal=%v legacy=%v error=%v", observation.Terminal != nil, observation.LegacyBlocked, err)
		}
		_, err = f.producer.Prepare(ctx, key, retirementID, prepared.prepare.Fingerprint)
		assertBlocked(t, "exact prepare replay", err)
		_, err = f.producer.Append(ctx, retirementID, barrier, preview.LastLive, floor)
		assertBlocked(t, "exact terminal replay", err)
		if after := state(t); after != before {
			t.Errorf("blocked replays changed W2 state: before=%+v after=%+v", before, after)
		}
	})
}
