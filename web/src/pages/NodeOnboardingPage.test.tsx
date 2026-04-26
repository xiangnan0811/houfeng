import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { setOnboardingTokenCache } from '../lib/onboardingTokenCache'
import { NodeOnboardingPage } from './NodeOnboardingPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

function renderOnboardingPage(initialEntry = '/nodes/nd_001/onboarding') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/nodes/:nodeId/onboarding" element={<NodeOnboardingPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('NodeOnboardingPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.sessionStorage.clear()
  })

  it('shows the cached token and installation steps while onboarding has not started', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '待接入',
          monitoring_status: '启用',
          binding_status: '未绑定',
          labels: ['edge'],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-26T09:00:00Z',
          updated_at: '2026-04-26T09:00:00Z',
          phase: '未开始接入',
          has_host_sample: false,
          has_accepted_observation: false,
          enrollment_token_issued_at: '2026-04-26T09:05:00Z',
        }),
      ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Tokyo Edge' })).toBeInTheDocument(),
    )

    expect(screen.getByText('enroll_tokyo_001')).toBeInTheDocument()
    expect(screen.getByText('在服务器上安装 agent')).toBeInTheDocument()
    expect(screen.getByText('写入该节点专属 token')).toBeInTheDocument()
    expect(screen.getByText('启动 systemd 服务')).toBeInTheDocument()
    expect(screen.getByText('等待首次同步与绑定完成')).toBeInTheDocument()
    expect(screen.getByText('等待首次接入')).toBeInTheDocument()
  })

  it('shows the bound-but-waiting state before stable observation arrives', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '待接入',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: ['edge'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-26T09:20:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-26T09:00:00Z',
          updated_at: '2026-04-26T09:20:00Z',
          phase: '已绑定，等待稳定观测',
          has_host_sample: false,
          has_accepted_observation: false,
          enrollment_token_issued_at: '2026-04-26T09:05:00Z',
        }),
      ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByText('已完成指纹绑定，等待稳定观测')).toBeInTheDocument(),
    )

    expect(screen.getByText('首批样本：未到达')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '查看节点详情' })).not.toBeInTheDocument()
  })

  it('shows completion state with a CTA back to node detail', async () => {
    setOnboardingTokenCache('nd_001', {
      token: 'enroll_tokyo_001',
      issued_at: '2026-04-26T09:05:00Z',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          labels: ['edge'],
          note: '',
          current_health_status: '正常',
          last_heartbeat_at: '2026-04-26T09:20:00Z',
          last_sync_at: '2026-04-26T09:21:00Z',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-26T09:00:00Z',
          updated_at: '2026-04-26T09:21:00Z',
          phase: '接入完成',
          has_host_sample: true,
          has_accepted_observation: true,
          enrollment_token_issued_at: '2026-04-26T09:05:00Z',
        }),
      ),
    )

    renderOnboardingPage()

    await waitFor(() => expect(screen.getByText('接入已完成，可以转入日常观察。')).toBeInTheDocument())

    expect(screen.getByRole('link', { name: '查看节点详情' })).toHaveAttribute(
      'href',
      '/nodes/nd_001',
    )
  })

  it('asks for regeneration when the plaintext token is no longer cached', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          node_id: 'nd_001',
          display_name: 'Tokyo Edge',
          region: 'ap-northeast-1',
          city: 'Tokyo',
          provider: 'Vultr',
          lifecycle_status: '待接入',
          monitoring_status: '启用',
          binding_status: '未绑定',
          labels: ['edge'],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          created_at: '2026-04-26T09:00:00Z',
          updated_at: '2026-04-26T09:00:00Z',
          phase: '未开始接入',
          has_host_sample: false,
          has_accepted_observation: false,
          enrollment_token_issued_at: '2026-04-26T09:05:00Z',
        }),
      ),
    )

    renderOnboardingPage()

    await waitFor(() =>
      expect(screen.getByText('当前会话里没有可显示的 Token 明文。')).toBeInTheDocument(),
    )

    expect(screen.getByText('请重新生成接入 Token，再继续安装或核对配置。')).toBeInTheDocument()
    expect(screen.queryByText('enroll_tokyo_001')).not.toBeInTheDocument()
  })
})
