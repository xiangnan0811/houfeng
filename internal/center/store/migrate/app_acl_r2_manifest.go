package migrate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"
)

const appACLR2PrivilegeSetMagic = "HOUFENG-APP-ACL-R2-PRIVILEGE-SET-V1"
const appACLR2ControlACLMagic = "HOUFENG-APP-ACL-R2-CONTROL-ACL-V1"
const appACLR2ManifestMagic = "HOUFENG-APP-ACL-MANIFEST-R2-V1"
const appACLR2CodecVersion uint16 = 1
const appACLR2PrivilegeCount = 206

// AppACLSubjectR2 is the fixed numeric application subject tag.
type AppACLSubjectR2 uint8

const (
	AppACLSubjectCenterRuntimeR2  AppACLSubjectR2 = 1
	AppACLSubjectDirectMigratorR2 AppACLSubjectR2 = 2
	AppACLSubjectPlatformAdminR2  AppACLSubjectR2 = 3
)

// AppACLObjectClassR2 is the fixed numeric object-class tag. Column remains a
// named rejection value so callers cannot silently treat tag 7 as supported.
type AppACLObjectClassR2 uint8

const (
	AppACLObjectClassDatabaseR2 AppACLObjectClassR2 = 1
	AppACLObjectClassSchemaR2   AppACLObjectClassR2 = 2
	AppACLObjectClassTableR2    AppACLObjectClassR2 = 3
	AppACLObjectClassViewR2     AppACLObjectClassR2 = 4
	AppACLObjectClassSequenceR2 AppACLObjectClassR2 = 5
	AppACLObjectClassFunctionR2 AppACLObjectClassR2 = 6
	AppACLObjectClassColumnR2   AppACLObjectClassR2 = 7
)

// AppACLPrivilegeKindR2 is the fixed numeric PostgreSQL privilege tag.
type AppACLPrivilegeKindR2 uint8

const (
	AppACLPrivilegeConnectR2 AppACLPrivilegeKindR2 = 1
	AppACLPrivilegeUsageR2   AppACLPrivilegeKindR2 = 2
	AppACLPrivilegeSelectR2  AppACLPrivilegeKindR2 = 3
	AppACLPrivilegeInsertR2  AppACLPrivilegeKindR2 = 4
	AppACLPrivilegeUpdateR2  AppACLPrivilegeKindR2 = 5
	AppACLPrivilegeDeleteR2  AppACLPrivilegeKindR2 = 6
	AppACLPrivilegeExecuteR2 AppACLPrivilegeKindR2 = 7
)

// AppACLRoleBindingR2V1 binds one fixed subject tag to a direct catalog role.
type AppACLRoleBindingR2V1 struct {
	Subject     AppACLSubjectR2
	CatalogRole string
}

// AppACLPrivilegeR2V1 is one exact R2 application privilege tuple.
type AppACLPrivilegeR2V1 struct {
	Subject        AppACLSubjectR2
	ObjectClass    AppACLObjectClassR2
	SchemaName     string
	ObjectIdentity string
	ColumnName     string
	Privilege      AppACLPrivilegeKindR2
	GrantOption    bool
}

// AppACLPrivilegeSetR2V1 is the decoded three-binding/206-tuple body.
type AppACLPrivilegeSetR2V1 struct {
	RoleBindings []AppACLRoleBindingR2V1
	Privileges   []AppACLPrivilegeR2V1
}

// CompileAppACLPrivilegeSetR2V1 builds the R2-owned exact application
// privilege contract. It never invokes a V1 compiler or codec.
func CompileAppACLPrivilegeSetR2V1(databaseName string, bindings []AppACLRoleBindingR2V1) ([]byte, error) {
	if !validAppACLR2RoleName(databaseName) {
		return nil, fmt.Errorf("invalid APP ACL R2 database name")
	}
	return CanonicalAppACLPrivilegeSetR2BodyV1(AppACLPrivilegeSetR2V1{
		RoleBindings: append([]AppACLRoleBindingR2V1(nil), bindings...),
		Privileges:   appACLR2PrivilegeContract(databaseName),
	})
}

// CanonicalAppACLPrivilegeSetR2BodyV1 emits only the exact 206-tuple R2
// contract in strict semantic-key order.
func CanonicalAppACLPrivilegeSetR2BodyV1(set AppACLPrivilegeSetR2V1) ([]byte, error) {
	if err := validateAppACLR2Bindings(set.RoleBindings); err != nil {
		return nil, err
	}
	databaseName, err := appACLR2PrivilegeDatabaseName(set.Privileges)
	if err != nil {
		return nil, err
	}
	expected := appACLR2PrivilegeContract(databaseName)
	if len(set.Privileges) != len(expected) {
		return nil, fmt.Errorf("APP ACL R2 privilege set has %d tuples, want %d", len(set.Privileges), len(expected))
	}
	for index, privilege := range set.Privileges {
		if err := validateAppACLR2Privilege(privilege); err != nil {
			return nil, fmt.Errorf("validate APP ACL R2 privilege %d: %w", index, err)
		}
		if index > 0 && compareAppACLR2Privilege(set.Privileges[index-1], privilege) >= 0 {
			return nil, fmt.Errorf("APP ACL R2 privileges are not strictly ordered")
		}
		if compareAppACLR2Privilege(privilege, expected[index]) != 0 {
			return nil, fmt.Errorf("APP ACL R2 privilege %d does not match the fixed contract", index)
		}
	}

	body := make([]byte, 0, len(appACLR2PrivilegeSetMagic)+4+len(set.Privileges)*32)
	body = append(body, appACLR2PrivilegeSetMagic...)
	body = appendAppACLR2Uint16(body, appACLR2CodecVersion)
	body = appendAppACLR2Uint16(body, uint16(len(set.RoleBindings)))
	for _, binding := range set.RoleBindings {
		body = append(body, byte(binding.Subject))
		body = appendAppACLR2String16(body, binding.CatalogRole)
	}
	body = appendAppACLR2Uint16(body, uint16(len(set.Privileges)))
	for _, privilege := range set.Privileges {
		body = append(body, byte(privilege.Subject), byte(privilege.ObjectClass))
		body = appendAppACLR2String16(body, privilege.SchemaName)
		body = appendAppACLR2String16(body, privilege.ObjectIdentity)
		body = appendAppACLR2String16(body, privilege.ColumnName)
		body = append(body, byte(privilege.Privilege), 0)
	}
	return body, nil
}

