import { useCallback, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'

import { listMonitoringInstances, listTargets } from '../../lib/api'
import type { IncidentDefaults, ProbeFrequencyDefaults } from '../../lib/types'
import { TARGET_TYPE_OPTIONS, distinctSorted, targetTypeLabel } from '../targets/targetHelpers'
import {
  FREQUENCY_TIER_OPTIONS,
  INCIDENT_NUMBER_FIELDS,
  NOTIFY_FIELDS,
  PROBE_KIND_OPTIONS,
  compileOverrideRules,
  createOverrideRule,
  frequencyTierLabel,
  moveOverrideRule,
  overrideGroupApplied,
  overrideRulesJson,
  previewHostFrequency,
  previewProbeFrequency,
  removeOverrideRule,
  ruleHeading,
  scopeHeading,
  setHostMode,
  setHostValue,
  setIncidentNumberMode,
  setIncidentNumberText,
  setNotifyMode,
  setProbeMode,
  setProbeValue,
  setRuleSelector,
  splitLabelTokens,
  type FrequencyPreview,
  type IncidentNumberField,
  type IncidentNumberGroup,
  type OverrideDraftError,
  type PreviewSourceKind,
  type ProbeKind,
} from './overrideRulesModel'
import type {
  FrequencyLeafDraft,
  NumberLeafDraft,
  OverrideRuleDraft,
  OverrideScope,
  SettingsFormState,
  TriBoolean,
} from './types'
import './OverrideRulesSection.css'

type RulesPatch = Partial<
  Pick<SettingsFormState, 'monitoringInstanceLabelOverrides' | 'targetTypeOverrides' | 'targetLabelOverrides'>
>

type OverrideRulesSectionProps = {
  monitoringInstanceLabelOverrides: OverrideRuleDraft[]
  targetTypeOverrides: OverrideRuleDraft[]
  targetLabelOverrides: OverrideRuleDraft[]
  globalHostFrequency: string
  globalProbeDefaults: ProbeFrequencyDefaults
  globalIncident: IncidentDefaults | null
  onChange: (patch: RulesPatch) => void
}

const INHERIT_OVERRIDE = [
  { value: 'inherit', label: '继承' },
  { value: 'override', label: '覆盖' },
] as const

const NOTIFY_CHOICES = [
  { value: 'inherit', label: '继承' },
  { value: 'on', label: '开启' },
  { value: 'off', label: '关闭' },
] as const

const NUMBER_GROUPS: readonly { group: IncidentNumberGroup; title: string; columns: 2 | 3 }[] = [
  { group: 'timing', title: '计时', columns: 3 },
  { group: 'cpu', title: 'CPU', columns: 3 },
  { group: 'mem', title: '内存', columns: 3 },
  { group: 'disk', title: '磁盘', columns: 3 },
  { group: 'inode', title: 'Inode', columns: 3 },
  { group: 'iowait', title: 'IOWait', columns: 2 },
  { group: 'load', title: 'Load5', columns: 2 },
]

function blockEnter(event: KeyboardEvent<HTMLInputElement>) {
  if (event.key === 'Enter') event.preventDefault()
}

function sourceText(source: PreviewSourceKind, ruleNumber: number | null): string {
  switch (source) {
    case 'global':
      return '全局'
    case 'persisted_probe':
      return '探测项已存频率'
    case 'monitoring_instance_label':
      return `监控实例标签规则 ${ruleNumber ?? ''}`
    case 'target_type':
      return `目标类型规则 ${ruleNumber ?? ''}`
    case 'target_label':
      return `目标标签规则 ${ruleNumber ?? ''}`
  }
}

function fallbackText(preview: FrequencyPreview): string {
  const source = sourceText(preview.fallbackSource, preview.fallbackRuleNumber)
  return `${source} ${frequencyTierLabel(preview.fallbackValue)}`
}

export function OverrideRulesSection({
  monitoringInstanceLabelOverrides,
  targetTypeOverrides,
  targetLabelOverrides,
  globalHostFrequency,
  globalProbeDefaults,
  globalIncident,
  onChange,
}: OverrideRulesSectionProps) {
  const compiled = compileOverrideRules({
    monitoring: monitoringInstanceLabelOverrides,
    targetTypes: targetTypeOverrides,
    targetLabels: targetLabelOverrides,
    globalIncident,
  })
  const [jsonOpen, setJsonOpen] = useState(false)
  const [probeKind, setProbeKind] = useState<ProbeKind>('http')
  const [storedOverride, setStoredOverride] = useState<string | null>(null)
  const storedFrequency = storedOverride ?? globalProbeDefaults[probeKind]
  const [monitoringLabels, setMonitoringLabels] = useState('')
  const [previewTargetType, setPreviewTargetType] = useState('service')
  const [targetLabels, setTargetLabels] = useState('')
  const monitoringSuggestions = useLazyLabels(useCallback(
    () => listMonitoringInstances('active').then((rows) => distinctSorted(rows.flatMap((row) => row.labels))),
    [],
  ))
  const targetSuggestions = useLazyLabels(useCallback(
    () => listTargets().then((rows) => distinctSorted(rows.flatMap((row) => row.labels))),
    [],
  ))

  const hostPreview = previewHostFrequency({
    rules: monitoringInstanceLabelOverrides,
    monitoringLabels: splitLabelTokens(monitoringLabels),
    globalHostFrequency,
  })
  const probePreview = previewProbeFrequency({
    targetTypeRules: targetTypeOverrides,
    targetLabelRules: targetLabelOverrides,
    targetType: previewTargetType,
    targetLabels: splitLabelTokens(targetLabels),
    probeKind,
    storedProbeFrequency: storedFrequency,
  })

  return (
    <div className="override-rules">
      <div>
        <div className="ss-title">覆盖规则</div>
        <p className="ss-desc">
          按数组顺序匹配。主机采样只看监控实例标签里实际写了主机频率的规则；探测频率先用探测项已存频率，再看目标类型，最后看目标标签里写了该探测类型的规则。异常判定，以及当前范围用不到的字段，可以编辑并保存，但当前运行链路未应用。
        </p>
      </div>
      {compiled.errors.length > 0 ? (
        <div className="override-rules__errors" role="alert">
          {compiled.errors.map((error) => (
            <p key={`${error.ruleKey}:${error.path}:${error.message}`}>{error.message}</p>
          ))}
        </div>
      ) : null}
      <ScopeEditor
        scope="monitoring_instance_labels"
        rules={monitoringInstanceLabelOverrides}
        errors={compiled.errors}
        globalHostFrequency={globalHostFrequency}
        globalProbeDefaults={globalProbeDefaults}
        suggestions={monitoringSuggestions.labels}
        onSuggest={monitoringSuggestions.load}
        onChange={(rules) => onChange({ monitoringInstanceLabelOverrides: rules })}
      />
      <ScopeEditor
        scope="target_types"
        rules={targetTypeOverrides}
        errors={compiled.errors}
        globalHostFrequency={globalHostFrequency}
        globalProbeDefaults={globalProbeDefaults}
        onChange={(rules) => onChange({ targetTypeOverrides: rules })}
      />
      <ScopeEditor
        scope="target_labels"
        rules={targetLabelOverrides}
        errors={compiled.errors}
        globalHostFrequency={globalHostFrequency}
        globalProbeDefaults={globalProbeDefaults}
        suggestions={targetSuggestions.labels}
        onSuggest={targetSuggestions.load}
        onChange={(rules) => onChange({ targetLabelOverrides: rules })}
      />
      <section className="override-rules__preview" aria-label="覆盖预览">
        <h3 className="override-rules__title">预览</h3>
        <p className="override-rules__hint">用精确标签试算当前草稿。修改预览条件不会保存，也不会把全局频率写进已有探测项。</p>
        <div className="override-rules__preview-grid">
          <label className="override-rules__field">
            <span className="override-rules__field-label">监控实例标签</span>
            <input
              className="input input--compact"
              aria-label="预览监控实例标签"
              value={monitoringLabels}
              onKeyDown={blockEnter}
              onChange={(event) => setMonitoringLabels(event.target.value)}
            />
          </label>
          <label className="override-rules__field">
            <span className="override-rules__field-label">目标类型</span>
            <select
              className="input input--compact"
              aria-label="预览目标类型"
              value={previewTargetType}
              onChange={(event) => setPreviewTargetType(event.target.value)}
            >
              {TARGET_TYPE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          </label>
          <label className="override-rules__field">
            <span className="override-rules__field-label">目标标签</span>
            <input
              className="input input--compact"
              aria-label="预览目标标签"
              value={targetLabels}
              onKeyDown={blockEnter}
              onChange={(event) => setTargetLabels(event.target.value)}
            />
          </label>
          <label className="override-rules__field">
            <span className="override-rules__field-label">探测类型</span>
            <select
              className="input input--compact"
              aria-label="预览探测类型"
              value={probeKind}
              onChange={(event) => setProbeKind(event.target.value as ProbeKind)}
            >
              {PROBE_KIND_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          </label>
          <label className="override-rules__field">
            <span className="override-rules__field-label">探测项已存频率</span>
            <select
              className="input input--compact"
              aria-label="预览探测项已存频率"
              aria-describedby="override-preview-base-help"
              value={storedFrequency}
              onChange={(event) => setStoredOverride(event.target.value)}
            >
              {FREQUENCY_TIER_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
              {isKnownFrequency(storedFrequency) ? null : <option value={storedFrequency}>{storedFrequency}</option>}
            </select>
          </label>
        </div>
        <p id="override-preview-base-help" className="override-rules__preview-note">
          未手改时，这里按所选探测类型填入当前全局默认，只代表假设中的新探测项。已有探测项请改成它保存的频率；保存过的探测项不会自动改用全局默认。
        </p>
        <PreviewFacts title="主机采样预览" preview={hostPreview} />
        <PreviewFacts title="探测频率预览" preview={probePreview} />
        <p className="override-rules__unapplied">异常判定覆盖当前运行链路未应用，这里不把它显示成已经生效的策略。</p>
      </section>
      <div className="override-rules__json-wrap">
        <button
          type="button"
          className="override-rules__disclosure"
          aria-expanded={jsonOpen}
          aria-controls="override-rules-json"
          onClick={() => setJsonOpen((open) => !open)}
        >
          <span>高级 JSON（只读）</span>
          <span className="override-rules__disclosure-mark">{jsonOpen ? '收起' : '展开'}</span>
        </button>
        <pre
          id="override-rules-json"
          className="override-rules__json"
          aria-label="覆盖规则 JSON"
          aria-readonly="true"
          hidden={!jsonOpen}
        >
          {overrideRulesJson(compiled.rules)}
        </pre>
      </div>
    </div>
  )
}

function PreviewFacts({ title, preview }: { title: string; preview: FrequencyPreview }) {
  return (
    <section aria-label={title}>
      <h4 className="override-rules__title">{title}</h4>
      <dl className="override-rules__facts">
        <dt>值</dt>
        <dd>{frequencyTierLabel(preview.value)}</dd>
        <dt>来源</dt>
        <dd>{sourceText(preview.source, preview.ruleNumber)}</dd>
        <dt>规则号</dt>
        <dd>{preview.ruleNumber ?? '无'}</dd>
        <dt>回退</dt>
        <dd>{fallbackText(preview)}</dd>
      </dl>
    </section>
  )
}

function ScopeEditor({
  scope,
  rules,
  errors,
  globalHostFrequency,
  globalProbeDefaults,
  suggestions,
  onSuggest,
  onChange,
}: {
  scope: OverrideScope
  rules: OverrideRuleDraft[]
  errors: OverrideDraftError[]
  globalHostFrequency: string
  globalProbeDefaults: ProbeFrequencyDefaults
  suggestions?: string[]
  onSuggest?: () => void
  onChange: (rules: OverrideRuleDraft[]) => void
}) {
  const heading = scopeHeading(scope)
  return (
    <section className="override-rules__scope" aria-label={`${heading}覆盖`}>
      <div className="override-rules__scope-head">
        <div className="override-rules__scope-copy">
          <h3 className="override-rules__title">{heading}</h3>
          <p className="override-rules__hint">靠前的规则先参与匹配。上移、下移会连同这条规则的原始值和已改字段一起移动。</p>
        </div>
        <button type="button" className="btn sm secondary" onClick={() => onChange([...rules, createOverrideRule(scope)])}>
          {`新增${heading}规则`}
        </button>
      </div>
      {rules.length === 0 ? <p className="override-rules__hint">{`还没有${heading}规则。`}</p> : null}
      {rules.map((rule, index) => (
        <RuleEditor
          key={rule.key}
          rule={rule}
          index={index}
          count={rules.length}
          errors={errors}
          globalHostFrequency={globalHostFrequency}
          globalProbeDefaults={globalProbeDefaults}
          suggestions={suggestions ?? []}
          onSuggest={onSuggest}
          onChange={(next) => onChange(rules.map((item, itemIndex) => (itemIndex === index ? next : item)))}
          onMove={(direction) => onChange(moveOverrideRule(rules, index, direction))}
          onDelete={() => onChange(removeOverrideRule(rules, index))}
        />
      ))}
    </section>
  )
}

function RuleEditor({
  rule,
  index,
  count,
  errors,
  globalHostFrequency,
  globalProbeDefaults,
  suggestions,
  onSuggest,
  onChange,
  onMove,
  onDelete,
}: {
  rule: OverrideRuleDraft
  index: number
  count: number
  errors: OverrideDraftError[]
  globalHostFrequency: string
  globalProbeDefaults: ProbeFrequencyDefaults
  suggestions: string[]
  onSuggest?: (() => void) | undefined
  onChange: (rule: OverrideRuleDraft) => void
  onMove: (direction: -1 | 1) => void
  onDelete: () => void
}) {
  const heading = ruleHeading(rule.scope, index)
  const selectorError = errors.find((error) => error.ruleKey === rule.key && error.path === 'selector')
  const ruleError = errors.find((error) => error.ruleKey === rule.key && (error.path === 'overrides' || error.path === 'incident_defaults'))
  return (
    <article className="override-rules__rule" aria-label={heading}>
      <div className="override-rules__rule-head">
        <div className="override-rules__selector">
          {rule.scope === 'target_types' ? (
            <label>
              <span className="override-rules__selector-label">目标类型</span>
              <select
                className="input input--compact"
                aria-label={`${heading} 类型`}
                aria-invalid={selectorError ? true : undefined}
                value={rule.selector}
                onChange={(event) => onChange(setRuleSelector(rule, event.target.value))}
              >
                {TARGET_TYPE_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
                {TARGET_TYPE_OPTIONS.some((option) => option.value === rule.selector) || rule.selector === '' ? null : (
                  <option value={rule.selector}>{targetTypeLabel(rule.selector)}</option>
                )}
              </select>
            </label>
          ) : (
            <label>
              <span className="override-rules__selector-label">{rule.scope === 'monitoring_instance_labels' ? '监控实例标签' : '目标标签'}</span>
              <input
                className="input input--compact"
                aria-label={`${heading} 标签`}
                aria-invalid={selectorError ? true : undefined}
                autoComplete="off"
                value={rule.selector}
                onFocus={onSuggest}
                onKeyDown={blockEnter}
                onChange={(event) => onChange(setRuleSelector(rule, event.target.value))}
              />
            </label>
          )}
          {selectorError ? <p className="override-rules__error">{selectorError.message}</p> : null}
          {rule.scope === 'target_types' ? null : (
            <SuggestionList
              label={rule.scope === 'monitoring_instance_labels' ? '已有监控实例标签' : '已有目标标签'}
              suggestions={suggestions}
              onPick={(label) => onChange(setRuleSelector(rule, label))}
            />
          )}
        </div>
        <div className="override-rules__actions">
          <button type="button" className="btn sm ghost" aria-label={`上移${heading}`} disabled={index === 0} onClick={() => onMove(-1)}>上移</button>
          <button type="button" className="btn sm ghost" aria-label={`下移${heading}`} disabled={index === count - 1} onClick={() => onMove(1)}>下移</button>
          <button type="button" className="btn sm ghost" aria-label={`删除${heading}`} onClick={onDelete}>删除</button>
        </div>
      </div>
      {ruleError ? <p className="override-rules__error">{ruleError.message}</p> : null}
      <Disclosure id={`${rule.key}-host`} visible="主机采样" label={`${heading} 主机采样`} unapplied={!overrideGroupApplied(rule.scope, 'host')}>
        <FrequencyControl
          legend={`${heading} 主机采样`}
          name={`${rule.key}-host`}
          leaf={rule.leaves.hostSampleFrequencyTier}
          onMode={(mode) => onChange(setHostMode(rule, mode, globalHostFrequency))}
          onValue={(value) => onChange(setHostValue(rule, value))}
        />
      </Disclosure>
      <Disclosure id={`${rule.key}-probe`} visible="探测频率" label={`${heading} 探测频率`} unapplied={!overrideGroupApplied(rule.scope, 'probe')}>
        <div className="override-rules__fields override-rules__fields--3">
          {PROBE_KIND_OPTIONS.map((kind) => (
            <FrequencyControl
              key={kind.value}
              visibleLabel={kind.label}
              legend={`${heading} ${kind.label}`}
              name={`${rule.key}-probe-${kind.value}`}
              leaf={rule.leaves.probe[kind.value]}
              onMode={(mode) => onChange(setProbeMode(rule, kind.value, mode, globalProbeDefaults[kind.value]))}
              onValue={(value) => onChange(setProbeValue(rule, kind.value, value))}
            />
          ))}
        </div>
      </Disclosure>
      <Disclosure id={`${rule.key}-incident`} visible="异常判定" label={`${heading} 异常判定`} unapplied>
        <p className="override-rules__unapplied">这些字段会保存，但当前运行链路未应用。</p>
        <div className="override-rules__fields override-rules__fields--3">
          {NOTIFY_FIELDS.map((field) => (
            <div key={field.key} className="override-rules__field">
              <span className="override-rules__field-label">{`${field.label}通知`}</span>
              <ModeRadios
                name={`${rule.key}-${field.key}`}
                legend={`${heading} ${field.label}通知`}
                value={rule.leaves[field.leaf]}
                choices={NOTIFY_CHOICES}
                onChange={(mode) => onChange(setNotifyMode(rule, field.key, mode as TriBoolean))}
              />
            </div>
          ))}
        </div>
        {NUMBER_GROUPS.map((group) => (
          <div key={group.group}>
            <span className="override-rules__field-label">{group.title}</span>
            <div className={`override-rules__fields override-rules__fields--${group.columns}`}>
              {INCIDENT_NUMBER_FIELDS.filter((field) => field.group === group.group).map((field) => (
                <NumberControl
                  key={field.key}
                  field={field}
                  heading={heading}
                  name={`${rule.key}-${field.key}`}
                  leaf={rule.leaves.incidentNumbers[field.key]}
                  onMode={(mode) => onChange(setIncidentNumberMode(rule, field.key, mode))}
                  onText={(text) => onChange(setIncidentNumberText(rule, field.key, text))}
                />
              ))}
            </div>
          </div>
        ))}
      </Disclosure>
    </article>
  )
}

function FrequencyControl({
  legend,
  visibleLabel,
  name,
  leaf,
  onMode,
  onValue,
}: {
  legend: string
  visibleLabel?: string
  name: string
  leaf: FrequencyLeafDraft
  onMode: (mode: 'inherit' | 'override') => void
  onValue: (value: string) => void
}) {
  return (
    <div className="override-rules__field">
      {visibleLabel ? <span className="override-rules__field-label">{visibleLabel}</span> : null}
      <ModeRadios name={name} legend={legend} value={leaf.mode} choices={INHERIT_OVERRIDE} onChange={(mode) => onMode(mode as 'inherit' | 'override')} />
      {leaf.mode === 'override' ? (
        <select className="input input--compact" aria-label={`${legend}频率`} value={leaf.value} onChange={(event) => onValue(event.target.value)}>
          {FREQUENCY_TIER_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>{option.label}</option>
          ))}
          {isKnownFrequency(leaf.value) ? null : <option value={leaf.value}>{leaf.value}</option>}
        </select>
      ) : null}
    </div>
  )
}

function NumberControl({
  field,
  heading,
  name,
  leaf,
  onMode,
  onText,
}: {
  field: IncidentNumberField
  heading: string
  name: string
  leaf: NumberLeafDraft
  onMode: (mode: 'inherit' | 'override') => void
  onText: (text: string) => void
}) {
  const legend = `${heading} ${field.label}`
  return (
    <div className="override-rules__field">
      <span className="override-rules__field-label">{field.label}</span>
      <ModeRadios name={name} legend={legend} value={leaf.mode} choices={INHERIT_OVERRIDE} onChange={(mode) => onMode(mode as 'inherit' | 'override')} />
      {leaf.mode === 'override' ? (
        <input
          className="input input--compact"
          aria-label={legend}
          inputMode={field.kind === 'load' ? 'decimal' : 'numeric'}
          value={leaf.text}
          onKeyDown={blockEnter}
          onChange={(event) => onText(event.target.value)}
        />
      ) : null}
      {field.unit ? <span className="override-rules__hint">{field.unit}</span> : null}
    </div>
  )
}

function ModeRadios({
  name,
  legend,
  value,
  choices,
  onChange,
}: {
  name: string
  legend: string
  value: string
  choices: readonly { value: string; label: string }[]
  onChange: (value: string) => void
}) {
  return (
    <div className="override-rules__modes" role="radiogroup" aria-label={legend}>
      {choices.map((choice) => (
        <label key={choice.value} className="override-rules__mode">
          <input
            type="radio"
            name={name}
            value={choice.value}
            checked={value === choice.value}
            aria-label={`${legend} ${choice.label}`}
            onChange={() => onChange(choice.value)}
          />
          {choice.label}
        </label>
      ))}
    </div>
  )
}

function Disclosure({
  id,
  visible,
  label,
  unapplied,
  children,
}: {
  id: string
  visible: string
  label: string
  unapplied: boolean
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const panelId = `${id}-panel`
  return (
    <div className="override-rules__group">
      <button
        type="button"
        className="override-rules__disclosure"
        aria-expanded={open}
        aria-controls={panelId}
        aria-label={unapplied ? `${label}，当前运行链路未应用` : label}
        onClick={() => setOpen((current) => !current)}
      >
        <span>{visible}</span>
        {unapplied ? <span className="override-rules__unapplied">当前运行链路未应用</span> : null}
        <span className="override-rules__disclosure-mark">{open ? '收起' : '展开'}</span>
      </button>
      <div id={panelId} className="override-rules__panel" role="group" aria-label={label} hidden={!open}>
        {open ? children : null}
      </div>
    </div>
  )
}

function SuggestionList({ label, suggestions, onPick }: { label: string; suggestions: string[]; onPick: (label: string) => void }) {
  if (suggestions.length === 0) return null
  return (
    <div className="override-rules__suggestions" role="group" aria-label={label}>
      {suggestions.map((item) => (
        <button key={item} type="button" className="btn sm secondary" onClick={() => onPick(item)}>
          {item}
        </button>
      ))}
    </div>
  )
}

function useLazyLabels(loader: () => Promise<string[]>) {
  const [labels, setLabels] = useState<string[]>([])
  const started = useRef(false)
  const load = useCallback(() => {
    if (started.current) return
    started.current = true
    void loader().then(setLabels).catch(() => setLabels([]))
  }, [loader])
  return { labels, load }
}

function isKnownFrequency(value: string): boolean {
  return FREQUENCY_TIER_OPTIONS.some((option) => option.value === value)
}
