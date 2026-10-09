import { describe, expect, it } from 'vitest'

import type { VPSAssetDetail } from '../../lib/types'
import {
  compareDecisionDraft,
  buildFactEditInput,
  compareFactDraftAgainstLatest,
  decisionDraftAlreadySatisfied,
  detailToFactEditForm,
  buildMonitoringInstanceCreateInput,
  monitoringInstanceCreateDraftFromDetail,
  mergeFactDraftWithLatest,
  updateMonitoringInstanceCreateDraft,
} from './vpsDetailHelpers'
import type { FactEditFormState } from './types'

function detailFixture(overrides: Partial<VPSAssetDetail> = {}): VPSAssetDetail {
  return {
    vps_id: 'vps_a',
    display_name: '东京边缘',
    provider_id: null,
    provider_name: 'Example',
    product_name: 'VPS',
    order_ref: 'ord-1',
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
    usage_tags: ['生产'],
    validity_mode: 'unknown',
    expires_at: null,
    auto_renew_check: 'unchecked',
    auto_renew_checked_at: null,
    renewal_reason: '',
    renewal_review_at: null,
    renewal_decision: 'keep',
    importance: 'high',
    labels: ['edge'],
    note: '',
    active_monitoring_instance_link_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    monitoring_instance_links: [],
    ...overrides,
  }
}

function edit(base: FactEditFormState, overrides: Partial<FactEditFormState>): FactEditFormState {
  return { ...base, ...overrides }
}

describe('fact draft 3-way merge', () => {
  it('keeps local name edits and takes concurrent product_name and region from latest', () => {
    const baseDetail = detailFixture()
    const latest = detailFixture({
      display_name: '东京边缘最新',
      product_name: 'edge-large',
      region: 'Osaka',
    })
    const base = detailToFactEditForm(baseDetail)
    const draft = edit(base, { displayName: '我的草稿' })

    const merged = mergeFactDraftWithLatest(base, draft, latest)
    expect(merged.displayName).toBe('我的草稿')
    expect(merged.productName).toBe('edge-large')
    expect(merged.region).toBe('Osaka')
    expect(merged.labels).toBe('edge')

    const rows = compareFactDraftAgainstLatest(base, draft, latest)
    expect(rows).toEqual([{ field: '名称', yours: '我的草稿', latest: '东京边缘最新' }])
    expect(rows.map((row) => row.field)).not.toContain('产品名')
    expect(rows.map((row) => row.field)).not.toContain('区域')
  })

  it('keeps a local address fix that only differs by a non-Go-trimmed character', () => {
    const baseDetail = detailFixture({ ipv4: '\ufeff203.0.113.10' })
    const latest = detailFixture({ ipv4: '\ufeff203.0.113.10', note: '他人更新备注' })
    const base = detailToFactEditForm(baseDetail)
    const draft = edit(base, { ipv4: '203.0.113.10' })

    const merged = mergeFactDraftWithLatest(base, draft, latest)
    expect(merged.ipv4).toBe('203.0.113.10')
    expect(merged.note).toBe('他人更新备注')
  })

  it('does not overwrite concurrent labels, IPv4, or SSH host with stale local values', () => {
    const baseDetail = detailFixture()
    const latest = detailFixture({
      labels: ['edge', 'prod'],
      ipv4: '198.51.100.9',
      ssh_host: 'ssh.example.test',
    })
    const base = detailToFactEditForm(baseDetail)
    const draft = edit(base, { note: '只改了备注' })

    const merged = mergeFactDraftWithLatest(base, draft, latest)
    expect(merged.note).toBe('只改了备注')
    expect(merged.labels).toBe('edge, prod')
    expect(merged.ipv4).toBe('198.51.100.9')
    expect(merged.sshHost).toBe('ssh.example.test')

    const fields = compareFactDraftAgainstLatest(base, draft, latest).map((row) => row.field)
    expect(fields).toEqual(['备注'])
    expect(fields).not.toContain('产品名')
    expect(fields).not.toContain('标签')
    expect(fields).not.toContain('IPv4')
    expect(fields).not.toContain('SSH Host')
  })

  it('treats label formatting, port padding, and trim-only strings as not user edits', () => {
    const baseDetail = detailFixture({
      labels: ['edge', 'prod'],
      ssh_port: 22,
      display_name: '东京边缘',
      note: 'keep',
    })
    const latest = detailFixture({
      labels: ['edge', 'prod', 'ops'],
      ssh_port: 2200,
      display_name: '东京边缘最新',
      note: 'server-note',
    })
    const base = detailToFactEditForm(baseDetail)
    const draft = edit(base, {
      labels: 'edge,prod',
      sshPort: '022',
      displayName: ' 东京边缘 ',
      note: 'keep',
    })

    const merged = mergeFactDraftWithLatest(base, draft, latest)
    expect(merged.labels).toBe('edge, prod, ops')
    expect(merged.sshPort).toBe('2200')
    expect(merged.displayName).toBe('东京边缘最新')
    expect(merged.note).toBe('server-note')

    const fields = compareFactDraftAgainstLatest(base, draft, latest).map((row) => row.field)
    expect(fields).toEqual([])
  })

  it('keeps server-added labels when an unrelated field is temporarily invalid', () => {
    const baseDetail = detailFixture({
      labels: ['edge', 'prod'],
      display_name: '东京边缘',
      note: 'keep',
    })
    const latest = detailFixture({
      labels: ['edge', 'prod', 'ops'],
      display_name: '东京边缘最新',
      note: 'server-note',
    })
    const base = detailToFactEditForm(baseDetail)
    const draft = edit(base, {
      displayName: '',
      labels: 'edge,prod',
      note: '  keep  ',
    })

    const merged = mergeFactDraftWithLatest(base, draft, latest)
    expect(merged.labels).toBe('edge, prod, ops')
    expect(merged.displayName).toBe('')
    expect(merged.note).toBe('server-note')

    const fields = compareFactDraftAgainstLatest(base, draft, latest).map((row) => row.field)
    expect(fields).toContain('名称')
    expect(fields).not.toContain('标签')
    expect(fields).not.toContain('备注')
  })

  it('does not let an invalid port or empty address disable label and trim compare', () => {
    const baseDetail = detailFixture({
      labels: ['edge', 'prod'],
      ssh_port: 22,
      note: 'keep',
    })
    const latest = detailFixture({
      labels: ['edge', 'prod', 'ops'],
      ssh_port: 2200,
      note: 'server-note',
    })
    const base = detailToFactEditForm(baseDetail)
    const draft = edit(base, {
      sshPort: 'not-a-port',
      ipv4: '',
      sshHost: '',
      labels: 'edge,prod',
      note: ' keep ',
    })

    const merged = mergeFactDraftWithLatest(base, draft, latest)
    expect(merged.labels).toBe('edge, prod, ops')
    expect(merged.note).toBe('server-note')
    expect(merged.sshPort).toBe('not-a-port')
    expect(merged.ipv4).toBe('')
    expect(merged.sshHost).toBe('')

    const fields = compareFactDraftAgainstLatest(base, draft, latest).map((row) => row.field)
    expect(fields).not.toContain('标签')
    expect(fields).not.toContain('备注')
    expect(fields).toContain('SSH 端口')
  })
})

