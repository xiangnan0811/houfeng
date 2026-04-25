package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultHTTPAddr              = ":8080"
	defaultWebDistDir            = "web/dist"
	defaultIncidentSweepInterval = time.Minute
)

type CenterConfig struct {
	HTTPAddr              string
	WebDistDir            string
	DatabaseURL           string
	TelegramBotToken      string
	TelegramChatID        string
	IncidentSweepInterval time.Duration
}

func LoadCenterConfig() (CenterConfig, error) {
	httpAddr, err := envOrDefault("HOUFENG_HTTP_ADDR", defaultHTTPAddr)
	if err != nil {
		return CenterConfig{}, err
	}

	webDistDir, err := envOrDefault("HOUFENG_WEB_DIST_DIR", defaultWebDistDir)
	if err != nil {
		return CenterConfig{}, err
	}

	databaseURL, err := requiredEnv("HOUFENG_DATABASE_URL")
	if err != nil {
		return CenterConfig{}, err
	}

	sweepInterval, err := durationEnvOrDefault("HOUFENG_INCIDENT_SWEEP_INTERVAL", defaultIncidentSweepInterval)
	if err != nil {
		return CenterConfig{}, err
	}

	telegramBotToken := strings.TrimSpace(os.Getenv("HOUFENG_TELEGRAM_BOT_TOKEN"))
	telegramChatID := strings.TrimSpace(os.Getenv("HOUFENG_TELEGRAM_CHAT_ID"))
	if (telegramBotToken == "") != (telegramChatID == "") {
		return CenterConfig{}, fmt.Errorf("HOUFENG_TELEGRAM_BOT_TOKEN and HOUFENG_TELEGRAM_CHAT_ID must both be set or both be empty")
	}

	return CenterConfig{
		HTTPAddr:              httpAddr,
		WebDistDir:            webDistDir,
		DatabaseURL:           databaseURL,
		TelegramBotToken:      telegramBotToken,
		TelegramChatID:        telegramChatID,
		IncidentSweepInterval: sweepInterval,
	}, nil
}

func envOrDefault(key, fallback string) (string, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		value = fallback
	}
	return nonEmptyEnvValue(key, value)
}

func requiredEnv(key string) (string, error) {
	return nonEmptyEnvValue(key, os.Getenv(key))
}

func durationEnvOrDefault(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s must not be empty", key)
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return duration, nil
}

func nonEmptyEnvValue(key, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}

	return value, nil
}
