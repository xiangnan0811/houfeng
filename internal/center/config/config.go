package config

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/recordplatform"
)

const (
	defaultHTTPAddr              = ":8080"
	defaultWebDistDir            = "web/dist"
	defaultIncidentSweepInterval = 5 * time.Second

	defaultAttachmentProcessorMaxAttempts = 3
	defaultClamAVDialTimeout              = 5 * time.Second
	defaultClamAVOperationTimeout         = 2 * time.Minute
	defaultClamAVChunkSize                = 64 * 1024
	defaultClamAVResponseLimit            = 4 * 1024
)

type CenterConfig struct {
	RecordPlatformMode        RecordPlatformMode
	ComparisonEnabled         bool
	ComparisonIntentKeyring   string
	ComparisonIntentKeyID     string
	ComparisonAdmissionBudget int64
	PortabilityEnabled        bool
	HTTPAddr                  string
	WebDistDir                string
	DatabaseURL               string
	PublicBaseURL             string
	LogFile                   string
	TelegramBotToken          string
	TelegramChatID            string
	TrustedProxies            []string
	IncidentSweepInterval     time.Duration
	InitialUsername           string
	InitialPassword           string
	InitialDisplayName        string
	SessionTTL                time.Duration
	SessionHMACKey            []byte
	PasswordBcryptCost        int
	Attachment                AttachmentConfig
	RecordInstanceID          string
	RecordDeploymentID        string
	RecordInstanceKind        string
	RecordInstanceCapability  string
}

