const MAX_SNAPSHOT_DATA_POINTS = 50_000
const MAX_METRIC_BUCKETS = 2_000
const MAX_PEAKS = 20
const MAX_BUCKET_METRICS = 24
const MAX_VISIBLE_TEXT = 2_048
const MAX_FIELD_ATOM = 128

const QUALITY_STATUSES = new Set(['complete', 'partial', 'degraded', 'unknown'])
const IP_REPORT_STATUSES = new Set(['success', 'partial'])
const IP_SOURCE_STATUSES = new Set(['success', 'failure', 'skipped', 'not_configured'])
const IP_SOURCE_TYPES = new Set(['default', 'optional', 'custom'])
const IP_SERVICE_STATUSES = new Set(['unlocked', 'blocked', 'unknown'])
const MONITORING_SOURCE_LAYERS = new Set(['raw', 'daily_aggregate'])
const EVENT_SEVERITIES = new Set(['正常', '关注', '告警', '严重'])
const EVENT_OBJECT_TYPES = new Set(['monitoring_instance', 'target'])
const EVENT_PROVENANCES = new Set(['agent_sync', 'center', 'web', 'retention_backfill', 'manual_correction'])
const EVENT_RULES = new Set([
  'incident-rules/v1',
  'monitoring-binding-rules/v1',
  'monitoring-lifecycle-rules/v1',
  'monitoring-runtime-rules/v1',
  'target-runtime-rules/v1',
])
const EVENT_TYPES = new Set([
  'incident_started',
  'incident_escalated',
  'incident_recovered',
  'monitoring_instance_binding_rebind_confirmed',
  'monitoring_instance_binding_pending_rejected',
  'monitoring_instance_binding_reset',
  'monitoring_instance_monitoring_maintenance_entered',
  'monitoring_instance_monitoring_maintenance_exited',
  'monitoring_instance_monitoring_paused',
  'monitoring_instance_monitoring_resumed',
  'monitoring_instance_lifecycle_updated',
  'monitoring_instance_retired',
  'monitoring_instance_restored_to_observing',
  'target_maintenance_entered',
  'target_maintenance_exited',
  'target_paused',
  'target_resumed',
  'target_archived',
  'target_restored_to_paused',
  'event_corrected',
])
const EVENT_CONTRACTS: Record<string, {
  objects: ReadonlySet<string>
  events: ReadonlySet<string>
  states: ReadonlySet<string>
}> = {
  'incident-rules/v1': {
    objects: EVENT_OBJECT_TYPES,
    events: new Set(['incident_started', 'incident_escalated', 'incident_recovered', 'event_corrected']),
    states: new Set(['normal', 'notice', 'alert', 'critical']),
  },
  'monitoring-binding-rules/v1': {
    objects: new Set(['monitoring_instance']),
    events: new Set(['monitoring_instance_binding_rebind_confirmed', 'monitoring_instance_binding_pending_rejected', 'monitoring_instance_binding_reset', 'event_corrected']),
    states: new Set(['未绑定', '已绑定', '指纹变更待确认']),
  },
  'monitoring-lifecycle-rules/v1': {
    objects: new Set(['monitoring_instance']),
    events: new Set(['monitoring_instance_lifecycle_updated', 'monitoring_instance_retired', 'monitoring_instance_restored_to_observing', 'event_corrected']),
    states: new Set(['待接入', '在用', '观察中', '不续费', '已退役', 'unarchived', 'archived']),
  },
  'monitoring-runtime-rules/v1': {
    objects: new Set(['monitoring_instance']),
    events: new Set(['monitoring_instance_monitoring_maintenance_entered', 'monitoring_instance_monitoring_maintenance_exited', 'monitoring_instance_monitoring_paused', 'monitoring_instance_monitoring_resumed', 'event_corrected']),
    states: new Set(['启用', '维护中', '暂停']),
  },
  'target-runtime-rules/v1': {
    objects: new Set(['target']),
    events: new Set(['target_maintenance_entered', 'target_maintenance_exited', 'target_paused', 'target_resumed', 'target_archived', 'target_restored_to_paused', 'event_corrected']),
    states: new Set(['启用', '维护中', '暂停', '已归档']),
  },
}
const BILLING_PERIOD_UNITS = new Set(['day', 'week', 'month', 'year'])
const CONVERSION_PROVIDERS = new Set(['identity', 'frankfurter', 'fixer'])
const BUDGET_STATUSES = new Set(['ok', 'warning', 'over', 'unknown'])
const COVERAGE_STATUSES = new Set(['complete', 'partial', 'missing_rate'])
const COMMAND_EVENTS = new Set(['queued', 'dispatched', 'completed', 'rejected'])
const COMMAND_OUTCOMES = new Set(['queued', 'dispatched', 'succeeded', 'failed', 'rejected'])
const COMMAND_SOURCES = new Set(['web', 'agent_sync'])
const COMMAND_SENSITIVITY = new Map([
  ['df_h', 'standard'],
  ['free_m', 'standard'],
  ['uptime', 'standard'],
  ['top_head', 'sensitive'],
  ['journalctl_u', 'sensitive'],
  ['systemctl_status', 'sensitive'],
  ['dmesg_err', 'sensitive'],
  ['docker_ps', 'sensitive'],
])
const HOST_METRIC_UNITS = new Map([
  ['cpu_iowait_pct', 'percent'],
  ['cpu_steal_pct', 'percent'],
  ['cpu_usage_pct', 'percent'],
  ['disk_busy_pct', 'percent'],
  ['disk_read_bytes_per_sec', 'bytes_per_second'],
  ['disk_total_bytes', 'bytes'],
  ['disk_used_pct', 'percent'],
  ['disk_write_bytes_per_sec', 'bytes_per_second'],
  ['inode_used_pct', 'percent'],
  ['load_1', 'load'],
  ['load_15', 'load'],
  ['load_5', 'load'],
  ['mem_available_bytes', 'bytes'],
  ['mem_total_bytes', 'bytes'],
  ['mem_used_pct', 'percent'],
  ['net_in_bytes_per_sec', 'bytes_per_second'],
  ['net_out_bytes_per_sec', 'bytes_per_second'],
  ['swap_used_pct', 'percent'],
  ['uptime_seconds', 'seconds'],
])
const PROBE_METRIC_UNITS = new Map([
  ['http_status', 'status_code'],
  ['latency_ms', 'ms'],
  ['success_ratio', 'ratio'],
  ['tls_expiry_days', 'days'],
])

