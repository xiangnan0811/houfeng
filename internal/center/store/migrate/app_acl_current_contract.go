package migrate

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

const appACLCurrentR1BoundaryMigration = "0051_create_record_platform_foundation.sql"
const appACLCurrentValidationDatabase = "app_acl_current_validation"
const recordsAuthorityCatalogRole = "houfeng_records_authority"

type AppACLCurrentFunctionContract struct {
	SchemaName      string
	Identity        string
	Kind            string
	SecurityDefiner bool
	Config          []string
}

// AppACLCurrentAuxiliaryPrivilege describes a grant to a fixed-purpose role
// outside the frozen runtime/admin privilege body. Auxiliary grants are still
// part of the current catalog contract, but they never alter the canonical r1
// application privilege-set representation.
type AppACLCurrentAuxiliaryPrivilege struct {
	CatalogRole    string
	ObjectClass    AppACLObjectClass
	SchemaName     string
	ObjectIdentity string
	Privilege      AppACLPrivilegeKind
	GrantOption    bool
}

type AppACLCurrentMigrationFragment struct {
	Migration           string
	Objects             []AppACLManagedObjectR1
	Privileges          func(databaseName string) []AppACLPrivilege
	AuxiliaryPrivileges []AppACLCurrentAuxiliaryPrivilege
	Functions           []AppACLCurrentFunctionContract
}

var appACLCurrentMigrationFragments = []AppACLCurrentMigrationFragment{
	recordsCoreAppACLCurrentMigrationFragment(),
	recordAttachmentsAppACLCurrentMigrationFragment(),
	recordEvidenceAppACLCurrentMigrationFragment(),
	recordCollaborationAppACLCurrentMigrationFragment(),
	recordSearchAppACLCurrentMigrationFragment(),
	recordActivityAppACLCurrentMigrationFragment(),
	recordPortabilityAppACLCurrentMigrationFragment(),
	recordPortabilityBlobKeyMuslAppACLCurrentMigrationFragment(),
	recordsAuthorityAppACLCurrentMigrationFragment(),
	subscriptionCreateIdempotencyAppACLCurrentMigrationFragment(),
	vpsCreateIdempotencyAppACLCurrentMigrationFragment(),
	heartbeatIncidentPolicyAppACLCurrentMigrationFragment(),
}

func heartbeatIncidentPolicyAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	return AppACLCurrentMigrationFragment{
		Migration:  "0063_tune_heartbeat_incident_policy.sql",
		Privileges: func(string) []AppACLPrivilege { return nil },
	}
}

var vpsCreateIdempotencyReceiptTables = []string{
	"experience_log_create_idempotency",
	"asset_service_create_idempotency",
	"asset_domain_create_idempotency",
	"vps_monitoring_instance_create_idempotency",
}

func vpsCreateIdempotencyAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, len(vpsCreateIdempotencyReceiptTables))
	for _, table := range vpsCreateIdempotencyReceiptTables {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	return AppACLCurrentMigrationFragment{
		Migration:  "0062_create_vps_create_idempotency.sql",
		Objects:    objects,
		Privileges: vpsCreateIdempotencyAppACLCurrentPrivileges,
	}
}

func vpsCreateIdempotencyAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, len(vpsCreateIdempotencyReceiptTables)*2)
	for _, table := range vpsCreateIdempotencyReceiptTables {
		for _, kind := range []AppACLPrivilegeKind{AppACLPrivilegeSelect, AppACLPrivilegeInsert} {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}
	return privileges
}

func subscriptionCreateIdempotencyAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	return AppACLCurrentMigrationFragment{
		Migration: "0061_create_subscription_create_idempotency.sql",
		Objects: []AppACLManagedObjectR1{{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: "subscription_create_idempotency",
		}},
		Privileges: subscriptionCreateIdempotencyAppACLCurrentPrivileges,
	}
}

func subscriptionCreateIdempotencyAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 2)
	for _, kind := range []AppACLPrivilegeKind{AppACLPrivilegeSelect, AppACLPrivilegeInsert} {
		privileges = append(privileges, AppACLPrivilege{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: "subscription_create_idempotency",
			Privilege:      kind,
		})
	}
	return privileges
}

func recordsAuthorityAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	const heartbeatIdentity = "record_platform_compose_membership_heartbeat(bytea)"
	return AppACLCurrentMigrationFragment{
		Migration: "0060_create_records_authority_heartbeat.sql",
		Objects: []AppACLManagedObjectR1{{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: heartbeatIdentity,
		}},
		Privileges: func(string) []AppACLPrivilege { return nil },
		AuxiliaryPrivileges: []AppACLCurrentAuxiliaryPrivilege{
			{
				CatalogRole:    recordsAuthorityCatalogRole,
				ObjectClass:    AppACLObjectClassDatabase,
				ObjectIdentity: appACLCurrentValidationDatabase,
				Privilege:      AppACLPrivilegeConnect,
			},
			{
				CatalogRole:    recordsAuthorityCatalogRole,
				ObjectClass:    AppACLObjectClassSchema,
				ObjectIdentity: appACLManagedPublicSchemaR1,
				Privilege:      AppACLPrivilegeUsage,
			},
			{
				CatalogRole:    recordsAuthorityCatalogRole,
				ObjectClass:    AppACLObjectClassFunction,
				ObjectIdentity: appACLManagedPublicSchemaR1 + "." + heartbeatIdentity,
				Privilege:      AppACLPrivilegeExecute,
			},
		},
		Functions: []AppACLCurrentFunctionContract{{
			SchemaName:      appACLManagedPublicSchemaR1,
			Identity:        heartbeatIdentity,
			Kind:            "f",
			SecurityDefiner: true,
			Config:          []string{"search_path=pg_catalog"},
		}},
	}
}

func recordActivityAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, 8)
	for _, table := range []string{
		"record_activity_projection_heads",
		"record_activity_projection",
		"record_activity_subjects",
		"record_activity_projection_checkpoints",
		"record_activity_revision_intervals",
		"record_activity_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	functions := []AppACLCurrentFunctionContract{
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "purge_record_activity(text, text, text, text, bigint, bigint, bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedPublicSchemaR1, Identity: "record_activity_purge(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
	}
	for _, function := range functions {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     function.SchemaName,
			ObjectIdentity: function.Identity,
		})
	}
	return AppACLCurrentMigrationFragment{
		Migration:  "0057_create_record_activity.sql",
		Objects:    objects,
		Privileges: recordActivityAppACLCurrentPrivileges,
		Functions:  functions,
	}
}

// recordActivityAppACLCurrentPrivileges grants the projector what a rebuildable
// read model needs and stops there. The runtime may delete the projected tables,
// because dropping the projection and replaying the authoritative sources is the
// documented recovery. What it deliberately cannot do is UPDATE a projected
// fact: a correction is a new event that points at the one it corrects, so an
// in-place rewrite would be the projector editing history rather than recording
// it. The platform admin may inspect purge receipts (counts and digests only)
// and otherwise gets nothing, since projection rows carry authorized presentation.
func recordActivityAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 17)
	appendTable := func(table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}

	// The head row advances its published watermark in place under a row lock,
	// so it needs UPDATE but never DELETE: retiring a generation flips its state.
	appendTable("record_activity_projection_heads",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	appendTable("record_activity_projection",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	appendTable("record_activity_subjects",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	appendTable("record_activity_projection_checkpoints",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	// Closing an interval writes its upper bound, which is why intervals accept
	// UPDATE while projected facts do not.
	appendTable("record_activity_revision_intervals",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	// Receipts are append-only proofs. The controlled purge function is what
	// removes projected rows under a reservation; receipts themselves never
	// update or delete.
	appendTable("record_activity_purge_receipts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	privileges = append(privileges, AppACLPrivilege{
		Subject:        AppACLSubjectPlatformAdmin,
		ObjectClass:    AppACLObjectClassTable,
		SchemaName:     appACLManagedPublicSchemaR1,
		ObjectIdentity: "record_activity_purge_receipts",
		Privilege:      AppACLPrivilegeSelect,
	})
	privileges = append(privileges, AppACLPrivilege{
		Subject:        AppACLSubjectCenterRuntime,
		ObjectClass:    AppACLObjectClassFunction,
		ObjectIdentity: "public.record_activity_purge(bytea)",
		Privilege:      AppACLPrivilegeExecute,
	})
	return privileges
}

func recordPortabilityAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, 11)
	for _, table := range []string{
		"record_export_jobs",
		"record_export_artifacts",
		"record_import_jobs",
		"record_import_plans",
		"record_import_artifacts",
		"record_import_entity_mappings",
		"record_origins",
		"record_origin_tombstones",
		"record_portability_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	functions := []AppACLCurrentFunctionContract{
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "purge_record_portability(text, text, text, text, bigint, bigint, bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedPublicSchemaR1, Identity: "record_portability_purge(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
	}
	for _, function := range functions {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     function.SchemaName,
			ObjectIdentity: function.Identity,
		})
	}
	return AppACLCurrentMigrationFragment{
		Migration:  "0058_create_record_portability.sql",
		Objects:    objects,
		Privileges: recordPortabilityAppACLCurrentPrivileges,
		Functions:  functions,
	}
}

func recordPortabilityBlobKeyMuslAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	return AppACLCurrentMigrationFragment{
		Migration:  "0059_relax_portability_blob_key_regex.sql",
		Privileges: recordPortabilityBlobKeyMuslAppACLCurrentPrivileges,
	}
}

func recordPortabilityBlobKeyMuslAppACLCurrentPrivileges(string) []AppACLPrivilege {
	return nil
}

// recordPortabilityAppACLCurrentPrivileges lets the runtime create and CAS
// export/import job rows and append origin/purge proofs. Platform admin may
// inspect digest-only tombstones and purge receipts, never job inventory or
// artifact locators.
func recordPortabilityAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 28)
	appendTable := func(table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}
	appendTable("record_export_jobs",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	appendTable("record_export_artifacts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	appendTable("record_import_jobs",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	appendTable("record_import_plans",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable("record_import_artifacts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable("record_import_entity_mappings",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable("record_origins",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable("record_origin_tombstones",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable("record_portability_purge_receipts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	privileges = append(privileges, AppACLPrivilege{
		Subject:        AppACLSubjectCenterRuntime,
		ObjectClass:    AppACLObjectClassFunction,
		ObjectIdentity: "public.record_portability_purge(bytea)",
		Privilege:      AppACLPrivilegeExecute,
	})
	for _, table := range []string{"record_origin_tombstones", "record_portability_purge_receipts"} {
		privileges = append(privileges, AppACLPrivilege{
			Subject:        AppACLSubjectPlatformAdmin,
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
			Privilege:      AppACLPrivilegeSelect,
		})
	}
	return privileges
}

func recordsCoreAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, 10)
	for _, table := range []string{
		"records",
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_drafts",
		"record_draft_checkpoints",
		"record_domain_activities",
		"record_core_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	objects = append(objects, AppACLManagedObjectR1{
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "validate_record_revision_primary_subject()",
	})
	return AppACLCurrentMigrationFragment{
		Migration:  "0052_create_records_core.sql",
		Objects:    objects,
		Privileges: recordsCoreAppACLCurrentPrivileges,
		Functions: []AppACLCurrentFunctionContract{
			{
				SchemaName:      appACLManagedInternalSchemaR1,
				Identity:        "validate_record_revision_primary_subject()",
				Kind:            "f",
				SecurityDefiner: false,
				Config:          []string{"search_path=pg_catalog"},
			},
		},
	}
}

func recordsCoreAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 29)
	appendTable := func(subject AppACLSubject, table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}

	runtime := AppACLSubjectCenterRuntime
	appendTable(runtime, "records",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	for _, table := range []string{
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_draft_checkpoints",
		"record_domain_activities",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	}
	appendTable(runtime, "record_drafts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	appendTable(runtime, "record_core_purge_receipts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable(AppACLSubjectPlatformAdmin, "record_core_purge_receipts", AppACLPrivilegeSelect)
	return privileges
}

func recordAttachmentsAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, 13)
	for _, table := range []string{
		"blob_objects",
		"attachment_quota_accounts",
		"record_attachments",
		"attachment_uploads",
		"attachment_upload_parts",
		"record_revision_attachments",
		"attachment_processor_jobs",
		"content_processor_workspaces",
		"blob_gc_pins",
		"blob_gc_deletions",
		"blob_publication_intents",
		"attachment_purge_receipts",
		"content_workspace_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	return AppACLCurrentMigrationFragment{
		Migration:  "0053_create_record_attachments.sql",
		Objects:    objects,
		Privileges: recordAttachmentsAppACLCurrentPrivileges,
	}
}

func recordAttachmentsAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 46)
	appendTable := func(subject AppACLSubject, table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}

	runtime := AppACLSubjectCenterRuntime
	for _, table := range []string{
		"blob_objects",
		"attachment_upload_parts",
		"record_revision_attachments",
		"blob_gc_pins",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	}
	for _, table := range []string{
		"attachment_quota_accounts",
		"record_attachments",
		"attachment_uploads",
		"attachment_processor_jobs",
		"content_processor_workspaces",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert,
			AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	}
	appendTable(runtime, "blob_gc_deletions",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	appendTable(runtime, "blob_publication_intents",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	for _, table := range []string{
		"attachment_purge_receipts",
		"content_workspace_purge_receipts",
	} {
		appendTable(runtime, table, AppACLPrivilegeSelect, AppACLPrivilegeInsert)
		appendTable(AppACLSubjectPlatformAdmin, table, AppACLPrivilegeSelect)
	}
	appendTable(AppACLSubjectPlatformAdmin, "blob_gc_deletions", AppACLPrivilegeSelect)
	appendTable(AppACLSubjectPlatformAdmin, "blob_publication_intents", AppACLPrivilegeSelect)
	return privileges
}

func recordEvidenceAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, 7)
	for _, table := range []string{
		"evidence_payloads",
		"evidence_snapshots",
		"evidence_capture_intents",
		"record_revision_evidence",
		"evidence_copy_lineage",
		"evidence_purge_receipts",
		"evidence_payload_gc_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	return AppACLCurrentMigrationFragment{
		Migration:  "0054_create_record_evidence.sql",
		Objects:    objects,
		Privileges: recordEvidenceAppACLCurrentPrivileges,
	}
}

func recordEvidenceAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 21)
	appendTable := func(subject AppACLSubject, table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}

	runtime := AppACLSubjectCenterRuntime
	for _, table := range []string{
		"evidence_payloads",
		"evidence_snapshots",
		"evidence_capture_intents",
		"record_revision_evidence",
		"evidence_copy_lineage",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	}
	for _, table := range []string{
		"evidence_purge_receipts",
		"evidence_payload_gc_receipts",
	} {
		appendTable(runtime, table, AppACLPrivilegeSelect, AppACLPrivilegeInsert)
		appendTable(AppACLSubjectPlatformAdmin, table, AppACLPrivilegeSelect)
	}
	return privileges
}

func recordCollaborationAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, 15)
	for _, table := range []string{
		"record_actions",
		"record_action_events",
		"record_comments",
		"record_comment_revisions",
		"record_comment_tombstones",
		"record_comment_replies",
		"record_comment_mentions",
		"record_followers",
		"record_notifications",
		"record_notification_recipients",
		"record_notification_deliveries",
		"record_notification_delivery_attempts",
		"record_notification_audit_summaries",
		"record_collaboration_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	functions := []AppACLCurrentFunctionContract{
		{
			SchemaName:      appACLManagedInternalSchemaR1,
			Identity:        "enforce_record_comment_mutation()",
			Kind:            "f",
			SecurityDefiner: false,
			Config:          []string{"search_path=pg_catalog"},
		},
		{
			SchemaName:      appACLManagedInternalSchemaR1,
			Identity:        "enforce_record_comment_revision_mutation()",
			Kind:            "f",
			SecurityDefiner: false,
			Config:          []string{"search_path=pg_catalog"},
		},
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "purge_record_collaboration(text, text, text, text, bigint, bigint, bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "prune_record_revision_followers(text, text[], bigint)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "prune_record_notification_recipients(text, text, text[], bigint)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedPublicSchemaR1, Identity: "record_collaboration_purge(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedPublicSchemaR1, Identity: "record_collaboration_prune_revision_followers(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedPublicSchemaR1, Identity: "record_collaboration_prune_notification_recipients(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
	}
	for _, function := range functions {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     function.SchemaName,
			ObjectIdentity: function.Identity,
		})
	}
	return AppACLCurrentMigrationFragment{
		Migration:  "0055_create_record_collaboration.sql",
		Objects:    objects,
		Privileges: recordCollaborationAppACLCurrentPrivileges,
		Functions:  functions,
	}
}

func recordSearchAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment {
	objects := make([]AppACLManagedObjectR1, 0, 7)
	for _, table := range []string{
		"record_search_generations",
		"record_search_documents",
		"record_search_subjects",
		"record_search_rebuild_jobs",
		"record_search_purge_receipts",
	} {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	functions := []AppACLCurrentFunctionContract{
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "purge_record_search(text, text, text, text, bigint, bigint, bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedInternalSchemaR1, Identity: "retire_record_search_generation(bigint)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedPublicSchemaR1, Identity: "record_search_purge(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
		{SchemaName: appACLManagedPublicSchemaR1, Identity: "record_search_retire_generation(bytea)", Kind: "f", SecurityDefiner: true, Config: []string{"search_path=pg_catalog"}},
	}
	for _, function := range functions {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     function.SchemaName,
			ObjectIdentity: function.Identity,
		})
	}
	return AppACLCurrentMigrationFragment{
		Migration:  "0056_create_record_search.sql",
		Objects:    objects,
		Privileges: recordSearchAppACLCurrentPrivileges,
		Functions:  functions,
	}
}

// recordSearchAppACLCurrentPrivileges grants the derived index exactly what the
// projector and the query path use. Raw DELETE is limited to the subject child
// rows, which one transaction replaces whenever it rewrites their parent
// document. Removing a document, a generation, or a purged record all go
// through controlled functions, so nothing can delete the published generation
// out from under a live query.
func recordSearchAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 16)
	appendTable := func(subject AppACLSubject, table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}

	runtime := AppACLSubjectCenterRuntime
	for _, table := range []string{
		"record_search_generations",
		"record_search_documents",
		"record_search_rebuild_jobs",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	}
	appendTable(runtime, "record_search_subjects",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	appendTable(runtime, "record_search_purge_receipts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable(AppACLSubjectPlatformAdmin, "record_search_purge_receipts", AppACLPrivilegeSelect)
	for _, function := range []string{
		"public.record_search_purge(bytea)",
		"public.record_search_retire_generation(bytea)",
	} {
		privileges = append(privileges, AppACLPrivilege{
			Subject:        runtime,
			ObjectClass:    AppACLObjectClassFunction,
			ObjectIdentity: function,
			Privilege:      AppACLPrivilegeExecute,
		})
	}
	return privileges
}

func recordCollaborationAppACLCurrentPrivileges(string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 35)
	appendTable := func(subject AppACLSubject, table string, kinds ...AppACLPrivilegeKind) {
		for _, kind := range kinds {
			privileges = append(privileges, AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassTable,
				SchemaName:     appACLManagedPublicSchemaR1,
				ObjectIdentity: table,
				Privilege:      kind,
			})
		}
	}

	runtime := AppACLSubjectCenterRuntime
	for _, table := range []string{
		"record_actions",
		"record_comments",
		"record_comment_revisions",
		"record_notification_deliveries",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	}
	appendTable(runtime, "record_notification_recipients",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert,
		AppACLPrivilegeUpdate)
	appendTable(runtime, "record_followers",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert,
		AppACLPrivilegeUpdate)
	for _, table := range []string{
		"record_action_events",
		"record_comment_tombstones",
		"record_comment_replies",
		"record_comment_mentions",
		"record_notifications",
		"record_notification_delivery_attempts",
		"record_notification_audit_summaries",
	} {
		appendTable(runtime, table,
			AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	}
	appendTable(runtime, "record_collaboration_purge_receipts",
		AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	appendTable(AppACLSubjectPlatformAdmin, "record_collaboration_purge_receipts", AppACLPrivilegeSelect)
	for _, function := range []string{
		"public.record_collaboration_purge(bytea)",
		"public.record_collaboration_prune_revision_followers(bytea)",
		"public.record_collaboration_prune_notification_recipients(bytea)",
	} {
		privileges = append(privileges, AppACLPrivilege{Subject: runtime, ObjectClass: AppACLObjectClassFunction, ObjectIdentity: function, Privilege: AppACLPrivilegeExecute})
	}
	return privileges
}

type appACLCurrentSourceContract struct {
	sources   migrationSourceSnapshot
	fragments []appACLCurrentCompiledMigrationFragment
}

type appACLCurrentCompiledMigrationFragment struct {
	Migration           string
	Objects             []AppACLManagedObjectR1
	Privileges          []AppACLPrivilege
	AuxiliaryPrivileges []AppACLCurrentAuxiliaryPrivilege
	Functions           []AppACLCurrentFunctionContract
}

func compileAppACLCurrentSourceContract(
	fsys fs.FS,
	fragments []AppACLCurrentMigrationFragment,
) (appACLCurrentSourceContract, error) {
	sources, err := snapshotMigrationSources(fsys)
	if err != nil {
		return appACLCurrentSourceContract{}, fmt.Errorf("snapshot current migration sources: %w", err)
	}
	if err := validateAppACLR1FrozenSourcePrefix(sources); err != nil {
		return appACLCurrentSourceContract{}, fmt.Errorf("validate current migration frozen r1 prefix: %w", err)
	}

	r1SourceCount := len(appACLR1MigrationSourceContract)
	laterNames := sources.names[r1SourceCount:]
	laterSet := make(map[string]struct{}, len(laterNames))
	for _, name := range laterNames {
		if name <= appACLCurrentR1BoundaryMigration {
			return appACLCurrentSourceContract{}, fmt.Errorf("current migration %q must sort after %q", name, appACLCurrentR1BoundaryMigration)
		}
		laterSet[name] = struct{}{}
	}

	fragmentByMigration := make(map[string]AppACLCurrentMigrationFragment, len(fragments))
	for _, fragment := range fragments {
		if _, duplicate := fragmentByMigration[fragment.Migration]; duplicate {
			return appACLCurrentSourceContract{}, fmt.Errorf("duplicate current APP ACL fragment for migration %q", fragment.Migration)
		}
		fragmentByMigration[fragment.Migration] = cloneAppACLCurrentMigrationFragment(fragment)
	}
	for migration := range fragmentByMigration {
		if _, present := laterSet[migration]; !present {
			return appACLCurrentSourceContract{}, fmt.Errorf("current APP ACL fragment migration %q is not present in the current migration sources", migration)
		}
	}

	orderedFragments := make([]AppACLCurrentMigrationFragment, 0, len(laterNames))
	for _, migration := range laterNames {
		fragment, present := fragmentByMigration[migration]
		if !present {
			return appACLCurrentSourceContract{}, fmt.Errorf("migration %q has no current APP ACL fragment", migration)
		}
		orderedFragments = append(orderedFragments, fragment)
	}
	compiledFragments := make([]appACLCurrentCompiledMigrationFragment, 0, len(orderedFragments))
	for _, fragment := range orderedFragments {
		compiled, err := compileAppACLCurrentMigrationFragment(fragment)
		if err != nil {
			return appACLCurrentSourceContract{}, err
		}
		compiledFragments = append(compiledFragments, compiled)
	}
	if err := validateAppACLCurrentFragments(compiledFragments); err != nil {
		return appACLCurrentSourceContract{}, err
	}

	return appACLCurrentSourceContract{sources: sources, fragments: compiledFragments}, nil
}

func cloneAppACLCurrentMigrationFragment(fragment AppACLCurrentMigrationFragment) AppACLCurrentMigrationFragment {
	cloned := AppACLCurrentMigrationFragment{
		Migration:           fragment.Migration,
		Objects:             append([]AppACLManagedObjectR1(nil), fragment.Objects...),
		Privileges:          fragment.Privileges,
		AuxiliaryPrivileges: append([]AppACLCurrentAuxiliaryPrivilege(nil), fragment.AuxiliaryPrivileges...),
		Functions:           append([]AppACLCurrentFunctionContract(nil), fragment.Functions...),
	}
	for index := range cloned.Functions {
		cloned.Functions[index].Config = append([]string(nil), cloned.Functions[index].Config...)
	}
	return cloned
}

func compileAppACLCurrentMigrationFragment(
	fragment AppACLCurrentMigrationFragment,
) (appACLCurrentCompiledMigrationFragment, error) {
	if fragment.Privileges == nil {
		return appACLCurrentCompiledMigrationFragment{}, fmt.Errorf("current APP ACL fragment %q has no privilege compiler", fragment.Migration)
	}
	return appACLCurrentCompiledMigrationFragment{
		Migration:           fragment.Migration,
		Objects:             append([]AppACLManagedObjectR1(nil), fragment.Objects...),
		Privileges:          append([]AppACLPrivilege(nil), fragment.Privileges(appACLCurrentValidationDatabase)...),
		AuxiliaryPrivileges: append([]AppACLCurrentAuxiliaryPrivilege(nil), fragment.AuxiliaryPrivileges...),
		Functions:           cloneAppACLCurrentFunctionContracts(fragment.Functions),
	}, nil
}

func cloneAppACLCurrentFunctionContracts(
	functions []AppACLCurrentFunctionContract,
) []AppACLCurrentFunctionContract {
	cloned := append([]AppACLCurrentFunctionContract(nil), functions...)
	for index := range cloned {
		cloned[index].Config = append([]string(nil), cloned[index].Config...)
	}
	return cloned
}

func validateAppACLCurrentFragments(fragments []appACLCurrentCompiledMigrationFragment) error {
	baseSurface, err := CompileAppACLManagedSurfaceR1(appACLCurrentValidationDatabase)
	if err != nil {
		return fmt.Errorf("compile frozen r1 managed surface for current APP ACL fragments: %w", err)
	}
	managedObjects := make(map[AppACLManagedObjectR1]struct{}, len(baseSurface.Objects))
	for _, object := range baseSurface.Objects {
		managedObjects[object] = struct{}{}
	}
	newFunctions := make(map[AppACLManagedObjectR1]struct{})
	for _, fragment := range fragments {
		for _, object := range fragment.Objects {
			if err := validateAppACLManagedObject(object); err != nil {
				return fmt.Errorf("current APP ACL fragment %q managed object: %w", fragment.Migration, err)
			}
			if _, duplicate := managedObjects[object]; duplicate {
				return fmt.Errorf("current APP ACL fragment %q has duplicate managed object %#v", fragment.Migration, object)
			}
			managedObjects[object] = struct{}{}
			if object.ObjectClass == AppACLObjectClassFunction {
				newFunctions[object] = struct{}{}
			}
		}
	}

	privileges := appACLPrivilegesR1(appACLCurrentValidationDatabase)
	for _, fragment := range fragments {
		for _, privilege := range fragment.Privileges {
			object, err := appACLCurrentManagedObjectFromPrivilege(privilege)
			if err != nil {
				return fmt.Errorf("current APP ACL fragment %q privilege: %w", fragment.Migration, err)
			}
			if _, managed := managedObjects[object]; !managed {
				return fmt.Errorf("current APP ACL fragment %q privilege references unmanaged object %#v", fragment.Migration, object)
			}
		}
		privileges = append(privileges, fragment.Privileges...)
	}
	if _, err := canonicalPrivileges(privileges); err != nil {
		return fmt.Errorf("validate current APP ACL fragment privileges: %w", err)
	}
	auxiliaryPrivileges := make([]AppACLCurrentAuxiliaryPrivilege, 0)
	for _, fragment := range fragments {
		for _, privilege := range fragment.AuxiliaryPrivileges {
			object, err := appACLCurrentAuxiliaryManagedObject(privilege)
			if err != nil {
				return fmt.Errorf("current APP ACL fragment %q auxiliary privilege: %w", fragment.Migration, err)
			}
			if _, managed := managedObjects[object]; !managed {
				return fmt.Errorf("current APP ACL fragment %q auxiliary privilege references unmanaged object %#v", fragment.Migration, object)
			}
			auxiliaryPrivileges = append(auxiliaryPrivileges, privilege)
		}
	}
	if _, err := canonicalAppACLCurrentAuxiliaryPrivileges(auxiliaryPrivileges); err != nil {
		return fmt.Errorf("validate current APP ACL fragment auxiliary privileges: %w", err)
	}

	hardenedFunctions := make(map[AppACLManagedObjectR1]struct{}, len(newFunctions))
	for _, fragment := range fragments {
		for _, function := range fragment.Functions {
			object := AppACLManagedObjectR1{
				ObjectClass:    AppACLObjectClassFunction,
				SchemaName:     function.SchemaName,
				ObjectIdentity: function.Identity,
			}
			if err := validateAppACLManagedObject(object); err != nil {
				return fmt.Errorf("current APP ACL fragment %q function hardening: %w", fragment.Migration, err)
			}
			if function.Kind != "f" {
				return fmt.Errorf("current APP ACL fragment %q function %s.%s has unsupported kind %q", fragment.Migration, function.SchemaName, function.Identity, function.Kind)
			}
			for _, setting := range function.Config {
				if strings.ContainsRune(setting, '\x00') {
					return fmt.Errorf("current APP ACL fragment %q function %s.%s has invalid configuration", fragment.Migration, function.SchemaName, function.Identity)
				}
			}
			if _, managed := newFunctions[object]; !managed {
				return fmt.Errorf("current APP ACL fragment %q function hardening references unmanaged function %#v", fragment.Migration, object)
			}
			if _, duplicate := hardenedFunctions[object]; duplicate {
				return fmt.Errorf("current APP ACL fragment %q has duplicate function hardening for %#v", fragment.Migration, object)
			}
			hardenedFunctions[object] = struct{}{}
		}
	}
	for function := range newFunctions {
		if _, hardened := hardenedFunctions[function]; !hardened {
			return fmt.Errorf("current APP ACL managed function has no hardening contract: %#v", function)
		}
	}
	return nil
}

func validateAppACLManagedObject(object AppACLManagedObjectR1) error {
	switch object.ObjectClass {
	case AppACLObjectClassDatabase:
		if object.SchemaName != "" || !validBareCatalogName(object.ObjectIdentity) {
			return fmt.Errorf("invalid database object %#v", object)
		}
	case AppACLObjectClassSchema:
		if object.SchemaName != object.ObjectIdentity || !validBareCatalogName(object.SchemaName) {
			return fmt.Errorf("invalid schema object %#v", object)
		}
	case AppACLObjectClassTable, AppACLObjectClassView, AppACLObjectClassSequence:
		if !validBareCatalogName(object.SchemaName) || !validBareCatalogName(object.ObjectIdentity) {
			return fmt.Errorf("invalid relation object %#v", object)
		}
	case AppACLObjectClassFunction:
		if !validBareCatalogName(object.SchemaName) || !validAppACLCurrentFunctionIdentity(object.ObjectIdentity) {
			return fmt.Errorf("invalid function object %#v", object)
		}
	default:
		return fmt.Errorf("unsupported managed object class %q", object.ObjectClass)
	}
	return nil
}

func validAppACLCurrentFunctionIdentity(identity string) bool {
	_, _, valid := appACLCurrentFunctionIdentityParts(identity)
	return valid
}

func appACLCurrentFunctionIdentityParts(identity string) (name string, arguments string, valid bool) {
	name, arguments, found := strings.Cut(identity, "(")
	if !found || !validBareCatalogName(name) || !strings.HasSuffix(arguments, ")") {
		return "", "", false
	}
	arguments = strings.TrimSuffix(arguments, ")")
	for _, character := range arguments {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("_ ,.[]", character) {
			continue
		}
		return "", "", false
	}
	return name, arguments, true
}

func appACLCurrentManagedObjectFromPrivilege(privilege AppACLPrivilege) (AppACLManagedObjectR1, error) {
	if err := validateAppACLPrivilege(privilege); err != nil {
		return AppACLManagedObjectR1{}, err
	}
	switch privilege.ObjectClass {
	case AppACLObjectClassDatabase:
		return AppACLManagedObjectR1{ObjectClass: privilege.ObjectClass, ObjectIdentity: privilege.ObjectIdentity}, nil
	case AppACLObjectClassSchema:
		return AppACLManagedObjectR1{
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.ObjectIdentity,
			ObjectIdentity: privilege.ObjectIdentity,
		}, nil
	case AppACLObjectClassTable, AppACLObjectClassView, AppACLObjectClassSequence:
		return AppACLManagedObjectR1{
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.SchemaName,
			ObjectIdentity: privilege.ObjectIdentity,
		}, nil
	case AppACLObjectClassFunction:
		schemaName, functionIdentity, found := appACLFunctionIdentityFromQualifiedIdentityR1(privilege.ObjectIdentity)
		if !found {
			return AppACLManagedObjectR1{}, fmt.Errorf("invalid function ACL object identity %q", privilege.ObjectIdentity)
		}
		return AppACLManagedObjectR1{
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     schemaName,
			ObjectIdentity: functionIdentity,
		}, nil
	default:
		return AppACLManagedObjectR1{}, fmt.Errorf("unsupported privilege object class %q", privilege.ObjectClass)
	}
}

func compileAppACLCurrentCatalogContract(
	source appACLCurrentSourceContract,
	databaseName string,
	bindings []AppACLRoleBinding,
	migratorRole string,
) (appACLEffectiveCatalogContract, error) {
	r1, err := CompileAppACLEffectiveCatalogContractR1(databaseName, bindings)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("compile frozen r1 catalog base: %w", err)
	}
	contract, err := appACLEffectiveCatalogContractFromR1(r1, migratorRole)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("adapt frozen r1 catalog base: %w", err)
	}

	fragmentFunctions := make(map[AppACLManagedObjectR1]struct{})
	for _, fragment := range source.fragments {
		contract.ManagedObjects = append(contract.ManagedObjects, fragment.Objects...)
		for _, object := range fragment.Objects {
			if object.ObjectClass == AppACLObjectClassFunction {
				fragmentFunctions[object] = struct{}{}
			}
		}
		fragmentPrivileges, err := appACLCurrentPrivilegesForDatabase(fragment.Privileges, databaseName)
		if err != nil {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("materialize current APP ACL fragment %q privileges: %w", fragment.Migration, err)
		}
		contract.Privileges = append(contract.Privileges, fragmentPrivileges...)
		fragmentAuxiliaryPrivileges, err := appACLCurrentAuxiliaryPrivilegesForDatabase(fragment.AuxiliaryPrivileges, databaseName)
		if err != nil {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("materialize current APP ACL fragment %q auxiliary privileges: %w", fragment.Migration, err)
		}
		contract.AuxiliaryPrivileges = append(contract.AuxiliaryPrivileges, fragmentAuxiliaryPrivileges...)
		for _, function := range fragment.Functions {
			contract.ExpectedFunctions = append(contract.ExpectedFunctions, appACLEffectiveCatalogFunctionContract{
				SchemaName:      function.SchemaName,
				Identity:        function.Identity,
				OwnerRole:       migratorRole,
				Kind:            function.Kind,
				SecurityDefiner: function.SecurityDefiner,
				Config:          append([]string(nil), function.Config...),
			})
		}
	}

	canonicalBody, err := CanonicalPrivilegeSetBodyV1(contract.RoleBindings, contract.Privileges)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("canonicalize current APP ACL privileges: %w", err)
	}
	canonicalSet, err := ParseCanonicalPrivilegeSetBodyV1(canonicalBody)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("parse current APP ACL privileges: %w", err)
	}
	contract.RoleBindings = append([]AppACLRoleBinding(nil), canonicalSet.RoleBindings...)
	contract.Privileges = append([]AppACLPrivilege(nil), canonicalSet.Privileges...)
	contract.AuxiliaryPrivileges, err = canonicalAppACLCurrentAuxiliaryPrivileges(contract.AuxiliaryPrivileges)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("canonicalize current APP ACL auxiliary privileges: %w", err)
	}
	for _, privilege := range contract.AuxiliaryPrivileges {
		if privilege.CatalogRole == migratorRole {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("current APP ACL auxiliary role reuses migrator role")
		}
		for _, binding := range contract.RoleBindings {
			if privilege.CatalogRole == binding.CatalogRole {
				return appACLEffectiveCatalogContract{}, fmt.Errorf("current APP ACL auxiliary role reuses application role")
			}
		}
	}

	contract.ManagedObjects, err = canonicalAppACLManagedObjects(contract.ManagedObjects)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("canonicalize current APP ACL managed objects: %w", err)
	}
	contract.ExpectedFunctions, err = canonicalAppACLEffectiveCatalogFunctionContracts(contract.ExpectedFunctions)
	if err != nil {
		return appACLEffectiveCatalogContract{}, fmt.Errorf("canonicalize current APP ACL function hardening: %w", err)
	}

	managed := make(map[AppACLManagedObjectR1]struct{}, len(contract.ManagedObjects))
	for _, object := range contract.ManagedObjects {
		managed[object] = struct{}{}
	}
	for _, privilege := range contract.Privileges {
		object, err := appACLCurrentManagedObjectFromPrivilege(privilege)
		if err != nil {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("map current APP ACL privilege to managed object: %w", err)
		}
		if _, ok := managed[object]; !ok {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("current APP ACL privilege references unmanaged object %#v", object)
		}
	}
	for _, privilege := range contract.AuxiliaryPrivileges {
		object, err := appACLCurrentAuxiliaryManagedObject(privilege)
		if err != nil {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("map current APP ACL auxiliary privilege to managed object: %w", err)
		}
		if _, ok := managed[object]; !ok {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("current APP ACL auxiliary privilege references unmanaged object %#v", object)
		}
	}
	expectedFunctions := make(map[AppACLManagedObjectR1]struct{}, len(contract.ExpectedFunctions))
	for _, function := range contract.ExpectedFunctions {
		object := AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     function.SchemaName,
			ObjectIdentity: function.Identity,
		}
		if _, ok := managed[object]; !ok {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("current APP ACL function hardening references unmanaged function %#v", object)
		}
		expectedFunctions[object] = struct{}{}
	}
	for function := range fragmentFunctions {
		if _, ok := expectedFunctions[function]; !ok {
			return appACLEffectiveCatalogContract{}, fmt.Errorf("current APP ACL managed function has no hardening contract: %#v", function)
		}
	}
	return contract, nil
}

