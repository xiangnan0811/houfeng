package migrate

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const appACLR2PrivilegeMagicVector = "HOUFENG-APP-ACL-R2-PRIVILEGE-SET-V1"
const appACLR2ControlACLMagicVector = "HOUFENG-APP-ACL-R2-CONTROL-ACL-V1"
const appACLR2ManifestMagicVector = "HOUFENG-APP-ACL-MANIFEST-R2-V1"

type testR2Binding struct {
	subject byte
	role    string
}

type testR2PrivilegeTuple struct {
	subject     byte
	objectClass byte
	schema      string
	identity    string
	column      string
	privilege   byte
	grantOption byte
}

func TestAppACLR2PrivilegeCanonicalVectorRoundTrips(t *testing.T) {
	bindings := appACLR2PrivilegeVectorBindings()
	tuples := appACLR2PrivilegeVectorTuples("houfeng")
	if len(tuples) != 206 {
		t.Fatalf("independent privilege vector tuple count = %d, want 206", len(tuples))
	}
	wantBody := rawAppACLR2PrivilegeBody(bindings, tuples, 3, 206)

	body, err := CompileAppACLPrivilegeSetR2V1("houfeng", productionR2Bindings(bindings))
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR2V1() error = %v", err)
	}
	if !bytes.Equal(body, wantBody) {
		t.Fatalf("CompileAppACLPrivilegeSetR2V1() differs from independent 206-tuple vector\n got: %x\nwant: %x", body, wantBody)
	}

	set, err := ParseCanonicalAppACLPrivilegeSetR2BodyV1(wantBody)
	if err != nil {
		t.Fatalf("ParseCanonicalAppACLPrivilegeSetR2BodyV1() error = %v", err)
	}
	if len(set.RoleBindings) != 3 || len(set.Privileges) != 206 {
		t.Fatalf("parsed privilege set has %d bindings/%d tuples, want 3/206", len(set.RoleBindings), len(set.Privileges))
	}
	for index, want := range bindings {
		got := set.RoleBindings[index]
		if byte(got.Subject) != want.subject || got.CatalogRole != want.role {
			t.Fatalf("parsed binding %d = %#v, want tag %d role %q", index, got, want.subject, want.role)
		}
	}

	receiptRows := 0
	for _, tuple := range set.Privileges {
		if tuple.ObjectClass == AppACLObjectClassTableR2 && tuple.SchemaName == "public" && tuple.ObjectIdentity == "app_acl_r2_bootstrap_receipt" {
			receiptRows++
			if (tuple.Subject != AppACLSubjectCenterRuntimeR2 && tuple.Subject != AppACLSubjectDirectMigratorR2) ||
				tuple.ColumnName != "" || tuple.Privilege != AppACLPrivilegeSelectR2 || tuple.GrantOption {
				t.Fatalf("receipt privilege tuple = %#v, want exact runtime/direct SELECT", tuple)
			}
		}
	}
	if receiptRows != 2 {
		t.Fatalf("receipt privilege tuple count = %d, want exactly 2", receiptRows)
	}

	reencoded, err := CanonicalAppACLPrivilegeSetR2BodyV1(set)
	if err != nil {
		t.Fatalf("CanonicalAppACLPrivilegeSetR2BodyV1(parsed) error = %v", err)
	}
	if !bytes.Equal(reencoded, wantBody) {
		t.Fatalf("privilege re-encoding = %x, want independent vector %x", reencoded, wantBody)
	}
}

func TestAppACLR2PrivilegeParserRejectsMalformedAndNonCanonicalBodies(t *testing.T) {
	bindings := appACLR2PrivilegeVectorBindings()
	tuples := appACLR2PrivilegeVectorTuples("houfeng")
	valid := rawAppACLR2PrivilegeBody(bindings, tuples, 3, 206)

	twoBindings := rawAppACLR2PrivilegeBody(bindings, tuples, 2, 206)
	fourBindings := rawAppACLR2PrivilegeBody(append(bindings, testR2Binding{subject: 4, role: "unexpected"}), tuples, 4, 206)
	wrongRoleMap := append([]testR2Binding(nil), bindings...)
	wrongRoleMap[1].role = wrongRoleMap[0].role
	unknownRoleMap := append([]testR2Binding(nil), bindings...)
	unknownRoleMap[2].subject = 4
	reorderedTuples := append([]testR2PrivilegeTuple(nil), tuples...)
	reorderedTuples[0], reorderedTuples[1] = reorderedTuples[1], reorderedTuples[0]
	duplicateTuples := append([]testR2PrivilegeTuple(nil), tuples...)
	duplicateTuples[1] = duplicateTuples[0]
	nonzeroGrantOption := append([]testR2PrivilegeTuple(nil), tuples...)
	nonzeroGrantOption[len(nonzeroGrantOption)-1].grantOption = 1
	unknownClass := append([]testR2PrivilegeTuple(nil), tuples...)
	unknownClass[0].objectClass = 7
	unknownPrivilege := append([]testR2PrivilegeTuple(nil), tuples...)
	unknownPrivilege[0].privilege = 8
	substitutedMembership := append([]testR2PrivilegeTuple(nil), tuples...)
	substitutedMembership[0].identity = "different_database"

	malformedRoleLength := append([]byte(nil), valid...)
	roleLengthOffset := len(appACLR2PrivilegeMagicVector) + 2 + 2 + 1
	binary.BigEndian.PutUint16(malformedRoleLength[roleLengthOffset:roleLengthOffset+2], 0xffff)
	r1Magic := append([]byte("HOUFENG-APP-PRIVILEGE-SET-V1"), valid[len(appACLR2PrivilegeMagicVector):]...)

	tests := []struct {
		name string
		body []byte
	}{
		{name: "r1 magic", body: r1Magic},
		{name: "bad version", body: replaceTestUint16(valid, len(appACLR2PrivilegeMagicVector), 2)},
		{name: "two bindings", body: twoBindings},
		{name: "four bindings", body: fourBindings},
		{name: "duplicate role map", body: rawAppACLR2PrivilegeBody(wrongRoleMap, tuples, 3, 206)},
		{name: "unknown role tag", body: rawAppACLR2PrivilegeBody(unknownRoleMap, tuples, 3, 206)},
		{name: "205 tuples", body: rawAppACLR2PrivilegeBody(bindings, tuples[:205], 3, 205)},
		{name: "207 tuples", body: rawAppACLR2PrivilegeBody(bindings, append(tuples, tuples[len(tuples)-1]), 3, 207)},
		{name: "reordered tuples", body: rawAppACLR2PrivilegeBody(bindings, reorderedTuples, 3, 206)},
		{name: "duplicate tuple", body: rawAppACLR2PrivilegeBody(bindings, duplicateTuples, 3, 206)},
		{name: "nonzero grant option", body: rawAppACLR2PrivilegeBody(bindings, nonzeroGrantOption, 3, 206)},
		{name: "column class", body: rawAppACLR2PrivilegeBody(bindings, unknownClass, 3, 206)},
		{name: "unknown privilege", body: rawAppACLR2PrivilegeBody(bindings, unknownPrivilege, 3, 206)},
		{name: "membership substitution", body: rawAppACLR2PrivilegeBody(bindings, substitutedMembership, 3, 206)},
		{name: "malformed length", body: malformedRoleLength},
		{name: "truncated", body: valid[:len(valid)-1]},
		{name: "trailing byte", body: append(append([]byte(nil), valid...), 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseCanonicalAppACLPrivilegeSetR2BodyV1(tt.body); err == nil {
				t.Fatal("ParseCanonicalAppACLPrivilegeSetR2BodyV1() error = nil, want rejection")
			}
		})
	}
}

