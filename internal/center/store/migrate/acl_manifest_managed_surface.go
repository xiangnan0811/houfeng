package migrate

import (
	"fmt"
	"sort"
)

const appACLManagedPublicSchemaR1 = "public"
const appACLManagedInternalSchemaR1 = "record_platform_internal"

// AppACLManagedObjectR1 is one migration-owned catalog object whose owner and
// ACL state participate in r1 admission. It intentionally excludes unrelated
// schemas and pgcrypto extension members.
type AppACLManagedObjectR1 struct {
	ObjectClass    AppACLObjectClass
	SchemaName     string
	ObjectIdentity string
}

// AppACLManagedSurfaceR1 is the closed APP-only catalog inventory for the
// 0001…0051 migration set. It is deliberately independent of the r1
// runtime/admin grant compiler: objects with no APP grant are still owned and
// ACL-audited migration surface.
type AppACLManagedSurfaceR1 struct {
	DatabaseName string
	Objects      []AppACLManagedObjectR1
}

func CompileAppACLManagedSurfaceR1(databaseName string) (AppACLManagedSurfaceR1, error) {
	if !validBareCatalogName(databaseName) {
		return AppACLManagedSurfaceR1{}, fmt.Errorf("invalid app ACL database name")
	}
	objects := map[AppACLManagedObjectR1]struct{}{
		{ObjectClass: AppACLObjectClassDatabase, ObjectIdentity: databaseName}:                                                           {},
		{ObjectClass: AppACLObjectClassSchema, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: appACLManagedPublicSchemaR1}:     {},
		{ObjectClass: AppACLObjectClassSchema, SchemaName: appACLManagedInternalSchemaR1, ObjectIdentity: appACLManagedInternalSchemaR1}: {},
	}
	for _, relation := range appACLManagedRelationsR1() {
		objects[AppACLManagedObjectR1{
			ObjectClass:    relation.objectClass,
			SchemaName:     relation.schemaName,
			ObjectIdentity: relation.identity,
		}] = struct{}{}
	}
	for _, function := range appACLManagedFunctionsR1() {
		objects[AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     function.schemaName,
			ObjectIdentity: function.identity,
		}] = struct{}{}
	}

	ordered := make([]AppACLManagedObjectR1, 0, len(objects))
	for object := range objects {
		ordered = append(ordered, object)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ObjectClass != ordered[j].ObjectClass {
			return ordered[i].ObjectClass < ordered[j].ObjectClass
		}
		if ordered[i].SchemaName != ordered[j].SchemaName {
			return ordered[i].SchemaName < ordered[j].SchemaName
		}
		return ordered[i].ObjectIdentity < ordered[j].ObjectIdentity
	})
	return AppACLManagedSurfaceR1{DatabaseName: databaseName, Objects: ordered}, nil
}

func canonicalAppACLManagedObjects(objects []AppACLManagedObjectR1) ([]AppACLManagedObjectR1, error) {
	ordered := append([]AppACLManagedObjectR1(nil), objects...)
	for _, object := range ordered {
		if err := validateAppACLManagedObject(object); err != nil {
			return nil, err
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ObjectClass != ordered[j].ObjectClass {
			return ordered[i].ObjectClass < ordered[j].ObjectClass
		}
		if ordered[i].SchemaName != ordered[j].SchemaName {
			return ordered[i].SchemaName < ordered[j].SchemaName
		}
		return ordered[i].ObjectIdentity < ordered[j].ObjectIdentity
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1] == ordered[index] {
			return nil, fmt.Errorf("duplicate managed object %#v", ordered[index])
		}
	}
	return ordered, nil
}

type appACLManagedRelationR1 struct {
	objectClass AppACLObjectClass
	schemaName  string
	identity    string
}

