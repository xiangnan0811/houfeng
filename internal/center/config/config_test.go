package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	centerconfig "houfeng/internal/center/config"
)

// setRequiredAuth sets the bcrypt-seed credentials so other Load* tests can
// focus on what they're asserting without LoadCenterConfig short-circuiting on
// missing initial creds.
func setRequiredAuth(t *testing.T) {
	t.Helper()
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "correct-horse-battery")
	t.Setenv("HOUFENG_SESSION_HMAC_KEY", "0123456789abcdef0123456789abcdef")
}

func setRequiredLocalAttachments(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "attachment-blobs")
	t.Setenv("HOUFENG_ATTACHMENT_BLOB_BACKEND", string(attachments.BackendKindLocal))
	t.Setenv("HOUFENG_ATTACHMENT_BLOB_ROOT", root)
	return root
}

func TestLoadRecordPlatformMode(t *testing.T) {
	tests := []struct {
		name                   string
		recordsEnabled         string
		permanentDeleteEnabled string
		want                   centerconfig.RecordPlatformMode
		wantErr                string
	}{
		{
			name:                   "legacy flags off",
			recordsEnabled:         "false",
			permanentDeleteEnabled: "false",
			want:                   centerconfig.RecordPlatformModeLegacy,
		},
		{
			name:                   "runtime admission only",
			recordsEnabled:         "true",
			permanentDeleteEnabled: "false",
			want:                   centerconfig.RecordPlatformModeRuntimeAdmission,
		},
		{
			name:                   "permanent delete without records",
			recordsEnabled:         "false",
			permanentDeleteEnabled: "true",
			wantErr:                "HOUFENG_RECORD_PERMANENT_DELETE_ENABLED requires HOUFENG_RECORDS_ENABLED=true",
		},
		{
			name:                   "permanent delete is not admitted",
			recordsEnabled:         "true",
			permanentDeleteEnabled: "true",
			wantErr:                "HOUFENG_RECORD_PERMANENT_DELETE_ENABLED=true is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOUFENG_RECORDS_ENABLED", tt.recordsEnabled)
			t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", tt.permanentDeleteEnabled)

			got, err := centerconfig.LoadRecordPlatformMode()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadRecordPlatformMode() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadRecordPlatformMode() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("LoadRecordPlatformMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadCenterConfigRejectsUnsupportedRecordPlatformModeBeforeOtherInput(t *testing.T) {
	tests := []struct {
		name                   string
		recordsEnabled         string
		permanentDeleteEnabled string
		wantErr                string
	}{
		{
			name:                   "delete without records",
			recordsEnabled:         "false",
			permanentDeleteEnabled: "true",
			wantErr:                "HOUFENG_RECORD_PERMANENT_DELETE_ENABLED requires HOUFENG_RECORDS_ENABLED=true",
		},
		{
			name:                   "delete with records",
			recordsEnabled:         "true",
			permanentDeleteEnabled: "true",
			wantErr:                "HOUFENG_RECORD_PERMANENT_DELETE_ENABLED=true is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOUFENG_RECORDS_ENABLED", tt.recordsEnabled)
			t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", tt.permanentDeleteEnabled)
			t.Setenv("HOUFENG_DATABASE_REQUIRE_TLS", "true")
			t.Setenv("HOUFENG_DATABASE_URL", "postgres://example?sslmode=disable")
			t.Setenv("HOUFENG_INITIAL_PASSWORD_FILE", filepath.Join(t.TempDir(), "missing-initial-password"))
			t.Setenv("HOUFENG_SESSION_HMAC_KEY_FILE", filepath.Join(t.TempDir(), "missing-session-hmac-key"))

			_, err := centerconfig.LoadCenterConfig()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadCenterConfig() error = %v, want record-platform mode rejection before URL or secret input", err)
			}
		})
	}
}

func TestLoadCenterConfigSelectsRuntimeAdmissionMode(t *testing.T) {
	setRequiredAuth(t)
	wantBlobRoot := setRequiredLocalAttachments(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime")
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}
	if cfg.RecordPlatformMode != centerconfig.RecordPlatformModeRuntimeAdmission {
		t.Fatalf("RecordPlatformMode = %v, want runtime admission", cfg.RecordPlatformMode)
	}
	if cfg.Attachment.BlobBackend != attachments.BackendKindLocal || cfg.Attachment.BlobRoot != wantBlobRoot {
		t.Fatalf("Attachment = %#v, want configured local backend", cfg.Attachment)
	}
	if cfg.ComparisonEnabled {
		t.Fatal("ComparisonEnabled = true, want default false")
	}
	if cfg.PortabilityEnabled {
		t.Fatal("PortabilityEnabled = true, want default false")
	}
}