// ParseCanonicalAppACLPrivilegeSetR2BodyV1 rejects any body other than the
// exact R2 contract, including V1 magic, count drift, tuple reordering, and
// trailing data.
func ParseCanonicalAppACLPrivilegeSetR2BodyV1(body []byte) (AppACLPrivilegeSetR2V1, error) {
	if len(body) > appACLR2MaximumBodyBytes || !bytes.HasPrefix(body, []byte(appACLR2PrivilegeSetMagic)) {
		return AppACLPrivilegeSetR2V1{}, fmt.Errorf("invalid APP ACL R2 privilege-set magic or size")
	}
	decoder := appACLR2Decoder{body: body, offset: len(appACLR2PrivilegeSetMagic)}
	version, err := decoder.uint16("privilege-set version")
	if err != nil {
		return AppACLPrivilegeSetR2V1{}, err
	}
	if version != appACLR2CodecVersion {
		return AppACLPrivilegeSetR2V1{}, fmt.Errorf("APP ACL R2 privilege-set version is %d, want %d", version, appACLR2CodecVersion)
	}
	bindingCount, err := decoder.uint16("binding count")
	if err != nil {
		return AppACLPrivilegeSetR2V1{}, err
	}
	if bindingCount != 3 {
		return AppACLPrivilegeSetR2V1{}, fmt.Errorf("APP ACL R2 privilege set has %d bindings, want 3", bindingCount)
	}
	bindings := make([]AppACLRoleBindingR2V1, 0, bindingCount)
	for range int(bindingCount) {
		subject, err := decoder.uint8("binding subject")
		if err != nil {
			return AppACLPrivilegeSetR2V1{}, err
		}
		role, err := decoder.string16(1, 63, "binding catalog role")
		if err != nil {
			return AppACLPrivilegeSetR2V1{}, err
		}
		bindings = append(bindings, AppACLRoleBindingR2V1{Subject: AppACLSubjectR2(subject), CatalogRole: role})
	}
	if err := validateAppACLR2Bindings(bindings); err != nil {
		return AppACLPrivilegeSetR2V1{}, err
	}
	tupleCount, err := decoder.uint16("privilege tuple count")
	if err != nil {
		return AppACLPrivilegeSetR2V1{}, err
	}
	if tupleCount != appACLR2PrivilegeCount {
		return AppACLPrivilegeSetR2V1{}, fmt.Errorf("APP ACL R2 privilege set has %d tuples, want %d", tupleCount, appACLR2PrivilegeCount)
	}
	privileges := make([]AppACLPrivilegeR2V1, 0, tupleCount)
	for index := range int(tupleCount) {
		subject, err := decoder.uint8("privilege subject")
		if err != nil {
			return AppACLPrivilegeSetR2V1{}, err
		}
		objectClass, err := decoder.uint8("privilege object class")
		if err != nil {
			return AppACLPrivilegeSetR2V1{}, err
		}
		schemaName, err := decoder.string16(0, 63, "privilege schema")
		if err != nil {
			return AppACLPrivilegeSetR2V1{}, err
		}
		identity, err := decoder.string16(1, 1024, "privilege object identity")
		if err != nil {
			return AppACLPrivilegeSetR2V1{}, err
		}
		columnName, err := decoder.string16(0, 63, "privilege column")
		if err != nil {
			return AppACLPrivilegeSetR2V1{}, err
		}
		privilegeTag, err := decoder.uint8("privilege tag")
		if err != nil {
			return AppACLPrivilegeSetR2V1{}, err
		}
		grantOption, err := decoder.uint8("privilege grant option")
		if err != nil {
			return AppACLPrivilegeSetR2V1{}, err
		}
		if grantOption != 0 {
			return AppACLPrivilegeSetR2V1{}, fmt.Errorf("APP ACL R2 privilege grant option must be zero")
		}
		value := AppACLPrivilegeR2V1{
			Subject:        AppACLSubjectR2(subject),
			ObjectClass:    AppACLObjectClassR2(objectClass),
			SchemaName:     schemaName,
			ObjectIdentity: identity,
			ColumnName:     columnName,
			Privilege:      AppACLPrivilegeKindR2(privilegeTag),
		}
		if err := validateAppACLR2Privilege(value); err != nil {
			return AppACLPrivilegeSetR2V1{}, fmt.Errorf("validate APP ACL R2 privilege %d: %w", index, err)
		}
		if index > 0 && compareAppACLR2Privilege(privileges[index-1], value) >= 0 {
			return AppACLPrivilegeSetR2V1{}, fmt.Errorf("APP ACL R2 privileges are not strictly ordered")
		}
		privileges = append(privileges, value)
	}
	if err := decoder.requireEOF("privilege set"); err != nil {
		return AppACLPrivilegeSetR2V1{}, err
	}
	set := AppACLPrivilegeSetR2V1{RoleBindings: bindings, Privileges: privileges}
	reencoded, err := CanonicalAppACLPrivilegeSetR2BodyV1(set)
	if err != nil {
		return AppACLPrivilegeSetR2V1{}, err
	}
	if !bytes.Equal(reencoded, body) {
		return AppACLPrivilegeSetR2V1{}, fmt.Errorf("APP ACL R2 privilege set is not byte-canonical")
	}
	return set, nil
}

func validateAppACLR2Bindings(bindings []AppACLRoleBindingR2V1) error {
	if len(bindings) != 3 {
		return fmt.Errorf("APP ACL R2 privilege set requires three bindings")
	}
	wantSubjects := [...]AppACLSubjectR2{
		AppACLSubjectCenterRuntimeR2,
		AppACLSubjectDirectMigratorR2,
		AppACLSubjectPlatformAdminR2,
	}
	seenRoles := make(map[string]struct{}, len(bindings))
	for index, binding := range bindings {
		if binding.Subject != wantSubjects[index] {
			return fmt.Errorf("APP ACL R2 binding %d has subject %d, want %d", index, binding.Subject, wantSubjects[index])
		}
		if !validAppACLR2RoleName(binding.CatalogRole) {
			return fmt.Errorf("APP ACL R2 binding %d has invalid catalog role", index)
		}
		if _, duplicate := seenRoles[binding.CatalogRole]; duplicate {
			return fmt.Errorf("APP ACL R2 bindings reuse catalog role %q", binding.CatalogRole)
		}
		seenRoles[binding.CatalogRole] = struct{}{}
	}
	return nil
}

func appACLR2PrivilegeDatabaseName(privileges []AppACLPrivilegeR2V1) (string, error) {
	var databaseName string
	for _, privilege := range privileges {
		if privilege.Subject != AppACLSubjectCenterRuntimeR2 || privilege.ObjectClass != AppACLObjectClassDatabaseR2 || privilege.Privilege != AppACLPrivilegeConnectR2 {
			continue
		}
		if databaseName != "" {
			return "", fmt.Errorf("APP ACL R2 privilege set has duplicate runtime database tuple")
		}
		databaseName = privilege.ObjectIdentity
	}
	if !validAppACLR2RoleName(databaseName) {
		return "", fmt.Errorf("APP ACL R2 privilege set has no valid runtime database tuple")
	}
	return databaseName, nil
}

