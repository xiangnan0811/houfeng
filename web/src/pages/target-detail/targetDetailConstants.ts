import type { ProbeCreateFormState } from '../../components/target-detail'
import type { FrequencyTier, ProbeKind } from '../../lib/types'
import type { HistoryTab, TimeWindow } from './types'

export const DEFAULT_FREQUENCY_BY_PROBE_KIND: Record<ProbeKind, FrequencyTier> = {
  tcp: '5s',
  http: '5s',
  tls: '6h',
}

export const PROBE_CONFIG_KEYS: Record<ProbeKind, Set<string>> = {
  tcp: new Set(['port']),
  http: new Set(['scheme', 'path', 'method', 'expected_status_range']),
  tls: new Set(['port', 'expiry_warning_days']),
}

export const INITIAL_PROBE_CREATE_FORM: ProbeCreateFormState = {
  probeKind: 'tcp',
  enabled: true,
  frequencyTier: '5s',
  timeoutSeconds: '5',
  port: '',
  httpScheme: 'https',
  httpPath: '/',
  httpMethod: 'GET',
  expectedStatusStart: '200',
  expectedStatusEnd: '299',
  tlsExpiryWarningDays: '14',
}

export const TIME_WINDOW_ITEMS: Array<{ value: TimeWindow; label: string }> = [
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
]

export const HISTORY_TAB_ITEMS: Array<{ value: HistoryTab; label: string }> = [
  { value: 'events', label: '事件时间线' },
  { value: 'incidents', label: '历史异常' },
]