type JSONRecord = Record<string, unknown>

export type EvidenceReadModelQuality = {
  status: 'complete' | 'partial' | 'degraded' | 'unknown'
  partial: boolean
  truncated: boolean
  sample_count: number
  maintenance_count: number
  backfilled_count: number
  bucket_count: number
  gap_count: number
  peak_count: number
  data_point_count: number
}

export type IPQualityProviderReadModel = {
  provider: string
  status: string
  risk_level?: string
}

export type IPQualityServiceReadModel = {
  service: string
  source: string
  status: string
  probe_status?: string
}

export type IPQualityEvidenceReadModel = {
  version: 'ip_quality_report_read_model/v1'
  report_id: string
  observed_at: string
  received_at: string
  ip_version: 4 | 6
  status: string
  stale: boolean
  stale_after_seconds: number
  risk_level: string
  coverage: Record<string, number>
  providers: IPQualityProviderReadModel[]
  services: IPQualityServiceReadModel[]
  quality: EvidenceReadModelQuality
}

export type MonitoringMetricReadModel = {
  name: string
  unit: string
  average?: number
  min?: number
  max?: number
  p95?: number
}

export type MonitoringBucketReadModel = {
  series_id: string
  series_kind: string
  start: string
  end: string
  source_layer: string
  source_granularity_seconds: number
  sample_count: number
  maintenance_count: number
  backfilled_count: number
  metrics: MonitoringMetricReadModel[]
}

export type MonitoringGapReadModel = {
  series_id: string
  start: string
  end: string
}

export type MonitoringPeakReadModel = {
  series_id: string
  metric: string
  at: string
  value: number
  source_layer: string
}

export type MonitoringEvidenceReadModel = {
  version: 'monitoring_host_read_model/v1' | 'monitoring_probe_read_model/v1'
  requested_start: string
  requested_end: string
  coverage_start: string
  coverage_end: string
  actual_precision_seconds: number
  buckets: MonitoringBucketReadModel[]
  gaps: MonitoringGapReadModel[]
  peaks: MonitoringPeakReadModel[]
  quality: EvidenceReadModelQuality
}

export type MonitoringEventItemReadModel = {
  event_id: string
  object_type: string
  object_id: string
  event_type: string
  severity: string
  summary: string
  event_at: string
  recorded_at: string
  backfilled: boolean
  provenance: string
  producer_version: string
  rule_version: string
  prior_state: string
  resulting_state: string
  correction_of_event_id: string
  metrics: MonitoringEventMetricReadModel[]
}

export type MonitoringEventMetricReadModel = {
  metric: string
  unit: string
  value: number
  threshold: number
}

export type MonitoringEventEvidenceReadModel = {
  version: 'monitoring_event_read_model/v2'
  quality_status: string
  event_count: number
  backfilled_count: number
  events: MonitoringEventItemReadModel[]
}

export type SubscriptionCostEvidenceReadModel = {
  version: 'subscription_cost_read_model/v1'
  subscription_id: string
  vps_id: string
  original_amount: number
  original_currency: string
  billing_period_unit: string
  billing_period_length: number
  conversion_rate: number
  conversion_provider: string
  rate_date: string
  rate_fetched_at: string
  rate_stale: boolean
  base_amount: number
  base_currency: string
  budget_source: 'subscription_monthly_budgets'
  budget_currency: string
  budget_month: string
  budget_monthly_limit: number
  budget_warning_pct: number
  budget_status: string
  budget_actual_spend: number
  coverage_start: string
  coverage_end: string
  coverage_status: string
  covered_days: number
  total_days: number
  converted_subscription_count: number
  missing_rate_count: number
}

export type CommandAuditItemReadModel = {
  audit_id: string
  action_id: string
  monitoring_instance_id: string
  monitoring_instance_name: string
  actor_user_id: string
  actor_username: string
  actor_display_name: string
  command_id: string
  sensitivity: string
  event_type: string
  outcome: string
  source: string
  exit_code?: number
  occurred_at: string
}

export type CommandAuditEvidenceReadModel = {
  version: 'command_audit_read_model/v1'
  audit_count: number
  command_result_retention_seconds: number
  command_result_payload_allowed: false
  audits: CommandAuditItemReadModel[]
}

function record(value: unknown): JSONRecord | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null
  return value as JSONRecord
}

function stringField(value: JSONRecord, key: string): string | null {
  const field = value[key]
  return typeof field === 'string' && field.length <= MAX_VISIBLE_TEXT ? field : null
}

function nonEmptyStringField(value: JSONRecord, key: string, maximum = MAX_VISIBLE_TEXT): string | null {
  const field = value[key]
  return typeof field === 'string' && field.length > 0 && field.length <= maximum && field.trim() === field
    ? field
    : null
}

function numberField(value: JSONRecord, key: string): number | null {
  const field = value[key]
  return typeof field === 'number' && Number.isFinite(field) ? field : null
}

function countField(value: JSONRecord, key: string): number | null {
  const field = value[key]
  return typeof field === 'number' && Number.isSafeInteger(field) && field >= 0 ? field : null
}

function positiveIntegerField(value: JSONRecord, key: string): number | null {
  const field = countField(value, key)
  return field !== null && field > 0 ? field : null
}

function booleanField(value: JSONRecord, key: string): boolean | null {
  const field = value[key]
  return typeof field === 'boolean' ? field : null
}

function optionalStringField(value: JSONRecord, key: string, maximum = MAX_VISIBLE_TEXT): string | undefined | null {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  return typeof field === 'string' && field.length <= maximum ? field : null
}

function optionalNumberField(value: JSONRecord, key: string): number | undefined | null {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  return typeof field === 'number' && Number.isFinite(field) ? field : null
}

function optionalIntegerField(value: JSONRecord, key: string): number | undefined | null {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  return typeof field === 'number' && Number.isSafeInteger(field) ? field : null
}

function optionalNonNegativeNumberField(value: JSONRecord, key: string): number | undefined | null {
  const field = optionalNumberField(value, key)
  return field === undefined || (field !== null && field >= 0) ? field : null
}

function optionalBooleanField(value: JSONRecord, key: string): boolean | undefined | null {
  const field = value[key]
  if (field === undefined || field === null) return undefined
  return typeof field === 'boolean' ? field : null
}

function boundedArray(value: unknown, maximum = MAX_SNAPSHOT_DATA_POINTS): unknown[] | null {
  return Array.isArray(value) && value.length <= maximum ? value : null
}

