package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	centersettings "houfeng/internal/center/settings"
)

func TestCenterSettingsRepositoryGetSettingsReturnsDefaultsWithoutCreatingSingletonWhenMissing(t *testing.T) {
	t.Parallel()

	queryCount := 0
	repo := &PostgresSettingsRepository{db: fakeSettingsQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			switch {
			case sql == getCenterSettingsSQL && len(args) == 1 && args[0] == centersettings.SingletonID && queryCount == 1:
				return fakeSettingsRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			default:
				return fakeSettingsRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
		},
	}}

	got, err := repo.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	if !reflect.DeepEqual(got, centersettings.Default()) {
		t.Fatalf("GetSettings() = %#v, want %#v", got, centersettings.Default())
	}
	if queryCount != 1 {
		t.Fatalf("queryCount = %d, want 1", queryCount)
	}
}

func TestCenterSettingsRepositoryPutSettingsRoundTripsStructuredSections(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 26, 10, 0, 0, 0, time.UTC)
	input := centersettings.CenterSettings{
		Telegram: centersettings.TelegramSettings{
			BotToken:       "bot-token",
			ChatID:         "chat-id",
			RuntimeManaged: true,
		},
		HostSampleFrequencyTier: "1m",
		ProbeFrequencyDefaults: centersettings.ProbeFrequencyDefaults{
			TCP:  "5m",
			HTTP: "1m",
			TLS:  "15m",
		},
		IncidentDefaults: centersettings.IncidentDefaults{
			HeartbeatIntervalSeconds: 60,
			StaleThresholdIntervals:  5,
			SweepIntervalSeconds:     180,
			NotifyOnStarted:          true,
			NotifyOnEscalated:        false,
			NotifyOnRecovered:        true,
		},
		OverrideRules: centersettings.OverrideRules{
			NodeLabels: []centersettings.NodeLabelOverrideRule{
				{
					Label: "core",
					Overrides: centersettings.SettingsOverrideFields{
						HostSampleFrequencyTier: settingsStringPtr("1m"),
					},
				},
			},
		},
		RetentionPolicy: centersettings.RetentionPolicy{
			RawLayerDays:          14,
			AggregateLayerDays:    60,
			EventLayerDays:        180,
			NotificationLayerDays: 365,
		},
	}

	var seenArgs []any
	repo := &PostgresSettingsRepository{db: fakeSettingsQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != upsertCenterSettingsSQL {
				return fakeSettingsRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
			seenArgs = append([]any(nil), args...)
			return fakeSettingsRow{scan: func(dest ...any) error {
				scanCenterSettingsRow(dest, input)
				*(dest[11].(*time.Time)) = now
				*(dest[12].(*time.Time)) = now
				return nil
			}}
		},
	}}

	got, err := repo.PutSettings(context.Background(), input)
	if err != nil {
		t.Fatalf("PutSettings() error = %v", err)
	}
	if got.RetentionPolicy.NotificationLayerDays != 365 {
		t.Fatalf("NotificationLayerDays = %d, want 365", got.RetentionPolicy.NotificationLayerDays)
	}
	if len(got.OverrideRules.NodeLabels) != 1 {
		t.Fatalf("len(NodeLabels) = %d, want 1", len(got.OverrideRules.NodeLabels))
	}
	if len(seenArgs) != 11 {
		t.Fatalf("len(args) = %d, want 11", len(seenArgs))
	}
	if seenArgs[0] != centersettings.SingletonID {
		t.Fatalf("settings_id = %#v, want %q", seenArgs[0], centersettings.SingletonID)
	}
	if got, ok := seenArgs[3].(bool); !ok || !got {
		t.Fatalf("telegram_runtime_managed = %#v, want true", seenArgs[3])
	}
	assertJSONArgContains(t, seenArgs[9], `"node_labels":[{"label":"core"`)
	assertJSONArgContains(t, seenArgs[10], `"raw_layer_days":14`)
}

