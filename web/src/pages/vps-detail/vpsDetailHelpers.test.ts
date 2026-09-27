import { describe, expect, it } from 'vitest'

import type { VPSAssetDetail } from '../../lib/types'
import {
  compareDecisionDraft,
  buildFactEditInput,
  compareFactDraftAgainstLatest,
  decisionDraftAlreadySatisfied,
  detailToFactEditForm,
  mergeFactDraftWithLatest,
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
