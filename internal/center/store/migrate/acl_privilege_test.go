package migrate

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestCanonicalPrivilegeSetBodyV1SortsAndRoundTrips(t *testing.T) {
	body, err := CanonicalPrivilegeSetBodyV1(
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		},
		[]AppACLPrivilege{
			{
				Subject:        AppACLSubjectPlatformAdmin,
				ObjectClass:    AppACLObjectClassDatabase,
				ObjectIdentity: "houfeng",
				Privilege:      AppACLPrivilegeConnect,
			},
			{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassFunction,
				ObjectIdentity: "public.record_platform_cas_contract_activation_projection(bytea)",
				Privilege:      AppACLPrivilegeExecute,
			},
		},
	)
	if err != nil {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	if !bytes.HasPrefix(body, []byte("HOUFENG-APP-PRIVILEGE-SET-V1")) {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() = %x, want v1 magic prefix", body)
	}

	set, err := ParseCanonicalPrivilegeSetBodyV1(body)
	if err != nil {
		t.Fatalf("ParseCanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	if len(set.RoleBindings) != 2 || set.RoleBindings[0].Subject != AppACLSubjectCenterRuntime || set.RoleBindings[1].Subject != AppACLSubjectPlatformAdmin {
		t.Fatalf("parsed role bindings = %#v, want semantic-subject order", set.RoleBindings)
	}
	if len(set.Privileges) != 2 || set.Privileges[0].Subject != AppACLSubjectCenterRuntime || set.Privileges[1].Subject != AppACLSubjectPlatformAdmin {
		t.Fatalf("parsed privileges = %#v, want tuple order", set.Privileges)
	}
}

func TestCanonicalPrivilegeSetBodyV1RejectsInvalidRoleAndPrivilegeShapes(t *testing.T) {
	validBindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}
	validPrivilege := AppACLPrivilege{
		Subject:        AppACLSubjectCenterRuntime,
		ObjectClass:    AppACLObjectClassDatabase,
		ObjectIdentity: "houfeng",
		Privilege:      AppACLPrivilegeConnect,
	}

	tests := []struct {
		name       string
		bindings   []AppACLRoleBinding
		privileges []AppACLPrivilege
	}{
		{
			name: "missing semantic subject",
			bindings: []AppACLRoleBinding{
				{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
			},
			privileges: []AppACLPrivilege{validPrivilege},
		},
		{
			name: "duplicate catalog role",
			bindings: []AppACLRoleBinding{
				{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_shared"},
				{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_shared"},
			},
			privileges: []AppACLPrivilege{validPrivilege},
		},
		{
			name: "non NFC catalog role",
			bindings: []AppACLRoleBinding{
				{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_e\u0301"},
				{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
			},
			privileges: []AppACLPrivilege{validPrivilege},
		},
		{
			name:     "function stores schema separately",
			bindings: validBindings,
			privileges: []AppACLPrivilege{{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassFunction,
				SchemaName:     "public",
				ObjectIdentity: "public.record_platform_cas_contract_activation_projection(bytea)",
				Privilege:      AppACLPrivilegeExecute,
			}},
		},
		{
			name:     "column grant",
			bindings: validBindings,
			privileges: []AppACLPrivilege{{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassColumn,
				SchemaName:     "public",
				ObjectIdentity: "users",
				ColumnName:     "username",
				Privilege:      AppACLPrivilegeSelect,
			}},
		},
		{
			name:     "grant option",
			bindings: validBindings,
			privileges: []AppACLPrivilege{{
				Subject:        AppACLSubjectCenterRuntime,
				ObjectClass:    AppACLObjectClassDatabase,
				ObjectIdentity: "houfeng",
				Privilege:      AppACLPrivilegeConnect,
				GrantOption:    true,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CanonicalPrivilegeSetBodyV1(tt.bindings, tt.privileges); err == nil {
				t.Fatal("CanonicalPrivilegeSetBodyV1() error = nil, want shape rejection")
			}
		})
	}
}

func TestParseCanonicalPrivilegeSetBodyV1RejectsTruncatedAndTrailingBytes(t *testing.T) {
	body, err := CanonicalPrivilegeSetBodyV1(
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
		[]AppACLPrivilege{{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassDatabase,
			ObjectIdentity: "houfeng",
			Privilege:      AppACLPrivilegeConnect,
		}},
	)
	if err != nil {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() error = %v", err)
	}

	for _, malformed := range [][]byte{
		[]byte("wrong magic"),
		body[:len(body)-1],
		append(append([]byte(nil), body...), 0),
	} {
		if _, err := ParseCanonicalPrivilegeSetBodyV1(malformed); err == nil {
			t.Fatalf("ParseCanonicalPrivilegeSetBodyV1(%x) error = nil, want rejection", malformed)
		}
	}
}

func TestParseCanonicalPrivilegeSetBodyV1RejectsNonCanonicalTupleOrderAndDuplicates(t *testing.T) {
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}
	centerDatabase := AppACLPrivilege{
		Subject:        AppACLSubjectCenterRuntime,
		ObjectClass:    AppACLObjectClassDatabase,
		ObjectIdentity: "houfeng",
		Privilege:      AppACLPrivilegeConnect,
	}
	adminDatabase := AppACLPrivilege{
		Subject:        AppACLSubjectPlatformAdmin,
		ObjectClass:    AppACLObjectClassDatabase,
		ObjectIdentity: "houfeng",
		Privilege:      AppACLPrivilegeConnect,
	}

	for _, privileges := range [][]AppACLPrivilege{
		{adminDatabase, centerDatabase},
		{centerDatabase, centerDatabase},
	} {
		if _, err := ParseCanonicalPrivilegeSetBodyV1(rawCanonicalPrivilegeSetBody(bindings, privileges)); err == nil {
			t.Fatal("ParseCanonicalPrivilegeSetBodyV1() error = nil, want non-canonical tuple rejection")
		}
	}
}

func rawCanonicalPrivilegeSetBody(bindings []AppACLRoleBinding, privileges []AppACLPrivilege) []byte {
	body := append([]byte(nil), "HOUFENG-APP-PRIVILEGE-SET-V1"...)
	body = appendRawPrivilegeUint32(body, uint32(len(bindings)))
	for _, binding := range bindings {
		body = appendRawPrivilegeString(body, string(binding.Subject))
		body = appendRawPrivilegeString(body, binding.CatalogRole)
	}
	body = appendRawPrivilegeUint32(body, uint32(len(privileges)))
	for _, privilege := range privileges {
		body = appendRawPrivilegeString(body, string(privilege.Subject))
		body = appendRawPrivilegeString(body, string(privilege.ObjectClass))
		body = appendRawPrivilegeString(body, privilege.SchemaName)
		body = appendRawPrivilegeString(body, privilege.ObjectIdentity)
		body = appendRawPrivilegeString(body, privilege.ColumnName)
		body = appendRawPrivilegeString(body, string(privilege.Privilege))
		body = append(body, 0)
	}
	return body
}

func appendRawPrivilegeUint32(body []byte, value uint32) []byte {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	return append(body, encoded[:]...)
}

func appendRawPrivilegeString(body []byte, value string) []byte {
	body = appendRawPrivilegeUint32(body, uint32(len(value)))
	return append(body, value...)
}