function enumField(value: JSONRecord, key: string, allowed: ReadonlySet<string>): string | null {
  const field = stringField(value, key)
  return field !== null && allowed.has(field) ? field : null
}

function canonicalUTCTimestamp(value: string): boolean {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,6}))?Z$/.exec(value)
  if (!match) return false
  const [, yearText, monthText, dayText, hourText, minuteText, secondText] = match
  const year = Number(yearText)
  const month = Number(monthText)
  const day = Number(dayText)
  const hour = Number(hourText)
  const minute = Number(minuteText)
  const second = Number(secondText)
  const date = new Date(Date.UTC(year, month - 1, day, hour, minute, second))
  return date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 &&
    date.getUTCDate() === day && date.getUTCHours() === hour &&
    date.getUTCMinutes() === minute && date.getUTCSeconds() === second
}

function timestampField(value: JSONRecord, key: string): string | null {
  const field = stringField(value, key)
  return field !== null && canonicalUTCTimestamp(field) ? field : null
}

function timestampMicros(value: string): number {
  const match = /^(.*?)(?:\.(\d{1,6}))?Z$/.exec(value)
  if (!match) return Number.NaN
  const secondsPart = match[1]
  if (!secondsPart) return Number.NaN
  const seconds = Date.parse(`${secondsPart}Z`)
  const fraction = Number((match[2] ?? '').padEnd(6, '0') || '0')
  return seconds * 1_000 + fraction
}

function canonicalDate(value: string): boolean {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!match) return false
  const [, yearText, monthText, dayText] = match
  const date = new Date(Date.UTC(Number(yearText), Number(monthText) - 1, Number(dayText)))
  return date.getUTCFullYear() === Number(yearText) &&
    date.getUTCMonth() === Number(monthText) - 1 && date.getUTCDate() === Number(dayText)
}

function canonicalMonth(value: string): boolean {
  return /^\d{4}-(?:0[1-9]|1[0-2])$/.test(value)
}

function currencyField(value: JSONRecord, key: string): string | null {
  const field = stringField(value, key)
  return field !== null && /^[A-Z]{3}$/.test(field) ? field : null
}

function nonNegativeNumberField(value: JSONRecord, key: string): number | null {
  const field = numberField(value, key)
  return field !== null && field >= 0 ? field : null
}

function decodeQuality(value: unknown): EvidenceReadModelQuality | null {
  const input = record(value)
  if (!input) return null
  const status = enumField(input, 'status', QUALITY_STATUSES)
  const partial = booleanField(input, 'partial')
  const truncated = booleanField(input, 'truncated')
  const sampleCount = countField(input, 'sample_count')
  const maintenanceCount = countField(input, 'maintenance_count')
  const backfilledCount = countField(input, 'backfilled_count')
  const bucketCount = countField(input, 'bucket_count')
  const gapCount = countField(input, 'gap_count')
  const peakCount = countField(input, 'peak_count')
  const dataPointCount = countField(input, 'data_point_count')
  if (status === null || partial === null || truncated === null || sampleCount === null ||
    maintenanceCount === null || backfilledCount === null || bucketCount === null ||
    gapCount === null || peakCount === null || dataPointCount === null ||
    bucketCount > MAX_METRIC_BUCKETS || peakCount > MAX_PEAKS ||
    dataPointCount > MAX_SNAPSHOT_DATA_POINTS || maintenanceCount > sampleCount ||
    backfilledCount > sampleCount || partial !== (status === 'partial') ||
    (truncated && !partial) || (gapCount > 0 && !partial)) return null
  return {
    status: status as EvidenceReadModelQuality['status'],
    partial,
    truncated,
    sample_count: sampleCount,
    maintenance_count: maintenanceCount,
    backfilled_count: backfilledCount,
    bucket_count: bucketCount,
    gap_count: gapCount,
    peak_count: peakCount,
    data_point_count: dataPointCount,
  }
}

function decodeCountMap(value: unknown): Record<string, number> | null {
  const input = record(value)
  if (!input) return null
  const output: Record<string, number> = {}
  for (const key of [
    'expected_provider_count',
    'successful_provider_count',
    'failed_provider_count',
    'skipped_provider_count',
    'not_configured_provider_count',
    'expected_service_count',
    'successful_service_count',
    'failed_service_count',
    'skipped_service_count',
    'not_configured_service_count',
  ]) {
    const count = countField(input, key)
    if (count === null) return null
    output[key] = count
  }
  return output
}

