package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	centersettings "houfeng/internal/center/settings"
)

func TestCenterSettingsRepositoryGetSettingsReturnsDefaultsWithoutCreatingSingletonWhenMissing(t *testing.T) {
	t.Parallel()

	queryCount := 0
	repo := &PostgresSettingsRepository{db: fakeSettingsQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			queryCount++
			if queryCount == 1 {
				return fakeSettingsRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			}
			return fakeSettingsRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
		},
	}}

	got, err := repo.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	want := centersettings.ResolveEffectiveSettings(centersettings.FreshInstallDefaults(), centersettings.DeploymentDefaults{})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetSettings() = %#v, want %#v", got, want)
	}
	if queryCount != 1 {
		t.Fatalf("queryCount = %d, want 1", queryCount)
	}
}

func TestCenterSettingsRepositoryMutationKeepsDeploymentDefaultsOutOfRawCallback(t *testing.T) {
	t.Parallel()

	current := centersettings.Default()
	current.Effective.Persisted = true
	updated := current

	tx := &fakeSettingsTx{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeSettingsRow{scan: func(dest ...any) error {
				scanCenterSettingsRow(dest, current)
				return nil
			}}
		},
	}
	repo := &PostgresSettingsRepository{
		db: fakeSettingsQueryer{},
		beginTx: func(context.Context, pgx.TxOptions) (settingsTx, error) {
			return tx, nil
		},
		deployment: centersettings.DeploymentDefaults{
			Telegram:                     centersettings.TelegramSettings{BotToken: "deployment-token", ChatID: "deployment-chat"},
			IncidentSweepIntervalSeconds: 90,
			IncidentSweepSource:          centersettings.ConfigSourceDeployment,
		},
	}

	got, err := repo.MutateSettings(context.Background(), func(input centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		if !reflect.DeepEqual(input, current) {
			t.Fatalf("callback current = %#v, want %#v", input, current)
		}
		return updated, nil
	})
	if err != nil {
		t.Fatalf("MutateSettings() error = %v", err)
	}
	if got.Telegram.BotToken != "deployment-token" || got.Telegram.ChatID != "deployment-chat" ||
		got.Telegram.RuntimeManaged || got.IncidentSweepRuntimeManaged ||
		got.IncidentDefaults.SweepIntervalSeconds != 90 || !got.Effective.Persisted ||
		got.Effective.TelegramSource != centersettings.ConfigSourceDeployment ||
		got.Effective.IncidentSweepSource != centersettings.ConfigSourceDeployment {
		t.Fatal("mutation response did not resolve the inherited deployment view")
	}
}

func TestCenterSettingsRepositoryMutateSettingsRejectsNilCallbackBeforeTransaction(t *testing.T) {
	t.Parallel()

	repo := &PostgresSettingsRepository{}
	_, err := repo.MutateSettings(context.Background(), nil)
	if !errors.Is(err, centersettings.ErrInvalidSettings) {
		t.Fatalf("MutateSettings(nil) error = %v, want ErrInvalidSettings", err)
	}
}

func TestCenterSettingsRepositoryMutateSettingsRollsBackCallbackAndValidationFailures(t *testing.T) {
	t.Parallel()
	callbackErr := errors.New("callback failed")

	for _, test := range []struct {
		name    string
		mutate  centersettings.MutateSettingsFunc
		wantErr error
	}{
		{
			name: "callback error",
			mutate: func(centersettings.CenterSettings) (centersettings.CenterSettings, error) {
				return centersettings.CenterSettings{}, callbackErr
			},
			wantErr: callbackErr,
		},
		{
			name: "validation error",
			mutate: func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
				current.HostSampleFrequencyTier = "invalid-tier"
				return current, nil
			},
			wantErr: centersettings.ErrInvalidSettings,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := newSettingsMutationTestTx(centersettings.Default())
			repo := newSettingsMutationTestRepository(tx)
			_, err := repo.MutateSettings(context.Background(), test.mutate)
			if err == nil || !errors.Is(err, test.wantErr) {
				t.Fatalf("MutateSettings() error = %v, want %v", err, test.wantErr)
			}
			if tx.committed {
				t.Fatal("failed mutation committed")
			}
			if tx.rollbackCalls != 1 {
				t.Fatalf("rollbackCalls = %d, want 1", tx.rollbackCalls)
			}
		})
	}
}

