package migrate

// The successor retires the application's ability to manufacture deletion
// authority. Independent management verification and worker commands own writes.
func appendRecordDeletionLegacyAuthority(fragment *AppACLCurrentMigrationFragment) {
	for _, table := range []string{"record_deletion_verified_entries", "record_deletion_effect_scopes", "record_deletion_recovery_sessions", "record_deletion_owner_commit_checks"} {
		fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{
			ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedInternalSchemaR1, ObjectIdentity: table,
		})
	}
	for _, function := range []struct {
		identity string
		definer  bool
	}{
		{"record_deletion_effect_scope(target text, kinds text[])", true},
		{"record_deletion_command(body bytea, fields text[], numbers text[])", false},
		{"record_deletion_canonical(v jsonb)", false},
		{"record_core_surface_digest()", false},
		{"record_core_purge_effects(op text, target text, project text, verify_only boolean)", true},
		{"record_core_purge(bytea, bigint, boolean)", true},
		{"record_deletion_management_admit(deployment text, rid text, generation bigint)", true},
		{"record_deletion_v1_continue(body bytea, kind text)", true},
		{"record_deletion_verify_online_receipts_v1(op text, expected bytea)", true},
		{"record_deletion_verify_online_receipts(op text, expected bytea, version bigint)", true},
		{"record_deletion_verify_online_receipts_v2(op text, expected bytea)", true},
		{"record_deletion_responsibilities_v2(op text, project text, target text)", true},
		{"record_deletion_completed_obligation_v2(id text)", true},
		{"workspace_purge_receipt_valid(id text, surface bytea, receipt bytea, removed bigint, verified timestamp with time zone)", false},
		{"workspace_purge_manifest_valid(op text)", true},
		{"record_deletion_recovery_surface_digest(kind text)", false},
		{"record_deletion_recovery_id(prefix text, domain text, deployment text, project text, operation text, kind text)", false},
		{"record_deletion_owner_commit_trigger()", true},
		{"record_deletion_queue_owner_commit_check(op text, caller text, generation bigint, expires timestamp with time zone)", true},
		{"record_deletion_worker_admit_v2()", true},
		{"legacy_adapter_surface(kind text)", false},
		{"legacy_adapter_receipt_digest(kind text, op text, project text, target text, surface bytea, removed bigint)", false},
		{"legacy_adapter_lock(body bytea, kind text)", true},
		{"adapter_lock_v2(body bytea, kind text)", true},
		{"adapter_purge(body bytea, kind text, verify_only boolean, version bigint)", true},
		{"evidence_purge_prepare(body bytea, version bigint)", true},
		{"evidence_purge_payload(body bytea, version bigint)", true},
		{"legacy_evidence_metadata(target text)", true},
		{"legacy_adapter_purge(body bytea, kind text, verify_only boolean)", true},
		{"attachment_v1_time(t timestamp with time zone)", true},
		{"attachment_v1_deletion_id(op text, backend text, key text, version text)", true},
		{"attachment_v1_gc_digest(g blob_gc_deletions, result text)", true},
		{"attachment_v1_gc_document(g blob_gc_deletions)", true},
		{"attachment_v1_live(v jsonb)", true},
		{"attachment_v1_queue(v jsonb)", true},
		{"attachment_v1_projection(v jsonb)", true},
		{"record_attachment_purge_v1(body bytea, verify_only boolean)", true},
		{"record_attachment_gc_v1(body bytea, kind text)", true},
	} {
		fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{
			ObjectClass: AppACLObjectClassFunction, SchemaName: appACLManagedInternalSchemaR1, ObjectIdentity: function.identity,
		})
		fragment.Functions = append(fragment.Functions, AppACLCurrentFunctionContract{
			SchemaName: appACLManagedInternalSchemaR1, Identity: function.identity,
			Kind: "f", SecurityDefiner: function.definer, Config: []string{"search_path=pg_catalog"},
		})
	}
	for _, identity := range recordDeletionLegacyPublicCommands {
		fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{
			ObjectClass: AppACLObjectClassFunction, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: identity,
		})
		fragment.Functions = append(fragment.Functions, AppACLCurrentFunctionContract{
			SchemaName: appACLManagedPublicSchemaR1, Identity: identity,
			Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"},
		})
	}
	for _, table := range []string{"deletion_reservations", "deletion_fence_leases"} {
		for _, privilege := range []AppACLPrivilegeKind{AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete} {
			fragment.RevokedPrivileges = append(fragment.RevokedPrivileges, AppACLPrivilege{
				Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable,
				SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: table, Privilege: privilege,
			})
		}
	}
	for _, table := range []string{"record_purge_operations", "deletion_replay_state"} {
		for _, privilege := range []AppACLPrivilegeKind{AppACLPrivilegeInsert, AppACLPrivilegeUpdate} {
			fragment.RevokedPrivileges = append(fragment.RevokedPrivileges, AppACLPrivilege{
				Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable,
				SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: table, Privilege: privilege,
			})
		}
	}
	for _, table := range []string{"record_deletion_audits", "record_core_purge_receipts", "record_activity_purge_receipts", "record_search_purge_receipts", "evidence_purge_receipts"} {
		fragment.RevokedPrivileges = append(fragment.RevokedPrivileges, AppACLPrivilege{
			Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable,
			SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: table, Privilege: AppACLPrivilegeInsert,
		})
	}
	for _, identity := range []string{"record_activity_purge(bytea)", "record_search_purge(bytea)"} {
		fragment.RevokedPrivileges = append(fragment.RevokedPrivileges, AppACLPrivilege{
			Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: "public." + identity, Privilege: AppACLPrivilegeExecute,
		})
	}
	for _, privilege := range []AppACLPrivilegeKind{AppACLPrivilegeInsert, AppACLPrivilegeUpdate} {
		fragment.RevokedPrivileges = append(fragment.RevokedPrivileges, AppACLPrivilege{
			Subject: AppACLSubjectPlatformAdmin, ObjectClass: AppACLObjectClassTable,
			SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "deletion_replay_state", Privilege: privilege,
		})
	}
}

