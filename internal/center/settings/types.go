package settings

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"houfeng/internal/center/targets"
)

const SingletonID = "center"

var ErrInvalidSettings = errors.New("invalid center settings")

type Repository interface {
	GetSettings(context.Context) (CenterSettings, error)
	PutSettings(context.Context, CenterSettings) (CenterSettings, error)
}

type CenterSettings struct {
	Telegram                TelegramSettings         `json:"telegram"`
	FeishuEnabled           bool                     `json:"feishu_enabled"`
	FeishuWebhookURL        string                   `json:"feishu_webhook_url"`
	HostSampleFrequencyTier string                   `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults  ProbeFrequencyDefaults   `json:"probe_frequency_defaults"`
	IncidentDefaults        IncidentDefaults         `json:"incident_defaults"`
	OverrideRules           OverrideRules            `json:"override_rules"`
	RetentionPolicy         RetentionPolicy          `json:"retention_policy"`
	SubscriptionCost        SubscriptionCostSettings `json:"subscription_cost_settings"`
	IPQuality               IPQualitySettings        `json:"ip_quality_settings"`
}

type TelegramSettings struct {
	BotToken       string `json:"bot_token"`
	ChatID         string `json:"chat_id"`
	RuntimeManaged bool   `json:"runtime_managed"`
}

func (t TelegramSettings) Enabled() bool {
	return strings.TrimSpace(t.BotToken) != "" && strings.TrimSpace(t.ChatID) != ""
}

type ProbeFrequencyDefaults struct {
	TCP  string `json:"tcp"`
	HTTP string `json:"http"`
	TLS  string `json:"tls"`
}

type IncidentDefaults struct {
	HeartbeatIntervalSeconds int  `json:"heartbeat_interval_seconds"`
	StaleThresholdIntervals  int  `json:"stale_threshold_intervals"`
	SweepIntervalSeconds     int  `json:"sweep_interval_seconds"`
	NotifyOnStarted          bool `json:"notify_on_started"`
	NotifyOnEscalated        bool `json:"notify_on_escalated"`
	NotifyOnRecovered        bool `json:"notify_on_recovered"`

	CPUWarningPct    int `json:"cpu_warning_pct"`
	CPUAlertPct      int `json:"cpu_alert_pct"`
	CPUCriticalPct   int `json:"cpu_critical_pct"`
	MemWarningPct    int `json:"mem_warning_pct"`
	MemAlertPct      int `json:"mem_alert_pct"`
	MemCriticalPct   int `json:"mem_critical_pct"`
	DiskWarningPct   int `json:"disk_warning_pct"`
	DiskAlertPct     int `json:"disk_alert_pct"`
	DiskCriticalPct  int `json:"disk_critical_pct"`
	InodeWarningPct  int `json:"inode_warning_pct"`
	InodeAlertPct    int `json:"inode_alert_pct"`
	InodeCriticalPct int `json:"inode_critical_pct"`

	IOWaitWarningPct  int     `json:"iowait_warning_pct"`
	IOWaitCriticalPct int     `json:"iowait_critical_pct"`
	Load5Warning      float64 `json:"load5_warning"`
	Load5Critical     float64 `json:"load5_critical"`
}

type OverrideRules struct {
	MonitoringInstanceLabels []MonitoringInstanceLabelOverrideRule `json:"monitoring_instance_labels"`
	TargetTypes              []TargetTypeOverrideRule              `json:"target_types"`
	TargetLabels             []TargetLabelOverrideRule             `json:"target_labels"`
}

type MonitoringInstanceLabelOverrideRule struct {
	Label     string                 `json:"label"`
	Overrides SettingsOverrideFields `json:"overrides"`
}

type TargetTypeOverrideRule struct {
	TargetType string                 `json:"target_type"`
	Overrides  SettingsOverrideFields `json:"overrides"`
}

type TargetLabelOverrideRule struct {
	Label     string                 `json:"label"`
	Overrides SettingsOverrideFields `json:"overrides"`
}

type SettingsOverrideFields struct {
	HostSampleFrequencyTier *string                   `json:"host_sample_frequency_tier,omitempty"`
	ProbeFrequencyDefaults  *ProbeFrequencyOverride   `json:"probe_frequency_defaults,omitempty"`
	IncidentDefaults        *IncidentDefaultsOverride `json:"incident_defaults,omitempty"`
}

type ProbeFrequencyOverride struct {
	TCP  *string `json:"tcp,omitempty"`
	HTTP *string `json:"http,omitempty"`
	TLS  *string `json:"tls,omitempty"`
}

type IncidentDefaultsOverride struct {
	HeartbeatIntervalSeconds *int  `json:"heartbeat_interval_seconds,omitempty"`
	StaleThresholdIntervals  *int  `json:"stale_threshold_intervals,omitempty"`
	SweepIntervalSeconds     *int  `json:"sweep_interval_seconds,omitempty"`
	NotifyOnStarted          *bool `json:"notify_on_started,omitempty"`
	NotifyOnEscalated        *bool `json:"notify_on_escalated,omitempty"`
	NotifyOnRecovered        *bool `json:"notify_on_recovered,omitempty"`

	CPUWarningPct    *int `json:"cpu_warning_pct,omitempty"`
	CPUAlertPct      *int `json:"cpu_alert_pct,omitempty"`
	CPUCriticalPct   *int `json:"cpu_critical_pct,omitempty"`
	MemWarningPct    *int `json:"mem_warning_pct,omitempty"`
	MemAlertPct      *int `json:"mem_alert_pct,omitempty"`
	MemCriticalPct   *int `json:"mem_critical_pct,omitempty"`
	DiskWarningPct   *int `json:"disk_warning_pct,omitempty"`
	DiskAlertPct     *int `json:"disk_alert_pct,omitempty"`
	DiskCriticalPct  *int `json:"disk_critical_pct,omitempty"`
	InodeWarningPct  *int `json:"inode_warning_pct,omitempty"`
	InodeAlertPct    *int `json:"inode_alert_pct,omitempty"`
	InodeCriticalPct *int `json:"inode_critical_pct,omitempty"`

	IOWaitWarningPct  *int     `json:"iowait_warning_pct,omitempty"`
	IOWaitCriticalPct *int     `json:"iowait_critical_pct,omitempty"`
	Load5Warning      *float64 `json:"load5_warning,omitempty"`
	Load5Critical     *float64 `json:"load5_critical,omitempty"`
}

type RetentionPolicy struct {
	RawLayerDays          int `json:"raw_layer_days"`
	AggregateLayerDays    int `json:"aggregate_layer_days"`
	EventLayerDays        int `json:"event_layer_days"`
	NotificationLayerDays int `json:"notification_layer_days"`
}

type SubscriptionExchangeRateProvider string

const (
	SubscriptionExchangeRateProviderFrankfurter SubscriptionExchangeRateProvider = "frankfurter"
	SubscriptionExchangeRateProviderFixer       SubscriptionExchangeRateProvider = "fixer"
)

type SubscriptionCostSettings struct {
	BaseCurrency                string `json:"base_currency"`
	ExchangeRateProvider        string `json:"exchange_rate_provider"`
	FixerAPIKey                 string `json:"fixer_api_key"`
	DefaultReminderOffsetsDays  []int  `json:"default_reminder_offsets_days"`
	MaxReminderLeadDays         int    `json:"max_reminder_lead_days"`
	ExchangeRateStaleAfterHours int    `json:"exchange_rate_stale_after_hours"`
}

type IPQualitySettings struct {
	Enabled              bool     `json:"enabled"`
	FrequencySeconds     int      `json:"frequency_seconds"`
	StaleAfterSeconds    int      `json:"stale_after_seconds"`
	TimeoutSeconds       int      `json:"timeout_seconds"`
	RawRetentionDays     int      `json:"raw_retention_days"`
	HistoryRetentionDays int      `json:"history_retention_days"`
	Services             []string `json:"services"`
}

var defaultIPQualityServices = []string{
	"netflix",
	"chatgpt",
	"youtube-premium",
	"amazon-prime-video",
	"disney-plus",
	"tiktok",
	"reddit",
}

func Default() CenterSettings {
	return CenterSettings{
		HostSampleFrequencyTier: targets.FrequencyTier5s,
		ProbeFrequencyDefaults: ProbeFrequencyDefaults{
			TCP:  targets.FrequencyTier5s,
			HTTP: targets.FrequencyTier5s,
			TLS:  targets.FrequencyTier6h,
		},
		IncidentDefaults: IncidentDefaults{
			HeartbeatIntervalSeconds: 5,
			StaleThresholdIntervals:  3,
			SweepIntervalSeconds:     5,
			NotifyOnStarted:          true,
			NotifyOnEscalated:        true,
			NotifyOnRecovered:        true,
			CPUWarningPct:            80,
			CPUAlertPct:              90,
			CPUCriticalPct:           95,
			MemWarningPct:            85,
			MemAlertPct:              92,
			MemCriticalPct:           95,
			DiskWarningPct:           85,
			DiskAlertPct:             92,
			DiskCriticalPct:          97,
			InodeWarningPct:          80,
			InodeAlertPct:            90,
			InodeCriticalPct:         95,
			IOWaitWarningPct:         20,
			IOWaitCriticalPct:        50,
			Load5Warning:             4.0,
			Load5Critical:            8.0,
		},
		OverrideRules: OverrideRules{
			MonitoringInstanceLabels: []MonitoringInstanceLabelOverrideRule{},
			TargetTypes:              []TargetTypeOverrideRule{},
			TargetLabels:             []TargetLabelOverrideRule{},
		},
		RetentionPolicy: RetentionPolicy{
			RawLayerDays:          30,
			AggregateLayerDays:    30,
			EventLayerDays:        90,
			NotificationLayerDays: 180,
		},
		SubscriptionCost: SubscriptionCostSettings{
			BaseCurrency:                "CNY",
			ExchangeRateProvider:        string(SubscriptionExchangeRateProviderFrankfurter),
			FixerAPIKey:                 "",
			DefaultReminderOffsetsDays:  []int{14, 7, 1},
			MaxReminderLeadDays:         30,
			ExchangeRateStaleAfterHours: 36,
		},
		IPQuality: IPQualitySettings{
			Enabled:              false,
			FrequencySeconds:     24 * 60 * 60,
			StaleAfterSeconds:    7 * 24 * 60 * 60,
			TimeoutSeconds:       15,
			RawRetentionDays:     90,
			HistoryRetentionDays: 365,
			Services:             append([]string(nil), defaultIPQualityServices...),
		},
	}
}

func Validate(input CenterSettings) (CenterSettings, error) {
	input.Telegram.BotToken = strings.TrimSpace(input.Telegram.BotToken)
	input.Telegram.ChatID = strings.TrimSpace(input.Telegram.ChatID)
	input.FeishuWebhookURL = strings.TrimSpace(input.FeishuWebhookURL)
	input.HostSampleFrequencyTier = strings.TrimSpace(input.HostSampleFrequencyTier)

	if (input.Telegram.BotToken == "") != (input.Telegram.ChatID == "") {
		return CenterSettings{}, invalidSettings("telegram bot token and chat id must be set together")
	}
	if !targets.IsValidFrequencyTier(input.HostSampleFrequencyTier) {
		return CenterSettings{}, invalidSettings("host sample frequency tier must be one of the known tiers")
	}

	probeDefaults, err := validateProbeFrequencyDefaults(input.ProbeFrequencyDefaults)
	if err != nil {
		return CenterSettings{}, err
	}
	input.ProbeFrequencyDefaults = probeDefaults

	incidentDefaults, err := validateIncidentDefaults(input.IncidentDefaults)
	if err != nil {
		return CenterSettings{}, err
	}
	input.IncidentDefaults = incidentDefaults

	overrideRules, err := validateOverrideRules(input.OverrideRules, input.IncidentDefaults)
	if err != nil {
		return CenterSettings{}, err
	}
	input.OverrideRules = overrideRules

	retentionPolicy, err := validateRetentionPolicy(input.RetentionPolicy)
	if err != nil {
		return CenterSettings{}, err
	}
	input.RetentionPolicy = retentionPolicy

	subscriptionCost, err := validateSubscriptionCostSettings(input.SubscriptionCost)
	if err != nil {
		return CenterSettings{}, err
	}
	input.SubscriptionCost = subscriptionCost

	ipQuality, err := validateIPQualitySettings(input.IPQuality)
	if err != nil {
		return CenterSettings{}, err
	}
	input.IPQuality = ipQuality

	return input, nil
}

func validateProbeFrequencyDefaults(input ProbeFrequencyDefaults) (ProbeFrequencyDefaults, error) {
	input.TCP = strings.TrimSpace(input.TCP)
	input.HTTP = strings.TrimSpace(input.HTTP)
	input.TLS = strings.TrimSpace(input.TLS)

	if !targets.IsValidFrequencyTier(input.TCP) || !targets.IsValidFrequencyTier(input.HTTP) || !targets.IsValidFrequencyTier(input.TLS) {
		return ProbeFrequencyDefaults{}, invalidSettings("probe frequency defaults must use known tiers")
	}
	return input, nil
}

func validateIncidentDefaults(input IncidentDefaults) (IncidentDefaults, error) {
	if input.HeartbeatIntervalSeconds <= 0 {
		return IncidentDefaults{}, invalidSettings("heartbeat interval must be positive")
	}
	if input.StaleThresholdIntervals <= 0 {
		return IncidentDefaults{}, invalidSettings("stale threshold intervals must be positive")
	}
	if input.SweepIntervalSeconds <= 0 {
		return IncidentDefaults{}, invalidSettings("sweep interval must be positive")
	}

	// Fill in defaults for zero-value thresholds (fields omitted in API input).
	defaults := Default().IncidentDefaults
	applyIntDefault(&input.CPUWarningPct, defaults.CPUWarningPct)
	applyIntDefault(&input.CPUAlertPct, defaults.CPUAlertPct)
	applyIntDefault(&input.CPUCriticalPct, defaults.CPUCriticalPct)
	applyIntDefault(&input.MemWarningPct, defaults.MemWarningPct)
	applyIntDefault(&input.MemAlertPct, defaults.MemAlertPct)
	applyIntDefault(&input.MemCriticalPct, defaults.MemCriticalPct)
	applyIntDefault(&input.DiskWarningPct, defaults.DiskWarningPct)
	applyIntDefault(&input.DiskAlertPct, defaults.DiskAlertPct)
	applyIntDefault(&input.DiskCriticalPct, defaults.DiskCriticalPct)
	applyIntDefault(&input.InodeWarningPct, defaults.InodeWarningPct)
	applyIntDefault(&input.InodeAlertPct, defaults.InodeAlertPct)
	applyIntDefault(&input.InodeCriticalPct, defaults.InodeCriticalPct)
	applyIntDefault(&input.IOWaitWarningPct, defaults.IOWaitWarningPct)
	applyIntDefault(&input.IOWaitCriticalPct, defaults.IOWaitCriticalPct)
	if input.Load5Warning == 0 {
		input.Load5Warning = defaults.Load5Warning
	}
	if input.Load5Critical == 0 {
		input.Load5Critical = defaults.Load5Critical
	}

	thresholdFields := []struct {
		name  string
		value int
	}{
		{"cpu warning pct", input.CPUWarningPct},
		{"cpu alert pct", input.CPUAlertPct},
		{"cpu critical pct", input.CPUCriticalPct},
		{"mem warning pct", input.MemWarningPct},
		{"mem alert pct", input.MemAlertPct},
		{"mem critical pct", input.MemCriticalPct},
		{"disk warning pct", input.DiskWarningPct},
		{"disk alert pct", input.DiskAlertPct},
		{"disk critical pct", input.DiskCriticalPct},
		{"inode warning pct", input.InodeWarningPct},
		{"inode alert pct", input.InodeAlertPct},
		{"inode critical pct", input.InodeCriticalPct},
		{"iowait warning pct", input.IOWaitWarningPct},
		{"iowait critical pct", input.IOWaitCriticalPct},
	}
	for _, f := range thresholdFields {
		if f.value < 1 || f.value > 100 {
			return IncidentDefaults{}, invalidSettings(fmt.Sprintf("%s must be between 1 and 100", f.name))
		}
	}

	if input.Load5Warning <= 0 {
		return IncidentDefaults{}, invalidSettings("load5 warning must be positive")
	}
	if input.Load5Critical <= 0 {
		return IncidentDefaults{}, invalidSettings("load5 critical must be positive")
	}
	if err := validateIncidentThresholdOrder("", input); err != nil {
		return IncidentDefaults{}, err
	}

	return input, nil
}

func applyIntDefault(dst *int, defaultVal int) {
	if *dst == 0 {
		*dst = defaultVal
	}
}

func validateOverrideRules(input OverrideRules, incidentDefaults IncidentDefaults) (OverrideRules, error) {
	if input.MonitoringInstanceLabels == nil {
		input.MonitoringInstanceLabels = []MonitoringInstanceLabelOverrideRule{}
	}
	seenMonitoringInstanceLabels := make(map[string]struct{}, len(input.MonitoringInstanceLabels))
	for i := range input.MonitoringInstanceLabels {
		label := strings.TrimSpace(input.MonitoringInstanceLabels[i].Label)
		if label == "" {
			return OverrideRules{}, invalidSettings("monitoringInstance label override label is required")
		}
		if _, ok := seenMonitoringInstanceLabels[label]; ok {
			return OverrideRules{}, invalidSettings("duplicate monitoringInstance label override selector")
		}
		seenMonitoringInstanceLabels[label] = struct{}{}
		overrides, err := validateSettingsOverrideFields(input.MonitoringInstanceLabels[i].Overrides, incidentDefaults)
		if err != nil {
			return OverrideRules{}, err
		}
		input.MonitoringInstanceLabels[i].Label = label
		input.MonitoringInstanceLabels[i].Overrides = overrides
	}

	if input.TargetTypes == nil {
		input.TargetTypes = []TargetTypeOverrideRule{}
	}
	seenTargetTypes := make(map[string]struct{}, len(input.TargetTypes))
	for i := range input.TargetTypes {
		targetType := strings.TrimSpace(input.TargetTypes[i].TargetType)
		if !targets.IsValidTargetType(targetType) {
			return OverrideRules{}, invalidSettings("target type override target type is invalid")
		}
		if _, ok := seenTargetTypes[targetType]; ok {
			return OverrideRules{}, invalidSettings("duplicate target type override selector")
		}
		seenTargetTypes[targetType] = struct{}{}
		overrides, err := validateSettingsOverrideFields(input.TargetTypes[i].Overrides, incidentDefaults)
		if err != nil {
			return OverrideRules{}, err
		}
		input.TargetTypes[i].TargetType = targetType
		input.TargetTypes[i].Overrides = overrides
	}

	if input.TargetLabels == nil {
		input.TargetLabels = []TargetLabelOverrideRule{}
	}
	seenTargetLabels := make(map[string]struct{}, len(input.TargetLabels))
	for i := range input.TargetLabels {
		label := strings.TrimSpace(input.TargetLabels[i].Label)
		if label == "" {
			return OverrideRules{}, invalidSettings("target label override label is required")
		}
		if _, ok := seenTargetLabels[label]; ok {
			return OverrideRules{}, invalidSettings("duplicate target label override selector")
		}
		seenTargetLabels[label] = struct{}{}
		overrides, err := validateSettingsOverrideFields(input.TargetLabels[i].Overrides, incidentDefaults)
		if err != nil {
			return OverrideRules{}, err
		}
		input.TargetLabels[i].Label = label
		input.TargetLabels[i].Overrides = overrides
	}

	return input, nil
}

func validateSettingsOverrideFields(input SettingsOverrideFields, incidentDefaults IncidentDefaults) (SettingsOverrideFields, error) {
	hasOverride := false

	if input.HostSampleFrequencyTier != nil {
		tier := strings.TrimSpace(*input.HostSampleFrequencyTier)
		if !targets.IsValidFrequencyTier(tier) {
			return SettingsOverrideFields{}, invalidSettings("override host sample frequency tier must be known")
		}
		input.HostSampleFrequencyTier = &tier
		hasOverride = true
	}

	if input.ProbeFrequencyDefaults != nil {
		override, err := validateProbeFrequencyOverride(*input.ProbeFrequencyDefaults)
		if err != nil {
			return SettingsOverrideFields{}, err
		}
		input.ProbeFrequencyDefaults = &override
		hasOverride = true
	}

	if input.IncidentDefaults != nil {
		override, err := validateIncidentDefaultsOverride(*input.IncidentDefaults, incidentDefaults)
		if err != nil {
			return SettingsOverrideFields{}, err
		}
		input.IncidentDefaults = &override
		hasOverride = true
	}

	if !hasOverride {
		return SettingsOverrideFields{}, invalidSettings("override rule must specify at least one override")
	}

	return input, nil
}

func validateProbeFrequencyOverride(input ProbeFrequencyOverride) (ProbeFrequencyOverride, error) {
	hasOverride := false
	for _, field := range []*string{input.TCP, input.HTTP, input.TLS} {
		if field == nil {
			continue
		}
		hasOverride = true
		tier := strings.TrimSpace(*field)
		if !targets.IsValidFrequencyTier(tier) {
			return ProbeFrequencyOverride{}, invalidSettings("probe frequency override must use known tiers")
		}
		*field = tier
	}
	if !hasOverride {
		return ProbeFrequencyOverride{}, invalidSettings("probe frequency override must set at least one kind")
	}
	return input, nil
}

func validateIncidentDefaultsOverride(input IncidentDefaultsOverride, incidentDefaults IncidentDefaults) (IncidentDefaultsOverride, error) {
	hasOverride := false

	if input.HeartbeatIntervalSeconds != nil {
		hasOverride = true
		if *input.HeartbeatIntervalSeconds <= 0 {
			return IncidentDefaultsOverride{}, invalidSettings("override heartbeat interval must be positive")
		}
	}
	if input.StaleThresholdIntervals != nil {
		hasOverride = true
		if *input.StaleThresholdIntervals <= 0 {
			return IncidentDefaultsOverride{}, invalidSettings("override stale threshold must be positive")
		}
	}
	if input.SweepIntervalSeconds != nil {
		hasOverride = true
		if *input.SweepIntervalSeconds <= 0 {
			return IncidentDefaultsOverride{}, invalidSettings("override sweep interval must be positive")
		}
	}
	if input.NotifyOnStarted != nil || input.NotifyOnEscalated != nil || input.NotifyOnRecovered != nil {
		hasOverride = true
	}

	thresholdPtrs := []struct {
		name string
		ptr  *int
	}{
		{"cpu warning pct", input.CPUWarningPct},
		{"cpu alert pct", input.CPUAlertPct},
		{"cpu critical pct", input.CPUCriticalPct},
		{"mem warning pct", input.MemWarningPct},
		{"mem alert pct", input.MemAlertPct},
		{"mem critical pct", input.MemCriticalPct},
		{"disk warning pct", input.DiskWarningPct},
		{"disk alert pct", input.DiskAlertPct},
		{"disk critical pct", input.DiskCriticalPct},
		{"inode warning pct", input.InodeWarningPct},
		{"inode alert pct", input.InodeAlertPct},
		{"inode critical pct", input.InodeCriticalPct},
		{"iowait warning pct", input.IOWaitWarningPct},
		{"iowait critical pct", input.IOWaitCriticalPct},
	}
	for _, f := range thresholdPtrs {
		if f.ptr == nil {
			continue
		}
		hasOverride = true
		if *f.ptr < 1 || *f.ptr > 100 {
			return IncidentDefaultsOverride{}, invalidSettings(fmt.Sprintf("override %s must be between 1 and 100", f.name))
		}
	}

	if input.Load5Warning != nil {
		hasOverride = true
		if *input.Load5Warning <= 0 {
			return IncidentDefaultsOverride{}, invalidSettings("override load5 warning must be positive")
		}
	}
	if input.Load5Critical != nil {
		hasOverride = true
		if *input.Load5Critical <= 0 {
			return IncidentDefaultsOverride{}, invalidSettings("override load5 critical must be positive")
		}
	}

	if !hasOverride {
		return IncidentDefaultsOverride{}, invalidSettings("incident override must set at least one field")
	}
	if err := validateIncidentThresholdOrder("override ", incidentDefaultsFromOverride(input, incidentDefaults)); err != nil {
		return IncidentDefaultsOverride{}, err
	}
	return input, nil
}

func incidentDefaultsFromOverride(input IncidentDefaultsOverride, defaults IncidentDefaults) IncidentDefaults {
	if input.CPUWarningPct != nil {
		defaults.CPUWarningPct = *input.CPUWarningPct
	}
	if input.CPUAlertPct != nil {
		defaults.CPUAlertPct = *input.CPUAlertPct
	}
	if input.CPUCriticalPct != nil {
		defaults.CPUCriticalPct = *input.CPUCriticalPct
	}
	if input.MemWarningPct != nil {
		defaults.MemWarningPct = *input.MemWarningPct
	}
	if input.MemAlertPct != nil {
		defaults.MemAlertPct = *input.MemAlertPct
	}
	if input.MemCriticalPct != nil {
		defaults.MemCriticalPct = *input.MemCriticalPct
	}
	if input.DiskWarningPct != nil {
		defaults.DiskWarningPct = *input.DiskWarningPct
	}
	if input.DiskAlertPct != nil {
		defaults.DiskAlertPct = *input.DiskAlertPct
	}
	if input.DiskCriticalPct != nil {
		defaults.DiskCriticalPct = *input.DiskCriticalPct
	}
	if input.InodeWarningPct != nil {
		defaults.InodeWarningPct = *input.InodeWarningPct
	}
	if input.InodeAlertPct != nil {
		defaults.InodeAlertPct = *input.InodeAlertPct
	}
	if input.InodeCriticalPct != nil {
		defaults.InodeCriticalPct = *input.InodeCriticalPct
	}
	if input.IOWaitWarningPct != nil {
		defaults.IOWaitWarningPct = *input.IOWaitWarningPct
	}
	if input.IOWaitCriticalPct != nil {
		defaults.IOWaitCriticalPct = *input.IOWaitCriticalPct
	}
	if input.Load5Warning != nil {
		defaults.Load5Warning = *input.Load5Warning
	}
	if input.Load5Critical != nil {
		defaults.Load5Critical = *input.Load5Critical
	}
	return defaults
}

func validateIncidentThresholdOrder(prefix string, input IncidentDefaults) error {
	threeLevelThresholds := []struct {
		name     string
		warning  int
		alert    int
		critical int
	}{
		{name: "cpu", warning: input.CPUWarningPct, alert: input.CPUAlertPct, critical: input.CPUCriticalPct},
		{name: "mem", warning: input.MemWarningPct, alert: input.MemAlertPct, critical: input.MemCriticalPct},
		{name: "disk", warning: input.DiskWarningPct, alert: input.DiskAlertPct, critical: input.DiskCriticalPct},
		{name: "inode", warning: input.InodeWarningPct, alert: input.InodeAlertPct, critical: input.InodeCriticalPct},
	}
	for _, t := range threeLevelThresholds {
		if !(t.warning < t.alert && t.alert < t.critical) {
			return invalidSettings(fmt.Sprintf("%s%s thresholds must satisfy warning < alert < critical", prefix, t.name))
		}
	}

	if input.IOWaitWarningPct >= input.IOWaitCriticalPct {
		return invalidSettings(fmt.Sprintf("%siowait thresholds must satisfy warning < critical", prefix))
	}
	if input.Load5Warning >= input.Load5Critical {
		return invalidSettings(fmt.Sprintf("%sload5 thresholds must satisfy warning < critical", prefix))
	}
	return nil
}

func validateRetentionPolicy(input RetentionPolicy) (RetentionPolicy, error) {
	if input.RawLayerDays < 30 {
		return RetentionPolicy{}, invalidSettings("raw retention days must be at least 30")
	}
	if input.AggregateLayerDays <= 0 {
		return RetentionPolicy{}, invalidSettings("aggregate retention days must be positive")
	}
	if input.EventLayerDays <= 0 {
		return RetentionPolicy{}, invalidSettings("event retention days must be positive")
	}
	if input.NotificationLayerDays <= 0 {
		return RetentionPolicy{}, invalidSettings("notification retention days must be positive")
	}
	return input, nil
}

func validateSubscriptionCostSettings(input SubscriptionCostSettings) (SubscriptionCostSettings, error) {
	defaults := Default().SubscriptionCost

	input.BaseCurrency = strings.ToUpper(strings.TrimSpace(input.BaseCurrency))
	if input.BaseCurrency == "" {
		input.BaseCurrency = defaults.BaseCurrency
	}
	if !isCurrencyCode(input.BaseCurrency) {
		return SubscriptionCostSettings{}, invalidSettings("subscription base currency must be a 3-letter uppercase code")
	}

	input.ExchangeRateProvider = strings.ToLower(strings.TrimSpace(input.ExchangeRateProvider))
	if input.ExchangeRateProvider == "" {
		input.ExchangeRateProvider = defaults.ExchangeRateProvider
	}
	switch SubscriptionExchangeRateProvider(input.ExchangeRateProvider) {
	case SubscriptionExchangeRateProviderFrankfurter, SubscriptionExchangeRateProviderFixer:
	default:
		return SubscriptionCostSettings{}, invalidSettings("subscription exchange rate provider is invalid")
	}

	input.FixerAPIKey = strings.TrimSpace(input.FixerAPIKey)
	if input.MaxReminderLeadDays == 0 {
		input.MaxReminderLeadDays = defaults.MaxReminderLeadDays
	}
	if input.MaxReminderLeadDays < 1 || input.MaxReminderLeadDays > 365 {
		return SubscriptionCostSettings{}, invalidSettings("subscription max reminder lead days must be between 1 and 365")
	}

	input.DefaultReminderOffsetsDays = normalizeReminderOffsets(input.DefaultReminderOffsetsDays)
	if len(input.DefaultReminderOffsetsDays) == 0 {
		input.DefaultReminderOffsetsDays = append([]int(nil), defaults.DefaultReminderOffsetsDays...)
	}
	for _, offset := range input.DefaultReminderOffsetsDays {
		if offset < 0 {
			return SubscriptionCostSettings{}, invalidSettings("subscription reminder offsets must be non-negative")
		}
		if offset > input.MaxReminderLeadDays {
			return SubscriptionCostSettings{}, invalidSettings("subscription reminder offsets cannot exceed max reminder lead days")
		}
	}

	if input.ExchangeRateStaleAfterHours == 0 {
		input.ExchangeRateStaleAfterHours = defaults.ExchangeRateStaleAfterHours
	}
	if input.ExchangeRateStaleAfterHours < 1 || input.ExchangeRateStaleAfterHours > 24*14 {
		return SubscriptionCostSettings{}, invalidSettings("subscription exchange rate stale hours must be between 1 and 336")
	}

	return input, nil
}

func validateIPQualitySettings(input IPQualitySettings) (IPQualitySettings, error) {
	defaults := Default().IPQuality
	if isZeroIPQualitySettings(input) {
		return defaults, nil
	}
	if input.FrequencySeconds < 60 {
		return IPQualitySettings{}, invalidSettings("ip quality frequency seconds must be at least 60")
	}
	if input.StaleAfterSeconds == 0 {
		input.StaleAfterSeconds = defaults.StaleAfterSeconds
	}
	if input.StaleAfterSeconds < input.FrequencySeconds {
		return IPQualitySettings{}, invalidSettings("ip quality stale after seconds must be at least frequency seconds")
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > 300 {
		return IPQualitySettings{}, invalidSettings("ip quality timeout seconds must be between 1 and 300")
	}
	if input.RawRetentionDays < 7 {
		return IPQualitySettings{}, invalidSettings("ip quality raw retention days must be at least 7")
	}
	if input.HistoryRetentionDays < input.RawRetentionDays {
		return IPQualitySettings{}, invalidSettings("ip quality history retention days must be at least raw retention days")
	}
	if input.Services == nil {
		input.Services = append([]string(nil), defaults.Services...)
	} else {
		services, err := normalizeIPQualityServices(input.Services)
		if err != nil {
			return IPQualitySettings{}, err
		}
		input.Services = services
	}
	if len(input.Services) == 0 {
		return IPQualitySettings{}, invalidSettings("ip quality services must not be empty")
	}
	return input, nil
}

func isZeroIPQualitySettings(input IPQualitySettings) bool {
	return !input.Enabled &&
		input.FrequencySeconds == 0 &&
		input.StaleAfterSeconds == 0 &&
		input.TimeoutSeconds == 0 &&
		input.RawRetentionDays == 0 &&
		input.HistoryRetentionDays == 0 &&
		len(input.Services) == 0
}

func normalizeIPQualityServices(values []string) ([]string, error) {
	allowed := make(map[string]struct{}, len(defaultIPQualityServices))
	for _, service := range defaultIPQualityServices {
		allowed[service] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		service := strings.ToLower(strings.TrimSpace(value))
		if service == "" {
			continue
		}
		if _, ok := allowed[service]; !ok {
			return nil, invalidSettings("ip quality service is invalid")
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		normalized = append(normalized, service)
	}
	return normalized, nil
}

func normalizeReminderOffsets(values []int) []int {
	if values == nil {
		return nil
	}
	seen := make(map[int]struct{}, len(values))
	normalized := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(normalized)))
	return normalized
}

func isCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, ch := range value {
		if ch < 'A' || ch > 'Z' {
			return false
		}
	}
	return true
}

func invalidSettings(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidSettings, message)
}
