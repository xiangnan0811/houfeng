package migrate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIntegrationLocalBlobNamespaceAuthority(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatal(err)
	}
	admin := fixture.openDirectRolePool(t, ctx, fixture.admin)
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker)
	encode := func(value map[string]any) []byte {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	const namespace = "a0000000-0000-4000-8000-000000000001"
	const incarnation = "a0000000-0000-4000-8000-000000000002"
	const request = "a0000000-0000-4000-8000-000000000003"
	provision := map[string]any{
		"namespace_id": namespace, "incarnation_id": incarnation, "request_id": request,
		"root_path": "/isolated/blob", "boot_id": "a0000000-0000-4000-8000-000000000004",
		"device": 1, "root_inode": 100, "digest_inode": 101, "locks_inode": 102, "marker_inode": 103,
		"owner_uid": 10001, "owner_gid": 10001, "marker_digest": strings.Repeat("ab", 32),
		"marker_nonce": "a0000000-0000-4000-8000-000000000005", "source_recovery_id": nil,
	}
	body := encode(provision)
	for name, pool := range map[string]*pgxpool.Pool{"runtime": runtime, "worker": worker} {
		if _, err := pool.Exec(ctx, `select public.local_blob_namespace_provision($1)`, body); err == nil {
			t.Fatalf("%s provisioned a namespace", name)
		}
	}
	var state string
	for range 2 {
		if err := admin.QueryRow(ctx, `select admission_state from public.local_blob_namespace_provision($1)`, body).Scan(&state); err != nil || state != "closed" {
			t.Fatalf("provision/replay state = %q, error = %v", state, err)
		}
	}
	provision["root_inode"] = 104
	if _, err := admin.Exec(ctx, `select public.local_blob_namespace_provision($1)`, encode(provision)); err == nil {
		t.Fatal("provision replay changed physical identity")
	}
	provision["root_inode"] = 100
	provision["namespace_id"] = "b0000000-0000-4000-8000-000000000001"
	provision["request_id"] = "b0000000-0000-4000-8000-000000000003"
	if _, err := admin.Exec(ctx, `select public.local_blob_namespace_provision($1)`, encode(provision)); err == nil {
		t.Fatal("same physical tree admitted under another namespace")
	}
	anchor := map[string]any{"namespace_id": namespace, "incarnation_id": incarnation,
		"key": "sha256/" + strings.Repeat("01", 32), "device": 1, "inode": 105,
		"nonce": "a0000000-0000-4000-8000-000000000006"}
	if _, err := runtime.Exec(ctx, `select public.local_blob_anchor_register($1)`, encode(anchor)); err == nil {
		t.Fatal("closed namespace accepted an anchor")
	}
	admit := map[string]any{"namespace_id": namespace, "incarnation_id": incarnation,
		"request_id": request, "marker_digest": strings.Repeat("ab", 32)}
	admit["incarnation_id"] = nil
	if _, err := admin.Exec(ctx, `select public.local_blob_namespace_admit($1)`, encode(admit)); err == nil {
		t.Fatal("null incarnation bypassed admission identity")
	}
	admit["incarnation_id"] = incarnation
	for range 2 {
		if err := admin.QueryRow(ctx, `select admission_state from public.local_blob_namespace_admit($1)`, encode(admit)).Scan(&state); err != nil || state != "active" {
			t.Fatalf("admission/replay state = %q, error = %v", state, err)
		}
	}
	for range 2 {
		var inode int64
		if err := runtime.QueryRow(ctx, `select lock_inode from public.local_blob_anchor_register($1)`, encode(anchor)).Scan(&inode); err != nil || inode != 105 {
			t.Fatalf("anchor/replay inode = %d, error = %v", inode, err)
		}
	}
	anchor["inode"] = 106
	if _, err := runtime.Exec(ctx, `select public.local_blob_anchor_register($1)`, encode(anchor)); err == nil {
		t.Fatal("anchor replay replaced its inode")
	}
	for _, statement := range []string{
		`update public.local_blob_namespaces set admission_state = 'closed', admitted_at = null`,
		`delete from public.local_blob_key_anchors`,
		`delete from public.record_write_obligations`,
		`update public.record_write_obligations set cleanup_outcome = 'already_absent'`,
	} {
		if _, err := runtime.Exec(ctx, statement); err == nil {
			t.Fatalf("runtime bypassed constrained command: %s", statement)
		}
	}
	if _, err := migrator.Exec(ctx, `update public.local_blob_key_anchors set lock_inode = 106`); err == nil {
		t.Fatal("registered anchor physical identity mutated")
	}
	t.Run("obligation_survives_origin_with_exact_dispatch_and_return", func(t *testing.T) {
		const job = "rij_localobligation"
		if _, err := migrator.Exec(ctx, `insert into public.users(user_id,username,password_hash,display_name,role)
			values('usr_localtest','local-obligation-actor','test-hash','Local obligation actor','admin')`); err != nil {
			t.Fatal(err)
		}
		if _, err := migrator.Exec(ctx, `insert into public.vps_assets(vps_id,display_name,lifecycle_status,usage_status)
			values('vps_0123456789abcdef','Local obligation destination','active','in_use')`); err != nil {
			t.Fatal(err)
		}
		if _, err := migrator.Exec(ctx, `insert into public.record_import_jobs
			(import_job_id, actor_id, idempotency_key, job_state, identity_classification, archive_digest, expires_at, destination_subject_kind, destination_subject_source_id)
			values ($1, 'usr_localtest', 'local-obligation', 'uploaded', 'unknown', decode($2, 'hex'), clock_timestamp() + interval '1 hour', 'vps', 'vps_0123456789abcdef')`, job, strings.Repeat("01", 32)); err != nil {
			t.Fatal(err)
		}
		const obligation = "rwo_01010101010101010101010101010101"
		const writer = "c0000000-0000-4000-8000-000000000001"
		const dispatch = "c0000000-0000-4000-8000-000000000002"
		registration := map[string]any{
			"obligation_id": obligation, "writer_instance": writer, "request_seq": 1,
			"project_id": "default", "origin_kind": "import_archive", "origin_id": job, "origin_generation": 1,
			"origin_owner_id": nil, "origin_owner_generation": nil, "origin_owner_expires_at": nil,
			"draft_id": nil, "plan_digest": nil, "archive_digest": strings.Repeat("01", 32),
			"namespace_id": namespace, "incarnation_id": incarnation, "backend_kind": "local",
			"key": anchor["key"], "sha256": strings.Repeat("01", 32), "size": 4,
			"publication_id": nil, "target_record_id": nil, "target_attachment_id": nil,
		}
		for range 2 {
			var temporary string
			var dispatched bool
			if err := runtime.QueryRow(ctx, `select temporary_key, dispatched_at is not null from public.record_write_register($1)`, encode(registration)).Scan(&temporary, &dispatched); err != nil || temporary != "sha256/.blob-tmp-"+obligation || dispatched {
				t.Fatalf("registration/replay temporary=%q dispatched=%t error=%v", temporary, dispatched, err)
			}
		}
		registration["obligation_id"] = "rwo_02020202020202020202020202020202"
		if _, err := runtime.Exec(ctx, `select public.record_write_register($1)`, encode(registration)); err == nil {
			t.Fatal("request sequence reused by another obligation")
		}
		registration["obligation_id"] = obligation
		dispatchBody := map[string]any{"obligation_id": obligation, "writer_instance": writer,
			"dispatch_id": dispatch, "anchor_inode": 105, "anchor_nonce": anchor["nonce"]}
		for range 2 {
			var actual string
			if err := runtime.QueryRow(ctx, `select dispatch_id::text from public.record_write_dispatch($1)`, encode(dispatchBody)).Scan(&actual); err != nil || actual != dispatch {
				t.Fatalf("dispatch/replay identity=%q error=%v", actual, err)
			}
		}
		dispatchBody["dispatch_id"] = "c0000000-0000-4000-8000-000000000003"
		if _, err := runtime.Exec(ctx, `select public.record_write_dispatch($1)`, encode(dispatchBody)); err == nil {
			t.Fatal("second physical dispatch accepted for one obligation")
		}
		if _, err := migrator.Exec(ctx, `delete from public.record_import_jobs where import_job_id = $1`, job); err != nil {
			t.Fatal(err)
		}
		returned := map[string]any{"obligation_id": obligation, "writer_instance": writer, "dispatch_id": dispatch,
			"outcome": "materialized", "version": "local-v2-test", "key": anchor["key"], "sha256": strings.Repeat("01", 32), "size": 4}
		for range 2 {
			var handedOff bool
			var version string
			if err := runtime.QueryRow(ctx, `select handed_off_at is not null, object_version from public.record_write_return($1)`, encode(returned)).Scan(&handedOff, &version); err != nil || !handedOff || version != "local-v2-test" {
				t.Fatalf("late return handoff=%t version=%q error=%v", handedOff, version, err)
			}
		}
		returned["version"] = "local-v2-conflict"
		if _, err := runtime.Exec(ctx, `select public.record_write_return($1)`, encode(returned)); err == nil {
			t.Fatal("late return changed exact materialization")
		}
		claim := map[string]any{
			"obligation_id": obligation, "namespace_id": namespace, "incarnation_id": incarnation,
			"anchor_inode": 105, "anchor_nonce": anchor["nonce"], "cleanup_owner_id": "cleanup_a",
			"cleanup_generation": 0, "cleanup_expires_at": nil,
			"cleanup_dispatch_id": "d0000000-0000-4000-8000-000000000001",
			"operation_id":        nil, "operation_owner_id": nil, "operation_generation": nil, "operation_expires_at": nil,
		}
		if _, err := runtime.Exec(ctx, `select public.local_blob_purge_claim($1)`, encode(claim)); err == nil {
			t.Fatal("runtime acquired worker purge authority")
		}
		if _, err := worker.Exec(ctx, `select public.local_blob_cleanup_claim($1)`, encode(claim)); err == nil {
			t.Fatal("worker acquired ordinary runtime cleanup authority")
		}
		var generation int64
		var expiry string
		if err := runtime.QueryRow(ctx, `select cleanup_generation, cleanup_expires_at::text from public.local_blob_cleanup_claim($1)`, encode(claim)).Scan(&generation, &expiry); err != nil || generation != 1 {
			t.Fatalf("root-independent cleanup generation=%d error=%v", generation, err)
		}
		claim["cleanup_owner_id"] = "cleanup_b"
		claim["cleanup_generation"] = generation
		claim["cleanup_expires_at"] = expiry
		if _, err := runtime.Exec(ctx, `select public.local_blob_cleanup_claim($1)`, encode(claim)); err == nil {
			t.Fatal("live cleanup claim was taken over")
		}
		// This is a SQL authority fixture. Filesystem absence and temp removal
		// are exercised by the managed Blob process tests, never fabricated here.
	})
}
