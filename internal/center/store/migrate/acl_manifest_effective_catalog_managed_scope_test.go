package migrate

import (
	"strings"
	"testing"
)

func TestAppACLEffectiveCatalogManagedSurfaceScopeR1RetainsUnknownInternalFunctionPrivilegeOverload(t *testing.T) {
	scope, err := newAppACLManagedSurfaceScopeR1("houfeng")
	if err != nil {
		t.Fatalf("newAppACLManagedSurfaceScopeR1() error = %v", err)
	}

	managed := AppACLEffectiveCatalogPrivilegeObservationR1{
		ObjectClass:    AppACLObjectClassFunction,
		ObjectIdentity: "record_platform_internal.record_platform_projection_read_bytes_v1(p_command bytea, p_offset integer, p_length integer)",
	}
	if !scope.containsPrivilege(managed) {
		t.Fatalf("managed function privilege %#v was excluded from its fixed surface", managed)
	}

	unknownInternalOverload := managed
	unknownInternalOverload.ObjectIdentity = "record_platform_internal.record_platform_projection_read_bytes_v1(p_command bytea, p_offset integer, p_length text)"
	if !scope.containsPrivilege(unknownInternalOverload) {
		t.Fatalf("unknown internal function overload %#v was excluded from the managed catalog scope", unknownInternalOverload)
	}
}

func TestAppACLEffectiveCatalogManagedSurfaceScopeR1RetainsEveryPublicProjectorOverload(t *testing.T) {
	scope, err := newAppACLManagedSurfaceScopeR1("houfeng")
	if err != nil {
		t.Fatalf("newAppACLManagedSurfaceScopeR1() error = %v", err)
	}

	for _, projector := range appACLProjectorFunctionsR1() {
		name, _, found := strings.Cut(projector.identity, "(")
		if !found {
			t.Fatalf("projector identity %q has no arguments", projector.identity)
		}
		textOverload := AppACLEffectiveCatalogFunctionR1{
			SchemaName:        projector.schemaName,
			Name:              name,
			IdentityArguments: "text",
		}
		if !scope.containsFunction(textOverload) {
			t.Fatalf("projector text overload %#v was excluded from the managed catalog scope", textOverload)
		}

		textOverloadPrivilege := AppACLEffectiveCatalogPrivilegeObservationR1{
			ObjectClass:    AppACLObjectClassFunction,
			ObjectIdentity: projector.schemaName + "." + name + "(text)",
			Privilege:      AppACLPrivilegeExecute,
		}
		if !scope.containsPrivilege(textOverloadPrivilege) {
			t.Fatalf("projector text-overload privilege %#v was excluded from the managed catalog scope", textOverloadPrivilege)
		}
	}

	unrelated := AppACLEffectiveCatalogFunctionR1{
		SchemaName:        appACLManagedPublicSchemaR1,
		Name:              "unrelated_catalog_fixture_function",
		IdentityArguments: "text",
	}
	if scope.containsFunction(unrelated) {
		t.Fatalf("unrelated public function %#v was included in the managed catalog scope", unrelated)
	}
	unrelatedPrivilege := AppACLEffectiveCatalogPrivilegeObservationR1{
		ObjectClass:    AppACLObjectClassFunction,
		ObjectIdentity: "public.unrelated_catalog_fixture_function(text)",
		Privilege:      AppACLPrivilegeExecute,
	}
	if scope.containsPrivilege(unrelatedPrivilege) {
		t.Fatalf("unrelated public function privilege %#v was included in the managed catalog scope", unrelatedPrivilege)
	}
}

