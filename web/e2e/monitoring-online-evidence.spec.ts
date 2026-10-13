import { test, expect } from './fixtures'
import { apiRouteKey, type ApiFixtureProfile } from './fixtures/contracts'
import { monitoringInstanceDetailProfile } from './fixtures/profiles'

const INSTANCE = 'mi_001'
const START = Date.parse('2026-08-20T09:00:00Z')
const iso = (ms: number) => new Date(ms).toISOString()

function sample(at: number, cpu = 12) {
  return {
    monitoring_instance_id: INSTANCE, observed_at: iso(at), received_at: iso(at),
    agent_version: 'dev', fingerprint: 'fp-e2e', sync_batch_id: `sync-${at}`,
    cpu_usage_pct: cpu, cpu_rates_valid: true, cpu_iowait_pct: 1, cpu_steal_pct: 0,
    load_1: 0.2, load_5: 0.3, load_15: 0.4, mem_used_pct: 40,
    mem_available_bytes: 1024, mem_total_bytes: 2048, swap_used_pct: 0,
    disk_used_pct: 30, disk_total_bytes: 4096, inode_used_pct: 5,
    net_in_bytes_per_sec: 0, net_out_bytes_per_sec: 0, network_rates_valid: true,
    disk_read_bytes_per_sec: 0, disk_write_bytes_per_sec: 0, disk_busy_pct: 0,
    uptime_seconds: 3600, maintenance_context: false, is_backfilled: false,
  }
}

function profile(window: 'realtime' | '24h'): ApiFixtureProfile {
  const base = monitoringInstanceDetailProfile(INSTANCE)
  const key = apiRouteKey('GET', `/api/monitoring-instances/${INSTANCE}/runtime-facts?window=${window}`)
  const facts = base[key]!.body as Record<string, unknown>
  return {
    ...base,
    [key]: { status: 200, body: {
      ...facts,
      latest_host_sample: sample(START),
      recent_host_samples: window === 'realtime' ? [sample(START)] : [],
    } },
  }
}

function evidence(readAt: number, heartbeatAt: number | null = readAt) {
  return {
    type: 'online_evidence', monitoring_instance_id: INSTANCE,
    last_trusted_online_at: heartbeatAt === null ? null : iso(heartbeatAt), read_at: iso(readAt),
  }
}

test('four-day controlled stay keeps trusted header fresh and realtime samples moving', async ({ page, api }) => {
  await page.clock.install({ time: new Date(START) })
  api.useProfile(profile('realtime'))
  await api.allowRuntimeStream(INSTANCE)
  await page.goto(`/monitoring/${INSTANCE}?window=realtime`)
  await expect(page.getByRole('heading', { name: 'Tokyo Monitor', exact: true })).toBeVisible()
  await expect.poll(() => api.snapshotRuntimeStreamSockets()).toHaveLength(1)
  await api.assertRuntimeStreamConnected(INSTANCE)
  await api.sendRuntimeStreamMessage(INSTANCE, evidence(START))
  await expect(page.locator('.monitoring-detail-status__heartbeat')).toContainText('刚刚')

  // Move the wall clock in controlled checkpoints, preserving a live evidence
  // stream at each checkpoint. This is accelerated browser evidence, not a
  // claim that a four-day physical soak was performed.
  for (let day = 1; day <= 4; day += 1) {
    const at = START + day * 24 * 60 * 60 * 1000
    await page.clock.setSystemTime(at)
    await api.sendRuntimeStreamMessage(INSTANCE, evidence(at))
    await api.sendRuntimeStreamMessage(INSTANCE, {
      type: 'host_sample', monitoring_instance_id: INSTANCE,
      sample: sample(at, 12 + day), received_at: iso(at),
    })
    await expect(page.locator('.monitoring-detail-status__heartbeat')).toContainText('刚刚')
    await expect(page.locator('.monitoring-detail-status__sample')).toContainText('刚刚')
    await expect(page.locator('.monitoring-detail-chart--cpu .monitoring-detail-chart__value')).toContainText(`${12 + day}.0%`)
    await expect(page.getByText('可信心跳已超时', { exact: true })).toHaveCount(0)
  }
  await expect(page.locator('.monitoring-detail-status__sample')).toContainText('1小时')
  expect(api.requestCount('GET', `/api/monitoring-instances/${INSTANCE}/runtime-facts?window=realtime`)).toBe(1)

  await api.disconnectRuntimeStream(INSTANCE)
  await expect(page.getByText('在线证据更新中断', { exact: true })).toBeVisible()
  await expect(page.locator('.monitoring-detail-status__heartbeat')).toContainText('上次读取结果')
  await page.clock.fastForward(2000)
  await expect.poll(() => api.snapshotRuntimeStreamSockets()).toHaveLength(2)
  await api.sendRuntimeStreamMessage(INSTANCE, evidence(START + 4 * 86400000 + 2000))
  await expect(page.getByText('在线证据更新中断', { exact: true })).toHaveCount(0)
  await expect(page.locator('.monitoring-detail-status__heartbeat')).toContainText('刚刚')
})

