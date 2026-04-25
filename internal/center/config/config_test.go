package config_test

import (
	"testing"

	centerconfig "houfeng/internal/center/config"
)

func TestLoadCenterConfigRequiresDatabaseURL(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":8080")
	t.Setenv("HOUFENG_WEB_DIST_DIR", "web/dist")
	t.Setenv("HOUFENG_DATABASE_URL", "")

	if _, err := centerconfig.LoadCenterConfig(); err == nil {
		t.Fatal("LoadCenterConfig() error = nil, want non-nil")
	}
}

func TestLoadCenterConfigParsesOptionalIncidentAndTelegramSettings(t *testing.T) {
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

func TestLoadCenterConfigRejectsHalfConfiguredTelegramOrInvalidSweepInterval(t *testing.T) {
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