func TestLoadCenterConfigComparisonEnabledRequiresRecords(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime")
	t.Setenv("HOUFENG_COMPARISON_ENABLED", "true")
	t.Setenv("HOUFENG_RECORDS_ENABLED", "false")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")

	if _, err := centerconfig.LoadCenterConfig(); err == nil || !strings.Contains(err.Error(), "HOUFENG_COMPARISON_ENABLED requires HOUFENG_RECORDS_ENABLED=true") {
		t.Fatalf("LoadCenterConfig() error = %v, want comparison stacked on records", err)
	}

	setRequiredLocalAttachments(t)
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	if _, err := centerconfig.LoadCenterConfig(); err == nil || !strings.Contains(err.Error(), "HOUFENG_COMPARISON_ENABLED requires HOUFENG_COMPARISON_INTENT_KEYRING") {
		t.Fatalf("LoadCenterConfig() error = %v, want keyring required when comparison is enabled", err)
	}
	t.Setenv("HOUFENG_COMPARISON_INTENT_KEYRING", t.TempDir())
	t.Setenv("HOUFENG_COMPARISON_INTENT_KEY_ID", "cmp_local")
	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}
	if !cfg.ComparisonEnabled {
		t.Fatal("ComparisonEnabled = false, want true when records and comparison are enabled")
	}
	if cfg.ComparisonAdmissionBudget != 64<<20 {
		t.Fatalf("ComparisonAdmissionBudget = %d, want 64 MiB default", cfg.ComparisonAdmissionBudget)
	}
}

func TestLoadCenterConfigPortabilityEnabledRequiresRecords(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime")
	t.Setenv("HOUFENG_PORTABILITY_ENABLED", "true")
	t.Setenv("HOUFENG_RECORDS_ENABLED", "false")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")

	if _, err := centerconfig.LoadCenterConfig(); err == nil || !strings.Contains(err.Error(), "HOUFENG_PORTABILITY_ENABLED requires HOUFENG_RECORDS_ENABLED=true") {
		t.Fatalf("LoadCenterConfig() error = %v, want portability stacked on records", err)
	}

	setRequiredLocalAttachments(t)
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}
	if !cfg.PortabilityEnabled {
		t.Fatal("PortabilityEnabled = false, want true when records and portability are enabled")
	}
}

