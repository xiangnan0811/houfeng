import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { authSnapshotError, type User } from '../lib/auth-client'
import * as authClient from '../lib/auth-client'
import { AuthProvider } from '../lib/auth-context'
import { ThemeProvider } from '../lib/theme-context'
import { SettingsPage } from './SettingsPage'

const sessionUser = (access: boolean): User => ({
  user_id: 'u1',
  username: 'admin',
  role: 'admin',
  display_name: '管理员',
  runtime_capabilities: { records: true, comparison: true, portability: true },
  management_capabilities: { access },
})

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

const deploymentTelegram = {
  configured: true,
  chat_id: 'chat-id',
  token_present: true,
  token_masked_summary: '****************oken',
}

function telegramResponse(overrides: Record<string, unknown> = {}) {
  return {
    mode: 'inherit',
    source: 'deployment',
    active: true,
    chat_id: 'chat-id',
    token_present: true,
    token_masked_summary: '****************oken',
    deployment: deploymentTelegram,
    ...overrides,
  }
}

const settingsResponseBody = {
  telegram: telegramResponse(),
  feishu: {
    enabled: false,
    webhook_url_present: false,
    webhook_url_masked_summary: '',
  },
  host_sample_frequency_tier: '5s',
  probe_frequency_defaults: {
    tcp: '5s',
    http: '5s',
    tls: '6h',
  },
  incident_defaults: {
    heartbeat_interval_seconds: 5,
    stale_threshold_intervals: 12,
    sweep_interval_seconds: 5,
    notify_on_started: true,
    notify_on_escalated: true,
    notify_on_recovered: true,
    cpu_warning_pct: 80,
    cpu_alert_pct: 90,
    cpu_critical_pct: 95,
    mem_warning_pct: 85,
    mem_alert_pct: 92,
    mem_critical_pct: 95,
    disk_warning_pct: 85,
    disk_alert_pct: 92,
    disk_critical_pct: 97,
    inode_warning_pct: 80,
    inode_alert_pct: 90,
    inode_critical_pct: 95,
    iowait_warning_pct: 20,
    iowait_critical_pct: 50,
    load5_warning: 4,
    load5_critical: 8,
  },
  override_rules: {
    monitoring_instance_labels: [
      {
        label: 'edge',
        overrides: {
          host_sample_frequency_tier: '1m',
          probe_frequency_defaults: { http: '1m' },
        },
      },
    ],
    target_types: [
      {
        target_type: 'service',
        overrides: {
          incident_defaults: { stale_threshold_intervals: 4 },
        },
      },
    ],
    target_labels: [
      {
        label: 'external',
        overrides: {
          probe_frequency_defaults: { tls: '15m' },
        },
      },
    ],
  },
  retention_policy: {
    raw_layer_days: 30,
    aggregate_layer_days: 365,
  },
  ip_quality_settings: {
    enabled: true,
    frequency_seconds: 86400,
    stale_after_seconds: 604800,
    timeout_seconds: 15,
    services: ['netflix', 'chatgpt', 'youtube-premium'],
    mail_enabled: true,
    sources: {
      ipapi_is: { configured: true, masked_summary: '****f00d' },
      abuseipdb: { configured: false, masked_summary: '' },
      maxmind: { configured: true, masked_summary: '账号 ****21 · Key ****9abc' },
    },
  },
  incident_sweep: {
    mode: 'inherit',
    source: 'default',
    deployment_seconds: 5,
  },
  settings_source: 'database',
  subscription_cost_settings: {
    base_currency: 'CNY',
    exchange_rate_provider: 'frankfurter',
    fixer_configured: false,
    fixer_masked_summary: '',
    default_reminder_offsets_days: [14, 7, 1],
    max_reminder_lead_days: 30,
    exchange_rate_stale_after_hours: 36,
  },
}

// 保存其它设置时 IP 质量块回传的是可编辑字段；凭据未改动则整块 sources 省略。
const ipQualityUpdatePayload = {
  enabled: true,
  frequency_seconds: 86400,
  stale_after_seconds: 604800,
  timeout_seconds: 15,
  services: ['netflix', 'chatgpt', 'youtube-premium'],
  mail_enabled: true,
}