func appACLCurrentAuxiliaryManagedObject(privilege AppACLCurrentAuxiliaryPrivilege) (AppACLManagedObjectR1, error) {
	if !validCatalogRoleName(privilege.CatalogRole) {
		return AppACLManagedObjectR1{}, fmt.Errorf("invalid auxiliary catalog role")
	}
	if privilege.GrantOption {
		return AppACLManagedObjectR1{}, fmt.Errorf("auxiliary privilege has grant option")
	}
	applicationPrivilege := AppACLPrivilege{
		Subject:        AppACLSubjectCenterRuntime,
		ObjectClass:    privilege.ObjectClass,
		SchemaName:     privilege.SchemaName,
		ObjectIdentity: privilege.ObjectIdentity,
		Privilege:      privilege.Privilege,
		GrantOption:    privilege.GrantOption,
	}
	return appACLCurrentManagedObjectFromPrivilege(applicationPrivilege)
}

func appACLCurrentAuxiliaryPrivilegesForDatabase(
	template []AppACLCurrentAuxiliaryPrivilege,
	databaseName string,
) ([]AppACLCurrentAuxiliaryPrivilege, error) {
	if !validBareCatalogName(databaseName) {
		return nil, fmt.Errorf("invalid app ACL database name")
	}
	privileges := append([]AppACLCurrentAuxiliaryPrivilege(nil), template...)
	for index := range privileges {
		if privileges[index].ObjectClass == AppACLObjectClassDatabase &&
			privileges[index].ObjectIdentity == appACLCurrentValidationDatabase {
			privileges[index].ObjectIdentity = databaseName
		}
	}
	return privileges, nil
}