func TestLoadCenterConfigComparisonIntentKeyringRequiresKeyID(t *testing.T) {
	setRequiredAuth(t)
	setRequiredLocalAttachments(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime")
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")
	t.Setenv("HOUFENG_COMPARISON_ENABLED", "true")
	t.Setenv("HOUFENG_COMPARISON_INTENT_KEYRING", t.TempDir())

	if _, err := centerconfig.LoadCenterConfig(); err == nil || !strings.Contains(err.Error(), "HOUFENG_COMPARISON_ENABLED requires HOUFENG_COMPARISON_INTENT_KEYRING") {
		t.Fatalf("LoadCenterConfig() error = %v, want keyring required when comparison is enabled", err)
	}
}

func TestLoadCenterConfigRequiresAttachmentConfigurationOnlyForRecordsRuntime(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime")
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")
	t.Setenv("HOUFENG_ATTACHMENT_BLOB_BACKEND", "")

	if _, err := centerconfig.LoadCenterConfig(); err == nil || !strings.Contains(err.Error(), "HOUFENG_ATTACHMENT_BLOB_BACKEND") {
		t.Fatalf("LoadCenterConfig() error = %v, want missing explicit attachment backend", err)
	}

	t.Setenv("HOUFENG_RECORDS_ENABLED", "false")
	if _, err := centerconfig.LoadCenterConfig(); err != nil {
		t.Fatalf("LoadCenterConfig() legacy error = %v, want attachment config ignored", err)
	}
}

func TestLoadAttachmentConfigLoadsPrivateLocalStorageAndSharedBounds(t *testing.T) {
	wantRoot := setRequiredLocalAttachments(t)
	t.Setenv("HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS", "7")
	t.Setenv("HOUFENG_CLAMAV_NETWORK", "tcp")
	t.Setenv("HOUFENG_CLAMAV_ADDRESS", "clamav.internal:3310")
	t.Setenv("HOUFENG_CLAMAV_DIAL_TIMEOUT", "3s")
	t.Setenv("HOUFENG_CLAMAV_OPERATION_TIMEOUT", "45s")
	t.Setenv("HOUFENG_CLAMAV_CHUNK_SIZE", "32768")
	t.Setenv("HOUFENG_CLAMAV_RESPONSE_LIMIT", "2048")

	cfg, err := centerconfig.LoadAttachmentConfig()
	if err != nil {
		t.Fatalf("LoadAttachmentConfig() error = %v", err)
	}
	if cfg.BlobBackend != attachments.BackendKindLocal || cfg.BlobRoot != wantRoot {
		t.Fatalf("local storage = (%q, %q), want (%q, %q)", cfg.BlobBackend, cfg.BlobRoot, attachments.BackendKindLocal, wantRoot)
	}
	if cfg.ProcessorMaxAttempts != 7 || cfg.Limits != attachments.DefaultLimits() {
		t.Fatalf("processor bounds = attempts %d limits %#v", cfg.ProcessorMaxAttempts, cfg.Limits)
	}
	if cfg.ClamAVNetwork != "tcp" || cfg.ClamAVAddress != "clamav.internal:3310" ||
		cfg.ClamAVDialTimeout != 3*time.Second || cfg.ClamAVOperationTimeout != 45*time.Second ||
		cfg.ClamAVChunkSize != 32768 || cfg.ClamAVResponseLimit != 2048 {
		t.Fatalf("ClamAV config = %#v, want configured bounded probe", cfg)
	}
}

func TestLoadAttachmentConfigReadsS3SecretsFromFiles(t *testing.T) {
	t.Setenv("HOUFENG_ATTACHMENT_BLOB_BACKEND", string(attachments.BackendKindS3))
	t.Setenv("HOUFENG_ATTACHMENT_S3_ENDPOINT", "minio.internal:9000")
	t.Setenv("HOUFENG_ATTACHMENT_S3_ACCESS_KEY", "environment-access-must-not-win")
	t.Setenv("HOUFENG_ATTACHMENT_S3_SECRET_KEY", "environment-secret-must-not-win")
	t.Setenv("HOUFENG_ATTACHMENT_S3_BUCKET", "houfeng-attachments")
	t.Setenv("HOUFENG_ATTACHMENT_S3_SECURE", "true")
	accessPath := filepath.Join(t.TempDir(), "s3-access-key")
	secretPath := filepath.Join(t.TempDir(), "s3-secret-key")
	if err := os.WriteFile(accessPath, []byte(" file-access \n"), 0o600); err != nil {
		t.Fatalf("write S3 access key: %v", err)
	}
	if err := os.WriteFile(secretPath, []byte(" file-secret \n"), 0o600); err != nil {
		t.Fatalf("write S3 secret key: %v", err)
	}
	t.Setenv("HOUFENG_ATTACHMENT_S3_ACCESS_KEY_FILE", accessPath)
	t.Setenv("HOUFENG_ATTACHMENT_S3_SECRET_KEY_FILE", secretPath)

	cfg, err := centerconfig.LoadAttachmentConfig()
	if err != nil {
		t.Fatalf("LoadAttachmentConfig() error = %v", err)
	}
	if cfg.S3Endpoint != "minio.internal:9000" || cfg.S3AccessKey != "file-access" ||
		cfg.S3SecretKey != "file-secret" || cfg.S3Bucket != "houfeng-attachments" || !cfg.S3Secure {
		t.Fatalf("S3 config = %#v, want file-backed private credentials", cfg)
	}
}

func TestLoadAttachmentConfigRejectsUnsafeOrUnboundedValues(t *testing.T) {
	tests := []struct {
		name    string
		values  map[string]string
		wantErr string
	}{
		{name: "backend is explicit", values: map[string]string{"HOUFENG_ATTACHMENT_BLOB_BACKEND": ""}, wantErr: "HOUFENG_ATTACHMENT_BLOB_BACKEND"},
		{name: "local root is absolute", values: map[string]string{"HOUFENG_ATTACHMENT_BLOB_ROOT": "relative/blobs"}, wantErr: "HOUFENG_ATTACHMENT_BLOB_ROOT"},
		{name: "local root is not filesystem root", values: map[string]string{"HOUFENG_ATTACHMENT_BLOB_ROOT": "/"}, wantErr: "HOUFENG_ATTACHMENT_BLOB_ROOT"},
		{name: "attempts are positive", values: map[string]string{"HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS": "0"}, wantErr: "HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS"},
		{name: "attempts are bounded", values: map[string]string{"HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS": "101"}, wantErr: "HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS"},
		{name: "scanner network is closed", values: map[string]string{"HOUFENG_CLAMAV_ADDRESS": "clamav:3310", "HOUFENG_CLAMAV_NETWORK": "udp"}, wantErr: "HOUFENG_CLAMAV_NETWORK"},
		{name: "scanner timeout is positive", values: map[string]string{"HOUFENG_CLAMAV_ADDRESS": "clamav:3310", "HOUFENG_CLAMAV_NETWORK": "tcp", "HOUFENG_CLAMAV_DIAL_TIMEOUT": "0s"}, wantErr: "HOUFENG_CLAMAV_DIAL_TIMEOUT"},
		{name: "scanner chunk is bounded", values: map[string]string{"HOUFENG_CLAMAV_ADDRESS": "clamav:3310", "HOUFENG_CLAMAV_NETWORK": "tcp", "HOUFENG_CLAMAV_CHUNK_SIZE": "1048577"}, wantErr: "HOUFENG_CLAMAV_CHUNK_SIZE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredLocalAttachments(t)
			for key, value := range tt.values {
				t.Setenv(key, value)
			}
			if _, err := centerconfig.LoadAttachmentConfig(); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadAttachmentConfig() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadCenterConfigRequiresDatabaseURL(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_WEB_DIST_DIR", "web/dist")
	t.Setenv("HOUFENG_DATABASE_URL", "")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil")
	}
}

func TestLoadCenterConfigParsesOptionalIncidentAndTelegramSettings(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_HTTP_ADDR", ":9090")
	t.Setenv("HOUFENG_WEB_DIST_DIR", "web/custom-dist")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INCIDENT_SWEEP_INTERVAL", "90s")
	t.Setenv("HOUFENG_LOG_FILE", " /var/log/houfeng/center.log ")
	t.Setenv("HOUFENG_TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("HOUFENG_TELEGRAM_CHAT_ID", "chat-001")
	t.Setenv("HOUFENG_TRUSTED_PROXIES", "127.0.0.1/32, 10.0.0.0/8")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}
	if cfg.HTTPAddr != ":9090" || cfg.WebDistDir != "web/custom-dist" {
		t.Fatalf("cfg = %#v, want parsed optional config", cfg)
	}
	if cfg.IncidentSweepInterval.Seconds() != 90 {
		t.Fatalf("IncidentSweepInterval = %s, want 90s", cfg.IncidentSweepInterval)
	}
	if cfg.LogFile != "/var/log/houfeng/center.log" {
		t.Fatalf("LogFile = %q, want trimmed file path", cfg.LogFile)
	}
	if cfg.TelegramBotToken != "bot-token" || cfg.TelegramChatID != "chat-001" {
		t.Fatalf("Telegram config = %#v, want populated values", cfg)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "127.0.0.1/32" || cfg.TrustedProxies[1] != "10.0.0.0/8" {
		t.Fatalf("TrustedProxies = %#v, want parsed CIDRs", cfg.TrustedProxies)
	}
}

func TestLoadCenterConfigRejectsInvalidTrustedProxyCIDR(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_TRUSTED_PROXIES", "127.0.0.1/32,not-a-cidr")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for invalid trusted proxy CIDR")
	}
}

func TestLoadCenterConfigRejectsOverbroadTrustedProxyCIDR(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")

	for _, value := range []string{"0.0.0.0/0", "::/0"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("HOUFENG_TRUSTED_PROXIES", value)
			if _, err := centerconfig.LoadCenterConfig(); err == nil {
				t.Fatal("LoadCenterConfig() error = nil, want non-nil for overbroad trusted proxy CIDR")
			}
		})
	}
}