func TestAppACLEffectiveCatalogManagedSurfaceScopeR1RetainsUnknownInternalObjects(t *testing.T) {
	scope, err := newAppACLManagedSurfaceScopeR1("houfeng")
	if err != nil {
		t.Fatalf("newAppACLManagedSurfaceScopeR1() error = %v", err)
	}

	internalTableOwner := AppACLEffectiveCatalogObjectOwnerR1{
		ObjectClass:    AppACLObjectClassTable,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "unexpected_internal_catalog_table",
	}
	if !scope.containsOwner(internalTableOwner) {
		t.Fatalf("unknown internal table owner %#v was excluded from the managed catalog scope", internalTableOwner)
	}

	internalViewOwner := AppACLEffectiveCatalogObjectOwnerR1{
		ObjectClass:    AppACLObjectClassView,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "unexpected_internal_catalog_view",
	}
	if !scope.containsOwner(internalViewOwner) {
		t.Fatalf("unknown internal view owner %#v was excluded from the managed catalog scope", internalViewOwner)
	}

	internalSequenceOwner := AppACLEffectiveCatalogObjectOwnerR1{
		ObjectClass:    AppACLObjectClassSequence,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "unexpected_internal_catalog_sequence",
	}
	if !scope.containsOwner(internalSequenceOwner) {
		t.Fatalf("unknown internal sequence owner %#v was excluded from the managed catalog scope", internalSequenceOwner)
	}

	internalFunctionOwner := AppACLEffectiveCatalogObjectOwnerR1{
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "unexpected_internal_catalog_function()",
	}
	if !scope.containsOwner(internalFunctionOwner) {
		t.Fatalf("unknown internal function owner %#v was excluded from the managed catalog scope", internalFunctionOwner)
	}

	internalTablePrivilege := AppACLEffectiveCatalogPrivilegeObservationR1{
		ObjectClass:    AppACLObjectClassTable,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "unexpected_internal_catalog_table",
		Privilege:      AppACLPrivilegeSelect,
	}
	if !scope.containsPrivilege(internalTablePrivilege) {
		t.Fatalf("unknown internal table privilege %#v was excluded from the managed catalog scope", internalTablePrivilege)
	}

	internalViewPrivilege := AppACLEffectiveCatalogPrivilegeObservationR1{
		ObjectClass:    AppACLObjectClassView,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "unexpected_internal_catalog_view",
		Privilege:      AppACLPrivilegeSelect,
	}
	if !scope.containsPrivilege(internalViewPrivilege) {
		t.Fatalf("unknown internal view privilege %#v was excluded from the managed catalog scope", internalViewPrivilege)
	}

	internalSequencePrivilege := AppACLEffectiveCatalogPrivilegeObservationR1{
		ObjectClass:    AppACLObjectClassSequence,
		SchemaName:     appACLManagedInternalSchemaR1,
		ObjectIdentity: "unexpected_internal_catalog_sequence",
		Privilege:      AppACLPrivilegeUsage,
	}
	if !scope.containsPrivilege(internalSequencePrivilege) {
		t.Fatalf("unknown internal sequence privilege %#v was excluded from the managed catalog scope", internalSequencePrivilege)
	}

	internalFunctionPrivilege := AppACLEffectiveCatalogPrivilegeObservationR1{
		ObjectClass:    AppACLObjectClassFunction,
		ObjectIdentity: appACLManagedInternalSchemaR1 + ".unexpected_internal_catalog_function()",
		Privilege:      AppACLPrivilegeExecute,
	}
	if !scope.containsPrivilege(internalFunctionPrivilege) {
		t.Fatalf("unknown internal function privilege %#v was excluded from the managed catalog scope", internalFunctionPrivilege)
	}

	internalColumnACL := AppACLEffectiveCatalogColumnACLR1{
		SchemaName:   appACLManagedInternalSchemaR1,
		RelationName: "unexpected_internal_catalog_table",
		ColumnName:   "id",
		Privilege:    AppACLPrivilegeSelect,
	}
	if !scope.containsColumnACL(internalColumnACL) {
		t.Fatalf("unknown internal column ACL %#v was excluded from the managed catalog scope", internalColumnACL)
	}

	internalViewColumnACL := AppACLEffectiveCatalogColumnACLR1{
		SchemaName:   appACLManagedInternalSchemaR1,
		RelationName: "unexpected_internal_catalog_view",
		ColumnName:   "id",
		Privilege:    AppACLPrivilegeSelect,
	}
	if !scope.containsColumnACL(internalViewColumnACL) {
		t.Fatalf("unknown internal view column ACL %#v was excluded from the managed catalog scope", internalViewColumnACL)
	}

	internalFunction := AppACLEffectiveCatalogFunctionR1{
		SchemaName:        appACLManagedInternalSchemaR1,
		Name:              "unexpected_internal_catalog_function",
		IdentityArguments: "",
	}
	if !scope.containsFunction(internalFunction) {
		t.Fatalf("unknown internal function %#v was excluded from the managed catalog scope", internalFunction)
	}

	publicTableOwner := internalTableOwner
	publicTableOwner.SchemaName = appACLManagedPublicSchemaR1
	if scope.containsOwner(publicTableOwner) {
		t.Fatalf("unrelated public table owner %#v was included in the managed catalog scope", publicTableOwner)
	}

	publicViewOwner := internalViewOwner
	publicViewOwner.SchemaName = appACLManagedPublicSchemaR1
	if scope.containsOwner(publicViewOwner) {
		t.Fatalf("unrelated public view owner %#v was included in the managed catalog scope", publicViewOwner)
	}

	publicSequenceOwner := internalSequenceOwner
	publicSequenceOwner.SchemaName = appACLManagedPublicSchemaR1
	if scope.containsOwner(publicSequenceOwner) {
		t.Fatalf("unrelated public sequence owner %#v was included in the managed catalog scope", publicSequenceOwner)
	}

	publicTablePrivilege := internalTablePrivilege
	publicTablePrivilege.SchemaName = appACLManagedPublicSchemaR1
	if scope.containsPrivilege(publicTablePrivilege) {
		t.Fatalf("unrelated public table privilege %#v was included in the managed catalog scope", publicTablePrivilege)
	}

	publicViewPrivilege := internalViewPrivilege
	publicViewPrivilege.SchemaName = appACLManagedPublicSchemaR1
	if scope.containsPrivilege(publicViewPrivilege) {
		t.Fatalf("unrelated public view privilege %#v was included in the managed catalog scope", publicViewPrivilege)
	}

	publicSequencePrivilege := internalSequencePrivilege
	publicSequencePrivilege.SchemaName = appACLManagedPublicSchemaR1
	if scope.containsPrivilege(publicSequencePrivilege) {
		t.Fatalf("unrelated public sequence privilege %#v was included in the managed catalog scope", publicSequencePrivilege)
	}

	publicColumnACL := internalColumnACL
	publicColumnACL.SchemaName = appACLManagedPublicSchemaR1
	if scope.containsColumnACL(publicColumnACL) {
		t.Fatalf("unrelated public column ACL %#v was included in the managed catalog scope", publicColumnACL)
	}

	publicViewColumnACL := internalViewColumnACL
	publicViewColumnACL.SchemaName = appACLManagedPublicSchemaR1
	if scope.containsColumnACL(publicViewColumnACL) {
		t.Fatalf("unrelated public view column ACL %#v was included in the managed catalog scope", publicViewColumnACL)
	}

	publicFunction := internalFunction
	publicFunction.SchemaName = appACLManagedPublicSchemaR1
	if scope.containsFunction(publicFunction) {
		t.Fatalf("unrelated public function %#v was included in the managed catalog scope", publicFunction)
	}
}
