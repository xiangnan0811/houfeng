package migrate

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"houfeng/db/migrations"
)

func TestAppACLCurrentDeletionWorkerBindingAndHistoricalC80(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   []byte
		size   int
		digest string
	}{
		{"migrations", appACLCurrentC80MigrationGolden, 5900, "496846a4343d7c1f5a09224504fa84cfe168f674d7d49228a3794e1de077cb3e"},
		{"privileges", appACLCurrentC80PrivilegeGolden, 35976, "6df43d88036b64162256b8d76c467a872bdfb82c314021523ac8c0576d55078c"},
	} {
		if len(tc.body) != tc.size || fmt.Sprintf("%x", sha256.Sum256(tc.body)) != tc.digest {
			t.Fatalf("independent pre-C81 C80 %s oracle changed", tc.name)
		}
	}
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileAppACLCurrentCatalogContract(source, appACLCurrentTransitionDatabase, appACLCurrentTransitionBindings, appACLCurrentTransitionMigrator); err == nil {
		t.Fatal("current source accepted missing mandatory worker binding")
	}
	body, err := appACLCurrentTransitionPrivilegeBody(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCanonicalPrivilegeSetBodyV2(body); err != nil {
		t.Fatalf("current contract is not V2: %v", err)
	}
	if _, err := ParseCanonicalPrivilegeSetBodyV1(body); err == nil {
		t.Fatal("V1 parser accepted mandatory-worker contract")
	}
	transitions, err := compileAppACLCurrentTransitions(source, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 18 || len(appACLCurrentAcceptedPredecessorProfileChains) != 196607 {
		t.Fatal("successor omitted registered upgrade histories")
	}
	c80 := transitions[len(transitions)-1]
	if c80.profile != appACLCurrentProfileP80 || !bytes.Equal(c80.predecessor.sources.canonicalSet, appACLCurrentC80MigrationGolden) || !bytes.Equal(c80.predecessorPrivilegeBody, appACLCurrentC80PrivilegeGolden) {
		t.Fatal("P80 predecessor no longer matches independently captured C80")
	}
	// Current role configuration must not change a historical body's bytes.
	historical, err := appACLCurrentTransitionPrivilegeBodyFor(c80.predecessor, appACLCurrentTransitionDatabase, appACLCurrentTransitionBindingsV2, appACLCurrentTransitionMigrator)
	if err != nil || !bytes.Equal(historical, appACLCurrentC80PrivilegeGolden) {
		t.Fatalf("V2 configuration rewrote historical V1 authority: %v", err)
	}
}

func TestAppACLCurrentDeletionRevocationRejectsUnownedAuthority(t *testing.T) {
	granted := AppACLPrivilege{
		Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable,
		SchemaName: "public", ObjectIdentity: "records", Privilege: AppACLPrivilegeDelete,
	}
	for _, tc := range []string{"valid", "missing", "duplicate", "regrant"} {
		t.Run(tc, func(t *testing.T) {
			fragments := make([]AppACLCurrentMigrationFragment, len(appACLCurrentMigrationFragments))
			for index, fragment := range appACLCurrentMigrationFragments {
				fragments[index] = cloneAppACLCurrentMigrationFragment(fragment)
			}
			last := &fragments[len(fragments)-1]
			last.RevokedPrivileges = []AppACLPrivilege{granted}
			switch tc {
			case "missing":
				last.RevokedPrivileges[0].ObjectIdentity = "no_such_record_table"
			case "duplicate":
				last.RevokedPrivileges = append(last.RevokedPrivileges, granted)
			case "regrant":
				original := last.Privileges
				last.Privileges = func(database string) []AppACLPrivilege {
					return append(original(database), granted)
				}
			}
			source, err := compileAppACLCurrentSourceContract(migrations.FS, fragments)
			if tc != "valid" {
				if err == nil {
					t.Fatal("accepted invalid authority revocation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			contract, err := compileAppACLCurrentCatalogContract(source, appACLCurrentTransitionDatabase, appACLCurrentTransitionBindingsV2, appACLCurrentTransitionMigrator)
			if err != nil {
				t.Fatal(err)
			}
			for _, privilege := range contract.Privileges {
				if privilege == granted {
					t.Fatal("revoked runtime DELETE remains effective")
				}
			}
		})
	}
}

func appACLCurrentSourceAtProfile(t *testing.T, current appACLCurrentSourceContract, profile appACLCurrentProfileID) appACLCurrentSourceContract {
	t.Helper()
	for _, definition := range appACLCurrentTransitionDefinitions {
		if definition.profile != profile {
			continue
		}
		index := indexOfAppACLCurrentMigration(current.sources.names, definition.predecessorLastMigration)
		transition, err := compileAppACLCurrentTransition(current, index, definition)
		if err != nil {
			t.Fatal(err)
		}
		return transition.predecessor
	}
	t.Fatalf("unregistered historical profile %d", profile)
	return appACLCurrentSourceContract{}
}

func TestAppACLCurrentCollaborationStartInventoryAuthority(t *testing.T) {
	want := AppACLPrivilege{
		Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassFunction,
		ObjectIdentity: "public.record_collaboration_start_inventory(text, bigint)", Privilege: AppACLPrivilegeExecute,
	}
	privileges := []AppACLPrivilege{want}
	body, err := CanonicalPrivilegeSetBodyV2(appACLCurrentTransitionBindingsV2, privileges)
	if err != nil {
		t.Fatalf("encode collaboration inventory authority: %v", err)
	}
	parsed, err := ParseCanonicalPrivilegeSetBodyV2(body)
	if err != nil || len(parsed.Privileges) != 1 || parsed.Privileges[0] != want {
		t.Fatalf("collaboration inventory authority changed: %#v, %v", parsed, err)
	}
	if _, err := CanonicalPrivilegeSetBodyV1(appACLCurrentTransitionBindings, privileges); err == nil {
		t.Fatal("historical encoder accepted collaboration inventory authority")
	}
	if _, err := ParseCanonicalPrivilegeSetBodyV1(rawCanonicalPrivilegeSetBody(appACLCurrentTransitionBindings, privileges)); err == nil {
		t.Fatal("historical parser accepted collaboration inventory authority")
	}
	for _, tc := range []struct {
		name      string
		privilege AppACLPrivilege
	}{
		{"other_overload", AppACLPrivilege{Subject: want.Subject, ObjectClass: want.ObjectClass, ObjectIdentity: "public.record_collaboration_start_inventory(text, integer)", Privilege: want.Privilege}},
		{"other_function", AppACLPrivilege{Subject: want.Subject, ObjectClass: want.ObjectClass, ObjectIdentity: "public.other_inventory(text, bigint)", Privilege: want.Privilege}},
		{"other_schema", AppACLPrivilege{Subject: want.Subject, ObjectClass: want.ObjectClass, ObjectIdentity: "other.record_collaboration_start_inventory(text, bigint)", Privilege: want.Privilege}},
		{"noncanonical_identity", AppACLPrivilege{Subject: want.Subject, ObjectClass: want.ObjectClass, ObjectIdentity: "public.record_collaboration_start_inventory(text,bigint)", Privilege: want.Privilege}},
		{"wrong_privilege", AppACLPrivilege{Subject: want.Subject, ObjectClass: want.ObjectClass, ObjectIdentity: want.ObjectIdentity, Privilege: AppACLPrivilegeSelect}},
		{"grant_option", AppACLPrivilege{Subject: want.Subject, ObjectClass: want.ObjectClass, ObjectIdentity: want.ObjectIdentity, Privilege: want.Privilege, GrantOption: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CanonicalPrivilegeSetBodyV2(appACLCurrentTransitionBindingsV2, []AppACLPrivilege{tc.privilege}); err == nil {
				t.Fatal("successor encoder accepted invalid function authority")
			}
			raw := rawCanonicalPrivilegeSetBody(appACLCurrentTransitionBindingsV2, []AppACLPrivilege{tc.privilege})
			raw[len("HOUFENG-APP-PRIVILEGE-SET-V1")-1] = '2'
			if tc.privilege.GrantOption {
				raw[len(raw)-1] = 1
			}
			if _, err := ParseCanonicalPrivilegeSetBodyV2(raw); err == nil {
				t.Fatal("successor parser accepted invalid function authority")
			}
		})
	}

	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile current ACL source: %v", err)
	}
	contract, err := compileAppACLCurrentCatalogContract(source, appACLCurrentTransitionDatabase, appACLCurrentTransitionBindingsV2, appACLCurrentTransitionMigrator)
	if err != nil {
		t.Fatalf("compile current catalog contract: %v", err)
	}
	count := 0
	for _, privilege := range contract.Privileges {
		if privilege.ObjectClass != AppACLObjectClassFunction || privilege.ObjectIdentity != want.ObjectIdentity {
			continue
		}
		if privilege != want {
			t.Fatalf("unexpected collaboration inventory grant: %#v", privilege)
		}
		count++
	}
	if count != 1 {
		t.Fatalf("collaboration inventory grant count = %d, want one runtime grant", count)
	}
}

func TestSourceAuthorityCommandsRequirePrivilegeSetV2(t *testing.T) {
	for _, identity := range []string{
		"public.source_application_admit()",
		"public.source_retirement_preview(text, text, text)",
		"public.source_retirement_commit(bigint, text, text, text, text, text, bytea, bytea, bytea, bytea)",
		"public.source_retirement_install_pin(bigint, text, bigint, bytea, bytea, bytea, bigint)",
		"public.source_legacy_retirement_blocked(text, text)",
	} {
		t.Run(identity, func(t *testing.T) {
			privileges := []AppACLPrivilege{{
				Subject: AppACLSubjectPlatformAdmin, ObjectClass: AppACLObjectClassFunction,
				ObjectIdentity: identity, Privilege: AppACLPrivilegeExecute,
			}}
			body, err := CanonicalPrivilegeSetBodyV2(appACLCurrentTransitionBindingsV2, privileges)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseCanonicalPrivilegeSetBodyV2(body)
			if err != nil || len(parsed.Privileges) != 1 || parsed.Privileges[0] != privileges[0] {
				t.Fatalf("exact successor command authority changed: %#v, %v", parsed, err)
			}
			if _, err := CanonicalPrivilegeSetBodyV1(appACLCurrentTransitionBindings, privileges); err == nil {
				t.Fatal("historical encoder accepted successor command authority")
			}
			if _, err := ParseCanonicalPrivilegeSetBodyV1(rawCanonicalPrivilegeSetBody(appACLCurrentTransitionBindings, privileges)); err == nil {
				t.Fatal("historical parser accepted successor command authority")
			}
			privileges[0].ObjectIdentity = "public.source_retirement_preview(text, text)"
			if _, err := CanonicalPrivilegeSetBodyV2(appACLCurrentTransitionBindingsV2, privileges); err == nil {
				t.Fatal("successor accepted unregistered command overload")
			}
		})
	}
}

func TestCanonicalPrivilegeSetV2RequiresExactWorkerAuthority(t *testing.T) {
	bindings := appACLCurrentTransitionBindingsV2
	privileges := []AppACLPrivilege{
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassDatabase, ObjectIdentity: "houfeng", Privilege: AppACLPrivilegeConnect},
		{Subject: AppACLSubjectRecordDeletionWorker, ObjectClass: AppACLObjectClassDatabase, ObjectIdentity: "houfeng", Privilege: AppACLPrivilegeConnect},
	}
	raw := func(bindings []AppACLRoleBinding, privileges []AppACLPrivilege) []byte {
		body := rawCanonicalPrivilegeSetBody(bindings, privileges)
		body[len("HOUFENG-APP-PRIVILEGE-SET-V1")-1] = '2'
		return body
	}
	body, err := CanonicalPrivilegeSetBodyV2(bindings, privileges)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, raw(bindings, privileges)) {
		t.Fatal("V2 framing differs from its independent wire encoding")
	}
	if _, err := ParseCanonicalPrivilegeSetBodyV1(body); err == nil {
		t.Fatal("V1 accepted V2 worker authority")
	}
	for cut := range len(body) {
		if _, err := ParseCanonicalPrivilegeSetBodyV2(body[:cut]); err == nil {
			t.Fatalf("V2 accepted truncated authority at byte %d", cut)
		}
	}
	for _, tc := range []struct {
		name       string
		bindings   []AppACLRoleBinding
		privileges []AppACLPrivilege
	}{
		{"missing_worker", bindings[:2], privileges[:1]},
		{"duplicate_worker", append(append([]AppACLRoleBinding(nil), bindings...), bindings[2]), privileges},
		{"unknown_subject", []AppACLRoleBinding{bindings[0], bindings[1], {Subject: "deletion_worker", CatalogRole: "worker"}}, privileges},
		{"reused_login", []AppACLRoleBinding{bindings[0], bindings[1], {Subject: AppACLSubjectRecordDeletionWorker, CatalogRole: bindings[0].CatalogRole}}, privileges},
		{"unbound_privilege", bindings, []AppACLPrivilege{{Subject: "unregistered", ObjectClass: AppACLObjectClassDatabase, ObjectIdentity: "houfeng", Privilege: AppACLPrivilegeConnect}}},
		{"duplicate_privilege", bindings, []AppACLPrivilege{privileges[1], privileges[1]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CanonicalPrivilegeSetBodyV2(tc.bindings, tc.privileges); err == nil {
				t.Fatal("encoder accepted invalid worker authority")
			}
			if _, err := ParseCanonicalPrivilegeSetBodyV2(raw(tc.bindings, tc.privileges)); err == nil {
				t.Fatal("parser accepted invalid worker authority")
			}
		})
	}
	for _, malformed := range [][]byte{
		append(append([]byte(nil), body...), 0),
		raw([]AppACLRoleBinding{bindings[2], bindings[0], bindings[1]}, privileges),
		raw(bindings, []AppACLPrivilege{privileges[1], privileges[0]}),
	} {
		if _, err := ParseCanonicalPrivilegeSetBodyV2(malformed); err == nil {
			t.Fatal("V2 accepted noncanonical framing/order")
		}
	}
	granted := append([]AppACLPrivilege(nil), privileges...)
	granted[1].GrantOption = true
	if _, err := CanonicalPrivilegeSetBodyV2(bindings, granted); err == nil {
		t.Fatal("V2 encoded a worker grant option")
	}
	for _, flag := range []byte{1, 2, 255} {
		malformed := append([]byte(nil), body...)
		malformed[len(malformed)-1] = flag
		if _, err := ParseCanonicalPrivilegeSetBodyV2(malformed); err == nil {
			t.Fatal("V2 accepted a nonzero worker grant flag")
		}
	}
}

func TestAppACLManifestChainRejectsWorkerAuthorityDowngrade(t *testing.T) {
	v1, err := CanonicalPrivilegeSetBodyV1(appACLCurrentTransitionBindings, nil)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := CanonicalPrivilegeSetBodyV2(appACLCurrentTransitionBindingsV2, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		bodies   [][]byte
		accepted bool
	}{
		{"historical_v1", [][]byte{v1, v1}, true},
		{"worker_successor", [][]byte{v1, v2, v2}, true},
		{"worker_genesis", [][]byte{v2, v2}, true},
		{"worker_downgrade", [][]byte{v2, v1}, false},
		{"worker_removed_after_upgrade", [][]byte{v1, v2, v1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var chain []AppACLManifestPersistedV1
			var previous [32]byte
			for index, body := range tc.bodies {
				manifest, err := NewAppACLManifestPersistedV1(uint64(index+1), appACLCurrentTransitionMigrator, previous, appACLCurrentC80MigrationGolden, body)
				if err != nil {
					t.Fatal(err)
				}
				chain = append(chain, manifest)
				previous = manifest.ManifestDigest
			}
			head := AppACLManifestHeadV1{ManifestRevision: uint64(len(chain)), ManifestDigest: previous}
			err := ValidateAppACLManifestChainV1(chain, head)
			if (err == nil) != tc.accepted {
				t.Fatalf("worker authority transition accepted = %t, want %t: %v", err == nil, tc.accepted, err)
			}
		})
	}
}
