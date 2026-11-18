package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/deploy"
)

const validSourceEnrollmentRequest = `{
  "config_path":"/private/source.json",
  "config":{
    "deployment_id":"deployment",
    "witness":{"id":"domain","system_identifier":"123","database_oid":1,"database_name":"witness","epoch":1},
    "roles":{"center_runtime":"runtime","platform_admin":"admin","deletion_worker":"worker","migrator":"migrator"},
    "authority_role":"authority",
    "passwords":{"runtime":"runtime-secret","platform_admin":"admin-secret","deletion_worker":"worker-secret","migrator":"migrator-secret","authority":"authority-secret"},
    "producer":{"principal":"producer","password":"producer-secret"}
  },
  "membership":{"instance_id":"instance","deployment_id":"deployment","project_id":"project","instance_kind":"api","capability":"records.runtime"}
}`

func TestSourceEnrollmentStrictRequestSchema(t *testing.T) {
	validProvision := `{"config_path":"/private/source.json","witness":{"id":"domain","system_identifier":"123","database_oid":1,"database_name":"witness","epoch":1},"reader_database_url":"postgres://reader:secret@localhost:5432/witness?sslmode=disable"}`
	for _, tc := range []struct {
		name   string
		body   string
		schema sourceRequestSchema
		valid  bool
	}{
		{"enrollment", validSourceEnrollmentRequest, sourceEnrollSchema, true},
		{"provision", validProvision, sourceProvisionSchema, true},
		{"duplicate", strings.Replace(validSourceEnrollmentRequest, `"producer-secret"`, `"producer-secret","password":"second-secret"`, 1), sourceEnrollSchema, false},
		{"case alias", strings.Replace(validSourceEnrollmentRequest, `"producer-secret"`, `"producer-secret","Password":"second-secret"`, 1), sourceEnrollSchema, false},
		{"wrong nested case", strings.Replace(validSourceEnrollmentRequest, `"system_identifier"`, `"System_Identifier"`, 1), sourceEnrollSchema, false},
		{"unknown nested", strings.Replace(validSourceEnrollmentRequest, `"principal":"producer"`, `"principal":"producer","unknown":"secret"`, 1), sourceEnrollSchema, false},
		{"missing nested", strings.Replace(validSourceEnrollmentRequest, `"authority":"authority-secret"`, `"unknown":"secret"`, 1), sourceEnrollSchema, false},
		{"null nested", strings.Replace(validSourceEnrollmentRequest, `"producer-secret"`, `null`, 1), sourceEnrollSchema, false},
		{"null object", strings.Replace(validSourceEnrollmentRequest, `{"principal":"producer","password":"producer-secret"}`, `null`, 1), sourceEnrollSchema, false},
		{"trailing", validSourceEnrollmentRequest + `{}`, sourceEnrollSchema, false},
		{"unknown top", strings.TrimSuffix(validProvision, "}") + `,"secret":"value"}`, sourceProvisionSchema, false},
		{"missing witness field", strings.Replace(validProvision, `,"epoch":1`, ``, 1), sourceProvisionSchema, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var destination any = &sourceEnrollmentRequest{}
			if tc.schema == nil {
				t.Fatal("missing test schema")
			}
			if tc.name == "provision" || tc.name == "unknown top" || tc.name == "missing witness field" {
				destination = &sourceMaintenanceProvisionRequest{}
			}
			err := decodeSourceEnrollmentRequest([]byte(tc.body), destination, tc.schema)
			if (err == nil) != tc.valid {
				t.Fatalf("decode error = %v, valid = %v", err, tc.valid)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("error disclosed credential: %v", err)
			}
		})
	}
	var request sourceEnrollmentRequest
	if err := decodeSourceEnrollmentRequest([]byte(validSourceEnrollmentRequest), &request, sourceEnrollSchema); err != nil {
		t.Fatal(err)
	}
	config := request.Config.enrollmentConfig()
	if config.Passwords.Runtime != "runtime-secret" || config.Producer.Password != "producer-secret" || config.Roles.DeletionWorker != "worker" || config.Witness.SystemIdentifier != "123" {
		t.Fatal("request mapping lost enrollment fields")
	}
}

func TestSourceEnrollmentCredentialRequestFileProtection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credential-secret.json")
	if err := os.WriteFile(path, []byte(validSourceEnrollmentRequest), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := readSourceEnrollmentRequest(path); err != nil {
			t.Fatalf("private mode %o rejected: %v", mode, err)
		}
	}
	for _, mode := range []os.FileMode{0o640, 0o644, 0o620, 0o000, 0o700} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := readSourceEnrollmentRequest(path); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe mode %o: %v", mode, err)
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{link, dir, filepath.Join(dir, "missing")} {
		if _, err := readSourceEnrollmentRequest(unsafe); err == nil || strings.Contains(err.Error(), unsafe) {
			t.Fatalf("unsafe path accepted or disclosed: %v", err)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", sourceRetirementFileLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSourceEnrollmentRequest(path); err == nil {
		t.Fatal("oversized credential request accepted")
	}
}

func TestSourceEnrollmentRequiresExistingMaintenanceConfig(t *testing.T) {
	request := sourceEnrollmentRequest{ConfigPath: filepath.Join(t.TempDir(), "missing")}
	err := enrollSourceOffline(context.Background(), request)
	if err != errSourceEnrollmentOperation || !strings.Contains(err.Error(), "external offline fencing") {
		t.Fatalf("missing maintenance config: %v", err)
	}
}