func validateAppACLR2Privilege(privilege AppACLPrivilegeR2V1) error {
	if privilege.Subject < AppACLSubjectCenterRuntimeR2 || privilege.Subject > AppACLSubjectPlatformAdminR2 {
		return fmt.Errorf("unknown subject tag %d", privilege.Subject)
	}
	if privilege.GrantOption {
		return fmt.Errorf("grant option must be false")
	}
	if !validAppACLR2Text(privilege.SchemaName, 0, 63) || !validAppACLR2Text(privilege.ObjectIdentity, 1, 1024) || !validAppACLR2Text(privilege.ColumnName, 0, 63) {
		return fmt.Errorf("invalid privilege text")
	}
	switch privilege.ObjectClass {
	case AppACLObjectClassDatabaseR2:
		if privilege.SchemaName != "" || privilege.ColumnName != "" || !validAppACLR2RoleName(privilege.ObjectIdentity) || privilege.Privilege != AppACLPrivilegeConnectR2 {
			return fmt.Errorf("invalid database privilege tuple")
		}
	case AppACLObjectClassSchemaR2:
		if privilege.SchemaName != "" || privilege.ColumnName != "" || !validAppACLR2RoleName(privilege.ObjectIdentity) || privilege.Privilege != AppACLPrivilegeUsageR2 {
			return fmt.Errorf("invalid schema privilege tuple")
		}
	case AppACLObjectClassTableR2:
		if privilege.SchemaName != "public" || privilege.ColumnName != "" || !validAppACLR2RoleName(privilege.ObjectIdentity) ||
			!appACLR2OneOfPrivilege(privilege.Privilege, AppACLPrivilegeSelectR2, AppACLPrivilegeInsertR2, AppACLPrivilegeUpdateR2, AppACLPrivilegeDeleteR2) {
			return fmt.Errorf("invalid table privilege tuple")
		}
	case AppACLObjectClassViewR2:
		if privilege.SchemaName != "public" || privilege.ColumnName != "" || !validAppACLR2RoleName(privilege.ObjectIdentity) || privilege.Privilege != AppACLPrivilegeSelectR2 {
			return fmt.Errorf("invalid view privilege tuple")
		}
	case AppACLObjectClassSequenceR2:
		if privilege.SchemaName != "public" || privilege.ColumnName != "" || !validAppACLR2RoleName(privilege.ObjectIdentity) ||
			!appACLR2OneOfPrivilege(privilege.Privilege, AppACLPrivilegeUsageR2, AppACLPrivilegeSelectR2) {
			return fmt.Errorf("invalid sequence privilege tuple")
		}
	case AppACLObjectClassFunctionR2:
		if privilege.SchemaName != "" || privilege.ColumnName != "" || !validAppACLR2FunctionIdentity(privilege.ObjectIdentity) || privilege.Privilege != AppACLPrivilegeExecuteR2 {
			return fmt.Errorf("invalid function privilege tuple")
		}
	case AppACLObjectClassColumnR2:
		return fmt.Errorf("column privilege class is rejected")
	default:
		return fmt.Errorf("unknown object class tag %d", privilege.ObjectClass)
	}
	return nil
}

func appACLR2OneOfPrivilege(got AppACLPrivilegeKindR2, allowed ...AppACLPrivilegeKindR2) bool {
	for _, value := range allowed {
		if got == value {
			return true
		}
	}
	return false
}

func validAppACLR2FunctionIdentity(identity string) bool {
	if !validAppACLR2Text(identity, 1, 1024) || !strings.HasSuffix(identity, ")") {
		return false
	}
	dot := strings.IndexByte(identity, '.')
	open := strings.IndexByte(identity, '(')
	if dot < 1 || open <= dot+1 {
		return false
	}
	return validAppACLR2RoleName(identity[:dot]) && validAppACLR2RoleName(identity[dot+1:open])
}

func compareAppACLR2Privilege(left, right AppACLPrivilegeR2V1) int {
	if left.Subject != right.Subject {
		return int(left.Subject) - int(right.Subject)
	}
	if left.ObjectClass != right.ObjectClass {
		return int(left.ObjectClass) - int(right.ObjectClass)
	}
	for _, pair := range [][2]string{{left.SchemaName, right.SchemaName}, {left.ObjectIdentity, right.ObjectIdentity}, {left.ColumnName, right.ColumnName}} {
		if comparison := strings.Compare(pair[0], pair[1]); comparison != 0 {
			return comparison
		}
	}
	if left.Privilege != right.Privilege {
		return int(left.Privilege) - int(right.Privilege)
	}
	if left.GrantOption == right.GrantOption {
		return 0
	}
	if left.GrantOption {
		return 1
	}
	return -1
}

func appACLR2PrivilegeContract(databaseName string) []AppACLPrivilegeR2V1 {
	privileges := make([]AppACLPrivilegeR2V1, 0, appACLR2PrivilegeCount)
	addDatabaseAndSchema := func(subject AppACLSubjectR2) {
		privileges = append(privileges,
			AppACLPrivilegeR2V1{Subject: subject, ObjectClass: AppACLObjectClassDatabaseR2, ObjectIdentity: databaseName, Privilege: AppACLPrivilegeConnectR2},
			AppACLPrivilegeR2V1{Subject: subject, ObjectClass: AppACLObjectClassSchemaR2, ObjectIdentity: "public", Privilege: AppACLPrivilegeUsageR2},
		)
	}
	addRelations := func(subject AppACLSubjectR2, objectClass AppACLObjectClassR2, names []string, kinds ...AppACLPrivilegeKindR2) {
		for _, name := range names {
			for _, kind := range kinds {
				privileges = append(privileges, AppACLPrivilegeR2V1{
					Subject: subject, ObjectClass: objectClass, SchemaName: "public", ObjectIdentity: name, Privilege: kind,
				})
			}
		}
	}

	addDatabaseAndSchema(AppACLSubjectCenterRuntimeR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{
		"schema_migrations", "app_acl_manifest_revisions", "app_acl_manifest_head",
	}, AppACLPrivilegeSelectR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{
		"active_incidents", "asset_decision_manual_group_members", "monitoring_instance_host_sample_daily_aggregates",
		"target_probe_daily_aggregates", "monitoring_instances", "ip_quality_reports", "probe_items", "sessions",
	}, AppACLPrivilegeSelectR2, AppACLPrivilegeInsertR2, AppACLPrivilegeUpdateR2, AppACLPrivilegeDeleteR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{
		"asset_decision_manual_groups", "asset_decision_record_members", "asset_decision_records",
		"asset_decision_scenario_templates", "center_settings", "providers", "subscription_budgets",
		"subscription_exchange_rates", "subscription_monthly_budgets", "subscription_reminder_deliveries",
		"subscriptions", "targets", "users", "vps_assets", "vps_monitoring_instance_links",
	}, AppACLPrivilegeSelectR2, AppACLPrivilegeInsertR2, AppACLPrivilegeUpdateR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{
		"asset_lifecycle_action_steps", "host_samples", "monitoring_instance_heartbeats", "notification_records",
		"probe_observations", "state_change_events",
	}, AppACLPrivilegeSelectR2, AppACLPrivilegeInsertR2, AppACLPrivilegeDeleteR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{
		"asset_decision_scenario_template_members", "asset_domains", "asset_services", "experience_logs",
		"ip_histories", "ip_quality_provider_results", "ip_quality_service_unlocks",
		"monitoring_instance_command_action_audit", "price_histories", "renewal_decisions", "vps_spec_snapshots",
	}, AppACLPrivilegeSelectR2, AppACLPrivilegeInsertR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{"agent_sync_batches", "asset_lifecycle_actions"}, AppACLPrivilegeInsertR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassViewR2, []string{
		"asset_decision_records_with_counts", "ip_quality_assigned_vps_reports", "ip_quality_latest_vps_summaries",
	}, AppACLPrivilegeSelectR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassSequenceR2, []string{
		"node_heartbeats_id_seq", "host_samples_id_seq", "probe_observations_id_seq",
	}, AppACLPrivilegeUsageR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{
		"record_outbox", "record_idempotency_keys", "identity_mutation_guards", "deletion_reservations",
		"deletion_fence_leases", "object_content_leases", "client_content_leases", "content_delivery_epochs",
		"backup_epochs", "recovery_inventory_projection", "deployment_membership",
	}, AppACLPrivilegeSelectR2, AppACLPrivilegeInsertR2, AppACLPrivilegeUpdateR2, AppACLPrivilegeDeleteR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{
		"record_purge_operations", "deletion_replay_state",
	}, AppACLPrivilegeSelectR2, AppACLPrivilegeInsertR2, AppACLPrivilegeUpdateR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{
		"record_deletion_audits", "source_deletion_tombstones",
	}, AppACLPrivilegeSelectR2, AppACLPrivilegeInsertR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{
		"record_access_groups", "record_access_group_members", "record_platform_domain_identity",
		"record_platform_domain_attestations", "deployment_contract_state",
	}, AppACLPrivilegeSelectR2)
	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassSequenceR2, []string{"record_outbox_outbox_row_id_seq"}, AppACLPrivilegeUsageR2)

	addDatabaseAndSchema(AppACLSubjectPlatformAdminR2)
	addRelations(AppACLSubjectPlatformAdminR2, AppACLObjectClassTableR2, []string{
		"schema_migrations", "app_acl_manifest_revisions", "app_acl_manifest_head", "record_platform_domain_identity",
		"record_platform_domain_attestations", "backup_epochs", "recovery_inventory_projection", "deletion_replay_state",
		"deployment_membership", "deployment_contract_state",
	}, AppACLPrivilegeSelectR2)
	addRelations(AppACLSubjectPlatformAdminR2, AppACLObjectClassTableR2, []string{"deletion_replay_state"}, AppACLPrivilegeInsertR2, AppACLPrivilegeUpdateR2)

	addRelations(AppACLSubjectCenterRuntimeR2, AppACLObjectClassTableR2, []string{"app_acl_r2_bootstrap_receipt"}, AppACLPrivilegeSelectR2)
	addRelations(AppACLSubjectDirectMigratorR2, AppACLObjectClassTableR2, []string{"app_acl_r2_bootstrap_receipt"}, AppACLPrivilegeSelectR2)

	sort.Slice(privileges, func(i, j int) bool {
		return compareAppACLR2Privilege(privileges[i], privileges[j]) < 0
	})
	return privileges
}