func canonicalAppACLCurrentAuxiliaryPrivileges(
	privileges []AppACLCurrentAuxiliaryPrivilege,
) ([]AppACLCurrentAuxiliaryPrivilege, error) {
	ordered := append([]AppACLCurrentAuxiliaryPrivilege(nil), privileges...)
	for _, privilege := range ordered {
		if _, err := appACLCurrentAuxiliaryManagedObject(privilege); err != nil {
			return nil, err
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.CatalogRole != right.CatalogRole {
			return left.CatalogRole < right.CatalogRole
		}
		if left.ObjectClass != right.ObjectClass {
			return left.ObjectClass < right.ObjectClass
		}
		if left.SchemaName != right.SchemaName {
			return left.SchemaName < right.SchemaName
		}
		if left.ObjectIdentity != right.ObjectIdentity {
			return left.ObjectIdentity < right.ObjectIdentity
		}
		if left.Privilege != right.Privilege {
			return left.Privilege < right.Privilege
		}
		return !left.GrantOption && right.GrantOption
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1] == ordered[index] {
			return nil, fmt.Errorf("duplicate auxiliary privilege tuple")
		}
	}
	return ordered, nil
}

func appACLCurrentPrivilegesForDatabase(
	template []AppACLPrivilege,
	databaseName string,
) ([]AppACLPrivilege, error) {
	if !validBareCatalogName(databaseName) {
		return nil, fmt.Errorf("invalid app ACL database name")
	}
	privileges := append([]AppACLPrivilege(nil), template...)
	for index := range privileges {
		if privileges[index].ObjectClass == AppACLObjectClassDatabase &&
			privileges[index].ObjectIdentity == appACLCurrentValidationDatabase {
			privileges[index].ObjectIdentity = databaseName
		}
	}
	return privileges, nil
}