func TestCenterSettingsRepositoryMutateSettingsRollsBackSQLFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("forced update failure")
	tx := newSettingsMutationTestTx(centersettings.Default())
	execCalls := 0
	tx.exec = func(context.Context) (pgconn.CommandTag, error) {
		execCalls++
		if execCalls == 2 {
			return pgconn.CommandTag{}, wantErr
		}
		return pgconn.CommandTag{}, nil
	}
	repo := newSettingsMutationTestRepository(tx)
	_, err := repo.MutateSettings(context.Background(), func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		current.HostSampleFrequencyTier = "1m"
		return current, nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("MutateSettings() error = %v, want %v", err, wantErr)
	}
	if tx.committed || tx.rollbackCalls != 1 {
		t.Fatalf("transaction committed=%t rollbackCalls=%d, want rollback only", tx.committed, tx.rollbackCalls)
	}
}

func TestCenterSettingsRepositoryMutateSettingsRollsBackCommitFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("forced commit failure")
	tx := newSettingsMutationTestTx(centersettings.Default())
	tx.commitErr = wantErr
	repo := newSettingsMutationTestRepository(tx)
	_, err := repo.MutateSettings(context.Background(), func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		current.HostSampleFrequencyTier = "1m"
		return current, nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("MutateSettings() error = %v, want %v", err, wantErr)
	}
	if tx.committed || tx.rollbackCalls != 1 {
		t.Fatalf("transaction committed=%t rollbackCalls=%d, want rollback only", tx.committed, tx.rollbackCalls)
	}
}
func TestCenterSettingsRepositoryGetSettingsReadsIPQualityEnabledLikeSyncPlan(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		raw  string
		want bool
	}{
		"missing enabled defaults on": {raw: `{"frequency_seconds":86400,"timeout_seconds":15,"stale_after_seconds":604800,"services":["netflix"]}`, want: true},
		"explicit disabled stays off": {raw: `{"enabled":false,"frequency_seconds":86400,"timeout_seconds":15,"stale_after_seconds":604800,"services":["netflix"]}`, want: false},
		"empty object defaults on":    {raw: `{}`, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &PostgresSettingsRepository{db: fakeSettingsQueryer{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeSettingsRow{scan: func(dest ...any) error {
						scanCenterSettingsRow(dest, centersettings.Default())
						*(dest[13].(*[]byte)) = []byte(test.raw)
						return nil
					}}
				},
			}}
			got, err := repo.GetSettings(context.Background())
			if err != nil {
				t.Fatalf("GetSettings() error = %v", err)
			}
			if got.IPQuality.Enabled != test.want {
				t.Fatalf("IPQuality.Enabled = %v, want %v", got.IPQuality.Enabled, test.want)
			}
			if got.IPQuality.FrequencySeconds != 86400 || len(got.IPQuality.Services) == 0 {
				t.Fatalf("IPQuality = %#v, want defaults filled in", got.IPQuality)
			}
		})
	}
}