export function decodeIPQualityEvidenceReadModel(value: unknown): IPQualityEvidenceReadModel | null {
  const input = record(value)
  if (!input || input.version !== 'ip_quality_report_read_model/v1') return null
  const reportId = nonEmptyStringField(input, 'report_id', MAX_FIELD_ATOM)
  const observedAt = timestampField(input, 'observed_at')
  const receivedAt = timestampField(input, 'received_at')
  const ipVersion = countField(input, 'ip_version')
  const status = enumField(input, 'status', IP_REPORT_STATUSES)
  const stale = booleanField(input, 'stale')
  const riskLevel = stringField(input, 'risk_level')
  const staleAfterSeconds = positiveIntegerField(input, 'stale_after_seconds')
  const coverage = decodeCountMap(input.coverage)
  const quality = decodeQuality(input.quality)
  const providerValues = boundedArray(input.providers)
  const serviceValues = boundedArray(input.services)
  if (reportId === null || observedAt === null || receivedAt === null ||
    (ipVersion !== 4 && ipVersion !== 6) || status === null || stale === null ||
    riskLevel === null || staleAfterSeconds === null || !coverage || !quality ||
    !providerValues || !serviceValues || timestampMicros(receivedAt) < timestampMicros(observedAt) ||
    1 + providerValues.length + serviceValues.length > MAX_SNAPSHOT_DATA_POINTS) return null

  const providers: IPQualityProviderReadModel[] = []
  const providerNames = new Set<string>()
  for (const value of providerValues) {
    const provider = record(value)
    if (!provider) return null
    const name = nonEmptyStringField(provider, 'provider', MAX_FIELD_ATOM)
    const providerStatus = enumField(provider, 'status', IP_SOURCE_STATUSES)
    const sourceType = enumField(provider, 'source_type', IP_SOURCE_TYPES)
    const latency = optionalNonNegativeNumberField(provider, 'latency_ms')
    const providerRisk = optionalStringField(provider, 'risk_level', MAX_FIELD_ATOM)
    const usageType = stringField(provider, 'usage_type')
    const companyType = stringField(provider, 'company_type')
    const riskScore = stringField(provider, 'risk_score')
    const errorCode = stringField(provider, 'error_code')
    const flags = ['is_proxy', 'is_tor', 'is_vpn', 'is_server', 'is_abuser', 'is_robot']
      .map((key) => optionalBooleanField(provider, key))
    if (name === null || providerStatus === null || sourceType === null || latency === null ||
      providerRisk === null || usageType === null || companyType === null || riskScore === null ||
      errorCode === null || flags.some((flag) => flag === null) || providerNames.has(name)) return null
    providerNames.add(name)
    const decoded: IPQualityProviderReadModel = { provider: name, status: providerStatus }
    if (providerRisk !== undefined) decoded.risk_level = providerRisk
    providers.push(decoded)
  }
  const services: IPQualityServiceReadModel[] = []
  const serviceIdentities = new Set<string>()
  for (const value of serviceValues) {
    const service = record(value)
    if (!service) return null
    const name = nonEmptyStringField(service, 'service', MAX_FIELD_ATOM)
    const serviceStatus = enumField(service, 'status', IP_SERVICE_STATUSES)
    const source = stringField(service, 'source')
    const probeStatus = enumField(service, 'probe_status', IP_SOURCE_STATUSES)
    const latency = optionalNonNegativeNumberField(service, 'latency_ms')
    const unlockType = stringField(service, 'unlock_type')
    const errorCode = stringField(service, 'error_code')
    const identity = `${name}\u0000${source}`
    if (name === null || serviceStatus === null || source === null || probeStatus === null ||
      latency === null || unlockType === null || errorCode === null || serviceIdentities.has(identity)) return null
    serviceIdentities.add(identity)
    const decoded: IPQualityServiceReadModel = { service: name, source, status: serviceStatus }
    decoded.probe_status = probeStatus
    services.push(decoded)
  }
  const expectedProviderCount = coverage.expected_provider_count ?? -1
  const expectedServiceCount = coverage.expected_service_count ?? -1
  const providerStatusCounts = providers.reduce<Record<string, number>>((counts, provider) => {
    counts[provider.status] = (counts[provider.status] ?? 0) + 1
    return counts
  }, {})
  const serviceStatusCounts = services.reduce<Record<string, number>>((counts, service) => {
    const statusKey = service.probe_status ?? ''
    counts[statusKey] = (counts[statusKey] ?? 0) + 1
    return counts
  }, {})
  if (providers.length !== expectedProviderCount || services.length !== expectedServiceCount ||
    (providerStatusCounts.success ?? 0) !== coverage.successful_provider_count ||
    (providerStatusCounts.failure ?? 0) !== coverage.failed_provider_count ||
    (providerStatusCounts.skipped ?? 0) !== coverage.skipped_provider_count ||
    (providerStatusCounts.not_configured ?? 0) !== coverage.not_configured_provider_count ||
    (serviceStatusCounts.success ?? 0) !== coverage.successful_service_count ||
    (serviceStatusCounts.failure ?? 0) !== coverage.failed_service_count ||
    (serviceStatusCounts.skipped ?? 0) !== coverage.skipped_service_count ||
    (serviceStatusCounts.not_configured ?? 0) !== coverage.not_configured_service_count ||
    quality.sample_count !== 1 || quality.bucket_count !== 1 || quality.maintenance_count !== 0 ||
    quality.gap_count !== 0 || quality.peak_count !== 0 ||
    quality.data_point_count !== 1 + providers.length + services.length) return null
  return {
    version: 'ip_quality_report_read_model/v1',
    report_id: reportId,
    observed_at: observedAt,
    received_at: receivedAt,
    ip_version: ipVersion,
    status,
    stale,
    stale_after_seconds: staleAfterSeconds,
    risk_level: riskLevel,
    coverage,
    providers,
    services,
    quality,
  }
}

function decodeMonitoringMetric(
  value: unknown,
  expectedUnits: ReadonlyMap<string, string>,
): MonitoringMetricReadModel | null {
  const input = record(value)
  if (!input) return null
  const name = nonEmptyStringField(input, 'name', MAX_FIELD_ATOM)
  const unit = nonEmptyStringField(input, 'unit', MAX_FIELD_ATOM)
  const average = optionalNumberField(input, 'average')
  const minimum = optionalNumberField(input, 'min')
  const maximum = optionalNumberField(input, 'max')
  const p95 = optionalNumberField(input, 'p95')
  if (name === null || unit === null || expectedUnits.get(name) !== unit || average === null ||
    minimum === null || maximum === null || p95 === null ||
    [average, minimum, maximum, p95].every((item) => item === undefined)) return null
  const result: MonitoringMetricReadModel = { name, unit }
  if (average !== undefined) result.average = average
  if (minimum !== undefined) result.min = minimum
  if (maximum !== undefined) result.max = maximum
  if (p95 !== undefined) result.p95 = p95
  return result
}

function decodeMonitoringBucket(
  value: unknown,
  expectedUnits: ReadonlyMap<string, string>,
): MonitoringBucketReadModel | null {
  const input = record(value)
  if (!input) return null
  const seriesId = nonEmptyStringField(input, 'series_id', MAX_FIELD_ATOM)
  const seriesKind = nonEmptyStringField(input, 'series_kind', MAX_FIELD_ATOM)
  const start = timestampField(input, 'start')
  const end = timestampField(input, 'end')
  const sourceLayer = enumField(input, 'source_layer', MONITORING_SOURCE_LAYERS)
  const granularity = positiveIntegerField(input, 'source_granularity_seconds')
  const sampleCount = positiveIntegerField(input, 'sample_count')
  const maintenanceCount = countField(input, 'maintenance_count')
  const backfilledCount = countField(input, 'backfilled_count')
  const metricValues = boundedArray(input.metrics, MAX_BUCKET_METRICS)
  if (seriesId === null || seriesKind === null || start === null || end === null ||
    sourceLayer === null || granularity === null || sampleCount === null ||
    maintenanceCount === null || backfilledCount === null || !metricValues || metricValues.length === 0 ||
    maintenanceCount > sampleCount || backfilledCount > sampleCount ||
    timestampMicros(end) <= timestampMicros(start)) return null
  const metrics: MonitoringMetricReadModel[] = []
  const metricNames = new Set<string>()
  for (const metricValue of metricValues) {
    const metric = decodeMonitoringMetric(metricValue, expectedUnits)
    if (!metric || metricNames.has(metric.name)) return null
    metricNames.add(metric.name)
    metrics.push(metric)
  }
  return {
    series_id: seriesId,
    series_kind: seriesKind,
    start,
    end,
    source_layer: sourceLayer,
    source_granularity_seconds: granularity,
    sample_count: sampleCount,
    maintenance_count: maintenanceCount,
    backfilled_count: backfilledCount,
    metrics,
  }
}

