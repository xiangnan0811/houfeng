import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

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
  host_sample_frequency_tier: '5m',
  probe_frequency_defaults: {
    tcp: '5m',
    http: '1m',
    tls: '15m',
  },
  incident_defaults: {
    heartbeat_interval_seconds: 30,
    stale_threshold_intervals: 3,
    sweep_interval_seconds: 60,
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
  },
  override_rules: {
    node_labels: [
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

  it('loads persisted settings into the required sections and keeps Telegram and retention copy truthful', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)),
    )

    render(<SettingsPage />)

    expect(screen.getByText('正在加载设置…')).toBeInTheDocument()

    await waitFor(() => expect(screen.getByRole('heading', { name: '设置 / Settings' })).toBeInTheDocument())

    expect(screen.getByText('设置', { selector: '.page-panel__eyebrow' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Telegram 通知设置' })).toBeInTheDocument()
    expect(screen.getByText('频率档位')).toBeInTheDocument()
    expect(screen.getByText('全局默认')).toBeInTheDocument()
    expect(screen.getByText('覆盖规则')).toBeInTheDocument()
    expect(screen.getByText('保留策略')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '默认频率档位' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '全局默认规则' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '少量覆盖规则' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '数据保留策略' })).toBeInTheDocument()

    expect(screen.getByLabelText('Telegram Chat ID')).toHaveValue('chat-id')
    expect(screen.getByLabelText('当前节点主机样本频率')).toHaveValue('5m')
    expect((screen.getByLabelText('节点标签覆盖规则 JSON') as HTMLTextAreaElement).value).toContain(
      '"label": "edge"',
    )

    expect(
      screen.getByText(
        (_, node) =>
          node?.textContent === '已配置 Telegram Bot Token：****************oken',
      ),
    ).toBeInTheDocument()
    expect(screen.getByText('****************oken')).toBeInTheDocument()
    expect(screen.queryByText('bot-token')).not.toBeInTheDocument()
    expect(screen.getByLabelText('使用持久化 Telegram 配置接管运行中的通知器')).not.toBeChecked()
    expect(screen.getByText('当前仅保存 Telegram 持久化配置，尚未驱动正在运行的通知器。')).toBeInTheDocument()
    expect(
      screen.getByText('当前节点主机样本默认频率已接入实时规划链；Probe 默认频率仍仅作为持久化策略保存。'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('heartbeat/sweep 时间参数与通知时机开关已接入实时异常与通知链路。'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        '仅保留节点标签、目标类型、目标标签三类结构化覆盖，不扩展为通用规则引擎。当前频率相关覆盖已接入实时规划链；异常默认覆盖仍仅作为持久化策略保存。',
      ),
    ).toBeInTheDocument()
    expect(
      screen.getByText('中心后台会按这些窗口自动清理原始观测、事件和通知记录，并维护日级聚合数据作为后续趋势与摘要基础。'),
    ).toBeInTheDocument()
    expect(
      screen.queryByText('当前仅保存保留策略，尚未自动执行清理或聚合任务。'),
    ).not.toBeInTheDocument()
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

    render(<SettingsPage />)

    await waitFor(() => expect(screen.getByRole('heading', { name: '设置 / Settings' })).toBeInTheDocument())

    expect(screen.getByLabelText('使用持久化 Telegram 配置接管运行中的通知器')).toBeChecked()
    expect(screen.getByText('当前持久化配置正在接管通知路径，并已显式停用 Telegram 投递。')).toBeInTheDocument()
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

    render(<SettingsPage />)

    await waitFor(() => expect(screen.getByRole('heading', { name: '设置 / Settings' })).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('当前节点主机样本频率'), {
      target: { value: '1m' },
    })
    fireEvent.change(screen.getByLabelText('原始层保留天数'), {
      target: { value: '14' },
    })

    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(
      () =>
        expect(
          screen.getByText('设置已保存。保留策略会由中心后台执行；仍仅持久化保存的策略不会立即影响运行时。'),
        ).toBeInTheDocument(),
    )

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
      telegram: {
        chat_id: 'chat-id',
        runtime_managed: false,
      },
      host_sample_frequency_tier: '1m',
      probe_frequency_defaults: {
        tcp: '5m',
        http: '1m',
        tls: '15m',
      },
      incident_defaults: {
        heartbeat_interval_seconds: 30,
        stale_threshold_intervals: 3,
        sweep_interval_seconds: 60,
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

    render(<SettingsPage />)

    await waitFor(() => expect(screen.getByRole('heading', { name: '设置 / Settings' })).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('使用持久化 Telegram 配置接管运行中的通知器'))
    fireEvent.change(screen.getByLabelText('新的 Telegram Bot Token'), {
      target: { value: 'replacement-token' },
    })
    fireEvent.change(screen.getByLabelText('当前节点主机样本频率'), {
      target: { value: '1m' },
    })
    fireEvent.change(screen.getByLabelText('原始层保留天数'), {
      target: { value: '14' },
    })

    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(
      () =>
        expect(
          screen.getByText('设置已保存。保留策略会由中心后台执行；仍仅持久化保存的策略不会立即影响运行时。'),
        ).toBeInTheDocument(),
    )

    expect(fetchMock).toHaveBeenCalledTimes(2)
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
      host_sample_frequency_tier: '1m',
      probe_frequency_defaults: {
        tcp: '5m',
        http: '1m',
        tls: '15m',
      },
      incident_defaults: {
        heartbeat_interval_seconds: 30,
        stale_threshold_intervals: 3,
        sweep_interval_seconds: 60,
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

    render(<SettingsPage />)

    await waitFor(() => expect(screen.getByRole('heading', { name: '设置 / Settings' })).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('使用持久化 Telegram 配置接管运行中的通知器'))
    fireEvent.change(screen.getByLabelText('Telegram Chat ID'), {
      target: { value: 'new-chat-id' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(
      () =>
        expect(
          screen.getByText('设置已保存。保留策略会由中心后台执行；仍仅持久化保存的策略不会立即影响运行时。'),
        ).toBeInTheDocument(),
    )

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
      telegram: {
        chat_id: 'new-chat-id',
        runtime_managed: true,
      },
      host_sample_frequency_tier: '5m',
      probe_frequency_defaults: {
        tcp: '5m',
        http: '1m',
        tls: '15m',
      },
      incident_defaults: {
        heartbeat_interval_seconds: 30,
        stale_threshold_intervals: 3,
        sweep_interval_seconds: 60,
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
      },
      override_rules: settingsResponseBody.override_rules,
      retention_policy: settingsResponseBody.retention_policy,
    })
  })

  it('renders a collapsible JSON preview when override textarea contains valid JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)),
    )

    render(<SettingsPage />)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '设置 / Settings' })).toBeInTheDocument(),
    )

    // Three OverrideTextarea instances (node labels / target types / target labels)
    // each load with valid JSON from the persisted settings, so each renders a 预览 summary.
    const previewSummaries = screen.getAllByText('预览')
    expect(previewSummaries.length).toBe(3)

    // The node-label preview should contain the formatted JSON of the persisted rule.
    expect(
      screen.getAllByText(
        (_, node) => node?.tagName === 'CODE' && (node.textContent ?? '').includes('"label": "edge"'),
      ).length,
    ).toBeGreaterThan(0)
  })

  it('hides the JSON preview when the override textarea contains invalid JSON', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse(settingsResponseBody)),
    )

    render(<SettingsPage />)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '设置 / Settings' })).toBeInTheDocument(),
    )

    // Initially three valid previews appear.
    expect(screen.getAllByText('预览').length).toBe(3)

    // Make the node-label textarea invalid JSON; its preview should disappear.
    fireEvent.change(screen.getByLabelText('节点标签覆盖规则 JSON'), {
      target: { value: 'not valid json {' },
    })
    expect(screen.getAllByText('预览').length).toBe(2)

    // Clearing the textarea should also drop its preview (empty string is treated as no preview).
    fireEvent.change(screen.getByLabelText('目标类型覆盖规则 JSON'), {
      target: { value: '   ' },
    })
    expect(screen.getAllByText('预览').length).toBe(1)
  })

  it('rejects malformed integer text instead of silently coercing it', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(settingsResponseBody))
    vi.stubGlobal('fetch', fetchMock)

    render(<SettingsPage />)

    await waitFor(() => expect(screen.getByRole('heading', { name: '设置 / Settings' })).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('心跳间隔秒数'), {
      target: { value: '30abc' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    expect(screen.getByText('心跳间隔秒数必须为正整数。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    fireEvent.change(screen.getByLabelText('心跳间隔秒数'), {
      target: { value: '1.5' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    expect(screen.getByText('心跳间隔秒数必须为正整数。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
