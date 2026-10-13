package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	centersettings "houfeng/internal/center/settings"
)

func TestPostgresIntegrationEffectiveSettingsRuntimeRole(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "effective-settings-runtime", 2)
	deployment := centersettings.DeploymentDefaults{
		Telegram: centersettings.TelegramSettings{
			BotToken: "555555:runtime-token",
			ChatID:   "runtime-chat",
		},
		IncidentSweepIntervalSeconds: 30,
		IncidentSweepSource:          centersettings.ConfigSourceDeployment,
	}
	repo := NewPostgresSettingsRepository(runtimePool, deployment)

	if _, err := repo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		current.IncidentSweepRuntimeManaged = true
		current.IncidentDefaults.SweepIntervalSeconds = 17
		return current, nil
	}); err != nil {
		t.Fatalf("runtime role settings ownership write: %v", err)
	}

	settings, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("runtime role settings read: %v", err)
	}
	if !settings.IncidentSweepRuntimeManaged || settings.IncidentDefaults.SweepIntervalSeconds != 17 {
		t.Fatalf("runtime role effective settings = %#v, want managed sweep 17", settings)
	}

	var managed bool
	if err := runtimePool.QueryRow(ctx, `
		select incident_sweep_runtime_managed
		from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(&managed); err != nil {
		t.Fatalf("runtime role ownership-column read: %v", err)
	}
	if !managed {
		t.Fatal("runtime role ownership-column write did not persist")
	}

	if _, err := runtimePool.Exec(ctx, `create table public.effective_settings_runtime_ddl_probe (id integer primary key)`); err == nil {
		t.Fatal("runtime role unexpectedly performed DDL")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("runtime DDL error = %v, want PostgreSQL insufficient_privilege", err)
		}
	}
}