function decodeMonitoringGap(value: unknown): MonitoringGapReadModel | null {
  const input = record(value)
  if (!input) return null
  const seriesId = nonEmptyStringField(input, 'series_id', MAX_FIELD_ATOM)
  const start = timestampField(input, 'start')
  const end = timestampField(input, 'end')
  return seriesId === null || start === null || end === null || timestampMicros(end) <= timestampMicros(start)
    ? null
    : { series_id: seriesId, start, end }
}

function decodeMonitoringPeak(
  value: unknown,
  expectedUnits: ReadonlyMap<string, string>,
): MonitoringPeakReadModel | null {
  const input = record(value)
  if (!input) return null
  const seriesId = nonEmptyStringField(input, 'series_id', MAX_FIELD_ATOM)
  const metric = nonEmptyStringField(input, 'metric', MAX_FIELD_ATOM)
  const at = timestampField(input, 'at')
  const peakValue = numberField(input, 'value')
  const sourceLayer = enumField(input, 'source_layer', MONITORING_SOURCE_LAYERS)
  return seriesId === null || metric === null || !expectedUnits.has(metric) || at === null ||
    peakValue === null || sourceLayer === null
    ? null
    : { series_id: seriesId, metric, at, value: peakValue, source_layer: sourceLayer }
}

export function decodeMonitoringEvidenceReadModel(
  value: unknown,
  expectedVersion: MonitoringEvidenceReadModel['version'],
): MonitoringEvidenceReadModel | null {
  const input = record(value)
  if (!input || input.version !== expectedVersion) return null
  const requestedStart = timestampField(input, 'requested_start')
  const requestedEnd = timestampField(input, 'requested_end')
  const coverageStart = timestampField(input, 'coverage_start')
  const coverageEnd = timestampField(input, 'coverage_end')
  const precision = positiveIntegerField(input, 'actual_precision_seconds')
  const bucketValues = boundedArray(input.buckets, MAX_SNAPSHOT_DATA_POINTS)
  const gapValues = boundedArray(input.gaps, MAX_SNAPSHOT_DATA_POINTS)
  const peakValues = boundedArray(input.peaks, MAX_PEAKS)
  const quality = decodeQuality(input.quality)
  if (requestedStart === null || requestedEnd === null || coverageStart === null ||
    coverageEnd === null || precision === null || !bucketValues || !gapValues ||
    !peakValues || !quality || bucketValues.length === 0) return null
  const requestedStartMicros = timestampMicros(requestedStart)
  const requestedEndMicros = timestampMicros(requestedEnd)
  const coverageStartMicros = timestampMicros(coverageStart)
  const coverageEndMicros = timestampMicros(coverageEnd)
  if (requestedEndMicros <= requestedStartMicros || coverageEndMicros <= coverageStartMicros ||
    coverageStartMicros < requestedStartMicros || coverageEndMicros > requestedEndMicros) return null
  const expectedUnits = expectedVersion === 'monitoring_probe_read_model/v1'
    ? PROBE_METRIC_UNITS
    : HOST_METRIC_UNITS
  const buckets: MonitoringBucketReadModel[] = []
  const seriesKinds = new Map<string, string>()
  const seriesEnds = new Map<string, number>()
  const seriesBucketCounts = new Map<string, number>()
  const seriesMetrics = new Set<string>()
  const bucketsBySeries = new Map<string, MonitoringBucketReadModel[]>()
  let sampleCount = 0
  let maintenanceCount = 0
  let backfilledCount = 0
  let dataPointCount = 0
  for (const bucketValue of bucketValues) {
    const bucket = decodeMonitoringBucket(bucketValue, expectedUnits)
    if (!bucket) return null
    const startMicros = timestampMicros(bucket.start)
    const endMicros = timestampMicros(bucket.end)
    const previousEnd = seriesEnds.get(bucket.series_id)
    const previousKind = seriesKinds.get(bucket.series_id)
    if (startMicros < requestedStartMicros || endMicros > requestedEndMicros ||
      endMicros - startMicros > precision * 1_000_000 ||
      bucket.source_granularity_seconds > precision ||
      (previousEnd !== undefined && startMicros < previousEnd) ||
      (previousKind !== undefined && previousKind !== bucket.series_kind)) return null
    seriesKinds.set(bucket.series_id, bucket.series_kind)
    seriesEnds.set(bucket.series_id, endMicros)
    seriesBucketCounts.set(bucket.series_id, (seriesBucketCounts.get(bucket.series_id) ?? 0) + 1)
    const sameSeriesBuckets = bucketsBySeries.get(bucket.series_id) ?? []
    sameSeriesBuckets.push(bucket)
    bucketsBySeries.set(bucket.series_id, sameSeriesBuckets)
    sampleCount += bucket.sample_count
    maintenanceCount += bucket.maintenance_count
    backfilledCount += bucket.backfilled_count
    dataPointCount += bucket.metrics.length
    if (![sampleCount, maintenanceCount, backfilledCount, dataPointCount].every(Number.isSafeInteger) ||
      dataPointCount > MAX_SNAPSHOT_DATA_POINTS) return null
    for (const metric of bucket.metrics) seriesMetrics.add(`${bucket.series_id}\u0000${metric.name}`)
    buckets.push(bucket)
  }
  const gaps: MonitoringGapReadModel[] = []
  let previousGapSeries = ''
  let previousGapStart = Number.NEGATIVE_INFINITY
  let previousGapEnd = Number.NEGATIVE_INFINITY
  const gapBucketCursors = new Map<string, number>()
  for (const gapValue of gapValues) {
    const gap = decodeMonitoringGap(gapValue)
    if (!gap || !seriesKinds.has(gap.series_id)) return null
    const gapStart = timestampMicros(gap.start)
    const gapEnd = timestampMicros(gap.end)
    const sameSeriesBuckets = bucketsBySeries.get(gap.series_id) ?? []
    let bucketCursor = gapBucketCursors.get(gap.series_id) ?? 0
    while (bucketCursor < sameSeriesBuckets.length) {
      const bucket = sameSeriesBuckets[bucketCursor]
      if (!bucket || timestampMicros(bucket.end) > gapStart) break
      bucketCursor++
    }
    gapBucketCursors.set(gap.series_id, bucketCursor)
    const candidateBucket = sameSeriesBuckets[bucketCursor]
    if (gapStart < requestedStartMicros || gapEnd > requestedEndMicros ||
      gapEnd - gapStart > precision * 1_000_000 || gap.series_id < previousGapSeries ||
      (gap.series_id === previousGapSeries && (gapStart < previousGapStart ||
        (gapStart === previousGapStart && gapEnd <= previousGapEnd) || gapStart < previousGapEnd)) ||
      (candidateBucket !== undefined && timestampMicros(candidateBucket.start) < gapEnd &&
        timestampMicros(candidateBucket.end) > gapStart)) return null
    previousGapSeries = gap.series_id
    previousGapStart = gapStart
    previousGapEnd = gapEnd
    gaps.push(gap)
  }
  const peaks: MonitoringPeakReadModel[] = []
  for (const peakValue of peakValues) {
    const peak = decodeMonitoringPeak(peakValue, expectedUnits)
    if (!peak || !seriesMetrics.has(`${peak.series_id}\u0000${peak.metric}`) ||
      timestampMicros(peak.at) < requestedStartMicros || timestampMicros(peak.at) >= requestedEndMicros) return null
    peaks.push(peak)
  }
  const maximumSeriesBucketCount = Math.max(...seriesBucketCounts.values())
  if (maximumSeriesBucketCount > MAX_METRIC_BUCKETS || quality.sample_count !== sampleCount ||
    quality.maintenance_count !== maintenanceCount || quality.backfilled_count !== backfilledCount ||
    quality.bucket_count !== maximumSeriesBucketCount || quality.gap_count !== gaps.length ||
    quality.peak_count !== peaks.length || quality.data_point_count !== dataPointCount ||
    ((coverageStartMicros > requestedStartMicros || coverageEndMicros < requestedEndMicros) && !quality.partial)) return null
  return {
    version: expectedVersion,
    requested_start: requestedStart,
    requested_end: requestedEnd,
    coverage_start: coverageStart,
    coverage_end: coverageEnd,
    actual_precision_seconds: precision,
    buckets,
    gaps,
    peaks,
    quality,
  }
}

