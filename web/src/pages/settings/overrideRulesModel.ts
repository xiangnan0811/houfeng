import type {
  IncidentDefaults,
  IncidentDefaultsOverride,
  MonitoringInstanceLabelOverrideRule,
  OverrideRules,
  ProbeFrequencyOverride,
  SettingsOverrideFields,
  TargetLabelOverrideRule,
  TargetTypeOverrideRule,
} from '../../lib/types'
import { targetTypeLabel } from '../targets/targetHelpers'
import type {
  FrequencyLeafDraft,
  IncidentIntegerKey,
  NotifyLeafKey,
  NumberLeafDraft,
  OverrideLeavesDraft,
  OverrideRuleDraft,
  OverrideScope,
  SettingsFormState,
  StoredOverrideRule,
  TriBoolean,
} from './types'

export type ProbeKind = 'tcp' | 'http' | 'tls'
export type OverrideGroup = 'host' | 'probe' | 'incident'
export type FrequencyTier = '5s' | '1m' | '5m' | '15m' | '6h'

/** maxIncidentDurationSeconds / 2 from the Go settings validator. */
export const MAX_HEARTBEAT_INTERVAL_SECONDS = 4_611_686_018
/** max int64 duration in seconds from the Go settings validator. */
export const MAX_SWEEP_INTERVAL_SECONDS = 9_223_372_036
/** maxInt/4 on 64-bit, kept as a decimal string so it is not rounded. */
export const MAX_STALE_THRESHOLD_INTERVALS = BigInt('2305843009213693951')

export const FREQUENCY_TIER_OPTIONS: readonly { value: FrequencyTier; label: string }[] = [
  { value: '5s', label: '5 秒' },
  { value: '1m', label: '1 分钟' },
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '6h', label: '6 小时' },
]

export const PROBE_KIND_OPTIONS: readonly { value: ProbeKind; label: string }[] = [
  { value: 'tcp', label: 'TCP' },
  { value: 'http', label: 'HTTP' },
  { value: 'tls', label: 'TLS' },
]

const FREQUENCY_LABELS: Record<FrequencyTier, string> = {
  '5s': '5 秒',
  '1m': '1 分钟',
  '5m': '5 分钟',
  '15m': '15 分钟',
  '6h': '6 小时',
}

const TARGET_TYPES = new Set(['service', 'china_reference'])

export type IncidentNumberKind = 'timing' | 'percent' | 'load'
export type IncidentNumberGroup = 'timing' | 'cpu' | 'mem' | 'disk' | 'inode' | 'iowait' | 'load'
export type ThresholdMetric = 'CPU' | '内存' | '磁盘' | 'Inode' | 'IOWait' | 'Load5'
export type ThresholdLevel = 'warning' | 'alert' | 'critical'

export type IncidentNumberField = {
  key: IncidentIntegerKey
  label: string
  unit: string
  kind: IncidentNumberKind
  group: IncidentNumberGroup
  bound?: 'heartbeat' | 'stale' | 'sweep'
  metric?: ThresholdMetric
  level?: ThresholdLevel
}

export const INCIDENT_NUMBER_FIELDS: readonly IncidentNumberField[] = [
  { key: 'heartbeat_interval_seconds', label: '心跳间隔', unit: '秒', kind: 'timing', group: 'timing', bound: 'heartbeat' },
  { key: 'stale_threshold_intervals', label: '失联阈值', unit: '次', kind: 'timing', group: 'timing', bound: 'stale' },
  { key: 'sweep_interval_seconds', label: '扫描间隔', unit: '秒', kind: 'timing', group: 'timing', bound: 'sweep' },
  { key: 'cpu_warning_pct', label: 'CPU 关注', unit: '%', kind: 'percent', group: 'cpu', metric: 'CPU', level: 'warning' },
  { key: 'cpu_alert_pct', label: 'CPU 告警', unit: '%', kind: 'percent', group: 'cpu', metric: 'CPU', level: 'alert' },
  { key: 'cpu_critical_pct', label: 'CPU 严重', unit: '%', kind: 'percent', group: 'cpu', metric: 'CPU', level: 'critical' },
  { key: 'mem_warning_pct', label: '内存 关注', unit: '%', kind: 'percent', group: 'mem', metric: '内存', level: 'warning' },
  { key: 'mem_alert_pct', label: '内存 告警', unit: '%', kind: 'percent', group: 'mem', metric: '内存', level: 'alert' },
  { key: 'mem_critical_pct', label: '内存 严重', unit: '%', kind: 'percent', group: 'mem', metric: '内存', level: 'critical' },
  { key: 'disk_warning_pct', label: '磁盘 关注', unit: '%', kind: 'percent', group: 'disk', metric: '磁盘', level: 'warning' },
  { key: 'disk_alert_pct', label: '磁盘 告警', unit: '%', kind: 'percent', group: 'disk', metric: '磁盘', level: 'alert' },
  { key: 'disk_critical_pct', label: '磁盘 严重', unit: '%', kind: 'percent', group: 'disk', metric: '磁盘', level: 'critical' },
  { key: 'inode_warning_pct', label: 'Inode 关注', unit: '%', kind: 'percent', group: 'inode', metric: 'Inode', level: 'warning' },
  { key: 'inode_alert_pct', label: 'Inode 告警', unit: '%', kind: 'percent', group: 'inode', metric: 'Inode', level: 'alert' },
  { key: 'inode_critical_pct', label: 'Inode 严重', unit: '%', kind: 'percent', group: 'inode', metric: 'Inode', level: 'critical' },
  { key: 'iowait_warning_pct', label: 'IOWait 关注', unit: '%', kind: 'percent', group: 'iowait', metric: 'IOWait', level: 'warning' },
  { key: 'iowait_critical_pct', label: 'IOWait 严重', unit: '%', kind: 'percent', group: 'iowait', metric: 'IOWait', level: 'critical' },
  { key: 'load5_warning', label: 'Load5 关注', unit: '', kind: 'load', group: 'load', metric: 'Load5', level: 'warning' },
  { key: 'load5_critical', label: 'Load5 严重', unit: '', kind: 'load', group: 'load', metric: 'Load5', level: 'critical' },
]