// AppACLControlRoleR2 is the fixed control-plane role tag.
type AppACLControlRoleR2 uint8

const (
	AppACLControlRoleBootstrapSuperuserR2 AppACLControlRoleR2 = 1
	AppACLControlRoleDirectMigratorR2     AppACLControlRoleR2 = 2
	AppACLControlRoleCenterRuntimeR2      AppACLControlRoleR2 = 3
	AppACLControlRolePlatformAdminR2      AppACLControlRoleR2 = 4
	AppACLControlRolePublicR2             AppACLControlRoleR2 = 5
)

// AppACLControlObjectKindR2 identifies one transition-control object.
type AppACLControlObjectKindR2 uint8

const (
	AppACLControlObjectTableR2    AppACLControlObjectKindR2 = 1
	AppACLControlObjectFunctionR2 AppACLControlObjectKindR2 = 2
)

// AppACLControlPrivilegeR2 is the relevant control-plane privilege.
type AppACLControlPrivilegeR2 uint8

const (
	AppACLControlPrivilegeSelectR2  AppACLControlPrivilegeR2 = 1
	AppACLControlPrivilegeExecuteR2 AppACLControlPrivilegeR2 = 2
)

// AppACLDefaultACLNamespaceR2 is the fixed namespace tag in an absence
// assertion.
type AppACLDefaultACLNamespaceR2 uint8

const (
	AppACLDefaultACLNamespacePublicR2                 AppACLDefaultACLNamespaceR2 = 1
	AppACLDefaultACLNamespaceRecordPlatformInternalR2 AppACLDefaultACLNamespaceR2 = 2
)

type AppACLControlGrantR2V1 struct {
	GranteeRole AppACLControlRoleR2
	Privilege   AppACLControlPrivilegeR2
	GrantOption bool
}

type AppACLControlObjectR2V1 struct {
	Kind                           AppACLControlObjectKindR2
	Schema                         string
	Identity                       string
	OwnerRole                      AppACLControlRoleR2
	OwnerOID                       uint32
	ExplicitGrants                 []AppACLControlGrantR2V1
	EffectiveRelevantPrivilegeMask uint8
}

type AppACLControlTriggerR2V1 struct {
	TableSchema      string
	TableName        string
	TriggerName      string
	FunctionSchema   string
	FunctionIdentity string
	TableOwnerOID    uint32
	FunctionOwnerOID uint32
	Enabled          bool
}

type AppACLDefaultACLAssertionR2V1 struct {
	OwnerRole AppACLControlRoleR2
	Kind      AppACLControlObjectKindR2
	Namespace AppACLDefaultACLNamespaceR2
}

// AppACLControlACLBodyR2V1 is the decoded exact M2 transition-control ACL.
type AppACLControlACLBodyR2V1 struct {
	Objects              []AppACLControlObjectR2V1
	Triggers             []AppACLControlTriggerR2V1
	DefaultACLAssertions []AppACLDefaultACLAssertionR2V1
}

// CompileAppACLControlACLBodyR2V1 builds the exact direct-migrator-owned M2
// control surface for one nonzero direct role OID.
func CompileAppACLControlACLBodyR2V1(directMigratorOID uint32) ([]byte, error) {
	if directMigratorOID == 0 {
		return nil, fmt.Errorf("APP ACL R2 control ACL has zero direct migrator OID")
	}
	return CanonicalAppACLControlACLBodyR2V1(appACLR2ControlACLContract(directMigratorOID))
}

// CanonicalAppACLControlACLBodyR2V1 validates the entire exact M2 control ACL
// before allocating its canonical body.
func CanonicalAppACLControlACLBodyR2V1(value AppACLControlACLBodyR2V1) ([]byte, error) {
	if err := validateAppACLR2ControlACL(value); err != nil {
		return nil, err
	}

	body := make([]byte, 0, len(appACLR2ControlACLMagic)+6+512)
	body = append(body, appACLR2ControlACLMagic...)
	body = appendAppACLR2Uint16(body, appACLR2CodecVersion)
	body = appendAppACLR2Uint16(body, uint16(len(value.Objects)))
	for _, object := range value.Objects {
		body = append(body, byte(object.Kind))
		body = appendAppACLR2String16(body, object.Schema)
		body = appendAppACLR2String16(body, object.Identity)
		body = append(body, byte(object.OwnerRole))
		body = appendAppACLR2Uint32(body, object.OwnerOID)
		body = appendAppACLR2Uint16(body, uint16(len(object.ExplicitGrants)))
		for _, grant := range object.ExplicitGrants {
			body = append(body, byte(grant.GranteeRole), byte(grant.Privilege), 0)
		}
		body = append(body, object.EffectiveRelevantPrivilegeMask)
	}
	body = appendAppACLR2Uint16(body, uint16(len(value.Triggers)))
	for _, trigger := range value.Triggers {
		body = appendAppACLR2String16(body, trigger.TableSchema)
		body = appendAppACLR2String16(body, trigger.TableName)
		body = appendAppACLR2String16(body, trigger.TriggerName)
		body = appendAppACLR2String16(body, trigger.FunctionSchema)
		body = appendAppACLR2String16(body, trigger.FunctionIdentity)
		body = appendAppACLR2Uint32(body, trigger.TableOwnerOID)
		body = appendAppACLR2Uint32(body, trigger.FunctionOwnerOID)
		body = append(body, 1)
	}
	body = appendAppACLR2Uint16(body, uint16(len(value.DefaultACLAssertions)))
	for _, assertion := range value.DefaultACLAssertions {
		body = append(body, byte(assertion.OwnerRole), byte(assertion.Kind), byte(assertion.Namespace))
	}
	return body, nil
}