func TestAppACLR2PrivilegeCompilerRejectsWrongBindings(t *testing.T) {
	valid := productionR2Bindings(appACLR2PrivilegeVectorBindings())
	tests := []struct {
		name     string
		database string
		bindings []AppACLRoleBindingR2V1
	}{
		{name: "invalid database", database: "houfeng-prod", bindings: valid},
		{name: "two bindings", database: "houfeng", bindings: valid[:2]},
		{name: "four bindings", database: "houfeng", bindings: append(append([]AppACLRoleBindingR2V1(nil), valid...), AppACLRoleBindingR2V1{Subject: 4, CatalogRole: "unexpected"})},
		{name: "duplicate role", database: "houfeng", bindings: []AppACLRoleBindingR2V1{
			{Subject: AppACLSubjectCenterRuntimeR2, CatalogRole: "shared"},
			{Subject: AppACLSubjectDirectMigratorR2, CatalogRole: "shared"},
			{Subject: AppACLSubjectPlatformAdminR2, CatalogRole: "houfeng_platform_admin"},
		}},
		{name: "noncanonical role", database: "houfeng", bindings: []AppACLRoleBindingR2V1{
			{Subject: AppACLSubjectCenterRuntimeR2, CatalogRole: "houfeng_center_runtime"},
			{Subject: AppACLSubjectDirectMigratorR2, CatalogRole: "HoufengMigrator"},
			{Subject: AppACLSubjectPlatformAdminR2, CatalogRole: "houfeng_platform_admin"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if body, err := CompileAppACLPrivilegeSetR2V1(tt.database, tt.bindings); err == nil || body != nil {
				t.Fatalf("CompileAppACLPrivilegeSetR2V1() = %x, %v; want nil and rejection", body, err)
			}
		})
	}
}

func appACLR2PrivilegeVectorBindings() []testR2Binding {
	return []testR2Binding{
		{subject: 1, role: "houfeng_center_runtime"},
		{subject: 2, role: "houfeng_direct_migrator"},
		{subject: 3, role: "houfeng_platform_admin"},
	}
}

func appACLR2PrivilegeVectorTuples(databaseName string) []testR2PrivilegeTuple {
	tuples := make([]testR2PrivilegeTuple, 0, 206)
	addDatabaseAndSchema := func(subject byte) {
		tuples = append(tuples,
			testR2PrivilegeTuple{subject: subject, objectClass: 1, identity: databaseName, privilege: 1},
			testR2PrivilegeTuple{subject: subject, objectClass: 2, identity: "public", privilege: 2},
		)
	}
	addRelations := func(subject, objectClass byte, names []string, privileges ...byte) {
		for _, name := range names {
			for _, privilege := range privileges {
				tuples = append(tuples, testR2PrivilegeTuple{
					subject:     subject,
					objectClass: objectClass,
					schema:      "public",
					identity:    name,
					privilege:   privilege,
				})
			}
		}
	}

	addDatabaseAndSchema(1)
	addRelations(1, 3, []string{
		"schema_migrations", "app_acl_manifest_revisions", "app_acl_manifest_head",
	}, 3)
	addRelations(1, 3, []string{
		"active_incidents", "asset_decision_manual_group_members", "monitoring_instance_host_sample_daily_aggregates",
		"target_probe_daily_aggregates", "monitoring_instances", "ip_quality_reports", "probe_items", "sessions",
	}, 3, 4, 5, 6)
	addRelations(1, 3, []string{
		"asset_decision_manual_groups", "asset_decision_record_members", "asset_decision_records",
		"asset_decision_scenario_templates", "center_settings", "providers", "subscription_budgets",
		"subscription_exchange_rates", "subscription_monthly_budgets", "subscription_reminder_deliveries",
		"subscriptions", "targets", "users", "vps_assets", "vps_monitoring_instance_links",
	}, 3, 4, 5)
	addRelations(1, 3, []string{
		"asset_lifecycle_action_steps", "host_samples", "monitoring_instance_heartbeats", "notification_records",
		"probe_observations", "state_change_events",
	}, 3, 4, 6)
	addRelations(1, 3, []string{
		"asset_decision_scenario_template_members", "asset_domains", "asset_services", "experience_logs",
		"ip_histories", "ip_quality_provider_results", "ip_quality_service_unlocks",
		"monitoring_instance_command_action_audit", "price_histories", "renewal_decisions", "vps_spec_snapshots",
	}, 3, 4)
	addRelations(1, 3, []string{"agent_sync_batches", "asset_lifecycle_actions"}, 4)
	addRelations(1, 4, []string{
		"asset_decision_records_with_counts", "ip_quality_assigned_vps_reports", "ip_quality_latest_vps_summaries",
	}, 3)
	addRelations(1, 5, []string{"node_heartbeats_id_seq", "host_samples_id_seq", "probe_observations_id_seq"}, 2)
	addRelations(1, 3, []string{
		"record_outbox", "record_idempotency_keys", "identity_mutation_guards", "deletion_reservations",
		"deletion_fence_leases", "object_content_leases", "client_content_leases", "content_delivery_epochs",
		"backup_epochs", "recovery_inventory_projection", "deployment_membership",
	}, 3, 4, 5, 6)
	addRelations(1, 3, []string{"record_purge_operations", "deletion_replay_state"}, 3, 4, 5)
	addRelations(1, 3, []string{"record_deletion_audits", "source_deletion_tombstones"}, 3, 4)
	addRelations(1, 3, []string{
		"record_access_groups", "record_access_group_members", "record_platform_domain_identity",
		"record_platform_domain_attestations", "deployment_contract_state",
	}, 3)
	addRelations(1, 5, []string{"record_outbox_outbox_row_id_seq"}, 2)

	addDatabaseAndSchema(3)
	addRelations(3, 3, []string{
		"schema_migrations", "app_acl_manifest_revisions", "app_acl_manifest_head", "record_platform_domain_identity",
		"record_platform_domain_attestations", "backup_epochs", "recovery_inventory_projection", "deletion_replay_state",
		"deployment_membership", "deployment_contract_state",
	}, 3)
	addRelations(3, 3, []string{"deletion_replay_state"}, 4, 5)

	addRelations(1, 3, []string{"app_acl_r2_bootstrap_receipt"}, 3)
	addRelations(2, 3, []string{"app_acl_r2_bootstrap_receipt"}, 3)

	sort.Slice(tuples, func(i, j int) bool {
		return compareTestR2PrivilegeTuple(tuples[i], tuples[j]) < 0
	})
	return tuples
}

func compareTestR2PrivilegeTuple(left, right testR2PrivilegeTuple) int {
	if left.subject != right.subject {
		return int(left.subject) - int(right.subject)
	}
	if left.objectClass != right.objectClass {
		return int(left.objectClass) - int(right.objectClass)
	}
	for _, pair := range [][2]string{{left.schema, right.schema}, {left.identity, right.identity}, {left.column, right.column}} {
		if comparison := strings.Compare(pair[0], pair[1]); comparison != 0 {
			return comparison
		}
	}
	if left.privilege != right.privilege {
		return int(left.privilege) - int(right.privilege)
	}
	return int(left.grantOption) - int(right.grantOption)
}

func productionR2Bindings(bindings []testR2Binding) []AppACLRoleBindingR2V1 {
	got := make([]AppACLRoleBindingR2V1, 0, len(bindings))
	for _, binding := range bindings {
		got = append(got, AppACLRoleBindingR2V1{Subject: AppACLSubjectR2(binding.subject), CatalogRole: binding.role})
	}
	return got
}

func rawAppACLR2PrivilegeBody(bindings []testR2Binding, tuples []testR2PrivilegeTuple, bindingCount, tupleCount uint16) []byte {
	body := append([]byte(nil), appACLR2PrivilegeMagicVector...)
	body = appendTestUint16(body, 1)
	body = appendTestUint16(body, bindingCount)
	for _, binding := range bindings {
		body = append(body, binding.subject)
		body = appendTestString16(body, binding.role)
	}
	body = appendTestUint16(body, tupleCount)
	for _, tuple := range tuples {
		body = append(body, tuple.subject, tuple.objectClass)
		body = appendTestString16(body, tuple.schema)
		body = appendTestString16(body, tuple.identity)
		body = appendTestString16(body, tuple.column)
		body = append(body, tuple.privilege, tuple.grantOption)
	}
	return body
}

func replaceTestUint16(body []byte, offset int, value uint16) []byte {
	got := append([]byte(nil), body...)
	binary.BigEndian.PutUint16(got[offset:offset+2], value)
	return got
}

type testR2ControlGrant struct {
	grantee     byte
	privilege   byte
	grantOption byte
}

type testR2ControlObject struct {
	kind          byte
	schema        string
	identity      string
	ownerRole     byte
	ownerOID      uint32
	grants        []testR2ControlGrant
	effectiveMask byte
}

type testR2ControlTrigger struct {
	tableSchema      string
	tableName        string
	triggerName      string
	functionSchema   string
	functionIdentity string
	tableOwnerOID    uint32
	functionOwnerOID uint32
	enabled          byte
}

type testR2DefaultACLAssertion struct {
	ownerRole byte
	kind      byte
	namespace byte
}

type testR2ControlACL struct {
	objects    []testR2ControlObject
	triggers   []testR2ControlTrigger
	assertions []testR2DefaultACLAssertion
}

func TestAppACLR2ManifestControlACLCanonicalVectorRoundTrips(t *testing.T) {
	want := appACLR2ControlACLVector(4242)
	wantBody := rawAppACLR2ControlACLBody(want, 3, 2, 2)

	body, err := CompileAppACLControlACLBodyR2V1(4242)
	if err != nil {
		t.Fatalf("CompileAppACLControlACLBodyR2V1() error = %v", err)
	}
	if !bytes.Equal(body, wantBody) {
		t.Fatalf("CompileAppACLControlACLBodyR2V1() differs from independent vector\n got: %x\nwant: %x", body, wantBody)
	}
	parsed, err := ParseCanonicalAppACLControlACLBodyR2V1(wantBody)
	if err != nil {
		t.Fatalf("ParseCanonicalAppACLControlACLBodyR2V1() error = %v", err)
	}
	if len(parsed.Objects) != 3 || len(parsed.Triggers) != 2 || len(parsed.DefaultACLAssertions) != 2 {
		t.Fatalf("parsed control ACL count = %d/%d/%d, want 3/2/2", len(parsed.Objects), len(parsed.Triggers), len(parsed.DefaultACLAssertions))
	}
	if parsed.Objects[0].Identity != "app_acl_r2_manifest_head" || parsed.Objects[1].Identity != "app_acl_r2_manifest_revisions" {
		t.Fatalf("parsed control objects = %#v, want raw-byte key order", parsed.Objects)
	}
	for _, object := range parsed.Objects[:2] {
		wantGrants := []AppACLControlGrantR2V1{
			{GranteeRole: AppACLControlRoleDirectMigratorR2, Privilege: AppACLControlPrivilegeSelectR2},
			{GranteeRole: AppACLControlRoleCenterRuntimeR2, Privilege: AppACLControlPrivilegeSelectR2},
		}
		if !reflect.DeepEqual(object.ExplicitGrants, wantGrants) {
			t.Fatalf("M2 table %q grants = %#v, want direct-migrator self SELECT plus center-runtime SELECT", object.Identity, object.ExplicitGrants)
		}
	}
	wantHelperGrants := []AppACLControlGrantR2V1{{
		GranteeRole: AppACLControlRoleDirectMigratorR2,
		Privilege:   AppACLControlPrivilegeExecuteR2,
	}}
	if !reflect.DeepEqual(parsed.Objects[2].ExplicitGrants, wantHelperGrants) {
		t.Fatalf("M2 helper grants = %#v, want direct-migrator self EXECUTE", parsed.Objects[2].ExplicitGrants)
	}
	reencoded, err := CanonicalAppACLControlACLBodyR2V1(parsed)
	if err != nil {
		t.Fatalf("CanonicalAppACLControlACLBodyR2V1(parsed) error = %v", err)
	}
	if !bytes.Equal(reencoded, wantBody) {
		t.Fatalf("control ACL re-encoding = %x, want independent vector %x", reencoded, wantBody)
	}
}

func TestAppACLR2ManifestControlACLRejectsMalformedAndNonCanonicalBodies(t *testing.T) {
	validVector := appACLR2ControlACLVector(4242)
	valid := rawAppACLR2ControlACLBody(validVector, 3, 2, 2)

	reorderedObjects := cloneTestR2ControlACL(validVector)
	reorderedObjects.objects[0], reorderedObjects.objects[1] = reorderedObjects.objects[1], reorderedObjects.objects[0]
	duplicateObjects := cloneTestR2ControlACL(validVector)
	duplicateObjects.objects[1] = duplicateObjects.objects[0]
	reversedGrants := cloneTestR2ControlACL(validVector)
	reversedGrants.objects[0].grants = []testR2ControlGrant{{grantee: 3, privilege: 1}, {grantee: 2, privilege: 1}}
	nonzeroGrantOption := cloneTestR2ControlACL(validVector)
	nonzeroGrantOption.objects[0].grants[0].grantOption = 1
	unknownOwner := cloneTestR2ControlACL(validVector)
	unknownOwner.objects[0].ownerRole = 5
	zeroOwnerOID := cloneTestR2ControlACL(validVector)
	zeroOwnerOID.objects[0].ownerOID = 0
	wrongMask := cloneTestR2ControlACL(validVector)
	wrongMask.objects[0].effectiveMask = 0x1f
	disabledTrigger := cloneTestR2ControlACL(validVector)
	disabledTrigger.triggers[0].enabled = 0
	reorderedTriggers := cloneTestR2ControlACL(validVector)
	reorderedTriggers.triggers[0], reorderedTriggers.triggers[1] = reorderedTriggers.triggers[1], reorderedTriggers.triggers[0]
	duplicateAssertions := cloneTestR2ControlACL(validVector)
	duplicateAssertions.assertions[1] = duplicateAssertions.assertions[0]
	unknownNamespace := cloneTestR2ControlACL(validVector)
	unknownNamespace.assertions[1].namespace = 3

	malformedStringLength := append([]byte(nil), valid...)
	firstSchemaLengthOffset := len(appACLR2ControlACLMagicVector) + 2 + 2 + 1
	binary.BigEndian.PutUint16(malformedStringLength[firstSchemaLengthOffset:firstSchemaLengthOffset+2], 0xffff)
	l2Magic := append([]byte("HOUFENG-APP-ACL-R2-L2-ACL-V1"), valid[len(appACLR2ControlACLMagicVector):]...)

	tests := []struct {
		name string
		body []byte
	}{
		{name: "l2 magic", body: l2Magic},
		{name: "bad version", body: replaceTestUint16(valid, len(appACLR2ControlACLMagicVector), 2)},
		{name: "two objects", body: rawAppACLR2ControlACLBody(validVector, 2, 2, 2)},
		{name: "four objects", body: rawAppACLR2ControlACLBody(validVector, 4, 2, 2)},
		{name: "reordered objects", body: rawAppACLR2ControlACLBody(reorderedObjects, 3, 2, 2)},
		{name: "duplicate object", body: rawAppACLR2ControlACLBody(duplicateObjects, 3, 2, 2)},
		{name: "reversed grants", body: rawAppACLR2ControlACLBody(reversedGrants, 3, 2, 2)},
		{name: "nonzero grant option", body: rawAppACLR2ControlACLBody(nonzeroGrantOption, 3, 2, 2)},
		{name: "unknown owner role", body: rawAppACLR2ControlACLBody(unknownOwner, 3, 2, 2)},
		{name: "zero owner oid", body: rawAppACLR2ControlACLBody(zeroOwnerOID, 3, 2, 2)},
		{name: "wrong effective mask", body: rawAppACLR2ControlACLBody(wrongMask, 3, 2, 2)},
		{name: "one trigger", body: rawAppACLR2ControlACLBody(validVector, 3, 1, 2)},
		{name: "disabled trigger", body: rawAppACLR2ControlACLBody(disabledTrigger, 3, 2, 2)},
		{name: "reordered triggers", body: rawAppACLR2ControlACLBody(reorderedTriggers, 3, 2, 2)},
		{name: "one default assertion", body: rawAppACLR2ControlACLBody(validVector, 3, 2, 1)},
		{name: "duplicate default assertion", body: rawAppACLR2ControlACLBody(duplicateAssertions, 3, 2, 2)},
		{name: "unknown namespace", body: rawAppACLR2ControlACLBody(unknownNamespace, 3, 2, 2)},
		{name: "malformed string length", body: malformedStringLength},
		{name: "truncated", body: valid[:len(valid)-1]},
		{name: "trailing byte", body: append(append([]byte(nil), valid...), 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseCanonicalAppACLControlACLBodyR2V1(tt.body); err == nil {
				t.Fatal("ParseCanonicalAppACLControlACLBodyR2V1() error = nil, want rejection")
			}
		})
	}
	if body, err := CompileAppACLControlACLBodyR2V1(0); err == nil || body != nil {
		t.Fatalf("CompileAppACLControlACLBodyR2V1(0) = %x, %v; want nil and rejection", body, err)
	}
}

func appACLR2ControlACLVector(directMigratorOID uint32) testR2ControlACL {
	const internalSchema = "record_platform_internal"
	const rejectIdentity = "record_platform_internal.app_acl_r2_reject_manifest_mutation()"
	return testR2ControlACL{
		objects: []testR2ControlObject{
			{
				kind: 1, schema: "public", identity: "app_acl_r2_manifest_head", ownerRole: 2, ownerOID: directMigratorOID,
				grants: []testR2ControlGrant{{grantee: 2, privilege: 1}, {grantee: 3, privilege: 1}}, effectiveMask: 0x06,
			},
			{
				kind: 1, schema: "public", identity: "app_acl_r2_manifest_revisions", ownerRole: 2, ownerOID: directMigratorOID,
				grants: []testR2ControlGrant{{grantee: 2, privilege: 1}, {grantee: 3, privilege: 1}}, effectiveMask: 0x06,
			},
			{
				kind: 2, schema: internalSchema, identity: rejectIdentity, ownerRole: 2, ownerOID: directMigratorOID,
				grants: []testR2ControlGrant{{grantee: 2, privilege: 2}}, effectiveMask: 0x02,
			},
		},
		triggers: []testR2ControlTrigger{
			{
				tableSchema: "public", tableName: "app_acl_r2_manifest_head", triggerName: "app_acl_r2_manifest_head_immutable",
				functionSchema: internalSchema, functionIdentity: rejectIdentity, tableOwnerOID: directMigratorOID,
				functionOwnerOID: directMigratorOID, enabled: 1,
			},
			{
				tableSchema: "public", tableName: "app_acl_r2_manifest_revisions", triggerName: "app_acl_r2_manifest_revisions_immutable",
				functionSchema: internalSchema, functionIdentity: rejectIdentity, tableOwnerOID: directMigratorOID,
				functionOwnerOID: directMigratorOID, enabled: 1,
			},
		},
		assertions: []testR2DefaultACLAssertion{
			{ownerRole: 2, kind: 1, namespace: 1},
			{ownerRole: 2, kind: 2, namespace: 2},
		},
	}
}

func cloneTestR2ControlACL(value testR2ControlACL) testR2ControlACL {
	clone := testR2ControlACL{
		objects:    append([]testR2ControlObject(nil), value.objects...),
		triggers:   append([]testR2ControlTrigger(nil), value.triggers...),
		assertions: append([]testR2DefaultACLAssertion(nil), value.assertions...),
	}
	for index := range clone.objects {
		clone.objects[index].grants = append([]testR2ControlGrant(nil), value.objects[index].grants...)
	}
	return clone
}

func rawAppACLR2ControlACLBody(value testR2ControlACL, objectCount, triggerCount, assertionCount uint16) []byte {
	body := append([]byte(nil), appACLR2ControlACLMagicVector...)
	body = appendTestUint16(body, 1)
	body = appendTestUint16(body, objectCount)
	for _, object := range value.objects {
		body = append(body, object.kind)
		body = appendTestString16(body, object.schema)
		body = appendTestString16(body, object.identity)
		body = append(body, object.ownerRole)
		body = appendTestUint32(body, object.ownerOID)
		body = appendTestUint16(body, uint16(len(object.grants)))
		for _, grant := range object.grants {
			body = append(body, grant.grantee, grant.privilege, grant.grantOption)
		}
		body = append(body, object.effectiveMask)
	}
	body = appendTestUint16(body, triggerCount)
	for _, trigger := range value.triggers {
		body = appendTestString16(body, trigger.tableSchema)
		body = appendTestString16(body, trigger.tableName)
		body = appendTestString16(body, trigger.triggerName)
		body = appendTestString16(body, trigger.functionSchema)
		body = appendTestString16(body, trigger.functionIdentity)
		body = appendTestUint32(body, trigger.tableOwnerOID)
		body = appendTestUint32(body, trigger.functionOwnerOID)
		body = append(body, trigger.enabled)
	}
	body = appendTestUint16(body, assertionCount)
	for _, assertion := range value.assertions {
		body = append(body, assertion.ownerRole, assertion.kind, assertion.namespace)
	}
	return body
}

type testR2ManifestVector struct {
	protocolVersion            uint16
	manifestRevision           uint64
	m1Revision                 uint64
	m1ManifestDigest           [32]byte
	m1SourceSetDigest          [32]byte
	m1PrivilegeSetDigest       [32]byte
	m1MigratorCatalogRole      string
	directMigratorName         string
	directMigratorOID          uint32
	r2SourceSetBody            []byte
	r2SourceSetDigest          [32]byte
	r2PrivilegeSetBody         []byte
	r2PrivilegeSetDigest       [32]byte
	domainBody                 []byte
	domainDigest               [32]byte
	receiptDigest              [32]byte
	controlACLBody             []byte
	controlACLDigest           [32]byte
	recordedAtUnixMicroseconds int64
}

func TestAppACLR2ManifestCanonicalVectorRoundTripsWithOpaqueDomain(t *testing.T) {
	want := appACLR2ManifestVector(t)
	wantBody := rawAppACLR2ManifestBody(want)
	wantDigest := sha256.Sum256(wantBody)

	body, err := CanonicalAppACLManifestR2BodyV1(productionR2Manifest(want))
	if err != nil {
		t.Fatalf("CanonicalAppACLManifestR2BodyV1() error = %v", err)
	}
	if !bytes.Equal(body, wantBody) {
		t.Fatalf("CanonicalAppACLManifestR2BodyV1() differs from independent M2 vector\n got: %x\nwant: %x", body, wantBody)
	}
	gotDigest, err := AppACLManifestR2DigestV1(wantBody)
	if err != nil {
		t.Fatalf("AppACLManifestR2DigestV1() error = %v", err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("AppACLManifestR2DigestV1() = %x, want SHA-256 %x", gotDigest, wantDigest)
	}

	parsed, err := ParseCanonicalAppACLManifestR2BodyV1(wantBody)
	if err != nil {
		t.Fatalf("ParseCanonicalAppACLManifestR2BodyV1() error = %v", err)
	}
	if parsed.ProtocolVersion != 2 || parsed.ManifestRevision != 2 || parsed.M1Revision != 1 {
		t.Fatalf("parsed manifest fixed revisions = %d/%d/%d, want 2/2/1", parsed.ProtocolVersion, parsed.ManifestRevision, parsed.M1Revision)
	}
	if !bytes.Equal(parsed.DomainBody, []byte{0xff, 0x00, 0xfe, 0x01}) {
		t.Fatalf("parsed opaque domain body = %x, want byte-exact opaque fixture", parsed.DomainBody)
	}
	if parsed.DirectMigratorName != want.directMigratorName || parsed.M1MigratorCatalogRole != want.m1MigratorCatalogRole || parsed.DirectMigratorOID != want.directMigratorOID {
		t.Fatalf("parsed migrator identity = %q/%q/%d, want %q/%q/%d", parsed.M1MigratorCatalogRole, parsed.DirectMigratorName, parsed.DirectMigratorOID, want.m1MigratorCatalogRole, want.directMigratorName, want.directMigratorOID)
	}
	reencoded, err := CanonicalAppACLManifestR2BodyV1(parsed)
	if err != nil {
		t.Fatalf("CanonicalAppACLManifestR2BodyV1(parsed) error = %v", err)
	}
	if !bytes.Equal(reencoded, wantBody) {
		t.Fatalf("manifest re-encoding = %x, want independent vector %x", reencoded, wantBody)
	}
}

func TestAppACLR2ManifestParserRejectsBadMagicFieldsNestedDigestsAndEOF(t *testing.T) {
	validVector := appACLR2ManifestVector(t)
	valid := rawAppACLR2ManifestBody(validVector)

	sourceDigestSubstitution := cloneTestR2ManifestVector(validVector)
	sourceDigestSubstitution.r2SourceSetDigest[0] ^= 0xff
	privilegeDigestSubstitution := cloneTestR2ManifestVector(validVector)
	privilegeDigestSubstitution.r2PrivilegeSetDigest[0] ^= 0xff
	domainDigestSubstitution := cloneTestR2ManifestVector(validVector)
	domainDigestSubstitution.domainDigest[0] ^= 0xff
	controlDigestSubstitution := cloneTestR2ManifestVector(validVector)
	controlDigestSubstitution.controlACLDigest[0] ^= 0xff

	sourceBodySubstitution := cloneTestR2ManifestVector(validVector)
	sourceBodySubstitution.r2SourceSetBody[len(sourceBodySubstitution.r2SourceSetBody)-1] ^= 0xff
	sourceBodySubstitution.r2SourceSetDigest = sha256.Sum256(sourceBodySubstitution.r2SourceSetBody)

	privilegeOrderSubstitution := cloneTestR2ManifestVector(validVector)
	tuples := appACLR2PrivilegeVectorTuples("houfeng")
	tuples[0], tuples[1] = tuples[1], tuples[0]
	privilegeOrderSubstitution.r2PrivilegeSetBody = rawAppACLR2PrivilegeBody(appACLR2PrivilegeVectorBindings(), tuples, 3, 206)
	privilegeOrderSubstitution.r2PrivilegeSetDigest = sha256.Sum256(privilegeOrderSubstitution.r2PrivilegeSetBody)

	controlOrderSubstitution := cloneTestR2ManifestVector(validVector)
	control := appACLR2ControlACLVector(validVector.directMigratorOID)
	control.objects[0], control.objects[1] = control.objects[1], control.objects[0]
	controlOrderSubstitution.controlACLBody = rawAppACLR2ControlACLBody(control, 3, 2, 2)
	controlOrderSubstitution.controlACLDigest = sha256.Sum256(controlOrderSubstitution.controlACLBody)

	roleMismatch := cloneTestR2ManifestVector(validVector)
	roleMismatch.directMigratorName = "different_direct_migrator"
	zeroOID := cloneTestR2ManifestVector(validVector)
	zeroOID.directMigratorOID = 0

	badSourceLength := append([]byte(nil), valid...)
	sourceLengthOffset := testR2ManifestSourceLengthOffset(validVector)
	binary.BigEndian.PutUint32(badSourceLength[sourceLengthOffset:sourceLengthOffset+4], 4*1024*1024+1)
	r1Magic := append([]byte("HOUFENG-APP-ACL-MANIFEST-V1"), valid[len(appACLR2ManifestMagicVector):]...)

	tests := []struct {
		name string
		body []byte
	}{
		{name: "r1 magic", body: r1Magic},
		{name: "bad codec version", body: replaceTestUint16(valid, len(appACLR2ManifestMagicVector), 2)},
		{name: "bad protocol version", body: replaceTestUint16(valid, len(appACLR2ManifestMagicVector)+2, 1)},
		{name: "bad manifest revision", body: replaceTestUint64(valid, len(appACLR2ManifestMagicVector)+4, 3)},
		{name: "bad m1 revision", body: replaceTestUint64(valid, len(appACLR2ManifestMagicVector)+12, 2)},
		{name: "migrator name mismatch", body: rawAppACLR2ManifestBody(roleMismatch)},
		{name: "zero direct oid", body: rawAppACLR2ManifestBody(zeroOID)},
		{name: "source nested digest substitution", body: rawAppACLR2ManifestBody(sourceDigestSubstitution)},
		{name: "privilege nested digest substitution", body: rawAppACLR2ManifestBody(privilegeDigestSubstitution)},
		{name: "domain nested digest substitution", body: rawAppACLR2ManifestBody(domainDigestSubstitution)},
		{name: "control nested digest substitution", body: rawAppACLR2ManifestBody(controlDigestSubstitution)},
		{name: "source body substitution with matching digest", body: rawAppACLR2ManifestBody(sourceBodySubstitution)},
		{name: "privilege noncanonical order with matching digest", body: rawAppACLR2ManifestBody(privilegeOrderSubstitution)},
		{name: "control noncanonical order with matching digest", body: rawAppACLR2ManifestBody(controlOrderSubstitution)},
		{name: "oversized source body length", body: badSourceLength},
		{name: "truncated", body: valid[:len(valid)-1]},
		{name: "trailing byte", body: append(append([]byte(nil), valid...), 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseCanonicalAppACLManifestR2BodyV1(tt.body); err == nil {
				t.Fatal("ParseCanonicalAppACLManifestR2BodyV1() error = nil, want rejection")
			}
		})
	}
}

func TestAppACLR2ManifestEncoderValidatesBeforeEmission(t *testing.T) {
	valid := productionR2Manifest(appACLR2ManifestVector(t))
	tests := []struct {
		name   string
		mutate func(*AppACLManifestR2V1)
	}{
		{name: "negative recorded time", mutate: func(value *AppACLManifestR2V1) { value.RecordedAtUnixMicroseconds = -1 }},
		{name: "invalid direct role", mutate: func(value *AppACLManifestR2V1) {
			value.DirectMigratorName = "HoufengMigrator"
			value.M1MigratorCatalogRole = "HoufengMigrator"
		}},
		{name: "mismatched roles", mutate: func(value *AppACLManifestR2V1) { value.DirectMigratorName = "different_direct_migrator" }},
		{name: "zero oid", mutate: func(value *AppACLManifestR2V1) { value.DirectMigratorOID = 0 }},
		{name: "empty domain", mutate: func(value *AppACLManifestR2V1) { value.DomainBody = nil; value.DomainDigest = sha256.Sum256(nil) }},
		{name: "oversized domain", mutate: func(value *AppACLManifestR2V1) {
			value.DomainBody = make([]byte, 4*1024*1024+1)
			value.DomainDigest = sha256.Sum256(value.DomainBody)
		}},
		{name: "wrong source digest", mutate: func(value *AppACLManifestR2V1) { value.R2SourceSetDigest[0] ^= 0xff }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := cloneProductionR2Manifest(valid)
			tt.mutate(&value)
			if body, err := CanonicalAppACLManifestR2BodyV1(value); err == nil || body != nil {
				t.Fatalf("CanonicalAppACLManifestR2BodyV1() = %x, %v; want nil and rejection", body, err)
			}
		})
	}
}

func appACLR2ManifestVector(t *testing.T) testR2ManifestVector {
	t.Helper()
	sourceBody := rawAppACLR2SourceBody(appACLR2SourceVectorEntries(t), 53)
	privilegeBody := rawAppACLR2PrivilegeBody(appACLR2PrivilegeVectorBindings(), appACLR2PrivilegeVectorTuples("houfeng"), 3, 206)
	controlBody := rawAppACLR2ControlACLBody(appACLR2ControlACLVector(4242), 3, 2, 2)
	domainBody := []byte{0xff, 0x00, 0xfe, 0x01}
	return testR2ManifestVector{
		protocolVersion:            2,
		manifestRevision:           2,
		m1Revision:                 1,
		m1ManifestDigest:           repeatedDigest(0x11),
		m1SourceSetDigest:          repeatedDigest(0x22),
		m1PrivilegeSetDigest:       repeatedDigest(0x33),
		m1MigratorCatalogRole:      "houfeng_direct_migrator",
		directMigratorName:         "houfeng_direct_migrator",
		directMigratorOID:          4242,
		r2SourceSetBody:            sourceBody,
		r2SourceSetDigest:          sha256.Sum256(sourceBody),
		r2PrivilegeSetBody:         privilegeBody,
		r2PrivilegeSetDigest:       sha256.Sum256(privilegeBody),
		domainBody:                 domainBody,
		domainDigest:               sha256.Sum256(domainBody),
		receiptDigest:              repeatedDigest(0x77),
		controlACLBody:             controlBody,
		controlACLDigest:           sha256.Sum256(controlBody),
		recordedAtUnixMicroseconds: 1_720_000_000_123_456,
	}
}

func cloneTestR2ManifestVector(value testR2ManifestVector) testR2ManifestVector {
	value.r2SourceSetBody = append([]byte(nil), value.r2SourceSetBody...)
	value.r2PrivilegeSetBody = append([]byte(nil), value.r2PrivilegeSetBody...)
	value.domainBody = append([]byte(nil), value.domainBody...)
	value.controlACLBody = append([]byte(nil), value.controlACLBody...)
	return value
}

func productionR2Manifest(value testR2ManifestVector) AppACLManifestR2V1 {
	return AppACLManifestR2V1{
		ProtocolVersion:            value.protocolVersion,
		ManifestRevision:           value.manifestRevision,
		M1Revision:                 value.m1Revision,
		M1ManifestDigest:           value.m1ManifestDigest,
		M1SourceSetDigest:          value.m1SourceSetDigest,
		M1PrivilegeSetDigest:       value.m1PrivilegeSetDigest,
		M1MigratorCatalogRole:      value.m1MigratorCatalogRole,
		DirectMigratorName:         value.directMigratorName,
		DirectMigratorOID:          value.directMigratorOID,
		R2SourceSetBody:            append([]byte(nil), value.r2SourceSetBody...),
		R2SourceSetDigest:          value.r2SourceSetDigest,
		R2PrivilegeSetBody:         append([]byte(nil), value.r2PrivilegeSetBody...),
		R2PrivilegeSetDigest:       value.r2PrivilegeSetDigest,
		DomainBody:                 append([]byte(nil), value.domainBody...),
		DomainDigest:               value.domainDigest,
		ReceiptDigest:              value.receiptDigest,
		ControlACLBody:             append([]byte(nil), value.controlACLBody...),
		ControlACLDigest:           value.controlACLDigest,
		RecordedAtUnixMicroseconds: value.recordedAtUnixMicroseconds,
	}
}

func cloneProductionR2Manifest(value AppACLManifestR2V1) AppACLManifestR2V1 {
	value.R2SourceSetBody = append([]byte(nil), value.R2SourceSetBody...)
	value.R2PrivilegeSetBody = append([]byte(nil), value.R2PrivilegeSetBody...)
	value.DomainBody = append([]byte(nil), value.DomainBody...)
	value.ControlACLBody = append([]byte(nil), value.ControlACLBody...)
	return value
}

func rawAppACLR2ManifestBody(value testR2ManifestVector) []byte {
	body := append([]byte(nil), appACLR2ManifestMagicVector...)
	body = appendTestUint16(body, 1)
	body = appendTestUint16(body, value.protocolVersion)
	body = appendTestUint64(body, value.manifestRevision)
	body = appendTestUint64(body, value.m1Revision)
	body = append(body, value.m1ManifestDigest[:]...)
	body = append(body, value.m1SourceSetDigest[:]...)
	body = append(body, value.m1PrivilegeSetDigest[:]...)
	body = appendTestString16(body, value.m1MigratorCatalogRole)
	body = appendTestString16(body, value.directMigratorName)
	body = appendTestUint32(body, value.directMigratorOID)
	body = appendTestBody32(body, value.r2SourceSetBody)
	body = append(body, value.r2SourceSetDigest[:]...)
	body = appendTestBody32(body, value.r2PrivilegeSetBody)
	body = append(body, value.r2PrivilegeSetDigest[:]...)
	body = appendTestBody32(body, value.domainBody)
	body = append(body, value.domainDigest[:]...)
	body = append(body, value.receiptDigest[:]...)
	body = appendTestBody32(body, value.controlACLBody)
	body = append(body, value.controlACLDigest[:]...)
	body = appendTestUint64(body, uint64(value.recordedAtUnixMicroseconds))
	return body
}

func testR2ManifestSourceLengthOffset(value testR2ManifestVector) int {
	return len(appACLR2ManifestMagicVector) + 2 + 2 + 8 + 8 + 32*3 +
		2 + len(value.m1MigratorCatalogRole) + 2 + len(value.directMigratorName) + 4
}

func replaceTestUint64(body []byte, offset int, value uint64) []byte {
	got := append([]byte(nil), body...)
	binary.BigEndian.PutUint64(got[offset:offset+8], value)
	return got
}

func repeatedDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}
