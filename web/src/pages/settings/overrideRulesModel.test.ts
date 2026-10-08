import { describe, expect, it } from 'vitest'

import type { IncidentDefaults, SettingsOverrideFields } from '../../lib/types'
import {
  buildMonitoringOverrideDrafts,
  buildTargetLabelOverrideDrafts,
  buildTargetTypeOverrideDrafts,
  compileOverrideRules,
  createOverrideRule,
  moveOverrideRule,
  overrideRulesJson,
  previewHostFrequency,
  previewProbeFrequency,
  setHostValue,
  setIncidentNumberMode,
  setIncidentNumberText,
  setNotifyMode,
  setProbeMode,
  setProbeValue,
  setRuleSelector,
} from './overrideRulesModel'
import type { OverrideRuleDraft } from './types'

function globalIncident(patch: Partial<IncidentDefaults> = {}): IncidentDefaults {
  return {
    heartbeat_interval_seconds: 5,
    stale_threshold_intervals: 12,
    sweep_interval_seconds: 5,
    notify_on_started: true,
    notify_on_escalated: true,
    notify_on_recovered: true,
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
    ...patch,
  }
}

function first(rules: OverrideRuleDraft[]): OverrideRuleDraft {
  const rule = rules[0]
  if (!rule) throw new Error('expected a rule')
  return rule
}

function monitoring(label: string, overrides: SettingsOverrideFields) {
  return first(buildMonitoringOverrideDrafts([{ label, overrides }]))
}

function targetType(targetTypeName: string, overrides: SettingsOverrideFields) {
  return first(buildTargetTypeOverrideDrafts([{ target_type: targetTypeName, overrides }]))
}

function targetLabel(label: string, overrides: SettingsOverrideFields) {
  return first(buildTargetLabelOverrideDrafts([{ label, overrides }]))
}

function compile(rules: {
  monitoring?: OverrideRuleDraft[]
  targetTypes?: OverrideRuleDraft[]
  targetLabels?: OverrideRuleDraft[]
  globalIncident?: IncidentDefaults | null
}) {
  return compileOverrideRules({
    monitoring: rules.monitoring ?? [],
    targetTypes: rules.targetTypes ?? [],
    targetLabels: rules.targetLabels ?? [],
    globalIncident: rules.globalIncident === undefined ? globalIncident() : rules.globalIncident,
  })
}