// ParseCanonicalAppACLControlACLBodyR2V1 accepts only the exact M2 control
// surface, with strict key ordering and EOF.
func ParseCanonicalAppACLControlACLBodyR2V1(body []byte) (AppACLControlACLBodyR2V1, error) {
	if len(body) > appACLR2MaximumBodyBytes || !bytes.HasPrefix(body, []byte(appACLR2ControlACLMagic)) {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("invalid APP ACL R2 control-ACL magic or size")
	}
	decoder := appACLR2Decoder{body: body, offset: len(appACLR2ControlACLMagic)}
	version, err := decoder.uint16("control-ACL version")
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if version != appACLR2CodecVersion {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control-ACL version is %d, want %d", version, appACLR2CodecVersion)
	}
	objectCount, err := decoder.uint16("control object count")
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if objectCount != 3 {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control ACL has %d objects, want 3", objectCount)
	}
	objects := make([]AppACLControlObjectR2V1, 0, objectCount)
	for objectIndex := range int(objectCount) {
		kind, err := decoder.uint8("control object kind")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		schema, err := decoder.string16(1, 63, "control object schema")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		identity, err := decoder.string16(1, 1024, "control object identity")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		ownerRole, err := decoder.uint8("control object owner role")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		ownerOID, err := decoder.uint32("control object owner OID")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		grantCount, err := decoder.uint16("control object grant count")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		if grantCount > 5 {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control object %d has too many grants", objectIndex)
		}
		grants := make([]AppACLControlGrantR2V1, 0, grantCount)
		for grantIndex := range int(grantCount) {
			granteeRole, err := decoder.uint8("control grant grantee")
			if err != nil {
				return AppACLControlACLBodyR2V1{}, err
			}
			privilege, err := decoder.uint8("control grant privilege")
			if err != nil {
				return AppACLControlACLBodyR2V1{}, err
			}
			grantOption, err := decoder.uint8("control grant option")
			if err != nil {
				return AppACLControlACLBodyR2V1{}, err
			}
			if grantOption != 0 {
				return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control grant option must be zero")
			}
			grant := AppACLControlGrantR2V1{GranteeRole: AppACLControlRoleR2(granteeRole), Privilege: AppACLControlPrivilegeR2(privilege)}
			if grantIndex > 0 && compareAppACLR2ControlGrant(grants[grantIndex-1], grant) >= 0 {
				return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control grants are not strictly ordered")
			}
			grants = append(grants, grant)
		}
		mask, err := decoder.uint8("effective relevant privilege mask")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		object := AppACLControlObjectR2V1{
			Kind: AppACLControlObjectKindR2(kind), Schema: schema, Identity: identity,
			OwnerRole: AppACLControlRoleR2(ownerRole), OwnerOID: ownerOID,
			ExplicitGrants: grants, EffectiveRelevantPrivilegeMask: mask,
		}
		if objectIndex > 0 && compareAppACLR2ControlObject(objects[objectIndex-1], object) >= 0 {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control objects are not strictly ordered")
		}
		objects = append(objects, object)
	}
	triggerCount, err := decoder.uint16("control trigger count")
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if triggerCount != 2 {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control ACL has %d triggers, want 2", triggerCount)
	}
	triggers := make([]AppACLControlTriggerR2V1, 0, triggerCount)
	for triggerIndex := range int(triggerCount) {
		tableSchema, err := decoder.string16(1, 63, "trigger table schema")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		tableName, err := decoder.string16(1, 63, "trigger table name")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		triggerName, err := decoder.string16(1, 63, "trigger name")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		functionSchema, err := decoder.string16(1, 63, "trigger function schema")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		functionIdentity, err := decoder.string16(1, 1024, "trigger function identity")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		tableOwnerOID, err := decoder.uint32("trigger table owner OID")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		functionOwnerOID, err := decoder.uint32("trigger function owner OID")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		enabled, err := decoder.uint8("trigger enabled")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		if enabled != 1 {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 trigger enabled byte must be one")
		}
		trigger := AppACLControlTriggerR2V1{
			TableSchema: tableSchema, TableName: tableName, TriggerName: triggerName,
			FunctionSchema: functionSchema, FunctionIdentity: functionIdentity,
			TableOwnerOID: tableOwnerOID, FunctionOwnerOID: functionOwnerOID, Enabled: true,
		}
		if triggerIndex > 0 && compareAppACLR2ControlTrigger(triggers[triggerIndex-1], trigger) >= 0 {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control triggers are not strictly ordered")
		}
		triggers = append(triggers, trigger)
	}
	assertionCount, err := decoder.uint16("default ACL assertion count")
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if assertionCount != 2 {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control ACL has %d default assertions, want 2", assertionCount)
	}
	assertions := make([]AppACLDefaultACLAssertionR2V1, 0, assertionCount)
	for assertionIndex := range int(assertionCount) {
		ownerRole, err := decoder.uint8("default ACL owner role")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		kind, err := decoder.uint8("default ACL kind")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		namespace, err := decoder.uint8("default ACL namespace")
		if err != nil {
			return AppACLControlACLBodyR2V1{}, err
		}
		assertion := AppACLDefaultACLAssertionR2V1{
			OwnerRole: AppACLControlRoleR2(ownerRole), Kind: AppACLControlObjectKindR2(kind), Namespace: AppACLDefaultACLNamespaceR2(namespace),
		}
		if assertionIndex > 0 && compareAppACLR2DefaultACLAssertion(assertions[assertionIndex-1], assertion) >= 0 {
			return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 default ACL assertions are not strictly ordered")
		}
		assertions = append(assertions, assertion)
	}
	if err := decoder.requireEOF("control ACL"); err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	value := AppACLControlACLBodyR2V1{Objects: objects, Triggers: triggers, DefaultACLAssertions: assertions}
	reencoded, err := CanonicalAppACLControlACLBodyR2V1(value)
	if err != nil {
		return AppACLControlACLBodyR2V1{}, err
	}
	if !bytes.Equal(reencoded, body) {
		return AppACLControlACLBodyR2V1{}, fmt.Errorf("APP ACL R2 control ACL is not byte-canonical")
	}
	return value, nil
}

