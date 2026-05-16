package config_test

import (
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
	t.Setenv("HOUFENG_TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("HOUFENG_TELEGRAM_CHAT_ID", "chat-001")

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
	if cfg.TelegramBotToken != "bot-token" || cfg.TelegramChatID != "chat-001" {
		t.Fatalf("Telegram config = %#v, want populated values", cfg)
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
}

func TestLoadCenterConfigDefaultsSessionTTL(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "correct-horse-battery")

	cfg, err := centerconfig.LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig: %v", err)
	}
	if cfg.SessionTTL != 7*24*time.Hour {
		t.Fatalf("SessionTTL default = %v, want 168h", cfg.SessionTTL)
	}
}

func TestLoadCenterConfigRequiresInitialUsername(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "correct-horse-battery")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for missing initial username")
	}
}

func TestLoadCenterConfigRequiresInitialPassword(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://example")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil for missing initial password")
	}
}