describe('compareDecisionDraft', () => {
  it('uses localized renewal labels and detects an already-satisfied decision', () => {
    const latest = detailFixture({ renewal_decision: 'keep' })
    expect(decisionDraftAlreadySatisfied({ renewalDecision: 'keep', reason: '本地' }, latest)).toBe(false)
    expect(compareDecisionDraft({ renewalDecision: 'keep', reason: '' }, latest)).toEqual([])
    expect(compareDecisionDraft({ renewalDecision: 'cancel', reason: '' }, latest)).toEqual([{
      field: '续费决策',
      yours: '决定不续费',
      latest: '继续续费',
    }])
  })
})

describe('independent VPS facts', () => {
  it('preserves arbitrary purposes and validity without a subscription', () => {
    const draft = detailToFactEditForm(detailFixture())
    const input = buildFactEditInput({ ...draft, usageTags: '应用甲，测试,应用甲', validityMode: 'fixed', expiresAt: '2027-01-01' })
    expect(input.usage_tags).toEqual(['应用甲', '测试'])
    expect(input.expires_at).toBe('2027-01-01')
    expect(input).not.toHaveProperty('usage_status')
    expect(input).not.toHaveProperty('renewal_decision')
  })

  it('rejects malformed addresses but keeps unchanged legacy text editable', () => {
    const draft = detailToFactEditForm(detailFixture())
    expect(() => buildFactEditInput({ ...draft, ipv4: '999.1.1' })).toThrow('IPv4 地址格式不正确')
    expect(() => buildFactEditInput({ ...draft, ipv4: '192.0.2.01' })).toThrow('IPv4 地址格式不正确')
    expect(() => buildFactEditInput({ ...draft, ipv4: '2001:db8::1' })).toThrow('IPv4 地址格式不正确')
    expect(() => buildFactEditInput({ ...draft, ipv6: '192.0.2.1' })).toThrow('IPv6 地址格式不正确')
    expect(() => buildFactEditInput({ ...draft, ipv6: 'fe80::1%eth0' })).toThrow('IPv6 地址格式不正确')
    expect(buildFactEditInput({ ...draft, ipv4: ' 203.0.113.10 ', ipv6: '2001:db8::10' })).toMatchObject({ ipv4: '203.0.113.10', ipv6: '2001:db8::10' })
    expect(buildFactEditInput({ ...draft, ipv4: '999.1.1', note: '只改备注' }, { ipv4: '999.1.1', ipv6: '' }).ipv4).toBe('999.1.1')
    // 只差首尾空白的存量非法文本仍视为未改动。
    expect(buildFactEditInput({ ...draft, ipv4: ' 999.1.1 ', note: '只改备注' }, { ipv4: '999.1.1 ', ipv6: '' }).ipv4).toBe('999.1.1')
  })

  it('shares address vectors and white-space rules with center', () => {
    const draft = detailToFactEditForm(detailFixture())
    expect(buildFactEditInput({ ...draft, ipv6: '::ffff:192.0.2.1' }).ipv6).toBe('::ffff:192.0.2.1')
    expect(buildFactEditInput({ ...draft, ipv6: '::ffff:c000:201' }).ipv6).toBe('::ffff:c000:201')
    expect(() => buildFactEditInput({ ...draft, ipv4: '::ffff:192.0.2.1' })).toThrow('IPv4 地址格式不正确')
    // Go TrimSpace 去掉 U+0085 / U+3000，但不去掉 U+FEFF；前端同口径。
    expect(buildFactEditInput({ ...draft, ipv4: '\u0085203.0.113.10\u3000' }).ipv4).toBe('203.0.113.10')
    expect(() => buildFactEditInput({ ...draft, ipv4: '\ufeff203.0.113.10' })).toThrow('IPv4 地址格式不正确')
    // 只有 Go 会裁掉的空白时，地址与 SSH Host 都视为空，不能绕过必填判定。
    expect(() => buildFactEditInput({ ...draft, ipv4: '\u0085', sshHost: '\u0085' })).toThrow('IPv4 或 SSH Host 至少需要填写一个')
    expect(buildFactEditInput({ ...draft, ipv4: '\u0085203.0.113.10', sshHost: '\u0085203.0.113.10' }).ssh_host).toBe('203.0.113.10')
  })

  it('clears a stale date for unlimited validity and requires a fixed date', () => {
    const draft = detailToFactEditForm(detailFixture())
    expect(buildFactEditInput({ ...draft, validityMode: 'unlimited', expiresAt: '2027-01-01' }).expires_at).toBeNull()
    expect(() => buildFactEditInput({ ...draft, validityMode: 'fixed', expiresAt: '' })).toThrow('固定有效期')
  })

  it('preserves review-date changes even when the renewal intent is unchanged', () => {
    const latest = detailFixture({ renewal_decision: 'cancel' })
    expect(decisionDraftAlreadySatisfied({ renewalDecision: 'cancel', reason: '', reviewAt: '2027-01-01' }, latest)).toBe(false)
  })
})

