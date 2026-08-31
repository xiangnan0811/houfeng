package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

func TestVPSCreateIdempotencyMigrationCreatesExplicitPrivateReceipts(t *testing.T) {
	t.Parallel()

	source, err := migrations.FS.ReadFile("0062_create_vps_create_idempotency.sql")
	if err != nil {
		t.Fatalf("read VPS create idempotency migration: %T", err)
	}
	sql := string(source)
	for _, table := range []string{
		"experience_log_create_idempotency",
		"asset_service_create_idempotency",
		"asset_domain_create_idempotency",
		"vps_monitoring_instance_create_idempotency",
	} {
		if !strings.Contains(sql, "create table if not exists "+table) {
			t.Fatalf("migration missing explicit receipt table %q", table)
		}
	}
	for index, snippet := range []string{
		"experience_log_id text not null references experience_logs(experience_log_id) on delete cascade",
		"service_id text not null references asset_services(service_id) on delete cascade",
		"domain_id text not null references asset_domains(domain_id) on delete cascade",
		"monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade",
		"link_id text not null references vps_monitoring_instance_links(link_id) on delete cascade",
		"char_length(idempotency_key) between 8 and 128",
		"idempotency_key ~ '^[A-Za-z0-9._:-]+$'",
		"request_digest ~ '^[0-9a-f]{64}$'",
		"created_at timestamptz not null default now()",
	} {
		if !strings.Contains(sql, snippet) {
			t.Fatalf("migration required fragment index %d is missing", index)
		}
	}
	for _, forbidden := range []string{"request_body", "raw_body", "credential", "secret", "password", "expires_at", "updated_at"} {
		if strings.Contains(strings.ToLower(sql), forbidden) {
			t.Fatalf("migration contains forbidden receipt field %q", forbidden)
		}
	}
	wantColumns := map[string][]string{
		"experience_log_create_idempotency":          {"idempotency_key", "request_digest", "experience_log_id", "created_at"},
		"asset_service_create_idempotency":           {"idempotency_key", "request_digest", "service_id", "created_at"},
		"asset_domain_create_idempotency":            {"idempotency_key", "request_digest", "domain_id", "created_at"},
		"vps_monitoring_instance_create_idempotency": {"idempotency_key", "request_digest", "monitoring_instance_id", "link_id", "created_at"},
	}
	for table, expected := range wantColumns {
		got := migrationTableColumns(t, sql, table)
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("receipt table %s column count = %d, want %d", table, len(got), len(expected))
		}
	}
	for _, indexName := range []string{
		"idx_experience_log_create_idempotency_result",
		"idx_asset_service_create_idempotency_result",
		"idx_asset_domain_create_idempotency_result",
		"idx_vps_monitoring_instance_create_idempotency_instance",
		"idx_vps_monitoring_instance_create_idempotency_link",
	} {
		if !strings.Contains(sql, "create index if not exists "+indexName) {
			t.Fatalf("migration missing result lookup index %q", indexName)
		}
	}
}

func migrationTableColumns(t *testing.T, sql, table string) []string {
	t.Helper()
	prefix := "create table if not exists " + table + " ("
	start := strings.Index(sql, prefix)
	if start < 0 {
		t.Fatalf("migration table %q not found", table)
	}
	bodyStart := start + len(prefix)
	bodyEnd := strings.Index(sql[bodyStart:], "\n);")
	if bodyEnd < 0 {
		t.Fatalf("migration table %q body is unterminated", table)
	}
	body := sql[bodyStart : bodyStart+bodyEnd]
	matches := regexp.MustCompile(`(?m)^  ([a-z_]+)\s+`).FindAllStringSubmatch(body, -1)
	columns := make([]string, 0, len(matches))
	for _, match := range matches {
		if match[1] != "constraint" && match[1] != "check" && match[1] != "and" {
			columns = append(columns, match[1])
		}
	}
	return columns
}

func TestReleasedSubscriptionCreateIdempotencyMigrationRemainsExact(t *testing.T) {
	t.Parallel()

	source, err := migrations.FS.ReadFile("0061_create_subscription_create_idempotency.sql")
	if err != nil {
		t.Fatalf("read released migration error type = %T", err)
	}
	digest := sha256.Sum256(source)
	if got, want := hex.EncodeToString(digest[:]), "ea15d700357b31aba400da0f8f778568d6349e7573fa7ca0d0c73b84e9cd0833"; got != want {
		t.Fatal("released 0061 digest changed")
	}
}

func TestVPSCreateIdempotencyAppACLFragmentRegistersExactSelectInsert(t *testing.T) {
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %T", err)
	}
	if got, want := len(source.fragments), 12; got != want {
		t.Fatalf("production current APP ACL fragments = %d, want %d", got, want)
	}
	fragment := source.fragments[10]
	if fragment.Migration != "0062_create_vps_create_idempotency.sql" {
		t.Fatalf("eleventh fragment migration = %q", fragment.Migration)
	}

	wantObjects, err := canonicalAppACLManagedObjects(vpsCreateIdempotencyExpectedAppACLObjects())
	if err != nil {
		t.Fatalf("canonical expected objects error type = %T", err)
	}
	gotObjects, err := canonicalAppACLManagedObjects(fragment.Objects)
	if err != nil {
		t.Fatalf("canonical actual objects error type = %T", err)
	}
	if !reflect.DeepEqual(gotObjects, wantObjects) {
		t.Fatalf("managed object counts = %d/%d and values differ", len(gotObjects), len(wantObjects))
	}
	if len(fragment.Functions) != 0 || len(fragment.AuxiliaryPrivileges) != 0 {
		t.Fatalf("function/auxiliary contract counts = %d/%d, want 0/0", len(fragment.Functions), len(fragment.AuxiliaryPrivileges))
	}

	gotPrivileges, err := canonicalPrivileges(fragment.Privileges)
	if err != nil {
		t.Fatalf("canonical actual privileges error type = %T", err)
	}
	wantPrivileges, err := canonicalPrivileges(vpsCreateIdempotencyExpectedAppACLPrivileges())
	if err != nil {
		t.Fatalf("canonical expected privileges error type = %T", err)
	}
	if !reflect.DeepEqual(gotPrivileges, wantPrivileges) {
		t.Fatalf("APP ACL privilege counts = %d/%d and values differ", len(gotPrivileges), len(wantPrivileges))
	}
	if len(gotPrivileges) != 8 {
		t.Fatalf("APP ACL privilege count = %d, want four tables x SELECT/INSERT", len(gotPrivileges))
	}
	for _, privilege := range gotPrivileges {
		if privilege.Privilege != AppACLPrivilegeSelect && privilege.Privilege != AppACLPrivilegeInsert {
			t.Fatalf("receipt privilege %q is outside SELECT/INSERT", privilege.Privilege)
		}
	}
}

func vpsCreateIdempotencyExpectedAppACLObjects() []AppACLManagedObjectR1 {
	objects := make([]AppACLManagedObjectR1, 0, 4)
	for _, table := range vpsCreateIdempotencyReceiptTables {
		objects = append(objects, AppACLManagedObjectR1{
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
		})
	}
	return objects
}

func vpsCreateIdempotencyExpectedAppACLPrivileges() []AppACLPrivilege {
	return vpsCreateIdempotencyAppACLCurrentPrivileges("")
}
