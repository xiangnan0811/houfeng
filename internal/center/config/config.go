package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"houfeng/internal/center/auth"
)

const (
	defaultHTTPAddr              = ":8080"
	defaultWebDistDir            = "web/dist"
	defaultIncidentSweepInterval = 5 * time.Second
)

type CenterConfig struct {
	RecordPlatformMode    RecordPlatformMode
	HTTPAddr              string
	WebDistDir            string
	DatabaseURL           string
	PublicBaseURL         string
	LogFile               string
	TelegramBotToken      string
	TelegramChatID        string
	TrustedProxies        []string
	IncidentSweepInterval time.Duration
	InitialUsername       string
	InitialPassword       string
	InitialDisplayName    string
	SessionTTL            time.Duration
	SessionHMACKey        []byte
	PasswordBcryptCost    int
}

// RecordPlatformMode is the allowed record-platform process boundary.
// Legacy keeps the existing owner migration path; RuntimeAdmission permits
// only the direct runtime identity after admission succeeds.
type RecordPlatformMode uint8

const (
	RecordPlatformModeLegacy RecordPlatformMode = iota
	RecordPlatformModeRuntimeAdmission
)

// LoadRecordPlatformMode parses the record-platform flags before any other
// center configuration can resolve a URL or read a secret file.
func LoadRecordPlatformMode() (RecordPlatformMode, error) {
	recordsEnabled, err := boolEnvOrDefault("HOUFENG_RECORDS_ENABLED", false)
	if err != nil {
		return RecordPlatformModeLegacy, err
	}
	permanentDeleteEnabled, err := boolEnvOrDefault("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", false)
	if err != nil {
		return RecordPlatformModeLegacy, err
	}
	if !recordsEnabled && permanentDeleteEnabled {
		return RecordPlatformModeLegacy, fmt.Errorf("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED requires HOUFENG_RECORDS_ENABLED=true")
	}
	if recordsEnabled && permanentDeleteEnabled {
		return RecordPlatformModeLegacy, fmt.Errorf("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED=true is not supported")
	}
	if recordsEnabled {
		return RecordPlatformModeRuntimeAdmission, nil
	}
	return RecordPlatformModeLegacy, nil
}

func LoadCenterConfig() (CenterConfig, error) {
	recordPlatformMode, err := LoadRecordPlatformMode()
	if err != nil {
		return CenterConfig{}, err
	}

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
	databaseRequireTLS, err := boolEnvOrDefault("HOUFENG_DATABASE_REQUIRE_TLS", false)
	if err != nil {
		return CenterConfig{}, err
	}
	if databaseRequireTLS {
		if err := requireDatabaseTLSMode(databaseURL); err != nil {
			return CenterConfig{}, err
		}
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

	trustedProxies, err := cidrListEnv("HOUFENG_TRUSTED_PROXIES")
	if err != nil {
		return CenterConfig{}, err
	}

	initialUsername, err := requiredEnv("HOUFENG_INITIAL_USERNAME")
	if err != nil {
		return CenterConfig{}, err
	}
	initialPassword, err := secretEnvOrFile("HOUFENG_INITIAL_PASSWORD")
	if err != nil {
		return CenterConfig{}, err
	}
	initialDisplayName := strings.TrimSpace(os.Getenv("HOUFENG_INITIAL_DISPLAY_NAME"))
	sessionTTL, err := durationEnvOrDefault("HOUFENG_SESSION_TTL", 7*24*time.Hour)
	if err != nil {
		return CenterConfig{}, err
	}
	sessionHMACKey, err := secretEnvOrFile("HOUFENG_SESSION_HMAC_KEY")
	if err != nil {
		return CenterConfig{}, err
	}
	if len([]byte(sessionHMACKey)) < 32 {
		return CenterConfig{}, fmt.Errorf("HOUFENG_SESSION_HMAC_KEY must be at least 32 bytes")
	}
	passwordBcryptCost, err := intEnvOrDefault("HOUFENG_PASSWORD_BCRYPT_COST", auth.DefaultPasswordBcryptCost)
	if err != nil {
		return CenterConfig{}, err
	}
	if passwordBcryptCost < bcrypt.MinCost || passwordBcryptCost > bcrypt.MaxCost {
		return CenterConfig{}, fmt.Errorf("HOUFENG_PASSWORD_BCRYPT_COST must be between %d and %d", bcrypt.MinCost, bcrypt.MaxCost)
	}

	return CenterConfig{
		RecordPlatformMode:    recordPlatformMode,
		HTTPAddr:              httpAddr,
		WebDistDir:            webDistDir,
		DatabaseURL:           databaseURL,
		PublicBaseURL:         publicBaseURL,
		LogFile:               logFile,
		TelegramBotToken:      telegramBotToken,
		TelegramChatID:        telegramChatID,
		TrustedProxies:        trustedProxies,
		IncidentSweepInterval: sweepInterval,
		InitialUsername:       initialUsername,
		InitialPassword:       initialPassword,
		InitialDisplayName:    initialDisplayName,
		SessionTTL:            sessionTTL,
		SessionHMACKey:        []byte(sessionHMACKey),
		PasswordBcryptCost:    passwordBcryptCost,
	}, nil
}

func cidrListEnv(key string) ([]string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	cidrs := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		_, network, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("parse %s CIDR %q: %w", key, part, err)
		}
		if isAllAddressCIDR(network) {
			return nil, fmt.Errorf("%s must not include all-address trusted proxy CIDR %q", key, part)
		}
		cidrs = append(cidrs, part)
	}
	return cidrs, nil
}

func isAllAddressCIDR(network *net.IPNet) bool {
	if network == nil {
		return false
	}
	ones, bits := network.Mask.Size()
	return ones == 0 && (bits == 32 || bits == 128)
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

func secretEnvOrFile(key string) (string, error) {
	fileKey := key + "_FILE"
	filePath := strings.TrimSpace(os.Getenv(fileKey))
	if filePath != "" {
		body, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", fileKey, err)
		}
		return nonEmptyEnvValue(fileKey, string(body))
	}
	return requiredEnv(key)
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

func intEnvOrDefault(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s must not be empty", key)
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return n, nil
}

func boolEnvOrDefault(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return false, fmt.Errorf("%s must not be empty", key)
	}
	switch strings.ToLower(value) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", key)
	}
}

func requireDatabaseTLSMode(databaseURL string) error {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse HOUFENG_DATABASE_URL: %w", err)
	}
	sslMode := strings.ToLower(strings.TrimSpace(parsed.Query().Get("sslmode")))
	switch sslMode {
	case "require", "verify-ca", "verify-full":
		return nil
	case "":
		return fmt.Errorf("HOUFENG_DATABASE_URL must include sslmode=require, verify-ca, or verify-full when HOUFENG_DATABASE_REQUIRE_TLS=true")
	default:
		return fmt.Errorf("HOUFENG_DATABASE_URL sslmode=%s is not allowed when HOUFENG_DATABASE_REQUIRE_TLS=true", sslMode)
	}
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
