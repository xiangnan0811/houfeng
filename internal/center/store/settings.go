package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	centersettings "houfeng/internal/center/settings"
)

const ipQualityEnabledSQL = `
		select coalesce((ip_quality_settings->>'enabled')::boolean, true)
		from center_settings
		where settings_id = $1`

const getCenterSettingsSQL = `
		select
			settings_id,
			telegram_bot_token,
			telegram_chat_id,
			telegram_runtime_managed,
			feishu_enabled,
			feishu_webhook_url,
			host_sample_frequency_tier,
			probe_frequency_defaults,
			incident_defaults,
			override_rules,
			retention_policy,
			subscription_cost_settings,
			ip_quality_settings,
			created_at,
			updated_at
		from center_settings
		where settings_id = $1`

const getCenterSettingsForUpdateSQL = getCenterSettingsSQL + `
		for update`

const insertDefaultCenterSettingsSQL = `
		insert into center_settings (
			settings_id,
			telegram_bot_token,
			telegram_chat_id,
			telegram_runtime_managed,
			feishu_enabled,
			feishu_webhook_url,
			host_sample_frequency_tier,
			probe_frequency_defaults,
			incident_defaults,
			override_rules,
			retention_policy,
			subscription_cost_settings,
			ip_quality_settings
		) values ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11::jsonb,$12::jsonb,$13::jsonb)
		on conflict (settings_id) do nothing`

const updateCenterSettingsSQL = `
		update center_settings
		set telegram_bot_token = $2,
			telegram_chat_id = $3,
			telegram_runtime_managed = $4,
			feishu_enabled = $5,
			feishu_webhook_url = $6,
			host_sample_frequency_tier = $7,
			probe_frequency_defaults = $8::jsonb,
			incident_defaults = $9::jsonb,
			override_rules = $10::jsonb,
			retention_policy = $11::jsonb,
			subscription_cost_settings = $12::jsonb,
			ip_quality_settings = $13::jsonb,
			updated_at = now()
		where settings_id = $1`

type settingsQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type settingsTx interface {
	settingsQueryer
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresSettingsRepository struct {
	db      settingsQueryer
	beginTx func(context.Context, pgx.TxOptions) (settingsTx, error)
}

func NewPostgresSettingsRepository(db *pgxpool.Pool) *PostgresSettingsRepository {
	repo := &PostgresSettingsRepository{}
	if db != nil {
		repo.db = db
		repo.beginTx = func(ctx context.Context, options pgx.TxOptions) (settingsTx, error) {
			return db.BeginTx(ctx, options)
		}
	}
	return repo
}

var _ centersettings.Repository = (*PostgresSettingsRepository)(nil)

// IPQualityEnabled reads only the IP Quality enabled flag. It must not decode
// the full settings document.
func (r *PostgresSettingsRepository) IPQualityEnabled(ctx context.Context) (bool, error) {
	if ctx == nil || r == nil || r.db == nil {
		return false, fmt.Errorf("query ip quality enabled: invalid repository")
	}
	var enabled bool
	err := r.db.QueryRow(ctx, ipQualityEnabledSQL, centersettings.SingletonID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return centersettings.Default().IPQuality.Enabled, nil
	}
	if err != nil {
		return false, fmt.Errorf("query ip quality enabled: %w", err)
	}
	return enabled, nil
}
func (r *PostgresSettingsRepository) GetPersistedIncidentDefaults(ctx context.Context) (centersettings.IncidentDefaults, bool, error) {
	if ctx == nil || r == nil || r.db == nil {
		return centersettings.IncidentDefaults{}, false, fmt.Errorf("query persisted incident defaults: invalid repository")
	}
	var raw []byte
	err := r.db.QueryRow(ctx, `
		select incident_defaults
		from center_settings
		where settings_id = $1`, centersettings.SingletonID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return centersettings.IncidentDefaults{}, false, nil
	}
	if err != nil {
		return centersettings.IncidentDefaults{}, false, fmt.Errorf("query persisted incident defaults: %w", err)
	}
	if len(raw) == 0 {
		return centersettings.IncidentDefaults{}, false, nil
	}
	var defaults centersettings.IncidentDefaults
	if err := decodeSettingsJSON(raw, &defaults); err != nil {
		return centersettings.IncidentDefaults{}, true, fmt.Errorf("decode persisted incident defaults: %w", err)
	}
	if _, err := centersettings.ValidateIncidentDefaults(defaults); err != nil {
		return centersettings.IncidentDefaults{}, true, fmt.Errorf("validate persisted incident defaults: %w", err)
	}
	return defaults, true, nil
}

func (r *PostgresSettingsRepository) GetSettings(ctx context.Context) (centersettings.CenterSettings, error) {
	if ctx == nil || r == nil || r.db == nil {
		return centersettings.CenterSettings{}, fmt.Errorf("query center settings: invalid repository")
	}
	record, err := scanSettingsRow(ctx, r.db, getCenterSettingsSQL, centersettings.SingletonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return centersettings.Default(), nil
	}
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("query center settings: %w", err)
	}
	return record, nil
}

