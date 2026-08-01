package migrate

import (
	"strings"
	"testing"
)

func TestCompileAppACLPrivilegeSetR1MatchesFrozenCatalogContract(t *testing.T) {
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}
	body, err := CompileAppACLPrivilegeSetR1("houfeng", bindings)
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR1() error = %v", err)
	}
	set, err := ParseCanonicalPrivilegeSetBodyV1(body)
	if err != nil {
		t.Fatalf("ParseCanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	if len(set.RoleBindings) != len(bindings) || set.RoleBindings[0] != bindings[0] || set.RoleBindings[1] != bindings[1] {
		t.Fatalf("role bindings = %#v, want %#v", set.RoleBindings, bindings)
	}

	want := appACLPrivilegeSetR1Contract("houfeng")
	got := make(map[AppACLPrivilege]struct{}, len(set.Privileges))
	for _, privilege := range set.Privileges {
		if privilege.GrantOption {
			t.Fatalf("privilege = %#v has a grant option", privilege)
		}
		got[privilege] = struct{}{}
	}
	if len(got) != 204 || len(got) != len(want) {
		t.Fatalf("privilege tuple count = %d, want %d", len(got), len(want))
	}
	for privilege := range want {
		if _, ok := got[privilege]; !ok {
			t.Fatalf("compiled r1 allowlist is missing %#v", privilege)
		}
	}
	for privilege := range got {
		if _, ok := want[privilege]; !ok {
			t.Fatalf("compiled r1 allowlist has unexpected %#v", privilege)
		}
	}
}

func TestCompileAppACLPrivilegeSetR1RejectsInvalidInputs(t *testing.T) {
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}
	for _, tc := range []struct {
		name         string
		databaseName string
		bindings     []AppACLRoleBinding
	}{
		{name: "empty database", databaseName: "", bindings: bindings},
		{name: "quoted database", databaseName: "houfeng-prod", bindings: bindings},
		{
			name:         "reused role",
			databaseName: "houfeng",
			bindings: []AppACLRoleBinding{
				{Subject: AppACLSubjectCenterRuntime, CatalogRole: "shared"},
				{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "shared"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CompileAppACLPrivilegeSetR1(tc.databaseName, tc.bindings); err == nil || !strings.Contains(err.Error(), "app ACL") {
				t.Fatalf("CompileAppACLPrivilegeSetR1() error = %v, want input rejection", err)
			}
		})
	}
}

func appACLPrivilegeSetR1Contract(databaseName string) map[AppACLPrivilege]struct{} {
	privileges := make(map[AppACLPrivilege]struct{}, 203)
	addDatabaseAndSchema := func(subject AppACLSubject) {
		privileges[AppACLPrivilege{
			Subject:        subject,
			ObjectClass:    AppACLObjectClassDatabase,
			ObjectIdentity: databaseName,
			Privilege:      AppACLPrivilegeConnect,
		}] = struct{}{}
		privileges[AppACLPrivilege{
			Subject:        subject,
			ObjectClass:    AppACLObjectClassSchema,
			ObjectIdentity: "public",
			Privilege:      AppACLPrivilegeUsage,
		}] = struct{}{}
	}
	addRelations := func(subject AppACLSubject, class AppACLObjectClass, names []string, kinds ...AppACLPrivilegeKind) {
		for _, name := range names {
			for _, kind := range kinds {
				privileges[AppACLPrivilege{
					Subject:        subject,
					ObjectClass:    class,
					SchemaName:     "public",
					ObjectIdentity: name,
					Privilege:      kind,
				}] = struct{}{}
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
