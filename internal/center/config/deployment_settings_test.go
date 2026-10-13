package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	centerconfig "houfeng/internal/center/config"
)

func setDeploymentConfigRequiredInputs(t *testing.T) {
	t.Helper()
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://snapshot-user:snapshot-dsn-secret@example.invalid:5432/houfeng")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "snapshot-admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "snapshot-password-secret")
	t.Setenv("HOUFENG_SESSION_HMAC_KEY", "snapshot-session-hmac-secret-0123456789")
	t.Setenv("HOUFENG_RECORDS_ENABLED", "false")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")
}

func deploymentItem(t *testing.T, snapshot centerconfig.DeploymentSettingsSnapshot, key string) centerconfig.DeploymentSettingItem {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("deployment snapshot missing %q", key)
	return centerconfig.DeploymentSettingItem{}
}

func TestLoadCenterConfigBuildsCompleteSafeDeploymentSnapshot(t *testing.T) {
	setDeploymentConfigRequiredInputs(t)
	passwordPath := filepath.Join(t.TempDir(), "initial-password")
	hmacPath := filepath.Join(t.TempDir(), "session-hmac")
	if err := os.WriteFile(passwordPath, []byte("file-password-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hmacPath, []byte("file-session-hmac-secret-0123456789\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOUFENG_INITIAL_PASSWORD_FILE", passwordPath)
	t.Setenv("HOUFENG_SESSION_HMAC_KEY_FILE", hmacPath)
	t.Setenv("HOUFENG_PUBLIC_BASE_URL", "https://user:public-url-secret@example.invalid/center")
	t.Setenv("HOUFENG_TELEGRAM_BOT_TOKEN", "telegram-token-secret")
	t.Setenv("HOUFENG_TELEGRAM_CHAT_ID", "chat-001")
	t.Setenv("HOUFENG_COMPARISON_INTENT_KEYRING", "comparison-keyring-secret-path")
	t.Setenv("HOUFENG_COMPARISON_INTENT_KEY_ID", "comparison-key-id")
	t.Setenv("HOUFENG_RECORD_DELETION_KEY_FILE", "/run/secrets/record-deletion-secret-path")
	t.Setenv("HOUFENG_BACKUP_HMAC_KEY_FILE", "/run/secrets/backup-hmac-secret-path")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}

	keys := []string{
		"HOUFENG_HTTP_ADDR", "HOUFENG_WEB_DIST_DIR", "HOUFENG_PUBLIC_BASE_URL", "HOUFENG_LOG_FILE", "HOUFENG_TRUSTED_PROXIES",
		"HOUFENG_DATABASE_URL", "HOUFENG_DATABASE_REQUIRE_TLS", "HOUFENG_TELEGRAM_BOT_TOKEN", "HOUFENG_TELEGRAM_CHAT_ID", "HOUFENG_INCIDENT_SWEEP_INTERVAL",
		"HOUFENG_INITIAL_USERNAME", "HOUFENG_INITIAL_DISPLAY_NAME", "HOUFENG_INITIAL_PASSWORD", "HOUFENG_SESSION_TTL", "HOUFENG_SESSION_HMAC_KEY", "HOUFENG_PASSWORD_BCRYPT_COST",
		"HOUFENG_RECORDS_ENABLED", "HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "HOUFENG_RECORD_INSTANCE_ID", "HOUFENG_RECORD_DEPLOYMENT_ID", "HOUFENG_RECORD_INSTANCE_KIND", "HOUFENG_RECORD_INSTANCE_CAPABILITY", "HOUFENG_PORTABILITY_ENABLED", "HOUFENG_COMPARISON_ENABLED", "HOUFENG_COMPARISON_INTENT_KEY_ID", "HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES", "HOUFENG_COMPARISON_INTENT_KEYRING",
		"HOUFENG_ATTACHMENT_BLOB_BACKEND", "HOUFENG_ATTACHMENT_BLOB_ROOT", "HOUFENG_ATTACHMENT_S3_ENDPOINT", "HOUFENG_ATTACHMENT_S3_BUCKET", "HOUFENG_ATTACHMENT_S3_SECURE", "HOUFENG_ATTACHMENT_S3_ACCESS_KEY", "HOUFENG_ATTACHMENT_S3_SECRET_KEY", "HOUFENG_CLAMAV_NETWORK", "HOUFENG_CLAMAV_ADDRESS", "HOUFENG_CLAMAV_DIAL_TIMEOUT", "HOUFENG_CLAMAV_OPERATION_TIMEOUT", "HOUFENG_CLAMAV_CHUNK_SIZE", "HOUFENG_CLAMAV_RESPONSE_LIMIT", "HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS",
		"HOUFENG_RECORD_DELETION_KEY_FILE", "HOUFENG_BACKUP_HMAC_KEY_FILE", "HOUFENG_DELETION_WITNESS_DATABASE_URL",
	}
	for _, key := range keys {
		deploymentItem(t, cfg.DeploymentSettings, key)
	}
	bootstrapSnapshot := cfg.DeploymentSettings.WithConfiguredOnly(
		"HOUFENG_DELETION_WITNESS_DATABASE_URL",
		centerconfig.DeploymentSettingGroupDatabase,
		centerconfig.DeploymentSettingApplicationStartup,
		true,
	)
	witness := deploymentItem(t, bootstrapSnapshot, "HOUFENG_DELETION_WITNESS_DATABASE_URL")
	if witness.Source != centerconfig.DeploymentSettingSourceEnvironment || !witness.Configured || witness.Value != nil || !witness.Sensitive || witness.Application != centerconfig.DeploymentSettingApplicationStartup {
		t.Fatalf("bootstrap witness item = %#v, want startup configured-only environment item", witness)
	}

	password := deploymentItem(t, cfg.DeploymentSettings, "HOUFENG_INITIAL_PASSWORD")
	if password.Source != centerconfig.DeploymentSettingSourceFile || !password.Configured || password.Value != nil || !password.Sensitive {
		t.Fatalf("initial password item = %#v, want file/configured/hidden/sensitive", password)
	}
	hmac := deploymentItem(t, cfg.DeploymentSettings, "HOUFENG_SESSION_HMAC_KEY")
	if hmac.Source != centerconfig.DeploymentSettingSourceFile || !hmac.Configured || hmac.Value != nil || !hmac.Sensitive {
		t.Fatalf("session HMAC item = %#v, want file/configured/hidden/sensitive", hmac)
	}
	publicURL := deploymentItem(t, cfg.DeploymentSettings, "HOUFENG_PUBLIC_BASE_URL")
	if publicURL.Value == nil || *publicURL.Value != "已设置" {
		t.Fatalf("public URL item = %#v, want safe placeholder for userinfo", publicURL)
	}
	recordIdentity := deploymentItem(t, cfg.DeploymentSettings, "HOUFENG_RECORD_INSTANCE_ID")
	if recordIdentity.Application != centerconfig.DeploymentSettingApplicationInactive {
		t.Fatalf("record identity item = %#v, want inactive when records are disabled", recordIdentity)
	}

	body, err := json.Marshal(cfg.DeploymentSettings)
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range []string{
		"snapshot-dsn-secret", "snapshot-password-secret", "snapshot-session-hmac-secret", "file-password-secret", "file-session-hmac-secret",
		"telegram-token-secret", "comparison-keyring-secret-path", "witness-dsn-secret", "record-deletion-secret-path", "backup-hmac-secret-path",
	} {
		if strings.Contains(string(body), sentinel) {
			t.Fatalf("deployment snapshot leaked %q: %s", sentinel, body)
		}
	}
}

func TestDeploymentSnapshotRemainsStableAfterProcessInputsChange(t *testing.T) {
	setDeploymentConfigRequiredInputs(t)
	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}
	before, err := json.Marshal(cfg.DeploymentSettings)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOUFENG_HTTP_ADDR", ":9999")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://changed-secret")
	t.Setenv("HOUFENG_TELEGRAM_BOT_TOKEN", "changed-token-secret")
	after, err := json.Marshal(cfg.DeploymentSettings)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("startup snapshot changed after environment mutation: before=%s after=%s", before, after)
	}
}