var recordDeletionLegacyPublicCommands = []string{
	"record_deletion_verify_legacy_entry(bytea)",
	"record_deletion_verified_binding(bytea)",
	"record_deletion_legacy_identity(bytea)",
	"record_deletion_recovery_begin(bytea)",
	"record_deletion_replay_v1(bytea)",
	"record_core_purge_v1(bytea)", "record_core_purge_verify_v1(bytea)",
	"record_core_purge_v2(bytea)", "record_core_purge_verify_v2(bytea)",
	"record_activity_purge_v1(bytea)", "record_activity_purge_verify_v1(bytea)",
	"record_search_purge_v1(bytea)", "record_search_purge_verify_v1(bytea)",
	"record_evidence_purge_prepare_v1(bytea)", "record_evidence_purge_payload_v1(bytea)",
	"record_evidence_purge_v1(bytea)", "record_evidence_purge_verify_v1(bytea)",
	"record_activity_purge_v2(bytea)", "record_activity_purge_verify_v2(bytea)",
	"record_search_purge_v2(bytea)", "record_search_purge_verify_v2(bytea)",
	"record_evidence_purge_prepare_v2(bytea)", "record_evidence_purge_payload_v2(bytea)",
	"record_evidence_purge_v2(bytea)", "record_evidence_purge_verify_v2(bytea)",
	"record_attachment_purge_prepare_v1(bytea)", "record_attachment_purge_verify_v1(bytea)",
	"record_attachment_gc_claim_v1(bytea)", "record_attachment_gc_complete_v1(bytea)",
	"record_attachment_gc_retry_v1(bytea)", "record_attachment_gc_resolve_v1(bytea)",
	"record_deletion_claim_v1(bytea)",
	"record_deletion_claim_v2(bytea)", "record_deletion_renew_v2(bytea)",
	"record_deletion_finish_v2(bytea)",
	"record_deletion_mark_delete_commit_unknown_v1(bytea)",
	"record_deletion_record_delete_entry_v1(bytea)",
	"record_deletion_mark_delete_witnessed_v1(bytea)",
	"record_deletion_mark_outcome_commit_unknown_v1(bytea)",
	"record_deletion_record_outcome_entry_v1(bytea)",
	"record_deletion_finalize_not_committed_v1(bytea)",
	"record_deletion_promote_permanent_fence_v1(bytea)",
	"record_deletion_mark_read_fenced_v1(bytea)",
	"record_deletion_begin_online_purge_v1(bytea)",
	"record_deletion_complete_online_purge_v1(bytea)",
	"record_deletion_mark_retry_required_v1(bytea)",
	"record_deletion_resume_retry_v1(bytea)",
}

func recordDeletionLegacyAuthorityPrivileges() []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, len(recordDeletionLegacyPublicCommands))
	for _, identity := range recordDeletionLegacyPublicCommands {
		subject := AppACLSubjectRecordDeletionWorker
		switch identity {
		case "record_deletion_verify_legacy_entry(bytea)", "record_deletion_legacy_identity(bytea)",
			"record_deletion_recovery_begin(bytea)", "record_deletion_replay_v1(bytea)":
			subject = AppACLSubjectPlatformAdmin
		}
		privileges = append(privileges, AppACLPrivilege{
			Subject: subject, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: "public." + identity, Privilege: AppACLPrivilegeExecute,
		})
	}
	return privileges
}
