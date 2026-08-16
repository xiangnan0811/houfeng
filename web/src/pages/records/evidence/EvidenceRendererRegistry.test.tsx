import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  EvidenceRendererRegistry,
} from './EvidenceRendererRegistry'
import {
  decodeMonitoringEvidenceReadModel,
  type EvidenceReadModelQuality,
  type MonitoringEvidenceReadModel,
} from './evidenceReadModels'
import { createEvidenceRendererRegistry } from './evidenceRendererRegistryCore'

afterEach(() => vi.restoreAllMocks())

const quality = {
  status: 'complete',
  partial: false,
  truncated: false,
  sample_count: 2,
  maintenance_count: 0,
  backfilled_count: 0,
  bucket_count: 2,
  gap_count: 0,
  peak_count: 0,
  data_point_count: 2,
} satisfies EvidenceReadModelQuality

const baseEvidence = {
  record_id: 'rec_renderer',
  snapshot_id: 'evs_renderer',
  kind: 'ip_quality.report',
  schema_version: 1,
  subject: { type: 'vps', id: 'vps_renderer', display_name: '边缘节点' },
  source: { type: 'ip_quality_report', id: 'ipq_renderer', display_name: '质量报告' },
  requested_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
  actual_window: { start: '2026-08-16T01:00:00Z', end: '2026-08-16T02:00:00Z' },
  observed_at: '2026-08-16T02:00:00Z',
  captured_at: '2026-08-16T02:00:01Z',
  referenced_at: '2026-08-16T02:01:00Z',
  source_revision: 'source-revision',
  source_watermark: 'source-watermark',
  producer_version: 'producer-v1',
  calculation_version: 'calculation-v1',
  units: { status: 'not_applicable', values: {}, reason: 'not applicable' },
  quality,
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
  source_available: true,
  renderer_version: 'ip_quality_report_v1',
  title: 'IP quality report',
  read_model: {
    version: 'ip_quality_report_read_model/v1',
    report_id: 'ipq_safe_report',
    observed_at: '2026-08-16T01:59:00Z',
    received_at: '2026-08-16T01:59:01Z',
    ip_version: 4,
    status: 'success',
    stale: false,
    stale_after_seconds: 604800,
    risk_level: 'low',
    coverage: {
      expected_provider_count: 1,
      successful_provider_count: 1,
      failed_provider_count: 0,
      skipped_provider_count: 0,
      not_configured_provider_count: 0,
      expected_service_count: 1,
      successful_service_count: 1,
      failed_service_count: 0,
      skipped_service_count: 0,
      not_configured_service_count: 0,
    },
    providers: [{
      provider: 'provider-safe',
      status: 'success',
      source_type: 'default',
      latency_ms: 25,
      usage_type: 'isp',
      company_type: 'hosting',
      risk_level: 'low',
      risk_score: '1',
      is_proxy: false,
      is_tor: false,
      is_vpn: false,
      is_server: false,
      is_abuser: false,
      is_robot: false,
      error_code: '',
    }],
    services: [{
      service: 'service-safe',
      source: 'default',
      status: 'unlocked',
      probe_status: 'success',
      latency_ms: 31,
      unlock_type: 'full',
      error_code: '',
    }],
    quality: { ...quality, sample_count: 1, bucket_count: 1, data_point_count: 3 },
  },
}