type AttachmentConfig struct {
	BlobBackend attachments.BackendKind
	BlobRoot    string
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Secure    bool

	ClamAVNetwork          string
	ClamAVAddress          string
	ClamAVDialTimeout      time.Duration
	ClamAVOperationTimeout time.Duration
	ClamAVChunkSize        int
	ClamAVResponseLimit    int

	ProcessorMaxAttempts int64
	Limits               attachments.Limits
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
	var attachmentConfig AttachmentConfig
	if recordPlatformMode == RecordPlatformModeRuntimeAdmission {
		attachmentConfig, err = LoadAttachmentConfig()
		if err != nil {
			return CenterConfig{}, err
		}
	}

	comparisonEnabled, err := boolEnvOrDefault("HOUFENG_COMPARISON_ENABLED", false)
	if err != nil {
		return CenterConfig{}, err
	}
	if comparisonEnabled && recordPlatformMode != RecordPlatformModeRuntimeAdmission {
		return CenterConfig{}, fmt.Errorf("HOUFENG_COMPARISON_ENABLED requires HOUFENG_RECORDS_ENABLED=true")
	}
	comparisonAdmissionBudget := int64(64 << 20)
	if comparisonEnabled {
		budget, err := int64EnvOrDefault("HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES", 64<<20)
		if err != nil {
			return CenterConfig{}, err
		}
		if budget < 8<<20 {
			return CenterConfig{}, fmt.Errorf("HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES must be at least 8388608")
		}
		comparisonAdmissionBudget = budget
	}
	comparisonIntentKeyring := strings.TrimSpace(os.Getenv("HOUFENG_COMPARISON_INTENT_KEYRING"))
	comparisonIntentKeyID := strings.TrimSpace(os.Getenv("HOUFENG_COMPARISON_INTENT_KEY_ID"))
	if comparisonEnabled && (comparisonIntentKeyring == "" || comparisonIntentKeyID == "") {
		return CenterConfig{}, fmt.Errorf("HOUFENG_COMPARISON_ENABLED requires HOUFENG_COMPARISON_INTENT_KEYRING and HOUFENG_COMPARISON_INTENT_KEY_ID")
	}
	recordInstanceID := strings.TrimSpace(os.Getenv("HOUFENG_RECORD_INSTANCE_ID"))
	recordDeploymentID := strings.TrimSpace(os.Getenv("HOUFENG_RECORD_DEPLOYMENT_ID"))
	if deploymentIDFile := strings.TrimSpace(os.Getenv("HOUFENG_RECORD_DEPLOYMENT_ID_FILE")); deploymentIDFile != "" {
		recordDeploymentID, err = readCanonicalRecordDeploymentIDFile(deploymentIDFile)
		if err != nil {
			return CenterConfig{}, err
		}
	}
	recordInstanceKind := strings.TrimSpace(os.Getenv("HOUFENG_RECORD_INSTANCE_KIND"))
	recordInstanceCapability := strings.TrimSpace(os.Getenv("HOUFENG_RECORD_INSTANCE_CAPABILITY"))
	recordIdentitySet := 0
	for _, value := range []string{recordInstanceID, recordDeploymentID, recordInstanceKind, recordInstanceCapability} {
		if value != "" {
			recordIdentitySet++
		}
	}
	if recordIdentitySet != 0 && recordIdentitySet != 4 {
		return CenterConfig{}, fmt.Errorf("HOUFENG_RECORD_INSTANCE_ID, HOUFENG_RECORD_DEPLOYMENT_ID, HOUFENG_RECORD_INSTANCE_KIND, and HOUFENG_RECORD_INSTANCE_CAPABILITY must all be set or all be empty")
	}
	if recordDeploymentID != "" {
		if err := recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(recordDeploymentID)); err != nil {
			return CenterConfig{}, fmt.Errorf("HOUFENG_RECORD_DEPLOYMENT_ID must be a canonical deployment ID")
		}
	}

	if !comparisonEnabled && (comparisonIntentKeyring == "") != (comparisonIntentKeyID == "") {
		return CenterConfig{}, fmt.Errorf("HOUFENG_COMPARISON_INTENT_KEYRING and HOUFENG_COMPARISON_INTENT_KEY_ID must both be set or both be empty")
	}
	if comparisonIntentKeyring != "" {
		sessionKeyFile := strings.TrimSpace(os.Getenv("HOUFENG_SESSION_HMAC_KEY_FILE"))
		if sessionKeyFile != "" && sameFilePath(comparisonIntentKeyring, sessionKeyFile) {
			return CenterConfig{}, fmt.Errorf("HOUFENG_COMPARISON_INTENT_KEYRING must not reuse HOUFENG_SESSION_HMAC_KEY_FILE")
		}
	}

	portabilityEnabled, err := boolEnvOrDefault("HOUFENG_PORTABILITY_ENABLED", false)
	if err != nil {
		return CenterConfig{}, err
	}
	if portabilityEnabled && recordPlatformMode != RecordPlatformModeRuntimeAdmission {
		return CenterConfig{}, fmt.Errorf("HOUFENG_PORTABILITY_ENABLED requires HOUFENG_RECORDS_ENABLED=true")
	}

	return CenterConfig{
		RecordPlatformMode:        recordPlatformMode,
		ComparisonEnabled:         comparisonEnabled,
		ComparisonIntentKeyring:   comparisonIntentKeyring,
		ComparisonIntentKeyID:     comparisonIntentKeyID,
		ComparisonAdmissionBudget: comparisonAdmissionBudget,
		PortabilityEnabled:        portabilityEnabled,
		HTTPAddr:                  httpAddr,
		WebDistDir:                webDistDir,
		DatabaseURL:               databaseURL,
		PublicBaseURL:             publicBaseURL,
		LogFile:                   logFile,
		TelegramBotToken:          telegramBotToken,
		TelegramChatID:            telegramChatID,
		TrustedProxies:            trustedProxies,
		IncidentSweepInterval:     sweepInterval,
		InitialUsername:           initialUsername,
		InitialPassword:           initialPassword,
		InitialDisplayName:        initialDisplayName,
		SessionTTL:                sessionTTL,
		SessionHMACKey:            []byte(sessionHMACKey),
		PasswordBcryptCost:        passwordBcryptCost,
		Attachment:                attachmentConfig,
		RecordInstanceID:          recordInstanceID,
		RecordDeploymentID:        recordDeploymentID,
		RecordInstanceKind:        recordInstanceKind,
		RecordInstanceCapability:  recordInstanceCapability,
	}, nil
}

