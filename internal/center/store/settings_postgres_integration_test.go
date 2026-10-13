package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/http/handlers"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/subscriptioncosts"
	"houfeng/internal/center/targets"
)

func TestPostgresIntegrationSettingsMutationSerializesConcurrentInitializersAndPartitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openSettingsPostgresIntegrationSchema(t, ctx)
	repo := store.NewPostgresSettingsRepository(pool, centersettings.DeploymentDefaults{
		IncidentSweepIntervalSeconds: 5,
		IncidentSweepSource:          centersettings.ConfigSourceDefault,
	})

	t.Run("GET remains read only when singleton is missing", func(t *testing.T) {
		resetCenterSettings(t, ctx, pool)
		if _, err := repo.GetSettings(ctx); err != nil {
			t.Fatalf("GetSettings() error = %v", err)
		}
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(&count); err != nil {
			t.Fatalf("count center settings: %v", err)
		}
		if count != 0 {
			t.Fatalf("center settings rows = %d, want zero after read-only GET", count)
		}
	})

	t.Run("deployment defaults stay raw until explicitly managed", func(t *testing.T) {
		resetCenterSettings(t, ctx, pool)
		deploymentA := centersettings.DeploymentDefaults{
			Telegram:                     centersettings.TelegramSettings{BotToken: "deployment-token-a", ChatID: "deployment-chat-a"},
			IncidentSweepIntervalSeconds: 60,
			IncidentSweepSource:          centersettings.ConfigSourceDeployment,
		}
		repoA := store.NewPostgresSettingsRepository(pool, deploymentA)
		created, err := repoA.MutateSettings(ctx, func(centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			return centersettings.Default(), nil
		})
		if err != nil {
			t.Fatalf("seed legacy settings row: %v", err)
		}
		if !created.Effective.Persisted {
			t.Fatalf("seed mutation effective metadata = %#v, want persisted=true", created.Effective)
		}
		legacy, err := repoA.GetSettings(ctx)
		if err != nil {
			t.Fatalf("read legacy settings row: %v", err)
		}
		if len(legacy.OverrideRules.MonitoringInstanceLabels) != 0 {
			t.Fatalf("legacy monitoring-instance overrides = %#v, want no fresh-install core rule", legacy.OverrideRules.MonitoringInstanceLabels)
		}
		resetCenterSettings(t, ctx, pool)

		fresh, err := repoA.GetSettings(ctx)
		if err != nil {
			t.Fatalf("fresh GetSettings() error = %v", err)
		}
		if fresh.Telegram.BotToken != deploymentA.Telegram.BotToken ||
			fresh.IncidentDefaults.SweepIntervalSeconds != 60 {
			t.Fatalf("fresh effective settings = %#v, want deployment Telegram/sweep", fresh)
		}
		notification, err := repoA.GetNotificationSettings(ctx)
		if err != nil {
			t.Fatalf("fresh GetNotificationSettings() error = %v", err)
		}
		if notification.Telegram.BotToken != deploymentA.Telegram.BotToken ||
			notification.Telegram.ChatID != deploymentA.Telegram.ChatID {
			t.Fatalf("fresh notification settings = %#v, want deployment Telegram", notification)
		}
		assertCenterSettingsRowCount(t, ctx, pool, 0, "after fresh effective reads")

		var callbackSawRaw bool
		mutated, err := repoA.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			callbackSawRaw = current.Telegram.BotToken == "" &&
				current.Telegram.ChatID == "" &&
				!current.Telegram.RuntimeManaged &&
				!current.IncidentSweepRuntimeManaged &&
				current.IncidentDefaults.SweepIntervalSeconds == 5
			current.SubscriptionCost.BaseCurrency = "USD"
			return current, nil
		})
		if err != nil {
			t.Fatalf("subscription-only mutation error = %v", err)
		}
		if !callbackSawRaw {
			t.Fatal("mutation callback received effective deployment values instead of raw settings")
		}
		if mutated.Telegram.BotToken != deploymentA.Telegram.BotToken ||
			mutated.IncidentDefaults.SweepIntervalSeconds != 60 {
			t.Fatalf("subscription-only effective result = %#v, want deployment Telegram/sweep", mutated)
		}
		var rawToken, rawChat string
		var rawTelegramManaged, rawSweepManaged bool
		var rawIncident []byte
		if err := pool.QueryRow(ctx, `
			select telegram_bot_token, telegram_chat_id, telegram_runtime_managed,
				incident_sweep_runtime_managed, incident_defaults
			from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(
			&rawToken, &rawChat, &rawTelegramManaged, &rawSweepManaged, &rawIncident,
		); err != nil {
			t.Fatalf("read raw settings after subscription-only mutation: %v", err)
		}
		var rawDefaults centersettings.IncidentDefaults
		if err := json.Unmarshal(rawIncident, &rawDefaults); err != nil {
			t.Fatalf("decode raw incident defaults: %v", err)
		}
		if rawToken != "" || rawChat != "" || rawTelegramManaged || rawSweepManaged ||
			rawDefaults.SweepIntervalSeconds != 5 {
			t.Fatalf("raw settings after subscription-only mutation = token=%q chat=%q telegram_managed=%t sweep_managed=%t sweep=%d; want untouched deployment ownership",
				rawToken, rawChat, rawTelegramManaged, rawSweepManaged, rawDefaults.SweepIntervalSeconds)
		}

		if _, err := pool.Exec(ctx, `
			update center_settings
			set subscription_cost_settings = '{"unknown":true}'::jsonb
			where settings_id = $1`, centersettings.SingletonID); err != nil {
			t.Fatalf("corrupt unrelated subscription JSON: %v", err)
		}
		if _, err := repoA.GetSettings(ctx); err == nil {
			t.Fatal("GetSettings() error = nil for unrelated corrupt settings JSON")
		}
		if _, err := repoA.GetNotificationSettings(ctx); err != nil {
			t.Fatalf("GetNotificationSettings() should ignore unrelated JSON corruption: %v", err)
		}
		if _, _, err := repoA.GetEffectiveIncidentDefaults(ctx); err != nil {
			t.Fatalf("GetEffectiveIncidentDefaults() should ignore unrelated JSON corruption: %v", err)
		}
		subscriptionJSON, err := json.Marshal(centersettings.Default().SubscriptionCost)
		if err != nil {
			t.Fatalf("marshal subscription defaults: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			update center_settings
			set subscription_cost_settings = $1::jsonb
			where settings_id = $2`, string(subscriptionJSON), centersettings.SingletonID); err != nil {
			t.Fatalf("restore subscription JSON: %v", err)
		}

		deploymentB := centersettings.DeploymentDefaults{
			Telegram:                     centersettings.TelegramSettings{BotToken: "deployment-token-b", ChatID: "deployment-chat-b"},
			IncidentSweepIntervalSeconds: 90,
			IncidentSweepSource:          centersettings.ConfigSourceDeployment,
		}
		repoB := store.NewPostgresSettingsRepository(pool, deploymentB)
		afterRestart, err := repoB.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after deployment change = %v", err)
		}
		if afterRestart.Telegram.BotToken != deploymentB.Telegram.BotToken ||
			afterRestart.IncidentDefaults.SweepIntervalSeconds != 90 {
			t.Fatalf("after deployment change = %#v, want deployment B values", afterRestart)
		}

		overridden, err := repoB.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			current.Telegram = centersettings.TelegramSettings{
				BotToken:       "stored-token",
				ChatID:         "stored-chat",
				RuntimeManaged: true,
			}
			current.IncidentSweepRuntimeManaged = true
			current.IncidentDefaults.SweepIntervalSeconds = 17
			return current, nil
		})
		if err != nil {
			t.Fatalf("override mutation error = %v", err)
		}
		if overridden.Telegram.BotToken != "stored-token" || overridden.IncidentDefaults.SweepIntervalSeconds != 17 {
			t.Fatalf("override effective result = %#v, want database values", overridden)
		}
		repoC := store.NewPostgresSettingsRepository(pool, centersettings.DeploymentDefaults{
			Telegram:                     centersettings.TelegramSettings{BotToken: "deployment-token-c", ChatID: "deployment-chat-c"},
			IncidentSweepIntervalSeconds: 120,
			IncidentSweepSource:          centersettings.ConfigSourceDeployment,
		})
		afterOverrideRestart, err := repoC.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after override restart = %v", err)
		}
		if afterOverrideRestart.Telegram.BotToken != "stored-token" ||
			afterOverrideRestart.IncidentDefaults.SweepIntervalSeconds != 17 {
			t.Fatalf("after override restart = %#v, want persisted values", afterOverrideRestart)
		}

		disabled, err := repoC.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			current.Telegram = centersettings.TelegramSettings{RuntimeManaged: true}
			return current, nil
		})
		if err != nil {
			t.Fatalf("disable mutation error = %v", err)
		}
		if disabled.Telegram.Enabled() {
			t.Fatalf("disabled effective Telegram = %#v, want disabled despite deployment", disabled.Telegram)
		}

		restored, err := repoC.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			current.Telegram = centersettings.TelegramSettings{}
			current.IncidentSweepRuntimeManaged = false
			current.IncidentDefaults.SweepIntervalSeconds = centersettings.Default().IncidentDefaults.SweepIntervalSeconds
			return current, nil
		})
		if err != nil {
			t.Fatalf("restore deployment mutation error = %v", err)
		}
		if restored.Telegram.BotToken != "deployment-token-c" ||
			restored.IncidentDefaults.SweepIntervalSeconds != 120 {
			t.Fatalf("restored effective settings = %#v, want deployment C values", restored)
		}
	})

	t.Run("concurrent initializers preserve both mutations", func(t *testing.T) {
		resetCenterSettings(t, ctx, pool)
		firstEntered := make(chan struct{})
		releaseFirst := make(chan struct{})
		results := make(chan error, 2)

		go func() {
			_, err := repo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
				close(firstEntered)
				<-releaseFirst
				current.HostSampleFrequencyTier = targets.FrequencyTier15m
				return current, nil
			})
			results <- err
		}()
		select {
		case <-firstEntered:
		case <-ctx.Done():
			t.Fatalf("first mutation did not enter callback: %v", ctx.Err())
		}

		go func() {
			_, err := repo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
				current.SubscriptionCost.BaseCurrency = "USD"
				return current, nil
			})
			results <- err
		}()
		if err := waitForSettingsBlockedSessions(ctx, pool, 1); err != nil {
			close(releaseFirst)
			t.Fatalf("wait for second initializer lock: %v", err)
		}
		close(releaseFirst)
		for range 2 {
			if err := <-results; err != nil {
				t.Fatalf("concurrent mutation error = %v", err)
			}
		}

		got, err := repo.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after concurrent initialization: %v", err)
		}
		if got.HostSampleFrequencyTier != targets.FrequencyTier15m || got.SubscriptionCost.BaseCurrency != "USD" {
			t.Fatalf("settings = %#v, want both initializer mutations", got)
		}

	})

	for _, first := range []string{"subscription settings first", "global settings first"} {
		t.Run(first, func(t *testing.T) {
			resetCenterSettings(t, ctx, pool)
			initial := centersettings.Default()
			initial.SubscriptionCost.FixerAPIKey = "existing-secret"
			if _, err := repo.MutateSettings(ctx, func(centersettings.CenterSettings) (centersettings.CenterSettings, error) {
				return initial, nil
			}); err != nil {
				t.Fatalf("seed settings: %v", err)
			}

			settingsHandler := handlers.Settings(repo, nil, centersettings.DeploymentDefaults{
				IncidentSweepIntervalSeconds: 5,
				IncidentSweepSource:          centersettings.ConfigSourceDefault,
			})
			subscriptionService := subscriptioncosts.NewService(nil, repo, nil)
			subscriptionHandler := handlers.SubscriptionSettings(subscriptionService, nil)
			globalRequest := func() *http.Request {
				return httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(settingsGlobalPutBody(t, targets.FrequencyTier15m)))
			}
			subscriptionRequest := func() *http.Request {
				return httptest.NewRequest(http.MethodPut, "/api/subscriptions/settings", bytes.NewReader([]byte(`{"base_currency":"USD"}`)))
			}

			holder, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatalf("begin lock holder: %v", err)
			}
			defer func() { _ = holder.Rollback(ctx) }()
			var locked int
			if err := holder.QueryRow(ctx, `select 1 from center_settings where settings_id = $1 for update`, centersettings.SingletonID).Scan(&locked); err != nil {
				t.Fatalf("lock center settings: %v", err)
			}

			responses := make(chan *httptest.ResponseRecorder, 2)
			var startFirst, startSecond func()
			if first == "subscription settings first" {
				startFirst = func() { go serveSettingsRequest(subscriptionHandler, subscriptionRequest, responses) }
				startSecond = func() { go serveSettingsRequest(settingsHandler, globalRequest, responses) }
			} else {
				startFirst = func() { go serveSettingsRequest(settingsHandler, globalRequest, responses) }
				startSecond = func() { go serveSettingsRequest(subscriptionHandler, subscriptionRequest, responses) }
			}
			startFirst()
			if err := waitForSettingsBlockedSessions(ctx, pool, 1); err != nil {
				t.Fatalf("wait for first settings handler lock: %v", err)
			}
			startSecond()
			if err := waitForSettingsBlockedSessions(ctx, pool, 2); err != nil {
				t.Fatalf("wait for second settings handler lock: %v", err)
			}
			if err := holder.Commit(ctx); err != nil {
				t.Fatalf("release settings lock: %v", err)
			}

			for range 2 {
				select {
				case response := <-responses:
					if response.Code != http.StatusOK {
						t.Fatalf("settings handler status = %d body=%s, want 200", response.Code, response.Body.String())
					}
				case <-ctx.Done():
					t.Fatalf("settings handlers did not finish: %v", ctx.Err())
				}
			}
			got, err := repo.GetSettings(ctx)
			if err != nil {
				t.Fatalf("GetSettings() after handler interleave: %v", err)
			}
			if got.HostSampleFrequencyTier != targets.FrequencyTier15m || got.SubscriptionCost.BaseCurrency != "USD" || got.SubscriptionCost.FixerAPIKey != "existing-secret" {
				t.Fatalf("settings = %#v, want latest-value partition merge", got)
			}
		})
	}

	t.Run("concurrent fixer update precedes omitted-key mutation", func(t *testing.T) {
		resetCenterSettings(t, ctx, pool)
		initial := centersettings.Default()
		initial.SubscriptionCost.FixerAPIKey = "old-secret"
		if _, err := repo.MutateSettings(ctx, func(centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			return initial, nil
		}); err != nil {
			t.Fatalf("seed settings: %v", err)
		}

		barrierRepo := &blockingSettingsRepository{
			repo:    repo,
			entered: make(chan struct{}),
			release: make(chan struct{}),
		}
		service := subscriptioncosts.NewService(nil, barrierRepo, nil)
		handler := handlers.SubscriptionSettings(service, nil)
		responses := make(chan *httptest.ResponseRecorder, 2)
		go serveSettingsRequest(handler, func() *http.Request {
			return httptest.NewRequest(http.MethodPut, "/api/subscriptions/settings", bytes.NewReader([]byte(`{"fixer_api_key":"new-secret"}`)))
		}, responses)
		select {
		case <-barrierRepo.entered:
		case <-ctx.Done():
			t.Fatalf("explicit secret mutation did not enter callback: %v", ctx.Err())
		}

		go serveSettingsRequest(handler, func() *http.Request {
			return httptest.NewRequest(http.MethodPut, "/api/subscriptions/settings", bytes.NewReader([]byte(`{"base_currency":"USD"}`)))
		}, responses)
		if err := waitForSettingsBlockedSessions(ctx, pool, 1); err != nil {
			close(barrierRepo.release)
			t.Fatalf("wait for omitted-key mutation lock: %v", err)
		}
		close(barrierRepo.release)

		for range 2 {
			select {
			case response := <-responses:
				if response.Code != http.StatusOK {
					t.Fatalf("subscription settings status = %d body=%s, want 200", response.Code, response.Body.String())
				}
			case <-ctx.Done():
				t.Fatalf("subscription settings handlers did not finish: %v", ctx.Err())
			}
		}
		got, err := repo.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after concurrent secret mutation: %v", err)
		}
		if got.SubscriptionCost.FixerAPIKey != "new-secret" || got.SubscriptionCost.BaseCurrency != "USD" {
			t.Fatalf("subscription cost settings = %#v, want latest secret and currency", got.SubscriptionCost)
		}
	})

	t.Run("explicit fixer key clear redacts response", func(t *testing.T) {
		resetCenterSettings(t, ctx, pool)
		initial := centersettings.Default()
		initial.SubscriptionCost.FixerAPIKey = "clear-secret"
		if _, err := repo.MutateSettings(ctx, func(centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			return initial, nil
		}); err != nil {
			t.Fatalf("seed settings: %v", err)
		}

		service := subscriptioncosts.NewService(nil, repo, nil)
		handler := handlers.SubscriptionSettings(service, nil)
		request := httptest.NewRequest(http.MethodPut, "/api/subscriptions/settings", bytes.NewReader([]byte(`{"fixer_api_key":""}`)))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("subscription settings clear status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "clear-secret") {
			t.Fatalf("subscription settings clear response leaked the previous key: %s", recorder.Body.String())
		}
		var response struct {
			FixerConfigured bool `json:"fixer_configured"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode subscription settings clear response: %v", err)
		}
		if response.FixerConfigured {
			t.Fatalf("subscription settings clear response fixer_configured = true, want false")
		}
		got, err := repo.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after explicit key clear: %v", err)
		}
		if got.SubscriptionCost.FixerAPIKey != "" {
			t.Fatalf("persisted fixer key = %q, want empty after explicit clear", got.SubscriptionCost.FixerAPIKey)
		}
	})

	t.Run("callback validation and SQL failures roll back", func(t *testing.T) {
		resetCenterSettings(t, ctx, pool)
		before := centersettings.Default()
		callbackErr := errors.New("callback failed")
		if _, err := repo.MutateSettings(ctx, func(centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			return centersettings.CenterSettings{}, callbackErr
		}); !errors.Is(err, callbackErr) {
			t.Fatalf("first-insert callback error = %v, want %v", err, callbackErr)
		}
		assertCenterSettingsRowCount(t, ctx, pool, 0, "after first-insert callback rollback")

		if _, err := repo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			current.HostSampleFrequencyTier = "invalid-tier"
			return current, nil
		}); !errors.Is(err, centersettings.ErrInvalidSettings) {
			t.Fatalf("first-insert validation error = %v, want ErrInvalidSettings", err)
		}
		assertCenterSettingsRowCount(t, ctx, pool, 0, "after first-insert validation rollback")

		if _, err := repo.MutateSettings(ctx, func(centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			return before, nil
		}); err != nil {
			t.Fatalf("seed settings: %v", err)
		}

		if _, err := repo.MutateSettings(ctx, func(centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			return centersettings.CenterSettings{}, callbackErr
		}); !errors.Is(err, callbackErr) {
			t.Fatalf("callback error = %v, want %v", err, callbackErr)
		}
		assertCenterSettingsEqual(t, ctx, repo, before, "after callback rollback")

		if _, err := repo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			current.HostSampleFrequencyTier = "invalid-tier"
			return current, nil
		}); !errors.Is(err, centersettings.ErrInvalidSettings) {
			t.Fatalf("validation error = %v, want ErrInvalidSettings", err)
		}
		assertCenterSettingsEqual(t, ctx, repo, before, "after validation rollback")

		if _, err := pool.Exec(ctx, `
create or replace function houfeng_test_reject_center_settings_update()
returns trigger language plpgsql as $$
begin
  raise exception 'forced center settings update failure';
end
$$`); err != nil {
			t.Fatalf("create rollback trigger function: %v", err)
		}
		if _, err := pool.Exec(ctx, `
create trigger houfeng_test_reject_center_settings_update
before update on center_settings
for each row execute function houfeng_test_reject_center_settings_update()`); err != nil {
			t.Fatalf("create rollback trigger: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `drop trigger if exists houfeng_test_reject_center_settings_update on center_settings`)
			_, _ = pool.Exec(context.Background(), `drop function if exists houfeng_test_reject_center_settings_update()`)
		})
		if _, err := repo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			current.HostSampleFrequencyTier = targets.FrequencyTier15m
			return current, nil
		}); err == nil {
			t.Fatal("SQL failure mutation error = nil")
		}
		assertCenterSettingsEqual(t, ctx, repo, before, "after SQL rollback")
		resetCenterSettings(t, ctx, pool)
		if _, err := repo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
			current.HostSampleFrequencyTier = targets.FrequencyTier15m
			return current, nil
		}); err == nil {
			t.Fatal("first-insert SQL failure mutation error = nil")
		}
		assertCenterSettingsRowCount(t, ctx, pool, 0, "after first-insert SQL rollback")
	})
}

func TestPostgresIntegrationEffectiveSettingsDeploymentOverrides(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool := openSettingsPostgresIntegrationSchema(t, ctx)
	deploymentA := centersettings.DeploymentDefaults{
		Telegram: centersettings.TelegramSettings{
			BotToken: "111111:deployment-token-a",
			ChatID:   "deployment-chat-a",
		},
		IncidentSweepIntervalSeconds: 60,
		IncidentSweepSource:          centersettings.ConfigSourceDeployment,
	}
	resetCenterSettings(t, ctx, pool)
	repoA := store.NewPostgresSettingsRepository(pool, deploymentA)
	handlerA := handlers.Settings(repoA, nil, deploymentA)

	t.Run("read-only inheritance and subscription save do not seed deployment values", func(t *testing.T) {
		getRecorder := httptest.NewRecorder()
		handlerA.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
		if getRecorder.Code != http.StatusOK {
			t.Fatalf("missing-row GET status = %d, want 200; body=%s", getRecorder.Code, getRecorder.Body.String())
		}
		var response effectiveSettingsAPIResponse
		if err := json.Unmarshal(getRecorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode missing-row settings response: %v", err)
		}
		if response.Telegram.Mode != "inherit" || response.Telegram.ChatID != deploymentA.Telegram.ChatID ||
			!response.Telegram.Active || response.IncidentDefaults.SweepIntervalSeconds != 60 {
			t.Fatalf("missing-row effective response = %#v, want deployment Telegram/sweep", response)
		}
		assertCenterSettingsRowCount(t, ctx, pool, 0, "after missing-row handler GET")

		putRecorder := servePostgresSettingsPut(handlerA, effectiveSettingsPutBody(t, map[string]any{
			"subscription_cost_settings": map[string]any{"base_currency": "USD"},
		}))
		if putRecorder.Code != http.StatusOK {
			t.Fatalf("subscription-only settings PUT status = %d; body=%s", putRecorder.Code, putRecorder.Body.String())
		}
		var rawToken, rawChat string
		var rawTelegramManaged, rawSweepManaged bool
		var rawIncident []byte
		if err := pool.QueryRow(ctx, `
			select telegram_bot_token, telegram_chat_id, telegram_runtime_managed,
				incident_sweep_runtime_managed, incident_defaults
			from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(
			&rawToken, &rawChat, &rawTelegramManaged, &rawSweepManaged, &rawIncident,
		); err != nil {
			t.Fatalf("read raw settings after subscription-only PUT: %v", err)
		}
		var rawDefaults centersettings.IncidentDefaults
		if err := json.Unmarshal(rawIncident, &rawDefaults); err != nil {
			t.Fatalf("decode raw incident defaults after subscription-only PUT: %v", err)
		}
		if rawToken != "" || rawChat != "" || rawTelegramManaged || rawSweepManaged || rawDefaults.SweepIntervalSeconds != 5 {
			t.Fatalf("subscription-only PUT seeded deployment values: token=%q chat=%q telegram_managed=%t sweep_managed=%t sweep=%d",
				rawToken, rawChat, rawTelegramManaged, rawSweepManaged, rawDefaults.SweepIntervalSeconds)
		}
		if strings.Contains(putRecorder.Body.String(), deploymentA.Telegram.BotToken) {
			t.Fatalf("subscription-only response leaked deployment token: %s", putRecorder.Body.String())
		}
	})

	t.Run("narrow notification and incident reads survive unrelated JSON corruption", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `
			update center_settings
			set subscription_cost_settings = '{"unknown":true}'::jsonb
			where settings_id = $1`, centersettings.SingletonID); err != nil {
			t.Fatalf("corrupt unrelated subscription JSON: %v", err)
		}
		if _, err := repoA.GetNotificationSettings(ctx); err != nil {
			t.Fatalf("GetNotificationSettings() with unrelated JSON corruption: %v", err)
		}
		if _, _, err := repoA.GetEffectiveIncidentDefaults(ctx); err != nil {
			t.Fatalf("GetEffectiveIncidentDefaults() with unrelated JSON corruption: %v", err)
		}
		subscriptionJSON, err := json.Marshal(centersettings.Default().SubscriptionCost)
		if err != nil {
			t.Fatalf("marshal subscription defaults: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			update center_settings set subscription_cost_settings = $1::jsonb
			where settings_id = $2`, string(subscriptionJSON), centersettings.SingletonID); err != nil {
			t.Fatalf("restore subscription JSON: %v", err)
		}
	})

	deploymentB := centersettings.DeploymentDefaults{
		Telegram: centersettings.TelegramSettings{
			BotToken: "222222:deployment-token-b",
			ChatID:   "deployment-chat-b",
		},
		IncidentSweepIntervalSeconds: 90,
		IncidentSweepSource:          centersettings.ConfigSourceDeployment,
	}
	repoB := store.NewPostgresSettingsRepository(pool, deploymentB)
	handlerB := handlers.Settings(repoB, nil, deploymentB)
	t.Run("repository restart uses changed deployment defaults while inherited", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handlerB.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("deployment B GET status = %d; body=%s", recorder.Code, recorder.Body.String())
		}
		var response effectiveSettingsAPIResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode deployment B response: %v", err)
		}
		if response.Telegram.ChatID != deploymentB.Telegram.ChatID || !response.Telegram.Active ||
			response.IncidentDefaults.SweepIntervalSeconds != 90 {
			t.Fatalf("deployment B inherited response = %#v, want B Telegram/sweep", response)
		}
	})

	t.Run("chat-only takeover is lock-local and preserves token under concurrency", func(t *testing.T) {
		barrierRepo := &blockingSettingsRepository{
			repo:    repoB,
			entered: make(chan struct{}),
			release: make(chan struct{}),
		}
		barrierHandler := handlers.Settings(barrierRepo, nil, deploymentB)
		takeoverBody := effectiveSettingsPutBody(t, map[string]any{
			"telegram": map[string]any{"mode": "override", "chat_id": "takeover-chat"},
		})
		responses := make(chan *httptest.ResponseRecorder, 1)
		go serveSettingsRequest(barrierHandler, func() *http.Request {
			return httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(takeoverBody))
		}, responses)
		select {
		case <-barrierRepo.entered:
		case <-ctx.Done():
			t.Fatalf("chat-only takeover did not enter lock-local callback: %v", ctx.Err())
		}

		mutationResult := make(chan error, 1)
		go func() {
			_, err := repoB.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
				current.HostSampleFrequencyTier = targets.FrequencyTier15m
				return current, nil
			})
			mutationResult <- err
		}()
		if err := waitForSettingsBlockedSessions(ctx, pool, 1); err != nil {
			close(barrierRepo.release)
			t.Fatalf("wait for concurrent settings mutation: %v", err)
		}
		close(barrierRepo.release)

		select {
		case recorder := <-responses:
			if recorder.Code != http.StatusOK {
				t.Fatalf("chat-only takeover status = %d; body=%s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), deploymentB.Telegram.BotToken) {
				t.Fatalf("chat-only takeover response leaked token: %s", recorder.Body.String())
			}
		case <-ctx.Done():
			t.Fatalf("chat-only takeover handler did not finish: %v", ctx.Err())
		}
		if err := <-mutationResult; err != nil {
			t.Fatalf("concurrent unrelated settings mutation: %v", err)
		}

		var rawToken, rawChat string
		var rawManaged bool
		if err := pool.QueryRow(ctx, `
			select telegram_bot_token, telegram_chat_id, telegram_runtime_managed
			from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(&rawToken, &rawChat, &rawManaged); err != nil {
			t.Fatalf("read raw Telegram after takeover: %v", err)
		}
		if rawToken != deploymentB.Telegram.BotToken || rawChat != "takeover-chat" || !rawManaged {
			t.Fatalf("raw Telegram after lock-local takeover = token=%q chat=%q managed=%t", rawToken, rawChat, rawManaged)
		}

		recorder := servePostgresSettingsPut(handlerB, effectiveSettingsPutBody(t, map[string]any{
			"telegram": map[string]any{"mode": "override", "chat_id": "takeover-chat-2"},
		}))
		if recorder.Code != http.StatusOK {
			t.Fatalf("omitted-token override status = %d; body=%s", recorder.Code, recorder.Body.String())
		}
		if err := pool.QueryRow(ctx, `
			select telegram_bot_token, telegram_chat_id
			from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(&rawToken, &rawChat); err != nil {
			t.Fatalf("read raw Telegram after omitted-token override: %v", err)
		}
		if rawToken != deploymentB.Telegram.BotToken || rawChat != "takeover-chat-2" {
			t.Fatalf("omitted-token override changed raw credentials: token=%q chat=%q", rawToken, rawChat)
		}
	})

	deploymentC := centersettings.DeploymentDefaults{
		Telegram: centersettings.TelegramSettings{
			BotToken: "333333:deployment-token-c",
			ChatID:   "deployment-chat-c",
		},
		IncidentSweepIntervalSeconds: 120,
		IncidentSweepSource:          centersettings.ConfigSourceDeployment,
	}
	repoC := store.NewPostgresSettingsRepository(pool, deploymentC)
	handlerC := handlers.Settings(repoC, nil, deploymentC)
	t.Run("database override survives restart, disable and restoration are explicit", func(t *testing.T) {
		restarted, err := repoC.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after override restart: %v", err)
		}
		if restarted.Telegram.BotToken != deploymentB.Telegram.BotToken || restarted.Telegram.ChatID != "takeover-chat-2" {
			t.Fatalf("database Telegram override after restart = %#v", restarted.Telegram)
		}

		disabledRecorder := servePostgresSettingsPut(handlerC, effectiveSettingsPutBody(t, map[string]any{
			"telegram": map[string]any{"mode": "disabled"},
		}))
		if disabledRecorder.Code != http.StatusOK {
			t.Fatalf("disabled PUT status = %d; body=%s", disabledRecorder.Code, disabledRecorder.Body.String())
		}
		disabled, err := repoC.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after disabled PUT: %v", err)
		}
		if disabled.Telegram.Enabled() {
			t.Fatalf("disabled effective Telegram = %#v, want disabled despite deployment C", disabled.Telegram)
		}
		notification, err := repoC.GetNotificationSettings(ctx)
		if err != nil {
			t.Fatalf("GetNotificationSettings() after disabled PUT: %v", err)
		}
		if notification.Telegram.Enabled() {
			t.Fatalf("disabled notification Telegram = %#v, want no fallback", notification.Telegram)
		}

		invalidOverride := servePostgresSettingsPut(handlerC, effectiveSettingsPutBody(t, map[string]any{
			"telegram": map[string]any{"mode": "override"},
		}))
		if invalidOverride.Code != http.StatusBadRequest {
			t.Fatalf("override without credentials after disabled status = %d; body=%s", invalidOverride.Code, invalidOverride.Body.String())
		}
		stillDisabled, err := repoC.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after rejected disabled override: %v", err)
		}
		if stillDisabled.Telegram.Enabled() {
			t.Fatalf("rejected disabled override changed effective Telegram = %#v", stillDisabled.Telegram)
		}

		restoredRecorder := servePostgresSettingsPut(handlerC, effectiveSettingsPutBody(t, map[string]any{
			"telegram":            map[string]any{"mode": "inherit"},
			"incident_sweep_mode": "inherit",
		}))
		if restoredRecorder.Code != http.StatusOK {
			t.Fatalf("inherit restoration PUT status = %d; body=%s", restoredRecorder.Code, restoredRecorder.Body.String())
		}
		restored, err := repoC.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after inherit restoration: %v", err)
		}
		if restored.Telegram.BotToken != deploymentC.Telegram.BotToken || restored.Telegram.ChatID != deploymentC.Telegram.ChatID ||
			restored.IncidentDefaults.SweepIntervalSeconds != deploymentC.IncidentSweepIntervalSeconds {
			t.Fatalf("restored inherited effective settings = %#v, want deployment C", restored)
		}
		var rawToken, rawChat string
		var rawTelegramManaged, rawSweepManaged bool
		var rawSweep int
		if err := pool.QueryRow(ctx, `
			select telegram_bot_token, telegram_chat_id, telegram_runtime_managed,
				incident_sweep_runtime_managed, (incident_defaults->>'sweep_interval_seconds')::integer
			from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(
			&rawToken, &rawChat, &rawTelegramManaged, &rawSweepManaged, &rawSweep,
		); err != nil {
			t.Fatalf("read raw settings after inherit restoration: %v", err)
		}
		if rawToken != "" || rawChat != "" || rawTelegramManaged || rawSweepManaged || rawSweep != 5 {
			t.Fatalf("raw settings after inherit restoration = token=%q chat=%q telegram_managed=%t sweep_managed=%t sweep=%d",
				rawToken, rawChat, rawTelegramManaged, rawSweepManaged, rawSweep)
		}
	})

	t.Run("sweep override persists across restart and invalid update is atomic", func(t *testing.T) {
		overrideDefaults := centersettings.FreshInstallDefaults().IncidentDefaults
		overrideDefaults.SweepIntervalSeconds = 17
		overrideRecorder := servePostgresSettingsPut(handlerC, effectiveSettingsPutBody(t, map[string]any{
			"incident_sweep_mode": "override",
			"incident_defaults":   overrideDefaults,
		}))
		if overrideRecorder.Code != http.StatusOK {
			t.Fatalf("sweep override PUT status = %d; body=%s", overrideRecorder.Code, overrideRecorder.Body.String())
		}
		override, err := repoC.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after sweep override: %v", err)
		}
		if override.IncidentDefaults.SweepIntervalSeconds != 17 {
			t.Fatalf("sweep override effective seconds = %d, want 17", override.IncidentDefaults.SweepIntervalSeconds)
		}
		repoD := store.NewPostgresSettingsRepository(pool, centersettings.DeploymentDefaults{
			Telegram:                     centersettings.TelegramSettings{BotToken: "444444:deployment-token-d", ChatID: "deployment-chat-d"},
			IncidentSweepIntervalSeconds: 90,
			IncidentSweepSource:          centersettings.ConfigSourceDeployment,
		})
		restarted, err := repoD.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings() after sweep restart: %v", err)
		}
		if restarted.IncidentDefaults.SweepIntervalSeconds != 17 {
			t.Fatalf("sweep override after restart = %d, want 17", restarted.IncidentDefaults.SweepIntervalSeconds)
		}

		var beforeSweep int
		var beforeManaged bool
		if err := pool.QueryRow(ctx, `
			select incident_sweep_runtime_managed, (incident_defaults->>'sweep_interval_seconds')::integer
			from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(&beforeManaged, &beforeSweep); err != nil {
			t.Fatalf("read sweep before invalid update: %v", err)
		}
		invalidRecorder := servePostgresSettingsPut(handlerC, effectiveSettingsPutBody(t, map[string]any{
			"telegram": map[string]any{"mode": "override", "bot_token": ""},
		}))
		if invalidRecorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid masked/empty credential update status = %d; body=%s", invalidRecorder.Code, invalidRecorder.Body.String())
		}
		var afterSweep int
		var afterManaged bool
		if err := pool.QueryRow(ctx, `
			select incident_sweep_runtime_managed, (incident_defaults->>'sweep_interval_seconds')::integer
			from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(&afterManaged, &afterSweep); err != nil {
			t.Fatalf("read sweep after invalid update: %v", err)
		}
		if beforeManaged != afterManaged || beforeSweep != afterSweep {
			t.Fatalf("invalid update partially changed raw sweep: before=%t/%d after=%t/%d", beforeManaged, beforeSweep, afterManaged, afterSweep)
		}
	})

}