func TestDeploymentSnapshotMarksUnloadedStorageInactive(t *testing.T) {
	setDeploymentConfigRequiredInputs(t)
	t.Setenv("HOUFENG_ATTACHMENT_BLOB_BACKEND", "s3")
	t.Setenv("HOUFENG_ATTACHMENT_S3_ENDPOINT", "user:password@object.invalid:9000")
	t.Setenv("HOUFENG_CLAMAV_ADDRESS", "tcp://clam.invalid:3310")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}
	backend := deploymentItem(t, cfg.DeploymentSettings, "HOUFENG_ATTACHMENT_BLOB_BACKEND")
	if backend.Application != centerconfig.DeploymentSettingApplicationInactive || backend.Value == nil || *backend.Value != "s3" {
		t.Fatalf("inactive backend = %#v, want configured s3/inactive", backend)
	}
	endpoint := deploymentItem(t, cfg.DeploymentSettings, "HOUFENG_ATTACHMENT_S3_ENDPOINT")
	if endpoint.Application != centerconfig.DeploymentSettingApplicationInactive || endpoint.Value == nil || *endpoint.Value != "已设置" {
		t.Fatalf("inactive unsafe endpoint = %#v, want safe placeholder/inactive", endpoint)
	}
	clamav := deploymentItem(t, cfg.DeploymentSettings, "HOUFENG_CLAMAV_ADDRESS")
	if clamav.Application != centerconfig.DeploymentSettingApplicationInactive || clamav.Value == nil || *clamav.Value != "已设置" {
		t.Fatalf("inactive unsafe ClamAV address = %#v, want safe placeholder/inactive", clamav)
	}
}