func TestCenterSettingsRepositoryPutSettingsValidatesBeforeWriting(t *testing.T) {
	t.Parallel()

	called := false
	repo := &PostgresSettingsRepository{db: fakeSettingsQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			called = true
			return fakeSettingsRow{scan: func(dest ...any) error { return nil }}
		},
	}}

	_, err := repo.PutSettings(context.Background(), centersettings.CenterSettings{
		Telegram: centersettings.TelegramSettings{BotToken: "bot-token"},
	})
	if !errors.Is(err, centersettings.ErrInvalidSettings) {
		t.Fatalf("PutSettings() error = %v, want ErrInvalidSettings", err)
	}
	if called {
		t.Fatal("PutSettings() touched the database for invalid input")
	}
}

func TestCenterSettingsRepositorySQLUsesSingletonUpsertAndJSONBSections(t *testing.T) {
	t.Parallel()

	if !strings.Contains(upsertCenterSettingsSQL, "insert into center_settings") {
		t.Fatalf("upsertCenterSettingsSQL = %q, want center_settings insert", upsertCenterSettingsSQL)
	}
	if !strings.Contains(upsertCenterSettingsSQL, "on conflict (settings_id) do update") {
		t.Fatalf("upsertCenterSettingsSQL = %q, want singleton upsert", upsertCenterSettingsSQL)
	}
	if !strings.Contains(upsertCenterSettingsSQL, "telegram_runtime_managed") {
		t.Fatalf("upsertCenterSettingsSQL = %q, want telegram_runtime_managed column", upsertCenterSettingsSQL)
	}
	if !strings.Contains(upsertCenterSettingsSQL, "feishu_enabled") {
		t.Fatalf("upsertCenterSettingsSQL = %q, want feishu_enabled column", upsertCenterSettingsSQL)
	}
	if !strings.Contains(upsertCenterSettingsSQL, "$8::jsonb") {
		t.Fatalf("upsertCenterSettingsSQL = %q, want jsonb cast for probe defaults", upsertCenterSettingsSQL)
	}
	if !strings.Contains(getCenterSettingsSQL, "where settings_id = $1") {
		t.Fatalf("getCenterSettingsSQL = %q, want singleton filter", getCenterSettingsSQL)
	}
}

type fakeSettingsQueryer struct {
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeSettingsQueryer) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRow(ctx, sql, args...)
}

type fakeSettingsRow struct {
	scan func(dest ...any) error
}

func (f fakeSettingsRow) Scan(dest ...any) error { return f.scan(dest...) }

func scanCenterSettingsRow(dest []any, value centersettings.CenterSettings) {
	probeJSON, _ := json.Marshal(value.ProbeFrequencyDefaults)
	incidentJSON, _ := json.Marshal(value.IncidentDefaults)
	overrideJSON, _ := json.Marshal(value.OverrideRules)
	retentionJSON, _ := json.Marshal(value.RetentionPolicy)

	*(dest[0].(*string)) = centersettings.SingletonID
	*(dest[1].(*string)) = value.Telegram.BotToken
	*(dest[2].(*string)) = value.Telegram.ChatID
	*(dest[3].(*bool)) = value.Telegram.RuntimeManaged
	*(dest[4].(*bool)) = value.FeishuEnabled
	*(dest[5].(*string)) = value.FeishuWebhookURL
	*(dest[6].(*string)) = value.HostSampleFrequencyTier
	*(dest[7].(*[]byte)) = probeJSON
	*(dest[8].(*[]byte)) = incidentJSON
	*(dest[9].(*[]byte)) = overrideJSON
	*(dest[10].(*[]byte)) = retentionJSON
	createdAt := time.Date(2026, time.April, 26, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	*(dest[11].(*time.Time)) = createdAt
	*(dest[12].(*time.Time)) = updatedAt
}

func assertJSONArgContains(t *testing.T, value any, snippet string) {
	t.Helper()

	body, ok := value.([]byte)
	if !ok {
		t.Fatalf("value = %#v, want []byte", value)
	}
	if !strings.Contains(string(body), snippet) {
		t.Fatalf("json = %s, want snippet %q", body, snippet)
	}
}

func settingsStringPtr(value string) *string { return &value }
