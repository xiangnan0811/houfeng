import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ThemeProvider } from '../lib/theme-context'
import { SettingsPage } from './SettingsPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

const settingsResponseBody = {
  telegram: {
    chat_id: 'chat-id',
    token_present: true,
    token_masked_summary: '****************oken',
    runtime_managed: false,
    runtime_apply_active: false,
  },
  feishu: {
    enabled: false,
    webhook_url: '',
  },
  host_sample_frequency_tier: '5s',
  probe_frequency_defaults: {
    tcp: '5s',
    http: '5s',
    tls: '6h',
  },
  incident_defaults: {
    heartbeat_interval_seconds: 5,
    stale_threshold_intervals: 3,
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
    raw_layer_days: 7,
    aggregate_layer_days: 30,
    event_layer_days: 90,
    notification_layer_days: 180,
  },
}

describe('SettingsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  function renderSettingsPage() {
    return render(
      <ThemeProvider>
        <SettingsPage />
      </ThemeProvider>,
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

  it('loads persisted settings into the required sections and keeps Telegram and retention copy truthful', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)),
    )

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    // Switch to monitoring tab to check frequency
    switchTab('监控策略')
    expect(screen.getByLabelText('当前监控实例主机样本频率')).toHaveValue('5s')

    // Switch to notification tab to check Telegram
    await expandTelegram()

    expect(screen.getByLabelText('Telegram Chat ID')).toHaveValue('chat-id')
    expect(
      screen.getByText(
        (_, node) =>
          node?.textContent === '已配置 Telegram Bot Token：****************oken',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: '运行时接管' })).not.toBeChecked()

    // Switch to advanced tab to check override rules
    switchTab('高级')
    expect((screen.getByLabelText('监控实例标签覆盖规则 JSON') as HTMLTextAreaElement).value).toContain(
      '"label": "edge"',
    )
  })

  it('keeps the runtime management toggle checked when persisted settings explicitly disable Telegram delivery', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: {
            chat_id: '',
            token_present: false,
            runtime_managed: true,
            runtime_apply_active: false,
          },
        }),
      ),
    )

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    await expandTelegram()
  })

  it('saves unrelated settings without requiring Telegram token re-entry and omits bot_token from the payload', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsResponseBody,
          host_sample_frequency_tier: '1m',
          retention_policy: {
            ...settingsResponseBody.retention_policy,
            raw_layer_days: 14,
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
      target: { value: '14' },
    })

    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
      telegram: {
        chat_id: 'chat-id',
        runtime_managed: false,
      },
      feishu: {
        enabled: false,
        webhook_url: '',
      },
      host_sample_frequency_tier: '1m',
      probe_frequency_defaults: {
        tcp: '5s',
        http: '5s',
        tls: '6h',
      },
      incident_defaults: {
        heartbeat_interval_seconds: 5,
        stale_threshold_intervals: 3,
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
        raw_layer_days: 14,
        aggregate_layer_days: 30,
        event_layer_days: 90,
        notification_layer_days: 180,
      },
    })
  })

  it('saves updated settings with a replacement Telegram token and refreshed defaults', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: {
            ...settingsResponseBody.telegram,
            runtime_managed: true,
            token_masked_summary: '***************oken',
            runtime_apply_active: true,
          },
          host_sample_frequency_tier: '1m',
          retention_policy: {
            ...settingsResponseBody.retention_policy,
            raw_layer_days: 14,
          },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    await expandTelegram()
    fireEvent.click(screen.getByRole('switch', { name: '运行时接管' }))
    fireEvent.change(screen.getByLabelText('新的 Telegram Bot Token'), {
      target: { value: 'replacement-token' },
    })
    switchTab('监控策略')
    fireEvent.change(screen.getByLabelText('当前监控实例主机样本频率'), {
      target: { value: '1m' },
    })
    fireEvent.change(screen.getByLabelText('原始层保留天数'), {
      target: { value: '14' },
    })

    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/settings')
    expect(fetchMock.mock.calls[1]?.[1]).toMatchObject({
      method: 'PUT',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
    })
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
      telegram: {
        bot_token: 'replacement-token',
        chat_id: 'chat-id',
        runtime_managed: true,
      },
      feishu: {
        enabled: false,
        webhook_url: '',
      },
      host_sample_frequency_tier: '1m',
      probe_frequency_defaults: {
        tcp: '5s',
        http: '5s',
        tls: '6h',
      },
      incident_defaults: {
        heartbeat_interval_seconds: 5,
        stale_threshold_intervals: 3,
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
        raw_layer_days: 14,
        aggregate_layer_days: 30,
        event_layer_days: 90,
        notification_layer_days: 180,
      },
    })
  })

  it('saves a Telegram chat-id-only update when a token is already stored', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: {
            ...settingsResponseBody.telegram,
            runtime_managed: true,
            chat_id: 'new-chat-id',
            runtime_apply_active: true,
          },
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    renderSettingsPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument())

    await expandTelegram()
    fireEvent.click(screen.getByRole('switch', { name: '运行时接管' }))
    fireEvent.change(screen.getByLabelText('Telegram Chat ID'), {
      target: { value: 'new-chat-id' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
      telegram: {
        chat_id: 'new-chat-id',
        runtime_managed: true,
      },
      feishu: {
        enabled: false,
        webhook_url: '',
      },
      host_sample_frequency_tier: '5s',
      probe_frequency_defaults: {
        tcp: '5s',
        http: '5s',
        tls: '6h',
      },
      incident_defaults: {
        heartbeat_interval_seconds: 5,
        stale_threshold_intervals: 3,
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
    })
  })

  it('shows inline validation errors when override textarea contains invalid JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)),
    )

    renderSettingsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument(),
    )

    switchTab('高级')

    // Valid JSON should not show errors
    expect(screen.queryByText('JSON 格式无效')).not.toBeInTheDocument()

    // Invalid JSON shows error
    fireEvent.change(screen.getByLabelText('监控实例标签覆盖规则 JSON'), {
      target: { value: 'not valid json {' },
    })
    expect(screen.getByText('JSON 格式无效')).toBeInTheDocument()

    // Non-array JSON shows specific error
    fireEvent.change(screen.getByLabelText('目标类型覆盖规则 JSON'), {
      target: { value: '{}' },
    })
    expect(screen.getByText('必须是 JSON 数组')).toBeInTheDocument()
  })

  it('formats JSON when format button is clicked', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)),
    )

    renderSettingsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '系统设置' })).toBeInTheDocument(),
    )

    switchTab('高级')

    const textarea = screen.getByLabelText('监控实例标签覆盖规则 JSON') as HTMLTextAreaElement
    // The initial value is already pretty-printed, compact it first
    fireEvent.change(textarea, { target: { value: '[{"label":"edge","overrides":{}}]' } })

    // Click the first format button
    const formatButtons = screen.getAllByRole('button', { name: '格式化' })
    fireEvent.click(formatButtons[0])

    expect(textarea.value).toContain('  "label": "edge"')
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
          telegram: {
            chat_id: '',
            token_present: false,
            token_masked_summary: '',
            runtime_managed: false,
            runtime_apply_active: false,
          },
        }),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          ...settingsResponseBody,
          telegram: {
            chat_id: '',
            token_present: false,
            token_masked_summary: '',
            runtime_managed: false,
            runtime_apply_active: false,
          },
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
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toMatchObject({
      telegram: {
        chat_id: '',
        runtime_managed: false,
      },
    })
  })
})