func TestDeploymentSnapshotSanitizesStorageEndpointsWithoutChangingLoaderInputs(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{name: "host path", endpoint: "storage.example/api-secret-sentinel:9000", want: "已设置"},
		{name: "absolute path", endpoint: "/run/api-secret-sentinel.sock", want: "已设置"},
		{name: "bracketed non IP", endpoint: "[api-secret-sentinel]:9000", want: "已设置"},
		{name: "bracketed DNS", endpoint: "[storage.example]:9000", want: "已设置"},
		{name: "bracketed IPv4", endpoint: "[192.0.2.1]:9000", want: "已设置"},
		{name: "unbracketed IPv6", endpoint: "2001:db8::1:9000", want: "已设置"},
		{name: "userinfo", endpoint: "user:api-secret-sentinel@storage.example:9000", want: "已设置"},
		{name: "query", endpoint: "storage.example:9000?api-secret-sentinel", want: "已设置"},
		{name: "fragment", endpoint: "storage.example:9000#api-secret-sentinel", want: "已设置"},
		{name: "backslash path", endpoint: `storage.example\api-secret-sentinel:9000`, want: "已设置"},
		{name: "invalid DNS character", endpoint: "storage_example:9000", want: "已设置"},
		{name: "invalid DNS label", endpoint: "-storage.example:9000", want: "已设置"},
		{name: "empty DNS label", endpoint: "storage..example:9000", want: "已设置"},
		{name: "overlong DNS label", endpoint: strings.Repeat("a", 64) + ".example:9000", want: "已设置"},
		{name: "signed port", endpoint: "storage.example:+9000", want: "已设置"},
		{name: "zero port", endpoint: "storage.example:0", want: "已设置"},
		{name: "out of range port", endpoint: "storage.example:65536", want: "已设置"},
		{name: "named port", endpoint: "storage.example:https", want: "已设置"},
		{name: "missing port", endpoint: "storage.example", want: "已设置"},
		{name: "DNS", endpoint: "storage.example:9000", want: "storage.example:9000"},
		{name: "single label DNS", endpoint: "minio:9000", want: "minio:9000"},
		{name: "IPv4", endpoint: "192.0.2.1:9000", want: "192.0.2.1:9000"},
		{name: "IPv6", endpoint: "[2001:db8::1]:9000", want: "[2001:db8::1]:9000"},
		{name: "IPv6 loopback", endpoint: "[::1]:3310", want: "[::1]:3310"},
		{name: "maximum port", endpoint: "storage.example:65535", want: "storage.example:65535"},
	}
	for _, active := range []bool{false, true} {
		state := "inactive"
		if active {
			state = "active"
		}
		t.Run(state, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					setDeploymentConfigRequiredInputs(t)
					if active {
						t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
					}
					t.Setenv("HOUFENG_ATTACHMENT_BLOB_BACKEND", "s3")
					t.Setenv("HOUFENG_ATTACHMENT_S3_ENDPOINT", tt.endpoint)
					t.Setenv("HOUFENG_ATTACHMENT_S3_BUCKET", "snapshot-attachments")
					t.Setenv("HOUFENG_ATTACHMENT_S3_ACCESS_KEY", "s3-access-secret-sentinel")
					t.Setenv("HOUFENG_ATTACHMENT_S3_SECRET_KEY", "s3-secret-secret-sentinel")
					t.Setenv("HOUFENG_ATTACHMENT_S3_ACCESS_KEY_FILE", "")
					t.Setenv("HOUFENG_ATTACHMENT_S3_SECRET_KEY_FILE", "")
					t.Setenv("HOUFENG_CLAMAV_NETWORK", "tcp")
					t.Setenv("HOUFENG_CLAMAV_ADDRESS", tt.endpoint)

					cfg, err := centerconfig.LoadCenterConfig()
					if err != nil {
						t.Fatalf("LoadCenterConfig() changed loader acceptance: %v", err)
					}
					if active && (cfg.Attachment.S3Endpoint != tt.endpoint || cfg.Attachment.ClamAVAddress != tt.endpoint) {
						t.Fatalf("loader rewrote endpoints: S3=%q ClamAV=%q, want %q", cfg.Attachment.S3Endpoint, cfg.Attachment.ClamAVAddress, tt.endpoint)
					}
					application := centerconfig.DeploymentSettingApplicationInactive
					if active {
						application = centerconfig.DeploymentSettingApplicationStartup
					}
					for _, key := range []string{"HOUFENG_ATTACHMENT_S3_ENDPOINT", "HOUFENG_CLAMAV_ADDRESS"} {
						item := deploymentItem(t, cfg.DeploymentSettings, key)
						if !item.Configured || item.Value == nil || *item.Value != tt.want || item.Application != application {
							t.Errorf("%s = %#v, want configured %q/%s", key, item, tt.want, application)
						}
					}
					for _, item := range cfg.DeploymentSettings.Items {
						if item.Sensitive && item.Value != nil {
							t.Errorf("sensitive setting %s has non-null value", item.Key)
						}
					}
					for _, key := range []string{"HOUFENG_ATTACHMENT_S3_ACCESS_KEY", "HOUFENG_ATTACHMENT_S3_SECRET_KEY"} {
						item := deploymentItem(t, cfg.DeploymentSettings, key)
						if !item.Configured || !item.Sensitive || item.Value != nil {
							t.Errorf("%s = %#v, want configured sensitive null value", key, item)
						}
					}
					body, err := json.Marshal(cfg.DeploymentSettings)
					if err != nil {
						t.Fatal(err)
					}
					for _, sentinel := range []string{"api-secret-sentinel", "s3-access-secret-sentinel", "s3-secret-secret-sentinel"} {
						if strings.Contains(string(body), sentinel) {
							t.Errorf("deployment snapshot leaked %q", sentinel)
						}
					}
				})
			}
		})
	}
}