func validateAppACLR2ControlACL(value AppACLControlACLBodyR2V1) error {
	if len(value.Objects) != 3 || len(value.Triggers) != 2 || len(value.DefaultACLAssertions) != 2 {
		return fmt.Errorf("APP ACL R2 control ACL count is %d/%d/%d, want 3/2/2", len(value.Objects), len(value.Triggers), len(value.DefaultACLAssertions))
	}
	for index, object := range value.Objects {
		if err := validateAppACLR2ControlObject(object); err != nil {
			return fmt.Errorf("validate APP ACL R2 control object %d: %w", index, err)
		}
		if index > 0 && compareAppACLR2ControlObject(value.Objects[index-1], object) >= 0 {
			return fmt.Errorf("APP ACL R2 control objects are not strictly ordered")
		}
	}
	for index, trigger := range value.Triggers {
		if err := validateAppACLR2ControlTrigger(trigger); err != nil {
			return fmt.Errorf("validate APP ACL R2 control trigger %d: %w", index, err)
		}
		if index > 0 && compareAppACLR2ControlTrigger(value.Triggers[index-1], trigger) >= 0 {
			return fmt.Errorf("APP ACL R2 control triggers are not strictly ordered")
		}
	}
	for index, assertion := range value.DefaultACLAssertions {
		if err := validateAppACLR2DefaultACLAssertion(assertion); err != nil {
			return fmt.Errorf("validate APP ACL R2 default ACL assertion %d: %w", index, err)
		}
		if index > 0 && compareAppACLR2DefaultACLAssertion(value.DefaultACLAssertions[index-1], assertion) >= 0 {
			return fmt.Errorf("APP ACL R2 default ACL assertions are not strictly ordered")
		}
	}
	directMigratorOID := value.Objects[0].OwnerOID
	if directMigratorOID == 0 {
		return fmt.Errorf("APP ACL R2 control ACL has zero direct migrator OID")
	}
	expected := appACLR2ControlACLContract(directMigratorOID)
	if !equalAppACLR2ControlACL(value, expected) {
		return fmt.Errorf("APP ACL R2 control ACL does not match the fixed M2 contract")
	}
	return nil
}

func validateAppACLR2ControlObject(object AppACLControlObjectR2V1) error {
	if !validAppACLR2RoleName(object.Schema) || !validAppACLR2Text(object.Identity, 1, 1024) || object.OwnerOID == 0 {
		return fmt.Errorf("invalid object identity or owner")
	}
	if object.OwnerRole < AppACLControlRoleBootstrapSuperuserR2 || object.OwnerRole > AppACLControlRolePlatformAdminR2 {
		return fmt.Errorf("invalid owner role %d", object.OwnerRole)
	}
	if object.EffectiveRelevantPrivilegeMask&^uint8(0x1f) != 0 {
		return fmt.Errorf("effective privilege mask uses unknown role bits")
	}
	switch object.Kind {
	case AppACLControlObjectTableR2:
		if !validAppACLR2RoleName(object.Identity) {
			return fmt.Errorf("invalid table identity")
		}
	case AppACLControlObjectFunctionR2:
		if !validAppACLR2FunctionIdentity(object.Identity) || !strings.HasPrefix(object.Identity, object.Schema+".") {
			return fmt.Errorf("invalid function identity")
		}
	default:
		return fmt.Errorf("unknown object kind %d", object.Kind)
	}
	for index, grant := range object.ExplicitGrants {
		if grant.GranteeRole < AppACLControlRoleBootstrapSuperuserR2 || grant.GranteeRole > AppACLControlRolePublicR2 || grant.GrantOption {
			return fmt.Errorf("invalid control grant")
		}
		if object.Kind == AppACLControlObjectTableR2 && grant.Privilege != AppACLControlPrivilegeSelectR2 ||
			object.Kind == AppACLControlObjectFunctionR2 && grant.Privilege != AppACLControlPrivilegeExecuteR2 {
			return fmt.Errorf("control grant privilege does not match object kind")
		}
		if index > 0 && compareAppACLR2ControlGrant(object.ExplicitGrants[index-1], grant) >= 0 {
			return fmt.Errorf("control grants are not strictly ordered")
		}
	}
	return nil
}

func validateAppACLR2ControlTrigger(trigger AppACLControlTriggerR2V1) error {
	if !validAppACLR2RoleName(trigger.TableSchema) || !validAppACLR2RoleName(trigger.TableName) || !validAppACLR2RoleName(trigger.TriggerName) ||
		!validAppACLR2RoleName(trigger.FunctionSchema) || !validAppACLR2FunctionIdentity(trigger.FunctionIdentity) ||
		!strings.HasPrefix(trigger.FunctionIdentity, trigger.FunctionSchema+".") || trigger.TableOwnerOID == 0 || trigger.FunctionOwnerOID == 0 || !trigger.Enabled {
		return fmt.Errorf("invalid control trigger")
	}
	return nil
}

func validateAppACLR2DefaultACLAssertion(assertion AppACLDefaultACLAssertionR2V1) error {
	if assertion.OwnerRole < AppACLControlRoleBootstrapSuperuserR2 || assertion.OwnerRole > AppACLControlRolePlatformAdminR2 {
		return fmt.Errorf("invalid default ACL owner role")
	}
	if assertion.Kind != AppACLControlObjectTableR2 && assertion.Kind != AppACLControlObjectFunctionR2 {
		return fmt.Errorf("invalid default ACL object kind")
	}
	if assertion.Namespace != AppACLDefaultACLNamespacePublicR2 && assertion.Namespace != AppACLDefaultACLNamespaceRecordPlatformInternalR2 {
		return fmt.Errorf("invalid default ACL namespace")
	}
	return nil
}

func compareAppACLR2ControlObject(left, right AppACLControlObjectR2V1) int {
	if left.Kind != right.Kind {
		return int(left.Kind) - int(right.Kind)
	}
	if comparison := strings.Compare(left.Schema, right.Schema); comparison != 0 {
		return comparison
	}
	return strings.Compare(left.Identity, right.Identity)
}

func compareAppACLR2ControlGrant(left, right AppACLControlGrantR2V1) int {
	if left.GranteeRole != right.GranteeRole {
		return int(left.GranteeRole) - int(right.GranteeRole)
	}
	return int(left.Privilege) - int(right.Privilege)
}

func compareAppACLR2ControlTrigger(left, right AppACLControlTriggerR2V1) int {
	for _, pair := range [][2]string{
		{left.TableSchema, right.TableSchema}, {left.TableName, right.TableName}, {left.TriggerName, right.TriggerName},
	} {
		if comparison := strings.Compare(pair[0], pair[1]); comparison != 0 {
			return comparison
		}
	}
	return 0
}

func compareAppACLR2DefaultACLAssertion(left, right AppACLDefaultACLAssertionR2V1) int {
	if left.OwnerRole != right.OwnerRole {
		return int(left.OwnerRole) - int(right.OwnerRole)
	}
	if left.Kind != right.Kind {
		return int(left.Kind) - int(right.Kind)
	}
	return int(left.Namespace) - int(right.Namespace)
}