function validEventMetadata(event: MonitoringEventItemReadModel): boolean {
  if (!EVENT_OBJECT_TYPES.has(event.object_type) || !EVENT_TYPES.has(event.event_type) ||
    !EVENT_PROVENANCES.has(event.provenance) || !EVENT_RULES.has(event.rule_version) ||
    event.producer_version !== 'center-monitoring-events/v1' ||
    (event.provenance === 'retention_backfill' && !event.backfilled)) return false
  const corrected = event.event_type === 'event_corrected'
  if (corrected !== (event.provenance === 'manual_correction') ||
    corrected !== (event.correction_of_event_id.length > 0) ||
    event.correction_of_event_id === event.event_id) return false
  const contract = EVENT_CONTRACTS[event.rule_version]
  if (!contract || !contract.objects.has(event.object_type) || !contract.events.has(event.event_type) ||
    !contract.states.has(event.prior_state) || !contract.states.has(event.resulting_state)) return false
  if (event.rule_version !== 'incident-rules/v1') return event.severity === ''
  const severityState = new Map([
    ['正常', 'normal'], ['关注', 'notice'], ['告警', 'alert'], ['严重', 'critical'],
  ]).get(event.severity)
  return EVENT_SEVERITIES.has(event.severity) && severityState !== undefined && severityState ===
    (event.event_type === 'incident_recovered' ? event.prior_state : event.resulting_state)
}

export function decodeMonitoringEventEvidenceReadModel(value: unknown): MonitoringEventEvidenceReadModel | null {
  const input = record(value)
  if (!input || input.version !== 'monitoring_event_read_model/v2') return null
  const qualityStatus = enumField(input, 'quality_status', QUALITY_STATUSES)
  const eventCount = positiveIntegerField(input, 'event_count')
  const backfilledCount = countField(input, 'backfilled_count')
  const eventValues = boundedArray(input.events)
  if (qualityStatus === null || eventCount === null || backfilledCount === null || !eventValues ||
    eventValues.length !== eventCount || backfilledCount > eventCount) return null
  const events: MonitoringEventItemReadModel[] = []
  const eventIds = new Set<string>()
  const metricUnits = new Map<string, string>()
  let decodedBackfilledCount = 0
  let dataPointCount = 0
  let previousEventAt = Number.NEGATIVE_INFINITY
  let previousEventId = ''
  for (const value of eventValues) {
    const event = record(value)
    if (!event) return null
    const eventId = nonEmptyStringField(event, 'event_id', MAX_FIELD_ATOM)
    const objectType = enumField(event, 'object_type', EVENT_OBJECT_TYPES)
    const objectId = nonEmptyStringField(event, 'object_id', MAX_FIELD_ATOM)
    const eventType = enumField(event, 'event_type', EVENT_TYPES)
    const severity = stringField(event, 'severity')
    const summary = stringField(event, 'summary')
    const eventAt = timestampField(event, 'event_at')
    const recordedAt = timestampField(event, 'recorded_at')
    const backfilled = booleanField(event, 'backfilled')
    const provenance = enumField(event, 'provenance', EVENT_PROVENANCES)
    const producerVersion = stringField(event, 'producer_version')
    const ruleVersion = enumField(event, 'rule_version', EVENT_RULES)
    const priorState = nonEmptyStringField(event, 'prior_state', MAX_FIELD_ATOM)
    const resultingState = nonEmptyStringField(event, 'resulting_state', MAX_FIELD_ATOM)
    const correctionOfEventId = stringField(event, 'correction_of_event_id')
    const metricValues = boundedArray(event.metrics, 20)
    if (eventId === null || objectType === null || objectId === null || eventType === null ||
      severity === null || summary === null || eventAt === null || recordedAt === null ||
      backfilled === null || provenance === null || producerVersion === null || ruleVersion === null ||
      priorState === null || resultingState === null || correctionOfEventId === null || !metricValues ||
      timestampMicros(recordedAt) < timestampMicros(eventAt) || eventIds.has(eventId)) return null
    const metrics: MonitoringEventMetricReadModel[] = []
    const eventMetricNames = new Set<string>()
    for (const metricValue of metricValues) {
      const metric = record(metricValue)
      if (!metric) return null
      const metricName = nonEmptyStringField(metric, 'metric', MAX_FIELD_ATOM)
      const unit = nonEmptyStringField(metric, 'unit', MAX_FIELD_ATOM)
      const metricValueNumber = numberField(metric, 'value')
      const threshold = numberField(metric, 'threshold')
      if (metricName === null || unit === null || metricValueNumber === null || threshold === null ||
        eventMetricNames.has(metricName) ||
        (metricUnits.has(metricName) && metricUnits.get(metricName) !== unit)) return null
      eventMetricNames.add(metricName)
      metricUnits.set(metricName, unit)
      metrics.push({ metric: metricName, unit, value: metricValueNumber, threshold })
    }
    const decoded: MonitoringEventItemReadModel = {
      event_id: eventId,
      object_type: objectType,
      object_id: objectId,
      event_type: eventType,
      severity,
      summary,
      event_at: eventAt,
      recorded_at: recordedAt,
      backfilled,
      provenance,
      producer_version: producerVersion,
      rule_version: ruleVersion,
      prior_state: priorState,
      resulting_state: resultingState,
      correction_of_event_id: correctionOfEventId,
      metrics,
    }
    const eventAtMicros = timestampMicros(eventAt)
    if (!validEventMetadata(decoded) || eventAtMicros < previousEventAt ||
      (eventAtMicros === previousEventAt && eventId <= previousEventId)) return null
    previousEventAt = eventAtMicros
    previousEventId = eventId
    eventIds.add(eventId)
    if (backfilled) decodedBackfilledCount++
    dataPointCount += 1 + metrics.length
    if (dataPointCount > MAX_SNAPSHOT_DATA_POINTS) return null
    events.push(decoded)
  }
  if (decodedBackfilledCount !== backfilledCount) return null
  return {
    version: 'monitoring_event_read_model/v2',
    quality_status: qualityStatus,
    event_count: eventCount,
    backfilled_count: backfilledCount,
    events,
  }
}