function monitoringReadModel(
  version: MonitoringEvidenceReadModel['version'],
): MonitoringEvidenceReadModel {
  const probe = version === 'monitoring_probe_read_model/v1'
  const metric = probe
    ? { name: 'latency_ms', unit: 'ms', average: 42, min: 42, max: 42 }
    : { name: 'cpu_usage_pct', unit: 'percent', average: 42, min: 42, max: 42 }
  return {
    version,
    requested_start: '2026-08-16T01:00:00Z',
    requested_end: '2026-08-16T01:15:00Z',
    coverage_start: '2026-08-16T01:00:00Z',
    coverage_end: '2026-08-16T01:15:00Z',
    actual_precision_seconds: 300,
    buckets: [
      {
        series_id: 'series-safe',
        series_kind: probe ? 'http' : 'host',
        start: '2026-08-16T01:00:00Z',
        end: '2026-08-16T01:05:00Z',
        source_layer: 'raw',
        source_granularity_seconds: 300,
        sample_count: 1,
        maintenance_count: 0,
        backfilled_count: 0,
        metrics: [metric],
      },
      {
        series_id: 'series-safe',
        series_kind: probe ? 'http' : 'host',
        start: '2026-08-16T01:10:00Z',
        end: '2026-08-16T01:15:00Z',
        source_layer: 'raw',
        source_granularity_seconds: 300,
        sample_count: 1,
        maintenance_count: 0,
        backfilled_count: 0,
        metrics: [{ ...metric, average: 61, min: 61, max: 61 }],
      },
    ],
    gaps: [{
      series_id: 'series-safe',
      start: '2026-08-16T01:05:00Z',
      end: '2026-08-16T01:10:00Z',
    }],
    peaks: [{
      series_id: 'series-safe',
      metric: metric.name,
      at: '2026-08-16T01:10:00Z',
      value: 61,
      source_layer: 'raw',
    }],
    quality: { ...quality, status: 'partial', partial: true, gap_count: 1, peak_count: 1 },
  }
}

const rendererCases = [
  {
    name: 'IP quality',
    evidence: baseEvidence,
    visible: 'ipq_safe_report',
  },
  {
    name: 'monitoring host',
    evidence: {
      ...baseEvidence,
      kind: 'monitoring.host',
      renderer_version: 'monitoring_host_v1',
      read_model: monitoringReadModel('monitoring_host_read_model/v1'),
    },
    visible: 'cpu_usage_pct',
  },
  {
    name: 'monitoring probe',
    evidence: {
      ...baseEvidence,
      kind: 'monitoring.probe',
      schema_version: 2,
      renderer_version: 'monitoring_probe_v2',
      read_model: monitoringReadModel('monitoring_probe_read_model/v1'),
    },
    visible: 'latency_ms',
  },
  {
    name: 'monitoring event',
    evidence: {
      ...baseEvidence,
      kind: 'monitoring.event',
      schema_version: 2,
      renderer_version: 'monitoring_event_v2',
      read_model: {
        version: 'monitoring_event_read_model/v2',
        quality_status: 'complete',
        event_count: 1,
        backfilled_count: 0,
        events: [{
          event_id: 'evt_safe',
          object_type: 'monitoring_instance',
          object_id: 'mi_safe',
          event_type: 'incident_started',
          severity: '严重',
          summary: '主机离线',
          event_at: '2026-08-16T01:30:00Z',
          recorded_at: '2026-08-16T01:30:01Z',
          backfilled: false,
          provenance: 'center',
          producer_version: 'center-monitoring-events/v1',
          rule_version: 'incident-rules/v1',
          prior_state: 'normal',
          resulting_state: 'critical',
          correction_of_event_id: '',
          metrics: [],
        }],
      },
    },
    visible: '主机离线',
  },
  {
    name: 'subscription cost',
    evidence: {
      ...baseEvidence,
      kind: 'subscription.cost',
      renderer_version: 'subscription_cost_v1',
      read_model: {
        version: 'subscription_cost_read_model/v1',
        subscription_id: 'sub_safe',
        vps_id: 'vps_renderer',
        original_amount: 18.5,
        original_currency: 'USD',
        billing_period_unit: 'month',
        billing_period_length: 1,
        conversion_rate: 132.5 / 18.5,
        conversion_provider: 'fixer',
        rate_date: '2026-08-01',
        rate_fetched_at: '2026-08-16T00:00:00Z',
        rate_stale: false,
        base_amount: 132.5,
        base_currency: 'CNY',
        budget_source: 'subscription_monthly_budgets',
        budget_currency: 'CNY',
        budget_month: '2026-08',
        budget_monthly_limit: 1000,
        budget_warning_pct: 80,
        budget_status: 'ok',
        budget_actual_spend: 200,
        coverage_start: '2026-08-01T00:00:00Z',
        coverage_end: '2026-09-01T00:00:00Z',
        coverage_status: 'complete',
        covered_days: 31,
        total_days: 31,
        converted_subscription_count: 1,
        missing_rate_count: 0,
      },
    },
    visible: 'ok',
  },
  {
    name: 'command audit',
    evidence: {
      ...baseEvidence,
      kind: 'command.audit',
      renderer_version: 'command_audit_v1',
      read_model: {
        version: 'command_audit_read_model/v1',
        audit_count: 1,
        command_result_retention_seconds: 86400,
        command_result_payload_allowed: false,
        audits: [{
          audit_id: 'audit_safe',
          action_id: 'action_safe',
          monitoring_instance_id: 'mi_safe',
          monitoring_instance_name: '边缘节点',
          actor_user_id: 'user_safe',
          actor_username: 'operator',
          actor_display_name: '值班员',
          command_id: 'uptime',
          sensitivity: 'standard',
          event_type: 'completed',
          outcome: 'succeeded',
          source: 'agent_sync',
          exit_code: 0,
          occurred_at: '2026-08-16T01:45:00Z',
        }],
      },
    },
    visible: 'uptime',
  },
] as const