func TestLoadCenterConfigParsesPublicBaseURL(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_PUBLIC_BASE_URL", " https://center.example.com/ ")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}
	if cfg.PublicBaseURL != "https://center.example.com" {
		t.Fatalf("PublicBaseURL = %q, want trimmed absolute URL without trailing slash", cfg.PublicBaseURL)
	}
}

func TestLoadCenterConfigAcceptsPublicIPPortBaseURL(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_PUBLIC_BASE_URL", "http://192.0.2.10:8080/")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}
	if cfg.PublicBaseURL != "http://192.0.2.10:8080" {
		t.Fatalf("PublicBaseURL = %q, want IP:port URL", cfg.PublicBaseURL)
	}
}

func TestLoadCenterConfigRejectsInvalidPublicBaseURL(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")

	for _, value := range []string{"center.example.com", "ftp://center.example.com", "https://", "https://center.example.com?token=bad"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("HOUFENG_PUBLIC_BASE_URL", value)
			if _, err := centerconfig.LoadCenterConfig(); err == nil {
				t.Fatal("LoadCenterConfig() error = nil, want non-nil for invalid public base URL")
			}
		})
	}
}

func TestLoadCenterConfigRejectsHalfConfiguredTelegramOrInvalidSweepInterval(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("HOUFENG_TELEGRAM_CHAT_ID", "")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for half-configured Telegram settings")
	}

	t.Setenv("HOUFENG_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("HOUFENG_TELEGRAM_CHAT_ID", "")
	t.Setenv("HOUFENG_INCIDENT_SWEEP_INTERVAL", "not-a-duration")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for invalid sweep interval")
	}
}