describe('override rule draft', () => {
  it('preserves untouched siblings, explicit false, and the original number when another leaf changes', () => {
    const weird = 0.1 + 0.2
    const source = {
      label: 'edge',
      overrides: {
        host_sample_frequency_tier: '1m',
        probe_frequency_defaults: { tcp: '5s', http: '1m', tls: '6h' },
        incident_defaults: {
          stale_threshold_intervals: 4,
          notify_on_started: false,
          load5_warning: weird,
          load5_critical: 8,
        },
      },
    }
    const draft = first(buildMonitoringOverrideDrafts([source]))
    draft.leaves.incidentNumbers.load5_warning.text = '1'
    const edited = setProbeValue(draft, 'http', '15m')
    const compiled = compile({ monitoring: [edited] })

    expect(compiled.errors).toEqual([])
    expect(compiled.rules.monitoring_instance_labels).toEqual([
      {
        label: 'edge',
        overrides: {
          host_sample_frequency_tier: '1m',
          probe_frequency_defaults: { tcp: '5s', http: '15m', tls: '6h' },
          incident_defaults: {
            stale_threshold_intervals: 4,
            notify_on_started: false,
            load5_warning: weird,
            load5_critical: 8,
          },
        },
      },
    ])
    expect(source.overrides.probe_frequency_defaults.http).toBe('1m')
    expect(edited.original).toBe(draft.original)
    expect(overrideRulesJson(compiled.rules)).toContain('false')
    expect(overrideRulesJson(compiled.rules)).not.toContain('touched')
    expect(overrideRulesJson(compiled.rules)).not.toContain('inherit')
  })

  it('prunes a nested object when its last leaf is cleared and keeps false on a sibling clear', () => {
    const probe = setProbeMode(monitoring('edge', {
      host_sample_frequency_tier: '1m',
      probe_frequency_defaults: { http: '1m' },
    }), 'http', 'inherit', '5s')
    expect(compile({ monitoring: [probe] }).rules.monitoring_instance_labels[0]?.overrides).toEqual({
      host_sample_frequency_tier: '1m',
    })

    const partial = setProbeMode(monitoring('edge', {
      probe_frequency_defaults: { http: '1m', tls: '6h' },
    }), 'http', 'inherit', '5s')
    expect(compile({ monitoring: [partial] }).rules.monitoring_instance_labels[0]?.overrides).toEqual({
      probe_frequency_defaults: { tls: '6h' },
    })

    const incident = setIncidentNumberMode(monitoring('edge', {
      host_sample_frequency_tier: '5s',
      incident_defaults: { notify_on_started: false, cpu_warning_pct: 70 },
    }), 'cpu_warning_pct', 'inherit')
    expect(compile({ monitoring: [incident] }).rules.monitoring_instance_labels[0]?.overrides.incident_defaults).toEqual({
      notify_on_started: false,
    })

    const cleared = setNotifyMode(incident, 'notify_on_started', 'inherit')
    const removed = compile({ monitoring: [cleared] })
    expect(removed.rules.monitoring_instance_labels[0]?.overrides).toEqual({
      host_sample_frequency_tier: '5s',
    })
    expect(removed.errors).toEqual([])
  })

  it('moves the original rule with its edits', () => {
    const low = monitoring('low', { host_sample_frequency_tier: '5s' })
    const high = setHostValue(monitoring('high', { host_sample_frequency_tier: '1m' }), '15m')
    const moved = moveOverrideRule([low, high], 1, -1)
    expect(moved[0]).toBe(high)
    expect(moved[1]).toBe(low)
    expect(moved[0]?.original).toBe(high.original)
    expect(compile({ monitoring: moved }).rules.monitoring_instance_labels.map((rule) => rule.label)).toEqual(['high', 'low'])
    expect(compile({ monitoring: moved }).rules.monitoring_instance_labels[0]?.overrides.host_sample_frequency_tier).toBe('15m')
  })

  it('rejects blank, zero, unsafe, and out-of-range values without turning them into zero', () => {
    const blank = setIncidentNumberText(
      setIncidentNumberMode(monitoring('edge', { host_sample_frequency_tier: '1m' }), 'cpu_warning_pct', 'override'),
      'cpu_warning_pct',
      '   ',
    )
    const blankResult = compile({ monitoring: [blank] })
    expect(blankResult.errors.map((error) => error.message).join('\n')).toContain('不能为空。留空不会按 0 保存。')
    expect(blankResult.rules.monitoring_instance_labels[0]?.overrides.incident_defaults?.cpu_warning_pct).toBeUndefined()

    const zero = setIncidentNumberText(blank, 'cpu_warning_pct', '0')
    expect(compile({ monitoring: [zero] }).errors.map((error) => error.message).join('\n')).toContain('必须在 1 到 100 之间。')

    const unsafe = setIncidentNumberText(
      setIncidentNumberMode(monitoring('edge', { host_sample_frequency_tier: '1m' }), 'stale_threshold_intervals', 'override'),
      'stale_threshold_intervals',
      '9007199254740993',
    )
    expect(compile({ monitoring: [unsafe] }).errors.map((error) => error.message).join('\n')).toContain('超出可精确保存的整数范围。')

    const heartbeat = setIncidentNumberText(
      setIncidentNumberMode(monitoring('edge', { host_sample_frequency_tier: '1m' }), 'heartbeat_interval_seconds', 'override'),
      'heartbeat_interval_seconds',
      '4611686019',
    )
    expect(compile({ monitoring: [heartbeat] }).errors.map((error) => error.message).join('\n')).toContain('超出允许范围。')
    const heartbeatOk = setIncidentNumberText(heartbeat, 'heartbeat_interval_seconds', '4611686018')
    expect(compile({ monitoring: [heartbeatOk] }).errors).toEqual([])

    const load = setIncidentNumberText(
      setIncidentNumberMode(monitoring('edge', { host_sample_frequency_tier: '1m' }), 'load5_warning', 'override'),
      'load5_warning',
      '0',
    )
    expect(compile({ monitoring: [load] }).errors.map((error) => error.message).join('\n')).toContain('必须为有限正数。')
    expect(compile({
      monitoring: [setIncidentNumberText(load, 'load5_warning', '0.5')],
    }).rules.monitoring_instance_labels[0]?.overrides.incident_defaults?.load5_warning).toBe(0.5)

    const empty = setRuleSelector(createOverrideRule('monitoring_instance_labels'), 'edge')
    expect(compile({ monitoring: [empty] }).errors.map((error) => error.message).join('\n')).toContain('必须至少指定一项覆盖。')

    const spaced = setRuleSelector(monitoring('edge', { host_sample_frequency_tier: '1m' }), ' edge ')
    const duplicate = setRuleSelector(monitoring('other', { host_sample_frequency_tier: '5m' }), 'edge')
    expect(compile({ monitoring: [spaced, duplicate] }).errors.map((error) => error.message).join('\n')).toContain('重复')
    expect(compile({
      monitoring: [
        setRuleSelector(monitoring('edge', { host_sample_frequency_tier: '1m' }), 'Edge'),
        monitoring('edge', { host_sample_frequency_tier: '5m' }),
      ],
    }).errors).toEqual([])
    expect(compile({
      targetTypes: [setRuleSelector(targetType('service', { host_sample_frequency_tier: '1m' }), 'http')],
    }).errors.map((error) => error.message).join('\n')).toContain('类型无效')
  })

  it('orders each rule against the current normalized global draft', () => {
    const custom = globalIncident({
      cpu_warning_pct: 10,
      cpu_alert_pct: 20,
      cpu_critical_pct: 30,
      load5_warning: 4,
      load5_critical: 10,
    })
    const highWarning = setIncidentNumberText(
      setIncidentNumberMode(targetType('service', { incident_defaults: { cpu_warning_pct: 15 } }), 'cpu_warning_pct', 'override'),
      'cpu_warning_pct',
      '25',
    )
    expect(compile({ targetTypes: [highWarning], globalIncident: custom }).errors.map((error) => error.message).join('\n')).toContain(
      'CPU 阈值必须满足 关注 < 告警 < 严重。',
    )
    const lowWarning = setIncidentNumberText(highWarning, 'cpu_warning_pct', '15')
    expect(compile({ targetTypes: [lowWarning], globalIncident: custom }).errors).toEqual([])

    const load = setIncidentNumberText(
      setIncidentNumberMode(monitoring('edge', { host_sample_frequency_tier: '1m' }), 'load5_warning', 'override'),
      'load5_warning',
      '9',
    )
    expect(compile({ monitoring: [load], globalIncident: custom }).errors).toEqual([])
    expect(compile({
      monitoring: [setIncidentNumberText(load, 'load5_warning', '11')],
      globalIncident: custom,
    }).errors.map((error) => error.message).join('\n')).toContain('Load5 阈值必须满足 关注 < 严重。')

    const filled = setIncidentNumberText(
      setIncidentNumberMode(monitoring('edge', { host_sample_frequency_tier: '1m' }), 'cpu_alert_pct', 'override'),
      'cpu_alert_pct',
      '70',
    )
    expect(compile({
      monitoring: [filled],
      globalIncident: globalIncident({ cpu_warning_pct: 0 }),
    }).errors.map((error) => error.message).join('\n')).toContain('CPU 阈值必须满足 关注 < 告警 < 严重。')
  })

  it('resolves host and probe previews by field presence, not by the first matching rule', () => {
    const missingHost = monitoring('edge', { probe_frequency_defaults: { http: '1m' } })
    const laterHost = monitoring('edge', { host_sample_frequency_tier: '15m' })
    const otherCase = monitoring('Edge', { host_sample_frequency_tier: '6h' })
    expect(previewHostFrequency({
      rules: [missingHost, laterHost, otherCase],
      monitoringLabels: [' edge '],
      globalHostFrequency: '5s',
    })).toMatchObject({
      value: '15m',
      source: 'monitoring_instance_label',
      ruleNumber: 2,
      fallbackValue: '5s',
      fallbackSource: 'global',
    })
    expect(previewHostFrequency({
      rules: [missingHost],
      monitoringLabels: ['edge'],
      globalHostFrequency: '5s',
    })).toMatchObject({ value: '5s', source: 'global', ruleNumber: null })
    expect(previewHostFrequency({
      rules: [targetType('service', { host_sample_frequency_tier: '6h' })],
      monitoringLabels: ['service'],
      globalHostFrequency: '5s',
    }).value).toBe('5s')

    const typeWithoutKind = targetType('service', { host_sample_frequency_tier: '1m', probe_frequency_defaults: { tls: '6h' } })
    const laterType = targetType('service', { probe_frequency_defaults: { http: '5m' } })
    const labelWithoutKind = targetLabel('external', { host_sample_frequency_tier: '6h', probe_frequency_defaults: { tls: '6h' } })
    const laterLabel = targetLabel('external', { probe_frequency_defaults: { http: '1m' } })
    expect(previewProbeFrequency({
      targetTypeRules: [typeWithoutKind, laterType],
      targetLabelRules: [labelWithoutKind, laterLabel],
      targetType: 'service',
      targetLabels: [' external '],
      probeKind: 'http',
      storedProbeFrequency: '15m',
    })).toMatchObject({
      value: '1m',
      source: 'target_label',
      ruleNumber: 2,
      fallbackValue: '15m',
      fallbackSource: 'persisted_probe',
    })

    const typeWithKind = targetType('service', { probe_frequency_defaults: { http: '5m' } })
    expect(previewProbeFrequency({
      targetTypeRules: [typeWithKind],
      targetLabelRules: [laterLabel],
      targetType: 'service',
      targetLabels: ['external'],
      probeKind: 'http',
      storedProbeFrequency: '15m',
    })).toMatchObject({
      value: '1m',
      source: 'target_label',
      ruleNumber: 1,
      fallbackValue: '5m',
      fallbackSource: 'target_type',
      fallbackRuleNumber: 1,
    })
    expect(previewProbeFrequency({
      targetTypeRules: [],
      targetLabelRules: [],
      targetType: 'service',
      targetLabels: [],
      probeKind: 'http',
      storedProbeFrequency: '15m',
    })).toMatchObject({ value: '15m', source: 'persisted_probe' })
  })
})