func canonicalAppACLEffectiveCatalogFunctionContracts(
	functions []appACLEffectiveCatalogFunctionContract,
) ([]appACLEffectiveCatalogFunctionContract, error) {
	ordered := append([]appACLEffectiveCatalogFunctionContract(nil), functions...)
	for index := range ordered {
		ordered[index].Config = append([]string(nil), ordered[index].Config...)
		object := AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     ordered[index].SchemaName,
			ObjectIdentity: ordered[index].Identity,
		}
		if err := validateAppACLManagedObject(object); err != nil {
			return nil, err
		}
		if !validCatalogRoleName(ordered[index].OwnerRole) {
			return nil, fmt.Errorf("invalid function owner role %q", ordered[index].OwnerRole)
		}
		if ordered[index].Kind != "f" {
			return nil, fmt.Errorf("function %s.%s has unsupported kind %q", ordered[index].SchemaName, ordered[index].Identity, ordered[index].Kind)
		}
		for _, setting := range ordered[index].Config {
			if strings.ContainsRune(setting, '\x00') {
				return nil, fmt.Errorf("function %s.%s has invalid configuration", ordered[index].SchemaName, ordered[index].Identity)
			}
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SchemaName != ordered[j].SchemaName {
			return ordered[i].SchemaName < ordered[j].SchemaName
		}
		return ordered[i].Identity < ordered[j].Identity
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].SchemaName == ordered[index].SchemaName && ordered[index-1].Identity == ordered[index].Identity {
			return nil, fmt.Errorf("duplicate function hardening for %s.%s", ordered[index].SchemaName, ordered[index].Identity)
		}
	}
	return ordered, nil
}