func LoadAttachmentConfig() (AttachmentConfig, error) {
	backendValue, err := requiredEnv("HOUFENG_ATTACHMENT_BLOB_BACKEND")
	if err != nil {
		return AttachmentConfig{}, err
	}
	processorMaxAttempts, err := intEnvOrDefault("HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS", defaultAttachmentProcessorMaxAttempts)
	if err != nil {
		return AttachmentConfig{}, err
	}
	if processorMaxAttempts <= 0 || processorMaxAttempts > 100 {
		return AttachmentConfig{}, fmt.Errorf("HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS must be between 1 and 100")
	}

	attachmentConfig := AttachmentConfig{
		BlobBackend:          attachments.BackendKind(strings.ToLower(backendValue)),
		ProcessorMaxAttempts: int64(processorMaxAttempts),
		Limits:               attachments.DefaultLimits(),
	}
	switch attachmentConfig.BlobBackend {
	case attachments.BackendKindLocal:
		attachmentConfig.BlobRoot, err = requiredEnv("HOUFENG_ATTACHMENT_BLOB_ROOT")
		if err == nil && (!filepath.IsAbs(attachmentConfig.BlobRoot) || isBroadAttachmentPath(attachmentConfig.BlobRoot)) {
			err = fmt.Errorf("HOUFENG_ATTACHMENT_BLOB_ROOT must be an absolute private directory")
		}
	case attachments.BackendKindS3:
		attachmentConfig.S3Endpoint, err = requiredEnv("HOUFENG_ATTACHMENT_S3_ENDPOINT")
		if err == nil {
			attachmentConfig.S3AccessKey, err = secretEnvOrFile("HOUFENG_ATTACHMENT_S3_ACCESS_KEY")
		}
		if err == nil {
			attachmentConfig.S3SecretKey, err = secretEnvOrFile("HOUFENG_ATTACHMENT_S3_SECRET_KEY")
		}
		if err == nil {
			attachmentConfig.S3Bucket, err = requiredEnv("HOUFENG_ATTACHMENT_S3_BUCKET")
		}
		if err == nil {
			attachmentConfig.S3Secure, err = boolEnvOrDefault("HOUFENG_ATTACHMENT_S3_SECURE", false)
		}
		if err == nil && (strings.Contains(attachmentConfig.S3Endpoint, "://") ||
			strings.TrimSpace(attachmentConfig.S3Endpoint) != attachmentConfig.S3Endpoint ||
			strings.TrimSpace(attachmentConfig.S3Bucket) != attachmentConfig.S3Bucket) {
			err = fmt.Errorf("HOUFENG_ATTACHMENT_S3_ENDPOINT and HOUFENG_ATTACHMENT_S3_BUCKET must be canonical")
		}
	default:
		err = fmt.Errorf("HOUFENG_ATTACHMENT_BLOB_BACKEND must be local or s3")
	}
	if err != nil {
		return AttachmentConfig{}, err
	}

	attachmentConfig.ClamAVAddress = strings.TrimSpace(os.Getenv("HOUFENG_CLAMAV_ADDRESS"))
	if attachmentConfig.ClamAVAddress == "" {
		if strings.TrimSpace(os.Getenv("HOUFENG_CLAMAV_NETWORK")) != "" {
			return AttachmentConfig{}, fmt.Errorf("HOUFENG_CLAMAV_ADDRESS must be set when HOUFENG_CLAMAV_NETWORK is configured")
		}
		return attachmentConfig, nil
	}
	attachmentConfig.ClamAVNetwork, err = envOrDefault("HOUFENG_CLAMAV_NETWORK", "unix")
	if err == nil && attachmentConfig.ClamAVNetwork != "tcp" && attachmentConfig.ClamAVNetwork != "unix" {
		err = fmt.Errorf("HOUFENG_CLAMAV_NETWORK must be tcp or unix")
	}
	if err == nil {
		attachmentConfig.ClamAVDialTimeout, err = durationEnvOrDefault("HOUFENG_CLAMAV_DIAL_TIMEOUT", defaultClamAVDialTimeout)
	}
	if err == nil && (attachmentConfig.ClamAVDialTimeout <= 0 || attachmentConfig.ClamAVDialTimeout > time.Minute) {
		err = fmt.Errorf("HOUFENG_CLAMAV_DIAL_TIMEOUT must be greater than zero and at most 1m")
	}
	if err == nil {
		attachmentConfig.ClamAVOperationTimeout, err = durationEnvOrDefault("HOUFENG_CLAMAV_OPERATION_TIMEOUT", defaultClamAVOperationTimeout)
	}
	if err == nil && (attachmentConfig.ClamAVOperationTimeout <= 0 || attachmentConfig.ClamAVOperationTimeout > time.Hour) {
		err = fmt.Errorf("HOUFENG_CLAMAV_OPERATION_TIMEOUT must be greater than zero and at most 1h")
	}
	if err == nil {
		attachmentConfig.ClamAVChunkSize, err = intEnvOrDefault("HOUFENG_CLAMAV_CHUNK_SIZE", defaultClamAVChunkSize)
	}
	if err == nil && (attachmentConfig.ClamAVChunkSize <= 0 || int64(attachmentConfig.ClamAVChunkSize) > attachments.MiB) {
		err = fmt.Errorf("HOUFENG_CLAMAV_CHUNK_SIZE must be between 1 byte and 1 MiB")
	}
	if err == nil {
		attachmentConfig.ClamAVResponseLimit, err = intEnvOrDefault("HOUFENG_CLAMAV_RESPONSE_LIMIT", defaultClamAVResponseLimit)
	}
	if err == nil && (attachmentConfig.ClamAVResponseLimit <= 0 || int64(attachmentConfig.ClamAVResponseLimit) > attachments.MiB) {
		err = fmt.Errorf("HOUFENG_CLAMAV_RESPONSE_LIMIT must be between 1 byte and 1 MiB")
	}
	if err != nil {
		return AttachmentConfig{}, err
	}
	return attachmentConfig, nil
}