func TestDeploymentSnapshotRetainsUnixClamAVPathsOnlyForUnixNetwork(t *testing.T) {
	for _, active := range []bool{false, true} {
		state := "inactive"
		if active {
			state = "active"
		}
		for _, network := range []string{"unix", ""} {
			name := network
			if name == "" {
				name = "default Unix"
			}
			t.Run(state+"/"+name, func(t *testing.T) {
				setDeploymentConfigRequiredInputs(t)
				if active {
					t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
				}
				t.Setenv("HOUFENG_ATTACHMENT_BLOB_BACKEND", "local")
				t.Setenv("HOUFENG_ATTACHMENT_BLOB_ROOT", filepath.Join(t.TempDir(), "attachments"))
				t.Setenv("HOUFENG_CLAMAV_NETWORK", network)
				if network == "" {
					if err := os.Unsetenv("HOUFENG_CLAMAV_NETWORK"); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("HOUFENG_CLAMAV_ADDRESS", "/run/clamav/clamd.sock")
				cfg, err := centerconfig.LoadCenterConfig()
				if err != nil {
					t.Fatalf("LoadCenterConfig() error = %v", err)
				}
				item := deploymentItem(t, cfg.DeploymentSettings, "HOUFENG_CLAMAV_ADDRESS")
				application := centerconfig.DeploymentSettingApplicationInactive
				if active {
					application = centerconfig.DeploymentSettingApplicationStartup
				}
				if !item.Configured || item.Value == nil || *item.Value != "/run/clamav/clamd.sock" || item.Application != application {
					t.Fatalf("Unix ClamAV address = %#v, want configured Unix path/%s", item, application)
				}
			})
		}
	}
}
