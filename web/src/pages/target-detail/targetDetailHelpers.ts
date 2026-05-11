import type { ProbeCreateFormState } from '../../components/target-detail'
import type { TargetRuntimeAction } from '../../components/target-detail'
import { ApiError } from '../../lib/api'
import type {
  CreateProbeItemInput,
  ProbeItemRecord,
  ProbeKind,
  TargetRecord,
  UpdateProbeItemInput,
} from '../../lib/types'
import {
  DEFAULT_FREQUENCY_BY_PROBE_KIND,
  INITIAL_PROBE_CREATE_FORM,
  PROBE_CONFIG_KEYS,
} from './targetDetailConstants'
import type { TargetDetailPageState } from './types'

export const INITIAL_TARGET_DETAIL_STATE: TargetDetailPageState = {
  requestedTargetId: null,
  error: null,
  target: null,
  probeItems: [],
  runtimeFacts: null,
  requestedActivityTargetId: null,
  incidents: [],
  incidentsError: null,
  events: [],
  eventsError: null,
}

export function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

export function parseLabels(value: string) {
  return value
    .split(/[,，]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function dedupeLabels(values: string[]) {
  return values.filter((value, index) => values.indexOf(value) === index)
}

function parseOptionalPositiveInteger(value: string, label: string): number | undefined {
  const normalized = value.trim()
  if (normalized === '') return undefined
  if (!/^[1-9]\d*$/.test(normalized)) {
    throw new Error(`${label}必须为正整数。`)
  }
  return Number.parseInt(normalized, 10)
}

function parseRequiredPositiveInteger(value: string, label: string): number {
  const parsed = parseOptionalPositiveInteger(value, label)
  if (parsed == null) {
    throw new Error(`${label}必须为正整数。`)
  }
  return parsed
}

export function buildProbeCreateInput(form: ProbeCreateFormState): CreateProbeItemInput {
  const timeoutSeconds = parseRequiredPositiveInteger(form.timeoutSeconds, '超时秒数')
  if (form.probeKind === 'tcp') {
    return {
      probe_kind: 'tcp',
      enabled: form.enabled,
      frequency_tier: form.frequencyTier,
      timeout_seconds: timeoutSeconds,
      config: { port: parseRequiredPositiveInteger(form.port, '端口') },
    }
  }
  if (form.probeKind === 'http') {
    const start = parseRequiredPositiveInteger(form.expectedStatusStart, '期望状态码起点')
    const end = parseRequiredPositiveInteger(form.expectedStatusEnd, '期望状态码终点')
    if (start > end) {
      throw new Error('期望状态码起点不能大于终点。')
    }
    return {
      probe_kind: 'http',
      enabled: form.enabled,
      frequency_tier: form.frequencyTier,
      timeout_seconds: timeoutSeconds,
      config: {
        scheme: form.httpScheme.trim(),
        path: form.httpPath.trim() || '/',
        method: form.httpMethod,
        expected_status_range: [start, end],
      },
    }
  }
  return {
    probe_kind: 'tls',
    enabled: form.enabled,
    frequency_tier: form.frequencyTier,
    timeout_seconds: timeoutSeconds,
    config: {
      port: parseRequiredPositiveInteger(form.port, '端口'),
      expiry_warning_days: parseRequiredPositiveInteger(form.tlsExpiryWarningDays, '证书预警天数'),
    },
  }
}

export function buildProbeUpdateInput(form: ProbeCreateFormState): UpdateProbeItemInput {
  return buildProbeCreateInput(form)
}

export function initialProbeCreateFormForTarget(target: TargetRecord): ProbeCreateFormState {
  return {
    ...INITIAL_PROBE_CREATE_FORM,
    port: target.base_port ? String(target.base_port) : '',
  }
}

export function probeCreateFormForKind(
  current: ProbeCreateFormState,
  probeKind: ProbeKind,
): ProbeCreateFormState {
  return {
    ...current,
    probeKind,
    frequencyTier: DEFAULT_FREQUENCY_BY_PROBE_KIND[probeKind],
  }
}

export function formStateForProbeItem(probeItem: ProbeItemRecord): ProbeCreateFormState {
  const config = probeItem.config as Record<string, unknown>
  if (probeItem.probe_kind === 'http') {
    const range = Array.isArray(config.expected_status_range)
      ? config.expected_status_range
      : []
    return {
      ...INITIAL_PROBE_CREATE_FORM,
      probeKind: 'http',
      enabled: probeItem.enabled,
      frequencyTier: probeItem.frequency_tier,
      timeoutSeconds: String(probeItem.timeout_seconds),
      httpScheme: typeof config.scheme === 'string' ? config.scheme : 'https',
      httpPath: typeof config.path === 'string' ? config.path : '/',
      httpMethod: config.method === 'HEAD' ? 'HEAD' : 'GET',
      expectedStatusStart: String(typeof range[0] === 'number' ? range[0] : 200),
      expectedStatusEnd: String(typeof range[1] === 'number' ? range[1] : 299),
    }
  }
  if (probeItem.probe_kind === 'tls') {
    return {
      ...INITIAL_PROBE_CREATE_FORM,
      probeKind: 'tls',
      enabled: probeItem.enabled,
      frequencyTier: probeItem.frequency_tier,
      timeoutSeconds: String(probeItem.timeout_seconds),
      port: String(typeof config.port === 'number' ? config.port : ''),
      tlsExpiryWarningDays: String(
        typeof config.expiry_warning_days === 'number' ? config.expiry_warning_days : 14,
      ),
    }
  }
  return {
    ...INITIAL_PROBE_CREATE_FORM,
    probeKind: 'tcp',
    enabled: probeItem.enabled,
    frequencyTier: probeItem.frequency_tier,
    timeoutSeconds: String(probeItem.timeout_seconds),
    port: String(typeof config.port === 'number' ? config.port : ''),
  }
}

export function hasUnsupportedProbeConfigFields(probeItem: ProbeItemRecord): boolean {
  const config = probeItem.config
  if (config == null || typeof config !== 'object' || Array.isArray(config)) {
    return true
  }
  const allowedKeys = PROBE_CONFIG_KEYS[probeItem.probe_kind]
  return Object.keys(config).some((key) => !allowedKeys.has(key))
}

export function focusRestoreActionAfterSuccess(action: TargetRuntimeAction): TargetRuntimeAction {
  switch (action) {
    case 'enter-maintenance':
      return 'exit-maintenance'
    case 'exit-maintenance':
      return 'enter-maintenance'
    case 'pause':
      return 'resume'
    case 'resume':
      return 'pause'
    case 'archive':
      return 'restore-to-paused'
    case 'restore-to-paused':
      return 'resume'
  }
}

export function mergeRuntimeTargetRecord(current: TargetRecord, updated: TargetRecord): TargetRecord {
  return {
    ...updated,
    labels: current.labels,
    note: current.note,
  }
}
