package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	centerconfig "houfeng/internal/center/config"
)

const (
	birthProducerURLKey        = "HOUFENG_RECORD_BIRTH_PRODUCER_DATABASE_URL"
	birthAuthorityIdentityKey  = "HOUFENG_RECORD_BIRTH_AUTHORITY_IDENTITY_FILE"
	birthProducerCredentialKey = "HOUFENG_RECORD_BIRTH_PRODUCER_CREDENTIAL_FILE"
	birthReaderURLKey          = "HOUFENG_RECORD_BIRTH_READER_DATABASE_URL"
	birthReaderIdentityKey     = "HOUFENG_RECORD_BIRTH_READER_IDENTITY_FILE"
	birthReaderCredentialKey   = "HOUFENG_RECORD_BIRTH_READER_CREDENTIAL_FILE"
	birthLaunchFileKey         = "HOUFENG_RECORD_BIRTH_LAUNCH_FILE"
)

func clearBirthProducerEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{birthProducerURLKey, birthAuthorityIdentityKey, birthProducerCredentialKey} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "PG") || key == "SSL_CERT_FILE" || key == "SSL_CERT_DIR" {
			t.Setenv(key, "")
		}
	}
}

func unsetBirthTestEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func clearBirthReaderEnvironment(t *testing.T) {
	t.Helper()
	unsetBirthTestEnv(t, birthReaderURLKey, birthReaderIdentityKey, birthReaderCredentialKey)
}

