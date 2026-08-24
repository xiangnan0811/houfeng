import { createElement } from 'react'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { UnifiedTimeline } from '../components/UnifiedTimeline'
import { ApiError } from './apiRequest'
import {
  archiveRecord,
  completeAttachmentUpload,
  captureEvidencePreview,
  createAttachmentUpload,
  createRecord,
  createRecordDraft,
  createRecordRevision,
  discardRecordDraft,
  evaluateFixedComparison,
  executeRecordPermanentDeletion,
  getAttachmentContent,
  getAttachmentMetadata,
  getEvidenceSnapshot,
  getRecord,
  getRecordDeletionOperation,
  getRecordDraft,
  getRecordRevision,
  getVPSOverview,
  InvalidVPSOverviewResponseError,
  listRecordDrafts,
  listRecordRevisions,
  listRecords,
  patchRecordDraft,
  previewRecordPermanentDeletion,
  resolveComparisonCandidates,
  restoreRecord,
  restoreRecordRevision,
  saveComparisonRecord,
  saveComparisonRevision,
  searchRecords,
  uploadAttachmentContent,
  listSubjectActivity,
} from './recordsApi'
import type {
  AttachmentMetadata,
  AttachmentUploadCompletion,
  AttachmentUploadSession,
  EvidenceCapturePreview,
  EvidenceCapturePreviewInput,
  EvidenceSnapshotRead,
  CreateRecordDraftInput,
  PublishRecordInput,
  PublishRecordRevisionInput,
  RecordDeletionExecuteInput,
  RecordDeletionOperation,
  RecordDeletionPreview,
  RecordDetail,
  RecordDraft,
  RecordDraftListResponse,
  RecordDraftPayload,
  RecordLifecycleResult,
  RecordListResponse,
  RecordMutationResult,
  RecordSearchResponse,
  RecordRevision,
  RecordRevisionListResponse,
  RecordSubjectReference,
  ComparisonCandidateResponse,
  ComparisonEvaluateResponse,
  VPSOverview,
} from './types'

const requestDefaults = {
  headers: { Accept: 'application/json' },
  cache: 'no-store',
  credentials: 'include',
}

function mockResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const subjectReference = {
  registry_version: 1,
  kind: 'vps',
  role: 'affected',
  source_id: 'vps_contract',
  primary: true,
} satisfies RecordSubjectReference

const payload = {
  title: 'Provider incident',
  body_markdown: '# Incident',
  markdown_dialect_version: 1,
  record_type: 'troubleshooting',
  business_status: 'investigating',
  impact_level: 'high',
  occurred_at: '2026-08-03T10:00:00Z',
  visibility: {
    kind: 'restricted',
    allowed_roles: ['project_admin'],
    allowed_group_ids: ['rag_ops'],
  },
  subjects: [subjectReference],
  tags: ['provider'],
  attachment_ids: ['att_contract_first', 'att_contract_second'],
  owner_id: 'usr_owner',
  participant_ids: ['usr_participant'],
  follow_up_at: '2026-08-04T10:00:00Z',
  template: { id: 'tpl_incident', version: 1 },
  save_reason: 'investigation updated',
} satisfies RecordDraftPayload

const revision = {
  record_id: 'rec_contract',
  revision_id: 'rrv_contract',
  revision_no: 1,
  title: payload.title,
  body_markdown: payload.body_markdown,
  markdown_dialect_version: 1,
  record_type: 'troubleshooting',
  business_status: 'investigating',
  status_group: 'in_progress',
  impact_level: 'high',
  occurred_at: payload.occurred_at,
  visibility: payload.visibility,
  subjects: [{
    ...subjectReference,
    identity: {
      display_name: 'Contract VPS',
      provider: 'Example Cloud',
      region: 'ap-east',
      purpose: 'control-plane',
    },
  }],
  tags: payload.tags,
  attachment_ids: payload.attachment_ids,
  owner_id: payload.owner_id,
  participants: [{ participant_id: 'usr_participant', display_name: 'Operator' }],
  follow_up_at: payload.follow_up_at,
  template: payload.template,
  author_id: 'usr_author',
  save_reason: payload.save_reason,
  created_at: '2026-08-03T10:10:00Z',
} satisfies RecordRevision

const record = {
  record_id: revision.record_id,
  lifecycle: 'active',
  current_revision_id: revision.revision_id,
  lock_version: 1,
  authorization_epoch: 1,
  current: revision,
  capabilities: {
    read: true,
    update: true,
    archive: true,
    restore: false,
    draft: true,
    permanent_delete: false,
  },
  created_at: revision.created_at,
  updated_at: revision.created_at,
} satisfies RecordDetail

const draft = {
  draft_id: 'rdf_contract',
  record_id: record.record_id,
  base_revision_id: revision.revision_id,
  payload,
  version: 2,
  etag: 'rdt1_contract',
  warning_at: '2026-10-25T10:00:00Z',
  created_at: '2026-08-03T10:00:00Z',
  updated_at: '2026-08-03T10:10:00Z',
  expires_at: '2026-11-01T10:00:00Z',
} satisfies RecordDraft

type EvidenceEnvelopeFixture = Omit<
  EvidenceCapturePreview,
  'capture_intent_id' | 'estimated_canonical_bytes' | 'previewed_at' | 'valid_until'
>

const evidenceEnvelope: EvidenceEnvelopeFixture = {
  record_id: 'rec_contract',
  snapshot_id: 'evs_contract',
  kind: 'monitoring.host',
  schema_version: 1,
  subject: { type: 'vps', id: 'vps_contract', display_name: 'Contract VPS' },
  source: { type: 'monitoring_instance', id: 'mon_contract', display_name: 'Primary monitor' },
  requested_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
  actual_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
  observed_at: '2026-08-16T02:00:00Z',
  source_revision: 'source-revision',
  source_watermark: 'source-watermark',
  producer_version: 'producer-v1',
  calculation_version: 'calculation-v1',
  units: { status: 'applicable', values: { cpu_usage_pct: 'percent' } },
  quality: {
    status: 'complete',
    sample_count: 12,
    gap_count: 0,
    maintenance_count: 0,
    backfilled_count: 0,
    bucket_count: 12,
    data_point_count: 12,
    peak_count: 1,
    truncated: false,
    partial: false,
  },
  sensitivity: 'normal',
  actual_precision_seconds: 300,
  bucket_width_seconds: 300,
  quota: { status: 'allowed' },
  retention: {
    immutable: true,
    scope: 'record_revision',
    source_deletion: 'snapshot_retained_source_unavailable',
  },
  redaction: [],
  renderer_version: 'monitoring_host_v1',
}