describe('SettingsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  function renderSettingsPage(initialEntry = '/settings', session: { user?: User; fail?: boolean } = {}) {
    const me = vi.spyOn(authClient, 'me')
    if (session.fail) me.mockRejectedValue(authSnapshotError())
    else me.mockResolvedValue(session.user ?? sessionUser(false))
    return render(
      <MemoryRouter initialEntries={[initialEntry]}>
        <AuthProvider>
          <ThemeProvider>
            <SettingsPage />
          </ThemeProvider>
        </AuthProvider>
      </MemoryRouter>,
    )
  }

  function switchTab(label: string) {
    fireEvent.click(screen.getByRole('tab', { name: label }))
  }

  async function expandTelegram() {
    switchTab('通知')
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Telegram 通知设置' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
  }

  it('hides the save bar on the appearance tab until another section has unsaved edits', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)))
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    expect(screen.getByText(/选择后立即生效，无需保存/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存设置' })).not.toBeInTheDocument()

    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('IP 质量采集周期秒数'), { target: { value: '172800' } })
    switchTab('外观')
    expect(screen.getByRole('button', { name: '保存设置' })).toBeInTheDocument()
    expect(screen.getByText('有未保存的修改')).toBeInTheDocument()

    // 在外观分区保存后，表单不再脏，但保存结果仍需可见。
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(screen.queryByText('有未保存的修改')).not.toBeInTheDocument())
    expect(document.querySelector('.settings-save-footer__message--success')).not.toBeNull()
  })

  it('loads persisted settings into the required sections and keeps Telegram and retention copy truthful', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)),
    )

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    expect(screen.getByText(/本地浏览器偏好/)).toBeInTheDocument()
    expect(screen.queryByText(/本分区保存在数据库中/)).not.toBeInTheDocument()

    // Switch to monitoring tab to check frequency
    switchTab('监控策略')
    expect(screen.getByText(/本分区保存在数据库中/)).toBeInTheDocument()
    expect(screen.getByLabelText('当前监控实例主机样本频率')).toHaveValue('5s')
    expect(screen.getByRole('switch', { name: '启用 IP 质量采集' })).toBeChecked()
    expect(screen.getByLabelText('IP 质量采集周期秒数')).toHaveValue('86400')
    // 秒数旁给出换算，免去手算。
    expect(screen.getByLabelText('IP 质量采集周期秒数').parentElement).toHaveTextContent('（1 天）')
    expect(screen.getByLabelText('IP 质量采集服务集合')).toHaveValue('netflix, chatgpt, youtube-premium')
    expect(screen.getByLabelText('原始层保留天数')).toHaveValue('30')
    expect(screen.getByLabelText('聚合层保留天数')).toHaveValue('365')
    for (const removed of ['事件层保留天数', '通知层保留天数', 'IP 质量原始 JSON 保留天数', 'IP 质量历史保留天数']) {
      expect(screen.queryByLabelText(removed)).not.toBeInTheDocument()
    }
    expect(screen.getByText('IP 质量报告及脱敏后的原始结果长期保留。')).toBeInTheDocument()

    // Switch to notification tab to check Telegram. 编辑会进入覆盖草稿，这里只核对已生效摘要。
    switchTab('通知')
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Telegram 通知设置' })).toBeInTheDocument())

    expect(screen.getByText(/当前生效：部署配置，已启用/)).toBeInTheDocument()
    expect(screen.getByText(/部署基线：已配置/)).toBeInTheDocument()
    expect(screen.getAllByText('****************oken').length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: '继承部署配置' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByLabelText('新的 Telegram Bot Token')).not.toBeInTheDocument()

    // Switch to advanced tab to check override rules
    switchTab('高级')
    expect(screen.getByLabelText('监控实例标签规则 1 标签')).toHaveValue('edge')
    expect(screen.getByLabelText('目标类型规则 1 类型')).toHaveValue('service')
    expect(screen.getByLabelText('目标标签规则 1 标签')).toHaveValue('external')
  })

  it('explains the default heartbeat incident boundaries and keeps a custom threshold unchanged in PUT', async () => {
    const customSettings = {
      ...settingsResponseBody,
      incident_defaults: {
        ...settingsResponseBody.incident_defaults,
        stale_threshold_intervals: 20,
      },
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(customSettings))
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...customSettings,
          incident_defaults: {
            ...customSettings.incident_defaults,
            sweep_interval_seconds: 10,
          },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')

    expect(screen.getByText('首次达到 N 次遗漏周期时判定失联；默认 N = 12，按 5 秒心跳约 60 秒。')).toBeInTheDocument()
    expect(screen.getByText('达到 2N 时升级为告警，达到 4N 时升级为严重；恢复需连续 3 次非回填实时心跳。')).toBeInTheDocument()
    expect(screen.getByLabelText('失联判定阈值')).toHaveValue('20')

    fireEvent.click(screen.getByRole('button', { name: '页面覆盖' }))
    fireEvent.change(screen.getByLabelText('扫描间隔秒数'), { target: { value: '10' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const saved = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(saved).toMatchObject({
      incident_defaults: {
        stale_threshold_intervals: 20,
        sweep_interval_seconds: 10,
      },
      incident_sweep_mode: 'override',
    })
    expect(saved).not.toHaveProperty('telegram')
  })

  it('keeps a disabled Telegram channel visible with its deployment baseline', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: telegramResponse({
            mode: 'disabled',
            source: 'default',
            active: false,
            chat_id: '',
            token_present: false,
            token_masked_summary: '',
            deployment: {
              configured: true,
              chat_id: 'deploy-chat',
              token_present: true,
              token_masked_summary: '****ploy',
            },
          }),
        }),
      ),
    )

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    await expandTelegram()
    expect(screen.getByRole('button', { name: '停用' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText(/部署基线：已配置/)).toBeInTheDocument()
    expect(screen.getByText(/deploy-chat/)).toBeInTheDocument()
    expect(screen.getByText(/已停用/)).toBeInTheDocument()
    expect(screen.queryByLabelText('新的 Telegram Bot Token')).not.toBeInTheDocument()
  })

  it('shows persisted Feishu webhook as masked write-only state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          ...settingsResponseBody,
          feishu: {
            enabled: true,
            webhook_url_present: true,
            webhook_url_masked_summary: 'https://open.feishu.cn/***abcd',
          },
        }),
      ),
    )

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    switchTab('通知')
    await waitFor(() => expect(screen.getByRole('heading', { name: '飞书通知设置' })).toBeInTheDocument())
    const feishuEditButton = screen.getAllByRole('button', { name: '编辑' })[1]
    if (!feishuEditButton) throw new Error('notification settings must expose Feishu editing')
    fireEvent.click(feishuEditButton)

    expect(screen.getByText(/已配置飞书 Webhook/)).toHaveTextContent('https://open.feishu.cn/***abcd')
    expect(screen.getByLabelText('Webhook URL')).toHaveValue('')
    expect(screen.queryByDisplayValue(/full-secret/)).not.toBeInTheDocument()
  })

  it('saves unrelated settings without requiring Telegram or Feishu secret re-entry and omits stored secrets from the payload', async () => {
    const settingsWithStoredSecrets = {
      ...settingsResponseBody,
      feishu: {
        enabled: true,
        webhook_url_present: true,
        webhook_url_masked_summary: 'https://open.feishu.cn/***abcd',
      },
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsWithStoredSecrets))
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsWithStoredSecrets,
          host_sample_frequency_tier: '1m',
          retention_policy: {
            ...settingsResponseBody.retention_policy,
            raw_layer_days: 45,
          },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('当前监控实例主机样本频率'), {
      target: { value: '1m' },
    })
    fireEvent.change(screen.getByLabelText('原始层保留天数'), {
      target: { value: '45' },
    })

    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    const payload = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(payload).not.toHaveProperty('subscription_cost_settings')
    expect(payload).not.toHaveProperty('telegram')
    expect(payload).not.toHaveProperty('incident_sweep_mode')
    expect(payload).toEqual({
      feishu: {
        enabled: true,
      },
      host_sample_frequency_tier: '1m',
      probe_frequency_defaults: {
        tcp: '5s',
        http: '5s',
        tls: '6h',
      },
      incident_defaults: {
        heartbeat_interval_seconds: 5,
        stale_threshold_intervals: 12,
        sweep_interval_seconds: 5,
        notify_on_started: true,
        notify_on_escalated: true,
        notify_on_recovered: true,
        cpu_warning_pct: 80,
        cpu_alert_pct: 90,
        cpu_critical_pct: 95,
        mem_warning_pct: 85,
        mem_alert_pct: 92,
        mem_critical_pct: 95,
        disk_warning_pct: 85,
        disk_alert_pct: 92,
        disk_critical_pct: 97,
        inode_warning_pct: 80,
        inode_alert_pct: 90,
        inode_critical_pct: 95,
        iowait_warning_pct: 20,
        iowait_critical_pct: 50,
        load5_warning: 4,
        load5_critical: 8,
      },
      override_rules: settingsResponseBody.override_rules,
      retention_policy: {
        raw_layer_days: 45,
        aggregate_layer_days: 365,
      },
      ip_quality_settings: ipQualityUpdatePayload,
    })
  })

  it('saves IP quality settings from the monitoring policy tab', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsResponseBody,
          ip_quality_settings: {
            enabled: false,
            frequency_seconds: 259200,
            stale_after_seconds: 864000,
            timeout_seconds: 20,
            services: ['netflix', 'chatgpt'],
            mail_enabled: false,
            sources: {
              ipapi_is: { configured: true, masked_summary: '****f00d' },
              abuseipdb: { configured: true, masked_summary: '****beef' },
              maxmind: { configured: true, masked_summary: '账号 ****21 · Key ****9abc' },
            },
          },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    switchTab('监控策略')
    fireEvent.click(screen.getByRole('switch', { name: '启用 IP 质量采集' }))
    fireEvent.change(screen.getByLabelText('IP 质量采集周期秒数'), { target: { value: '259200' } })
    fireEvent.change(screen.getByLabelText('IP 质量过期窗口秒数'), { target: { value: '864000' } })
    expect(screen.getByLabelText('IP 质量过期窗口秒数').parentElement).toHaveTextContent('（10 天）')
    fireEvent.change(screen.getByLabelText('IP 质量请求超时秒数'), { target: { value: '20' } })
    fireEvent.change(screen.getByLabelText('IP 质量采集服务集合'), { target: { value: 'netflix, chatgpt' } })
    fireEvent.click(screen.getByRole('switch', { name: '检测邮局 25 端口连通性' }))
    fireEvent.change(screen.getByLabelText('AbuseIPDB API Key'), { target: { value: 'abuse-secret-value' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const body = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(body.ip_quality_settings).toEqual({
      enabled: false,
      frequency_seconds: 259200,
      stale_after_seconds: 864000,
      timeout_seconds: 20,
      services: ['netflix', 'chatgpt'],
      mail_enabled: false,
      // 未改动的 ipapi.is 省略即保留；掩码从不回传。
      sources: { abuseipdb_key: 'abuse-secret-value' },
    })
    expect(String(fetchMock.mock.calls[1]?.[1]?.body)).not.toContain('****f00d')
    await waitFor(() => expect(screen.getByText('已配置 · ****beef')).toBeInTheDocument())
    expect(screen.getByLabelText('AbuseIPDB API Key')).toHaveValue('')
  })

  it('clears an IP quality credential only when explicitly requested and rejects masked keys', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(mockJSONResponse({
        ...settingsResponseBody,
        ip_quality_settings: {
          ...settingsResponseBody.ip_quality_settings,
          sources: { ...settingsResponseBody.ip_quality_settings.sources, ipapi_is: { configured: false, masked_summary: '' } },
        },
      }))
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')

    // 掩码不能作为新 Key 提交。
    fireEvent.change(screen.getByLabelText('ipapi.is API Key'), { target: { value: '****f00d' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(screen.getByText('ipapi.is Key 不能使用掩码。')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '清除 ipapi.is Key' }))
    expect(screen.getByLabelText('ipapi.is API Key')).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const body = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(body.ip_quality_settings.sources).toEqual({ ipapi_is_key: '' })
    await waitFor(() => expect(screen.getByLabelText('ipapi.is API Key').parentElement).toHaveTextContent('未配置'))
    expect(screen.getByLabelText('ipapi.is API Key')).toBeEnabled()
  })

  it('writes GeoLite account ID and license key as one pair and clears both together', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(mockJSONResponse({
        ...settingsResponseBody,
        ip_quality_settings: {
          ...settingsResponseBody.ip_quality_settings,
          sources: { ...settingsResponseBody.ip_quality_settings.sources, maxmind: { configured: false, masked_summary: '' } },
        },
      }))
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')

    // 已保存的凭据只显示掩码，两个输入框都不预填。
    expect(screen.getByText('已配置 · 账号 ****21 · Key ****9abc')).toBeInTheDocument()
    expect(screen.getByLabelText('MaxMind GeoLite 账号 ID')).toHaveValue('')
    expect(screen.getByLabelText('MaxMind GeoLite License Key')).toHaveValue('')

    // 只填一半不提交。
    fireEvent.change(screen.getByLabelText('MaxMind GeoLite License Key'), { target: { value: 'new-license' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(screen.getByText('GeoLite 账号 ID 与 License Key 需同时填写。')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fireEvent.change(screen.getByLabelText('MaxMind GeoLite 账号 ID'), { target: { value: ' 123421 ' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const replaced = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(replaced.ip_quality_settings.sources).toEqual({ maxmind_account_id: '123421', maxmind_license_key: 'new-license' })
    expect(String(fetchMock.mock.calls[1]?.[1]?.body)).not.toContain('****')

    fireEvent.click(screen.getByRole('button', { name: '清除 MaxMind GeoLite 账号 ID 与 License Key' }))
    expect(screen.getByLabelText('MaxMind GeoLite 账号 ID')).toBeDisabled()
    expect(screen.getByLabelText('MaxMind GeoLite License Key')).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    const cleared = JSON.parse(String(fetchMock.mock.calls[2]?.[1]?.body))
    expect(cleared.ip_quality_settings.sources).toEqual({ maxmind_account_id: '', maxmind_license_key: '' })
    await waitFor(() => expect(screen.getByText('未配置 · 不配置也有默认检测')).toBeInTheDocument())
  })

  it('opens subscription settings from query tab and manages subscription-specific APIs', async () => {
    let subscriptionSettings = settingsResponseBody.subscription_cost_settings
    const monthlyBudget = {
      budget_month: '2026-06-01',
      base_currency: 'CNY',
      monthly_limit: 100,
      warning_pct: 80,
      note: '',
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
    }
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/settings') return Promise.resolve(mockJSONResponse(settingsResponseBody))
      if (url === '/api/subscriptions/settings' && method === 'GET') return Promise.resolve(mockJSONResponse(subscriptionSettings))
      if (url === '/api/subscriptions/settings' && method === 'PUT') {
        subscriptionSettings = {
          ...subscriptionSettings,
          base_currency: 'USD',
          max_reminder_lead_days: 45,
        }
        return Promise.resolve(mockJSONResponse(subscriptionSettings))
      }
      if (url === '/api/subscription-monthly-budgets' && method === 'GET') return Promise.resolve(mockJSONResponse([monthlyBudget]))
      if (url === '/api/subscription-monthly-budgets/2026-07' && method === 'PUT') return Promise.resolve(mockJSONResponse({
        ...monthlyBudget,
        budget_month: '2026-07-01',
        base_currency: 'USD',
        monthly_limit: 120,
        note: '增长期',
      }))
      if (url === '/api/subscription-monthly-budgets/bulk' && method === 'POST') return Promise.resolve(mockJSONResponse({
        scope: 'current_year',
        start_month: '2026-01-01',
        end_month: '2026-07-01',
        records: [
          { ...monthlyBudget, budget_month: '2026-01-01', base_currency: 'USD', monthly_limit: 200, note: '首次基线' },
          { ...monthlyBudget, budget_month: '2026-07-01', base_currency: 'USD', monthly_limit: 200, note: '首次基线' },
        ],
      }))
      if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
        return Promise.resolve(mockJSONResponse({ items: [] }))
      }
      if (url === '/api/subscriptions/exchange-rates/refresh' && method === 'POST') {
        return Promise.resolve(mockJSONResponse({
          items: [{ provider: 'frankfurter', base_currency: 'CNY', quote_currency: 'USD', rate_status: 'fresh', refresh_status: 'idle', attempt_count: 0 }],
        }, 202))
      }
      return Promise.resolve(mockJSONResponse({ error: `unhandled ${method} ${url}` }, 404))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage('/settings?tab=subscriptions')

    await waitFor(() => expect(screen.getByRole('heading', { name: '成本基准与汇率' })).toBeInTheDocument())
    expect(screen.getByRole('tab', { name: '订阅' })).toHaveAttribute('aria-selected', 'true')
    expect(fetchMock.mock.calls.some(([url]) => url === '/api/settings')).toBe(false)

    fireEvent.change(screen.getByLabelText('基准货币'), { target: { value: 'USD' } })
    fireEvent.change(screen.getByLabelText('最远提前天数'), { target: { value: '45' } })
    fireEvent.click(screen.getByRole('button', { name: '保存订阅配置' }))
    await waitFor(() => expect(fetchMock.mock.calls.some(([url, init]) => url === '/api/subscriptions/settings' && (init as RequestInit | undefined)?.method === 'PUT')).toBe(true))
    await waitFor(() => expect(screen.getByLabelText('月预算 USD')).toBeInTheDocument())
    const settingsPuts = () => fetchMock.mock.calls.filter(([url, init]) => url === '/api/subscriptions/settings' && (init as RequestInit | undefined)?.method === 'PUT').length
    expect(settingsPuts()).toBe(1)

    // 新增月预算有自己的保存按钮，不再触发成本设置 PUT。
    const addForm = screen.getByRole('form', { name: '新增月预算' })
    fireEvent.change(within(addForm).getByLabelText('预算月份'), { target: { value: '2026-07' } })
    fireEvent.change(within(addForm).getByLabelText('月预算 USD'), { target: { value: '120' } })
    fireEvent.change(within(addForm).getByLabelText('备注'), { target: { value: '增长期' } })
    fireEvent.click(within(addForm).getByRole('button', { name: '添加预算' }))
    await waitFor(() => expect(fetchMock.mock.calls.some(([url, init]) => url === '/api/subscription-monthly-budgets/2026-07' && (init as RequestInit | undefined)?.method === 'PUT')).toBe(true))
    expect(await within(addForm).findByRole('status')).toHaveTextContent('预算已保存')
    expect(settingsPuts()).toBe(1)
    expect(screen.getByRole('heading', { name: '月预算' })).toBeInTheDocument()

    fireEvent.change(within(addForm).getByLabelText('月预算 USD'), { target: { value: '200' } })
    fireEvent.change(within(addForm).getByLabelText('备注'), { target: { value: '首次基线' } })
    fireEvent.click(within(addForm).getByRole('checkbox', { name: '批量覆盖历史月份' }))
    // 批量覆盖按范围写入，不需要单个月份。
    expect(within(addForm).queryByLabelText('预算月份')).not.toBeInTheDocument()
    fireEvent.change(within(addForm).getByLabelText('覆盖范围'), { target: { value: 'current_year' } })
    fireEvent.click(within(addForm).getByRole('button', { name: '添加预算' }))
    await waitFor(() => expect(fetchMock.mock.calls.some(([url, init]) => url === '/api/subscription-monthly-budgets/bulk' && (init as RequestInit | undefined)?.method === 'POST')).toBe(true))
    const bulkCall = fetchMock.mock.calls.find(([url, init]) => url === '/api/subscription-monthly-budgets/bulk' && (init as RequestInit | undefined)?.method === 'POST')
    expect(JSON.parse(String((bulkCall?.[1] as RequestInit | undefined)?.body))).toEqual({
      scope: 'current_year',
      base_currency: 'USD',
      monthly_limit: 200,
      warning_pct: 80,
      note: '首次基线',
    })
    expect(await within(addForm).findByRole('status')).toHaveTextContent('今年月预算已保存，共覆盖 2 个月')
    expect(settingsPuts()).toBe(1)

    fireEvent.click(screen.getByRole('button', { name: '刷新汇率' }))
    await waitFor(() => expect(fetchMock.mock.calls.some(([url, init]) => url === '/api/subscriptions/exchange-rates/refresh' && (init as RequestInit | undefined)?.method === 'POST')).toBe(true))
    await waitFor(() => expect(screen.getByRole('button', { name: '刷新汇率' })).toBeEnabled())
    expect(screen.queryByText('汇率已更新')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '月预算' })).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([url]) => String(url).startsWith('/api/subscription-budgets'))).toBe(false)
  })

  it('validates and saves budgets on their own form without touching cost settings', async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/subscriptions/settings' && method === 'GET') {
        return Promise.resolve(mockJSONResponse(settingsResponseBody.subscription_cost_settings))
      }
      if (url === '/api/subscription-monthly-budgets' && method === 'GET') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (url === '/api/subscriptions/settings' && method === 'PUT') {
        return Promise.resolve(mockJSONResponse(settingsResponseBody.subscription_cost_settings))
      }
      if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
        return Promise.resolve(mockJSONResponse({ items: [] }))
      }
      return Promise.resolve(mockJSONResponse({ error: `unhandled ${method} ${url}` }, 404))
    })
    vi.stubGlobal('fetch', fetchMock)
    const writes = () => fetchMock.mock.calls.filter(([, init]) => ((init as RequestInit | undefined)?.method ?? 'GET') !== 'GET').map(([url]) => url)

    renderSettingsPage('/settings?tab=subscriptions')

    await waitFor(() => expect(screen.getByRole('heading', { name: '成本基准与汇率' })).toBeInTheDocument())
    expect(screen.getByText('尚未配置月预算')).toBeInTheDocument()
    const addForm = screen.getByRole('form', { name: '新增月预算' })
    // 负数由原生 min 约束拦截；空金额走页面校验并给出中文提示。
    fireEvent.click(within(addForm).getByRole('button', { name: '添加预算' }))
    expect(await within(addForm).findByRole('alert')).toHaveTextContent('月预算不能为空。')
    expect(writes()).toEqual([])

    // 底部保存只提交成本设置，未提交的预算草稿不随之写入。
    fireEvent.change(within(addForm).getByLabelText('月预算 CNY'), { target: { value: '120' } })
    fireEvent.click(screen.getByRole('button', { name: '保存订阅配置' }))
    await waitFor(() => expect(writes()).toEqual(['/api/subscriptions/settings']))
    expect(await screen.findByText('订阅成本设置已保存')).toBeInTheDocument()
  })

  it('disables subscription save while the settings save is in flight', async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET'
      if (url === '/api/subscriptions/settings' && method === 'GET') {
        return Promise.resolve(mockJSONResponse(settingsResponseBody.subscription_cost_settings))
      }
      if (url === '/api/subscription-monthly-budgets' && method === 'GET') {
        return Promise.resolve(mockJSONResponse([]))
      }
      if (url === '/api/subscriptions/settings' && method === 'PUT') {
        return new Promise<Response>(() => {})
      }
      if (url === '/api/subscriptions/exchange-rates/status' && method === 'GET') {
        return Promise.resolve(mockJSONResponse({ items: [] }))
      }
      return Promise.resolve(mockJSONResponse({ error: `unhandled ${method} ${url}` }, 404))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage('/settings?tab=subscriptions')

    await waitFor(() => expect(screen.getByRole('heading', { name: '成本基准与汇率' })).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText('最远提前天数'), { target: { value: '45' } })
    fireEvent.click(screen.getByRole('button', { name: '保存订阅配置' }))

    const savingButton = await screen.findByRole('button', { name: '保存中…' })
    expect(savingButton).toBeDisabled()
  })

  it('falls back to appearance for invalid settings tab', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)))

    renderSettingsPage('/settings?tab=missing')

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    const tablist = screen.getByRole('tablist', { name: '系统设置分区' })
    const activeTab = within(tablist).getByRole('tab', { name: '外观' })
    const panel = screen.getByRole('tabpanel')
    expect(activeTab).toHaveAttribute('aria-selected', 'true')
    expect(activeTab).toHaveAttribute('aria-controls', panel.id)
    expect(panel).toHaveAttribute('aria-labelledby', activeTab.id)
  })

  it('uses segmented buttons for theme values instead of tabs without panels', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)))

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    const presetGroup = screen.getByRole('group', { name: '主题风格' })
    const modeGroup = screen.getByRole('group', { name: '主题明暗' })
    expect(within(presetGroup).getByRole('button', { name: '候风' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(modeGroup).getByRole('button', { name: '深色' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(presetGroup).queryByRole('tab')).not.toBeInTheDocument()
    expect(screen.queryByText(/观测台仅提供深色/)).not.toBeInTheDocument()

    fireEvent.click(within(presetGroup).getByRole('button', { name: '精密' }))
    expect(within(presetGroup).getByRole('button', { name: '精密' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(within(presetGroup).getByRole('button', { name: '观测台' }))
    expect(within(presetGroup).getByRole('button', { name: '观测台' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText(/观测台仅提供深色/)).toBeInTheDocument()
  })

  it('saves updated settings with a replacement Telegram token and refreshed defaults', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: telegramResponse({
            mode: 'override',
            source: 'database',
            token_masked_summary: '***************oken',
          }),
          host_sample_frequency_tier: '1m',
          retention_policy: {
            ...settingsResponseBody.retention_policy,
            raw_layer_days: 45,
          },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    await expandTelegram()
    fireEvent.change(screen.getByLabelText('新的 Telegram Bot Token'), {
      target: { value: 'replacement-token' },
    })
    expect(screen.queryByText('replacement-token')).not.toBeInTheDocument()
    // 先保存 Telegram 替换 token（此举会清空草稿，避免跨 tab 未保存拦截）
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toMatchObject({
      telegram: {
        mode: 'override',
        bot_token: 'replacement-token',
      },
    })
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body)).telegram).not.toHaveProperty('chat_id')

    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('当前监控实例主机样本频率'), {
      target: { value: '1m' },
    })
    fireEvent.change(screen.getByLabelText('原始层保留天数'), {
      target: { value: '45' },
    })

    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    const finalPayload = JSON.parse(String(fetchMock.mock.calls[2]?.[1]?.body))
    expect(finalPayload).toMatchObject({
      host_sample_frequency_tier: '1m',
      retention_policy: {
        raw_layer_days: 45,
      },
    })
    expect(finalPayload).not.toHaveProperty('telegram')
    expect(finalPayload).not.toHaveProperty('incident_sweep_mode')
  })

  it('warns before leaving a tab with unsaved changes and can discard', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)))

    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('原始层保留天数'), { target: { value: '45' } })
    expect(screen.getByText('有未保存的修改')).toBeInTheDocument()

    // 未保存就切到外观：应弹窗且不真正切换
    switchTab('外观')
    expect(screen.getByRole('button', { name: '不保存' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存并切换' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '监控策略' })).toHaveAttribute('aria-selected', 'true')

    // 选择“不保存”后丢弃修改并切换
    fireEvent.click(screen.getByRole('button', { name: '不保存' }))
    await waitFor(() => expect(screen.getByRole('tab', { name: '外观' })).toHaveAttribute('aria-selected', 'true'))
    expect(screen.queryByText('有未保存的修改')).not.toBeInTheDocument()
  })

  it('discards an unsaved newly added notification channel when leaving without saving', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: telegramResponse({
            mode: 'inherit',
            source: 'default',
            active: false,
            chat_id: '',
            token_present: false,
            token_masked_summary: '',
            deployment: { configured: false, chat_id: '', token_present: false },
          }),
        }),
      ),
    )

    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    switchTab('通知')
    fireEvent.click(screen.getByRole('button', { name: '+ 新增通知渠道' }))
    fireEvent.click(screen.getByRole('button', { name: 'Telegram' }))
    fireEvent.change(screen.getByLabelText('新的 Telegram Bot Token'), { target: { value: 'draft-token' } })
    fireEvent.change(screen.getByLabelText('Telegram Chat ID'), { target: { value: 'draft-chat' } })
    fireEvent.click(screen.getByRole('button', { name: '添加并编辑' }))
    expect(screen.getByRole('heading', { name: 'Telegram 通知设置' })).toBeInTheDocument()
    expect(screen.getByText('有未保存的修改')).toBeInTheDocument()

    switchTab('监控策略')
    fireEvent.click(screen.getByRole('button', { name: '不保存' }))
    await waitFor(() => expect(screen.getByRole('tab', { name: '监控策略' })).toHaveAttribute('aria-selected', 'true'))

    switchTab('通知')
    await waitFor(() => expect(screen.getByRole('tab', { name: '通知' })).toHaveAttribute('aria-selected', 'true'))
    expect(screen.queryByRole('heading', { name: 'Telegram 通知设置' })).not.toBeInTheDocument()
  })

  it('rejects raw retention below the 30 day monitoring window minimum', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('原始层保留天数'), {
      target: { value: '14' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('原始层天数必须至少为 30 天。'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('rejects raw retention above the 365 day maximum', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('原始层保留天数'), { target: { value: '366' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('原始层天数不能超过 365 天。'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('rejects misordered incident thresholds before submitting settings', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('CPU 关注阈值'), {
      target: { value: '95' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('CPU 阈值必须满足 关注 < 告警 < 严重。'))
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fireEvent.change(screen.getByLabelText('CPU 关注阈值'), {
      target: { value: '80' },
    })
    fireEvent.change(screen.getByLabelText('Load5 严重阈值'), {
      target: { value: '4' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Load5 阈值必须满足 关注 < 严重。'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('saves a Telegram chat-id-only update when a token is already stored', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: telegramResponse({
            mode: 'override',
            source: 'database',
            chat_id: 'new-chat-id',
          }),
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    await expandTelegram()
    fireEvent.change(screen.getByLabelText('Telegram Chat ID'), {
      target: { value: 'new-chat-id' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
      telegram: {
        mode: 'override',
        chat_id: 'new-chat-id',
      },
      feishu: {
        enabled: false,
      },
      host_sample_frequency_tier: '5s',
      probe_frequency_defaults: {
        tcp: '5s',
        http: '5s',
        tls: '6h',
      },
      incident_defaults: {
        heartbeat_interval_seconds: 5,
        stale_threshold_intervals: 12,
        sweep_interval_seconds: 5,
        notify_on_started: true,
        notify_on_escalated: true,
        notify_on_recovered: true,
        cpu_warning_pct: 80,
        cpu_alert_pct: 90,
        cpu_critical_pct: 95,
        mem_warning_pct: 85,
        mem_alert_pct: 92,
        mem_critical_pct: 95,
        disk_warning_pct: 85,
        disk_alert_pct: 92,
        disk_critical_pct: 97,
        inode_warning_pct: 80,
        inode_alert_pct: 90,
        inode_critical_pct: 95,
        iowait_warning_pct: 20,
        iowait_critical_pct: 50,
        load5_warning: 4,
        load5_critical: 8,
      },
      override_rules: settingsResponseBody.override_rules,
      retention_policy: settingsResponseBody.retention_policy,
      ip_quality_settings: ipQualityUpdatePayload,
    })
  })

  it('keeps explicit false and untouched nested overrides when another field is saved', async () => {
    const settings = {
      ...settingsResponseBody,
      override_rules: {
        ...settingsResponseBody.override_rules,
        target_types: [
          {
            target_type: 'service',
            overrides: {
              incident_defaults: {
                stale_threshold_intervals: 4,
                notify_on_started: false,
                cpu_warning_pct: 70,
              },
            },
          },
        ],
      },
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settings))
      .mockResolvedValueOnce(mockJSONResponse(settings))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('高级')
    fireEvent.click(screen.getByRole('button', { name: '监控实例标签规则 1 主机采样' }))
    fireEvent.change(screen.getByLabelText('监控实例标签规则 1 主机采样频率'), { target: { value: '5m' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body)).override_rules).toEqual({
      monitoring_instance_labels: [
        {
          label: 'edge',
          overrides: {
            host_sample_frequency_tier: '5m',
            probe_frequency_defaults: { http: '1m' },
          },
        },
      ],
      target_types: settings.override_rules.target_types,
      target_labels: settings.override_rules.target_labels,
    })
  })

  it('keeps collapsed override controls out of the tab order and labels each probe kind', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)))
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('高级')

    const groups = screen.getAllByRole('button').filter((button) => button.getAttribute('aria-controls')?.endsWith('-panel'))
    expect(groups.length).toBeGreaterThan(0)
    for (const group of groups) {
      expect(group).toHaveAttribute('aria-expanded', 'false')
      expect(group).toHaveTextContent('展开')
      const panel = document.getElementById(group.getAttribute('aria-controls') ?? '')
      expect(panel?.querySelector('input, select, textarea, button')).toBeNull()
    }
    expect(screen.queryByRole('radio', { name: '监控实例标签规则 1 TCP 继承', hidden: true })).not.toBeInTheDocument()

    const probe = screen.getByRole('button', { name: '监控实例标签规则 1 探测频率，当前运行链路未应用' })
    fireEvent.click(probe)
    expect(probe).toHaveAttribute('aria-expanded', 'true')
    expect(probe).toHaveTextContent('收起')
    const probePanel = document.getElementById(probe.getAttribute('aria-controls') ?? '')
    expect(probePanel).not.toBeNull()
    expect(within(probePanel as HTMLElement).getByText('TCP')).toBeVisible()
    expect(within(probePanel as HTMLElement).getByText('HTTP')).toBeVisible()
    expect(within(probePanel as HTMLElement).getByText('TLS')).toBeVisible()
    const tcp = within(probePanel as HTMLElement).getByRole('radio', { name: '监控实例标签规则 1 TCP 继承' })
    tcp.focus()
    expect(tcp).toHaveFocus()

    fireEvent.click(probe)
    expect(probe).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('radio', { name: '监控实例标签规则 1 TCP 继承', hidden: true })).not.toBeInTheDocument()
    expect(tcp).not.toHaveFocus()
  })

  it('removes a probe object after its last kind is cleared and keeps the host override', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('高级')
    fireEvent.click(screen.getByRole('button', { name: '监控实例标签规则 1 探测频率，当前运行链路未应用' }))
    fireEvent.click(screen.getByRole('radio', { name: '监控实例标签规则 1 HTTP 继承' }))
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body)).override_rules.monitoring_instance_labels).toEqual([
      { label: 'edge', overrides: { host_sample_frequency_tier: '1m' } },
    ])
  })

  it('moves a new rule ahead of an existing one without dropping the existing override', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('高级')
    fireEvent.click(screen.getByRole('button', { name: '新增目标标签规则' }))
    fireEvent.change(screen.getByLabelText('目标标签规则 2 标签'), { target: { value: 'later' } })
    fireEvent.click(screen.getByRole('button', { name: '目标标签规则 2 探测频率' }))
    fireEvent.click(screen.getByRole('radio', { name: '目标标签规则 2 TLS 覆盖' }))
    fireEvent.click(screen.getByRole('button', { name: '上移目标标签规则 2' }))
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body)).override_rules.target_labels).toEqual([
      { label: 'later', overrides: { probe_frequency_defaults: { tls: '6h' } } },
      { label: 'external', overrides: { probe_frequency_defaults: { tls: '15m' } } },
    ])
  })

  it('keeps an invalid override draft and does not send it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('高级')
    fireEvent.change(screen.getByLabelText('监控实例标签规则 1 标签'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    expect(screen.getByLabelText('监控实例标签规则 1 标签')).toHaveAttribute('aria-invalid', 'true')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(screen.getByLabelText('监控实例标签规则 1 标签')).toHaveValue('   ')

    fireEvent.change(screen.getByLabelText('监控实例标签规则 1 标签'), { target: { value: 'edge' } })
    fireEvent.click(screen.getByRole('button', { name: '目标类型规则 1 异常判定，当前运行链路未应用' }))
    fireEvent.click(screen.getByRole('radio', { name: '目标类型规则 1 CPU 关注 覆盖' }))
    fireEvent.change(screen.getByRole('textbox', { name: '目标类型规则 1 CPU 关注' }), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('textbox', { name: '目标类型规则 1 CPU 关注' })).toHaveValue('')
  })

  it('keeps the override draft when the settings request fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(mockJSONResponse({ error: '保存失败' }, 500))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('高级')
    fireEvent.change(screen.getByLabelText('监控实例标签规则 1 标签'), { target: { value: 'edge-2' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('保存失败'))
    expect(screen.getByLabelText('监控实例标签规则 1 标签')).toHaveValue('edge-2')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('derives read-only JSON from the draft without marking it dirty', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)))
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('高级')
    expect(screen.queryByText('有未保存的修改')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /高级 JSON/ }))
    const json = screen.getByLabelText('覆盖规则 JSON')
    expect(json).toHaveTextContent('"label": "edge"')
    expect(json).not.toHaveTextContent('touched')
    expect(json).not.toHaveTextContent('inherit')
    expect(screen.queryByText('有未保存的修改')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('监控实例标签规则 1 标签'), { target: { value: 'edge-2' } })
    expect(json).toHaveTextContent('"label": "edge-2"')
    expect(screen.getByText('有未保存的修改')).toBeInTheDocument()
  })

  it('previews host and probe frequency from the unsaved draft', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)))
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('高级')
    const hostPreview = screen.getByRole('region', { name: '主机采样预览' })
    expect(within(hostPreview).getByText('5 秒')).toBeInTheDocument()
    expect(within(hostPreview).getByText('全局')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('预览监控实例标签'), { target: { value: ' edge ' } })
    expect(within(hostPreview).getByText('1 分钟')).toBeInTheDocument()
    expect(within(hostPreview).getByText('监控实例标签规则 1')).toBeInTheDocument()
    expect(within(hostPreview).getByText('1')).toBeInTheDocument()
    expect(screen.queryByText('有未保存的修改')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '监控实例标签规则 1 主机采样' }))
    fireEvent.change(screen.getByLabelText('监控实例标签规则 1 主机采样频率'), { target: { value: '15m' } })
    expect(within(hostPreview).getByText('15 分钟')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('预览探测类型'), { target: { value: 'tls' } })
    fireEvent.change(screen.getByLabelText('预览目标标签'), { target: { value: 'external' } })
    const probePreview = screen.getByRole('region', { name: '探测频率预览' })
    expect(within(probePreview).getByText('15 分钟')).toBeInTheDocument()
    expect(within(probePreview).getByText('目标标签规则 1')).toBeInTheDocument()
    expect(within(probePreview).getByText(/探测项已存频率/)).toBeInTheDocument()
  })

  it('checks override threshold order against the current global draft', async () => {
    const widened = {
      ...settingsResponseBody,
      incident_defaults: {
        ...settingsResponseBody.incident_defaults,
        cpu_warning_pct: 70,
        cpu_alert_pct: 98,
        cpu_critical_pct: 99,
      },
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(mockJSONResponse(widened))
      .mockResolvedValueOnce(mockJSONResponse({
        ...widened,
        override_rules: {
          ...widened.override_rules,
          target_types: [{
            target_type: 'service',
            overrides: { incident_defaults: { stale_threshold_intervals: 4, cpu_warning_pct: 96 } },
          }],
        },
      }))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('CPU 关注阈值'), { target: { value: '70' } })
    fireEvent.change(screen.getByLabelText('CPU 告警阈值'), { target: { value: '98' } })
    fireEvent.change(screen.getByLabelText('CPU 严重阈值'), { target: { value: '99' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    switchTab('高级')
    fireEvent.click(screen.getByRole('button', { name: '目标类型规则 1 异常判定，当前运行链路未应用' }))
    fireEvent.click(screen.getByRole('radio', { name: '目标类型规则 1 CPU 关注 覆盖' }))
    fireEvent.change(screen.getByRole('textbox', { name: '目标类型规则 1 CPU 关注' }), { target: { value: '96' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    const saved = JSON.parse(String(fetchMock.mock.calls[2]?.[1]?.body))
    expect(saved.incident_defaults).toMatchObject({ cpu_warning_pct: 70, cpu_alert_pct: 98, cpu_critical_pct: 99 })
    expect(saved.override_rules.target_types[0].overrides.incident_defaults).toEqual({
      stale_threshold_intervals: 4,
      cpu_warning_pct: 96,
    })

    fireEvent.change(screen.getByRole('textbox', { name: '目标类型规则 1 CPU 关注' }), { target: { value: '99' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(screen.getByRole('textbox', { name: '目标类型规则 1 CPU 关注' })).toHaveValue('99')
  })

  it('accepts a future label when suggestions fail and only suggests target labels', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/monitoring-instances')) return Promise.resolve(mockJSONResponse({ error: 'no' }, 500))
      if (url.includes('/api/targets')) {
        return Promise.resolve(mockJSONResponse([
          { labels: ['from-target'], execution_monitoring_instance_labels: ['executor-only'] },
        ]))
      }
      return Promise.resolve(mockJSONResponse(settingsResponseBody))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('高级')
    fireEvent.focus(screen.getByLabelText('监控实例标签规则 1 标签'))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/monitoring-instances'),
      expect.anything(),
    ))
    expect(screen.queryByRole('button', { name: 'executor-only' })).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('监控实例标签规则 1 标签'), { target: { value: 'future-label' } })
    expect(screen.getByLabelText('监控实例标签规则 1 标签')).toHaveValue('future-label')

    fireEvent.focus(screen.getByLabelText('目标标签规则 1 标签'))
    expect(await screen.findByRole('button', { name: 'from-target' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'executor-only' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'from-target' }))
    expect(screen.getByLabelText('目标标签规则 1 标签')).toHaveValue('from-target')
  })

  it('rejects malformed integer text instead of silently coercing it', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('心跳间隔秒数'), {
      target: { value: '30abc' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    expect(screen.getByText('心跳间隔必须为正整数。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fireEvent.change(screen.getByLabelText('心跳间隔秒数'), {
      target: { value: '1.5' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    expect(screen.getByText('心跳间隔必须为正整数。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('does not persist notification channel draft fields when the add-channel modal is dismissed', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: telegramResponse({
            mode: 'inherit',
            source: 'default',
            active: false,
            chat_id: '',
            token_present: false,
            token_masked_summary: '',
            deployment: { configured: false, chat_id: '', token_present: false },
          }),
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: telegramResponse({
            mode: 'inherit',
            source: 'default',
            active: false,
            chat_id: '',
            token_present: false,
            token_masked_summary: '',
            deployment: { configured: false, chat_id: '', token_present: false },
          }),
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument(),
    )

    switchTab('通知')
    fireEvent.click(screen.getByRole('button', { name: '+ 新增通知渠道' }))
    fireEvent.click(screen.getByRole('button', { name: 'Telegram' }))
    fireEvent.change(screen.getByLabelText('新的 Telegram Bot Token'), {
      target: { value: 'draft-token' },
    })
    fireEvent.change(screen.getByLabelText('Telegram Chat ID'), {
      target: { value: 'draft-chat' },
    })
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const dismissed = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(dismissed).not.toHaveProperty('telegram')
    expect(JSON.stringify(dismissed)).not.toContain('draft-token')
    expect(JSON.stringify(dismissed)).not.toContain('draft-chat')
  })

  it('hides user management and does not request the directory when the session cannot manage access', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new Error('settings must not fetch for a forbidden access tab')))
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage('/settings?tab=access')

    expect(await screen.findByRole('heading', { name: '无权管理用户与权限' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: '用户与权限' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { selected: true })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存设置' })).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('keeps a failed capability read closed and retries only the session', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new Error('a failed capability read must not open management')))
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage('/settings?tab=access', { fail: true })

    expect(await screen.findByRole('heading', { name: '能力读取失败' })).toBeInTheDocument()
    expect(screen.getByText('暂时无法确认管理能力，不会打开用户与权限目录。')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '账号' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    await waitFor(() => expect(authClient.me).toHaveBeenCalledTimes(2))
    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.getByRole('heading', { name: '能力读取失败' })).toBeInTheDocument()
  })

  it('opens the access directory without loading system settings when management is allowed', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/admin/users') return Promise.resolve(mockJSONResponse({ items: [] }))
      if (url === '/api/admin/record-access-groups') return Promise.resolve(mockJSONResponse({ items: [] }))
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${url}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage('/settings?tab=access', { user: sessionUser(true) })

    expect(await screen.findByRole('heading', { name: '账号' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '用户与权限' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.queryByRole('button', { name: '保存设置' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('当前监控实例主机样本频率')).not.toBeInTheDocument()
    await waitFor(() => {
      expect(fetchMock.mock.calls.map(([url]) => url)).toEqual(expect.arrayContaining([
        '/api/admin/users',
        '/api/admin/record-access-groups',
      ]))
    })
    expect(fetchMock.mock.calls.some(([url]) => url === '/api/settings' || url === '/api/settings/deployment' || url === '/api/record-access-groups/mine')).toBe(false)
  })

  it('confirms a dirty system form before opening user management', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/settings') return Promise.resolve(mockJSONResponse(settingsResponseBody))
      if (url === '/api/admin/users' || url === '/api/admin/record-access-groups') return Promise.resolve(mockJSONResponse({ items: [] }))
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${url}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)
    const adminCalls = () => fetchMock.mock.calls.filter(([url]) => String(url).startsWith('/api/admin'))

    renderSettingsPage('/settings', { user: sessionUser(true) })
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('原始层保留天数'), { target: { value: '45' } })
    switchTab('用户与权限')

    expect(screen.getByRole('button', { name: '不保存' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '监控策略' })).toHaveAttribute('aria-selected', 'true')
    expect(adminCalls()).toEqual([])

    fireEvent.click(screen.getByRole('button', { name: '不保存' }))
    await waitFor(() => expect(screen.getByRole('tab', { name: '用户与权限' })).toHaveAttribute('aria-selected', 'true'))
    await waitFor(() => expect(adminCalls().map(([url]) => url)).toEqual(expect.arrayContaining([
      '/api/admin/users',
      '/api/admin/record-access-groups',
    ])))
  })

  it('confirms telegram restore as a draft and submits inherit only after save', async () => {
    const initial = {
      ...settingsResponseBody,
      telegram: telegramResponse({ mode: 'override', source: 'database' }),
    }
    const restored = {
      ...settingsResponseBody,
      telegram: telegramResponse(),
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse(initial))
      .mockResolvedValueOnce(mockJSONResponse(restored))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('通知')
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Telegram 通知设置' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '恢复部署配置' }))
    expect(screen.getByRole('alertdialog', { name: '恢复部署配置' })).toHaveTextContent('保存后清除页面覆盖，重新使用部署配置。')
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.getByRole('button', { name: '页面覆盖' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText(/当前生效：页面设置，已启用/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '恢复部署配置' }))
    fireEvent.click(screen.getByRole('button', { name: '确认恢复' }))
    expect(screen.getByRole('button', { name: '继承部署配置' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText(/当前生效：页面设置，已启用/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const saved = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(saved.telegram).toEqual({ mode: 'inherit' })
    expect(await screen.findByText(/当前生效：部署配置，已启用/)).toBeInTheDocument()
  })

  it('keeps the telegram draft and effective summary when saving fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(mockJSONResponse({ error: 'invalid input' }, 400))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    await expandTelegram()
    fireEvent.change(screen.getByLabelText('Telegram Chat ID'), { target: { value: 'kept-chat' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('invalid input'))
    expect(screen.getByLabelText('Telegram Chat ID')).toHaveValue('kept-chat')
    expect(screen.getByText(/当前生效：部署配置，已启用/)).toBeInTheDocument()
    expect(screen.queryByText('设置已保存')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('does not let an older save replace a newer telegram draft', async () => {
    let resolveSave: ((value: Response) => void) | undefined
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockImplementationOnce(() => new Promise<Response>((resolve) => { resolveSave = resolve }))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    await expandTelegram()
    fireEvent.change(screen.getByLabelText('Telegram Chat ID'), { target: { value: 'mid-chat' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    fireEvent.change(screen.getByLabelText('Telegram Chat ID'), { target: { value: 'newer-chat' } })
    resolveSave?.(mockJSONResponse({
      ...settingsResponseBody,
      telegram: telegramResponse({ mode: 'override', source: 'database', chat_id: 'mid-chat' }),
    }))
    await waitFor(() => expect(screen.getByLabelText('Telegram Chat ID')).toHaveValue('newer-chat'))
    expect(screen.queryByText('设置已保存')).not.toBeInTheDocument()
  })

  it('omits telegram and sweep ownership on the next save after a concurrent disable and sweep override when only a newer unrelated field changed', async () => {
    let resolveSave: ((value: Response) => void) | undefined
    const concurrent = {
      ...settingsResponseBody,
      telegram: telegramResponse({
        mode: 'disabled',
        source: 'database',
        active: false,
        chat_id: '',
        token_present: false,
        token_masked_summary: '',
      }),
      incident_defaults: { ...settingsResponseBody.incident_defaults, sweep_interval_seconds: 17 },
      incident_sweep: { mode: 'override', source: 'database', deployment_seconds: 60 },
      retention_policy: { raw_layer_days: 45, aggregate_layer_days: 365 },
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockImplementationOnce(() => new Promise<Response>((resolve) => { resolveSave = resolve }))
      .mockResolvedValueOnce(mockJSONResponse(concurrent))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('原始层保留天数'), { target: { value: '45' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const submitted = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(submitted).not.toHaveProperty('telegram')
    expect(submitted).not.toHaveProperty('incident_sweep_mode')
    expect(submitted.retention_policy.raw_layer_days).toBe(45)
    expect(submitted.incident_defaults.heartbeat_interval_seconds).toBe(5)
    expect(submitted.incident_defaults.sweep_interval_seconds).toBe(5)

    fireEvent.change(screen.getByLabelText('心跳间隔秒数'), { target: { value: '9' } })
    resolveSave?.(mockJSONResponse(concurrent))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '保存设置' })).toBeEnabled()
      expect(screen.getByLabelText('心跳间隔秒数')).toHaveValue('9')
      expect(screen.getByLabelText('扫描间隔秒数')).toHaveValue('17')
    })
    expect(screen.getByLabelText('扫描间隔秒数')).not.toHaveAttribute('readonly')
    expect(screen.getByRole('button', { name: '页面覆盖' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    const saved = JSON.parse(String(fetchMock.mock.calls[2]?.[1]?.body))
    expect(saved).not.toHaveProperty('telegram')
    expect(saved).not.toHaveProperty('incident_sweep_mode')
    expect(saved.incident_defaults.heartbeat_interval_seconds).toBe(9)
    expect(saved.incident_defaults.sweep_interval_seconds).toBe(17)
    expect(saved.retention_policy).toEqual({ raw_layer_days: 45, aggregate_layer_days: 365 })
  })

  it('hides telegram after a saved restore when deployment is not configured', async () => {
    const initial = {
      ...settingsResponseBody,
      telegram: telegramResponse({ mode: 'override', source: 'database' }),
    }
    const hidden = {
      ...settingsResponseBody,
      telegram: telegramResponse({
        mode: 'inherit',
        source: 'default',
        active: false,
        chat_id: '',
        token_present: false,
        token_masked_summary: '',
        deployment: { configured: false, chat_id: '', token_present: false },
      }),
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse(initial))
      .mockResolvedValueOnce(mockJSONResponse(hidden))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('通知')
    fireEvent.click(screen.getByRole('button', { name: '恢复部署配置' }))
    fireEvent.click(screen.getByRole('button', { name: '确认恢复' }))
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Telegram 通知设置' })).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: '+ 新增通知渠道' })).toBeInTheDocument()
  })

  it('keeps inherited sweep seconds read-only until the page overrides them', async () => {
    const inherited = {
      ...settingsResponseBody,
      incident_defaults: { ...settingsResponseBody.incident_defaults, sweep_interval_seconds: 60 },
      incident_sweep: { mode: 'inherit', source: 'deployment', deployment_seconds: 60 },
    }
    const overridden = {
      ...inherited,
      incident_defaults: { ...inherited.incident_defaults, sweep_interval_seconds: 17 },
      incident_sweep: { mode: 'override', source: 'database', deployment_seconds: 60 },
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse(inherited))
      .mockResolvedValueOnce(mockJSONResponse(overridden))
      .mockResolvedValueOnce(mockJSONResponse(inherited))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')
    const sweep = screen.getByLabelText('扫描间隔秒数')
    expect(sweep).toHaveValue('60')
    expect(sweep).toHaveAttribute('readonly')
    expect(screen.getByText('当前来源：部署配置')).toBeInTheDocument()
    fireEvent.change(sweep, { target: { value: '17' } })
    expect(sweep).toHaveValue('60')

    fireEvent.click(screen.getByRole('button', { name: '页面覆盖' }))
    fireEvent.change(screen.getByLabelText('扫描间隔秒数'), { target: { value: '17' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toMatchObject({
      incident_sweep_mode: 'override',
      incident_defaults: { sweep_interval_seconds: 17 },
    })
    await waitFor(() => expect(screen.getByText('当前来源：页面设置')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '恢复部署配置' }))
    expect(screen.getByLabelText('扫描间隔秒数')).toHaveValue('60')
    expect(screen.getByLabelText('扫描间隔秒数')).toHaveAttribute('readonly')
    expect(screen.getByText('当前来源：页面设置')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    const restored = JSON.parse(String(fetchMock.mock.calls[2]?.[1]?.body))
    expect(restored.incident_sweep_mode).toBe('inherit')
    expect(restored).not.toHaveProperty('telegram')
  })

  it('keeps a saved sweep override of 17 when inherit only shows the deployment interval', async () => {
    const savedOverride = {
      ...settingsResponseBody,
      incident_defaults: { ...settingsResponseBody.incident_defaults, sweep_interval_seconds: 17 },
      incident_sweep: { mode: 'override', source: 'database', deployment_seconds: 60 },
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse(savedOverride))
      .mockResolvedValueOnce(mockJSONResponse(savedOverride))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')
    expect(screen.getByLabelText('扫描间隔秒数')).toHaveValue('17')
    expect(screen.getByLabelText('扫描间隔秒数')).not.toHaveAttribute('readonly')

    const sweepMode = screen.getByRole('group', { name: '扫描间隔配置方式' })
    fireEvent.click(within(sweepMode).getByRole('button', { name: '继承部署' }))
    expect(screen.getByLabelText('扫描间隔秒数')).toHaveValue('60')
    expect(screen.getByLabelText('扫描间隔秒数')).toHaveAttribute('readonly')
    expect(within(sweepMode).getByRole('button', { name: '继承部署' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(within(sweepMode).getByRole('button', { name: '页面覆盖' }))
    expect(screen.getByLabelText('扫描间隔秒数')).toHaveValue('17')
    expect(screen.getByLabelText('扫描间隔秒数')).not.toHaveAttribute('readonly')

    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const saved = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(saved).not.toHaveProperty('incident_sweep_mode')
    expect(saved).not.toHaveProperty('telegram')
    expect(saved.incident_defaults.sweep_interval_seconds).toBe(17)
  })

  it.each(['', '0'])('restores inherited sweep while preserving an invalid override draft (%j)', async (invalidDraft) => {
    const savedOverride = {
      ...settingsResponseBody,
      incident_defaults: { ...settingsResponseBody.incident_defaults, sweep_interval_seconds: 17 },
      incident_sweep: { mode: 'override', source: 'database', deployment_seconds: 60 },
    }
    const inherited = {
      ...savedOverride,
      incident_defaults: { ...savedOverride.incident_defaults, sweep_interval_seconds: 60 },
      incident_sweep: { mode: 'inherit', source: 'deployment', deployment_seconds: 60 },
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(mockJSONResponse(savedOverride))
      .mockResolvedValueOnce(mockJSONResponse(inherited))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage()
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('扫描间隔秒数'), { target: { value: invalidDraft } })
    fireEvent.click(screen.getByRole('button', { name: '恢复部署配置' }))
    expect(screen.getByLabelText('扫描间隔秒数')).toHaveValue('60')
    expect(screen.getByLabelText('扫描间隔秒数')).toHaveAttribute('readonly')
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    await waitFor(() => expect(screen.getByText('设置已保存')).toBeInTheDocument())
    const saved = JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))
    expect(saved.incident_sweep_mode).toBe('inherit')
    expect(saved.incident_defaults.sweep_interval_seconds).toBe(60)
    expect(screen.getByText('当前来源：部署配置')).toBeInTheDocument()
  })

  const deploymentSnapshot = {
    items: [
      {
        key: 'HOUFENG_HTTP_ADDR',
        group: 'process',
        source: 'environment',
        value: '127.0.0.1:18080',
        configured: true,
        sensitive: false,
        application: 'startup',
      },
      {
        key: 'HOUFENG_DATABASE_URL',
        group: 'database',
        source: 'environment',
        value: 'postgres://secret-user:secret-pass@db/houfeng',
        configured: true,
        sensitive: true,
        application: 'startup',
      },
      {
        key: 'HOUFENG_TELEGRAM_BOT_TOKEN',
        group: 'notifications',
        source: 'file',
        value: null,
        configured: true,
        sensitive: true,
        application: 'fallback',
      },
    ],
  }

  it('hides deployment configuration from a session that cannot manage access', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new Error('deployment must not fetch without management')))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage('/settings?tab=deployment')
    expect(await screen.findByRole('heading', { name: '无权查看部署配置' })).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: '部署配置' })).not.toBeInTheDocument()
    expect(screen.queryByText('postgres://secret-user:secret-pass@db/houfeng')).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('keeps a failed deployment capability read closed', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new Error('deployment capability failure must not open the snapshot')))
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage('/settings?tab=deployment', { fail: true })
    expect(await screen.findByRole('heading', { name: '能力读取失败' })).toBeInTheDocument()
    expect(screen.getByText('暂时无法确认管理能力，不会打开部署配置。')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('shows the startup deployment snapshot without a save action or secret values', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/settings/deployment') return Promise.resolve(mockJSONResponse(deploymentSnapshot))
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${url}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage('/settings?tab=deployment', { user: sessionUser(true) })
    expect(await screen.findByRole('heading', { name: '进程' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '部署配置' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('监听地址')).toBeInTheDocument()
    expect(screen.getByText('127.0.0.1:18080')).toBeInTheDocument()
    expect(screen.getByText('HOUFENG_HTTP_ADDR')).toBeInTheDocument()
    expect(screen.getAllByText('环境变量').length).toBeGreaterThan(0)
    expect(screen.getByText('数据库连接')).toBeInTheDocument()
    expect(screen.queryByText('postgres://secret-user:secret-pass@db/houfeng')).not.toBeInTheDocument()
    expect(screen.getByText(/页面覆盖或停用后/)).toBeInTheDocument()
    expect(screen.getByText(/需要重启才会反映到这里/)).toBeInTheDocument()
    expect(screen.getByText('部署编排参数请在部署文件查看。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存设置' })).not.toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([url]) => url === '/api/settings')).toBe(false)
  })

  it('shows a forbidden deployment snapshot when the server rejects the session', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/settings/deployment') return Promise.resolve(mockJSONResponse({ error: 'forbidden' }, 403))
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${url}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage('/settings?tab=deployment', { user: sessionUser(true) })
    expect(await screen.findByRole('heading', { name: '无权查看部署配置' })).toBeInTheDocument()
    expect(screen.queryByText('postgres://secret-user:secret-pass@db/houfeng')).not.toBeInTheDocument()
    expect(screen.queryByText('forbidden')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存设置' })).not.toBeInTheDocument()
  })

  it('retries a failed deployment snapshot without treating the error as empty configuration', async () => {
    let failed = true
    const fetchMock = vi.fn((url: string) => {
      if (url !== '/api/settings/deployment') return Promise.resolve(mockJSONResponse({ error: `unexpected ${url}` }, 500))
      if (failed) return Promise.resolve(mockJSONResponse({ error: 'database password leaked' }, 500))
      return Promise.resolve(mockJSONResponse(deploymentSnapshot))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage('/settings?tab=deployment', { user: sessionUser(true) })
    expect(await screen.findByRole('heading', { name: '部署配置不可用' })).toBeInTheDocument()
    expect(screen.getByText('部署配置暂时无法读取。')).toBeInTheDocument()
    expect(screen.queryByText('database password leaked')).not.toBeInTheDocument()
    expect(screen.queryByText('监听地址')).not.toBeInTheDocument()
    failed = false
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('监听地址')).toBeInTheDocument()
  })

  it('asks before leaving unsaved system settings for the deployment tab', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/settings') return Promise.resolve(mockJSONResponse(settingsResponseBody))
      if (url === '/api/settings/deployment') return Promise.resolve(mockJSONResponse(deploymentSnapshot))
      return Promise.resolve(mockJSONResponse({ error: `unexpected ${url}` }, 500))
    })
    vi.stubGlobal('fetch', fetchMock)
    renderSettingsPage('/settings', { user: sessionUser(true) })
    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())
    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('原始层保留天数'), { target: { value: '45' } })
    switchTab('部署配置')
    expect(screen.getByRole('button', { name: '不保存' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '监控策略' })).toHaveAttribute('aria-selected', 'true')
    expect(fetchMock.mock.calls.some(([url]) => url === '/api/settings/deployment')).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: '不保存' }))
    await waitFor(() => expect(screen.getByRole('tab', { name: '部署配置' })).toHaveAttribute('aria-selected', 'true'))
    expect(await screen.findByText('监听地址')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '保存设置' })).not.toBeInTheDocument()
  })
})
