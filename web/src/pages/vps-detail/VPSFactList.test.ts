import { describe, expect, it } from 'vitest'

import type { VPSOverviewFact, VPSOverviewIdentity } from '../../lib/types'
import { factLayoutFor, legacyOverviewFactRows, modernOverviewFactRows } from './vpsFactPresentation'

const identity: VPSOverviewIdentity = {
  vps_id: 'vps_001',
  display_name: '东京边缘',
  provider_name: 'Example',
  product_name: 'Compute Cloud Pro',
  country: 'JP',
  region: 'Tokyo',
  city: 'Tokyo',
  datacenter: 'TK1',
  ipv4: '192.0.2.1',
  ipv6: '',
  lifecycle_status: 'active',
  usage_status: 'in_use',
  renewal_decision: 'keep',
  importance: 'normal',
  labels: ['QA验证'],
  updated_at: '2026-08-20T00:00:00Z',
}

describe('modernOverviewFactRows', () => {
  it('adds identity product, importance, and labels without inventing a note', () => {
    const facts: VPSOverviewFact[] = [
      { key: 'ipv4', label: 'IPv4', value: '192.0.2.1' },
      { key: 'ssh', label: 'SSH', value: 'root@192.0.2.1:22' },
      { key: 'os_name', label: '操作系统', value: 'Debian' },
    ]
    const rows = modernOverviewFactRows(facts, identity)
    expect(rows.find((row) => row.key === 'product_name')?.value).toBe('Compute Cloud Pro')
    expect(rows.find((row) => row.key === 'os_name')?.value).toBe('Debian')
    expect(rows.find((row) => row.key === 'importance')?.value).toBe('普通')
    expect(rows.find((row) => row.key === 'labels')?.value).toBe('QA验证')
    expect(rows.some((row) => row.key === 'note' || row.label === '备注')).toBe(false)
    expect(factLayoutFor({ key: 'ssh', label: 'SSH' })).toBe('full')
  })

  it('keeps an existing product fact and a provided note instead of duplicating or synthesizing', () => {
    const facts: VPSOverviewFact[] = [
      { key: 'product_name', label: '产品', value: 'edge-small' },
      { key: 'note', label: '备注', value: 'operator note' },
      { key: 'importance', label: '重要性', value: 'normal' },
    ]
    const rows = modernOverviewFactRows(facts, identity)
    expect(rows.filter((row) => row.key === 'product_name')).toHaveLength(1)
    expect(rows.find((row) => row.key === 'product_name')?.value).toBe('edge-small')
    expect(rows.find((row) => row.key === 'importance')?.value).toBe('普通')
    expect(rows.find((row) => row.key === 'note')).toMatchObject({
      value: 'operator note',
      layout: 'full',
    })
  })
})

describe('legacyOverviewFactRows', () => {
  it('formats provided importance with the shared display formatter', () => {
    const rows = legacyOverviewFactRows([
      { domain: 'identity', label: '重要性', value: 'normal' },
      { domain: 'identity', label: '规格', value: 'cx22' },
      { domain: 'identity', label: '系统', value: 'Debian' },
    ])
    expect(rows.find((row) => row.label === '重要性')?.value).toBe('普通')
    expect(rows.find((row) => row.label === '规格')?.value).toBe('cx22')
    expect(rows.find((row) => row.label === '系统')?.value).toBe('Debian')
  })
})
