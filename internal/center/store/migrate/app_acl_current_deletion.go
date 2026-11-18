package migrate

import "fmt"

const recordDeletionSuccessorMigration = "0081_record_deletion_successor.sql"

var appACLCurrentTransitionBindingsV2 = []AppACLRoleBinding{
	{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_runtime"},
	{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	{Subject: AppACLSubjectRecordDeletionWorker, CatalogRole: "houfeng_record_deletion_worker"},
}

// A historical source is deliberately projected onto V1. The successor never
// supplies a default worker role: its binding must be explicit in the caller.
func splitAppACLCurrentRoleBindings(bindings []AppACLRoleBinding) ([]AppACLRoleBinding, *AppACLRoleBinding, error) {
	canonical, err := canonicalAppACLRoleBindings(bindings)
	if err != nil {
		return nil, nil, err
	}
	historical := make([]AppACLRoleBinding, 0, 2)
	var worker *AppACLRoleBinding
	for _, binding := range canonical {
		switch binding.Subject {
		case AppACLSubjectCenterRuntime, AppACLSubjectPlatformAdmin:
			historical = append(historical, binding)
		case AppACLSubjectRecordDeletionWorker:
			worker = &binding
		default:
			return nil, nil, fmt.Errorf("unsupported current APP ACL subject %q", binding.Subject)
		}
	}
	return historical, worker, nil
}

func appACLCurrentSourceRequiresDeletionWorker(source appACLCurrentSourceContract) bool {
	for _, fragment := range source.fragments {
		if fragment.Migration == recordDeletionSuccessorMigration {
			return true
		}
	}
	return indexOfAppACLCurrentMigration(source.sources.names, recordDeletionSuccessorMigration) >= 0
}

func recordDeletionSuccessorAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	fragment := AppACLCurrentMigrationFragment{
		Migration:  recordDeletionSuccessorMigration,
		Privileges: recordDeletionSuccessorAppACLCurrentPrivileges,
		RevokedPrivileges: []AppACLPrivilege{
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "records", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "records", Privilege: AppACLPrivilegeDelete},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_outbox", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_outbox", Privilege: AppACLPrivilegeUpdate},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_outbox", Privilege: AppACLPrivilegeDelete},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "blob_objects", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_export_jobs", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_export_jobs", Privilege: AppACLPrivilegeUpdate},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_export_artifacts", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_export_artifacts", Privilege: AppACLPrivilegeUpdate},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_import_artifacts", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "blob_publication_intents", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "attachment_upload_parts", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "blob_gc_pins", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_attachments", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_revision_attachments", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_import_entity_mappings", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "evidence_capture_intents", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "evidence_payloads", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_notification_deliveries", Privilege: AppACLPrivilegeUpdate},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_notification_delivery_attempts", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "record_collaboration_purge_receipts", Privilege: AppACLPrivilegeInsert},
			{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassFunction, ObjectIdentity: "public.record_collaboration_purge(bytea)", Privilege: AppACLPrivilegeExecute},
		},
		AuxiliaryPrivileges: []AppACLCurrentAuxiliaryPrivilege{{
			CatalogRole:    recordsAuthorityCatalogRole,
			ObjectClass:    AppACLObjectClassFunction,
			ObjectIdentity: "public.source_maintenance_status()",
			Privilege:      AppACLPrivilegeExecute,
		}, {
			CatalogRole:    recordsAuthorityCatalogRole,
			ObjectClass:    AppACLObjectClassFunction,
			ObjectIdentity: "public.record_birth_launch_inspect(bigint, bytea)",
			Privilege:      AppACLPrivilegeExecute,
		}, {
			CatalogRole:    recordsAuthorityCatalogRole,
			ObjectClass:    AppACLObjectClassFunction,
			ObjectIdentity: "public.record_platform_compose_membership_heartbeat_v2(bytea)",
			Privilege:      AppACLPrivilegeExecute,
		}},
	}
	for _, table := range []string{
		"record_deletion_gates", "record_deletion_previews",
		"record_markdown_client_purge_receipts", "record_comparison_purge_receipts",
		"comparison_result_dependencies", "comparison_result_dependency_index_completions",
		"record_notification_delivery_starts",
		"record_notification_delivery_facts", "record_notification_delivery_completions",
		"record_notification_delivery_handoffs",
		"browser_record_cleanup_targets", "browser_record_cleanup_events", "browser_record_cleanup_receipts",
		"markdown_client_draft_buffers", "markdown_client_workspaces",
		"comparison_client_workbenches", "comparison_client_save_attempts",
		"source_authority_admission", "source_application_principals",
		"source_retirement_barriers", "source_retirement_outbox",
		"source_retirement_pins", "source_retirement_coverage_unknown",
		"source_legacy_retirement_bindings",
		"local_blob_namespaces", "local_blob_namespace_incarnations", "local_blob_key_anchors",
		"record_write_obligations", "record_write_obligation_targets",
		"local_blob_gc_obligations",
		"attachment_purge_preparations", "attachment_purge_targets", "attachment_purge_workspace_targets",
		"record_portability_cleanup_preparations", "record_portability_cleanup_targets",
		"record_portability_v1_preparations", "record_portability_v1_targets", "record_portability_v1_artifacts", "record_portability_v1_obligations",
	} {
		fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{
			ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: table,
		})
	}
	for _, table := range []string{
		"record_birth_lineage", "record_birth_keys", "record_birth_bindings", "record_birth_enabled", "record_birth_allocations",
		"record_birth_checkpoint_head", "record_birth_checkpoints",
		"record_birth_launch_state",
		"compose_membership_transition",
		"record_external_delivery_commit_checks",
		"record_portability_v1_epoch",
	} {
		fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{
			ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedInternalSchemaR1, ObjectIdentity: table,
		})
	}
	fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{
		ObjectClass: AppACLObjectClassSequence, SchemaName: appACLManagedPublicSchemaR1,
		ObjectIdentity: "browser_record_cleanup_events_cursor_seq",
	})
	for _, identity := range []string{
		"enforce_record_purge_operation_contract_version()",
		"enforce_record_deletion_gate()", "enforce_record_deletion_preview()",
		"record_text_set_is_canonical(text[])", "enforce_browser_record_cleanup_immutable()",
	} {
		schema := appACLManagedInternalSchemaR1
		if identity == "record_text_set_is_canonical(text[])" {
			schema = appACLManagedPublicSchemaR1
		}
		fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{
			ObjectClass: AppACLObjectClassFunction, SchemaName: schema, ObjectIdentity: identity,
		})
		fragment.Functions = append(fragment.Functions, AppACLCurrentFunctionContract{
			SchemaName: schema, Identity: identity,
			Kind: "f", Config: []string{"search_path=pg_catalog"},
		})
	}
	for _, function := range []struct {
		schema   string
		identity string
		definer  bool
	}{
		{appACLManagedPublicSchemaR1, "record_platform_compose_inventory_upgrade(bytea, bytea)", true},
		{appACLManagedPublicSchemaR1, "record_platform_compose_membership_heartbeat_v2(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_processor_worker_admit(text, text, text, text)", true},
		{appACLManagedInternalSchemaR1, "source_legacy_retirement_command(body bytea)", false},
		{appACLManagedInternalSchemaR1, "source_legacy_retirement_binding(body bytea, install boolean)", true},
		{appACLManagedPublicSchemaR1, "source_legacy_retirement_install(bytea)", true},
		{appACLManagedPublicSchemaR1, "source_legacy_retirement_verify(bytea)", true},
		{appACLManagedPublicSchemaR1, "source_legacy_retirement_blocked(text, text)", true},
		{appACLManagedInternalSchemaR1, "record_birth_command(bytea, text[], text[])", false},
		{appACLManagedInternalSchemaR1, "record_birth_validate_request(jsonb)", false},
		{appACLManagedInternalSchemaR1, "record_birth_admit()", true},
		{appACLManagedInternalSchemaR1, "record_birth_binding()", true},
		{appACLManagedInternalSchemaR1, "record_birth_installed_binding()", true},
		{appACLManagedInternalSchemaR1, "record_birth_launch_validate(raw bytea, kind text, source source_authority_admission)", true},
		{appACLManagedInternalSchemaR1, "record_birth_launch_stage(center_body bytea, processor_body bytea, runtime_role name, worker_role name, expected_revision bigint)", true},
		{appACLManagedInternalSchemaR1, "record_birth_launch_close(expected_revision bigint)", true},
		{appACLManagedInternalSchemaR1, "record_birth_launch_open(expected_revision bigint)", true},
		{appACLManagedInternalSchemaR1, "record_birth_launch_admit(source source_authority_admission)", true},
		{appACLManagedPublicSchemaR1, "record_birth_launch_inspect(bigint, bytea)", true},
		{appACLManagedInternalSchemaR1, "record_birth_continuity_validate(raw bytea)", true},
		{appACLManagedInternalSchemaR1, "record_birth_continuity_check(raw bytea, exclusive_head boolean)", true},
		{appACLManagedInternalSchemaR1, "record_birth_continuity_guard()", true},
		{appACLManagedInternalSchemaR1, "record_birth_binding_bytes(b record_platform_internal.record_birth_bindings)", false},
		{appACLManagedInternalSchemaR1, "record_birth_checkpoint_bytes(c record_platform_internal.record_birth_checkpoints)", true},
		{appACLManagedInternalSchemaR1, "record_birth_checkpoint_append(a record_platform_internal.record_birth_allocations)", true},
		{appACLManagedInternalSchemaR1, "record_birth_project(a record_platform_internal.record_birth_allocations)", true},
		{appACLManagedInternalSchemaR1, "record_birth_request_row(v jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_birth_initial_commitment(a record_platform_internal.record_birth_allocations)", false},
		{appACLManagedInternalSchemaR1, "record_birth_receipt_check(v jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_birth_actual_first(a record_platform_internal.record_birth_allocations)", true},
		{appACLManagedInternalSchemaR1, "record_birth_events_valid(a record_platform_internal.record_birth_allocations)", true},
		{appACLManagedInternalSchemaR1, "record_birth_writer_guard(target text)", true},
		{appACLManagedInternalSchemaR1, "evidence_current_actor(text, text)", true},
		{appACLManagedInternalSchemaR1, "evidence_live_source(text, text)", true},
		{appACLManagedInternalSchemaR1, "evidence_target_guard(text, text, text, text)", true},
		{appACLManagedPublicSchemaR1, "record_evidence_intent_insert(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_evidence_payload_insert(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_comparison_index_write(bytea, bytea)", true},
		{appACLManagedInternalSchemaR1, "record_export_job_write(bytea, text)", true},
		{appACLManagedPublicSchemaR1, "record_export_job_claim(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_export_job_advance(bytea)", true},
		{appACLManagedInternalSchemaR1, "comparison_input_admit()", true},
		{appACLManagedInternalSchemaR1, "record_draft_launch_guard()", true},
		{appACLManagedInternalSchemaR1, "comparison_v1_cleanup_guard(target text)", true},
		{appACLManagedInternalSchemaR1, "comparison_evidence_row_guard()", true},
		{appACLManagedInternalSchemaR1, "comparison_root_epoch_guard()", true},
		{appACLManagedInternalSchemaR1, "record_birth_allocation_guard()", true},
		{appACLManagedInternalSchemaR1, "record_birth_root_insert_guard()", false},
		{appACLManagedInternalSchemaR1, "record_birth_root_identity_guard()", false},
		{appACLManagedInternalSchemaR1, "record_birth_root_tombstone()", true},
		{appACLManagedInternalSchemaR1, "record_birth_root_complete()", true},
		{appACLManagedPublicSchemaR1, "record_birth_allocation_find(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_allocation_get(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_publication_bind(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_publication_lookup(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_current_binding()", true},
		{appACLManagedPublicSchemaR1, "record_birth_checkpoint_proof(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_continuity_binding()", true},
		{appACLManagedPublicSchemaR1, "record_birth_continuity_assert(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_allocation_begin(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_receipt_register(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_prepare_create(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_committed(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_certification(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_consume(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_birth_finish(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_deletion_gate_cleanup_event()", true},
		{appACLManagedInternalSchemaR1, "record_deletion_issuer_admit()", true},
		{appACLManagedInternalSchemaR1, "record_comparison_writer_fence()", true},
		{appACLManagedPublicSchemaR1, "record_mutation_rows_lock(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_outbox_route_immutable()", false},
		{appACLManagedInternalSchemaR1, "record_outbox_transition(bytea, text)", true},
		{appACLManagedPublicSchemaR1, "record_outbox_enqueue(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_outbox_claim(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_outbox_retry(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_outbox_sent(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_outbox_cancel(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_outbox_cleanup(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_delivery_retained_immutable()", true},
		{appACLManagedInternalSchemaR1, "record_external_delivery_command(bytea, text)", false},
		{appACLManagedInternalSchemaR1, "record_external_delivery_correlation(jsonb, bigint, timestamp with time zone)", false},
		{appACLManagedInternalSchemaR1, "record_external_delivery_fact_digest(bytea, text, text, text)", false},
		{appACLManagedInternalSchemaR1, "record_external_delivery_scope(jsonb, boolean)", true},
		{appACLManagedInternalSchemaR1, "record_external_delivery_lock_claim(jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_external_delivery_begin(bytea, boolean)", true},
		{appACLManagedInternalSchemaR1, "record_external_delivery_finish(bytea, boolean)", true},
		{appACLManagedInternalSchemaR1, "record_outbox_observed_route(record_outbox)", true},
		{appACLManagedInternalSchemaR1, "record_outbox_handoff_complete(text, text)", true},
		{appACLManagedInternalSchemaR1, "record_outbox_drain(bytea, bigint)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_target_guard()", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_epoch_share()", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_touch_retained(op text, target text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_carried(p_record text, obligation text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_carried_successor(prior_op text, prior_target text, next_op text, next_target text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_frame_digest(p_op text, p_project text, p_record text, p_surface bytea, p_domain bytea, p_sequence bigint, p_entry_hash bytea, p_witness bytea, p_owners jsonb, p_removed bigint, p_pdf boolean, p_artifacts jsonb, p_obligations jsonb, p_targets jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_inventory_digest(op text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_source_links(op text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_s3_source_valid(snapshot jsonb, obligation text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_local_source_expected(snapshot jsonb, obligation text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_prepare_command(body bytea)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_owners(record_id text, export_jobs text[], import_jobs text[])", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_sources(p_record text, p_export_jobs text[], p_import_jobs text[])", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_retained_candidates(t record_portability_v1_targets, p_record text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_candidate_match(source_table text, source_row jsonb, t record_portability_v1_targets)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_ambiguous_references(t record_portability_v1_targets, p_record text, p_removable_jobs text[], p_obligations text[], evidence jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_removable_jobs(p_record text, p_import_jobs text[])", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_removed_count(p_record text, p_export_jobs text[], p_removable_jobs text[])", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_inventory_candidate(v jsonb, p_export_jobs text[], p_import_jobs text[], p_removable_jobs text[], pdf boolean)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_eligible(op text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_target_id(op text, backend text, key text, version text, namespace uuid, incarnation uuid, binding bytea)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_owner_digest(table_name text, pk jsonb, physical jsonb, route jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_local_owner_version(o record_write_obligations)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_artifact_owner_digest(op text, kind text, artifact text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_live_artifact_digest(op text, target text, kind text, artifact text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_artifact_owner_survives(op text, kind text, artifact text, digest bytea)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_upload_part_survives(op text, target text, owner_key text, digest bytea)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_attachment_survives(op text, target text, owner_key text, digest bytea)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_blob_object_survives(op text, target text, owner_key text, digest bytea, excluded_kind text, excluded_id text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_lock_parents(source_table text, before_row jsonb, after_row jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_reference_identity(source_table text, source_row jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_reference_owner(source_table text, source_row jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_reference_match(source_table text, source_row jsonb, target_row record_portability_v1_targets)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_scope_member(source_table text, source_row jsonb, target_row record_portability_v1_targets)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_obligation_owner_digest(op text, obligation text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_local_expected_version(op text, target text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_live_obligation_digest(op text, target text, obligation text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_obligation_owner_survives(op text, obligation text, digest bytea)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_local_successor(op text, target text, task text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_local_task_proof(g blob_gc_deletions)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_retained_proof(op text, target text, excluded_kind text, excluded_id text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_finished(op text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_finish_v1(bytea, boolean)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_old_owner_valid(op text, target text, kind text, owner_id text, expected bytea)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_append_fact_only(before_row jsonb, after_row jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_reference_guard()", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_reference_check()", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_link_guard()", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_control_check()", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_parent_route(source_table text, source_row jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_parent_guard()", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_parent_check()", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_gc_prepare_barrier(v jsonb, purge boolean, legacy bytea)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_gc_barrier(backend text, namespace uuid, incarnation uuid, key text, version text, registered_deletion_id text)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_gc_barrier_impl(backend text, namespace uuid, incarnation uuid, key text, version text, registered_deletion_id text, bound_route bytea)", true},
		{appACLManagedInternalSchemaR1, "record_portability_v1_gc_transition_guard()", true},
		{appACLManagedInternalSchemaR1, "record_external_delivery_insert_guard()", true},
		{appACLManagedInternalSchemaR1, "record_external_delivery_commit_trigger()", true},
		{appACLManagedInternalSchemaR1, "record_external_delivery_queue_commit_check(jsonb, text, bigint, timestamp with time zone)", true},
		{appACLManagedPublicSchemaR1, "record_external_delivery_start(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_external_delivery_cancel(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_external_delivery_reconcile(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_external_delivery_outcome(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_external_delivery_finalize(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_outbox_drain_v2(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_collaboration_delivery_inventory(text, bigint)", true},
		{appACLManagedPublicSchemaR1, "record_collaboration_start_inventory(text, bigint)", true},
		{appACLManagedPublicSchemaR1, "record_collaboration_purge_v2(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_collaboration_purge_verify_v2(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_deletion_reservation_gate_guard()", true},
		{appACLManagedInternalSchemaR1, "record_deletion_issuer_command(bytea, text[])", false},
		{appACLManagedInternalSchemaR1, "record_deletion_issuer_snapshot(text)", true},
		{appACLManagedPublicSchemaR1, "record_deletion_preview_create(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_deletion_preview_lock(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_deletion_issue(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_deletion_inventory_lock(bytea)", true},
		{appACLManagedInternalSchemaR1, "enforce_client_purge_receipt_immutable()", false},
		{appACLManagedInternalSchemaR1, "client_purge_field(bytea)", false},
		{appACLManagedInternalSchemaR1, "client_metadata_purge(bytea, text, boolean, bigint)", true},
		{appACLManagedPublicSchemaR1, "record_markdown_client_purge(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_markdown_client_purge_verify(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_comparison_purge(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_comparison_purge_verify(bytea)", true},
		{appACLManagedInternalSchemaR1, "reject_source_retirement_mutation()", true},
		{appACLManagedInternalSchemaR1, "source_prepare_set_is_canonical(text[])", false},
		{appACLManagedInternalSchemaR1, "source_retirement_field(bytea)", false},
		{appACLManagedInternalSchemaR1, "source_retirement_revision(text, text)", true},
		{appACLManagedPublicSchemaR1, "source_application_admit()", true},
		{appACLManagedPublicSchemaR1, "source_maintenance_status()", true},
		{appACLManagedInternalSchemaR1, "source_membership_effect_guard()", true},
		{appACLManagedPublicSchemaR1, "source_retirement_preview(text, text, text)", true},
		{appACLManagedPublicSchemaR1, "source_retirement_commit(bigint, text, text, text, text, text, bytea, bytea, bytea, bytea)", true},
		{appACLManagedPublicSchemaR1, "source_retirement_install_pin(bigint, text, bigint, bytea, bytea, bytea, bigint)", true},
		{appACLManagedInternalSchemaR1, "local_blob_immutable_identity()", false},
		{appACLManagedInternalSchemaR1, "record_write_obligation_identity_guard()", false},
		{appACLManagedInternalSchemaR1, "blob_gc_operation_identity_guard()", false},
		{appACLManagedInternalSchemaR1, "record_attachment_publication_guard()", false},
		{appACLManagedInternalSchemaR1, "local_blob_direct_login()", true},
		{appACLManagedInternalSchemaR1, "local_blob_command(bytea, text[])", false},
		{appACLManagedInternalSchemaR1, "local_blob_admission(uuid, uuid)", true},
		{appACLManagedInternalSchemaR1, "local_blob_adapter_purge_authority(bytea)", true},
		{appACLManagedInternalSchemaR1, "local_attachment_purge_finish(bytea, boolean)", true},
		{appACLManagedPublicSchemaR1, "local_attachment_purge_prepare(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_attachment_purge_complete(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_attachment_purge_verify(bytea)", true},
		{appACLManagedInternalSchemaR1, "local_portability_purge_finish(bytea, boolean)", true},
		{appACLManagedPublicSchemaR1, "local_portability_purge_prepare(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_portability_purge_complete(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_portability_purge_verify(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_recovery_inventory()", true},
		{appACLManagedPublicSchemaR1, "record_draft_upload_parts_handoff(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_namespace_provision(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_namespace_admit(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_anchor_register(bytea)", true},
		{appACLManagedInternalSchemaR1, "local_blob_record_epoch(text)", true},
		{appACLManagedInternalSchemaR1, "record_blob_strong_fence()", true},
		{appACLManagedInternalSchemaR1, "record_write_origin(jsonb)", true},
		{appACLManagedInternalSchemaR1, "local_blob_write_fence(text, uuid, text, text)", true},
		{appACLManagedPublicSchemaR1, "record_write_register(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_write_dispatch(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_write_resolve(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_write_temporary_created(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_write_return(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_blob_publication_bind(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_blob_publication_prepare(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_upload_part_publish(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_processor_publication_command(bytea)", false},
		{appACLManagedInternalSchemaR1, "record_processor_publication_lock(jsonb)", true},
		{appACLManagedInternalSchemaR1, "record_blob_metadata_publish(text, bytea, text, bigint, text, uuid)", true},
		{appACLManagedPublicSchemaR1, "record_processor_publication_lock(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_processor_publication_finish(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_blob_reference_fence(text, text)", true},
		{appACLManagedPublicSchemaR1, "record_blob_pin_create(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_blob_pin_release(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_attachment_copy_source(jsonb)", true},
		{appACLManagedPublicSchemaR1, "record_attachment_copy_source(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_attachment_copy_publish(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_attachment_draft_lock(jsonb)", true},
		{appACLManagedPublicSchemaR1, "record_attachment_draft_lock(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_attachment_reserve(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_attachment_state_set(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_revision_namespace_lock(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_revision_blob_fence()", true},
		{appACLManagedPublicSchemaR1, "record_import_attachment_publish(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_revision_attachment_publish(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_import_plan_lock(p_job text, p_namespace_ids text)", true},
		{appACLManagedPublicSchemaR1, "record_birth_allocation_lock(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_import_plan_lock(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_import_mapping_insert(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_artifact_publication_origin(jsonb, text)", true},
		{appACLManagedPublicSchemaR1, "record_export_artifact_publish(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_import_artifact_publish(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_export_artifact_revoke(bytea)", true},
		{appACLManagedInternalSchemaR1, "local_blob_cleanup_operation(text, text, bigint, timestamp with time zone, text)", true},
		{appACLManagedInternalSchemaR1, "local_blob_origin_closed(text)", true},
		{appACLManagedInternalSchemaR1, "local_blob_key_referenced(uuid, text)", true},
		{appACLManagedInternalSchemaR1, "local_blob_cleanup_claim(bytea, boolean)", true},
		{appACLManagedInternalSchemaR1, "local_blob_cleanup_complete(bytea, boolean)", true},
		{appACLManagedPublicSchemaR1, "local_blob_cleanup_claim(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_cleanup_complete(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_purge_claim(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_purge_complete(bytea)", true},
		{appACLManagedInternalSchemaR1, "record_write_origin_handoff()", true},
		{appACLManagedInternalSchemaR1, "record_write_import_target_register()", true},
		{appACLManagedPublicSchemaR1, "record_write_bind_record(bytea)", true},
		{appACLManagedInternalSchemaR1, "local_blob_gc_authority(jsonb, boolean, bytea, boolean)", true},
		{appACLManagedInternalSchemaR1, "local_blob_gc_prepare(bytea, boolean, bytea)", true},
		{appACLManagedInternalSchemaR1, "local_blob_gc_snapshot(text)", true},
		{appACLManagedInternalSchemaR1, "local_blob_gc_claim(bytea, boolean, bytea)", true},
		{appACLManagedInternalSchemaR1, "local_blob_gc_current(jsonb, boolean, bytea)", true},
		{appACLManagedInternalSchemaR1, "local_blob_gc_resolve(bytea, boolean, bytea)", true},
		{appACLManagedInternalSchemaR1, "local_blob_gc_bind_final(bytea, boolean, bytea)", true},
		{appACLManagedInternalSchemaR1, "local_blob_gc_complete(bytea, boolean, bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_gc_prepare(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_purge_prepare(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_gc_claim(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_gc_resolve(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_gc_bind_final(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_gc_complete(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_purge_task_claim(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_purge_task_resolve(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_purge_bind_final(bytea)", true},
		{appACLManagedPublicSchemaR1, "local_blob_purge_task_complete(bytea)", true},
		{appACLManagedPublicSchemaR1, "record_serving_deletion_fence_lock(bytea)", true},
	} {
		fragment.Objects = append(fragment.Objects, AppACLManagedObjectR1{
			ObjectClass: AppACLObjectClassFunction, SchemaName: function.schema, ObjectIdentity: function.identity,
		})
		fragment.Functions = append(fragment.Functions, AppACLCurrentFunctionContract{
			SchemaName: function.schema, Identity: function.identity, Kind: "f",
			SecurityDefiner: function.definer, Config: []string{"search_path=pg_catalog"},
		})
	}
	appendRecordDeletionLegacyAuthority(&fragment)
	fragment.FunctionReplacements = []AppACLCurrentFunctionReplacement{{
		Before: AppACLCurrentFunctionContract{SchemaName: appACLManagedInternalSchemaR1,
			Identity: "validate_record_revision_primary_subject()", Kind: "f", Config: []string{"search_path=pg_catalog"}},
		After: AppACLCurrentFunctionContract{SchemaName: appACLManagedInternalSchemaR1,
			Identity: "validate_record_revision_primary_subject()", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
	}, {
		Before: AppACLCurrentFunctionContract{SchemaName: appACLManagedPublicSchemaR1,
			Identity: "record_platform_compose_membership_heartbeat(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		After: AppACLCurrentFunctionContract{SchemaName: appACLManagedPublicSchemaR1,
			Identity: "record_platform_compose_membership_heartbeat(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
	}}
	return fragment
}

func recordDeletionSuccessorAppACLCurrentPrivileges(databaseName string) []AppACLPrivilege {
	worker := AppACLSubjectRecordDeletionWorker
	privileges := []AppACLPrivilege{
		{Subject: worker, ObjectClass: AppACLObjectClassDatabase, ObjectIdentity: databaseName, Privilege: AppACLPrivilegeConnect},
		{Subject: worker, ObjectClass: AppACLObjectClassSchema, ObjectIdentity: appACLManagedPublicSchemaR1, Privilege: AppACLPrivilegeUsage},
	}
	privileges = append(privileges, AppACLPrivilege{
		Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassFunction,
		ObjectIdentity: "public.record_processor_worker_admit(text, text, text, text)", Privilege: AppACLPrivilegeExecute,
	})
	for _, function := range []string{"public.source_legacy_retirement_install(bytea)", "public.source_legacy_retirement_verify(bytea)"} {
		privileges = append(privileges, AppACLPrivilege{
			Subject: AppACLSubjectPlatformAdmin, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: function, Privilege: AppACLPrivilegeExecute,
		})
	}
	for _, subject := range []AppACLSubject{AppACLSubjectCenterRuntime, AppACLSubjectPlatformAdmin, worker} {
		privileges = append(privileges, AppACLPrivilege{
			Subject: subject, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: "public.source_legacy_retirement_blocked(text, text)", Privilege: AppACLPrivilegeExecute,
		})
	}
	appendTable := func(subject AppACLSubject, table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject: subject, ObjectClass: AppACLObjectClassTable,
				SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: table, Privilege: kind,
			})
		}
	}
	for _, table := range []string{"app_acl_manifest_revisions", "app_acl_manifest_head", "schema_migrations", "deployment_membership", "deployment_contract_state"} {
		appendTable(worker, table, AppACLPrivilegeSelect)
	}
	// Gate, preview and receipt mutations are exclusively constrained-command
	// authority, never ordinary runtime or worker table DML.
	for _, table := range []string{
		"record_deletion_gates", "record_deletion_previews",
		"record_markdown_client_purge_receipts", "record_comparison_purge_receipts",
		"browser_record_cleanup_events",
	} {
		appendTable(AppACLSubjectCenterRuntime, table, AppACLPrivilegeSelect)
	}
	for _, table := range []string{"record_markdown_client_purge_receipts", "record_comparison_purge_receipts"} {
		appendTable(AppACLSubjectPlatformAdmin, table, AppACLPrivilegeSelect)
	}
	for _, table := range []string{
		"comparison_result_dependencies", "comparison_result_dependency_index_completions",
		"comparison_client_workbenches", "comparison_client_save_attempts",
	} {
		appendTable(AppACLSubjectCenterRuntime, table, AppACLPrivilegeSelect)
	}
	for _, table := range []string{
		"browser_record_cleanup_targets", "markdown_client_draft_buffers", "markdown_client_workspaces",
	} {
		appendTable(AppACLSubjectCenterRuntime, table, AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	}
	appendTable(AppACLSubjectCenterRuntime, "browser_record_cleanup_receipts", AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	privileges = append(privileges, AppACLPrivilege{
		Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassFunction,
		ObjectIdentity: "public.record_text_set_is_canonical(text[])", Privilege: AppACLPrivilegeExecute,
	})
	for _, subject := range []AppACLSubject{AppACLSubjectCenterRuntime, AppACLSubjectPlatformAdmin, worker} {
		for _, table := range []string{
			"source_authority_admission", "source_application_principals",
			"source_retirement_barriers", "source_retirement_pins", "source_retirement_coverage_unknown",
		} {
			appendTable(subject, table, AppACLPrivilegeSelect)
		}
		privileges = append(privileges, AppACLPrivilege{
			Subject: subject, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: "public.source_application_admit()", Privilege: AppACLPrivilegeExecute,
		})
		privileges = append(privileges, AppACLPrivilege{
			Subject: subject, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: "public.source_maintenance_status()", Privilege: AppACLPrivilegeExecute,
		})
	}
	appendTable(AppACLSubjectPlatformAdmin, "source_retirement_outbox", AppACLPrivilegeSelect)
	for _, function := range []string{
		"public.source_retirement_preview(text, text, text)",
		"public.source_retirement_commit(bigint, text, text, text, text, text, bytea, bytea, bytea, bytea)",
		"public.source_retirement_install_pin(bigint, text, bigint, bytea, bytea, bytea, bigint)",
	} {
		privileges = append(privileges, AppACLPrivilege{
			Subject: AppACLSubjectPlatformAdmin, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: function, Privilege: AppACLPrivilegeExecute,
		})
	}
	for _, subject := range []AppACLSubject{AppACLSubjectCenterRuntime, AppACLSubjectPlatformAdmin, worker} {
		for _, table := range []string{"local_blob_namespaces", "local_blob_namespace_incarnations", "local_blob_key_anchors", "record_write_obligations", "record_write_obligation_targets"} {
			appendTable(subject, table, AppACLPrivilegeSelect)
		}
		privileges = append(privileges, AppACLPrivilege{Subject: subject, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: "public.local_blob_recovery_inventory()", Privilege: AppACLPrivilegeExecute})
	}
	for _, function := range []string{"public.local_blob_namespace_provision(bytea)", "public.local_blob_namespace_admit(bytea)"} {
		privileges = append(privileges, AppACLPrivilege{
			Subject: AppACLSubjectPlatformAdmin, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: function, Privilege: AppACLPrivilegeExecute,
		})
	}
	for _, subject := range []AppACLSubject{AppACLSubjectCenterRuntime, worker} {
		privileges = append(privileges, AppACLPrivilege{
			Subject: subject, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: "public.local_blob_anchor_register(bytea)", Privilege: AppACLPrivilegeExecute,
		})
	}
	for _, function := range []string{
		"public.record_birth_allocation_find(bytea)", "public.record_birth_current_binding()",
		"public.record_birth_checkpoint_proof(bytea)",
		"public.record_birth_continuity_binding()", "public.record_birth_continuity_assert(bytea)",
		"public.record_birth_allocation_get(bytea)", "public.record_birth_publication_bind(bytea)", "public.record_birth_publication_lookup(bytea)",
		"public.record_evidence_intent_insert(bytea)",
		"public.record_evidence_payload_insert(bytea)",
		"public.record_comparison_index_write(bytea, bytea)",
		"public.record_export_job_claim(bytea)", "public.record_export_job_advance(bytea)",
		"public.record_birth_allocation_begin(bytea)", "public.record_birth_receipt_register(bytea)",
		"public.record_birth_prepare_create(bytea)", "public.record_birth_committed(bytea)",
		"public.record_birth_certification(bytea)", "public.record_birth_consume(bytea)", "public.record_birth_finish(bytea)",
		"public.record_deletion_preview_create(bytea)", "public.record_deletion_preview_lock(bytea)",
		"public.record_deletion_issue(bytea)",
		"public.record_deletion_inventory_lock(bytea)",
		"public.record_mutation_rows_lock(bytea)",
		"public.record_outbox_enqueue(bytea)", "public.record_outbox_claim(bytea)",
		"public.record_outbox_retry(bytea)", "public.record_outbox_sent(bytea)",
		"public.record_outbox_cancel(bytea)", "public.record_outbox_cleanup(bytea)",
		"public.record_external_delivery_outcome(bytea)",
		"public.record_external_delivery_start(bytea)", "public.record_external_delivery_cancel(bytea)",
		"public.record_external_delivery_reconcile(bytea)", "public.record_external_delivery_finalize(bytea)",
		"public.record_collaboration_delivery_inventory(text, bigint)",
		"public.record_collaboration_start_inventory(text, bigint)",
		"public.record_write_register(bytea)", "public.record_write_dispatch(bytea)",
		"public.record_write_resolve(bytea)", "public.record_write_temporary_created(bytea)",
		"public.record_write_return(bytea)",
		"public.record_blob_publication_bind(bytea)",
		"public.record_blob_publication_prepare(bytea)",
		"public.record_upload_part_publish(bytea)",
		"public.record_processor_publication_lock(bytea)", "public.record_processor_publication_finish(bytea)",
		"public.record_blob_pin_create(bytea)", "public.record_blob_pin_release(bytea)",
		"public.record_attachment_copy_source(bytea)", "public.record_attachment_copy_publish(bytea)",
		"public.record_attachment_draft_lock(bytea)", "public.record_attachment_reserve(bytea)", "public.record_attachment_state_set(bytea)",
		"public.record_draft_upload_parts_handoff(bytea)",
		"public.record_revision_namespace_lock(bytea)", "public.record_revision_blob_fence()",
		"public.record_import_attachment_publish(bytea)",
		"public.record_revision_attachment_publish(bytea)",
		"public.record_birth_allocation_lock(bytea)", "public.record_import_plan_lock(bytea)", "public.record_import_mapping_insert(bytea)",
		"public.record_export_artifact_publish(bytea)", "public.record_import_artifact_publish(bytea)",
		"public.record_export_artifact_revoke(bytea)",
		"public.local_blob_cleanup_claim(bytea)", "public.local_blob_cleanup_complete(bytea)",
		"public.record_write_bind_record(bytea)",
		"public.local_blob_gc_prepare(bytea)", "public.local_blob_gc_claim(bytea)",
		"public.local_blob_gc_resolve(bytea)", "public.local_blob_gc_bind_final(bytea)", "public.local_blob_gc_complete(bytea)",
		"public.record_serving_deletion_fence_lock(bytea)",
	} {
		privileges = append(privileges, AppACLPrivilege{
			Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: function, Privilege: AppACLPrivilegeExecute,
		})
	}
	for _, function := range []string{"public.local_blob_purge_claim(bytea)", "public.local_blob_purge_complete(bytea)",
		"public.local_blob_purge_prepare(bytea)", "public.local_blob_purge_task_claim(bytea)",
		"public.local_attachment_purge_prepare(bytea)", "public.local_attachment_purge_complete(bytea)", "public.local_attachment_purge_verify(bytea)",
		"public.local_portability_purge_prepare(bytea)", "public.local_portability_purge_complete(bytea)", "public.local_portability_purge_verify(bytea)",
		"public.record_markdown_client_purge(bytea)", "public.record_markdown_client_purge_verify(bytea)",
		"public.record_comparison_purge(bytea)", "public.record_comparison_purge_verify(bytea)",
		"public.record_collaboration_purge_v2(bytea)", "public.record_collaboration_purge_verify_v2(bytea)",
		"public.record_outbox_drain_v2(bytea)",
		"public.local_blob_purge_task_resolve(bytea)", "public.local_blob_purge_bind_final(bytea)", "public.local_blob_purge_task_complete(bytea)"} {
		privileges = append(privileges, AppACLPrivilege{
			Subject: worker, ObjectClass: AppACLObjectClassFunction,
			ObjectIdentity: function, Privilege: AppACLPrivilegeExecute,
		})
	}
	privileges = append(privileges, recordDeletionLegacyAuthorityPrivileges()...)
	return privileges
}