type effectiveSettingsAPIResponse struct {
	Telegram struct {
		Mode       string `json:"mode"`
		Active     bool   `json:"active"`
		ChatID     string `json:"chat_id"`
		Source     string `json:"source"`
		Deployment struct {
			Configured bool `json:"configured"`
		} `json:"deployment"`
	} `json:"telegram"`
	IncidentDefaults centersettings.IncidentDefaults `json:"incident_defaults"`
	IncidentSweep    struct {
		Mode              string `json:"mode"`
		Source            string `json:"source"`
		DeploymentSeconds int    `json:"deployment_seconds"`
	} `json:"incident_sweep"`
}

func servePostgresSettingsPut(handler http.Handler, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	return recorder
}

func openSettingsPostgresIntegrationSchema(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for settings PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for settings PostgreSQL integration tests")
	}
	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_settings_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(databaseName) {
		t.Fatalf("unsafe generated database name %q", databaseName)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres pool for settings schema setup: %v", err)
	}
	t.Cleanup(adminPool.Close)
	quotedDatabase := `"` + strings.ReplaceAll(databaseName, `"`, `""`) + `"`
	if _, err := adminPool.Exec(ctx, `create database `+quotedDatabase); err != nil {
		t.Fatalf("create temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop database if exists `+quotedDatabase+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", databaseName, err)
		}
	})
	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := storemigrate.Apply(ctx, testPool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return testPool
}