export function decodeSubscriptionCostEvidenceReadModel(value: unknown): SubscriptionCostEvidenceReadModel | null {
  const input = record(value)
  if (!input || input.version !== 'subscription_cost_read_model/v1') return null
  const subscriptionId = nonEmptyStringField(input, 'subscription_id', MAX_FIELD_ATOM)
  const vpsId = nonEmptyStringField(input, 'vps_id', MAX_FIELD_ATOM)
  const originalAmount = nonNegativeNumberField(input, 'original_amount')
  const originalCurrency = currencyField(input, 'original_currency')
  const billingPeriodUnit = enumField(input, 'billing_period_unit', BILLING_PERIOD_UNITS)
  const billingPeriodLength = positiveIntegerField(input, 'billing_period_length')
  const conversionRate = numberField(input, 'conversion_rate')
  const conversionProvider = enumField(input, 'conversion_provider', CONVERSION_PROVIDERS)
  const rateDate = stringField(input, 'rate_date')
  const rateFetchedAt = timestampField(input, 'rate_fetched_at')
  const rateStale = booleanField(input, 'rate_stale')
  const baseAmount = nonNegativeNumberField(input, 'base_amount')
  const baseCurrency = currencyField(input, 'base_currency')
  const budgetSource = stringField(input, 'budget_source')
  const budgetCurrency = currencyField(input, 'budget_currency')
  const budgetMonth = stringField(input, 'budget_month')
  const budgetMonthlyLimit = nonNegativeNumberField(input, 'budget_monthly_limit')
  const budgetWarningPct = positiveIntegerField(input, 'budget_warning_pct')
  const budgetStatus = enumField(input, 'budget_status', BUDGET_STATUSES)
  const budgetActualSpend = nonNegativeNumberField(input, 'budget_actual_spend')
  const coverageStart = timestampField(input, 'coverage_start')
  const coverageEnd = timestampField(input, 'coverage_end')
  const coverageStatus = enumField(input, 'coverage_status', COVERAGE_STATUSES)
  const coveredDays = positiveIntegerField(input, 'covered_days')
  const totalDays = positiveIntegerField(input, 'total_days')
  const convertedSubscriptionCount = countField(input, 'converted_subscription_count')
  const missingRateCount = countField(input, 'missing_rate_count')
  if (subscriptionId === null || vpsId === null || originalAmount === null || originalCurrency === null ||
    billingPeriodUnit === null || billingPeriodLength === null || billingPeriodLength > 120 ||
    conversionRate === null || conversionRate <= 0 || conversionProvider === null || rateDate === null ||
    !canonicalDate(rateDate) || rateFetchedAt === null || rateStale === null || baseAmount === null ||
    baseCurrency === null || budgetSource !== 'subscription_monthly_budgets' || budgetCurrency === null ||
    budgetMonth === null || !canonicalMonth(budgetMonth) || budgetMonthlyLimit === null ||
    budgetWarningPct === null || budgetWarningPct > 100 || budgetStatus === null ||
    budgetActualSpend === null || coverageStart === null || coverageEnd === null || coverageStatus === null ||
    coveredDays === null || totalDays === null || convertedSubscriptionCount === null || missingRateCount === null ||
    budgetCurrency !== baseCurrency || timestampMicros(coverageEnd) <= timestampMicros(coverageStart) ||
    timestampMicros(`${rateDate}T00:00:00Z`) > timestampMicros(rateFetchedAt) ||
    Math.abs(baseAmount - originalAmount * conversionRate) > 0.0001 ||
    convertedSubscriptionCount + missingRateCount > MAX_SNAPSHOT_DATA_POINTS) return null
  const identityConversion = conversionProvider === 'identity'
  const durationDays = (timestampMicros(coverageEnd) - timestampMicros(coverageStart)) / 86_400_000_000
  const warningLimit = budgetMonthlyLimit * budgetWarningPct / 100
  const validBudget = (budgetStatus === 'ok' && budgetMonthlyLimit > 0 && budgetActualSpend < warningLimit) ||
    (budgetStatus === 'warning' && budgetMonthlyLimit > 0 && budgetActualSpend >= warningLimit &&
      budgetActualSpend < budgetMonthlyLimit) ||
    (budgetStatus === 'over' && budgetMonthlyLimit > 0 && budgetActualSpend >= budgetMonthlyLimit) ||
    (budgetStatus === 'unknown' && (missingRateCount > 0 || budgetMonthlyLimit <= 0))
  if (identityConversion !== (originalCurrency === baseCurrency) ||
    (identityConversion && conversionRate !== 1) || (missingRateCount > 0 && budgetStatus !== 'unknown') ||
    coveredDays !== durationDays || coveredDays > totalDays ||
    ((coverageStatus === 'complete') !== (coveredDays === totalDays && missingRateCount === 0)) ||
    !validBudget) return null
  return {
    version: 'subscription_cost_read_model/v1',
    subscription_id: subscriptionId,
    vps_id: vpsId,
    original_amount: originalAmount,
    original_currency: originalCurrency,
    billing_period_unit: billingPeriodUnit,
    billing_period_length: billingPeriodLength,
    conversion_rate: conversionRate,
    conversion_provider: conversionProvider,
    rate_date: rateDate,
    rate_fetched_at: rateFetchedAt,
    rate_stale: rateStale,
    base_amount: baseAmount,
    base_currency: baseCurrency,
    budget_source: 'subscription_monthly_budgets',
    budget_currency: budgetCurrency,
    budget_month: budgetMonth,
    budget_monthly_limit: budgetMonthlyLimit,
    budget_warning_pct: budgetWarningPct,
    budget_status: budgetStatus,
    budget_actual_spend: budgetActualSpend,
    coverage_start: coverageStart,
    coverage_end: coverageEnd,
    coverage_status: coverageStatus,
    covered_days: coveredDays,
    total_days: totalDays,
    converted_subscription_count: convertedSubscriptionCount,
    missing_rate_count: missingRateCount,
  }
}

