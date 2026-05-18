package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultHTTPAddr              = ":8080"
	defaultWebDistDir            = "web/dist"
	defaultIncidentSweepInterval = 5 * time.Second
)

type CenterConfig struct {
	HTTPAddr              string
	WebDistDir            string
	DatabaseURL           string
	PublicBaseURL         string
	LogFile               string
	TelegramBotToken      string
	TelegramChatID        string
	IncidentSweepInterval time.Duration
	InitialUsername       string
	InitialPassword       string
	InitialDisplayName    string
	SessionTTL            time.Duration
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

	publicBaseURL, err := optionalHTTPBaseURL("HOUFENG_PUBLIC_BASE_URL")
	if err != nil {
		return CenterConfig{}, err
	}

	logFile := strings.TrimSpace(os.Getenv("HOUFENG_LOG_FILE"))

	telegramBotToken := strings.TrimSpace(os.Getenv("HOUFENG_TELEGRAM_BOT_TOKEN"))
	telegramChatID := strings.TrimSpace(os.Getenv("HOUFENG_TELEGRAM_CHAT_ID"))
	if (telegramBotToken == "") != (telegramChatID == "") {
		return CenterConfig{}, fmt.Errorf("HOUFENG_TELEGRAM_BOT_TOKEN and HOUFENG_TELEGRAM_CHAT_ID must both be set or both be empty")
	}

	initialUsername, err := requiredEnv("HOUFENG_INITIAL_USERNAME")
	if err != nil {
		return CenterConfig{}, err
	}
	initialPassword, err := requiredEnv("HOUFENG_INITIAL_PASSWORD")
	if err != nil {
		return CenterConfig{}, err
	}
	initialDisplayName := strings.TrimSpace(os.Getenv("HOUFENG_INITIAL_DISPLAY_NAME"))
	sessionTTL, err := durationEnvOrDefault("HOUFENG_SESSION_TTL", 7*24*time.Hour)
	if err != nil {
		return CenterConfig{}, err
	}

	return CenterConfig{
		HTTPAddr:              httpAddr,
		WebDistDir:            webDistDir,
		DatabaseURL:           databaseURL,
		PublicBaseURL:         publicBaseURL,
		LogFile:               logFile,
		TelegramBotToken:      telegramBotToken,
		TelegramChatID:        telegramChatID,
		IncidentSweepInterval: sweepInterval,
		InitialUsername:       initialUsername,
		InitialPassword:       initialPassword,
		InitialDisplayName:    initialDisplayName,
		SessionTTL:            sessionTTL,
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

func optionalHTTPBaseURL(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", nil
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", key, err)
	}
	if !parsed.IsAbs() || parsed.Host == "" {
		return "", fmt.Errorf("%s must be an absolute HTTP(S) URL", key)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s must use http or https", key)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s must not include query or fragment", key)
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func nonEmptyEnvValue(key, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}

	return value, nil
}