func TestLoadCenterConfigParsesAuthFields(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "correct-horse-battery")
	t.Setenv("HOUFENG_SESSION_HMAC_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("HOUFENG_INITIAL_DISPLAY_NAME", "管理员")
	t.Setenv("HOUFENG_SESSION_TTL", "168h")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig: %v", err)
	}
	if cfg.InitialUsername != "admin" {
		t.Fatalf("InitialUsername = %q", cfg.InitialUsername)
	}
	if cfg.InitialPassword != "correct-horse-battery" {
		t.Fatalf("InitialPassword wrong: %q", cfg.InitialPassword)
	}
	if cfg.InitialDisplayName != "管理员" {
		t.Fatalf("InitialDisplayName = %q", cfg.InitialDisplayName)
	}
	if cfg.SessionTTL != 168*time.Hour {
		t.Fatalf("SessionTTL = %v", cfg.SessionTTL)
	}
	if string(cfg.SessionHMACKey) != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("SessionHMACKey = %q, want configured key", string(cfg.SessionHMACKey))
	}
}

func TestLoadCenterConfigReadsInitialPasswordFile(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "env-password-should-not-win")
	t.Setenv("HOUFENG_SESSION_HMAC_KEY", "0123456789abcdef0123456789abcdef")
	passwordPath := filepath.Join(t.TempDir(), "initial-password")
	if err := os.WriteFile(passwordPath, []byte(" file-password-xx \n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	t.Setenv("HOUFENG_INITIAL_PASSWORD_FILE", passwordPath)

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig: %v", err)
	}
	if cfg.InitialPassword != "file-password-xx" {
		t.Fatalf("InitialPassword = %q, want password read from file", cfg.InitialPassword)
	}
}

func TestLoadCenterConfigDefaultsSessionTTL(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "correct-horse-battery")
	t.Setenv("HOUFENG_SESSION_HMAC_KEY", "0123456789abcdef0123456789abcdef")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig: %v", err)
	}
	if cfg.SessionTTL != 7*24*time.Hour {
		t.Fatalf("SessionTTL default = %v, want 168h", cfg.SessionTTL)
	}
}

func TestLoadCenterConfigReadsSessionHMACKeyFile(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "correct-horse-battery")
	t.Setenv("HOUFENG_SESSION_HMAC_KEY", "env-key-should-not-win-0123456789")
	keyPath := filepath.Join(t.TempDir(), "session-hmac-key")
	if err := os.WriteFile(keyPath, []byte(" file-session-hmac-key-0123456789 \n"), 0o600); err != nil {
		t.Fatalf("write session hmac key file: %v", err)
	}
	t.Setenv("HOUFENG_SESSION_HMAC_KEY_FILE", keyPath)

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig: %v", err)
	}
	if string(cfg.SessionHMACKey) != "file-session-hmac-key-0123456789" {
		t.Fatalf("SessionHMACKey = %q, want key read from file", string(cfg.SessionHMACKey))
	}
}

func TestLoadCenterConfigRequiresSessionHMACKey(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "correct-horse-battery")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for missing session HMAC key")
	}
}

func TestLoadCenterConfigRejectsShortSessionHMACKey(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_SESSION_HMAC_KEY", "short")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for short session HMAC key")
	}
}

func TestLoadCenterConfigParsesPasswordBcryptCost(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_PASSWORD_BCRYPT_COST", "12")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig: %v", err)
	}
	if cfg.PasswordBcryptCost != 12 {
		t.Fatalf("PasswordBcryptCost = %d, want 12", cfg.PasswordBcryptCost)
	}
}