describe('EvidenceRendererRegistry', () => {
  it.each(rendererCases)('renders the exact $name tuple through its dedicated decoder', ({ evidence, visible }) => {
    render(<EvidenceRendererRegistry evidence={evidence} />)

    expect(screen.getByText(visible)).toBeInTheDocument()
  })

  it.each([
    ['unknown kind', { kind: 'monitoring.future' }],
    ['schema mismatch', { schema_version: 9 }],
    ['renderer mismatch', { renderer_version: 'monitoring_host_v99' }],
    ['read model mismatch', {
      read_model: { ...monitoringReadModel('monitoring_host_read_model/v1'), version: 'monitoring_host_read_model/v99' },
    }],
    ['invalid model', { read_model: { version: 'monitoring_host_read_model/v1', buckets: 'not-an-array' } }],
  ])('fails closed for %s', (_name, mutation) => {
    const evidence = {
      ...baseEvidence,
      kind: 'monitoring.host',
      renderer_version: 'monitoring_host_v1',
      read_model: monitoringReadModel('monitoring_host_read_model/v1'),
      ...mutation,
    }
    const { container } = render(<EvidenceRendererRegistry evidence={evidence} />)

    expect(container).toBeEmptyDOMElement()
  })

  it('keeps forbidden lookalikes outside every rendered source path', () => {
    const evidence = {
      ...baseEvidence,
      read_model: {
        ...baseEvidence.read_model,
        providers: [{
          ...baseEvidence.read_model.providers[0],
          payload: 'nested-provider-payload',
          metadata: 'nested-provider-metadata',
          authorization: 'nested-provider-authorization',
          digest: 'nested-provider-digest',
          stdout: 'nested-provider-stdout',
          stderr: 'nested-provider-stderr',
          ip_address: '198.51.100.44',
          region_name: 'nested-provider-topology',
        }],
        services: [{
          ...baseEvidence.read_model.services[0],
          ip_address: '203.0.113.55',
          region: 'nested-service-topology',
        }],
        payload: 'payload-secret',
        metadata: 'metadata-secret',
        authorization: 'authorization-secret',
        digest: 'digest-secret',
        stdout: 'stdout-secret',
        stderr: 'stderr-secret',
        ip_address: '192.0.2.44',
        asn: 'AS64500',
        registered_region_name: 'topology-secret',
      },
    }
    const { container } = render(<EvidenceRendererRegistry evidence={evidence} />)

    expect(screen.getByText('ipq_safe_report')).toBeInTheDocument()
    expect(container).not.toHaveTextContent(/payload-secret|metadata-secret|authorization-secret|digest-secret|stdout-secret|stderr-secret|nested-provider|nested-service|192\.0\.2\.44|198\.51\.100\.44|203\.0\.113\.55|AS64500|topology-secret/)
  })

  it('never exposes command output fields nested beside an allowlisted audit row', () => {
    const commandEvidence = rendererCases[5].evidence
    const readModel = commandEvidence.read_model
    const evidence = {
      ...commandEvidence,
      read_model: {
        ...readModel,
        audits: [{
          ...readModel.audits[0],
          stdout: 'nested-stdout-secret',
          stderr: 'nested-stderr-secret',
          payload: 'nested-payload-secret',
          metadata: 'nested-metadata-secret',
        }],
      },
    }
    const { container } = render(<EvidenceRendererRegistry evidence={evidence} />)

    expect(screen.getByText('uptime')).toBeInTheDocument()
    expect(container).not.toHaveTextContent(/nested-stdout-secret|nested-stderr-secret|nested-payload-secret|nested-metadata-secret/)
  })

  it('maps authoritative monitoring gaps to separate MetricChart line segments', () => {
    const { container } = render(<EvidenceRendererRegistry evidence={rendererCases[1].evidence} />)

    const segments = Array.from(container.querySelectorAll('polyline'))
    expect(segments).toHaveLength(2)
    const points = segments.map((segment) => segment.getAttribute('points')?.split(' ') ?? [])
    expect(points.map((segment) => segment.length)).toEqual([2, 2])
    expect(points.every((segment) => new Set(segment).size === 1)).toBe(true)
  })

  it('does not connect a metric across an interval where only another metric has a bucket', () => {
    const model = monitoringReadModel('monitoring_host_read_model/v1')
    const firstBucket = model.buckets[0]
    if (!firstBucket) throw new Error('monitoring fixture requires a first bucket')
    model.buckets = [
      {
        ...firstBucket,
        end: '2026-08-16T01:05:00Z',
      },
      {
        ...firstBucket,
        start: '2026-08-16T01:05:00Z',
        end: '2026-08-16T01:10:00Z',
        metrics: [{ name: 'mem_used_pct', unit: 'percent', average: 51 }],
      },
      {
        ...firstBucket,
        start: '2026-08-16T01:10:00Z',
        end: '2026-08-16T01:15:00Z',
        metrics: [{ name: 'cpu_usage_pct', unit: 'percent', average: 49 }],
      },
    ]
    model.gaps = []
    model.peaks = []
    model.quality = { ...quality, sample_count: 3, bucket_count: 3, data_point_count: 3 }
    const evidence = {
      ...rendererCases[1].evidence,
      read_model: model,
    }

    render(<EvidenceRendererRegistry evidence={evidence} />)

    const chart = screen.getByRole('img', { name: 'series-safe cpu_usage_pct 趋势' })
    expect(chart.querySelectorAll('polyline')).toHaveLength(2)
  })

  it('validates bounded gaps without rescanning every bucket for every gap', () => {
    const start = new Date('2026-08-16T00:00:00Z').getTime()
    const buckets = Array.from({ length: 20 }, (_, index) => ({
      series_id: 'series-safe',
      series_kind: 'host',
      start: new Date(start + index * 10 * 60_000).toISOString(),
      end: new Date(start + (index * 10 + 5) * 60_000).toISOString(),
      source_layer: 'raw',
      source_granularity_seconds: 300,
      sample_count: 1,
      maintenance_count: 0,
      backfilled_count: 0,
      metrics: [{ name: 'cpu_usage_pct', unit: 'percent', average: index }],
    }))
    const gaps = Array.from({ length: 19 }, (_, index) => ({
      series_id: 'series-safe',
      start: new Date(start + (index * 10 + 5) * 60_000).toISOString(),
      end: new Date(start + (index + 1) * 10 * 60_000).toISOString(),
    }))
    const model = {
      version: 'monitoring_host_read_model/v1',
      requested_start: buckets[0]?.start,
      requested_end: buckets.at(-1)?.end,
      coverage_start: buckets[0]?.start,
      coverage_end: buckets.at(-1)?.end,
      actual_precision_seconds: 300,
      buckets,
      gaps,
      peaks: [],
      quality: {
        ...quality,
        status: 'partial',
        partial: true,
        sample_count: 20,
        bucket_count: 20,
        gap_count: 19,
        data_point_count: 20,
      },
    }
    const parse = vi.spyOn(Date, 'parse')

    expect(decodeMonitoringEvidenceReadModel(model, 'monitoring_host_read_model/v1')).not.toBeNull()
    expect(parse.mock.calls.length).toBeLessThan(300)
  })

  it('fails closed instead of combining different units in one series and metric', () => {
    const model = monitoringReadModel('monitoring_host_read_model/v1')
    const firstBucket = model.buckets[0]
    const secondBucket = model.buckets[1]
    if (!firstBucket || !secondBucket) throw new Error('monitoring fixture requires two buckets')
    model.gaps = []
    model.peaks = []
    model.buckets[1] = {
      ...secondBucket,
      start: firstBucket.end,
      metrics: [{ name: 'cpu_usage_pct', unit: 'bytes', average: 61 }],
    }
    const { container } = render(<EvidenceRendererRegistry evidence={{
      ...rendererCases[1].evidence,
      read_model: model,
    }} />)

    expect(container).toBeEmptyDOMElement()
  })

  it.each([
    ['unknown quality status', () => ({ ...baseEvidence.read_model, quality: { ...quality, status: 'trusted' } })],
    ['negative quality count', () => ({ ...baseEvidence.read_model, quality: { ...quality, sample_count: -1 } })],
    ['fractional coverage count', () => ({
      ...baseEvidence.read_model,
      coverage: { ...baseEvidence.read_model.coverage, expected_provider_count: 1.5 },
    })],
    ['duplicate provider identity', () => ({
      ...baseEvidence.read_model,
      coverage: {
        ...baseEvidence.read_model.coverage,
        expected_provider_count: 2,
        successful_provider_count: 2,
      },
      providers: [baseEvidence.read_model.providers[0], baseEvidence.read_model.providers[0]],
      quality: { ...baseEvidence.read_model.quality, data_point_count: 4 },
    })],
  ])('fails closed for %s', (_name, buildReadModel) => {
    const { container } = render(<EvidenceRendererRegistry evidence={{
      ...baseEvidence,
      read_model: buildReadModel(),
    }} />)

    expect(container).toBeEmptyDOMElement()
  })

  it('fails closed for non-canonical timestamps and unbounded visible text', () => {
    const eventEvidence = rendererCases[3].evidence
    const invalidTimestamp = {
      ...eventEvidence,
      read_model: {
        ...eventEvidence.read_model,
        events: [{ ...eventEvidence.read_model.events[0], event_at: '2026-08-16T09:30:00+08:00' }],
      },
    }
    const unboundedSummary = {
      ...eventEvidence,
      read_model: {
        ...eventEvidence.read_model,
        events: [{ ...eventEvidence.read_model.events[0], summary: 'x'.repeat(2049) }],
      },
    }

    const first = render(<EvidenceRendererRegistry evidence={invalidTimestamp} />)
    expect(first.container).toBeEmptyDOMElement()
    first.unmount()
    const second = render(<EvidenceRendererRegistry evidence={unboundedSummary} />)
    expect(second.container).toBeEmptyDOMElement()
  })

  it('accepts canonical chronological events across an omitted-to-present fractional second', () => {
    const eventEvidence = rendererCases[3].evidence
    const firstEvent = eventEvidence.read_model.events[0]
    const evidence = {
      ...eventEvidence,
      read_model: {
        ...eventEvidence.read_model,
        event_count: 2,
        events: [firstEvent, {
          ...firstEvent,
          event_id: 'evt_safe_2',
          summary: '主机仍离线',
          event_at: '2026-08-16T01:30:00.000001Z',
          recorded_at: '2026-08-16T01:30:01.000001Z',
        }],
      },
    }

    render(<EvidenceRendererRegistry evidence={evidence} />)

    expect(screen.getByText('主机离线')).toBeInTheDocument()
    expect(screen.getByText('主机仍离线')).toBeInTheDocument()
  })

  it('fails closed when command retention or audit cardinality contradicts the authoritative model', () => {
    const commandEvidence = rendererCases[5].evidence
    const invalidRetention = render(<EvidenceRendererRegistry evidence={{
      ...commandEvidence,
      read_model: { ...commandEvidence.read_model, command_result_retention_seconds: 1 },
    }} />)
    expect(invalidRetention.container).toBeEmptyDOMElement()
    invalidRetention.unmount()

    const invalidCount = render(<EvidenceRendererRegistry evidence={{
      ...commandEvidence,
      read_model: { ...commandEvidence.read_model, audit_count: 2 },
    }} />)
    expect(invalidCount.container).toBeEmptyDOMElement()
  })

  it('fails the whole constructed registry closed when a tuple is registered twice', () => {
    const registration = {
      kind: 'ip_quality.report',
      schema_version: 1,
      renderer_version: 'ip_quality_report_v1',
      read_model_version: 'ip_quality_report_read_model/v1',
      decode: () => ({ report_id: 'must-not-render' }),
      render: () => <span>must-not-render</span>,
    } as const
    const DuplicateRegistry = createEvidenceRendererRegistry([registration, registration])
    const { container } = render(<DuplicateRegistry evidence={baseEvidence} />)

    expect(container).toBeEmptyDOMElement()
  })
})
