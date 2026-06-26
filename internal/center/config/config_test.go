package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