export function decodeCommandAuditEvidenceReadModel(value: unknown): CommandAuditEvidenceReadModel | null {
  const input = record(value)
  if (!input || input.version !== 'command_audit_read_model/v1' || input.command_result_payload_allowed !== false) return null
  const auditCount = positiveIntegerField(input, 'audit_count')
  const retentionSeconds = positiveIntegerField(input, 'command_result_retention_seconds')
  const auditValues = boundedArray(input.audits)
  if (auditCount === null || retentionSeconds !== 86_400 || !auditValues || auditValues.length !== auditCount) return null
  const audits: CommandAuditItemReadModel[] = []
  const auditIds = new Set<string>()
  const actionIdentities = new Map<string, readonly string[]>()
  let previousAuditAt = Number.NEGATIVE_INFINITY
  let previousAuditId = ''
  for (const value of auditValues) {
    const audit = record(value)
    if (!audit) return null
    const auditId = nonEmptyStringField(audit, 'audit_id', MAX_FIELD_ATOM)
    const actionId = stringField(audit, 'action_id')
    const monitoringInstanceId = nonEmptyStringField(audit, 'monitoring_instance_id', MAX_FIELD_ATOM)
    const monitoringInstanceName = stringField(audit, 'monitoring_instance_name')
    const actorUserId = stringField(audit, 'actor_user_id')
    const actorUsername = stringField(audit, 'actor_username')
    const actorDisplayName = stringField(audit, 'actor_display_name')
    const commandId = nonEmptyStringField(audit, 'command_id', MAX_FIELD_ATOM)
    const sensitivity = stringField(audit, 'sensitivity')
    const eventType = enumField(audit, 'event_type', COMMAND_EVENTS)
    const outcome = enumField(audit, 'outcome', COMMAND_OUTCOMES)
    const source = enumField(audit, 'source', COMMAND_SOURCES)
    const exitCode = optionalIntegerField(audit, 'exit_code')
    const occurredAt = timestampField(audit, 'occurred_at')
    if (auditId === null || actionId === null || actionId.length > MAX_FIELD_ATOM ||
      monitoringInstanceId === null || monitoringInstanceName === null || monitoringInstanceName.length > 256 ||
      actorUserId === null || actorUserId.length > MAX_FIELD_ATOM || actorUsername === null || actorUsername.length > 256 ||
      actorDisplayName === null || actorDisplayName.length > 256 || commandId === null || sensitivity === null ||
      COMMAND_SENSITIVITY.get(commandId) !== sensitivity || eventType === null || outcome === null ||
      source === null || exitCode === null || occurredAt === null || auditIds.has(auditId)) return null
    if ((eventType === 'rejected' && (actionId !== '' || exitCode !== undefined || source !== 'web' || outcome !== 'rejected')) ||
      (eventType !== 'rejected' && actionId === '') ||
      (eventType === 'queued' && source !== 'web') ||
      ((eventType === 'dispatched' || eventType === 'completed') && source !== 'agent_sync') ||
      (eventType === 'completed' && (exitCode === undefined || (exitCode === 0) !== (outcome === 'succeeded') ||
        (exitCode !== 0 && outcome !== 'failed'))) ||
      (eventType !== 'completed' && exitCode !== undefined)) return null
    const decoded: CommandAuditItemReadModel = {
      audit_id: auditId,
      action_id: actionId,
      monitoring_instance_id: monitoringInstanceId,
      monitoring_instance_name: monitoringInstanceName,
      actor_user_id: actorUserId,
      actor_username: actorUsername,
      actor_display_name: actorDisplayName,
      command_id: commandId,
      sensitivity,
      event_type: eventType,
      outcome,
      source,
      occurred_at: occurredAt,
    }
    if (exitCode !== undefined) decoded.exit_code = exitCode
    const auditAtMicros = timestampMicros(occurredAt)
    if (auditAtMicros < previousAuditAt ||
      (auditAtMicros === previousAuditAt && auditId <= previousAuditId)) return null
    previousAuditAt = auditAtMicros
    previousAuditId = auditId
    auditIds.add(auditId)
    if (actionId !== '') {
      const identity = [
        monitoringInstanceName, actorUserId, actorUsername, actorDisplayName, commandId, sensitivity, outcome,
      ] as const
      const existingIdentity = actionIdentities.get(actionId)
      if (existingIdentity?.some((field, index) => field !== identity[index])) return null
      actionIdentities.set(actionId, identity)
    }
    audits.push(decoded)
  }
  return {
    version: 'command_audit_read_model/v1',
    audit_count: auditCount,
    command_result_retention_seconds: retentionSeconds,
    command_result_payload_allowed: false,
    audits,
  }
}

export function evidenceReadModelVersion(value: unknown): string | null {
  const input = record(value)
  return input ? stringField(input, 'version') : null
}
