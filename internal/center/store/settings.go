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
	"github.com/jackc/pgx/v5/pgxpool"

	centersettings "houfeng/internal/center/settings"
)

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
			created_at,
			updated_at
		from center_settings
		where settings_id = $1`

const upsertCenterSettingsSQL = `
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
			retention_policy
		) values ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11::jsonb)
		on conflict (settings_id) do update
		set telegram_bot_token = excluded.telegram_bot_token,
			telegram_chat_id = excluded.telegram_chat_id,
			telegram_runtime_managed = excluded.telegram_runtime_managed,
			feishu_enabled = excluded.feishu_enabled,
			feishu_webhook_url = excluded.feishu_webhook_url,
			host_sample_frequency_tier = excluded.host_sample_frequency_tier,
			probe_frequency_defaults = excluded.probe_frequency_defaults,
			incident_defaults = excluded.incident_defaults,
			override_rules = excluded.override_rules,
			retention_policy = excluded.retention_policy,
			updated_at = now()
		returning
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
			created_at,
			updated_at`

type settingsQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresSettingsRepository struct {
	db settingsQueryer
}

func NewPostgresSettingsRepository(db *pgxpool.Pool) *PostgresSettingsRepository {
	return &PostgresSettingsRepository{db: db}
}

var _ centersettings.Repository = (*PostgresSettingsRepository)(nil)

func (r *PostgresSettingsRepository) GetSettings(ctx context.Context) (centersettings.CenterSettings, error) {
	record, err := r.scanSettingsRow(ctx, getCenterSettingsSQL, centersettings.SingletonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return centersettings.Default(), nil
	}
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("query center settings: %w", err)
	}
	return record, nil
}

func (r *PostgresSettingsRepository) PutSettings(ctx context.Context, input centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	record, err := r.putSettings(ctx, input)
	if err != nil {
		return centersettings.CenterSettings{}, err
	}
	return record, nil
}

func (r *PostgresSettingsRepository) putSettings(ctx context.Context, input centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	normalized, err := centersettings.Validate(input)
	if err != nil {
		return centersettings.CenterSettings{}, err
	}

	probeDefaults, err := json.Marshal(normalized.ProbeFrequencyDefaults)
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("marshal probe frequency defaults: %w", err)
	}
	incidentDefaults, err := json.Marshal(normalized.IncidentDefaults)
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("marshal incident defaults: %w", err)
	}
	overrideRules, err := json.Marshal(normalized.OverrideRules)
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("marshal override rules: %w", err)
	}
	retentionPolicy, err := json.Marshal(normalized.RetentionPolicy)
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("marshal retention policy: %w", err)
	}

	record, err := r.scanSettingsRow(
		ctx,
		upsertCenterSettingsSQL,
		centersettings.SingletonID,
		normalized.Telegram.BotToken,
		normalized.Telegram.ChatID,
		normalized.Telegram.RuntimeManaged,
		normalized.FeishuEnabled,
		normalized.FeishuWebhookURL,
		normalized.HostSampleFrequencyTier,
		probeDefaults,
		incidentDefaults,
		overrideRules,
		retentionPolicy,
	)
	if err != nil {
		return centersettings.CenterSettings{}, fmt.Errorf("upsert center settings: %w", err)
	}
	return record, nil
}

func (r *PostgresSettingsRepository) scanSettingsRow(ctx context.Context, sql string, args ...any) (centersettings.CenterSettings, error) {
	var (
		settingsID              string
		telegramBotToken        string
		telegramChatID          string
		telegramRuntimeManaged  bool
		feishuEnabled           bool
		feishuWebhookURL        string
		hostSampleFrequencyTier string
		probeFrequencyDefaults  []byte
		incidentDefaults        []byte
		overrideRules           []byte
		retentionPolicy         []byte
		createdAt               time.Time
		updatedAt               time.Time
	)

	if err := r.db.QueryRow(ctx, sql, args...).Scan(
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