func configureBirthCenterTest(t *testing.T) (string, string) {
	t.Helper()
	clearBirthProducerEnvironment(t)
	clearBirthReaderEnvironment(t)
	unsetBirthTestEnv(t, birthLaunchFileKey)
	setRequiredAuth(t)
	setRequiredLocalAttachments(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime")
	t.Setenv("HOUFENG_DATABASE_REQUIRE_TLS", "false")
	t.Setenv("HOUFENG_COMPARISON_ENABLED", "false")
	t.Setenv("HOUFENG_PORTABILITY_ENABLED", "false")
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")
	credential := filepath.Join(t.TempDir(), "birth-password")
	if err := os.WriteFile(credential, []byte("birth-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(t.TempDir(), "authority-identity.json")
	identityBody := `{"ID":"rd-` + strings.Repeat("a", 64) + `","SystemIdentifier":"12345","DatabaseOID":16384,"DatabaseName":"witness","Epoch":1}`
	if err := os.WriteFile(identity, []byte(identityBody), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(birthProducerURLKey, "postgres://birth_writer@127.0.0.1:5432/witness?sslmode=disable")
	t.Setenv(birthAuthorityIdentityKey, identity)
	t.Setenv(birthProducerCredentialKey, credential)
	return credential, identity
}

func configureBirthReaderTest(t *testing.T) (string, string) {
	t.Helper()
	configureBirthCenterTest(t)
	credential := filepath.Join(t.TempDir(), "reader-password")
	if err := os.WriteFile(credential, []byte("reader-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(t.TempDir(), "reader-identity.json")
	t.Setenv(birthReaderURLKey, "postgres://birth_reader@127.0.0.1:5432/witness?sslmode=disable")
	t.Setenv(birthReaderIdentityKey, identity)
	t.Setenv(birthReaderCredentialKey, credential)
	return credential, identity
}

func TestCenterBirthProducerConfiguration(t *testing.T) {
	t.Run("absent leaves producer unconfigured even with ambient postgres settings", func(t *testing.T) {
		configureBirthCenterTest(t)
		clearBirthProducerEnvironment(t)
		t.Setenv("PGOPTIONS", "-c role=other")
		cfg, err := centerconfig.LoadCenterConfig()
		if err != nil || cfg.BirthProducer.Connection != nil {
			t.Fatalf("ordinary config unexpectedly configured, error = %v", err)
		}
	})
	t.Run("configured producer parses explicit credentials", func(t *testing.T) {
		credential, identity := configureBirthCenterTest(t)
		readerCredential := filepath.Join(t.TempDir(), "reader-file-path-sentinel")
		if err := os.WriteFile(readerCredential, []byte("reader-file-content-sentinel\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		readerIdentity := filepath.Join(t.TempDir(), "reader-identity-path-sentinel")
		launchFile := filepath.Join(t.TempDir(), "birth-launch-path-sentinel")
		t.Setenv(birthReaderURLKey, "postgres://reader_sentinel@reader-dsn-sentinel.invalid:5432/witness?sslmode=disable")
		t.Setenv(birthReaderIdentityKey, readerIdentity)
		t.Setenv(birthReaderCredentialKey, readerCredential)
		t.Setenv(birthLaunchFileKey, launchFile)
		cfg, err := centerconfig.LoadCenterConfig()
		if err != nil {
			t.Fatal(err)
		}
		connection := cfg.BirthProducer.Connection
		if connection == nil || connection.ConnConfig.User != "birth_writer" || connection.ConnConfig.Password != "birth-secret" ||
			connection.ConnConfig.Host != "127.0.0.1" || connection.ConnConfig.Port != 5432 || connection.ConnConfig.Database != "witness" ||
			cfg.BirthProducer.IdentityFile != identity || cfg.BirthProducer.CredentialFile != credential {
			t.Fatal("birth producer config did not preserve its dedicated connection and files")
		}
		if len(connection.ConnConfig.RuntimeParams) != 0 {
			t.Fatal("birth producer accepted runtime parameters")
		}
		for _, key := range []string{birthProducerURLKey, birthAuthorityIdentityKey, birthProducerCredentialKey} {
			item := deploymentItem(t, cfg.DeploymentSettings, key)
			if !item.Configured || !item.Sensitive || item.Value != nil {
				t.Errorf("birth setting %s is not redacted", key)
			}
		}
		for _, key := range []string{birthReaderURLKey, birthReaderIdentityKey, birthReaderCredentialKey, birthLaunchFileKey} {
			item := deploymentItem(t, cfg.DeploymentSettings, key)
			if !item.Configured || !item.Sensitive || item.Value != nil ||
				item.Source != centerconfig.DeploymentSettingSourceEnvironment ||
				item.Group != centerconfig.DeploymentSettingGroupRecords ||
				item.Application != centerconfig.DeploymentSettingApplicationStartup {
				t.Errorf("birth setting %s is not configured-only and redacted: %#v", key, item)
			}
		}
		body, err := json.Marshal(cfg.DeploymentSettings)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{
			"reader_sentinel", "reader-dsn-sentinel", "reader-file-path-sentinel",
			"reader-identity-path-sentinel", "reader-file-content-sentinel", "birth-launch-path-sentinel",
		} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("deployment snapshot disclosed birth setting %q", secret)
			}
		}
	})
	t.Run("owner read-only credential", func(t *testing.T) {
		credential, _ := configureBirthCenterTest(t)
		if err := os.Chmod(credential, 0o400); err != nil {
			t.Fatal(err)
		}
		cfg, err := centerconfig.LoadCenterConfig()
		if err != nil || cfg.BirthProducer.Connection == nil {
			t.Fatalf("owner-private credential rejected: %v", err)
		}
	})
}

func TestCenterBirthProducerRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, string, string)
	}{
		{"missing URL", func(t *testing.T, _, _ string) { t.Setenv(birthProducerURLKey, "") }},
		{"missing identity", func(t *testing.T, _, _ string) { t.Setenv(birthAuthorityIdentityKey, "") }},
		{"missing credential", func(t *testing.T, _, _ string) { t.Setenv(birthProducerCredentialKey, "") }},
		{"records disabled", func(t *testing.T, _, _ string) { t.Setenv("HOUFENG_RECORDS_ENABLED", "false") }},
		{"embedded password", func(t *testing.T, _, _ string) {
			t.Setenv(birthProducerURLKey, "postgres://birth_writer:embedded-secret@127.0.0.1:5432/witness?sslmode=disable")
		}},
		{"URL options", func(t *testing.T, _, _ string) {
			t.Setenv(birthProducerURLKey, "postgres://birth_writer@127.0.0.1:5432/witness?sslmode=disable&options=-c%20role%3Dother")
		}},
		{"URL role", func(t *testing.T, _, _ string) {
			t.Setenv(birthProducerURLKey, "postgres://birth_writer@127.0.0.1:5432/witness?sslmode=disable&role=other")
		}},
		{"missing TLS mode", func(t *testing.T, _, _ string) {
			t.Setenv(birthProducerURLKey, "postgres://birth_writer@127.0.0.1:5432/witness")
		}},
		{"ambient postgres override", func(t *testing.T, _, _ string) { t.Setenv("PGHOST", "ambient-secret") }},
		{"ambient postgres options", func(t *testing.T, _, _ string) { t.Setenv("PGOPTIONS", "-c role=other") }},
		{"ambient SSL override", func(t *testing.T, _, _ string) { t.Setenv("SSL_CERT_FILE", "ambient-secret") }},
		{"noncanonical principal", func(t *testing.T, _, _ string) {
			t.Setenv(birthProducerURLKey, "postgres://BirthWriter@127.0.0.1:5432/witness?sslmode=disable")
		}},
		{"overlong principal", func(t *testing.T, _, _ string) {
			t.Setenv(birthProducerURLKey, "postgres://"+strings.Repeat("a", 64)+"@127.0.0.1:5432/witness?sslmode=disable")
		}},
		{"missing credential file", func(t *testing.T, credential, _ string) { t.Setenv(birthProducerCredentialKey, credential+"-missing") }},
		{"unstaged group-readable credential", func(t *testing.T, credential, _ string) {
			if err := os.Chmod(credential, 0o440); err != nil {
				t.Fatal(err)
			}
		}},
		{"readable credential", func(t *testing.T, credential, _ string) {
			if err := os.Chmod(credential, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink credential", func(t *testing.T, credential, _ string) {
			link := filepath.Join(t.TempDir(), "credential-link")
			if err := os.Symlink(credential, link); err != nil {
				t.Fatal(err)
			}
			t.Setenv(birthProducerCredentialKey, link)
		}},
		{"multiline credential", func(t *testing.T, credential, _ string) {
			if err := os.WriteFile(credential, []byte("birth-secret\nother\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			credential, identity := configureBirthCenterTest(t)
			test.mutate(t, credential, identity)
			_, err := centerconfig.LoadCenterConfig()
			if err == nil {
				t.Fatal("invalid birth producer configuration was accepted")
			}
			for _, secret := range []string{"birth-secret", "embedded-secret", "ambient-secret"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("configuration error disclosed a secret: %v", err)
				}
			}
		})
	}
}

func TestBirthReaderConfiguration(t *testing.T) {
	t.Run("all absent remains unset in both modes", func(t *testing.T) {
		configureBirthCenterTest(t)
		clearBirthReaderEnvironment(t)
		t.Setenv("PGOPTIONS", "-c role=other")
		for _, mode := range []centerconfig.RecordPlatformMode{
			centerconfig.RecordPlatformModeLegacy, centerconfig.RecordPlatformModeRuntimeAdmission,
		} {
			cfg, err := centerconfig.LoadBirthReaderConfig(mode)
			if err != nil || cfg.Connection != nil || cfg.IdentityFile != "" || cfg.CredentialFile != "" {
				t.Fatalf("absent birth reader should remain unset in mode %v: %v", mode, err)
			}
		}
	})

	t.Run("dedicated reader is loaded by center and processor parser", func(t *testing.T) {
		credential, identity := configureBirthReaderTest(t)
		reader, err := centerconfig.LoadBirthReaderConfig(centerconfig.RecordPlatformModeRuntimeAdmission)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := centerconfig.LoadCenterConfig()
		if err != nil {
			t.Fatal(err)
		}
		for _, got := range []centerconfig.BirthReaderConfig{reader, cfg.BirthReader} {
			if got.Connection == nil || got.Connection.ConnConfig.User != "birth_reader" ||
				got.Connection.ConnConfig.Password != "reader-secret" || got.Connection.ConnConfig.Host != "127.0.0.1" ||
				got.Connection.ConnConfig.Port != 5432 || got.Connection.ConnConfig.Database != "witness" ||
				got.IdentityFile != identity || got.CredentialFile != credential {
				t.Fatal("birth reader did not retain its separate connection and file bindings")
			}
			if len(got.Connection.ConnConfig.RuntimeParams) != 0 {
				t.Fatal("birth reader accepted runtime parameters")
			}
		}
		if cfg.BirthProducer.Connection == nil || cfg.BirthProducer.Connection.ConnConfig.User != "birth_writer" {
			t.Fatal("the independent producer was not retained")
		}
	})

	t.Run("reader works without producer", func(t *testing.T) {
		configureBirthReaderTest(t)
		clearBirthProducerEnvironment(t)
		cfg, err := centerconfig.LoadCenterConfig()
		if err != nil || cfg.BirthProducer.Connection != nil || cfg.BirthReader.Connection == nil {
			t.Fatalf("reader without producer configuration failed: %v", err)
		}
	})

	t.Run("owner read-only credential", func(t *testing.T) {
		credential, _ := configureBirthReaderTest(t)
		if err := os.Chmod(credential, 0o400); err != nil {
			t.Fatal(err)
		}
		reader, err := centerconfig.LoadBirthReaderConfig(centerconfig.RecordPlatformModeRuntimeAdmission)
		if err != nil || reader.Connection == nil {
			t.Fatalf("owner-private reader credential rejected: %v", err)
		}
	})
}

func TestBirthReaderRejectsInvalidConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, string, string)
	}{
		{"missing URL", func(t *testing.T, _, _ string) { unsetBirthTestEnv(t, birthReaderURLKey) }},
		{"missing identity", func(t *testing.T, _, _ string) { unsetBirthTestEnv(t, birthReaderIdentityKey) }},
		{"missing credential", func(t *testing.T, _, _ string) { unsetBirthTestEnv(t, birthReaderCredentialKey) }},
		{"only URL", func(t *testing.T, _, _ string) {
			unsetBirthTestEnv(t, birthReaderIdentityKey, birthReaderCredentialKey)
		}},
		{"only identity", func(t *testing.T, _, _ string) { unsetBirthTestEnv(t, birthReaderURLKey, birthReaderCredentialKey) }},
		{"only credential", func(t *testing.T, _, _ string) { unsetBirthTestEnv(t, birthReaderURLKey, birthReaderIdentityKey) }},
		{"empty URL", func(t *testing.T, _, _ string) { t.Setenv(birthReaderURLKey, "") }},
		{"empty identity", func(t *testing.T, _, _ string) { t.Setenv(birthReaderIdentityKey, " ") }},
		{"empty credential", func(t *testing.T, _, _ string) { t.Setenv(birthReaderCredentialKey, "") }},
		{"malformed URL", func(t *testing.T, _, _ string) { t.Setenv(birthReaderURLKey, "://bad") }},
		{"embedded password", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://birth_reader:embedded-secret@127.0.0.1:5432/witness?sslmode=disable")
		}},
		{"embedded empty password", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://birth_reader:@127.0.0.1:5432/witness?sslmode=disable")
		}},
		{"password query", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://birth_reader@127.0.0.1:5432/witness?sslmode=disable&password=embedded-secret")
		}},
		{"TLS password query", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://birth_reader@127.0.0.1:5432/witness?sslmode=disable&sslpassword=embedded-secret")
		}},
		{"URL options", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://birth_reader@127.0.0.1:5432/witness?sslmode=disable&options=-c%20role%3Dother")
		}},
		{"URL role", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://birth_reader@127.0.0.1:5432/witness?sslmode=disable&role=other")
		}},
		{"URL fragment", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://birth_reader@127.0.0.1:5432/witness?sslmode=disable#fragment")
		}},
		{"missing TLS mode", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://birth_reader@127.0.0.1:5432/witness")
		}},
		{"noncanonical principal", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://BirthReader@127.0.0.1:5432/witness?sslmode=disable")
		}},
		{"overlong principal", func(t *testing.T, _, _ string) {
			t.Setenv(birthReaderURLKey, "postgres://"+strings.Repeat("a", 64)+"@127.0.0.1:5432/witness?sslmode=disable")
		}},
		{"ambient postgres override", func(t *testing.T, _, _ string) { t.Setenv("PGHOST", "ambient-secret") }},
		{"ambient SSL override", func(t *testing.T, _, _ string) { t.Setenv("SSL_CERT_FILE", "ambient-secret") }},
		{"missing credential file", func(t *testing.T, credential, _ string) { t.Setenv(birthReaderCredentialKey, credential+"-missing") }},
		{"credential directory", func(t *testing.T, _, _ string) { t.Setenv(birthReaderCredentialKey, t.TempDir()) }},
		{"unstaged group-readable credential", func(t *testing.T, credential, _ string) {
			if err := os.Chmod(credential, 0o440); err != nil {
				t.Fatal(err)
			}
		}},
		{"world-readable credential", func(t *testing.T, credential, _ string) {
			if err := os.Chmod(credential, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink credential", func(t *testing.T, credential, _ string) {
			link := filepath.Join(t.TempDir(), "reader-link")
			if err := os.Symlink(credential, link); err != nil {
				t.Fatal(err)
			}
			t.Setenv(birthReaderCredentialKey, link)
		}},
		{"empty credential file", func(t *testing.T, credential, _ string) {
			if err := os.WriteFile(credential, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"multiline credential", func(t *testing.T, credential, _ string) {
			if err := os.WriteFile(credential, []byte("reader-secret\nother\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"oversized credential", func(t *testing.T, credential, _ string) {
			if err := os.WriteFile(credential, []byte(strings.Repeat("x", 64*1024+1)), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			credential, identity := configureBirthReaderTest(t)
			test.mutate(t, credential, identity)
			_, readerErr := centerconfig.LoadBirthReaderConfig(centerconfig.RecordPlatformModeRuntimeAdmission)
			_, centerErr := centerconfig.LoadCenterConfig()
			if readerErr == nil || centerErr == nil {
				t.Fatalf("invalid birth reader accepted: reader error = %v, center error = %v", readerErr, centerErr)
			}
			for _, err := range []error{readerErr, centerErr} {
				for _, secret := range []string{"reader-secret", "embedded-secret", "ambient-secret"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("configuration error disclosed a secret: %v", err)
					}
				}
			}
		})
	}

	t.Run("configured reader requires runtime admission", func(t *testing.T) {
		configureBirthReaderTest(t)
		if _, err := centerconfig.LoadBirthReaderConfig(centerconfig.RecordPlatformModeLegacy); err == nil {
			t.Fatal("configured reader accepted in legacy mode")
		}
		clearBirthProducerEnvironment(t)
		t.Setenv("HOUFENG_RECORDS_ENABLED", "false")
		if _, err := centerconfig.LoadCenterConfig(); err == nil {
			t.Fatal("center accepted a configured reader with records disabled")
		}
	})
}

func TestCenterBirthReaderRequiresIndependentPrincipal(t *testing.T) {
	configureBirthReaderTest(t)
	// Role identity is independent of host or database naming.
	t.Setenv(birthReaderURLKey, "postgres://birth_writer@other-host:6432/other_database?sslmode=disable")
	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("center accepted the producer role as the birth reader")
	}
}

func TestCenterBirthLaunchFile(t *testing.T) {
	t.Run("runtime admission has a protected launch default", func(t *testing.T) {
		configureBirthCenterTest(t)
		clearBirthProducerEnvironment(t)
		cfg, err := centerconfig.LoadCenterConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.BirthLaunchFile != centerconfig.RecordBirthLaunchPath {
			t.Fatalf("unexpected launch binding path: %q", cfg.BirthLaunchFile)
		}
		if cfg.BirthProducer.Connection != nil || cfg.BirthReader.Connection != nil {
			t.Fatal("the launch path must not infer birth connections or installation state")
		}
		item := deploymentItem(t, cfg.DeploymentSettings, birthLaunchFileKey)
		if item.Source != centerconfig.DeploymentSettingSourceDefault || item.Configured ||
			!item.Sensitive || item.Value != nil || item.Application != centerconfig.DeploymentSettingApplicationStartup {
			t.Fatalf("default launch setting should be redacted and marked as default: %#v", item)
		}
	})

	t.Run("missing explicit file is left for runtime verification", func(t *testing.T) {
		configureBirthCenterTest(t)
		path := filepath.Join(t.TempDir(), "not-created", "birth-launch.json")
		t.Setenv(birthLaunchFileKey, path)
		cfg, err := centerconfig.LoadCenterConfig()
		if err != nil || cfg.BirthLaunchFile != path {
			t.Fatalf("configuration read or rejected the missing launch file: %v", err)
		}
	})

	t.Run("legacy does not require a launch setting", func(t *testing.T) {
		configureBirthCenterTest(t)
		clearBirthProducerEnvironment(t)
		t.Setenv("HOUFENG_RECORDS_ENABLED", "false")
		cfg, err := centerconfig.LoadCenterConfig()
		if err != nil || cfg.BirthLaunchFile != "" {
			t.Fatalf("legacy mode should not require a launch file: %v", err)
		}
		item := deploymentItem(t, cfg.DeploymentSettings, birthLaunchFileKey)
		if item.Source != centerconfig.DeploymentSettingSourceUnset || item.Configured ||
			!item.Sensitive || item.Value != nil || item.Application != centerconfig.DeploymentSettingApplicationInactive {
			t.Fatalf("absent legacy launch setting should be redacted and inactive: %#v", item)
		}
		path := filepath.Join(t.TempDir(), "not-created", "birth-launch.json")
		t.Setenv(birthLaunchFileKey, path)
		cfg, err = centerconfig.LoadCenterConfig()
		if err != nil || cfg.BirthLaunchFile != path {
			t.Fatalf("valid explicit legacy launch path was rejected: %v", err)
		}
		item = deploymentItem(t, cfg.DeploymentSettings, birthLaunchFileKey)
		if item.Source != centerconfig.DeploymentSettingSourceEnvironment || !item.Configured ||
			!item.Sensitive || item.Value != nil || item.Application != centerconfig.DeploymentSettingApplicationInactive {
			t.Fatalf("explicit legacy launch setting should be redacted and inactive: %#v", item)
		}
	})

	for _, test := range []struct {
		name string
		path string
	}{
		{"empty", ""},
		{"whitespace", "  "},
		{"relative", "birth-launch.json"},
		{"parent traversal", "/var/lib/houfeng/../birth-launch.json"},
		{"repeated separator", "/var/lib//birth-launch.json"},
		{"trailing separator", "/var/lib/houfeng/"},
		{"padded path", "/var/lib/houfeng/birth-launch.json "},
	} {
		for _, recordsEnabled := range []string{"true", "false"} {
			t.Run(test.name+"/records="+recordsEnabled, func(t *testing.T) {
				configureBirthCenterTest(t)
				clearBirthProducerEnvironment(t)
				t.Setenv("HOUFENG_RECORDS_ENABLED", recordsEnabled)
				t.Setenv(birthLaunchFileKey, test.path)
				if _, err := centerconfig.LoadCenterConfig(); err == nil || !strings.Contains(err.Error(), birthLaunchFileKey) {
					t.Fatalf("invalid explicit launch path accepted: %v", err)
				}
			})
		}
	}
}