func TestLoadCenterConfigRejectsInvalidPasswordBcryptCost(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")

	for _, value := range []string{"3", "32", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("HOUFENG_PASSWORD_BCRYPT_COST", value)
			if _, err := centerconfig.LoadCenterConfig(); err == nil {
				t.Fatal("LoadCenterConfig() error = nil, want non-nil for invalid bcrypt cost")
			}
		})
	}
}

func TestLoadCenterConfigRequiresDatabaseTLSWhenConfigured(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_REQUIRE_TLS", "true")

	for _, databaseURL := range []string{
		"postgres://example",
		"postgres://example?sslmode=disable",
		"postgres://example?sslmode=allow",
		"postgres://example?sslmode=prefer",
	} {
		t.Run(databaseURL, func(t *testing.T) {
			t.Setenv("HOUFENG_DATABASE_URL", databaseURL)
			if _, err := centerconfig.LoadCenterConfig(); err == nil {
				t.Fatal("LoadCenterConfig() error = nil, want non-nil when database TLS is required")
			}
		})
	}
}

func TestLoadCenterConfigAcceptsSecureDatabaseTLSModesWhenRequired(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_REQUIRE_TLS", "true")

	for _, databaseURL := range []string{
		"postgres://example?sslmode=require",
		"postgres://example?sslmode=verify-ca",
		"postgres://example?sslmode=verify-full",
	} {
		t.Run(databaseURL, func(t *testing.T) {
			t.Setenv("HOUFENG_DATABASE_URL", databaseURL)
			if _, err := centerconfig.LoadCenterConfig(); err != nil {
				t.Fatalf("LoadCenterConfig() error = %v, want nil", err)
			}
		})
	}
}

func TestLoadCenterConfigRejectsInvalidDatabaseRequireTLS(t *testing.T) {
	setRequiredAuth(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example?sslmode=require")
	t.Setenv("HOUFENG_DATABASE_REQUIRE_TLS", "sometimes")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for invalid HOUFENG_DATABASE_REQUIRE_TLS")
	}
}

func TestLoadCenterConfigRequiresInitialUsername(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "correct-horse-battery")
	t.Setenv("HOUFENG_SESSION_HMAC_KEY", "0123456789abcdef0123456789abcdef")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for missing initial username")
	}
}

func TestLoadCenterConfigRecordAdmissionIdentityIsOptionalAndAllOrNothing(t *testing.T) {
	setRequiredAuth(t)
	setRequiredLocalAttachments(t)
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime")
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_INSTANCE_ID", "api-01")

	if _, err := centerconfig.LoadCenterConfig(); err == nil || !strings.Contains(err.Error(), "HOUFENG_RECORD_INSTANCE_ID") {
		t.Fatalf("LoadCenterConfig() error = %v, want all-or-nothing record admission identity", err)
	}

	deploymentID := "dp-" + strings.Repeat("a", 64)
	t.Setenv("HOUFENG_RECORD_DEPLOYMENT_ID", deploymentID)
	t.Setenv("HOUFENG_RECORD_INSTANCE_KIND", "api")
	t.Setenv("HOUFENG_RECORD_INSTANCE_CAPABILITY", "records.runtime")
	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() error = %v", err)
	}
	if cfg.RecordInstanceID != "api-01" || cfg.RecordDeploymentID != deploymentID ||
		cfg.RecordInstanceKind != "api" || cfg.RecordInstanceCapability != "records.runtime" {
		t.Fatalf("record admission identity = %#v", cfg)
	}

	t.Setenv("HOUFENG_RECORD_INSTANCE_ID", "")
	t.Setenv("HOUFENG_RECORD_DEPLOYMENT_ID", "")
	t.Setenv("HOUFENG_RECORD_INSTANCE_KIND", "")
	t.Setenv("HOUFENG_RECORD_INSTANCE_CAPABILITY", "")
	cfg, err = centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig() empty identity error = %v", err)
	}
	if cfg.RecordInstanceID != "" || cfg.RecordDeploymentID != "" ||
		cfg.RecordInstanceKind != "" || cfg.RecordInstanceCapability != "" {
		t.Fatalf("empty record admission identity = %#v", cfg)
	}
}

func TestLoadCenterConfigRequiresInitialPassword(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "")
	t.Setenv("HOUFENG_SESSION_HMAC_KEY", "0123456789abcdef0123456789abcdef")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for missing initial password")
	}
}