const MONITORING_CREATE_BODY_KEYS = [
  'display_name',
  'group',
  'region',
  'city',
  'provider',
  'labels',
  'note',
  'link_note',
] as const

describe('monitoring instance create copy', () => {
  it('leaves unknown identity empty and keeps the previous name, group, labels, and note rules', () => {
    const draft = monitoringInstanceCreateDraftFromDetail(detailFixture({
      display_name: '边缘甲',
      region: '',
      country: '',
      city: '',
      datacenter: '',
      provider_name: '',
      labels: ['edge', 'prod'],
      note: '资产备注',
    }))

    expect(draft).toEqual({
      displayName: '边缘甲',
      group: '',
      region: '',
      city: '',
      provider: '',
      labels: 'edge, prod',
      note: '资产备注',
      linkNote: '',
      clearedFields: [],
    })
    const input = buildMonitoringInstanceCreateInput(draft)
    expect(input).toEqual({
      display_name: '边缘甲',
      group: '',
      region: '',
      city: '',
      provider: '',
      labels: ['edge', 'prod'],
      note: '资产备注',
      link_note: '',
    })
    expect(Object.keys(input)).toEqual(MONITORING_CREATE_BODY_KEYS)
    expect(JSON.stringify(input)).not.toMatch(/未确认|未关联服务商|created from vps detail/)
  })

  it('copies region from country and city from datacenter only when the primary field is empty', () => {
    const fallback = monitoringInstanceCreateDraftFromDetail(detailFixture({
      region: '',
      country: 'JP',
      city: '',
      datacenter: 'NRT',
      provider_name: '',
    }))
    expect(fallback.region).toBe('JP')
    expect(fallback.city).toBe('NRT')
    expect(fallback.provider).toBe('')
    expect(fallback.linkNote).toBe('')
    expect(buildMonitoringInstanceCreateInput(fallback)).not.toHaveProperty('clear_fields')

    const preferred = monitoringInstanceCreateDraftFromDetail(detailFixture({
      region: 'Kanto',
      country: 'JP',
      city: 'Shinjuku',
      datacenter: 'NRT',
      provider_name: 'Example',
    }))
    expect(preferred.region).toBe('Kanto')
    expect(preferred.city).toBe('Shinjuku')
    expect(preferred.provider).toBe('Example')
  })

  it('sends the unchanged default as the old eight fields and omits clear_fields', () => {
    const input = buildMonitoringInstanceCreateInput(monitoringInstanceCreateDraftFromDetail(detailFixture()))
    expect(input).toEqual({
      display_name: '东京边缘',
      group: '',
      region: 'Tokyo',
      city: 'Tokyo',
      provider: 'Example',
      labels: ['edge'],
      note: '',
      link_note: '',
    })
    expect(Object.keys(input)).toEqual(MONITORING_CREATE_BODY_KEYS)
  })

  it('records an explicit clear, sorts it, and removes that intent when the field is refilled', () => {
    let draft = monitoringInstanceCreateDraftFromDetail(detailFixture({ region: '', country: 'JP' }))
    expect(draft.region).toBe('JP')
    draft = updateMonitoringInstanceCreateDraft(draft, 'provider', '')
    draft = updateMonitoringInstanceCreateDraft(draft, 'region', '   ')
    draft = updateMonitoringInstanceCreateDraft(draft, 'city', '')
    draft = updateMonitoringInstanceCreateDraft(draft, 'region', '   ')
    draft = updateMonitoringInstanceCreateDraft(draft, 'displayName', '新名称')
    draft = updateMonitoringInstanceCreateDraft(draft, 'group', '边缘')
    draft = updateMonitoringInstanceCreateDraft(draft, 'labels', 'a, b')
    draft = updateMonitoringInstanceCreateDraft(draft, 'note', '备注')
    draft = updateMonitoringInstanceCreateDraft(draft, 'linkNote', '  ')

    expect(draft.clearedFields).toEqual(['city', 'provider', 'region'])
    expect(draft.displayName).toBe('新名称')
    expect(draft.group).toBe('边缘')
    expect(draft.labels).toBe('a, b')
    expect(draft.note).toBe('备注')
    const cleared = buildMonitoringInstanceCreateInput(draft)
    expect(cleared.clear_fields).toEqual(['city', 'provider', 'region'])
    expect(cleared).toMatchObject({
      display_name: '新名称',
      group: '边缘',
      region: '',
      city: '',
      provider: '',
      labels: ['a', 'b'],
      note: '备注',
      link_note: '',
    })

    draft = updateMonitoringInstanceCreateDraft(draft, 'region', '关东')
    expect(draft.clearedFields).toEqual(['city', 'provider'])
    const refilled = buildMonitoringInstanceCreateInput(draft)
    expect(refilled.region).toBe('关东')
    expect(refilled.clear_fields).toEqual(['city', 'provider'])

    draft = updateMonitoringInstanceCreateDraft(draft, 'city', '大阪')
    draft = updateMonitoringInstanceCreateDraft(draft, 'provider', '本地')
    const restored = buildMonitoringInstanceCreateInput(draft)
    expect(restored).not.toHaveProperty('clear_fields')
    expect(Object.keys(restored)).toEqual(MONITORING_CREATE_BODY_KEYS)
    expect(restored).toMatchObject({ city: '大阪', provider: '本地', region: '关东' })
  })

  it('ignores a stale clear flag once the field has a value again', () => {
    const cleared = updateMonitoringInstanceCreateDraft(
      monitoringInstanceCreateDraftFromDetail(detailFixture()),
      'region',
      '',
    )
    const input = buildMonitoringInstanceCreateInput({ ...cleared, region: 'Osaka' })
    expect(input.region).toBe('Osaka')
    expect(input).not.toHaveProperty('clear_fields')
    expect(Object.keys(input)).toEqual(MONITORING_CREATE_BODY_KEYS)
  })
})