test('historical header reaches exact heartbeat timeout and recovers without refetching charts', async ({ page, api }) => {
  api.useProfile(profile('24h'))
  await api.allowRuntimeStream(INSTANCE)
  await page.goto(`/monitoring/${INSTANCE}`)
  await expect(page.getByRole('heading', { name: 'Tokyo Monitor', exact: true })).toBeVisible()
  await api.assertRuntimeStreamConnected(INSTANCE, false)
  const historyBefore = await page.locator('.monitoring-detail-charts').innerHTML()
  await api.sendRuntimeStreamMessage(INSTANCE, evidence(START))
  await api.sendRuntimeStreamMessage(INSTANCE, evidence(START + 59000, START))
  await expect(page.getByText('可信心跳已超时', { exact: true })).toHaveCount(0)
  await api.sendRuntimeStreamMessage(INSTANCE, evidence(START + 60000, START))
  await expect(page.getByText('可信心跳已超时', { exact: true })).toBeVisible()
  await expect(page.locator('.monitoring-detail-status__sample')).toContainText('1 分钟前')
  await api.sendRuntimeStreamMessage(INSTANCE, evidence(START + 65000))
  await expect(page.getByText('可信心跳已超时', { exact: true })).toHaveCount(0)
  await expect(page.locator('.monitoring-detail-status__heartbeat')).toContainText('刚刚')
  expect(api.requestCount('GET', `/api/monitoring-instances/${INSTANCE}/runtime-facts?window=24h`)).toBe(1)
  expect(await page.locator('.monitoring-detail-charts').innerHTML()).toBe(historyBefore)
  await page.getByRole('button', { name: '实时', exact: true }).click()
  await expect.poll(() => api.snapshotRuntimeStreamSockets()).toHaveLength(2)
  await api.assertRuntimeStreamConnected(INSTANCE)
  const sockets = await api.snapshotRuntimeStreamSockets()
  expect(sockets.filter((socket) => socket.phase === 'open')).toHaveLength(1)
})

test('initial stream without evidence stops automatic reconnect and offers manual retry', async ({ page, api }) => {
  await page.clock.install({ time: new Date(START) })
  api.useProfile(profile('24h'))
  await api.allowRuntimeStream(INSTANCE)
  await page.goto(`/monitoring/${INSTANCE}`)
  await expect(page.getByRole('heading', { name: 'Tokyo Monitor', exact: true })).toBeVisible()
  await api.assertRuntimeStreamConnected(INSTANCE, false)
  await page.clock.fastForward(15000)
  await expect(page.getByText('在线证据自动更新不可用', { exact: true })).toBeVisible()
  await page.clock.fastForward(30000)
  expect(await api.snapshotRuntimeStreamSockets()).toHaveLength(1)
  await page.getByRole('button', { name: '重试在线证据', exact: true }).click()
  await expect.poll(() => api.snapshotRuntimeStreamSockets()).toHaveLength(2)
  await api.sendRuntimeStreamMessage(INSTANCE, evidence(START + 45000))
  await expect(page.locator('.monitoring-detail-status__heartbeat')).toContainText('刚刚')
  await expect(page.getByText('在线证据自动更新不可用', { exact: true })).toHaveCount(0)
})

test('realtime chart reread failure retains old chart and exposes local retry', async ({ page, api }) => {
  await page.clock.install({ time: new Date(START) })
  const good = profile('realtime')
  api.useProfile(good)
  await api.allowRuntimeStream(INSTANCE)
  await page.goto(`/monitoring/${INSTANCE}?window=realtime`)
  await expect(page.locator('.monitoring-detail-status__sample')).toBeVisible()
  await api.sendRuntimeStreamMessage(INSTANCE, evidence(START))
  api.useProfile({
    ...good,
    [apiRouteKey('GET', `/api/monitoring-instances/${INSTANCE}/runtime-facts?window=realtime`)]: {
      status: 503, body: { error: 'controlled metrics failure' },
    },
  })
  await api.disconnectRuntimeStream(INSTANCE)
  await expect(page.getByText(/运行指标刷新失败，仍显示上次已标记窗口/)).toBeVisible()
  await expect(page.locator('.monitoring-detail-status__sample')).toContainText('已运行')
  api.useProfile(good)
  await page.getByRole('button', { name: '重试运行指标', exact: true }).click()
  await expect(page.getByText(/运行指标刷新失败，仍显示上次已标记窗口/)).toHaveCount(0)
})
