import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { AssetDecisionRecordSummary, SubscriptionRecord, VPSAssetRecord } from '../../../../lib/types'
import { decisionRecord, LocationProbe, subscription, vps } from '../../testFixtures'
import type { QueueState } from '../../types'
import { RecordsWorkbench } from './RecordsWorkbench'
import { RenewalsWorkbench } from './RenewalsWorkbench'
import { compactLocation, localDay } from './workbenchFormat'

const originalTZ = process.env.TZ

afterEach(() => {
  if (originalTZ === undefined) delete process.env.TZ
  else process.env.TZ = originalTZ
})

function queueState(renewals: SubscriptionRecord[]): QueueState {
  return { renewalsLoading: false, renewalsError: null, queueLoading: false, queueError: null, renewals, subscriptions: [], unreviewed: [], cancel: [] }
}

describe('workbench formatting', () => {
  it('drops repeated location levels and keeps a missing fallback', () => {
    expect(compactLocation({ country: 'JP', region: 'Tokyo', city: 'tokyo' })).toBe('JP · Tokyo')
    expect(compactLocation({ country: 'DE', region: 'Hesse', city: 'Frankfurt' })).toBe('DE · Hesse · Frankfurt')
    expect(compactLocation({ country: '', region: '', city: '' })).toBe('位置缺失')
  })

  it('shows timestamps as local calendar days', () => {
    process.env.TZ = 'America/Los_Angeles'
    expect(localDay('2026-10-02T03:00:00Z')).toBe('2026-10-01')
    expect(localDay('not-a-date')).toBe('—')
    expect(localDay(null)).toBe('—')
  })
})

describe('RecordsWorkbench', () => {
  it('opens a record from the row or its button exactly once and only badges readback that needs review', () => {
    const onOpenRecord = vi.fn()
    const drift = decisionRecord({
      record_id: 'adr_drift', title: '漂移记录',
      execution_readback: { status: 'drift', summary: '', open_count: 0, aligned_count: 1, drift_count: 2, blocked_count: 0, needs_evidence_count: 0 },
    }) as unknown as AssetDecisionRecordSummary
    const aligned = decisionRecord({
      record_id: 'adr_aligned', title: '对齐记录',
      execution_readback: { status: 'aligned', summary: '', open_count: 0, aligned_count: 2, drift_count: 0, blocked_count: 0, needs_evidence_count: 0 },
    }) as unknown as AssetDecisionRecordSummary
    // 旧记录可能没有回读字段，不能因此崩溃；未知回读状态原样显示，不当作已对齐。
    const legacy = { ...decisionRecord({ record_id: 'adr_legacy', title: '旧记录' }), execution_readback: undefined } as unknown as AssetDecisionRecordSummary
    const future = decisionRecord({
      record_id: 'adr_future', title: '新状态记录',
      execution_readback: { status: 'future_review_required', summary: '', open_count: 0, aligned_count: 0, drift_count: 0, blocked_count: 0, needs_evidence_count: 0 },
    }) as unknown as AssetDecisionRecordSummary
    render(<RecordsWorkbench recordsState={{ loading: false, error: null, records: [drift, aligned, legacy, future] }} onOpenRecord={onOpenRecord} />)

    const rows = within(screen.getByRole('list', { name: '已保存组合决策' })).getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('与决定不符 2')
    expect(within(rows[1]!).queryAllByText(/与决定一致|待核对结果/)).toHaveLength(0)
    expect(rows[2]).toHaveTextContent('旧记录')
    expect(rows[3]).toHaveTextContent('future_review_required')

    // 点行内标题：先聚焦行内“查看”再打开，弹窗关闭后焦点能回到入口。
    fireEvent.click(within(rows[0]!).getByText('漂移记录'))
    expect(onOpenRecord).toHaveBeenCalledTimes(1)
    expect(onOpenRecord).toHaveBeenLastCalledWith('adr_drift')
    expect(within(rows[0]!).getByRole('button', { name: '查看' })).toHaveFocus()
    fireEvent.click(within(rows[1]!).getByRole('button', { name: '查看' }))
    expect(onOpenRecord).toHaveBeenCalledTimes(2)
    expect(onOpenRecord).toHaveBeenLastCalledWith('adr_aligned')
  })
})

describe('RenewalsWorkbench', () => {
  it('shows status only when it is not active and monthly price only when it differs', () => {
    const yearly = { ...subscription, subscription_id: 'sub_year', vps_id: 'vps_review', price: 120, monthly_price: 10, status: 'paused', display_name: '年付套餐' } as unknown as SubscriptionRecord
    const monthly = { ...subscription, subscription_id: 'sub_month', vps_id: 'vps_missing' } as unknown as SubscriptionRecord
    const vpsByID = new Map<string, VPSAssetRecord>([['vps_review', vps as unknown as VPSAssetRecord]])
    render(
      <MemoryRouter>
        <RenewalsWorkbench queueState={queueState([yearly, monthly])} vpsByID={vpsByID} renewalWindow={60} />
        <LocationProbe />
      </MemoryRouter>,
    )
    const rows = within(screen.getByRole('list', { name: '续费窗口' })).getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('已暂停')
    expect(rows[0]).toHaveTextContent('月付 USD 10.00')
    expect(within(rows[0]!).getByRole('link', { name: 'Tokyo Review' })).toHaveAttribute('href', '/vps/vps_review')
    expect(rows[1]).not.toHaveTextContent('生效中')
    expect(rows[1]).not.toHaveTextContent('月付')
    expect(rows[1]).toHaveTextContent('VPS 名称未加载')
    expect(rows[1]).not.toHaveTextContent('vps_missing')
    expect(screen.getByRole('link', { name: '查看续费取舍组' })).toHaveAttribute('href', '/asset-decisions?view=renewal&renew_within_days=60')

    // 点行内非交互区域：经由 VPS 名称链接导航到 VPS 详情。
    fireEvent.click(within(rows[0]!).getByText('USD 120.00'))
    expect(screen.getByLabelText('current-url')).toHaveTextContent('/vps/vps_review')
  })
})