func appACLR2ControlACLContract(directMigratorOID uint32) AppACLControlACLBodyR2V1 {
	const internalSchema = "record_platform_internal"
	const rejectIdentity = "record_platform_internal.app_acl_r2_reject_manifest_mutation()"
	return AppACLControlACLBodyR2V1{
		Objects: []AppACLControlObjectR2V1{
			{
				Kind: AppACLControlObjectTableR2, Schema: "public", Identity: "app_acl_r2_manifest_head",
				OwnerRole: AppACLControlRoleDirectMigratorR2, OwnerOID: directMigratorOID,
				ExplicitGrants: []AppACLControlGrantR2V1{
					{GranteeRole: AppACLControlRoleDirectMigratorR2, Privilege: AppACLControlPrivilegeSelectR2},
					{GranteeRole: AppACLControlRoleCenterRuntimeR2, Privilege: AppACLControlPrivilegeSelectR2},
				},
				EffectiveRelevantPrivilegeMask: 0x06,
			},
			{
				Kind: AppACLControlObjectTableR2, Schema: "public", Identity: "app_acl_r2_manifest_revisions",
				OwnerRole: AppACLControlRoleDirectMigratorR2, OwnerOID: directMigratorOID,
				ExplicitGrants: []AppACLControlGrantR2V1{
					{GranteeRole: AppACLControlRoleDirectMigratorR2, Privilege: AppACLControlPrivilegeSelectR2},
					{GranteeRole: AppACLControlRoleCenterRuntimeR2, Privilege: AppACLControlPrivilegeSelectR2},
				},
				EffectiveRelevantPrivilegeMask: 0x06,
			},
			{
				Kind: AppACLControlObjectFunctionR2, Schema: internalSchema, Identity: rejectIdentity,
				OwnerRole: AppACLControlRoleDirectMigratorR2, OwnerOID: directMigratorOID,
				ExplicitGrants:                 []AppACLControlGrantR2V1{{GranteeRole: AppACLControlRoleDirectMigratorR2, Privilege: AppACLControlPrivilegeExecuteR2}},
				EffectiveRelevantPrivilegeMask: 0x02,
			},
		},
		Triggers: []AppACLControlTriggerR2V1{
			{
				TableSchema: "public", TableName: "app_acl_r2_manifest_head", TriggerName: "app_acl_r2_manifest_head_immutable",
				FunctionSchema: internalSchema, FunctionIdentity: rejectIdentity,
				TableOwnerOID: directMigratorOID, FunctionOwnerOID: directMigratorOID, Enabled: true,
			},
			{
				TableSchema: "public", TableName: "app_acl_r2_manifest_revisions", TriggerName: "app_acl_r2_manifest_revisions_immutable",
				FunctionSchema: internalSchema, FunctionIdentity: rejectIdentity,
				TableOwnerOID: directMigratorOID, FunctionOwnerOID: directMigratorOID, Enabled: true,
			},
		},
		DefaultACLAssertions: []AppACLDefaultACLAssertionR2V1{
			{OwnerRole: AppACLControlRoleDirectMigratorR2, Kind: AppACLControlObjectTableR2, Namespace: AppACLDefaultACLNamespacePublicR2},
			{OwnerRole: AppACLControlRoleDirectMigratorR2, Kind: AppACLControlObjectFunctionR2, Namespace: AppACLDefaultACLNamespaceRecordPlatformInternalR2},
		},
	}
}

func equalAppACLR2ControlACL(left, right AppACLControlACLBodyR2V1) bool {
	if len(left.Objects) != len(right.Objects) || len(left.Triggers) != len(right.Triggers) || len(left.DefaultACLAssertions) != len(right.DefaultACLAssertions) {
		return false
	}
	for index := range left.Objects {
		leftObject, rightObject := left.Objects[index], right.Objects[index]
		if leftObject.Kind != rightObject.Kind || leftObject.Schema != rightObject.Schema || leftObject.Identity != rightObject.Identity ||
			leftObject.OwnerRole != rightObject.OwnerRole || leftObject.OwnerOID != rightObject.OwnerOID ||
			leftObject.EffectiveRelevantPrivilegeMask != rightObject.EffectiveRelevantPrivilegeMask || len(leftObject.ExplicitGrants) != len(rightObject.ExplicitGrants) {
			return false
		}
		for grantIndex := range leftObject.ExplicitGrants {
			if leftObject.ExplicitGrants[grantIndex] != rightObject.ExplicitGrants[grantIndex] {
				return false
			}
		}
	}
	for index := range left.Triggers {
		if left.Triggers[index] != right.Triggers[index] {
			return false
		}
	}
	for index := range left.DefaultACLAssertions {
		if left.DefaultACLAssertions[index] != right.DefaultACLAssertions[index] {
			return false
		}
	}
	return true
}

// AppACLManifestR2V1 is the complete M2 canonical preimage. DomainBody remains
// opaque in Slice 2: only its body32 bounds and sibling SHA-256 are checked.
type AppACLManifestR2V1 struct {
	ProtocolVersion            uint16
	ManifestRevision           uint64
	M1Revision                 uint64
	M1ManifestDigest           [32]byte
	M1SourceSetDigest          [32]byte
	M1PrivilegeSetDigest       [32]byte
	M1MigratorCatalogRole      string
	DirectMigratorName         string
	DirectMigratorOID          uint32
	R2SourceSetBody            []byte
	R2SourceSetDigest          [32]byte
	R2PrivilegeSetBody         []byte
	R2PrivilegeSetDigest       [32]byte
	DomainBody                 []byte
	DomainDigest               [32]byte
	ReceiptDigest              [32]byte
	ControlACLBody             []byte
	ControlACLDigest           [32]byte
	RecordedAtUnixMicroseconds int64
}

// CanonicalAppACLManifestR2BodyV1 validates every fixed and nested field before
// emitting the complete M2 preimage.
func CanonicalAppACLManifestR2BodyV1(manifest AppACLManifestR2V1) ([]byte, error) {
	if err := validateAppACLR2Manifest(manifest); err != nil {
		return nil, err
	}
	body := make([]byte, 0, len(appACLR2ManifestMagic)+256+len(manifest.R2SourceSetBody)+len(manifest.R2PrivilegeSetBody)+len(manifest.DomainBody)+len(manifest.ControlACLBody))
	body = append(body, appACLR2ManifestMagic...)
	body = appendAppACLR2Uint16(body, appACLR2CodecVersion)
	body = appendAppACLR2Uint16(body, manifest.ProtocolVersion)
	body = appendAppACLR2Uint64(body, manifest.ManifestRevision)
	body = appendAppACLR2Uint64(body, manifest.M1Revision)
	body = append(body, manifest.M1ManifestDigest[:]...)
	body = append(body, manifest.M1SourceSetDigest[:]...)
	body = append(body, manifest.M1PrivilegeSetDigest[:]...)
	body = appendAppACLR2String16(body, manifest.M1MigratorCatalogRole)
	body = appendAppACLR2String16(body, manifest.DirectMigratorName)
	body = appendAppACLR2Uint32(body, manifest.DirectMigratorOID)
	body = appendAppACLR2Body32(body, manifest.R2SourceSetBody)
	body = append(body, manifest.R2SourceSetDigest[:]...)
	body = appendAppACLR2Body32(body, manifest.R2PrivilegeSetBody)
	body = append(body, manifest.R2PrivilegeSetDigest[:]...)
	body = appendAppACLR2Body32(body, manifest.DomainBody)
	body = append(body, manifest.DomainDigest[:]...)
	body = append(body, manifest.ReceiptDigest[:]...)
	body = appendAppACLR2Body32(body, manifest.ControlACLBody)
	body = append(body, manifest.ControlACLDigest[:]...)
	body = appendAppACLR2Uint64(body, uint64(manifest.RecordedAtUnixMicroseconds))
	return body, nil
}