export const NOTIFY_FIELDS: readonly { key: NotifyLeafKey; leaf: 'notifyOnStarted' | 'notifyOnEscalated' | 'notifyOnRecovered'; label: string }[] = [
  { key: 'notify_on_started', leaf: 'notifyOnStarted', label: '开始' },
  { key: 'notify_on_escalated', leaf: 'notifyOnEscalated', label: '升级' },
  { key: 'notify_on_recovered', leaf: 'notifyOnRecovered', label: '恢复' },
]

const DEFAULT_THRESHOLDS: Pick<
  IncidentDefaults,
  | 'cpu_warning_pct'
  | 'cpu_alert_pct'
  | 'cpu_critical_pct'
  | 'mem_warning_pct'
  | 'mem_alert_pct'
  | 'mem_critical_pct'
  | 'disk_warning_pct'
  | 'disk_alert_pct'
  | 'disk_critical_pct'
  | 'inode_warning_pct'
  | 'inode_alert_pct'
  | 'inode_critical_pct'
  | 'iowait_warning_pct'
  | 'iowait_critical_pct'
  | 'load5_warning'
  | 'load5_critical'
> = {
  cpu_warning_pct: 80,
  cpu_alert_pct: 90,
  cpu_critical_pct: 95,
  mem_warning_pct: 85,
  mem_alert_pct: 92,
  mem_critical_pct: 95,
  disk_warning_pct: 85,
  disk_alert_pct: 92,
  disk_critical_pct: 97,
  inode_warning_pct: 80,
  inode_alert_pct: 90,
  inode_critical_pct: 95,
  iowait_warning_pct: 20,
  iowait_critical_pct: 50,
  load5_warning: 4,
  load5_critical: 8,
}

export type OverrideDraftError = {
  ruleKey: string
  path: string
  message: string
}

export type PreviewSourceKind =
  | 'global'
  | 'persisted_probe'
  | 'monitoring_instance_label'
  | 'target_type'
  | 'target_label'

export type FrequencyPreview = {
  value: string
  source: PreviewSourceKind
  ruleNumber: number | null
  fallbackValue: string
  fallbackSource: PreviewSourceKind
  fallbackRuleNumber: number | null
}

export function frequencyTierLabel(value: string): string {
  if (isFrequencyTier(value)) return FREQUENCY_LABELS[value]
  return value
}

export function isFrequencyTier(value: string): value is FrequencyTier {
  return Object.hasOwn(FREQUENCY_LABELS, value)
}

export function overrideGroupApplied(scope: OverrideScope, group: OverrideGroup): boolean {
  if (group === 'incident') return false
  if (group === 'host') return scope === 'monitoring_instance_labels'
  return scope !== 'monitoring_instance_labels'
}

export function scopeHeading(scope: OverrideScope): string {
  switch (scope) {
    case 'monitoring_instance_labels':
      return '监控实例标签'
    case 'target_types':
      return '目标类型'
    case 'target_labels':
      return '目标标签'
  }
}

export function ruleHeading(scope: OverrideScope, index: number): string {
  return `${scopeHeading(scope)}规则 ${index + 1}`
}

export function buildMonitoringOverrideDrafts(rules: MonitoringInstanceLabelOverrideRule[]): OverrideRuleDraft[] {
  return rules.map((rule, index) => buildDraft('monitoring_instance_labels', rule, index))
}

export function buildTargetTypeOverrideDrafts(rules: TargetTypeOverrideRule[]): OverrideRuleDraft[] {
  return rules.map((rule, index) => buildDraft('target_types', rule, index))
}

export function buildTargetLabelOverrideDrafts(rules: TargetLabelOverrideRule[]): OverrideRuleDraft[] {
  return rules.map((rule, index) => buildDraft('target_labels', rule, index))
}