func isBroadAttachmentPath(path string) bool {
	cleaned := filepath.Clean(path)
	return cleaned == filepath.VolumeName(cleaned)+string(filepath.Separator) || filepath.Dir(cleaned) == cleaned
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

func readCanonicalRecordDeploymentIDFile(path string) (string, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return "", fmt.Errorf("HOUFENG_RECORD_DEPLOYMENT_ID_FILE could not be read as a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("HOUFENG_RECORD_DEPLOYMENT_ID_FILE could not be read as a regular file")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return "", fmt.Errorf("HOUFENG_RECORD_DEPLOYMENT_ID_FILE changed while opening")
	}
	body, err := io.ReadAll(io.LimitReader(file, 69))
	if err != nil || len(body) != 68 || body[len(body)-1] != '\n' {
		return "", fmt.Errorf("HOUFENG_RECORD_DEPLOYMENT_ID_FILE is not a canonical deployment ID file")
	}
	deploymentID := recordplatform.DeploymentID(string(body[:len(body)-1]))
	if err := recordplatform.ValidateDeploymentID(deploymentID); err != nil {
		return "", fmt.Errorf("HOUFENG_RECORD_DEPLOYMENT_ID_FILE is not a canonical deployment ID file")
	}
	return string(deploymentID), nil
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

func int64EnvOrDefault(key string, fallback int64) (int64, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s must not be empty", key)
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return n, nil
}

func sameFilePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(strings.TrimSpace(left))
	rightAbs, rightErr := filepath.Abs(strings.TrimSpace(right))
	return leftErr == nil && rightErr == nil && leftAbs == rightAbs
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