// ParseCanonicalAppACLManifestR2BodyV1 decodes only M2 magic and validates all
// nested R2 bodies, digests, fixed revisions, and strict EOF.
func ParseCanonicalAppACLManifestR2BodyV1(body []byte) (AppACLManifestR2V1, error) {
	const maximumManifestBytes = 4*appACLR2MaximumBodyBytes + 4096
	if len(body) > maximumManifestBytes || !bytes.HasPrefix(body, []byte(appACLR2ManifestMagic)) {
		return AppACLManifestR2V1{}, fmt.Errorf("invalid APP ACL M2 manifest magic or size")
	}
	decoder := appACLR2Decoder{body: body, offset: len(appACLR2ManifestMagic)}
	version, err := decoder.uint16("manifest codec version")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	if version != appACLR2CodecVersion {
		return AppACLManifestR2V1{}, fmt.Errorf("APP ACL M2 codec version is %d, want %d", version, appACLR2CodecVersion)
	}
	protocolVersion, err := decoder.uint16("manifest protocol version")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	manifestRevision, err := decoder.uint64("manifest revision")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	m1Revision, err := decoder.uint64("M1 revision")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	m1ManifestDigest, err := decoder.digest("M1 manifest digest")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	m1SourceSetDigest, err := decoder.digest("M1 source-set digest")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	m1PrivilegeSetDigest, err := decoder.digest("M1 privilege-set digest")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	m1MigratorRole, err := decoder.string16(1, 63, "M1 migrator catalog role")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	directMigratorName, err := decoder.string16(1, 63, "direct migrator name")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	directMigratorOID, err := decoder.uint32("direct migrator OID")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	r2SourceBody, err := decoder.body32(1, appACLR2MaximumBodyBytes, "R2 source-set body")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	r2SourceDigest, err := decoder.digest("R2 source-set digest")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	r2PrivilegeBody, err := decoder.body32(1, appACLR2MaximumBodyBytes, "R2 privilege-set body")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	r2PrivilegeDigest, err := decoder.digest("R2 privilege-set digest")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	domainBody, err := decoder.body32(1, appACLR2MaximumBodyBytes, "domain body")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	domainDigest, err := decoder.digest("domain digest")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	receiptDigest, err := decoder.digest("receipt digest")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	controlBody, err := decoder.body32(1, appACLR2MaximumBodyBytes, "control-ACL body")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	controlDigest, err := decoder.digest("control-ACL digest")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	recordedAt, err := decoder.uint64("recorded-at Unix microseconds")
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	if recordedAt > math.MaxInt64 {
		return AppACLManifestR2V1{}, fmt.Errorf("APP ACL M2 recorded-at value exceeds nonnegative UTC microsecond range")
	}
	if err := decoder.requireEOF("manifest"); err != nil {
		return AppACLManifestR2V1{}, err
	}
	manifest := AppACLManifestR2V1{
		ProtocolVersion: protocolVersion, ManifestRevision: manifestRevision, M1Revision: m1Revision,
		M1ManifestDigest: m1ManifestDigest, M1SourceSetDigest: m1SourceSetDigest, M1PrivilegeSetDigest: m1PrivilegeSetDigest,
		M1MigratorCatalogRole: m1MigratorRole, DirectMigratorName: directMigratorName, DirectMigratorOID: directMigratorOID,
		R2SourceSetBody: r2SourceBody, R2SourceSetDigest: r2SourceDigest,
		R2PrivilegeSetBody: r2PrivilegeBody, R2PrivilegeSetDigest: r2PrivilegeDigest,
		DomainBody: domainBody, DomainDigest: domainDigest, ReceiptDigest: receiptDigest,
		ControlACLBody: controlBody, ControlACLDigest: controlDigest,
		RecordedAtUnixMicroseconds: int64(recordedAt),
	}
	reencoded, err := CanonicalAppACLManifestR2BodyV1(manifest)
	if err != nil {
		return AppACLManifestR2V1{}, err
	}
	if !bytes.Equal(reencoded, body) {
		return AppACLManifestR2V1{}, fmt.Errorf("APP ACL M2 manifest is not byte-canonical")
	}
	return manifest, nil
}

// AppACLManifestR2DigestV1 validates and hashes the complete M2 body. The
// digest is not included in its own preimage.
func AppACLManifestR2DigestV1(body []byte) ([32]byte, error) {
	if _, err := ParseCanonicalAppACLManifestR2BodyV1(body); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(body), nil
}

func validateAppACLR2Manifest(manifest AppACLManifestR2V1) error {
	if manifest.ProtocolVersion != 2 || manifest.ManifestRevision != 2 || manifest.M1Revision != 1 {
		return fmt.Errorf("APP ACL M2 fixed versions are %d/%d/%d, want 2/2/1", manifest.ProtocolVersion, manifest.ManifestRevision, manifest.M1Revision)
	}
	if !validAppACLR2RoleName(manifest.M1MigratorCatalogRole) || !validAppACLR2RoleName(manifest.DirectMigratorName) {
		return fmt.Errorf("APP ACL M2 has invalid migrator role name")
	}
	if manifest.M1MigratorCatalogRole != manifest.DirectMigratorName {
		return fmt.Errorf("APP ACL M2 migrator role names differ")
	}
	if manifest.DirectMigratorOID == 0 {
		return fmt.Errorf("APP ACL M2 direct migrator OID is zero")
	}
	if manifest.RecordedAtUnixMicroseconds < 0 {
		return fmt.Errorf("APP ACL M2 recorded-at Unix microseconds is negative")
	}
	for _, nested := range []struct {
		name   string
		body   []byte
		digest [32]byte
	}{
		{name: "R2 source set", body: manifest.R2SourceSetBody, digest: manifest.R2SourceSetDigest},
		{name: "R2 privilege set", body: manifest.R2PrivilegeSetBody, digest: manifest.R2PrivilegeSetDigest},
		{name: "domain", body: manifest.DomainBody, digest: manifest.DomainDigest},
		{name: "control ACL", body: manifest.ControlACLBody, digest: manifest.ControlACLDigest},
	} {
		if len(nested.body) < 1 || len(nested.body) > appACLR2MaximumBodyBytes {
			return fmt.Errorf("APP ACL M2 %s body size is outside bounds", nested.name)
		}
		if sha256.Sum256(nested.body) != nested.digest {
			return fmt.Errorf("APP ACL M2 %s sibling digest does not match", nested.name)
		}
	}
	if _, err := ParseCanonicalAppACLSourceSetR2BodyV1(manifest.R2SourceSetBody); err != nil {
		return fmt.Errorf("parse APP ACL M2 R2 source set: %w", err)
	}
	privilegeSet, err := ParseCanonicalAppACLPrivilegeSetR2BodyV1(manifest.R2PrivilegeSetBody)
	if err != nil {
		return fmt.Errorf("parse APP ACL M2 R2 privilege set: %w", err)
	}
	if len(privilegeSet.RoleBindings) != 3 || privilegeSet.RoleBindings[1].CatalogRole != manifest.DirectMigratorName {
		return fmt.Errorf("APP ACL M2 direct migrator binding does not match manifest")
	}
	if _, err := ParseCanonicalAppACLControlACLBodyR2V1(manifest.ControlACLBody); err != nil {
		return fmt.Errorf("parse APP ACL M2 control ACL: %w", err)
	}
	expectedControl, err := CompileAppACLControlACLBodyR2V1(manifest.DirectMigratorOID)
	if err != nil {
		return err
	}
	if !bytes.Equal(expectedControl, manifest.ControlACLBody) {
		return fmt.Errorf("APP ACL M2 control ACL owner OID does not match direct migrator")
	}
	return nil
}
