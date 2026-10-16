package migrate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"houfeng/db/migrations"
)

func TestAppACLCurrentCollectionScopeFragmentHasNoAuthorizationDelta(t *testing.T) {
	fragment := ipQualityCollectionScopeAppACLCurrentMigrationFragment()
	if fragment.Migration != "0078_ip_quality_collection_scope.sql" || len(fragment.Objects) != 0 ||
		len(fragment.Functions) != 0 || len(fragment.AuxiliaryPrivileges) != 0 {
		t.Fatalf("0078 must replace an existing view without adding managed objects/functions/auxiliary privileges: %#v", fragment)
	}
	for _, database := range []string{"houfeng", "other_database"} {
		if privileges := fragment.Privileges(database); len(privileges) != 0 {
			t.Fatalf("0078 adds privileges for %s: %#v", database, privileges)
		}
	}
}

func TestAppACLCurrentC77IndependentGoldensPreserveC76(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		sha  string
	}{
		{name: "migrations", body: appACLCurrentC77MigrationGolden, sha: "84d5be810a09f4fbda9d98594c11db085f968944c002b55a10efe322346da1e6"},
		{name: "privileges", body: appACLCurrentC77PrivilegeGolden, sha: "3bda892781478202e876702943a66463e25345c205c9c125304b2b6ba1a1eb0c"},
	} {
		if got := fmt.Sprintf("%x", sha256.Sum256(tc.body)); got != tc.sha {
			t.Fatalf("independent pre-C78 C77 %s golden digest = %s, want %s", tc.name, got, tc.sha)
		}
	}
	entries, err := ParseCanonicalMigrationSetBodyV1(appACLCurrentC77MigrationGolden)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 78 || entries[77].Filename != "0077_ip_quality_product.sql" {
		t.Fatalf("independent C77 source oracle must end in exactly 0077: %#v", entries)
	}
	prefix, err := CanonicalMigrationSetBodyV1(entries[:77])
	if err != nil || !bytes.Equal(prefix, appACLCurrentC76MigrationGolden) {
		t.Fatalf("independent C77 source oracle changed the released C76 prefix: %v", err)
	}
	c76, err := ParseCanonicalPrivilegeSetBodyV1(appACLCurrentC76PrivilegeGolden)
	if err != nil {
		t.Fatal(err)
	}
	body, err := CanonicalPrivilegeSetBodyV1(c76.RoleBindings, append(c76.Privileges, appACLCurrentProductExpectedPrivileges()...))
	if err != nil || !bytes.Equal(body, appACLCurrentC77PrivilegeGolden) {
		t.Fatalf("independent C77 privilege oracle differs from C76 plus exactly seven product grants: %v", err)
	}
}

func TestAppACLCurrentP77RequiresExactlyPreservedPrivileges(t *testing.T) {
	testAppACLCurrentPreservedPrivileges(t, appACLCurrentProfileP77, appACLCurrentC77PrivilegeGolden)
}

func testAppACLCurrentPreservedPrivileges(t *testing.T, profile appACLCurrentProfileID, golden []byte) {
	t.Helper()
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	body, err := appACLCurrentTransitionPrivilegeBody(current)
	if err != nil || !bytes.Equal(body, golden) {
		t.Fatalf("current must preserve the independently frozen predecessor authorization body exactly: %v", err)
	}
	if err := validateAppACLCurrentTransitionPrivilegeDelta(golden, current, profile); err != nil {
		t.Fatalf("unchanged predecessor authorization was rejected: %v", err)
	}
	for _, change := range []string{"add_runtime", "add_admin", "remove", "grant_option", "role_binding"} {
		t.Run(change, func(t *testing.T) {
			modified := current
			modified.fragments = make([]appACLCurrentCompiledMigrationFragment, len(current.fragments))
			for i, fragment := range current.fragments {
				modified.fragments[i] = cloneAppACLCurrentCompiledMigrationFragment(fragment)
			}
			index := -1
			for i, fragment := range modified.fragments {
				if fragment.Migration == "0077_ip_quality_product.sql" {
					index = i
					break
				}
			}
			if index < 0 {
				t.Fatal("missing product fragment in current source")
			}
			predecessorBody := golden
			switch change {
			case "add_runtime", "add_admin":
				subject := AppACLSubjectCenterRuntime
				if change == "add_admin" {
					subject = AppACLSubjectPlatformAdmin
				}
				modified.fragments[index].Privileges = append(modified.fragments[index].Privileges, AppACLPrivilege{
					Subject: subject, ObjectClass: AppACLObjectClassTable, SchemaName: "public",
					ObjectIdentity: "ip_quality_intelligence", Privilege: AppACLPrivilegeDelete,
				})
			case "remove":
				modified.fragments[index].Privileges = modified.fragments[index].Privileges[1:]
			case "grant_option":
				modified.fragments[index].Privileges[0].GrantOption = true
			case "role_binding":
				predecessor, err := ParseCanonicalPrivilegeSetBodyV1(predecessorBody)
				if err != nil {
					t.Fatal(err)
				}
				predecessor.RoleBindings[0].CatalogRole = "different_runtime"
				predecessorBody, err = CanonicalPrivilegeSetBodyV1(predecessor.RoleBindings, predecessor.Privileges)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := validateAppACLCurrentTransitionPrivilegeDelta(predecessorBody, modified, profile); err == nil {
				t.Fatalf("profile %d accepted %s authorization drift", profile, change)
			}
		})
	}
}

func TestAppACLCurrentP77RejectsPublishedSourceDriftBeforeBeginTx(t *testing.T) {
	fsys := appACLCurrentTransitionTestFS(t)
	file := fsys["0077_ip_quality_product.sql"]
	file.Data = append(append([]byte(nil), file.Data...), []byte("\n-- altered historical source\n")...)
	assertAppACLCurrentTransitionRejectedBeforeBeginTx(t, fsys, appACLCurrentMigrationFragments, appACLCurrentTransitionDefinitions,
		"predecessor migration source differs from released golden")
}
