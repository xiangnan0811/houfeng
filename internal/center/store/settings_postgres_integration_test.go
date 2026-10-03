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
	repo := store.NewPostgresSettingsRepository(pool)

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

			settingsHandler := handlers.Settings(repo)
			subscriptionService := subscriptioncosts.NewService(nil, repo, nil)
			subscriptionHandler := handlers.SubscriptionSettings(subscriptionService)
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
		handler := handlers.SubscriptionSettings(service)
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
		handler := handlers.SubscriptionSettings(service)
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
		"telegram": map[string]any{
			"chat_id":         defaults.Telegram.ChatID,
			"runtime_managed": defaults.Telegram.RuntimeManaged,
		},
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

func assertCenterSettingsEqual(t *testing.T, ctx context.Context, repo *store.PostgresSettingsRepository, want centersettings.CenterSettings, label string) {
	t.Helper()
	got, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings() %s: %v", label, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("settings %s = %#v, want %#v", label, got, want)
	}
}
