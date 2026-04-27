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
		},
		OverrideRules: OverrideRules{
			NodeLabels: []NodeLabelOverrideRule{
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

func TestSettingsValidateRejectsDuplicateNodeLabelOverrides(t *testing.T) {
	t.Parallel()

	input := Default()
	input.OverrideRules = OverrideRules{
		NodeLabels: []NodeLabelOverrideRule{
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
	if got.HostSampleFrequencyTier != "5m" {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", got.HostSampleFrequencyTier, "5m")
	}
	if got.ProbeFrequencyDefaults.HTTP != "5m" {
		t.Fatalf("ProbeFrequencyDefaults.HTTP = %q, want %q", got.ProbeFrequencyDefaults.HTTP, "5m")
	}
	if got.Telegram.RuntimeManaged {
		t.Fatal("Telegram.RuntimeManaged = true, want false by default")
	}
	if got.RetentionPolicy.EventLayerDays <= 0 {
		t.Fatalf("EventLayerDays = %d, want positive", got.RetentionPolicy.EventLayerDays)
	}
}

func stringPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }
