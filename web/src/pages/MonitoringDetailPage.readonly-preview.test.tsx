import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useSearchParams } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../lib/api'
import { MonitoringDetailPage } from './MonitoringDetailPage'

vi.mock('../lib/readOnlyPreview', () => ({
  READ_ONLY_PREVIEW: true,
}))

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

function SearchProbe() {
  const [searchParams] = useSearchParams()
  return <div data-testid="search">{searchParams.toString()}</div>
}

describe('MonitoringDetailPage read-only preview', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('discards ?onboarding=1 and never issues an install command', async () => {
    const issue = vi.spyOn(api, 'issueMonitoringInstanceInstallCommand')
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/install-command')) {
          throw new Error('install-command must not be issued in read-only preview')
        }
        if (url === '/api/monitoring-instances/mi_001') {
          return mockJSONResponse({
            monitoring_instance_id: 'mi_001',
            display_name: 'Tokyo Edge',
            region: 'ap-northeast-1',
            city: 'Tokyo',
            provider: 'Vultr',
            lifecycle_status: '待接入',
            monitoring_status: '未启用',
            binding_status: '未绑定',
            labels: [],
            note: '',
            current_health_status: '正常',
            last_heartbeat_at: null,
            last_sync_at: null,
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:00:00Z',
          })
        }
        if (url.startsWith('/api/monitoring-instances/mi_001/runtime-facts')) {
          return mockJSONResponse({
            monitoring_instance_id: 'mi_001',
            latest_host_sample: null,
          })
        }
        if (url.startsWith('/api/settings')) {
          return mockJSONResponse({
            incident_defaults: {
              heartbeat_interval_seconds: 30,
              stale_threshold_intervals: 3,
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
          })
        }
        return mockJSONResponse([])
      }),
    )

    render(
      <MemoryRouter initialEntries={['/monitoring/mi_001?onboarding=1']}>
        <Routes>
          <Route
            path="/monitoring/:monitoringInstanceId"
            element={(
              <>
                <SearchProbe />
                <MonitoringDetailPage />
              </>
            )}
          />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.getByText('只读预览')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByTestId('search').textContent).not.toContain('onboarding=1')
    })
    expect(screen.queryByRole('button', { name: '生成一键安装命令' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '生成升级/重新接入命令' })).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '监控实例接入抽屉' })).not.toBeInTheDocument()
    expect(screen.queryByText(/curl .*install\.sh/)).not.toBeInTheDocument()
    expect(issue).not.toHaveBeenCalled()
  })
})
