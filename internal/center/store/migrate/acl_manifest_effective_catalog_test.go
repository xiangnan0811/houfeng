package migrate

import (
	"reflect"
	"testing"
)

func TestCompileAppACLEffectiveCatalogContractR1DerivesCanonicalExpectedCatalog(t *testing.T) {
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
	}

	contract, err := CompileAppACLEffectiveCatalogContractR1("houfeng", bindings)
	if err != nil {
		t.Fatalf("CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	if contract.DatabaseName != "houfeng" {
		t.Fatalf("contract database = %q, want houfeng", contract.DatabaseName)
	}

	wantBindings := [2]AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}
	if contract.RoleBindings != wantBindings {
		t.Fatalf("contract role bindings = %#v, want %#v", contract.RoleBindings, wantBindings)
	}

	canonicalBody, err := CompileAppACLPrivilegeSetR1("houfeng", bindings)
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR1() error = %v", err)
	}
	canonicalSet, err := ParseCanonicalPrivilegeSetBodyV1(canonicalBody)
	if err != nil {
		t.Fatalf("ParseCanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	if got := len(contract.Privileges); got != 206 {
		t.Fatalf("contract privilege count = %d, want 206", got)
	}
	for index, privilege := range contract.Privileges {
		if privilege != canonicalSet.Privileges[index] {
			t.Fatalf("contract privilege %d = %#v, want canonical %#v", index, privilege, canonicalSet.Privileges[index])
		}
		if privilege.GrantOption {
			t.Fatalf("contract privilege %d unexpectedly has grant option", index)
		}
	}

	sequenceUsage := make(map[string]struct{})
	functionExecute := make(map[string]struct{})
	for _, privilege := range contract.Privileges {
		switch privilege.ObjectClass {
		case AppACLObjectClassSequence:
			if privilege.Subject != AppACLSubjectCenterRuntime || privilege.Privilege != AppACLPrivilegeUsage {
				t.Fatalf("sequence privilege = %#v, want runtime USAGE", privilege)
			}
			sequenceUsage[privilege.SchemaName+"."+privilege.ObjectIdentity] = struct{}{}
		case AppACLObjectClassFunction:
			if privilege.Subject != AppACLSubjectCenterRuntime || privilege.Privilege != AppACLPrivilegeExecute {
				t.Fatalf("function privilege = %#v, want runtime EXECUTE", privilege)
			}
			functionExecute[privilege.ObjectIdentity] = struct{}{}
		}
	}
	if want := map[string]struct{}{
		"public.host_samples_id_seq":             {},
		"public.node_heartbeats_id_seq":          {},
		"public.probe_observations_id_seq":       {},
		"public.record_outbox_outbox_row_id_seq": {},
	}; !reflect.DeepEqual(sequenceUsage, want) {
		t.Fatalf("sequence USAGE = %#v, want %#v", sequenceUsage, want)
	}
	if want := map[string]struct{}{
		"public.record_platform_cas_contract_activation_projection(bytea)": {},
		"public.record_platform_cas_domain_rotation_projection(bytea)":     {},
	}; !reflect.DeepEqual(functionExecute, want) {
		t.Fatalf("function EXECUTE = %#v, want %#v", functionExecute, want)
	}

	wantPolicies := [2]AppACLEffectiveCatalogRolePolicyR1{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}
	if contract.RolePolicies != wantPolicies {
		t.Fatalf("contract role policies = %#v, want %#v", contract.RolePolicies, wantPolicies)
	}
}

func TestCompileAppACLEffectiveCatalogContractR1IsComparableAndBindingSensitive(t *testing.T) {
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}

	first, err := CompileAppACLEffectiveCatalogContractR1("houfeng", bindings)
	if err != nil {
		t.Fatalf("first CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	second, err := CompileAppACLEffectiveCatalogContractR1("houfeng", []AppACLRoleBinding{
		bindings[1],
		bindings[0],
	})
	if err != nil {
		t.Fatalf("second CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	if first != second {
		t.Fatal("same canonical role bindings produced unequal effective catalog contracts")
	}

	differentDatabase, err := CompileAppACLEffectiveCatalogContractR1("otherdb", bindings)
	if err != nil {
		t.Fatalf("different database CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	if first == differentDatabase {
		t.Fatal("different database produced an equal effective catalog contract")
	}

	differentBindings, err := CompileAppACLEffectiveCatalogContractR1("houfeng", []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "other_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "other_platform_admin"},
	})
	if err != nil {
		t.Fatalf("different bindings CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	if first == differentBindings {
		t.Fatal("different role bindings produced an equal effective catalog contract")
	}
}