const evidencePreviewResponse = {
  ...evidenceEnvelope,
  capture_intent_id: 'eci_contract',
  estimated_canonical_bytes: 4096,
  previewed_at: '2026-08-16T02:00:01Z',
  valid_until: '2026-08-16T02:05:01Z',
} satisfies EvidenceCapturePreview

const evidenceReadResponse = {
  ...evidenceEnvelope,
  captured_at: '2026-08-16T02:00:01Z',
  referenced_at: '2026-08-16T02:01:00Z',
  source_available: true,
  title: 'Monitoring host evidence',
  read_model: { version: 'monitoring_host_read_model/v1' },
} satisfies EvidenceSnapshotRead

function vpsOverviewResponse(): VPSOverview {
  const ready = {
    state: 'ready' as const,
    observed_at: '2026-08-20T08:59:00Z',
    last_success_at: '2026-08-20T08:59:00Z',
    reason_code: '',
  }
  return {
    generated_at: '2026-08-20T09:00:00Z',
    identity: {
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
      provider_name: 'Example Cloud',
      product_name: 'VPS',
      country: 'JP',
      region: 'Tokyo',
      city: 'Tokyo',
      datacenter: 'TK1',
      ipv4: '192.0.2.10',
      ipv6: '',
      lifecycle_status: '在用',
      usage_status: '生产',
      renewal_decision: '续费',
      importance: '高',
      labels: ['edge'],
      updated_at: '2026-08-20T08:58:00Z',
    },
    anomalies: [{
      rule_id: 'ip_quality.stale.v1',
      severity: 'notice',
      title: 'IP 质量证据过期',
      source: 'ip_quality',
      event_at: '2026-08-19T09:00:00Z',
      primary_action: {
        id: 'open_ip_quality',
        label: '查看 IP 质量',
        route: '/vps/vps_001/ip-quality',
      },
      secondary_actions: [],
    }],
    summary: {
      overall: { status: '需要关注', detail: '存在陈旧证据', section: { ...ready } },
      monitoring: { status: '正常', section: { ...ready } },
      ip_quality: {
        status: '未知',
        section: {
          state: 'stale',
          observed_at: '2026-08-19T09:00:00Z',
          last_success_at: '2026-08-19T09:00:00Z',
          reason_code: 'ip_quality_stale',
        },
      },
      renewal: { status: '续费', section: { ...ready } },
    },
    recent_activity: {
      section: { ...ready },
      items: [{
        activity_id: 'act_recent',
        event_kind: 'record_created',
        event_at: '2026-08-19T12:00:00Z',
        recorded_at: '2026-08-19T12:00:01Z',
        source_kind: 'record_domain',
        backfilled: false,
        actor: { actor_id: 'usr_operator', display_name: 'Operator' },
        subjects: [{
          kind: 'vps',
          source_id: 'vps_001',
          role: 'affected',
          primary: true,
          identity: { display_name: 'Tokyo Edge' },
          live_route: '/vps/vps_001',
          tombstoned: false,
        }],
        presentation: { version: 1, title: '创建记录', summary: '运维记录已创建' },
        corrects_activity_id: 'act_previous',
      }],
      snapshot_cursor: 'snap-opaque',
    },
    facts: [{ key: 'ipv4', label: 'IPv4', value: '192.0.2.10' }],
    relations: [
      {
        kind: 'monitoring_instances',
        count: 1,
        status: '正常',
        label: '监控实例',
        section: { ...ready },
      },
      {
        kind: 'subscriptions',
        count: 1,
        status: '续费中',
        route: '/subscriptions?vps_id=vps_001',
        label: '订阅',
        section: { ...ready },
      },
      {
        kind: 'services',
        count: 1,
        status: 'active',
        label: '服务',
        section: { ...ready },
      },
      {
        kind: 'domains',
        count: 1,
        status: 'active',
        label: '域名',
        section: { ...ready },
      },
    ],
    capabilities: ['records_v2_read'],
  }
}

function mutateVPSOverview(mutator: (wire: Record<string, unknown>) => void): unknown {
  const wire = structuredClone(vpsOverviewResponse()) as unknown as Record<string, unknown>
  mutator(wire)
  return wire
}

function fixtureObject(value: unknown): Record<string, unknown> {
  return value as Record<string, unknown>
}

