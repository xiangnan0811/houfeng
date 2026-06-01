package settings

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"houfeng/internal/center/targets"
)

func TestSettingsValidateAcceptsStructuredSettings(t *testing.T) {
	t.Parallel()

	input := CenterSettings{
		Telegram: TelegramSettings{
			BotToken:       " bot-token ",
			ChatID:         " chat-id ",
			RuntimeManaged: true,
		},
		HostSampleFrequencyTier: " 1m ",
		ProbeFrequencyDefaults: ProbeFrequencyDefaults{
			TCP:  "5m",
			HTTP: "1m",
			TLS:  "15m",
		},
		IncidentDefaults: IncidentDefaults{
			HeartbeatIntervalSeconds: 45,
			StaleThresholdIntervals:  4,
			SweepIntervalSeconds:     120,
			NotifyOnStarted:          true,
			NotifyOnEscalated:        true,
			NotifyOnRecovered:        false,
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
			MonitoringInstanceLabels: []MonitoringInstanceLabelOverrideRule{
				{
					Label: "core",
					Overrides: SettingsOverrideFields{
						HostSampleFrequencyTier: stringPtr("1m"),
					},
				},
			},
			TargetTypes: []TargetTypeOverrideRule{
				{
					TargetType: targets.TargetTypeService,
					Overrides: SettingsOverrideFields{
						ProbeFrequencyDefaults: &ProbeFrequencyOverride{
							HTTP: stringPtr("5m"),
						},
					},
				},
			},
			TargetLabels: []TargetLabelOverrideRule{
				{
					Label: "external",
					Overrides: SettingsOverrideFields{
						IncidentDefaults: &IncidentDefaultsOverride{
							SweepIntervalSeconds: intPtr(90),
						},
					},
				},
			},
		},
		RetentionPolicy: RetentionPolicy{
			RawLayerDays:          7,
			AggregateLayerDays:    30,
			EventLayerDays:        90,
			NotificationLayerDays: 180,
		},
	}

	got, err := Validate(input)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got.Telegram.BotToken != "bot-token" {
		t.Fatalf("BotToken = %q, want %q", got.Telegram.BotToken, "bot-token")
	}
	if got.Telegram.ChatID != "chat-id" {
		t.Fatalf("ChatID = %q, want %q", got.Telegram.ChatID, "chat-id")
	}
	if !got.Telegram.RuntimeManaged {
		t.Fatal("RuntimeManaged = false, want true")
	}
	if !got.Telegram.Enabled() {
		t.Fatal("Telegram.Enabled() = false, want true")
	}
	if got.HostSampleFrequencyTier != "1m" {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", got.HostSampleFrequencyTier, "1m")
	}
	if got.OverrideRules.TargetTypes[0].TargetType != targets.TargetTypeService {
		t.Fatalf("TargetType = %q, want %q", got.OverrideRules.TargetTypes[0].TargetType, targets.TargetTypeService)
	}
	body, err := json.Marshal(got.RetentionPolicy)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(body) != `{"raw_layer_days":7,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}` {
		t.Fatalf("RetentionPolicy JSON = %s", body)
	}
}

func TestSettingsValidateRequiresTelegramPair(t *testing.T) {
	t.Parallel()

	_, err := Validate(CenterSettings{
		Telegram: TelegramSettings{
			BotToken: "bot-token",
		},
	})
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSettings", err)
	}
}

func TestSettingsValidateRejectsUnknownFrequencyTier(t *testing.T) {
	t.Parallel()

	_, err := Validate(CenterSettings{
		HostSampleFrequencyTier: "30s",
	})
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSettings", err)
	}
}

func TestSettingsValidateRejectsInvalidOverrideScope(t *testing.T) {
	t.Parallel()

	_, err := Validate(CenterSettings{
		OverrideRules: OverrideRules{
			TargetTypes: []TargetTypeOverrideRule{
				{
					TargetType: "custom",
					Overrides: SettingsOverrideFields{
						HostSampleFrequencyTier: stringPtr("1m"),
					},
				},
			},
		},
	})
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSettings", err)
	}
}

func TestSettingsValidateRejectsDuplicateMonitoringInstanceLabelOverrides(t *testing.T) {
	t.Parallel()

	input := Default()
	input.OverrideRules = OverrideRules{
		MonitoringInstanceLabels: []MonitoringInstanceLabelOverrideRule{
			{
				Label: " core ",
				Overrides: SettingsOverrideFields{
					HostSampleFrequencyTier: stringPtr("1m"),
				},
			},
			{
				Label: "core",
				Overrides: SettingsOverrideFields{
					HostSampleFrequencyTier: stringPtr("5m"),
				},
			},
		},
	}

	_, err := Validate(input)
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSettings", err)
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Validate() error = %v, want duplicate selector error", err)
	}
}

func TestSettingsValidateRejectsDuplicateTargetTypeOverrides(t *testing.T) {
	t.Parallel()

	input := Default()
	input.OverrideRules = OverrideRules{
		TargetTypes: []TargetTypeOverrideRule{
			{
				TargetType: targets.TargetTypeService,
				Overrides: SettingsOverrideFields{
					HostSampleFrequencyTier: stringPtr("1m"),
				},
			},
			{
				TargetType: " " + targets.TargetTypeService + " ",
				Overrides: SettingsOverrideFields{
					HostSampleFrequencyTier: stringPtr("5m"),
				},
			},
		},
	}

	_, err := Validate(input)
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSettings", err)
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Validate() error = %v, want duplicate selector error", err)
	}
}

