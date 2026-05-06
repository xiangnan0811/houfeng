package settings

import (
	"context"
	"errors"
	"fmt"
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
	Telegram                TelegramSettings       `json:"telegram"`
	HostSampleFrequencyTier string                 `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults  ProbeFrequencyDefaults `json:"probe_frequency_defaults"`
	IncidentDefaults        IncidentDefaults       `json:"incident_defaults"`
	OverrideRules           OverrideRules          `json:"override_rules"`
	RetentionPolicy         RetentionPolicy        `json:"retention_policy"`
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
}

type OverrideRules struct {
	NodeLabels   []NodeLabelOverrideRule   `json:"node_labels"`
	TargetTypes  []TargetTypeOverrideRule  `json:"target_types"`
	TargetLabels []TargetLabelOverrideRule `json:"target_labels"`
}

type NodeLabelOverrideRule struct {
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
}

type RetentionPolicy struct {
	RawLayerDays          int `json:"raw_layer_days"`
	AggregateLayerDays    int `json:"aggregate_layer_days"`
	EventLayerDays        int `json:"event_layer_days"`
	NotificationLayerDays int `json:"notification_layer_days"`
}

func Default() CenterSettings {
	return CenterSettings{
		HostSampleFrequencyTier: targets.FrequencyTier5m,
		ProbeFrequencyDefaults: ProbeFrequencyDefaults{
			TCP:  targets.FrequencyTier5m,
			HTTP: targets.FrequencyTier5m,
			TLS:  targets.FrequencyTier6h,
		},
		IncidentDefaults: IncidentDefaults{
			HeartbeatIntervalSeconds: 30,
			StaleThresholdIntervals:  3,
			SweepIntervalSeconds:     60,
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
		},
		OverrideRules: OverrideRules{
			NodeLabels:   []NodeLabelOverrideRule{},
			TargetTypes:  []TargetTypeOverrideRule{},
			TargetLabels: []TargetLabelOverrideRule{},
		},
		RetentionPolicy: RetentionPolicy{
			RawLayerDays:          7,
			AggregateLayerDays:    30,
			EventLayerDays:        90,
			NotificationLayerDays: 180,
		},
	}
}

func Validate(input CenterSettings) (CenterSettings, error) {
	input.Telegram.BotToken = strings.TrimSpace(input.Telegram.BotToken)
	input.Telegram.ChatID = strings.TrimSpace(input.Telegram.ChatID)
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

	overrideRules, err := validateOverrideRules(input.OverrideRules)
	if err != nil {
		return CenterSettings{}, err
	}
	input.OverrideRules = overrideRules

	retentionPolicy, err := validateRetentionPolicy(input.RetentionPolicy)
	if err != nil {
		return CenterSettings{}, err
	}
	input.RetentionPolicy = retentionPolicy

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
	}
	for _, f := range thresholdFields {
		if f.value < 1 || f.value > 100 {
			return IncidentDefaults{}, invalidSettings(fmt.Sprintf("%s must be between 1 and 100", f.name))
		}
	}

	return input, nil
}

func applyIntDefault(dst *int, defaultVal int) {
	if *dst == 0 {
		*dst = defaultVal
	}
}

func validateOverrideRules(input OverrideRules) (OverrideRules, error) {
	if input.NodeLabels == nil {
		input.NodeLabels = []NodeLabelOverrideRule{}
	}
	seenNodeLabels := make(map[string]struct{}, len(input.NodeLabels))
	for i := range input.NodeLabels {
		label := strings.TrimSpace(input.NodeLabels[i].Label)
		if label == "" {
			return OverrideRules{}, invalidSettings("node label override label is required")
		}
		if _, ok := seenNodeLabels[label]; ok {
			return OverrideRules{}, invalidSettings("duplicate node label override selector")
		}
		seenNodeLabels[label] = struct{}{}
		overrides, err := validateSettingsOverrideFields(input.NodeLabels[i].Overrides)
		if err != nil {
			return OverrideRules{}, err
		}
		input.NodeLabels[i].Label = label
		input.NodeLabels[i].Overrides = overrides
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
		overrides, err := validateSettingsOverrideFields(input.TargetTypes[i].Overrides)
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
		overrides, err := validateSettingsOverrideFields(input.TargetLabels[i].Overrides)
		if err != nil {
			return OverrideRules{}, err
		}
		input.TargetLabels[i].Label = label
		input.TargetLabels[i].Overrides = overrides
	}

	return input, nil
}

func validateSettingsOverrideFields(input SettingsOverrideFields) (SettingsOverrideFields, error) {
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
		override, err := validateIncidentDefaultsOverride(*input.IncidentDefaults)
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

func validateIncidentDefaultsOverride(input IncidentDefaultsOverride) (IncidentDefaultsOverride, error) {
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

	if !hasOverride {
		return IncidentDefaultsOverride{}, invalidSettings("incident override must set at least one field")
	}
	return input, nil
}

func validateRetentionPolicy(input RetentionPolicy) (RetentionPolicy, error) {
	if input.RawLayerDays <= 0 {
		return RetentionPolicy{}, invalidSettings("raw retention days must be positive")
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

func invalidSettings(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidSettings, message)
}
