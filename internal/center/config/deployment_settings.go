package config

import (
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DeploymentSettingSource identifies the process input that supplied a
// deployment setting. It describes this process' startup snapshot only; it is
// not a promise about the current environment or files on disk.
type DeploymentSettingSource string

const (
	DeploymentSettingSourceEnvironment DeploymentSettingSource = "environment"
	DeploymentSettingSourceFile        DeploymentSettingSource = "file"
	DeploymentSettingSourceDefault     DeploymentSettingSource = "default"
	DeploymentSettingSourceUnset       DeploymentSettingSource = "unset"
)

// DeploymentSettingGroup is the stable grouping used by the deployment
// settings endpoint.
type DeploymentSettingGroup string

const (
	DeploymentSettingGroupProcess        DeploymentSettingGroup = "process"
	DeploymentSettingGroupDatabase       DeploymentSettingGroup = "database"
	DeploymentSettingGroupNotifications  DeploymentSettingGroup = "notifications"
	DeploymentSettingGroupMonitoring     DeploymentSettingGroup = "monitoring"
	DeploymentSettingGroupAuthentication DeploymentSettingGroup = "authentication"
	DeploymentSettingGroupRecords        DeploymentSettingGroup = "records"
	DeploymentSettingGroupStorage        DeploymentSettingGroup = "storage"
)

// DeploymentSettingApplication explains where a setting participates in the
// center lifecycle. inactive is used when a setting is present in the
// allowlist but the selected process mode does not load or use it.
type DeploymentSettingApplication string

const (
	DeploymentSettingApplicationStartup         DeploymentSettingApplication = "startup"
	DeploymentSettingApplicationBootstrapOnly   DeploymentSettingApplication = "bootstrap_only"
	DeploymentSettingApplicationFallback        DeploymentSettingApplication = "fallback"
	DeploymentSettingApplicationInactive        DeploymentSettingApplication = "inactive"
	DeploymentSettingApplicationReservationOnly DeploymentSettingApplication = "reservation_only"
)

// DeploymentSettingItem is the allowlisted, safe projection returned by the
// supervisor-only deployment settings endpoint. Value is nil for secrets or
// for values that are not configured.
type DeploymentSettingItem struct {
	Key         string                       `json:"key"`
	Group       DeploymentSettingGroup       `json:"group"`
	Source      DeploymentSettingSource      `json:"source"`
	Value       *string                      `json:"value"`
	Configured  bool                         `json:"configured"`
	Sensitive   bool                         `json:"sensitive"`
	Application DeploymentSettingApplication `json:"application"`
}

// DeploymentSettingsSnapshot is captured once, after LoadCenterConfig has
// successfully parsed startup input. The handler receives a deep copy, so
// later environment/file changes cannot alter its response.
type DeploymentSettingsSnapshot struct {
	Items []DeploymentSettingItem `json:"items"`
}

// Clone returns an independent snapshot copy. Strings are immutable, while the
// Value pointers and Items backing array must still be copied to prevent a
// caller from mutating another holder's response.
func (snapshot DeploymentSettingsSnapshot) Clone() DeploymentSettingsSnapshot {
	if len(snapshot.Items) == 0 {
		return DeploymentSettingsSnapshot{Items: []DeploymentSettingItem{}}
	}
	items := make([]DeploymentSettingItem, len(snapshot.Items))
	copy(items, snapshot.Items)
	for index := range items {
		if items[index].Sensitive || sensitiveDeploymentSettingKey(items[index].Key) {
			items[index].Sensitive = true
			items[index].Value = nil
			continue
		}
		if items[index].Value != nil {
			value := *items[index].Value
			items[index].Value = &value
		}
	}
	return DeploymentSettingsSnapshot{Items: items}
}

func sensitiveDeploymentSettingKey(key string) bool {
	switch key {
	case
		"HOUFENG_DATABASE_URL",
		"HOUFENG_TELEGRAM_BOT_TOKEN",
		"HOUFENG_INITIAL_PASSWORD",
		"HOUFENG_SESSION_HMAC_KEY",
		"HOUFENG_COMPARISON_INTENT_KEYRING",
		"HOUFENG_ATTACHMENT_S3_ACCESS_KEY",
		"HOUFENG_ATTACHMENT_S3_SECRET_KEY",
		"HOUFENG_DELETION_WITNESS_DATABASE_URL",
		"HOUFENG_RECORD_DELETION_KEY_FILE",
		"HOUFENG_BACKUP_HMAC_KEY_FILE":
		return true
	default:
		return false
	}
}

// WithConfiguredOnly appends or replaces a safe configured-only item. It is
// used by bootstrap for inputs read at that phase, while keeping their raw
// values out of the snapshot.
func (snapshot DeploymentSettingsSnapshot) WithConfiguredOnly(key string, group DeploymentSettingGroup, application DeploymentSettingApplication, configured bool) DeploymentSettingsSnapshot {
	updated := snapshot.Clone()
	source := DeploymentSettingSourceUnset
	if configured {
		source = DeploymentSettingSourceEnvironment
	} else if _, present := os.LookupEnv(key); present {
		source = DeploymentSettingSourceEnvironment
	}
	for index := range updated.Items {
		if updated.Items[index].Key != key {
			continue
		}
		updated.Items[index].Group = group
		updated.Items[index].Source = source
		updated.Items[index].Value = nil
		updated.Items[index].Configured = configured
		updated.Items[index].Sensitive = true
		updated.Items[index].Application = application
		return updated
	}
	updated.Items = append(updated.Items, DeploymentSettingItem{
		Key: key, Group: group, Source: source, Configured: configured,
		Sensitive: true, Application: application,
	})
	return updated
}

func newDeploymentSettingSnapshot(cfg CenterConfig) DeploymentSettingsSnapshot {
	items := make([]DeploymentSettingItem, 0, 44)
	add := func(key string, group DeploymentSettingGroup, source DeploymentSettingSource, value *string, configured, sensitive bool, application DeploymentSettingApplication) {
		items = append(items, DeploymentSettingItem{
			Key: key, Group: group, Source: source, Value: value,
			Configured: configured, Sensitive: sensitive, Application: application,
		})
	}
	addString := func(key string, group DeploymentSettingGroup, source DeploymentSettingSource, value string, configured, sensitive bool, application DeploymentSettingApplication) {
		var output *string
		if value != "" && !sensitive {
			output = new(value)
		}
		add(key, group, source, output, configured, sensitive, application)
	}
	addBool := func(key string, group DeploymentSettingGroup, source DeploymentSettingSource, value bool, configured bool, application DeploymentSettingApplication) {
		add(key, group, source, new(strconv.FormatBool(value)), configured, false, application)
	}
	addDefaultedString := func(key string, group DeploymentSettingGroup, value string, application DeploymentSettingApplication) {
		addString(key, group, sourceForDefaultedEnv(key), value, envConfigured(key), false, application)
	}
	addOptionalString := func(key string, group DeploymentSettingGroup, value string, application DeploymentSettingApplication) {
		addString(key, group, sourceForOptionalEnv(key), value, strings.TrimSpace(value) != "", false, application)
	}

	addDefaultedString("HOUFENG_HTTP_ADDR", DeploymentSettingGroupProcess, cfg.HTTPAddr, DeploymentSettingApplicationStartup)
	addDefaultedString("HOUFENG_WEB_DIST_DIR", DeploymentSettingGroupProcess, cfg.WebDistDir, DeploymentSettingApplicationStartup)
	publicBaseURL := safePublicURLDisplay(cfg.PublicBaseURL)
	addOptionalString("HOUFENG_PUBLIC_BASE_URL", DeploymentSettingGroupProcess, publicBaseURL, DeploymentSettingApplicationStartup)
	addOptionalString("HOUFENG_LOG_FILE", DeploymentSettingGroupProcess, strings.TrimSpace(cfg.LogFile), DeploymentSettingApplicationStartup)
	trustedProxies := strings.Join(cfg.TrustedProxies, ", ")
	addOptionalString("HOUFENG_TRUSTED_PROXIES", DeploymentSettingGroupProcess, trustedProxies, DeploymentSettingApplicationStartup)

	add("HOUFENG_DATABASE_URL", DeploymentSettingGroupDatabase, sourceForOptionalEnv("HOUFENG_DATABASE_URL"), nil, strings.TrimSpace(cfg.DatabaseURL) != "", true, DeploymentSettingApplicationStartup)
	addBool("HOUFENG_DATABASE_REQUIRE_TLS", DeploymentSettingGroupDatabase, sourceForDefaultedEnv("HOUFENG_DATABASE_REQUIRE_TLS"), cfg.DatabaseRequireTLS, envConfigured("HOUFENG_DATABASE_REQUIRE_TLS"), DeploymentSettingApplicationStartup)

	telegramTokenSource := sourceForOptionalEnv("HOUFENG_TELEGRAM_BOT_TOKEN")
	add("HOUFENG_TELEGRAM_BOT_TOKEN", DeploymentSettingGroupNotifications, telegramTokenSource, nil, strings.TrimSpace(cfg.TelegramBotToken) != "", true, DeploymentSettingApplicationFallback)
	addOptionalString("HOUFENG_TELEGRAM_CHAT_ID", DeploymentSettingGroupNotifications, strings.TrimSpace(cfg.TelegramChatID), DeploymentSettingApplicationFallback)
	addString("HOUFENG_INCIDENT_SWEEP_INTERVAL", DeploymentSettingGroupMonitoring, sourceForDefaultedEnv("HOUFENG_INCIDENT_SWEEP_INTERVAL"), cfg.IncidentSweepInterval.String(), envConfigured("HOUFENG_INCIDENT_SWEEP_INTERVAL"), false, DeploymentSettingApplicationFallback)

	addOptionalString("HOUFENG_INITIAL_USERNAME", DeploymentSettingGroupAuthentication, strings.TrimSpace(cfg.InitialUsername), DeploymentSettingApplicationBootstrapOnly)
	addOptionalString("HOUFENG_INITIAL_DISPLAY_NAME", DeploymentSettingGroupAuthentication, strings.TrimSpace(cfg.InitialDisplayName), DeploymentSettingApplicationBootstrapOnly)
	initialPasswordSource, _ := sourceForSecret("HOUFENG_INITIAL_PASSWORD")
	add("HOUFENG_INITIAL_PASSWORD", DeploymentSettingGroupAuthentication, initialPasswordSource, nil, strings.TrimSpace(cfg.InitialPassword) != "", true, DeploymentSettingApplicationBootstrapOnly)
	addString("HOUFENG_SESSION_TTL", DeploymentSettingGroupAuthentication, sourceForDefaultedEnv("HOUFENG_SESSION_TTL"), cfg.SessionTTL.String(), envConfigured("HOUFENG_SESSION_TTL"), false, DeploymentSettingApplicationStartup)
	hmacSource, _ := sourceForSecret("HOUFENG_SESSION_HMAC_KEY")
	add("HOUFENG_SESSION_HMAC_KEY", DeploymentSettingGroupAuthentication, hmacSource, nil, len(cfg.SessionHMACKey) > 0, true, DeploymentSettingApplicationStartup)
	addString("HOUFENG_PASSWORD_BCRYPT_COST", DeploymentSettingGroupAuthentication, sourceForDefaultedEnv("HOUFENG_PASSWORD_BCRYPT_COST"), strconv.Itoa(cfg.PasswordBcryptCost), envConfigured("HOUFENG_PASSWORD_BCRYPT_COST"), false, DeploymentSettingApplicationStartup)

	recordsEnabled := cfg.RecordPlatformMode == RecordPlatformModeRuntimeAdmission
	addBool("HOUFENG_RECORDS_ENABLED", DeploymentSettingGroupRecords, sourceForDefaultedEnv("HOUFENG_RECORDS_ENABLED"), recordsEnabled, envConfigured("HOUFENG_RECORDS_ENABLED"), DeploymentSettingApplicationStartup)
	addBool("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", DeploymentSettingGroupRecords, sourceForDefaultedEnv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED"), false, envConfigured("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED"), DeploymentSettingApplicationStartup)
	recordIdentityApplication := DeploymentSettingApplicationInactive
	recordFeatureApplication := DeploymentSettingApplicationInactive
	if recordsEnabled {
		recordIdentityApplication = DeploymentSettingApplicationStartup
		recordFeatureApplication = DeploymentSettingApplicationStartup
	}
	addOptionalString("HOUFENG_RECORD_INSTANCE_ID", DeploymentSettingGroupRecords, strings.TrimSpace(cfg.RecordInstanceID), recordIdentityApplication)
	deploymentIDSource, _ := sourceForSecret("HOUFENG_RECORD_DEPLOYMENT_ID")
	if _, fileConfigured := os.LookupEnv("HOUFENG_RECORD_DEPLOYMENT_ID_FILE"); fileConfigured && strings.TrimSpace(os.Getenv("HOUFENG_RECORD_DEPLOYMENT_ID_FILE")) != "" {
		deploymentIDSource = DeploymentSettingSourceFile
	}
	addString("HOUFENG_RECORD_DEPLOYMENT_ID", DeploymentSettingGroupRecords, deploymentIDSource, strings.TrimSpace(cfg.RecordDeploymentID), strings.TrimSpace(cfg.RecordDeploymentID) != "", false, recordIdentityApplication)
	addOptionalString("HOUFENG_RECORD_INSTANCE_KIND", DeploymentSettingGroupRecords, strings.TrimSpace(cfg.RecordInstanceKind), recordIdentityApplication)
	addOptionalString("HOUFENG_RECORD_INSTANCE_CAPABILITY", DeploymentSettingGroupRecords, strings.TrimSpace(cfg.RecordInstanceCapability), recordIdentityApplication)
	addBool("HOUFENG_PORTABILITY_ENABLED", DeploymentSettingGroupRecords, sourceForDefaultedEnv("HOUFENG_PORTABILITY_ENABLED"), cfg.PortabilityEnabled, envConfigured("HOUFENG_PORTABILITY_ENABLED"), recordFeatureApplication)
	addBool("HOUFENG_COMPARISON_ENABLED", DeploymentSettingGroupRecords, sourceForDefaultedEnv("HOUFENG_COMPARISON_ENABLED"), cfg.ComparisonEnabled, envConfigured("HOUFENG_COMPARISON_ENABLED"), recordFeatureApplication)
	comparisonApplication := DeploymentSettingApplicationInactive
	if recordsEnabled && cfg.ComparisonEnabled {
		comparisonApplication = DeploymentSettingApplicationStartup
	}
	addOptionalString("HOUFENG_COMPARISON_INTENT_KEY_ID", DeploymentSettingGroupRecords, strings.TrimSpace(cfg.ComparisonIntentKeyID), comparisonApplication)

	add("HOUFENG_COMPARISON_INTENT_KEYRING", DeploymentSettingGroupRecords, sourceForOptionalEnv("HOUFENG_COMPARISON_INTENT_KEYRING"), nil, strings.TrimSpace(cfg.ComparisonIntentKeyring) != "", true, comparisonApplication)
	if recordsEnabled && cfg.ComparisonEnabled {
		addString("HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES", DeploymentSettingGroupRecords, sourceForDefaultedEnv("HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES"), strconv.FormatInt(cfg.ComparisonAdmissionBudget, 10), envConfigured("HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES"), false, DeploymentSettingApplicationStartup)
	} else {
		addRawString("HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES", DeploymentSettingGroupRecords, DeploymentSettingApplicationInactive, add)
	}

	attachmentActive := recordsEnabled
	backend := strings.TrimSpace(string(cfg.Attachment.BlobBackend))
	if attachmentActive {
		addString("HOUFENG_ATTACHMENT_BLOB_BACKEND", DeploymentSettingGroupStorage, sourceForOptionalEnv("HOUFENG_ATTACHMENT_BLOB_BACKEND"), backend, backend != "", false, DeploymentSettingApplicationStartup)
	} else {
		addRawString("HOUFENG_ATTACHMENT_BLOB_BACKEND", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
	}
	backendIsLocal := attachmentActive && backend == "local"
	backendIsS3 := attachmentActive && backend == "s3"
	if backendIsLocal {
		addOptionalString("HOUFENG_ATTACHMENT_BLOB_ROOT", DeploymentSettingGroupStorage, strings.TrimSpace(cfg.Attachment.BlobRoot), DeploymentSettingApplicationStartup)
	} else {
		addRawString("HOUFENG_ATTACHMENT_BLOB_ROOT", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
	}
	if backendIsS3 {
		addSafeEndpoint("HOUFENG_ATTACHMENT_S3_ENDPOINT", DeploymentSettingGroupStorage, cfg.Attachment.S3Endpoint, "", DeploymentSettingApplicationStartup, add)
		addOptionalString("HOUFENG_ATTACHMENT_S3_BUCKET", DeploymentSettingGroupStorage, strings.TrimSpace(cfg.Attachment.S3Bucket), DeploymentSettingApplicationStartup)
		addBool("HOUFENG_ATTACHMENT_S3_SECURE", DeploymentSettingGroupStorage, sourceForDefaultedEnv("HOUFENG_ATTACHMENT_S3_SECURE"), cfg.Attachment.S3Secure, envConfigured("HOUFENG_ATTACHMENT_S3_SECURE"), DeploymentSettingApplicationStartup)
		s3AccessSource, _ := sourceForSecret("HOUFENG_ATTACHMENT_S3_ACCESS_KEY")
		add("HOUFENG_ATTACHMENT_S3_ACCESS_KEY", DeploymentSettingGroupStorage, s3AccessSource, nil, strings.TrimSpace(cfg.Attachment.S3AccessKey) != "", true, DeploymentSettingApplicationStartup)
		s3SecretSource, _ := sourceForSecret("HOUFENG_ATTACHMENT_S3_SECRET_KEY")
		add("HOUFENG_ATTACHMENT_S3_SECRET_KEY", DeploymentSettingGroupStorage, s3SecretSource, nil, strings.TrimSpace(cfg.Attachment.S3SecretKey) != "", true, DeploymentSettingApplicationStartup)
	} else {
		addRawSafeEndpoint("HOUFENG_ATTACHMENT_S3_ENDPOINT", DeploymentSettingGroupStorage, "", DeploymentSettingApplicationInactive, add)
		addRawString("HOUFENG_ATTACHMENT_S3_BUCKET", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
		addRawBool("HOUFENG_ATTACHMENT_S3_SECURE", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
		addRawSecret("HOUFENG_ATTACHMENT_S3_ACCESS_KEY", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
		addRawSecret("HOUFENG_ATTACHMENT_S3_SECRET_KEY", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
	}

	scannerActive := attachmentActive && strings.TrimSpace(cfg.Attachment.ClamAVAddress) != ""
	if scannerActive {
		addString("HOUFENG_CLAMAV_NETWORK", DeploymentSettingGroupStorage, sourceForDefaultedEnv("HOUFENG_CLAMAV_NETWORK"), strings.TrimSpace(cfg.Attachment.ClamAVNetwork), envConfigured("HOUFENG_CLAMAV_NETWORK"), false, DeploymentSettingApplicationStartup)
		addSafeEndpoint("HOUFENG_CLAMAV_ADDRESS", DeploymentSettingGroupStorage, cfg.Attachment.ClamAVAddress, cfg.Attachment.ClamAVNetwork, DeploymentSettingApplicationStartup, add)
		addString("HOUFENG_CLAMAV_DIAL_TIMEOUT", DeploymentSettingGroupStorage, sourceForDefaultedEnv("HOUFENG_CLAMAV_DIAL_TIMEOUT"), cfg.Attachment.ClamAVDialTimeout.String(), envConfigured("HOUFENG_CLAMAV_DIAL_TIMEOUT"), false, DeploymentSettingApplicationStartup)
		addString("HOUFENG_CLAMAV_OPERATION_TIMEOUT", DeploymentSettingGroupStorage, sourceForDefaultedEnv("HOUFENG_CLAMAV_OPERATION_TIMEOUT"), cfg.Attachment.ClamAVOperationTimeout.String(), envConfigured("HOUFENG_CLAMAV_OPERATION_TIMEOUT"), false, DeploymentSettingApplicationStartup)
		addString("HOUFENG_CLAMAV_CHUNK_SIZE", DeploymentSettingGroupStorage, sourceForDefaultedEnv("HOUFENG_CLAMAV_CHUNK_SIZE"), strconv.Itoa(cfg.Attachment.ClamAVChunkSize), envConfigured("HOUFENG_CLAMAV_CHUNK_SIZE"), false, DeploymentSettingApplicationStartup)
		addString("HOUFENG_CLAMAV_RESPONSE_LIMIT", DeploymentSettingGroupStorage, sourceForDefaultedEnv("HOUFENG_CLAMAV_RESPONSE_LIMIT"), strconv.Itoa(cfg.Attachment.ClamAVResponseLimit), envConfigured("HOUFENG_CLAMAV_RESPONSE_LIMIT"), false, DeploymentSettingApplicationStartup)
	} else {
		addRawString("HOUFENG_CLAMAV_NETWORK", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
		network, present := os.LookupEnv("HOUFENG_CLAMAV_NETWORK")
		if !present {
			network = "unix"
		}
		addRawSafeEndpoint("HOUFENG_CLAMAV_ADDRESS", DeploymentSettingGroupStorage, strings.TrimSpace(network), DeploymentSettingApplicationInactive, add)
		addRawString("HOUFENG_CLAMAV_DIAL_TIMEOUT", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
		addRawString("HOUFENG_CLAMAV_OPERATION_TIMEOUT", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
		addRawString("HOUFENG_CLAMAV_CHUNK_SIZE", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
		addRawString("HOUFENG_CLAMAV_RESPONSE_LIMIT", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
	}
	if attachmentActive {
		addString("HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS", DeploymentSettingGroupStorage, sourceForDefaultedEnv("HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS"), strconv.FormatInt(cfg.Attachment.ProcessorMaxAttempts, 10), envConfigured("HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS"), false, DeploymentSettingApplicationStartup)
	} else {
		addRawString("HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS", DeploymentSettingGroupStorage, DeploymentSettingApplicationInactive, add)
	}

	// The witness URL is read only at the existing bootstrap point. Keep an
	// unset placeholder here so every allowlisted key remains represented even
	// when a test or embedded caller supplies an already-open witness pool.
	items = append(items, DeploymentSettingItem{
		Key:   "HOUFENG_DELETION_WITNESS_DATABASE_URL",
		Group: DeploymentSettingGroupDatabase, Source: DeploymentSettingSourceUnset,
		Configured: false, Sensitive: true, Application: DeploymentSettingApplicationStartup,
	})

	// These entries are allowlisted without exposing the DSN or secret-file
	// paths. The values are consumed by bootstrap or reserved to prevent key
	// reuse, rather than being ordinary editable settings.
	addRawConfiguredSecret("HOUFENG_RECORD_DELETION_KEY_FILE", DeploymentSettingGroupRecords, DeploymentSettingApplicationReservationOnly, add)
	addRawConfiguredSecret("HOUFENG_BACKUP_HMAC_KEY_FILE", DeploymentSettingGroupRecords, DeploymentSettingApplicationReservationOnly, add)

	return DeploymentSettingsSnapshot{Items: items}
}

func addRawString(key string, group DeploymentSettingGroup, application DeploymentSettingApplication, add func(string, DeploymentSettingGroup, DeploymentSettingSource, *string, bool, bool, DeploymentSettingApplication)) {
	value, present := os.LookupEnv(key)
	value = strings.TrimSpace(value)
	if !present {
		add(key, group, DeploymentSettingSourceUnset, nil, false, false, application)
		return
	}
	if value == "" {
		add(key, group, DeploymentSettingSourceEnvironment, nil, false, false, application)
		return
	}
	add(key, group, DeploymentSettingSourceEnvironment, new(value), true, false, application)
}

func addRawBool(key string, group DeploymentSettingGroup, application DeploymentSettingApplication, add func(string, DeploymentSettingGroup, DeploymentSettingSource, *string, bool, bool, DeploymentSettingApplication)) {
	value, present := os.LookupEnv(key)
	value = strings.TrimSpace(value)
	if !present {
		add(key, group, DeploymentSettingSourceUnset, nil, false, false, application)
		return
	}
	if value == "" {
		add(key, group, DeploymentSettingSourceEnvironment, nil, false, false, application)
		return
	}
	add(key, group, DeploymentSettingSourceEnvironment, new(value), true, false, application)
}

func addRawSecret(key string, group DeploymentSettingGroup, application DeploymentSettingApplication, add func(string, DeploymentSettingGroup, DeploymentSettingSource, *string, bool, bool, DeploymentSettingApplication)) {
	source, _ := sourceForSecret(key)
	value, present := os.LookupEnv(key)
	filePath, filePresent := os.LookupEnv(key + "_FILE")
	configured := (present && strings.TrimSpace(value) != "") || (filePresent && strings.TrimSpace(filePath) != "")
	add(key, group, source, nil, configured, true, application)
}
func addRawConfiguredSecret(key string, group DeploymentSettingGroup, application DeploymentSettingApplication, add func(string, DeploymentSettingGroup, DeploymentSettingSource, *string, bool, bool, DeploymentSettingApplication)) {
	value, present := os.LookupEnv(key)
	add(key, group, sourceForOptionalEnv(key), nil, present && strings.TrimSpace(value) != "", true, application)
}

func addSafeEndpoint(key string, group DeploymentSettingGroup, value, network string, application DeploymentSettingApplication, add func(string, DeploymentSettingGroup, DeploymentSettingSource, *string, bool, bool, DeploymentSettingApplication)) {
	output := safeEndpointDisplay(value, network)
	add(key, group, sourceForOptionalEnv(key), optionalStringPointer(output), strings.TrimSpace(value) != "", false, application)
}

func addRawSafeEndpoint(key string, group DeploymentSettingGroup, network string, application DeploymentSettingApplication, add func(string, DeploymentSettingGroup, DeploymentSettingSource, *string, bool, bool, DeploymentSettingApplication)) {
	value, present := os.LookupEnv(key)
	value = strings.TrimSpace(value)
	if !present || value == "" {
		add(key, group, sourceForOptionalEnv(key), nil, false, false, application)
		return
	}
	output := safeEndpointDisplay(value, network)
	add(key, group, DeploymentSettingSourceEnvironment, optionalStringPointer(output), true, false, application)
}

func sourceForOptionalEnv(key string) DeploymentSettingSource {
	if _, ok := os.LookupEnv(key); ok {
		return DeploymentSettingSourceEnvironment
	}
	return DeploymentSettingSourceUnset
}

func sourceForDefaultedEnv(key string) DeploymentSettingSource {
	if _, ok := os.LookupEnv(key); ok {
		return DeploymentSettingSourceEnvironment
	}
	return DeploymentSettingSourceDefault
}

func sourceForSecret(key string) (DeploymentSettingSource, bool) {
	if path, ok := os.LookupEnv(key + "_FILE"); ok && strings.TrimSpace(path) != "" {
		return DeploymentSettingSourceFile, true
	}
	if _, ok := os.LookupEnv(key); ok {
		return DeploymentSettingSourceEnvironment, true
	}
	return DeploymentSettingSourceUnset, false
}

func envConfigured(key string) bool {
	value, ok := os.LookupEnv(key)
	return ok && strings.TrimSpace(value) != ""
}

func optionalStringPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return new(value)
}

func safePublicURLDisplay(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "已设置"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String()
}

func safeEndpointDisplay(value, network string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\r\n") || strings.ContainsAny(value, "@?#") || strings.Contains(value, "://") {
		return "已设置"
	}
	if network == "unix" {
		if filepath.IsAbs(value) {
			return value
		}
	}
	if network == "unix" || (network != "" && network != "tcp") || strings.ContainsAny(value, "/\\") {
		return "已设置"
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		return "已设置"
	}
	if net.ParseIP(host) != nil {
		// Only IPv6 literals use brackets. SplitHostPort itself also accepts
		// bracketed DNS names and IPv4 addresses, which are not canonical.
		if strings.HasPrefix(value, "[") != strings.Contains(host, ":") {
			return "已设置"
		}
	} else if strings.HasPrefix(value, "[") || !validDeploymentEndpointHostname(host) {
		return "已设置"
	}
	for index := range len(port) {
		if port[index] < '0' || port[index] > '9' {
			return "已设置"
		}
	}
	if parsedPort, err := strconv.Atoi(port); err != nil || parsedPort < 1 || parsedPort > 65535 {
		return "已设置"
	}
	return value
}

// validDeploymentEndpointHostname checks the presentation allowlist only.
// Unlike asset domain names, endpoint DNS hosts may have a single label.
func validDeploymentEndpointHostname(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	labelLength := 0
	for index := range len(host) {
		character := host[index]
		if character == '.' {
			if labelLength == 0 || host[index-1] == '-' {
				return false
			}
			labelLength = 0
			continue
		}
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
		if labelLength == 0 && character == '-' {
			return false
		}
		labelLength++
		if labelLength > 63 {
			return false
		}
	}
	return labelLength > 0 && host[len(host)-1] != '-'
}