// appACLManagedRelationsR1 is the final relation/view/sequence inventory
// created or renamed by the fixed 0001…0051 APP migration stream, plus the
// migration runner's public schema_migrations ledger. It must not be derived
// from runtime/admin grants: r1 still audits owner and ACL drift for objects
// that carry no APP privilege tuple.
func appACLManagedRelationsR1() []appACLManagedRelationR1 {
	publicRelations := func(objectClass AppACLObjectClass, names ...string) []appACLManagedRelationR1 {
		relations := make([]appACLManagedRelationR1, 0, len(names))
		for _, name := range names {
			relations = append(relations, appACLManagedRelationR1{
				objectClass: objectClass,
				schemaName:  appACLManagedPublicSchemaR1,
				identity:    name,
			})
		}
		return relations
	}

	relations := publicRelations(AppACLObjectClassTable,
		"schema_migrations",
		"active_incidents",
		"agent_sync_batches",
		"app_acl_manifest_head",
		"app_acl_manifest_revisions",
		"asset_decision_manual_group_members",
		"asset_decision_manual_groups",
		"asset_decision_record_members",
		"asset_decision_records",
		"asset_decision_scenario_template_members",
		"asset_decision_scenario_templates",
		"asset_domains",
		"asset_lifecycle_action_steps",
		"asset_lifecycle_actions",
		"asset_services",
		"backup_epochs",
		"center_settings",
		"client_content_leases",
		"content_delivery_epochs",
		"deletion_fence_leases",
		"deletion_replay_state",
		"deletion_reservations",
		"deployment_contract_state",
		"deployment_membership",
		"experience_logs",
		"host_samples",
		"identity_mutation_guards",
		"ip_histories",
		"ip_quality_provider_results",
		"ip_quality_reports",
		"ip_quality_service_unlocks",
		"monitoring_instance_command_action_audit",
		"monitoring_instance_heartbeats",
		"monitoring_instance_host_sample_daily_aggregates",
		"monitoring_instances",
		"notification_records",
		"object_content_leases",
		"price_histories",
		"probe_items",
		"probe_observations",
		"providers",
		"record_access_group_members",
		"record_access_groups",
		"record_deletion_audits",
		"record_idempotency_keys",
		"record_outbox",
		"record_platform_domain_attestations",
		"record_platform_domain_identity",
		"record_purge_operations",
		"recovery_inventory_projection",
		"renewal_decisions",
		"sessions",
		"source_deletion_tombstones",
		"state_change_events",
		"subscription_budgets",
		"subscription_exchange_rates",
		"subscription_monthly_budgets",
		"subscription_reminder_deliveries",
		"subscriptions",
		"target_probe_daily_aggregates",
		"targets",
		"users",
		"vps_assets",
		"vps_monitoring_instance_links",
		"vps_spec_snapshots",
	)
	relations = append(relations,
		publicRelations(AppACLObjectClassView,
			"asset_decision_records_with_counts",
			"ip_quality_assigned_vps_reports",
			"ip_quality_latest_vps_summaries",
		)...,
	)
	relations = append(relations,
		publicRelations(AppACLObjectClassSequence,
			"host_samples_id_seq",
			"node_heartbeats_id_seq",
			"probe_observations_id_seq",
			"record_outbox_outbox_row_id_seq",
		)...,
	)
	return relations
}

type appACLManagedFunctionR1 struct {
	schemaName string
	identity   string
}

func appACLManagedFunctionsR1() []appACLManagedFunctionR1 {
	functions := []appACLManagedFunctionR1{
		{schemaName: appACLManagedInternalSchemaR1, identity: "reject_acl_manifest_revision_mutation()"},
		{schemaName: appACLManagedInternalSchemaR1, identity: "reject_immutable_mutation()"},
		{schemaName: appACLManagedInternalSchemaR1, identity: "record_platform_projection_cas_receipt_v1(p_command bytea)"},
		{schemaName: appACLManagedInternalSchemaR1, identity: "record_platform_projection_read_bytes_v1(p_command bytea, p_offset integer, p_length integer)"},
		{schemaName: appACLManagedInternalSchemaR1, identity: "record_platform_projection_read_profile_v1(p_command bytea, p_offset integer)"},
		{schemaName: appACLManagedInternalSchemaR1, identity: "record_platform_projection_read_token_v1(p_command bytea, p_offset integer, p_prefix text)"},
		{schemaName: appACLManagedInternalSchemaR1, identity: "record_platform_projection_read_uint64_v1(p_command bytea, p_offset integer)"},
		{schemaName: appACLManagedInternalSchemaR1, identity: "record_platform_projection_validate_header_v1(p_command bytea, p_operation integer, p_field_count integer, p_exact_length integer)"},
	}
	projectors := appACLProjectorFunctionsR1()
	return append(functions, projectors[:]...)
}

// appACLProjectorFunctionsR1 is the sole r1 identity source for the two
// public bytea projectors. The managed surface and verifier both consume it.
func appACLProjectorFunctionsR1() [2]appACLManagedFunctionR1 {
	return [2]appACLManagedFunctionR1{
		{schemaName: appACLManagedPublicSchemaR1, identity: "record_platform_cas_contract_activation_projection(bytea)"},
		{schemaName: appACLManagedPublicSchemaR1, identity: "record_platform_cas_domain_rotation_projection(bytea)"},
	}
}