func (r *PostgresSettingsRepository) MutateSettings(ctx context.Context, mutate centersettings.MutateSettingsFunc) (centersettings.CenterSettings, error) {
	if mutate == nil {
		return centersettings.CenterSettings{}, fmt.Errorf("%w: mutation callback is nil", centersettings.ErrInvalidSettings)
	}
	if ctx == nil || r == nil || r.beginTx == nil {
		return centersettings.CenterSettings{}, fmt.Errorf("mutate center settings: invalid repository")
	}

	tx, err := r.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("begin center settings mutation: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	defaults, err := settingsWriteArgs(centersettings.Default())
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("encode default center settings: %w", err)
	}
	if _, err := tx.Exec(ctx, insertDefaultCenterSettingsSQL, defaults...); err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("initialize center settings: %w", err)
	}

	current, err := scanSettingsRow(ctx, tx, getCenterSettingsForUpdateSQL, centersettings.SingletonID)
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("read center settings for mutation: %w", err)
	}
	updated, err := mutate(current)
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("mutate center settings: %w", err)
	}
	normalized, err := centersettings.Validate(updated)
	if err != nil {
		return centersettings.CenterSettings{}, err
	}
	values, err := settingsWriteArgs(normalized)
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("encode center settings: %w", err)
	}
	if _, err := tx.Exec(ctx, updateCenterSettingsSQL, values...); err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("update center settings: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("commit center settings mutation: %w", err)
	}
	return normalized, nil
}

func settingsWriteArgs(input centersettings.CenterSettings) ([]any, error) {
	probeDefaults, err := json.Marshal(input.ProbeFrequencyDefaults)
	if err != nil {
		return nil, fmt.Errorf("marshal probe frequency defaults: %w", err)
	}
	incidentDefaults, err := json.Marshal(input.IncidentDefaults)
	if err != nil {
		return nil, fmt.Errorf("marshal incident defaults: %w", err)
	}
	overrideRules, err := json.Marshal(input.OverrideRules)
	if err != nil {
		return nil, fmt.Errorf("marshal override rules: %w", err)
	}
	retentionPolicy, err := json.Marshal(input.RetentionPolicy)
	if err != nil {
		return nil, fmt.Errorf("marshal retention policy: %w", err)
	}
	subscriptionCostSettings, err := json.Marshal(input.SubscriptionCost)
	if err != nil {
		return nil, fmt.Errorf("marshal subscription cost settings: %w", err)
	}
	ipQualitySettings, err := json.Marshal(input.IPQuality)
	if err != nil {
		return nil, fmt.Errorf("marshal ip quality settings: %w", err)
	}
	return []any{
		centersettings.SingletonID,
		input.Telegram.BotToken,
		input.Telegram.ChatID,
		input.Telegram.RuntimeManaged,
		input.FeishuEnabled,
		input.FeishuWebhookURL,
		input.HostSampleFrequencyTier,
		probeDefaults,
		incidentDefaults,
		overrideRules,
		retentionPolicy,
		subscriptionCostSettings,
		ipQualitySettings,
	}, nil
}

func scanSettingsRow(ctx context.Context, queryer settingsQueryer, sql string, args ...any) (centersettings.CenterSettings, error) {
	var (
		settingsID               string
		telegramBotToken         string
		telegramChatID           string
		telegramRuntimeManaged   bool
		feishuEnabled            bool
		feishuWebhookURL         string
		hostSampleFrequencyTier  string
		probeFrequencyDefaults   []byte
		incidentDefaults         []byte
		overrideRules            []byte
		retentionPolicy          []byte
		subscriptionCostSettings []byte
		ipQualitySettings        []byte
		createdAt                time.Time
		updatedAt                time.Time
	)

	if err := queryer.QueryRow(ctx, sql, args...).Scan(
		&settingsID,
		&telegramBotToken,
		&telegramChatID,
		&telegramRuntimeManaged,
		&feishuEnabled,
		&feishuWebhookURL,
		&hostSampleFrequencyTier,
		&probeFrequencyDefaults,
		&incidentDefaults,
		&overrideRules,
		&retentionPolicy,
		&subscriptionCostSettings,
		&ipQualitySettings,
		&createdAt,
		&updatedAt,
	); err != nil {
		return centersettings.CenterSettings{}, err
	}

	if settingsID != centersettings.SingletonID {
		return centersettings.CenterSettings{}, fmt.Errorf("unexpected settings id %q", settingsID)
	}

	record := centersettings.CenterSettings{
		Telegram: centersettings.TelegramSettings{
			BotToken:       telegramBotToken,
			ChatID:         telegramChatID,
			RuntimeManaged: telegramRuntimeManaged,
		},
		FeishuEnabled:           feishuEnabled,
		FeishuWebhookURL:        feishuWebhookURL,
		HostSampleFrequencyTier: hostSampleFrequencyTier,
	}

	if err := decodeSettingsJSON(probeFrequencyDefaults, &record.ProbeFrequencyDefaults); err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("decode probe frequency defaults: %w", err)
	}
	if err := decodeSettingsJSON(incidentDefaults, &record.IncidentDefaults); err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("decode incident defaults: %w", err)
	}
	if err := decodeSettingsJSON(overrideRules, &record.OverrideRules); err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("decode override rules: %w", err)
	}
	if err := decodeSettingsJSON(retentionPolicy, &record.RetentionPolicy); err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("decode retention policy: %w", err)
	}
	if len(subscriptionCostSettings) == 0 {
		record.SubscriptionCost = centersettings.Default().SubscriptionCost
	} else if err := decodeSettingsJSON(subscriptionCostSettings, &record.SubscriptionCost); err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("decode subscription cost settings: %w", err)
	}
	// 先填默认值再解码：缺少的字段（包括 enabled）与 sync plan、IPQualityEnabled 读取保持一致。
	record.IPQuality = centersettings.Default().IPQuality
	if len(ipQualitySettings) > 0 {
		if err := decodeSettingsJSON(ipQualitySettings, &record.IPQuality); err != nil {
			return centersettings.CenterSettings{}, fmt.Errorf("decode ip quality settings: %w", err)
		}
	}

	validated, err := centersettings.Validate(record)
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("validate stored center settings: %w", err)
	}
	return validated, nil
}

func decodeSettingsJSON(raw []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing data")
		}
		return err
	}
	return nil
}
