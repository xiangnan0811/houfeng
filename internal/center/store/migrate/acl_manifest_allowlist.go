package migrate

import "fmt"

// CompileAppACLPrivilegeSetR1 returns the frozen r1 application catalog
// allowlist for one database and its pre-created runtime/admin role bindings.
func CompileAppACLPrivilegeSetR1(databaseName string, bindings []AppACLRoleBinding) ([]byte, error) {
	if !validBareCatalogName(databaseName) {
		return nil, fmt.Errorf("invalid app ACL database name")
	}
	body, err := CanonicalPrivilegeSetBodyV1(bindings, appACLPrivilegesR1(databaseName))
	if err != nil {
		return nil, fmt.Errorf("build app ACL r1 privilege set: %w", err)
	}
	return body, nil
}

func appACLPrivilegesR1(databaseName string) []AppACLPrivilege {
	privileges := make([]AppACLPrivilege, 0, 204)
	addDatabaseAndSchema := func(subject AppACLSubject) {
		privileges = append(privileges,
			AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassDatabase,
				ObjectIdentity: databaseName,
				Privilege:      AppACLPrivilegeConnect,
			},
			AppACLPrivilege{
				Subject:        subject,
				ObjectClass:    AppACLObjectClassSchema,
				ObjectIdentity: "public",
				Privilege:      AppACLPrivilegeUsage,
			},
		)
	}
	addRelations := func(subject AppACLSubject, class AppACLObjectClass, names []string, kinds ...AppACLPrivilegeKind) {
		for _, name := range names {
			for _, kind := range kinds {
				privileges = append(privileges, AppACLPrivilege{
					Subject:        subject,
					ObjectClass:    class,
					SchemaName:     "public",
					ObjectIdentity: name,
					Privilege:      kind,
				})
			}
		}
	}
	runtime := AppACLSubjectCenterRuntime
	addDatabaseAndSchema(runtime)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"schema_migrations",
		"app_acl_manifest_revisions",
		"app_acl_manifest_head",
	}, AppACLPrivilegeSelect)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"active_incidents",
		"asset_decision_manual_group_members",
		"monitoring_instance_host_sample_daily_aggregates",
		"target_probe_daily_aggregates",
		"monitoring_instances",
		"ip_quality_reports",
		"probe_items",
		"sessions",
	}, AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"asset_decision_manual_groups",
		"asset_decision_record_members",
		"asset_decision_records",
		"asset_decision_scenario_templates",
		"center_settings",
		"providers",
		"subscription_budgets",
		"subscription_exchange_rates",
		"subscription_monthly_budgets",
		"subscription_reminder_deliveries",
		"subscriptions",
		"targets",
		"users",
		"vps_assets",
		"vps_monitoring_instance_links",
	}, AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"asset_lifecycle_action_steps",
		"host_samples",
		"monitoring_instance_heartbeats",
		"notification_records",
		"probe_observations",
		"state_change_events",
	}, AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeDelete)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"asset_decision_scenario_template_members",
		"asset_domains",
		"asset_services",
		"experience_logs",
		"ip_histories",
		"ip_quality_provider_results",
		"ip_quality_service_unlocks",
		"monitoring_instance_command_action_audit",
		"price_histories",
		"renewal_decisions",
		"vps_spec_snapshots",
	}, AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"agent_sync_batches",
		"asset_lifecycle_actions",
	}, AppACLPrivilegeInsert)
	addRelations(runtime, AppACLObjectClassView, []string{
		"asset_decision_records_with_counts",
		"ip_quality_assigned_vps_reports",
		"ip_quality_latest_vps_summaries",
	}, AppACLPrivilegeSelect)
	addRelations(runtime, AppACLObjectClassSequence, []string{
		"node_heartbeats_id_seq",
		"host_samples_id_seq",
		"probe_observations_id_seq",
	}, AppACLPrivilegeUsage)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"record_outbox",
		"record_idempotency_keys",
		"identity_mutation_guards",
		"deletion_reservations",
		"deletion_fence_leases",
		"object_content_leases",
		"client_content_leases",
		"content_delivery_epochs",
		"backup_epochs",
		"recovery_inventory_projection",
		"deployment_membership",
	}, AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"record_purge_operations",
		"deletion_replay_state",
	}, AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"record_deletion_audits",
		"source_deletion_tombstones",
	}, AppACLPrivilegeSelect, AppACLPrivilegeInsert)
	addRelations(runtime, AppACLObjectClassTable, []string{
		"record_access_groups",
		"record_access_group_members",
		"record_platform_domain_identity",
		"record_platform_domain_attestations",
		"deployment_contract_state",
	}, AppACLPrivilegeSelect)
	addRelations(runtime, AppACLObjectClassSequence, []string{"record_outbox_outbox_row_id_seq"}, AppACLPrivilegeUsage)
	admin := AppACLSubjectPlatformAdmin
	addDatabaseAndSchema(admin)
	addRelations(admin, AppACLObjectClassTable, []string{
		"schema_migrations",
		"app_acl_manifest_revisions",
		"app_acl_manifest_head",
		"record_platform_domain_identity",
		"record_platform_domain_attestations",
		"backup_epochs",
		"recovery_inventory_projection",
		"deletion_replay_state",
		"deployment_membership",
		"deployment_contract_state",
	}, AppACLPrivilegeSelect)
	addRelations(admin, AppACLObjectClassTable, []string{"deletion_replay_state"}, AppACLPrivilegeInsert, AppACLPrivilegeUpdate)

	return privileges
}
