package migrate

import (
	"reflect"
	"testing"

	"houfeng/db/migrations"
)

func TestSubscriptionCreateIdempotencyAppACLFragmentRegistersTableSelectInsert(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	if len(source.fragments) != 10 {
		t.Fatalf("production current APP ACL fragments = %d, want records-core through subscription create idempotency", len(source.fragments))
	}
	fragment := source.fragments[9]
	if fragment.Migration != "0061_create_subscription_create_idempotency.sql" {
		t.Fatalf("tenth production current APP ACL fragment migration = %q, want subscription create idempotency", fragment.Migration)
	}

	wantObjects, err := canonicalAppACLManagedObjects(subscriptionCreateIdempotencyExpectedAppACLObjects())
	if err != nil {
		t.Fatal(err)
	}
	gotObjects, err := canonicalAppACLManagedObjects(fragment.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		t.Fatalf("subscription create idempotency managed objects = %#v, want %#v", gotObjects, wantObjects)
	}
	if len(fragment.Functions) != 0 {
		t.Fatalf("subscription create idempotency function hardening contracts = %#v, want none", fragment.Functions)
	}

	wantPrivileges, err := canonicalPrivileges(subscriptionCreateIdempotencyExpectedAppACLPrivileges())
	if err != nil {
		t.Fatal(err)
	}
	gotPrivileges, err := canonicalPrivileges(fragment.Privileges)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPrivileges, wantPrivileges) {
		t.Fatalf("subscription create idempotency APP ACL privileges = %#v, want %#v", gotPrivileges, wantPrivileges)
	}
	for _, privilege := range gotPrivileges {
		if privilege.Privilege != AppACLPrivilegeSelect && privilege.Privilege != AppACLPrivilegeInsert {
			t.Fatalf("subscription create idempotency privilege %q is outside select/insert", privilege.Privilege)
		}
	}
}

func subscriptionCreateIdempotencyExpectedAppACLObjects() []AppACLManagedObjectR1 {
	return []AppACLManagedObjectR1{{
		ObjectClass:    AppACLObjectClassTable,
		SchemaName:     appACLManagedPublicSchemaR1,
		ObjectIdentity: "subscription_create_idempotency",
	}}
}

func subscriptionCreateIdempotencyExpectedAppACLPrivileges() []AppACLPrivilege {
	return subscriptionCreateIdempotencyAppACLCurrentPrivileges("")
}