func waitForSettingsBlockedSessions(ctx context.Context, pool *pgxpool.Pool, want int) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := pool.QueryRow(ctx, `
select count(*)
from pg_stat_activity
where datname = current_database()
  and pid <> pg_backend_pid()
  and wait_event_type = 'Lock'
  and state = 'active'`).Scan(&waiting); err != nil {
			return err
		}
		if waiting >= want {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for %d lock waiters", want)
}

func resetCenterSettings(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `delete from center_settings where settings_id = $1`, centersettings.SingletonID); err != nil {
		t.Fatalf("reset center settings: %v", err)
	}
}

type blockingSettingsRepository struct {
	repo    *store.PostgresSettingsRepository
	entered chan struct{}
	release chan struct{}
	first   sync.Once
}

func (r *blockingSettingsRepository) GetSettings(ctx context.Context) (centersettings.CenterSettings, error) {
	return r.repo.GetSettings(ctx)
}

func (r *blockingSettingsRepository) MutateSettings(ctx context.Context, mutate centersettings.MutateSettingsFunc) (centersettings.CenterSettings, error) {
	if mutate == nil {
		return r.repo.MutateSettings(ctx, nil)
	}
	block := false
	r.first.Do(func() { block = true })
	return r.repo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		if block {
			close(r.entered)
			select {
			case <-r.release:
			case <-ctx.Done():
				return centersettings.CenterSettings{}, ctx.Err()
			}
		}
		return mutate(current)
	})
}

func assertCenterSettingsRowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want int, label string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from center_settings where settings_id = $1`, centersettings.SingletonID).Scan(&count); err != nil {
		t.Fatalf("count center settings %s: %v", label, err)
	}
	if count != want {
		t.Fatalf("center settings rows %s = %d, want %d", label, count, want)
	}
}

func serveSettingsRequest(handler http.Handler, request func() *http.Request, responses chan<- *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request())
	responses <- recorder
}

func settingsGlobalPutBody(t *testing.T, hostTier string) []byte {
	t.Helper()
	defaults := centersettings.Default()
	body := map[string]any{
		"feishu": map[string]any{
			"enabled":     defaults.FeishuEnabled,
			"webhook_url": defaults.FeishuWebhookURL,
		},
		"host_sample_frequency_tier": hostTier,
		"probe_frequency_defaults":   defaults.ProbeFrequencyDefaults,
		"incident_defaults":          defaults.IncidentDefaults,
		"override_rules":             defaults.OverrideRules,
		"retention_policy":           defaults.RetentionPolicy,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal global settings request: %v", err)
	}
	return encoded
}

func effectiveSettingsPutBody(t *testing.T, extras map[string]any) string {
	t.Helper()
	defaults := centersettings.FreshInstallDefaults()
	body := map[string]any{
		"feishu": map[string]any{
			"enabled":     defaults.FeishuEnabled,
			"webhook_url": defaults.FeishuWebhookURL,
		},
		"host_sample_frequency_tier": defaults.HostSampleFrequencyTier,
		"probe_frequency_defaults":   defaults.ProbeFrequencyDefaults,
		"incident_defaults":          defaults.IncidentDefaults,
		"override_rules":             defaults.OverrideRules,
		"retention_policy":           defaults.RetentionPolicy,
	}
	for key, value := range extras {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal effective settings request: %v", err)
	}
	return string(encoded)
}

func assertCenterSettingsEqual(t *testing.T, ctx context.Context, repo *store.PostgresSettingsRepository, want centersettings.CenterSettings, label string) {
	t.Helper()
	got, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings() %s: %v", label, err)
	}
	want.Effective = centersettings.EffectiveSettingsMetadata{
		Persisted:           true,
		TelegramSource:      centersettings.ConfigSourceDefault,
		IncidentSweepSource: centersettings.ConfigSourceDefault,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("settings %s = %#v, want %#v", label, got, want)
	}
}