function fixtureArray(value: unknown): unknown[] {
  return value as unknown[]
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Records API transport', () => {
  it('rejects an empty successful VPS overview instead of synthesizing a DTO', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, {}))

    await expect(getVPSOverview('vps_001')).rejects.toMatchObject({
      name: 'InvalidVPSOverviewResponseError',
      reason: 'invalid_shape',
    })
  })

  it('projects a complete VPS overview through an allowlist and accepts additive fields', async () => {
    const expected = vpsOverviewResponse()
    expected.identity.vps_id = 'vps 001/edge'
    const wire = mutateVPSOverview((value) => {
      fixtureObject(value.identity).vps_id = 'vps 001/edge'
      value.internal_checkpoint = 'must-not-leave-wire'
      fixtureObject(value.identity).provider_account = 'must-not-leave-wire'
      const anomaly = fixtureObject(fixtureArray(value.anomalies)[0])
      anomaly.debug = { raw: 'must-not-leave-wire' }
      fixtureObject(anomaly.primary_action).internal_destination = '/private'
      const item = fixtureObject(fixtureArray(fixtureObject(value.recent_activity).items)[0])
      item.projection_generation = 42
      fixtureObject(item.presentation).private_summary = 'must-not-leave-wire'
      fixtureObject(fixtureArray(value.relations)[0]).source_ids = ['mi_private']
    })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, wire))

    await expect(getVPSOverview('vps 001/edge')).resolves.toEqual(expected)
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps%20001%2Fedge/overview', requestDefaults)
  })

  it('strips non-authoritative overview activity fields before timeline presentation', async () => {
    const wire = mutateVPSOverview((value) => {
      const activity = fixtureObject(fixtureArray(fixtureObject(value.recent_activity).items)[0])
      activity.event_kind = 'evidence_captured'
      activity.source_kind = 'evidence_snapshot'
      activity.record_id = 'rec_private'
      activity.revision_id = 'rrv_private'
      activity.evidence_snapshot_id = 'evs_private'
      fixtureObject(activity.presentation).summary = ''
      const subject = fixtureObject(fixtureArray(activity.subjects)[0])
      const identity = fixtureObject(subject.identity)
      identity.provider = 'Example Cloud'
      identity.region = 'Tokyo'
      identity.purpose = 'edge'
      identity.evidence_snapshot_id = 'evs_identity_private'
      identity.coverage = 'private coverage'
      identity.bucket = 'private bucket'
      identity.quality = 'private quality'
      identity.private_key = 'private identity'
      Object.defineProperty(identity, '__proto__', {
        value: 'private prototype',
        enumerable: true,
        configurable: true,
        writable: true,
      })
    })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, wire))

    const overview = await getVPSOverview('vps_001')
    const item = overview.recent_activity.items[0]
    const identity = item?.subjects[0]?.identity
    expect(item).not.toHaveProperty('record_id')
    expect(item).not.toHaveProperty('revision_id')
    expect(item).not.toHaveProperty('evidence_snapshot_id')
    expect(identity).toEqual({
      display_name: 'Tokyo Edge',
      provider: 'Example Cloud',
      region: 'Tokyo',
      purpose: 'edge',
    })
    expect(Object.prototype.hasOwnProperty.call(identity, '__proto__')).toBe(false)

    render(createElement(
      MemoryRouter,
      null,
      createElement(UnifiedTimeline, { items: overview.recent_activity.items }),
    ))
    expect(screen.queryByRole('link', { name: '查看证据' })).not.toBeInTheDocument()
    expect(document.querySelector('.unified-timeline__evidence-meta')).toBeNull()
    expect(document.body).not.toHaveTextContent('private')
  })

  it('projects only the authoritative identity fields for each activity subject kind', async () => {
    const wire = mutateVPSOverview((value) => {
      const activity = fixtureObject(fixtureArray(fixtureObject(value.recent_activity).items)[0])
      activity.subjects = [
        {
          kind: 'vps',
          source_id: 'vps_001',
          role: 'affected',
          primary: true,
          identity: {
            display_name: 'Tokyo Edge',
            provider: 'Example Cloud',
            region: 'Tokyo',
            purpose: 'edge',
            version: 'must-strip',
          },
          tombstoned: false,
        },
        {
          kind: 'monitoring_instance',
          source_id: 'mi_001',
          role: 'context',
          primary: false,
          identity: {
            display_name: 'Tokyo Monitor',
            version: '1.2.3',
            provider: 'must-strip',
          },
          tombstoned: false,
        },
        {
          kind: 'target',
          source_id: 'tg_001',
          role: 'evidence_source',
          primary: false,
          identity: {
            display_name: 'HTTPS probe',
            target_type: 'https',
            quality: 'must-strip',
          },
          tombstoned: false,
        },
      ]
    })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, wire))

    const overview = await getVPSOverview('vps_001')
    expect(overview.recent_activity.items[0]?.subjects.map((subject) => subject.identity)).toEqual([
      {
        display_name: 'Tokyo Edge',
        provider: 'Example Cloud',
        region: 'Tokyo',
        purpose: 'edge',
      },
      { display_name: 'Tokyo Monitor', version: '1.2.3' },
      { display_name: 'HTTPS probe', target_type: 'https' },
    ])
  })

  it('accepts an empty activity identity map without inventing display data', async () => {
    const wire = mutateVPSOverview((value) => {
      const activity = fixtureObject(fixtureArray(fixtureObject(value.recent_activity).items)[0])
      const subject = fixtureObject(fixtureArray(activity.subjects)[0])
      subject.identity = {}
    })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, wire))

    const overview = await getVPSOverview('vps_001')
    expect(overview.recent_activity.items[0]?.subjects[0]?.identity).toEqual({})
  })

  it('decodes a valid capability-off overview without inventing records access', async () => {
    const wire = vpsOverviewResponse()
    wire.capabilities = []
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, wire))

    await expect(getVPSOverview('vps_001')).resolves.toEqual(wire)
  })

  it('rejects a valid overview whose identity belongs to another requested VPS', async () => {
    const wire = vpsOverviewResponse()
    wire.identity.vps_id = 'vps_private_other'
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, wire))

    const error = await getVPSOverview('vps_001').catch((reason: unknown) => reason)
    expect(error).toMatchObject({
      name: 'InvalidVPSOverviewResponseError',
      reason: 'invalid_shape',
    })
    expect(JSON.stringify(error)).not.toContain('vps_private_other')
  })

  it('maps malformed success JSON to a minimal typed error without retaining the payload', async () => {
    const raw = '{"token":"must-not-retain"'
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(raw, {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))

    const error = await getVPSOverview('vps_private').catch((reason: unknown) => reason)

    expect(error).toBeInstanceOf(InvalidVPSOverviewResponseError)
    expect(error).toMatchObject({ reason: 'malformed_json' })
    expect(JSON.stringify(error)).toBe('{"reason":"malformed_json"}')
    expect(String(error)).not.toContain(raw)
    expect(error).not.toHaveProperty('body')
    expect(error).not.toHaveProperty('value')
    expect(error).not.toHaveProperty('url')
    expect(error).not.toHaveProperty('cause')
  })

  it.each([
    ['null response', () => null],
    ['array response', () => []],
    ['missing identity', () => mutateVPSOverview((wire) => { delete wire.identity })],
    ['invalid identity labels', () => mutateVPSOverview((wire) => {
      fixtureObject(wire.identity).labels = ['edge', 7]
    })],
    ['invalid identity scalar', () => mutateVPSOverview((wire) => {
      fixtureObject(wire.identity).provider_name = null
    })],
    ['invalid generated timestamp', () => mutateVPSOverview((wire) => { wire.generated_at = '2026-02-30T09:00:00Z' })],
    ['missing summary cell', () => mutateVPSOverview((wire) => {
      delete fixtureObject(wire.summary).renewal
    })],
    ['invalid section enum', () => mutateVPSOverview((wire) => {
      const overall = fixtureObject(fixtureObject(wire.summary).overall)
      fixtureObject(overall.section).state = 'loading'
    })],
    ['invalid nullable section timestamp', () => mutateVPSOverview((wire) => {
      const overall = fixtureObject(fixtureObject(wire.summary).overall)
      fixtureObject(overall.section).observed_at = 'yesterday'
    })],
    ['invalid activity items collection', () => mutateVPSOverview((wire) => {
      fixtureObject(wire.recent_activity).items = null
    })],
    ['invalid activity event kind', () => mutateVPSOverview((wire) => {
      const activity = fixtureObject(fixtureArray(fixtureObject(wire.recent_activity).items)[0])
      activity.event_kind = 'command_output_exposed'
    })],
    ['invalid activity presentation version', () => mutateVPSOverview((wire) => {
      const activity = fixtureObject(fixtureArray(fixtureObject(wire.recent_activity).items)[0])
      fixtureObject(activity.presentation).version = 2
    })],
    ['invalid activity subject kind', () => mutateVPSOverview((wire) => {
      const activity = fixtureObject(fixtureArray(fixtureObject(wire.recent_activity).items)[0])
      fixtureObject(fixtureArray(activity.subjects)[0]).kind = 'service'
    })],
    ['invalid activity subject role', () => mutateVPSOverview((wire) => {
      const activity = fixtureObject(fixtureArray(fixtureObject(wire.recent_activity).items)[0])
      fixtureObject(fixtureArray(activity.subjects)[0]).role = 'owner'
    })],
    ['invalid activity identity value', () => mutateVPSOverview((wire) => {
      const activity = fixtureObject(fixtureArray(fixtureObject(wire.recent_activity).items)[0])
      const subject = fixtureObject(fixtureArray(activity.subjects)[0])
      fixtureObject(subject.identity).display_name = 42
    })],
    ['unknown anomaly rule', () => mutateVPSOverview((wire) => {
      fixtureObject(fixtureArray(wire.anomalies)[0]).rule_id = 'unknown.rule.v1'
    })],
    ['unknown anomaly action', () => mutateVPSOverview((wire) => {
      const anomaly = fixtureObject(fixtureArray(wire.anomalies)[0])
      fixtureObject(anomaly.primary_action).id = 'open_private_console'
    })],
    ['invalid anomaly route scalar', () => mutateVPSOverview((wire) => {
      const anomaly = fixtureObject(fixtureArray(wire.anomalies)[0])
      fixtureObject(anomaly.primary_action).route = 42
    })],
    ['invalid fact', () => mutateVPSOverview((wire) => {
      fixtureObject(fixtureArray(wire.facts)[0]).value = 42
    })],
    ['missing relation', () => mutateVPSOverview((wire) => {
      wire.relations = fixtureArray(wire.relations).slice(0, 3)
    })],
    ['out-of-order relations', () => mutateVPSOverview((wire) => {
      const relations = fixtureArray(wire.relations)
      ;[relations[0], relations[1]] = [relations[1], relations[0]]
    })],
    ['negative relation count', () => mutateVPSOverview((wire) => {
      fixtureObject(fixtureArray(wire.relations)[0]).count = -1
    })],
    ['fractional relation count', () => mutateVPSOverview((wire) => {
      fixtureObject(fixtureArray(wire.relations)[0]).count = 1.5
    })],
    ['unexpected command relation route', () => mutateVPSOverview((wire) => {
      fixtureObject(fixtureArray(wire.relations)[0]).route = '/monitoring'
    })],
    ['missing subscription relation route', () => mutateVPSOverview((wire) => {
      delete fixtureObject(fixtureArray(wire.relations)[1]).route
    })],
    ['invalid relation section', () => mutateVPSOverview((wire) => {
      const relation = fixtureObject(fixtureArray(wire.relations)[2])
      fixtureObject(relation.section).reason_code = false
    })],
    ['invalid capability', () => mutateVPSOverview((wire) => { wire.capabilities = ['records_v2_read', 3] })],
  ])('rejects an invalid VPS overview shape: %s', async (_caseName, candidate) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, candidate()))

    await expect(getVPSOverview('vps_001')).rejects.toMatchObject({
      name: 'InvalidVPSOverviewResponseError',
      reason: 'invalid_shape',
    })
  })

  it('preserves HTTP, network, abort and non-JSON transport errors unchanged', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(mockResponse(503, {
      code: 'upstream_timeout',
      message: 'private upstream detail',
    }))
    const apiError = await getVPSOverview('vps_001').catch((reason: unknown) => reason)
    expect(apiError).toBeInstanceOf(ApiError)
    expect(apiError).toMatchObject({ status: 503, code: 'upstream_timeout' })

    const networkError = new TypeError('network failed')
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(networkError)
    await expect(getVPSOverview('vps_001')).rejects.toBe(networkError)

    const abortError = new DOMException('request aborted', 'AbortError')
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(abortError)
    await expect(getVPSOverview('vps_001')).rejects.toBe(abortError)

    const bodyReadError = new Error('body stream failed')
    const response = mockResponse(200, vpsOverviewResponse())
    vi.spyOn(response, 'text').mockRejectedValue(bodyReadError)
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(response)
    await expect(getVPSOverview('vps_001')).rejects.toBe(bodyReadError)
  })

  it('captures an evidence preview with an allowlisted body and abort signal', async () => {
    const input = {
      record_id: 'rec_contract',
      kind: 'monitoring.host',
      schema_version: 1,
      source_type: 'monitoring_instance',
      source_id: 'mon /contract',
      requested_window: {
        start: '2026-08-16T01:00:00Z',
        end: '2026-08-16T02:00:00Z',
      },
      metrics: ['cpu_usage_pct'],
      precision_seconds: 300,
      sensitive_topology_fields: [],
      payload: 'must-not-leave-the-client',
      authorization: 'must-not-leave-the-client',
    } satisfies EvidenceCapturePreviewInput & { payload: string; authorization: string }
    const response = evidencePreviewResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(201, response))
    const controller = new AbortController()

    await expect(captureEvidencePreview(input, controller.signal)).resolves.toEqual(response)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [path, init] = fetchMock.mock.calls[0] ?? []
    expect(path).toBe('/api/evidence/capture-previews')
    expect(init).toMatchObject({
      method: 'POST',
      signal: controller.signal,
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    })
    expect(JSON.parse(String(init?.body))).toEqual({
      record_id: 'rec_contract',
      kind: 'monitoring.host',
      schema_version: 1,
      source_type: 'monitoring_instance',
      source_id: 'mon /contract',
      requested_window: input.requested_window,
      metrics: ['cpu_usage_pct'],
      precision_seconds: 300,
      sensitive_topology_fields: [],
    })
  })

  it('reads an encoded evidence snapshot through the shared records transport', async () => {
    const response = { ...evidenceReadResponse, snapshot_id: 'evs /contract' }
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))
    const controller = new AbortController()

    await expect(getEvidenceSnapshot('evs /contract', controller.signal)).resolves.toEqual(response)
    expect(fetchMock).toHaveBeenCalledWith('/api/evidence/evs%20%2Fcontract', {
      ...requestDefaults,
      signal: controller.signal,
    })
  })

  it.each([
    [409, 'evidence_preview_stale'],
    [503, 'evidence_service_unavailable'],
  ])('keeps evidence failure metadata allowlisted for HTTP %i', async (status, code) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(status, {
      code,
      message: 'evidence unavailable',
      metadata: 'must-not-enter-error',
      authorization: 'must-not-enter-error',
    }))

    await expect(captureEvidencePreview({
      kind: 'monitoring.host',
      schema_version: 1,
      source_type: 'monitoring_instance',
      source_id: 'mon_contract',
      requested_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
      metrics: ['cpu_usage_pct'],
      precision_seconds: 300,
      sensitive_topology_fields: [],
    })).rejects.toMatchObject({ status, code, message: 'evidence unavailable' })
  })

  it('preserves non-null ordered attachment IDs in draft and revision DTOs', () => {
    expect(draft.payload.attachment_ids).toEqual([
      'att_contract_first',
      'att_contract_second',
    ])
    expect(revision.attachment_ids).toEqual(draft.payload.attachment_ids)
    expect({ ...draft.payload, attachment_ids: [] }.attachment_ids).toEqual([])
  })

  it('follows a local attachment instruction through complete and metadata polling', async () => {
    const session = {
      upload_id: 'aup_contract',
      attachment_id: 'att_contract',
      state: 'created',
      expires_at: '2026-08-09T20:00:00Z',
      quota: {
        logical_bytes: 0,
        reserved_bytes: 12,
        physical_bytes: 0,
        effective_record_bytes: 12,
        project_warning: false,
      },
      target: {
        transport: 'local',
        upload_url: '/api/attachment-uploads/aup_contract/content',
        method: 'PUT',
        required_headers: ['X-Houfeng-Draft-ID', 'X-Content-SHA256'],
      },
    } satisfies AttachmentUploadSession
    const completion = {
      upload_id: session.upload_id,
      attachment_id: session.attachment_id,
      state: 'quarantined',
      quota: session.quota,
    } satisfies AttachmentUploadCompletion
    const metadata = {
      attachment_id: session.attachment_id,
      state: 'available',
      display_name: 'incident.txt',
      media_type: 'text/plain',
      size_bytes: 12,
      preview_available: true,
    } satisfies AttachmentMetadata
    const content = new Blob(['safe content'], { type: 'text/plain' })
    const sha256 = 'a'.repeat(64)
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(201, session))
      .mockResolvedValueOnce(mockResponse(200, {
        upload_id: session.upload_id,
        attachment_id: session.attachment_id,
        size_bytes: content.size,
        sha256,
      }))
      .mockResolvedValueOnce(mockResponse(202, completion))
      .mockResolvedValueOnce(mockResponse(200, metadata))

    await expect(createAttachmentUpload({
      draft_id: 'rdf_contract',
      display_name: metadata.display_name,
      media_type: metadata.media_type,
      declared_size_bytes: content.size,
    })).resolves.toEqual(session)
    await uploadAttachmentContent(session, 'rdf_contract', sha256, content)
    await expect(completeAttachmentUpload(session.upload_id, 'rdf_contract')).resolves.toEqual(completion)
    await expect(getAttachmentMetadata(session.attachment_id)).resolves.toEqual(metadata)

    expect(fetchMock).toHaveBeenNthCalledWith(2, session.target.upload_url, {
      cache: 'no-store',
      credentials: 'include',
      method: 'PUT',
      headers: {
        Accept: 'application/json',
        'X-Houfeng-Draft-ID': 'rdf_contract',
        'X-Content-SHA256': sha256,
      },
      body: content,
    })
  })

  it('uses an S3 instruction without forwarding first-party credentials', async () => {
    const session = {
      upload_id: 'aup_s3contract',
      attachment_id: 'att_s3contract',
      state: 'uploading',
      expires_at: '2026-08-09T20:00:00Z',
      quota: {
        logical_bytes: 0,
        reserved_bytes: 4,
        physical_bytes: 0,
        effective_record_bytes: 4,
        project_warning: false,
      },
      target: {
        transport: 's3',
        upload_url: 'https://objects.example.test/private-upload',
        method: 'PUT',
        required_headers: [],
        temporary_object_key: 'temporary/' + 'b'.repeat(64),
      },
    } satisfies AttachmentUploadSession
    const content = new Blob(['safe'], { type: 'text/plain' })
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 200 }))

    await uploadAttachmentContent(session, 'rdf_contract', 'c'.repeat(64), content)

    expect(fetchMock).toHaveBeenCalledWith(session.target.upload_url, {
      cache: 'no-store',
      credentials: 'omit',
      method: 'PUT',
      headers: {},
      body: content,
    })
  })

  it('fetches authorized attachment bytes through the shared transport', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('preview', {
      status: 200,
      headers: { 'Content-Type': 'text/plain' },
    }))

    const result = await getAttachmentContent('att /contract', 'preview')

    expect(result.type).toBe('text/plain')
    await expect(result.text()).resolves.toBe('preview')
    expect(fetchMock).toHaveBeenCalledWith('/api/attachments/att%20%2Fcontract/content?variant=preview', {
      cache: 'no-store',
      credentials: 'include',
      headers: { Accept: 'application/octet-stream' },
    })
  })

  it('decodes an opaque authorized attachment denial through the shared error contract', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(404, {
      code: 'resource_not_found',
      message: 'resource not found',
      field_errors: [],
    }))

    await expect(getAttachmentContent('att_denied')).rejects.toMatchObject({
      status: 404,
      code: 'resource_not_found',
      message: 'resource not found',
    })
  })

  it('fails a rejected S3 instruction without converting it into a successful upload', async () => {
    const session = {
      upload_id: 'aup_s3denied',
      attachment_id: 'att_s3denied',
      state: 'uploading',
      expires_at: '2026-08-09T20:00:00Z',
      quota: {
        logical_bytes: 0,
        reserved_bytes: 4,
        physical_bytes: 0,
        effective_record_bytes: 4,
        project_warning: false,
      },
      target: {
        transport: 's3',
        upload_url: 'https://objects.example.test/rejected-upload',
        method: 'PUT',
        required_headers: [],
        temporary_object_key: 'temporary/' + 'd'.repeat(64),
      },
    } satisfies AttachmentUploadSession
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 503 }))

    await expect(uploadAttachmentContent(
      session,
      'rdf_contract',
      'e'.repeat(64),
      new Blob(['safe']),
    )).rejects.toMatchObject({
      status: 503,
      message: 'Request failed: 503',
    })
  })

  it('normalizes record list filters and preserves the server cursor response', async () => {
    const response = { items: [record], next_cursor: ' cursor-next ' } satisfies RecordListResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))

    await expect(listRecords({
      sort: 'updated_at_desc',
      limit: 25,
      cursor: '  cursor-current  ',
    })).resolves.toEqual(response)

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/records?sort=updated_at_desc&limit=25&cursor=cursor-current',
      requestDefaults,
    )
  })

  it('repeats multi-value search filters and flattens subjects positionally', async () => {
    const response = {
      items: [record],
      next_cursor: 'search-cursor',
      generation: 4,
    } satisfies RecordSearchResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))

    await expect(searchRecords({
      q: '  磁盘 IO  ',
      type: ['troubleshooting', 'migration'],
      status_group: ['in_progress'],
      lifecycle: ['active'],
      tag: ['ops', 'disk'],
      subject: [
        { kind: 'vps', source_id: 'vps_alpha', role: 'affected', placement: 'primary' },
        { kind: 'vps', role: 'context' },
      ],
      follow_up: 'overdue',
      sort: 'updated_at_desc',
      limit: 25,
      cursor: '  search-cursor  ',
    })).resolves.toEqual(response)

    const [url] = fetchMock.mock.calls[0] ?? []
    const query = new URLSearchParams(String(url).split('?')[1] ?? '')
    expect(String(url).startsWith('/api/records/search?')).toBe(true)
    expect(query.get('q')).toBe('磁盘 IO')
    expect(query.getAll('type')).toEqual(['troubleshooting', 'migration'])
    expect(query.getAll('tag')).toEqual(['ops', 'disk'])
    // Trailing empty segments are kept so the server reads each position by index.
    expect(query.getAll('subject')).toEqual(['vps:vps_alpha:affected:primary', 'vps::context:'])
    expect(query.get('follow_up')).toBe('overdue')
    expect(query.get('limit')).toBe('25')
    expect(query.get('cursor')).toBe('search-cursor')
  })

  it('omits absent search filters instead of sending empty parameters', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(
      () => Promise.resolve(mockResponse(200, { items: [], generation: 1 } satisfies RecordSearchResponse)),
    )

    await searchRecords()
    await searchRecords({ q: '   ', type: [], subject: [] })

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/records/search', requestDefaults)
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/records/search', requestDefaults)
  })

  it('sends the draft list cursor and preserves the one the server returns', async () => {
    const response = {
      items: [],
      next_cursor: ' draft-cursor-next ',
    } satisfies RecordDraftListResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))

    await expect(listRecordDrafts({
      limit: 25,
      cursor: '  draft-cursor-current  ',
    })).resolves.toEqual(response)

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/record-drafts?limit=25&cursor=draft-cursor-current',
      requestDefaults,
    )
  })

  it('uses encoded record and revision read paths', async () => {
    const revisionList = { items: [revision] } satisfies RecordRevisionListResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(200, record))
      .mockResolvedValueOnce(mockResponse(200, revisionList))
      .mockResolvedValueOnce(mockResponse(200, revision))

    await expect(getRecord('rec /contract')).resolves.toEqual(record)
    await expect(listRecordRevisions('rec /contract', { limit: 20 })).resolves.toEqual(revisionList)
    await expect(getRecordRevision('rec /contract', 'rrv /contract')).resolves.toEqual(revision)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/records/rec%20%2Fcontract', requestDefaults)
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/records/rec%20%2Fcontract/revisions?limit=20',
      requestDefaults,
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/records/rec%20%2Fcontract/revisions/rrv%20%2Fcontract',
      requestDefaults,
    )
  })

  it('keeps formal mutation idempotency headers and bodies exact', async () => {
    const mutation = {
      record_id: record.record_id,
      revision_id: revision.revision_id,
      revision_no: 1,
      lock_version: 1,
      authorization_epoch: 1,
      lifecycle: 'active',
      created: true,
      replayed: false,
      committed_at: revision.created_at,
    } satisfies RecordMutationResult
    const lifecycle = {
      record_id: record.record_id,
      current_revision_id: revision.revision_id,
      lock_version: 2,
      authorization_epoch: 2,
      lifecycle: 'archived',
      replayed: false,
      changed_at: '2026-08-03T11:00:00Z',
    } satisfies RecordLifecycleResult
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(201, mutation))
      .mockResolvedValueOnce(mockResponse(201, mutation))
      .mockResolvedValueOnce(mockResponse(201, mutation))
      .mockResolvedValueOnce(mockResponse(200, lifecycle))
      .mockResolvedValueOnce(mockResponse(200, { ...lifecycle, lifecycle: 'active' }))
    const createInput = {
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
    } satisfies PublishRecordInput
    const revisionInput = {
      ...createInput,
      base_revision_id: revision.revision_id,
      lock_version: 1,
      authorization_epoch: 1,
    } satisfies PublishRecordRevisionInput

    await createRecord(createInput, 'create-key')
    await createRecordRevision('rec /contract', revisionInput, 'revision-key')
    await restoreRecordRevision('rec /contract', 'rrv /contract', { save_reason: 'restore exact revision' }, 'restore-revision-key')
    await archiveRecord('rec /contract', 'archive-key')
    await restoreRecord('rec /contract', 'restore-record-key')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/records', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'Idempotency-Key': 'create-key',
      },
      body: JSON.stringify(createInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/records/rec%20%2Fcontract/revisions', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'Idempotency-Key': 'revision-key',
      },
      body: JSON.stringify(revisionInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/records/rec%20%2Fcontract/revisions/rrv%20%2Fcontract/restore',
      {
        ...requestDefaults,
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'Idempotency-Key': 'restore-revision-key',
        },
        body: JSON.stringify({ save_reason: 'restore exact revision' }),
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/records/rec%20%2Fcontract/archive', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Idempotency-Key': 'archive-key',
      },
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/records/rec%20%2Fcontract/restore', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Idempotency-Key': 'restore-record-key',
      },
    })
  })

  it('keeps draft routing fields on create and out of PATCH', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(
      () => Promise.resolve(mockResponse(200, draft)),
    )
    const createInput = {
      record_id: record.record_id,
      base_revision_id: revision.revision_id,
      payload,
    } satisfies CreateRecordDraftInput

    await listRecordDrafts({ limit: 25 })
    await createRecordDraft(createInput)
    await getRecordDraft('rdf /contract')
    await patchRecordDraft('rdf /contract', { payload }, draft.etag)
    await discardRecordDraft('rdf /contract')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/record-drafts?limit=25', requestDefaults)
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/record-drafts', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(createInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/record-drafts/rdf%20%2Fcontract', requestDefaults)
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/record-drafts/rdf%20%2Fcontract', {
      ...requestDefaults,
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'If-Match': draft.etag,
      },
      body: JSON.stringify({ payload }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/record-drafts/rdf%20%2Fcontract', {
      ...requestDefaults,
      method: 'DELETE',
    })
  })

  it('omits both routing fields when creating a new-record draft', async () => {
    const input = { payload } satisfies CreateRecordDraftInput
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(201, draft))

    await createRecordDraft(input)

    expect(fetchMock).toHaveBeenCalledWith('/api/record-drafts', {
      ...requestDefaults,
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ payload }),
    })
  })

  it('keeps the deletion request token header-only and reads operation status separately', async () => {
    const preview = {
      reservation_id: 'drs_contract',
      deletion_request_token: 'drt1_contract',
      expires_at: '2026-08-03T10:20:00Z',
      online_purge_scopes: [
        'record_core',
        'record_attachments',
        'record_evidence',
        'record_markdown_client',
        'record_search',
        'record_activity_projection',
        'record_comparison',
        'record_collaboration',
        'record_portability',
      ],
      surviving_copies: [{
        scope: 'record_attachments',
        kind: 'other_record',
        copy_count: 2,
      }],
      managed_backup: {
        retained_copy_count: 0,
        maximum_retention_days: 0,
        latest_expires_at: null,
      },
      ledger_health: 'healthy',
    } satisfies RecordDeletionPreview
    const operation = {
      operation_id: 'rpo_contract',
      state: 'witness_pending',
    } satisfies RecordDeletionOperation
    const executeInput = {
      reservation_id: preview.reservation_id,
      deletion_request_token: preview.deletion_request_token,
    } satisfies RecordDeletionExecuteInput
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(200, preview))
      .mockResolvedValueOnce(mockResponse(202, operation))
      .mockResolvedValueOnce(mockResponse(202, operation))

    await previewRecordPermanentDeletion('rec /contract')
    await executeRecordPermanentDeletion('rec /contract', executeInput)
    await getRecordDeletionOperation('rpo /contract')

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/records/rec%20%2Fcontract/permanent-delete-preview',
      { ...requestDefaults, method: 'POST' },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/records/rec%20%2Fcontract/permanent-delete',
      {
        ...requestDefaults,
        method: 'POST',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'Idempotency-Key': preview.deletion_request_token,
        },
        body: JSON.stringify({ reservation_id: preview.reservation_id }),
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/api/record-deletions/rpo%20%2Fcontract',
      requestDefaults,
    )
  })

  it('surfaces stable 404, conflict recovery and unavailable codes', async () => {
    const recovery = {
      server_revision_id: revision.revision_id,
      server_lock_version: 4,
      server_authorization_epoch: 5,
      draft,
    }
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(404, {
        code: 'resource_not_found',
        message: 'resource not found',
        field_errors: [],
      }))
      .mockResolvedValueOnce(mockResponse(409, {
        code: 'record_revision_conflict',
        message: 'record revision changed',
        field_errors: [],
        recovery,
      }))
      .mockResolvedValueOnce(mockResponse(503, {
        code: 'deletion_safety_unavailable',
        message: 'deletion safety unavailable',
        field_errors: [],
      }))

    await expect(getRecord(record.record_id)).rejects.toMatchObject({
      status: 404,
      code: 'resource_not_found',
      field_errors: [],
    })
    const conflict = await createRecordRevision(record.record_id, {
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
      base_revision_id: revision.revision_id,
      lock_version: 1,
      authorization_epoch: 1,
    }, 'conflict-key').catch((reason: unknown) => reason)
    expect(conflict).toBeInstanceOf(ApiError)
    expect(conflict).toMatchObject({
      status: 409,
      code: 'record_revision_conflict',
      recovery,
    })
    await expect(previewRecordPermanentDeletion(record.record_id)).rejects.toMatchObject({
      status: 503,
      code: 'deletion_safety_unavailable',
      recovery: undefined,
    })
  })

  it('encodes subject activity filters with default omission and multi-value OR', async () => {
    const wire = {
      subject: {
        kind: 'vps',
        source_id: 'vps_001',
        identity: { display_name: 'Edge' },
        status: 'live',
      },
      view: 'records',
      snapshot_cursor: ' snap-opaque ',
      freshness: {
        state: 'ready',
        visible_observed_at: null,
        new_items_available: true,
        reason_code: '',
      },
      items: null,
      source_statuses: null,
      next_cursor: ' next-opaque ',
      projection_generation: 9,
      as_of_ingest_sequence: 42,
    }
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(wire), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    await expect(listSubjectActivity('vps', 'vps 001/edge', {
      view: 'activity',
      source: ['record_domain', 'command_audit'],
      event_kind: ['record_revised'],
      versions: 'history',
      limit: 50,
      cursor: '  opaque-cursor  ',
    })).resolves.toEqual({
      subject: {
        kind: 'vps',
        source_id: 'vps_001',
        identity: { display_name: 'Edge' },
        status: 'live',
      },
      view: 'records',
      snapshot_cursor: 'snap-opaque',
      freshness: {
        state: 'ready',
        visible_observed_at: null,
        new_items_available: true,
        reason_code: '',
      },
      items: [],
      source_statuses: [],
      next_cursor: 'next-opaque',
    })

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/subjects/vps/vps%20001%2Fedge/activity'
        + '?source=record_domain&source=command_audit'
        + '&event_kind=record_revised&cursor=opaque-cursor',
      requestDefaults,
    )
  })

  it('omits default activity view and encodes non-default view/limit/versions', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({
        subject: { kind: 'target', source_id: 'tg_001', identity: {}, status: 'live' },
        view: 'evidence',
        snapshot_cursor: 's',
        freshness: {
          state: 'ready',
          visible_observed_at: null,
          new_items_available: false,
          reason_code: '',
        },
        items: [],
        source_statuses: [],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    await listSubjectActivity('target', 'tg_001', {
      view: 'evidence',
      versions: 'current',
      limit: 25,
    })

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/subjects/target/tg_001/activity?view=evidence&versions=current&limit=25',
      requestDefaults,
    )
  })

  it('resolves comparison candidates with an allowlisted body and abort signal', async () => {
    const response = {
      subjects: [
        { kind: 'vps', id: 'vps_0123456789abcdef' },
        { kind: 'vps', id: 'vps_0123456789abcde0' },
      ],
      candidates: [{
        subject: { kind: 'vps', id: 'vps_0123456789abcdef' },
        snapshot_id: 'evs_candidate',
        record_id: 'rec_candidate',
        revision_ids: ['rrv_candidate'],
        kind: 'monitoring.host',
        schema_version: 1,
        canonical_hash: 'ab'.repeat(32),
        requested_window: { start: '2026-08-10T11:00:00Z', end: '2026-08-10T12:00:00Z' },
        actual_window: { start: '2026-08-10T11:00:00Z', end: '2026-08-10T12:00:00Z' },
        quality_status: 'complete',
        captured_at: '2026-08-10T12:00:00Z',
        recommendation: 'nearest_window',
      }],
    } satisfies ComparisonCandidateResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))
    const controller = new AbortController()

    const firstCandidate = response.candidates[0]
    if (!firstCandidate) throw new Error('expected a comparison candidate fixture')
    await expect(resolveComparisonCandidates({
      subjects: response.subjects,
      requested_window: firstCandidate.requested_window,
      kinds: [{ kind: 'monitoring.host', schema_version: 1 }],
      payload: 'must-not-leave-the-client',
      token: 'must-not-leave-the-client',
    } as never, controller.signal)).resolves.toEqual(response)

    const [, init] = fetchMock.mock.calls[0] ?? []
    expect(fetchMock).toHaveBeenCalledWith('/api/evidence/comparison-candidates', expect.objectContaining({
      method: 'POST',
      signal: controller.signal,
    }))
    expect(JSON.parse(String(init?.body))).toEqual({
      subjects: response.subjects,
      requested_window: firstCandidate.requested_window,
      kinds: [{ kind: 'monitoring.host', schema_version: 1 }],
    })
  })

  it('evaluates a fixed comparison without client payload or secrets', async () => {
    const response = {
      digest: 'cd'.repeat(32),
      items: [{
        snapshot_id: 'evs_fixeda',
        canonical_hash: '11'.repeat(32),
        kind: 'command.audit',
        schema_version: 1,
        revision_context: 'not_applicable',
      }],
      review: [],
      available_kinds: [{ kind: 'command.audit', schema_version: 1 }],
      pairwise: [],
      series: [],
      save_eligibility: { eligible: true, blockers: [] },
      comparison_intent: {
        token: 'cmp1.valid.payload.mac',
        key_id: 'cmp_key',
        issued_at: '2026-08-20T10:00:00Z',
        expires_at: '2026-08-20T10:15:00Z',
      },
    } satisfies ComparisonEvaluateResponse
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(mockResponse(200, response))
    const controller = new AbortController()

    await expect(evaluateFixedComparison({
      items: [
        { snapshot_id: 'evs_fixeda', payload: 'nope' } as never,
        { record_id: 'rec_fixedb', revision_id: 'rrv_fixedb', snapshot_ids: ['evs_fixedb'] },
      ],
      baseline_index: 0,
      alignment: 'actual_coverage',
      requested_window: { start: '2026-08-10T11:00:00Z', end: '2026-08-10T12:00:00Z' },
      tolerance_seconds: 60,
      bucket_seconds: 300,
      detail: { kind: 'monitoring.probe', schema_version: 2, metric: 'latency_ms' },
    }, controller.signal)).resolves.toEqual(response)

    const [, init] = fetchMock.mock.calls[0] ?? []
    expect(fetchMock).toHaveBeenCalledWith('/api/evidence/comparisons', expect.objectContaining({
      method: 'POST',
      signal: controller.signal,
    }))
    expect(JSON.parse(String(init?.body))).toEqual({
      items: [
        { snapshot_id: 'evs_fixeda' },
        { record_id: 'rec_fixedb', revision_id: 'rrv_fixedb', snapshot_ids: ['evs_fixedb'] },
      ],
      baseline_index: 0,
      alignment: 'actual_coverage',
      requested_window: { start: '2026-08-10T11:00:00Z', end: '2026-08-10T12:00:00Z' },
      tolerance_seconds: 60,
      bucket_seconds: 300,
      detail: { kind: 'monitoring.probe', schema_version: 2, metric: 'latency_ms' },
    })
  })

  it('saves a comparison through records create/revision without client evidence items', async () => {
    const mutation = {
      record_id: 'rec_comparisonsave',
      revision_id: 'rrv_comparisonsave',
      revision_no: 1,
      lock_version: 1,
      authorization_epoch: 1,
      lifecycle: 'active',
      created: true,
      replayed: false,
      committed_at: '2026-08-20T10:00:00Z',
    } satisfies RecordMutationResult
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(mockResponse(201, mutation))
      .mockResolvedValueOnce(mockResponse(201, mutation))

    await saveComparisonRecord({
      record_id: 'rec_comparisonsave',
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
      comparison_intent: 'cmp1.valid.payload.mac',
      evidence_items: [{ existing_snapshot_id: 'evs_client' }],
      payload: { body_markdown: 'nope' },
    } as never, 'comparison-save-key')
    await saveComparisonRevision('rec /compare', {
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
      base_revision_id: revision.revision_id,
      lock_version: 1,
      authorization_epoch: 1,
      comparison_intent: 'cmp1.valid.payload.mac',
    }, 'comparison-revision-key')

    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      record_id: 'rec_comparisonsave',
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
      comparison_intent: 'cmp1.valid.payload.mac',
    })
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({
      headers: expect.objectContaining({ 'Idempotency-Key': 'comparison-save-key' }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/records/rec%20%2Fcompare/revisions', expect.objectContaining({
      method: 'POST',
    }))
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
      base_revision_id: revision.revision_id,
      lock_version: 1,
      authorization_epoch: 1,
      comparison_intent: 'cmp1.valid.payload.mac',
    })
  })
})