func TestCenterSettingsRepositoryIPQualityDefaultsPreservePersistedValues(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		raw         string
		wantTimeout int
		wantMail    bool
		wantEnabled bool
		wantSources centersettings.IPQualitySources
	}{
		"missing block": {wantTimeout: 60, wantMail: true, wantEnabled: true},
		"legacy explicit timeout": {
			raw:         `{"enabled":true,"frequency_seconds":86400,"timeout_seconds":15,"stale_after_seconds":604800,"services":["netflix"]}`,
			wantTimeout: 15, wantMail: true, wantEnabled: true,
		},
		"legacy explicitly disabled": {
			raw:         `{"enabled":false,"timeout_seconds":20}`,
			wantTimeout: 20, wantMail: true,
		},
		"mail explicitly disabled": {
			raw:         `{"mail_enabled":false,"timeout_seconds":25}`,
			wantTimeout: 25, wantEnabled: true,
		},
		"credentials persisted": {
			raw:         `{"mail_enabled":true,"sources":{"ipapi_is_key":"private-ipapi","abuseipdb_key":"private-abuse"}}`,
			wantTimeout: 60, wantMail: true, wantEnabled: true,
			wantSources: centersettings.IPQualitySources{IPAPIISKey: "private-ipapi", AbuseIPDBKey: "private-abuse"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &PostgresSettingsRepository{db: fakeSettingsQueryer{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeSettingsRow{scan: func(dest ...any) error {
						scanCenterSettingsRow(dest, centersettings.Default())
						*(dest[13].(*[]byte)) = []byte(test.raw)
						return nil
					}}
				},
			}}
			got, err := repo.GetSettings(context.Background())
			if err != nil {
				t.Fatalf("GetSettings() error = %v", err)
			}
			if got.IPQuality.TimeoutSeconds != test.wantTimeout || got.IPQuality.MailEnabled != test.wantMail || got.IPQuality.Enabled != test.wantEnabled {
				t.Fatalf("timeout/mail/enabled = %d/%v/%v, want %d/%v/%v", got.IPQuality.TimeoutSeconds, got.IPQuality.MailEnabled, got.IPQuality.Enabled, test.wantTimeout, test.wantMail, test.wantEnabled)
			}
			if got.IPQuality.Sources != test.wantSources {
				t.Fatal("persisted credentials were lost or changed")
			}
		})
	}
}
func TestCenterSettingsRepositoryGetSettingsPreservesExplicitFalseOverride(t *testing.T) {
	t.Parallel()

	persisted := centersettings.Default()
	persisted.OverrideRules = centersettings.OverrideRules{
		MonitoringInstanceLabels: []centersettings.MonitoringInstanceLabelOverrideRule{{
			Label: "core",
			Overrides: centersettings.SettingsOverrideFields{
				IncidentDefaults: &centersettings.IncidentDefaultsOverride{
					NotifyOnStarted: new(false),
				},
			},
		}},
	}
	repo := &PostgresSettingsRepository{db: fakeSettingsQueryer{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeSettingsRow{scan: func(dest ...any) error {
				scanCenterSettingsRow(dest, persisted)
				return nil
			}}
		},
	}}

	got, err := repo.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	override := got.OverrideRules.MonitoringInstanceLabels[0].Overrides.IncidentDefaults
	if override == nil || override.NotifyOnStarted == nil || *override.NotifyOnStarted {
		t.Fatalf("GetSettings() notify_on_started = %#v, want explicit false pointer", override)
	}
}
func TestLoadNotificationSettingsUsesDeploymentWhenSingletonMissing(t *testing.T) {
	t.Parallel()

	deployment := centersettings.DeploymentDefaults{
		Telegram: centersettings.TelegramSettings{BotToken: "deployment-token", ChatID: "deployment-chat"},
	}
	repo := &PostgresSettingsRepository{
		db: fakeSettingsQueryer{queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeSettingsRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		}},
		deployment: deployment,
	}
	got, err := repo.GetNotificationSettings(context.Background())
	if err != nil {
		t.Fatalf("GetNotificationSettings() error = %v", err)
	}
	if got.Telegram.BotToken != "deployment-token" || got.Telegram.ChatID != "deployment-chat" || !got.Telegram.Enabled() {
		t.Fatalf("notification settings = %#v, want deployment Telegram", got)
	}
}