func TestSourceMaintenanceConfigPath(t *testing.T) {
	t.Setenv(sourceMaintenanceConfigEnv, "")
	if got := sourceMaintenanceConfigPath(); got != deploy.SourceMaintenanceConfigPath {
		t.Fatalf("default config path = %q", got)
	}
	t.Setenv(sourceMaintenanceConfigEnv, "/private/guard.json")
	if got := sourceMaintenanceConfigPath(); got != "/private/guard.json" {
		t.Fatalf("explicit config path = %q", got)
	}
}

func TestSourceEnrollmentRejectsMalformedInvocation(t *testing.T) {
	for _, args := range [][]string{
		{"source-authority", "enroll-offline", "--fenced", "true"},
		{"source-authority", "maintenance-provision", "--request-file"},
		{"source-authority", "enroll-offline", "--request-file", ""},
		{"source-authority", "enroll-offline", "--request-file", "secret", "--fenced"},
	} {
		err := runSourceAuthority(context.Background(), args)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("malformed invocation: %v", err)
		}
	}
}

func TestSourceComposeEnrollmentStrictRequest(t *testing.T) {
	valid := strings.TrimSuffix(validSourceEnrollmentRequest, "}") + `,
		"application":{"id":"domain","system_identifier":"456","database_oid":2,"database_name":"houfeng","epoch":1},
		"fence":{"recovery_id":"rid","project":"project","working_directory":"/private","database_container_id":"container","control_config_path":"/private/source.json"}}`
	for _, tc := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"valid", valid, true},
		{"missing application", validSourceEnrollmentRequest, false},
		{"unknown fence field", strings.Replace(valid, `"recovery_id":"rid"`, `"recovery_id":"rid","fenced":true`, 1), false},
		{"case alias", strings.Replace(valid, `"project":"project"`, `"Project":"project"`, 1), false},
		{"duplicate", strings.Replace(valid, `"recovery_id":"rid"`, `"recovery_id":"rid","recovery_id":"other"`, 1), false},
		{"missing application identity", strings.Replace(valid, `"system_identifier":"456",`, ``, 1), false},
		{"null fence", strings.Replace(valid, `"fence":{`, `"fence":null,"bad":{`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request sourceComposeEnrollmentRequest
			err := decodeSourceEnrollmentRequest([]byte(tc.body), &request, sourceComposeEnrollSchema)
			if (err == nil) != tc.valid {
				t.Fatalf("decode error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestSourceComposeAppConfigPinsEveryFallback(t *testing.T) {
	original, err := pgxpool.ParseConfig("postgres://postgres:secret@original.example:6543/houfeng?sslmode=prefer")
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := sourceComposeAppConfig(original, "172.30.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if pinned.MaxConns != 1 || pinned.MinConns != 0 || pinned.ConnConfig.Host != "172.30.0.2" || pinned.ConnConfig.Port != 5432 {
		t.Fatal("primary connection is not pinned to isolated DB")
	}
	if len(pinned.ConnConfig.Fallbacks) == 0 {
		t.Fatal("test requires a TLS fallback")
	}
	for _, fallback := range pinned.ConnConfig.Fallbacks {
		if fallback.Host != "172.30.0.2" || fallback.Port != 5432 {
			t.Fatal("fallback can escape isolated DB")
		}
	}
	if original.ConnConfig.Host != "original.example" || original.ConnConfig.Port != 6543 {
		t.Fatal("pinning mutated the source configuration")
	}
	for _, fallback := range original.ConnConfig.Fallbacks {
		if fallback.Host != "original.example" || fallback.Port != 6543 {
			t.Fatal("pinning mutated a source fallback")
		}
	}
	for _, address := range []string{"", "original.example", "127.0.0.1:5432"} {
		if _, err := sourceComposeAppConfig(original, address); err == nil {
			t.Fatalf("invalid isolated address %q accepted", address)
		}
	}
	wrongUser := original.Copy()
	wrongUser.ConnConfig.User = "other"
	if _, err := sourceComposeAppConfig(wrongUser, "172.30.0.2"); err == nil {
		t.Fatal("non-bootstrap user accepted")
	}
	wrongDB := original.Copy()
	wrongDB.ConnConfig.Database = "other"
	if _, err := sourceComposeAppConfig(wrongDB, "172.30.0.2"); err == nil {
		t.Fatal("wrong application database accepted")
	}
}

func TestSourceComposeRejectsDifferentControlPathBeforeFencing(t *testing.T) {
	request := sourceComposeEnrollmentRequest{
		ConfigPath: "/private/source.json",
		Fence:      sourceComposeFenceRequest{ControlConfigPath: "/other/source.json"},
	}
	if err := enrollSourceCompose(context.Background(), request); err != errSourceEnrollmentInput {
		t.Fatalf("different control path returned %v", err)
	}
}

func TestSourceComposeRejectsInvalidCredentialSet(t *testing.T) {
	var request sourceEnrollmentRequest
	if err := decodeSourceEnrollmentRequest([]byte(validSourceEnrollmentRequest), &request, sourceEnrollSchema); err != nil {
		t.Fatal(err)
	}
	config := request.Config.enrollmentConfig()
	if !sourceComposeCredentialsValid(config) {
		t.Fatal("valid distinct credential set rejected")
	}
	config.Producer.Principal = config.Roles.PlatformAdmin
	if sourceComposeCredentialsValid(config) {
		t.Fatal("duplicate producer principal accepted")
	}
	config = request.Config.enrollmentConfig()
	config.Passwords.Authority = ""
	if sourceComposeCredentialsValid(config) {
		t.Fatal("empty password accepted")
	}
}
