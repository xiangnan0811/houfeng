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
	if !reflect.DeepEqual(got, centersettings.Default()) {
		t.Fatalf("GetSettings() = %#v, want %#v", got, centersettings.Default())
	}
	if queryCount != 1 {
		t.Fatalf("queryCount = %d, want 1", queryCount)
	}
}

func TestCenterSettingsRepositoryMutateSettingsCallsCallbackOnce(t *testing.T) {
	t.Parallel()

	current := centersettings.Default()
	updated := current
	updated.HostSampleFrequencyTier = "1m"

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
	}
	callbackCalls := 0

	got, err := repo.MutateSettings(context.Background(), func(input centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		callbackCalls++
		if !reflect.DeepEqual(input, current) {
			t.Fatalf("callback current = %#v, want %#v", input, current)
		}
		return updated, nil
	})
	if err != nil {
		t.Fatalf("MutateSettings() error = %v", err)
	}
	if callbackCalls != 1 {
		t.Fatalf("callback calls = %d, want 1", callbackCalls)
	}
	if !reflect.DeepEqual(got, updated) {
		t.Fatalf("MutateSettings() = %#v, want %#v", got, updated)
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
						*(dest[12].(*[]byte)) = []byte(test.raw)
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
	*(dest[4].(*bool)) = value.FeishuEnabled
	*(dest[5].(*string)) = value.FeishuWebhookURL
	*(dest[6].(*string)) = value.HostSampleFrequencyTier
	*(dest[7].(*[]byte)) = probeJSON
	*(dest[8].(*[]byte)) = incidentJSON
	*(dest[9].(*[]byte)) = overrideJSON
	*(dest[10].(*[]byte)) = retentionJSON
	*(dest[11].(*[]byte)) = subscriptionCostJSON
	*(dest[12].(*[]byte)) = ipQualityJSON
	createdAt := time.Date(2026, time.April, 26, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	*(dest[13].(*time.Time)) = createdAt
	*(dest[14].(*time.Time)) = updatedAt
}