export function createOverrideRule(scope: OverrideScope): OverrideRuleDraft {
  const id = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(16).slice(2)}`
  return {
    key: `${scope}:${id}`,
    scope,
    selector: scope === 'target_types' ? 'service' : '',
    original: null,
    touched: [],
    leaves: emptyLeaves(),
  }
}

export function moveOverrideRule(rules: readonly OverrideRuleDraft[], index: number, direction: -1 | 1): OverrideRuleDraft[] {
  const next = index + direction
  if (next < 0 || next >= rules.length) return [...rules]
  const copy = [...rules]
  const [item] = copy.splice(index, 1)
  if (!item) return [...rules]
  copy.splice(next, 0, item)
  return copy
}

export function removeOverrideRule(rules: readonly OverrideRuleDraft[], index: number): OverrideRuleDraft[] {
  return rules.filter((_, ruleIndex) => ruleIndex !== index)
}

export function setRuleSelector(rule: OverrideRuleDraft, selector: string): OverrideRuleDraft {
  return withTouch({ ...rule, selector }, 'selector')
}

export function setHostMode(rule: OverrideRuleDraft, mode: 'inherit' | 'override', fallbackTier: string): OverrideRuleDraft {
  return withTouch({
    ...rule,
    leaves: {
      ...rule.leaves,
      hostSampleFrequencyTier: nextFrequency(rule.leaves.hostSampleFrequencyTier, mode, fallbackTier),
    },
  }, 'host_sample_frequency_tier')
}

export function setHostValue(rule: OverrideRuleDraft, value: string): OverrideRuleDraft {
  return withTouch({
    ...rule,
    leaves: {
      ...rule.leaves,
      hostSampleFrequencyTier: { mode: 'override', value },
    },
  }, 'host_sample_frequency_tier')
}

export function setProbeMode(
  rule: OverrideRuleDraft,
  kind: ProbeKind,
  mode: 'inherit' | 'override',
  fallbackTier: string,
): OverrideRuleDraft {
  return withTouch({
    ...rule,
    leaves: {
      ...rule.leaves,
      probe: replaceProbe(rule.leaves.probe, kind, nextFrequency(rule.leaves.probe[kind], mode, fallbackTier)),
    },
  }, `probe_frequency_defaults.${kind}`)
}

export function setProbeValue(rule: OverrideRuleDraft, kind: ProbeKind, value: string): OverrideRuleDraft {
  return withTouch({
    ...rule,
    leaves: {
      ...rule.leaves,
      probe: replaceProbe(rule.leaves.probe, kind, { mode: 'override', value }),
    },
  }, `probe_frequency_defaults.${kind}`)
}

export function setIncidentNumberMode(rule: OverrideRuleDraft, key: IncidentIntegerKey, mode: 'inherit' | 'override'): OverrideRuleDraft {
  const current = rule.leaves.incidentNumbers[key]
  return withTouch({
    ...rule,
    leaves: {
      ...rule.leaves,
      incidentNumbers: {
        ...rule.leaves.incidentNumbers,
        [key]: { mode, text: current.text },
      },
    },
  }, `incident_defaults.${key}`)
}

export function setIncidentNumberText(rule: OverrideRuleDraft, key: IncidentIntegerKey, text: string): OverrideRuleDraft {
  return withTouch({
    ...rule,
    leaves: {
      ...rule.leaves,
      incidentNumbers: {
        ...rule.leaves.incidentNumbers,
        [key]: { mode: 'override', text },
      },
    },
  }, `incident_defaults.${key}`)
}

export function setNotifyMode(rule: OverrideRuleDraft, key: NotifyLeafKey, mode: TriBoolean): OverrideRuleDraft {
  const leaves = { ...rule.leaves }
  if (key === 'notify_on_started') leaves.notifyOnStarted = mode
  else if (key === 'notify_on_escalated') leaves.notifyOnEscalated = mode
  else leaves.notifyOnRecovered = mode
  return withTouch({ ...rule, leaves }, `incident_defaults.${key}`)
}

export function comparableSettingsForm(form: SettingsFormState) {
  return {
    ...form,
    monitoringInstanceLabelOverrides: form.monitoringInstanceLabelOverrides.map(comparableRule),
    targetTypeOverrides: form.targetTypeOverrides.map(comparableRule),
    targetLabelOverrides: form.targetLabelOverrides.map(comparableRule),
  }
}

export function compileOverrideRules(input: {
  monitoring: readonly OverrideRuleDraft[]
  targetTypes: readonly OverrideRuleDraft[]
  targetLabels: readonly OverrideRuleDraft[]
  globalIncident: IncidentDefaults | null
}): { rules: OverrideRules; errors: OverrideDraftError[] } {
  const errors: OverrideDraftError[] = []
  const seenMonitoring = new Map<string, number>()
  const seenTypes = new Map<string, number>()
  const seenLabels = new Map<string, number>()
  return {
    rules: {
      monitoring_instance_labels: input.monitoring.map((rule, index) =>
        compileMonitoring(rule, index, seenMonitoring, input.globalIncident, errors),
      ),
      target_types: input.targetTypes.map((rule, index) =>
        compileTargetType(rule, index, seenTypes, input.globalIncident, errors),
      ),
      target_labels: input.targetLabels.map((rule, index) =>
        compileTargetLabel(rule, index, seenLabels, input.globalIncident, errors),
      ),
    },
    errors,
  }
}

export function overrideRulesJson(rules: OverrideRules): string {
  return JSON.stringify(rules, null, 2)
}

/** Zero threshold leaves follow Go's global default fill. Non-zero draft values stay. */
export function normalizeIncidentThresholds(global: IncidentDefaults): IncidentDefaults {
  return {
    ...global,
    cpu_warning_pct: filledThreshold(global.cpu_warning_pct, DEFAULT_THRESHOLDS.cpu_warning_pct),
    cpu_alert_pct: filledThreshold(global.cpu_alert_pct, DEFAULT_THRESHOLDS.cpu_alert_pct),
    cpu_critical_pct: filledThreshold(global.cpu_critical_pct, DEFAULT_THRESHOLDS.cpu_critical_pct),
    mem_warning_pct: filledThreshold(global.mem_warning_pct, DEFAULT_THRESHOLDS.mem_warning_pct),
    mem_alert_pct: filledThreshold(global.mem_alert_pct, DEFAULT_THRESHOLDS.mem_alert_pct),
    mem_critical_pct: filledThreshold(global.mem_critical_pct, DEFAULT_THRESHOLDS.mem_critical_pct),
    disk_warning_pct: filledThreshold(global.disk_warning_pct, DEFAULT_THRESHOLDS.disk_warning_pct),
    disk_alert_pct: filledThreshold(global.disk_alert_pct, DEFAULT_THRESHOLDS.disk_alert_pct),
    disk_critical_pct: filledThreshold(global.disk_critical_pct, DEFAULT_THRESHOLDS.disk_critical_pct),
    inode_warning_pct: filledThreshold(global.inode_warning_pct, DEFAULT_THRESHOLDS.inode_warning_pct),
    inode_alert_pct: filledThreshold(global.inode_alert_pct, DEFAULT_THRESHOLDS.inode_alert_pct),
    inode_critical_pct: filledThreshold(global.inode_critical_pct, DEFAULT_THRESHOLDS.inode_critical_pct),
    iowait_warning_pct: filledThreshold(global.iowait_warning_pct, DEFAULT_THRESHOLDS.iowait_warning_pct),
    iowait_critical_pct: filledThreshold(global.iowait_critical_pct, DEFAULT_THRESHOLDS.iowait_critical_pct),
    load5_warning: filledThreshold(global.load5_warning, DEFAULT_THRESHOLDS.load5_warning),
    load5_critical: filledThreshold(global.load5_critical, DEFAULT_THRESHOLDS.load5_critical),
  }
}

function filledThreshold(value: number, fallback: number): number {
  return value === 0 ? fallback : value
}

export function previewHostFrequency(input: {
  rules: readonly OverrideRuleDraft[]
  monitoringLabels: readonly string[]
  globalHostFrequency: string
}): FrequencyPreview {
  const labels = normalizeTokens(input.monitoringLabels)
  for (let index = 0; index < input.rules.length; index += 1) {
    const rule = input.rules[index]
    if (!rule || rule.scope !== 'monitoring_instance_labels' || !selectorMatches(persistedSelector(rule), labels)) continue
    const tier = hostTier(rule)
    if (tier == null) continue
    return {
      value: tier,
      source: 'monitoring_instance_label',
      ruleNumber: index + 1,
      fallbackValue: input.globalHostFrequency,
      fallbackSource: 'global',
      fallbackRuleNumber: null,
    }
  }
  return {
    value: input.globalHostFrequency,
    source: 'global',
    ruleNumber: null,
    fallbackValue: input.globalHostFrequency,
    fallbackSource: 'global',
    fallbackRuleNumber: null,
  }
}

export function previewProbeFrequency(input: {
  targetTypeRules: readonly OverrideRuleDraft[]
  targetLabelRules: readonly OverrideRuleDraft[]
  targetType: string
  targetLabels: readonly string[]
  probeKind: ProbeKind
  storedProbeFrequency: string
}): FrequencyPreview {
  let value = input.storedProbeFrequency
  let source: PreviewSourceKind = 'persisted_probe'
  let ruleNumber: number | null = null
  let fallbackValue = input.storedProbeFrequency
  let fallbackSource: PreviewSourceKind = 'persisted_probe'
  let fallbackRuleNumber: number | null = null
  const targetType = input.targetType.trim()
  for (let index = 0; index < input.targetTypeRules.length; index += 1) {
    const rule = input.targetTypeRules[index]
    if (!rule || rule.scope !== 'target_types' || persistedSelector(rule) !== targetType) continue
    const tier = probeTier(rule, input.probeKind)
    if (tier != null) {
      fallbackValue = value
      fallbackSource = source
      fallbackRuleNumber = ruleNumber
      value = tier
      source = 'target_type'
      ruleNumber = index + 1
    }
    break
  }
  const labels = normalizeTokens(input.targetLabels)
  for (let index = 0; index < input.targetLabelRules.length; index += 1) {
    const rule = input.targetLabelRules[index]
    if (!rule || rule.scope !== 'target_labels' || !selectorMatches(persistedSelector(rule), labels)) continue
    const tier = probeTier(rule, input.probeKind)
    if (tier == null) continue
    return {
      value: tier,
      source: 'target_label',
      ruleNumber: index + 1,
      fallbackValue: value,
      fallbackSource: source,
      fallbackRuleNumber: ruleNumber,
    }
  }
  return { value, source, ruleNumber, fallbackValue, fallbackSource, fallbackRuleNumber }
}

export function splitLabelTokens(value: string): string[] {
  return normalizeTokens(value.split(/[,\n]/))
}

function compileMonitoring(
  rule: OverrideRuleDraft,
  index: number,
  seen: Map<string, number>,
  globalIncident: IncidentDefaults | null,
  errors: OverrideDraftError[],
): MonitoringInstanceLabelOverrideRule {
  const selector = persistedSelector(rule)
  validateLabelSelector(rule, index, selector, seen, '标签', errors)
  const overrides = materializeOverrides(rule, index, globalIncident, errors)
  return { label: selector, overrides }
}

function compileTargetType(
  rule: OverrideRuleDraft,
  index: number,
  seen: Map<string, number>,
  globalIncident: IncidentDefaults | null,
  errors: OverrideDraftError[],
): TargetTypeOverrideRule {
  const selector = persistedSelector(rule)
  const where = ruleHeading('target_types', index)
  if (selector === '') {
    errors.push({ ruleKey: rule.key, path: 'selector', message: `${where} 的类型不能为空。` })
  } else if (!TARGET_TYPES.has(selector)) {
    errors.push({ ruleKey: rule.key, path: 'selector', message: `${where} 的类型无效。` })
  } else if (seen.has(selector)) {
    errors.push({ ruleKey: rule.key, path: 'selector', message: `${where} 与规则 ${(seen.get(selector) ?? 0) + 1} 的类型「${targetTypeLabel(selector)}」重复。` })
  } else {
    seen.set(selector, index)
  }
  const overrides = materializeOverrides(rule, index, globalIncident, errors)
  return { target_type: selector, overrides }
}

function compileTargetLabel(
  rule: OverrideRuleDraft,
  index: number,
  seen: Map<string, number>,
  globalIncident: IncidentDefaults | null,
  errors: OverrideDraftError[],
): TargetLabelOverrideRule {
  const selector = persistedSelector(rule)
  validateLabelSelector(rule, index, selector, seen, '标签', errors)
  const overrides = materializeOverrides(rule, index, globalIncident, errors)
  return { label: selector, overrides }
}

function validateLabelSelector(
  rule: OverrideRuleDraft,
  index: number,
  selector: string,
  seen: Map<string, number>,
  noun: string,
  errors: OverrideDraftError[],
) {
  const where = ruleHeading(rule.scope, index)
  if (selector === '') {
    errors.push({ ruleKey: rule.key, path: 'selector', message: `${where} 的${noun}不能为空。` })
    return
  }
  const previous = seen.get(selector)
  if (previous !== undefined) {
    errors.push({ ruleKey: rule.key, path: 'selector', message: `${where} 与规则 ${previous + 1} 的${noun}「${selector}」重复。` })
    return
  }
  seen.set(selector, index)
}

function materializeOverrides(
  rule: OverrideRuleDraft,
  index: number,
  globalIncident: IncidentDefaults | null,
  errors: OverrideDraftError[],
): SettingsOverrideFields {
  const where = ruleHeading(rule.scope, index)
  const overrides: SettingsOverrideFields = rule.original ? structuredClone(rule.original.overrides) : {}
  const overrideTouched = rule.touched.some((path) => path !== 'selector')
  if (overrideTouched || !rule.original) applyTouches(rule, overrides, where, errors)
  pruneEmptyOverrides(overrides)
  validateMaterialized(rule, overrides, where, errors)
  if (!hasAnyOverride(overrides)) {
    errors.push({ ruleKey: rule.key, path: 'overrides', message: `${where} 必须至少指定一项覆盖。` })
  } else if (globalIncident && overrides.incident_defaults) {
    const orderError = thresholdOrderError(where, normalizeIncidentThresholds(globalIncident), overrides.incident_defaults)
    if (orderError) errors.push({ ruleKey: rule.key, path: 'incident_defaults', message: orderError })
  }
  return overrides
}

function applyTouches(rule: OverrideRuleDraft, overrides: SettingsOverrideFields, where: string, errors: OverrideDraftError[]) {
  if (rule.touched.includes('host_sample_frequency_tier')) {
    applyFrequency(rule.leaves.hostSampleFrequencyTier, where, '主机采样频率', errors, rule.key, 'host_sample_frequency_tier', (tier) => {
      if (tier == null) delete overrides.host_sample_frequency_tier
      else overrides.host_sample_frequency_tier = tier
    })
  }
  if (rule.touched.some((path) => path.startsWith('probe_frequency_defaults.'))) {
    const probe: ProbeFrequencyOverride = { ...(overrides.probe_frequency_defaults ?? {}) }
    for (const kind of ['tcp', 'http', 'tls'] as const) {
      const path = `probe_frequency_defaults.${kind}`
      if (!rule.touched.includes(path)) continue
      applyFrequency(rule.leaves.probe[kind], where, `${kind.toUpperCase()} 频率`, errors, rule.key, path, (tier) => {
        if (tier == null) delete probe[kind]
        else probe[kind] = tier
      })
    }
    if (probe.tcp === undefined && probe.http === undefined && probe.tls === undefined) delete overrides.probe_frequency_defaults
    else overrides.probe_frequency_defaults = probe
  }
  if (rule.touched.some((path) => path.startsWith('incident_defaults.'))) {
    const incident: IncidentDefaultsOverride = { ...(overrides.incident_defaults ?? {}) }
    for (const field of INCIDENT_NUMBER_FIELDS) {
      const path = `incident_defaults.${field.key}`
      if (!rule.touched.includes(path)) continue
      const leaf = rule.leaves.incidentNumbers[field.key]
      if (leaf.mode === 'inherit') {
        clearIncidentNumber(incident, field.key)
        continue
      }
      const parsed = parseTouchedNumber(leaf.text, field, where)
      if (!parsed.ok) {
        errors.push({ ruleKey: rule.key, path, message: parsed.error })
        clearIncidentNumber(incident, field.key)
      } else {
        writeIncidentNumber(incident, field.key, parsed.value)
      }
    }
    for (const field of NOTIFY_FIELDS) {
      const path = `incident_defaults.${field.key}`
      if (!rule.touched.includes(path)) continue
      const mode = rule.leaves[field.leaf]
      if (mode === 'inherit') clearNotify(incident, field.key)
      else writeNotify(incident, field.key, mode === 'on')
    }
    if (Object.keys(incident).length === 0) delete overrides.incident_defaults
    else overrides.incident_defaults = incident
  }
}

function applyFrequency(
  leaf: FrequencyLeafDraft,
  where: string,
  label: string,
  errors: OverrideDraftError[],
  ruleKey: string,
  path: string,
  write: (tier: string | null) => void,
) {
  if (leaf.mode === 'inherit') {
    write(null)
    return
  }
  const tier = leaf.value.trim()
  if (!isFrequencyTier(tier)) {
    errors.push({ ruleKey, path, message: `${where} 的${label}无效。` })
    write(null)
    return
  }
  write(tier)
}

function validateMaterialized(rule: OverrideRuleDraft, overrides: SettingsOverrideFields, where: string, errors: OverrideDraftError[]) {
  if (overrides.host_sample_frequency_tier !== undefined && !isFrequencyTier(overrides.host_sample_frequency_tier)) {
    errors.push({ ruleKey: rule.key, path: 'host_sample_frequency_tier', message: `${where} 的主机采样频率无效。` })
  }
  const probe = overrides.probe_frequency_defaults
  if (probe) {
    for (const kind of ['tcp', 'http', 'tls'] as const) {
      const tier = probe[kind]
      if (tier !== undefined && !isFrequencyTier(tier)) {
        errors.push({ ruleKey: rule.key, path: `probe_frequency_defaults.${kind}`, message: `${where} 的${kind.toUpperCase()} 频率无效。` })
      }
    }
  }
  const incident = overrides.incident_defaults
  if (!incident) return
  for (const field of INCIDENT_NUMBER_FIELDS) {
    const path = `incident_defaults.${field.key}`
    if (rule.touched.includes(path)) continue
    const value = readIncidentNumber(incident, field.key)
    if (value === undefined) continue
    const message = storedNumberError(value, field, where)
    if (message) errors.push({ ruleKey: rule.key, path, message })
  }
}

function hasAnyOverride(overrides: SettingsOverrideFields): boolean {
  return overrides.host_sample_frequency_tier !== undefined
    || overrides.probe_frequency_defaults !== undefined
    || overrides.incident_defaults !== undefined
}

function pruneEmptyOverrides(overrides: SettingsOverrideFields) {
  const probe = overrides.probe_frequency_defaults
  if (probe && probe.tcp === undefined && probe.http === undefined && probe.tls === undefined) {
    delete overrides.probe_frequency_defaults
  }
  const incident = overrides.incident_defaults
  if (incident && Object.keys(incident).length === 0) delete overrides.incident_defaults
}

function thresholdOrderError(where: string, global: IncidentDefaults, incident: IncidentDefaultsOverride): string | null {
  const cpuWarning = incident.cpu_warning_pct ?? global.cpu_warning_pct
  const cpuAlert = incident.cpu_alert_pct ?? global.cpu_alert_pct
  const cpuCritical = incident.cpu_critical_pct ?? global.cpu_critical_pct
  const memWarning = incident.mem_warning_pct ?? global.mem_warning_pct
  const memAlert = incident.mem_alert_pct ?? global.mem_alert_pct
  const memCritical = incident.mem_critical_pct ?? global.mem_critical_pct
  const diskWarning = incident.disk_warning_pct ?? global.disk_warning_pct
  const diskAlert = incident.disk_alert_pct ?? global.disk_alert_pct
  const diskCritical = incident.disk_critical_pct ?? global.disk_critical_pct
  const inodeWarning = incident.inode_warning_pct ?? global.inode_warning_pct
  const inodeAlert = incident.inode_alert_pct ?? global.inode_alert_pct
  const inodeCritical = incident.inode_critical_pct ?? global.inode_critical_pct
  const iowaitWarning = incident.iowait_warning_pct ?? global.iowait_warning_pct
  const iowaitCritical = incident.iowait_critical_pct ?? global.iowait_critical_pct
  const loadWarning = incident.load5_warning ?? global.load5_warning
  const loadCritical = incident.load5_critical ?? global.load5_critical
  if (!(cpuWarning < cpuAlert && cpuAlert < cpuCritical)) return `${where}：CPU 阈值必须满足 关注 < 告警 < 严重。`
  if (!(memWarning < memAlert && memAlert < memCritical)) return `${where}：内存 阈值必须满足 关注 < 告警 < 严重。`
  if (!(diskWarning < diskAlert && diskAlert < diskCritical)) return `${where}：磁盘 阈值必须满足 关注 < 告警 < 严重。`
  if (!(inodeWarning < inodeAlert && inodeAlert < inodeCritical)) return `${where}：Inode 阈值必须满足 关注 < 告警 < 严重。`
  if (!(iowaitWarning < iowaitCritical)) return `${where}：IOWait 阈值必须满足 关注 < 严重。`
  if (!(loadWarning < loadCritical)) return `${where}：Load5 阈值必须满足 关注 < 严重。`
  return null
}

function parseTouchedNumber(text: string, field: IncidentNumberField, where: string): { ok: true; value: number } | { ok: false; error: string } {
  const trimmed = text.trim()
  if (trimmed === '') return { ok: false, error: `${where} 的${field.label}不能为空。留空不会按 0 保存。` }
  if (field.kind === 'load') {
    if (!/^(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$/.test(trimmed)) {
      return { ok: false, error: `${where} 的${field.label}必须为有限正数。` }
    }
    const value = Number(trimmed)
    if (!Number.isFinite(value) || value <= 0) return { ok: false, error: `${where} 的${field.label}必须为有限正数。` }
    return { ok: true, value }
  }
  const parsed = parseExactPositiveInteger(trimmed)
  if (!parsed.ok) {
    if (parsed.reason === 'blank') return { ok: false, error: `${where} 的${field.label}不能为空。留空不会按 0 保存。` }
    if (parsed.reason === 'unsafe') return { ok: false, error: `${where} 的${field.label}超出可精确保存的整数范围。` }
    if (field.kind === 'percent') return { ok: false, error: `${where} 的${field.label}必须在 1 到 100 之间。` }
    return { ok: false, error: `${where} 的${field.label}必须为正整数。` }
  }
  if (field.kind === 'percent' && (parsed.value < 1 || parsed.value > 100)) {
    return { ok: false, error: `${where} 的${field.label}必须在 1 到 100 之间。` }
  }
  const max = integerMax(field)
  if (max != null && BigInt(parsed.value) > max) return { ok: false, error: `${where} 的${field.label}超出允许范围。` }
  return { ok: true, value: parsed.value }
}

function storedNumberError(value: number, field: IncidentNumberField, where: string): string | null {
  if (!Number.isFinite(value) || value <= 0) {
    return field.kind === 'load'
      ? `${where} 的${field.label}必须为有限正数。`
      : `${where} 的${field.label}必须为正整数。`
  }
  if (field.kind === 'load') return null
  if (field.kind === 'percent') {
    if (!Number.isInteger(value) || value < 1 || value > 100) return `${where} 的${field.label}必须在 1 到 100 之间。`
    return null
  }
  if (field.bound === 'stale') {
    if (Number.isSafeInteger(value)) return null
    if (value > 1e19) return `${where} 的${field.label}超出允许范围。`
    return null
  }
  if (!Number.isSafeInteger(value)) return `${where} 的${field.label}超出可精确保存的整数范围。`
  const max = integerMax(field)
  if (max != null && BigInt(value) > max) return `${where} 的${field.label}超出允许范围。`
  return null
}

function integerMax(field: IncidentNumberField): bigint | null {
  if (field.bound === 'heartbeat') return BigInt(MAX_HEARTBEAT_INTERVAL_SECONDS)
  if (field.bound === 'sweep') return BigInt(MAX_SWEEP_INTERVAL_SECONDS)
  if (field.bound === 'stale') return MAX_STALE_THRESHOLD_INTERVALS
  return null
}

function parseExactPositiveInteger(text: string): { ok: true; value: number } | { ok: false; reason: 'blank' | 'format' | 'unsafe' } {
  if (text === '') return { ok: false, reason: 'blank' }
  if (!/^[1-9]\d*$/.test(text)) return { ok: false, reason: 'format' }
  if (text.length > 16 || !Number.isSafeInteger(Number(text)) || String(Number(text)) !== text) {
    return { ok: false, reason: 'unsafe' }
  }
  return { ok: true, value: Number(text) }
}

function hostTier(rule: OverrideRuleDraft): string | null {
  if (rule.touched.includes('host_sample_frequency_tier') || !rule.original) {
    return rule.leaves.hostSampleFrequencyTier.mode === 'override' ? rule.leaves.hostSampleFrequencyTier.value.trim() : null
  }
  return rule.original.overrides.host_sample_frequency_tier ?? null
}

function probeTier(rule: OverrideRuleDraft, kind: ProbeKind): string | null {
  const path = `probe_frequency_defaults.${kind}`
  if (rule.touched.includes(path) || !rule.original) {
    return rule.leaves.probe[kind].mode === 'override' ? rule.leaves.probe[kind].value.trim() : null
  }
  return rule.original.overrides.probe_frequency_defaults?.[kind] ?? null
}

function persistedSelector(rule: OverrideRuleDraft): string {
  if (!rule.touched.includes('selector') && rule.original) return originalSelector(rule.scope, rule.original).trim()
  return rule.selector.trim()
}

function selectorMatches(selector: string, tokens: readonly string[]): boolean {
  if (selector === '') return false
  return tokens.some((token) => token === selector)
}

function normalizeTokens(values: readonly string[]): string[] {
  return values.map((value) => value.trim()).filter((value) => value !== '')
}

function buildDraft(scope: OverrideScope, rule: StoredOverrideRule, index: number): OverrideRuleDraft {
  return {
    key: `${scope}:${index}`,
    scope,
    selector: originalSelector(scope, rule),
    original: structuredClone(rule),
    touched: [],
    leaves: leavesFromOverrides(rule.overrides),
  }
}

function originalSelector(scope: OverrideScope, rule: StoredOverrideRule): string {
  if (scope === 'target_types' && 'target_type' in rule) return rule.target_type
  if ('label' in rule) return rule.label
  return ''
}

function leavesFromOverrides(overrides: SettingsOverrideFields): OverrideLeavesDraft {
  const numbers = emptyNumbers()
  const incident = overrides.incident_defaults
  if (incident) {
    for (const field of INCIDENT_NUMBER_FIELDS) {
      const value = readIncidentNumber(incident, field.key)
      if (value !== undefined) numbers[field.key] = { mode: 'override', text: String(value) }
    }
  }
  return {
    hostSampleFrequencyTier: frequencyFrom(overrides.host_sample_frequency_tier),
    probe: {
      tcp: frequencyFrom(overrides.probe_frequency_defaults?.tcp),
      http: frequencyFrom(overrides.probe_frequency_defaults?.http),
      tls: frequencyFrom(overrides.probe_frequency_defaults?.tls),
    },
    incidentNumbers: numbers,
    notifyOnStarted: booleanFrom(incident?.notify_on_started),
    notifyOnEscalated: booleanFrom(incident?.notify_on_escalated),
    notifyOnRecovered: booleanFrom(incident?.notify_on_recovered),
  }
}

function emptyLeaves(): OverrideLeavesDraft {
  return {
    hostSampleFrequencyTier: { mode: 'inherit', value: '5s' },
    probe: {
      tcp: { mode: 'inherit', value: '5s' },
      http: { mode: 'inherit', value: '5s' },
      tls: { mode: 'inherit', value: '5s' },
    },
    incidentNumbers: emptyNumbers(),
    notifyOnStarted: 'inherit',
    notifyOnEscalated: 'inherit',
    notifyOnRecovered: 'inherit',
  }
}

function emptyNumbers(): Record<IncidentIntegerKey, NumberLeafDraft> {
  const numbers = {} as Record<IncidentIntegerKey, NumberLeafDraft>
  for (const field of INCIDENT_NUMBER_FIELDS) numbers[field.key] = { mode: 'inherit', text: '' }
  return numbers
}

function frequencyFrom(value: string | undefined): FrequencyLeafDraft {
  if (value == null) return { mode: 'inherit', value: '5s' }
  return { mode: 'override', value }
}

function booleanFrom(value: boolean | undefined): TriBoolean {
  if (value === undefined) return 'inherit'
  return value ? 'on' : 'off'
}

function replaceProbe(
  probe: OverrideLeavesDraft['probe'],
  kind: ProbeKind,
  leaf: FrequencyLeafDraft,
): OverrideLeavesDraft['probe'] {
  if (kind === 'tcp') return { ...probe, tcp: leaf }
  if (kind === 'http') return { ...probe, http: leaf }
  return { ...probe, tls: leaf }
}

function nextFrequency(current: FrequencyLeafDraft, mode: 'inherit' | 'override', fallbackTier: string): FrequencyLeafDraft {
  if (mode === 'inherit') return { mode: 'inherit', value: current.value }
  const value = current.mode === 'override' && isFrequencyTier(current.value) ? current.value : fallbackTier
  return { mode: 'override', value: isFrequencyTier(value) ? value : '5s' }
}

function withTouch(rule: OverrideRuleDraft, path: string): OverrideRuleDraft {
  if (rule.touched.includes(path)) return rule
  return { ...rule, touched: [...rule.touched, path].sort() }
}

function comparableRule(rule: OverrideRuleDraft) {
  const incidentNumbers = {} as Record<IncidentIntegerKey, NumberLeafDraft>
  for (const field of INCIDENT_NUMBER_FIELDS) {
    incidentNumbers[field.key] = comparableNumber(rule.leaves.incidentNumbers[field.key])
  }
  return {
    scope: rule.scope,
    selector: rule.selector,
    leaves: {
      hostSampleFrequencyTier: comparableFrequency(rule.leaves.hostSampleFrequencyTier),
      probe: {
        tcp: comparableFrequency(rule.leaves.probe.tcp),
        http: comparableFrequency(rule.leaves.probe.http),
        tls: comparableFrequency(rule.leaves.probe.tls),
      },
      incidentNumbers,
      notifyOnStarted: rule.leaves.notifyOnStarted,
      notifyOnEscalated: rule.leaves.notifyOnEscalated,
      notifyOnRecovered: rule.leaves.notifyOnRecovered,
    },
  }
}

function comparableFrequency(leaf: FrequencyLeafDraft): FrequencyLeafDraft {
  if (leaf.mode === 'inherit') return { mode: 'inherit', value: '' }
  return { mode: 'override', value: leaf.value }
}

function comparableNumber(leaf: NumberLeafDraft): NumberLeafDraft {
  if (leaf.mode === 'inherit') return { mode: 'inherit', text: '' }
  return { mode: 'override', text: leaf.text }
}

function writeIncidentNumber(target: IncidentDefaultsOverride, key: IncidentIntegerKey, value: number) {
  const record = target as unknown as Record<IncidentIntegerKey, number>
  record[key] = value
}

function clearIncidentNumber(target: IncidentDefaultsOverride, key: IncidentIntegerKey) {
  const record = target as unknown as Record<IncidentIntegerKey, number>
  delete record[key]
}

function writeNotify(target: IncidentDefaultsOverride, key: NotifyLeafKey, value: boolean) {
  const record = target as unknown as Record<NotifyLeafKey, boolean>
  record[key] = value
}

function clearNotify(target: IncidentDefaultsOverride, key: NotifyLeafKey) {
  const record = target as unknown as Record<NotifyLeafKey, boolean>
  delete record[key]
}

function readIncidentNumber(target: IncidentDefaultsOverride, key: IncidentIntegerKey): number | undefined {
  const record = target as unknown as Record<IncidentIntegerKey, number | undefined>
  return record[key]
}
