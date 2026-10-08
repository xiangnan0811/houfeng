import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { VPSAssetDetail } from '../../lib/types'
import type { MonitoringInstanceCreateDraftState } from './types'
import { VPSMonitoringInstanceCreateForm } from './VPSMonitoringInstanceCreateForm'
import { monitoringInstanceCreateDraftFromDetail } from './vpsDetailHelpers'

function detailFixture(overrides: Partial<VPSAssetDetail> = {}): VPSAssetDetail {
  return {
    vps_id: 'vps_a',
    display_name: '东京边缘',
    provider_id: null,
    provider_name: 'Example',
    product_name: 'VPS',
    order_ref: '',
    country: 'JP',
    region: 'Tokyo',
    city: 'Tokyo',
    datacenter: 'TK1',
    ipv4: '192.0.2.1',
    ipv6: '',
    ssh_host: '192.0.2.1',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian',
    virtualization: 'KVM',
    lifecycle_status: 'active',
    usage_tags: [],
    renewal_decision: 'keep',
    importance: 'high',
    labels: ['edge'],
    note: '资产备注',
    active_monitoring_instance_link_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    monitoring_instance_links: [],
    ...overrides,
  }
}

function renderForm(detail: VPSAssetDetail) {
  const seen: MonitoringInstanceCreateDraftState[] = []
  const onFeedbackClear = vi.fn()
  function Harness() {
    const [draft, setDraft] = useState(() => monitoringInstanceCreateDraftFromDetail(detail))
    return (
      <VPSMonitoringInstanceCreateForm
        formId="mi-create"
        detail={detail}
        draft={draft}
        submitting={false}
        onDraftChange={(next) => {
          seen.push(next)
          setDraft(next)
        }}
        onFeedbackClear={onFeedbackClear}
        onSubmit={(event) => event.preventDefault()}
      />
    )
  }
  render(<Harness />)
  return { seen, onFeedbackClear }
}

describe('VPSMonitoringInstanceCreateForm', () => {
  it('shows the copied identity as values and explains the independent copy', () => {
    renderForm(detailFixture())

    expect(screen.getByRole('textbox', { name: '监控实例名称' })).toHaveValue('东京边缘')
    expect(screen.getByRole('textbox', { name: '分组' })).toHaveValue('')
    expect(screen.getByRole('textbox', { name: '区域' })).toHaveValue('Tokyo')
    expect(screen.getByRole('textbox', { name: '城市' })).toHaveValue('Tokyo')
    expect(screen.getByRole('textbox', { name: '服务商' })).toHaveValue('Example')
    expect(screen.getByRole('textbox', { name: '标签' })).toHaveValue('edge')
    expect(screen.getByRole('textbox', { name: '监控备注' })).toHaveValue('资产备注')
    expect(screen.getByRole('textbox', { name: '关联备注' })).toHaveValue('')
    expect(screen.getByRole('textbox', { name: '区域' })).toHaveAttribute('placeholder', '未知')
    expect(screen.getByRole('textbox', { name: '区域' })).not.toHaveValue('未知')
    expect(screen.getByText('区域、城市和服务商复制自当前 VPS。创建后为独立副本，之后修改 VPS 不会更新此监控实例。')).toBeInTheDocument()
    expect(screen.queryByDisplayValue('未确认')).not.toBeInTheDocument()
    expect(screen.queryByDisplayValue('未关联服务商')).not.toBeInTheDocument()
    expect(screen.queryByDisplayValue('created from vps detail')).not.toBeInTheDocument()
  })

  it('keeps unknown fields empty instead of writing a placeholder into the value', () => {
    renderForm(detailFixture({
      region: '',
      country: '',
      city: '',
      datacenter: '',
      provider_name: '',
    }))

    for (const name of ['区域', '城市', '服务商']) {
      const field = screen.getByRole('textbox', { name })
      expect(field).toHaveValue('')
      expect(field).toHaveAttribute('placeholder', '未知')
    }
    expect(screen.getByRole('textbox', { name: '监控实例名称' })).toHaveValue('东京边缘')
    expect(screen.getByRole('textbox', { name: '标签' })).toHaveValue('edge')
    expect(screen.getByRole('textbox', { name: '监控备注' })).toHaveValue('资产备注')
    expect(screen.getByRole('textbox', { name: '关联备注' })).toHaveValue('')
  })

  it('tracks an explicit clear and removes it when the same field is refilled', () => {
    const { seen, onFeedbackClear } = renderForm(detailFixture({ region: '', country: 'JP', city: '', datacenter: 'NRT' }))
    const region = screen.getByRole('textbox', { name: '区域' })
    const city = screen.getByRole('textbox', { name: '城市' })
    const provider = screen.getByRole('textbox', { name: '服务商' })
    expect(region).toHaveValue('JP')
    expect(city).toHaveValue('NRT')

    fireEvent.change(provider, { target: { value: '' } })
    fireEvent.change(region, { target: { value: '   ' } })
    fireEvent.change(city, { target: { value: '' } })
    expect(region).toHaveValue('   ')
    expect(region).not.toHaveValue('未知')
    expect(seen.at(-1)?.clearedFields).toEqual(['city', 'provider', 'region'])

    fireEvent.change(screen.getByRole('textbox', { name: '监控实例名称' }), { target: { value: '新名称' } })
    fireEvent.change(screen.getByRole('textbox', { name: '分组' }), { target: { value: '边缘' } })
    fireEvent.change(screen.getByRole('textbox', { name: '标签' }), { target: { value: 'a, b' } })
    fireEvent.change(screen.getByRole('textbox', { name: '监控备注' }), { target: { value: '备注' } })
    fireEvent.change(screen.getByRole('textbox', { name: '关联备注' }), { target: { value: '现场备注' } })
    expect(seen.at(-1)).toMatchObject({
      displayName: '新名称',
      group: '边缘',
      labels: 'a, b',
      note: '备注',
      linkNote: '现场备注',
      clearedFields: ['city', 'provider', 'region'],
    })
    expect(screen.queryByDisplayValue('created from vps detail')).not.toBeInTheDocument()

    fireEvent.change(region, { target: { value: '关东' } })
    expect(region).toHaveValue('关东')
    expect(seen.at(-1)?.clearedFields).toEqual(['city', 'provider'])
    fireEvent.change(city, { target: { value: '大阪' } })
    fireEvent.change(provider, { target: { value: '本地' } })
    expect(seen.at(-1)?.clearedFields).toEqual([])
    expect(onFeedbackClear).toHaveBeenCalled()
  })
})