func TestLoadNotificationSettingsRejectsHalfConfiguredManagedTelegram(t *testing.T) {
	t.Parallel()

	repo := &PostgresSettingsRepository{
		db: fakeSettingsQueryer{queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeSettingsRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "stored-token"
				*(dest[1].(*string)) = ""
				*(dest[2].(*bool)) = true
				*(dest[3].(*bool)) = false
				*(dest[4].(*string)) = ""
				return nil
			}}
		}},
		deployment: centersettings.DeploymentDefaults{
			Telegram: centersettings.TelegramSettings{BotToken: "deployment-token", ChatID: "deployment-chat"},
		},
	}
	if _, err := repo.GetNotificationSettings(context.Background()); err == nil {
		t.Fatal("GetNotificationSettings() error = nil, want half-configured managed Telegram failure")
	}
}

func TestLoadNotificationSettingsHonorsManagedDisableWithoutDeploymentFallback(t *testing.T) {
	t.Parallel()

	deployment := centersettings.DeploymentDefaults{
		Telegram: centersettings.TelegramSettings{BotToken: "deployment-token", ChatID: "deployment-chat"},
	}
	row := func(botToken, chatID string, managed bool) pgx.Row {
		return fakeSettingsRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = botToken
			*(dest[1].(*string)) = chatID
			*(dest[2].(*bool)) = managed
			*(dest[3].(*bool)) = true
			*(dest[4].(*string)) = "  https://feishu.example/hook  "
			return nil
		}}
	}

	disabledRepo := &PostgresSettingsRepository{
		db:         fakeSettingsQueryer{queryRow: func(context.Context, string, ...any) pgx.Row { return row("", "", true) }},
		deployment: deployment,
	}
	disabled, err := disabledRepo.GetNotificationSettings(context.Background())
	if err != nil {
		t.Fatalf("disabled GetNotificationSettings() error = %v", err)
	}
	if disabled.Telegram.Enabled() || !disabled.Telegram.RuntimeManaged {
		t.Fatalf("disabled Telegram = %#v, want managed disabled", disabled.Telegram)
	}
	if !disabled.FeishuEnabled || disabled.FeishuWebhookURL != "https://feishu.example/hook" {
		t.Fatalf("Feishu settings = %#v, want trimmed enabled webhook", disabled)
	}

	inheritedRepo := &PostgresSettingsRepository{
		db:         fakeSettingsQueryer{queryRow: func(context.Context, string, ...any) pgx.Row { return row("legacy-token", "", false) }},
		deployment: deployment,
	}
	inherited, err := inheritedRepo.GetNotificationSettings(context.Background())
	if err != nil {
		t.Fatalf("inherited GetNotificationSettings() error = %v", err)
	}
	if inherited.Telegram.BotToken != deployment.Telegram.BotToken ||
		inherited.Telegram.ChatID != deployment.Telegram.ChatID ||
		inherited.Telegram.RuntimeManaged {
		t.Fatalf("inherited Telegram = %#v, want deployment fallback", inherited.Telegram)
	}
}

func TestLoadNotificationSettingsReturnsQueryErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database unavailable")
	repo := &PostgresSettingsRepository{
		db: fakeSettingsQueryer{queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeSettingsRow{scan: func(dest ...any) error { return wantErr }}
		}},
	}
	if _, err := repo.GetNotificationSettings(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("GetNotificationSettings() error = %v, want %v", err, wantErr)
	}
}
func TestGetEffectiveIncidentDefaultsResolvesDeploymentSweepOwnership(t *testing.T) {
	t.Parallel()

	deployment := centersettings.DeploymentDefaults{
		IncidentSweepIntervalSeconds: 90,
		IncidentSweepSource:          centersettings.ConfigSourceDeployment,
	}
	makeRepo := func(scan func(...any) error) *PostgresSettingsRepository {
		return &PostgresSettingsRepository{
			db:         fakeSettingsQueryer{queryRow: func(context.Context, string, ...any) pgx.Row { return fakeSettingsRow{scan: scan} }},
			deployment: deployment,
		}
	}
	raw, err := json.Marshal(centersettings.Default().IncidentDefaults)
	if err != nil {
		t.Fatal(err)
	}

	inheritedRepo := makeRepo(func(dest ...any) error {
		*(dest[0].(*[]byte)) = raw
		*(dest[1].(*bool)) = false
		return nil
	})
	inherited, exists, err := inheritedRepo.GetEffectiveIncidentDefaults(context.Background())
	if err != nil || !exists {
		t.Fatalf("inherited GetEffectiveIncidentDefaults() = (%#v,%t,%v)", inherited, exists, err)
	}
	if inherited.SweepIntervalSeconds != 90 {
		t.Fatalf("inherited sweep = %d, want 90", inherited.SweepIntervalSeconds)
	}

	overriddenRepo := makeRepo(func(dest ...any) error {
		*(dest[0].(*[]byte)) = raw
		*(dest[1].(*bool)) = true
		return nil
	})
	overridden, exists, err := overriddenRepo.GetEffectiveIncidentDefaults(context.Background())
	if err != nil || !exists {
		t.Fatalf("overridden GetEffectiveIncidentDefaults() = (%#v,%t,%v)", overridden, exists, err)
	}
	if overridden.SweepIntervalSeconds != 5 {
		t.Fatalf("overridden sweep = %d, want persisted 5", overridden.SweepIntervalSeconds)
	}
}

type fakeSettingsQueryer struct {
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeSettingsQueryer) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRow(ctx, sql, args...)
}

type fakeSettingsTx struct {
	queryRow      func(context.Context, string, ...any) pgx.Row
	exec          func(context.Context) (pgconn.CommandTag, error)
	committed     bool
	rollbackCalls int
	commitErr     error
}

func (f *fakeSettingsTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRow(ctx, sql, args...)
}

func (f *fakeSettingsTx) Exec(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	if f.exec != nil {
		return f.exec(ctx)
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeSettingsTx) Commit(context.Context) error {
	if f.commitErr != nil {
		return f.commitErr
	}
	f.committed = true
	return nil
}

func (f *fakeSettingsTx) Rollback(context.Context) error {
	f.rollbackCalls++
	return nil
}

func newSettingsMutationTestTx(current centersettings.CenterSettings) *fakeSettingsTx {
	return &fakeSettingsTx{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeSettingsRow{scan: func(dest ...any) error {
				scanCenterSettingsRow(dest, current)
				return nil
			}}
		},
	}
}

func newSettingsMutationTestRepository(tx *fakeSettingsTx) *PostgresSettingsRepository {
	return &PostgresSettingsRepository{
		db:      fakeSettingsQueryer{},
		beginTx: func(context.Context, pgx.TxOptions) (settingsTx, error) { return tx, nil },
	}
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
	subscriptionCostJSON, _ := json.Marshal(value.SubscriptionCost)
	ipQualityJSON, _ := json.Marshal(value.IPQuality)

	*(dest[0].(*string)) = centersettings.SingletonID
	*(dest[1].(*string)) = value.Telegram.BotToken
	*(dest[2].(*string)) = value.Telegram.ChatID
	*(dest[3].(*bool)) = value.Telegram.RuntimeManaged
	*(dest[4].(*bool)) = value.IncidentSweepRuntimeManaged
	*(dest[5].(*bool)) = value.FeishuEnabled
	*(dest[6].(*string)) = value.FeishuWebhookURL
	*(dest[7].(*string)) = value.HostSampleFrequencyTier
	*(dest[8].(*[]byte)) = probeJSON
	*(dest[9].(*[]byte)) = incidentJSON
	*(dest[10].(*[]byte)) = overrideJSON
	*(dest[11].(*[]byte)) = retentionJSON
	*(dest[12].(*[]byte)) = subscriptionCostJSON
	*(dest[13].(*[]byte)) = ipQualityJSON
	createdAt := time.Date(2026, time.April, 26, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	*(dest[14].(*time.Time)) = createdAt
	*(dest[15].(*time.Time)) = updatedAt
}
