import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import * as api from '../../lib/api'
import type { MonitoringInstancePhase } from '../../lib/types'
import { MonitoringInstancePhases } from './MonitoringInstancePhases'

describe('monitoring phase history', () => {
  afterEach(() => vi.restoreAllMocks())
  it('loads on demand and preserves evidence-only old-session online history', async () => {
    const phases: MonitoringInstancePhase[] = [{ session_id: 'old_session', capability: 'evidence_only', fingerprint_hash: 'sha256:old', started_at: '2026-09-01T00:00:00Z', ended_at: '2026-09-20T00:00:00Z', last_trusted_online_at: '2026-09-26T00:00:00Z', ever_connected: true }]
    const request = vi.spyOn(api, 'listMonitoringInstancePhases').mockResolvedValue(phases)
    render(<MonitoringInstancePhases monitoringInstanceId="mi_one" />)
    expect(request).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '接入阶段历史' }))
    await screen.findByText('仅在线证据')
    expect(screen.getByText('old_session')).toBeInTheDocument()
    expect(screen.getByText('最后可信在线')).toBeInTheDocument()
    expect(request).toHaveBeenCalledWith('mi_one')
  })

  it('does not render a previous instance delayed session response', async () => {
    let resolveOld!: (value: MonitoringInstancePhase[]) => void
    vi.spyOn(api, 'listMonitoringInstancePhases').mockReturnValueOnce(new Promise((resolve) => { resolveOld = resolve })).mockResolvedValueOnce([])
    const view = render(<MonitoringInstancePhases monitoringInstanceId="mi_old" />)
    fireEvent.click(screen.getByRole('button', { name: '接入阶段历史' }))
    view.rerender(<MonitoringInstancePhases monitoringInstanceId="mi_new" />)
    await screen.findByText('尚无接入会话')
    await act(async () => resolveOld([{ session_id: 'stale_session', capability: 'full', fingerprint_hash: 'old', started_at: '2026-09-01T00:00:00Z', ever_connected: true }]))
    expect(screen.queryByText('stale_session')).not.toBeInTheDocument()
    expect(screen.getByText('尚无接入会话')).toBeInTheDocument()
  })
})