func TestSettingsValidateRejectsDuplicateTargetLabelOverrides(t *testing.T) {
	t.Parallel()

	input := Default()
	input.OverrideRules = OverrideRules{
		TargetLabels: []TargetLabelOverrideRule{
			{
				Label: " edge ",
				Overrides: SettingsOverrideFields{
					HostSampleFrequencyTier: stringPtr("1m"),
				},
			},
			{
				Label: "edge",
				Overrides: SettingsOverrideFields{
					HostSampleFrequencyTier: stringPtr("5m"),
				},
			},
		},
	}

	_, err := Validate(input)
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSettings", err)
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Validate() error = %v, want duplicate selector error", err)
	}
}

func TestSettingsDefaultProvidesDeterministicSingletonShape(t *testing.T) {
	t.Parallel()

	got, err := Validate(Default())
	if err != nil {
		t.Fatalf("Validate(Default()) error = %v", err)
	}
	if got.HostSampleFrequencyTier != "5s" {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", got.HostSampleFrequencyTier, "5s")
	}
	if got.ProbeFrequencyDefaults.TCP != "5s" {
		t.Fatalf("ProbeFrequencyDefaults.TCP = %q, want %q", got.ProbeFrequencyDefaults.TCP, "5s")
	}
	if got.ProbeFrequencyDefaults.HTTP != "5s" {
		t.Fatalf("ProbeFrequencyDefaults.HTTP = %q, want %q", got.ProbeFrequencyDefaults.HTTP, "5s")
	}
	if got.ProbeFrequencyDefaults.TLS != "6h" {
		t.Fatalf("ProbeFrequencyDefaults.TLS = %q, want %q", got.ProbeFrequencyDefaults.TLS, "6h")
	}
	if got.IncidentDefaults.HeartbeatIntervalSeconds != 5 {
		t.Fatalf("HeartbeatIntervalSeconds = %d, want 5", got.IncidentDefaults.HeartbeatIntervalSeconds)
	}
	if got.IncidentDefaults.SweepIntervalSeconds != 5 {
		t.Fatalf("SweepIntervalSeconds = %d, want 5", got.IncidentDefaults.SweepIntervalSeconds)
	}
	if got.Telegram.RuntimeManaged {
		t.Fatal("Telegram.RuntimeManaged = true, want false by default")
	}
	if got.RetentionPolicy.EventLayerDays <= 0 {
		t.Fatalf("EventLayerDays = %d, want positive", got.RetentionPolicy.EventLayerDays)
	}
	if got.IncidentDefaults.CPUWarningPct != 80 {
		t.Fatalf("CPUWarningPct = %d, want 80", got.IncidentDefaults.CPUWarningPct)
	}
	if got.IncidentDefaults.CPUCriticalPct != 95 {
		t.Fatalf("CPUCriticalPct = %d, want 95", got.IncidentDefaults.CPUCriticalPct)
	}
	if got.IncidentDefaults.MemCriticalPct != 95 {
		t.Fatalf("MemCriticalPct = %d, want 95", got.IncidentDefaults.MemCriticalPct)
	}
	if got.IncidentDefaults.DiskCriticalPct != 97 {
		t.Fatalf("DiskCriticalPct = %d, want 97", got.IncidentDefaults.DiskCriticalPct)
	}
	if got.IncidentDefaults.InodeWarningPct != 80 {
		t.Fatalf("InodeWarningPct = %d, want 80", got.IncidentDefaults.InodeWarningPct)
	}
	if got.FeishuEnabled {
		t.Fatal("FeishuEnabled = true, want false by default")
	}
	if got.FeishuWebhookURL != "" {
		t.Fatalf("FeishuWebhookURL = %q, want empty by default", got.FeishuWebhookURL)
	}
}

func TestSettingsValidateRejectsOutOfRangeThreshold(t *testing.T) {
	t.Parallel()

	input := Default()
	input.IncidentDefaults.CPUWarningPct = 101
	_, err := Validate(input)
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSettings", err)
	}
	if !strings.Contains(err.Error(), "cpu warning pct") {
		t.Fatalf("Validate() error = %v, want mention of cpu warning pct", err)
	}

	input = Default()
	input.IncidentDefaults.MemCriticalPct = -5
	_, err = Validate(input)
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSettings for negative threshold", err)
	}

	input = Default()
	input.IncidentDefaults.DiskAlertPct = 0
	// 0 is filled with default (92) in validate, so it should pass
	got, err := Validate(input)
	if err != nil {
		t.Fatalf("Validate() error = %v, want nil (0 should be filled with default)", err)
	}
	if got.IncidentDefaults.DiskAlertPct != 92 {
		t.Fatalf("DiskAlertPct = %d, want 92 (default filled)", got.IncidentDefaults.DiskAlertPct)
	}
}

func TestSettingsValidateRejectsOutOfRangeOverrideThreshold(t *testing.T) {
	t.Parallel()

	input := Default()
	input.OverrideRules = OverrideRules{
		MonitoringInstanceLabels: []MonitoringInstanceLabelOverrideRule{{
			Label: "core",
			Overrides: SettingsOverrideFields{
				IncidentDefaults: &IncidentDefaultsOverride{
					CPUCriticalPct: intPtr(200),
				},
			},
		}},
	}
	_, err := Validate(input)
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSettings", err)
	}
	if !strings.Contains(err.Error(), "cpu critical pct") {
		t.Fatalf("Validate() error = %v, want mention of cpu critical pct", err)
	}
}

func stringPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }
