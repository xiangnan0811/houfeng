import { useEffect, useMemo, useRef, useState } from 'react'

import { Badge, Button, Input, SegmentedControl, Select } from '../../../components/atoms'
import type { BadgeTone } from '../../../components/atoms'
import { formatBytes } from '../../../lib/format'
import type {
  EvidenceCapturePreview,
  EvidenceCapturePreviewInput,
  RecordSubjectKind,
} from '../../../lib/types'
import {
  EVIDENCE_CAPTURE_KINDS,
  precisionOptionsForWindow,
  formatEvidenceWindow,
  formatLocalTime,
  sortedSelection,
  type EvidenceCaptureKind,
} from './evidenceCaptureCatalog'
import { QUALITY_BADGE_LABELS, qualityTone } from './evidencePresentation'
import { useOtherEvidenceSource, useSubjectMonitoringInstances, type OtherEvidenceSourceLoaders } from './useOtherEvidenceSource'

/** 可作为证据来源的记录主体。 */
export type EvidenceCaptureSubject = Readonly<{
  kind: RecordSubjectKind
  source_id: string
  label: string
}>

/** 确认后交给工作区的待保存证据：发布时以 capture_intent_id 写入，正文以 snapshot_id 引用。 */
export type PendingEvidence = Readonly<{
  record_id: string
  capture_intent_id: string
  snapshot_id: string
  kind_label: string
  source_label: string
  window_label: string
  valid_until: string
}>

type Props = {
  /** 已有记录的 ID，或本次新建记录在首次预览时由服务端预分配的 ID。 */
  recordId?: string | undefined
  subjects: readonly EvidenceCaptureSubject[]
  /** 提供时可在记录主体之外再选其他 VPS（及其监控实例），用于跨主机对比。 */
  otherSources?: OtherEvidenceSourceLoaders | undefined
  requestPreview: (input: EvidenceCapturePreviewInput, signal: AbortSignal) => Promise<EvidenceCapturePreview>
  onConfirm: (evidence: PendingEvidence) => void
  /** 记录发布进行中：可以继续预览，但不能加入，免得新证据赶不上这次发布。 */
  disabled?: boolean
  now?: () => Date
}

type WindowPreset = '1h' | '6h' | '24h' | '7d' | 'custom'

const PRESET_SECONDS: Record<Exclude<WindowPreset, 'custom'>, number> = {
  '1h': 3600,
  '6h': 6 * 3600,
  '24h': 24 * 3600,
  '7d': 7 * 86_400,
}

const PRESET_ITEMS: readonly { value: WindowPreset; label: string }[] = [
  { value: '1h', label: '近 1 小时' },
  { value: '6h', label: '近 6 小时' },
  { value: '24h', label: '近 24 小时' },
  { value: '7d', label: '近 7 天' },
  { value: 'custom', label: '自定义' },
]

const QUOTA_LABELS: Record<EvidenceCapturePreview['quota']['status'], string> = {
  allowed: '容量充足',
  warning: '证据容量接近上限',
  exceeded: '证据容量已满，无法加入',
  unavailable: '证据容量暂不可知，无法加入',
}

const QUOTA_TONES: Record<EvidenceCapturePreview['quota']['status'], BadgeTone> = {
  allowed: 'normal',
  warning: 'notice',
  exceeded: 'critical',
  unavailable: 'offline',
}

const FIVE_MINUTES_MS = 5 * 60 * 1000

// 快捷窗口只取已结束的时段：截止到至少 5 分钟前、对齐 5 分钟。正在写入的数据会让发布时的重新采集与预览不一致。
function presetWindow(preset: Exclude<WindowPreset, 'custom'>, now: Date): { start: string; end: string } {
  const end = Math.floor((now.getTime() - FIVE_MINUTES_MS) / FIVE_MINUTES_MS) * FIVE_MINUTES_MS
  return { start: new Date(end - PRESET_SECONDS[preset] * 1000).toISOString(), end: new Date(end).toISOString() }
}

// datetime-local 的值按浏览器本地时区解释，再换成 UTC。
function localInputToISO(value: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date.toISOString()
}

// 订阅成本必须是一个完整的 UTC 自然月。
function monthWindow(value: string): { start: string; end: string } | null {
  const match = /^(\d{4})-(\d{2})$/.exec(value)
  if (!match) return null
  const year = Number(match[1])
  const month = Number(match[2]) - 1
  if (month < 0 || month > 11) return null
  return { start: new Date(Date.UTC(year, month, 1)).toISOString(), end: new Date(Date.UTC(year, month + 1, 1)).toISOString() }
}

function utcInstantMicros(value: string): number {
  const match = /^(.*?)(?:\.(\d{1,6}))?Z$/.exec(value)
  if (!match?.[1]) return Number.NaN
  const seconds = Date.parse(`${match[1]}Z`)
  const fraction = Number((match[2] ?? '').padEnd(6, '0') || '0')
  return seconds * 1_000 + fraction
}

function sameUTCInstant(left: string, right: string): boolean {
  const leftMicros = utcInstantMicros(left)
  return Number.isFinite(leftMicros) && leftMicros === utcInstantMicros(right)
}

function previewQuotaIsValid(preview: EvidenceCapturePreview): boolean {
  const quota = (preview as { quota?: unknown }).quota
  if (typeof quota !== 'object' || quota === null) return false
  const status = (quota as { status?: unknown }).status
  const reason = (quota as { reason?: unknown }).reason
  if (status === 'allowed') return reason === undefined
  if (status === 'warning') return reason === 'project evidence quota warning threshold reached'
  if (status === 'exceeded') return reason === 'project evidence quota exceeded'
  if (status === 'unavailable') return reason === 'project evidence capacity unavailable'
  return false
}

function previewIsStale(preview: EvidenceCapturePreview, now: Date): boolean {
  const validUntil = new Date(preview.valid_until).getTime()
  return Number.isNaN(validUntil) || validUntil <= now.getTime()
}

const systemNow = () => new Date()

const OTHER_SOURCE = 'other'
// “其他 VPS”能提供的来源类型：VPS 本身与它的监控实例；入口探测目标不挂在 VPS 下，只能取记录主体。
const OTHER_SOURCE_KINDS: readonly RecordSubjectKind[] = ['vps', 'monitoring_instance']

export function EvidenceCapturePicker({ recordId, subjects, otherSources, requestPreview, onConfirm, disabled = false, now = systemNow }: Props) {
  const kinds = useMemo(() => EVIDENCE_CAPTURE_KINDS.filter((kind) => kind.sourceKinds.some((sourceKind) =>
    subjects.some((subject) => subject.kind === sourceKind) || (otherSources !== undefined && OTHER_SOURCE_KINDS.includes(sourceKind)))),
  [otherSources, subjects])
  const [kindValue, setKindValue] = useState(kinds[0]?.kind ?? '')
  const kind: EvidenceCaptureKind | undefined = kinds.find((option) => option.kind === kindValue)
  const vpsSubjects = subjects.filter((subject) => subject.kind === 'vps')
  const subjectInstances = useSubjectMonitoringInstances(vpsSubjects.map((subject) => subject.source_id), otherSources)
  const needsSubjectInstances = Boolean(kind?.sourceKinds.includes('monitoring_instance'))
  // 记录主体在前，主体 VPS 名下的监控实例随后；已作为主体出现的实例不重复列出。
  const sources: EvidenceCaptureSubject[] = [
    ...subjects.filter((subject) => kind?.sourceKinds.includes(subject.kind)),
    ...(needsSubjectInstances ? subjectInstances.instances
      .filter((instance) => !subjects.some((subject) => subject.kind === 'monitoring_instance' && subject.source_id === instance.id))
      .map((instance): EvidenceCaptureSubject => ({
        kind: 'monitoring_instance',
        source_id: instance.id,
        label: `${vpsSubjects.find((subject) => subject.source_id === instance.vpsId)?.label ?? instance.vpsId} · ${instance.label}`,
      })) : []),
  ]
  const sourcesLoading = needsSubjectInstances && subjectInstances.loading
  const otherAllowed = otherSources !== undefined && Boolean(kind?.sourceKinds.some((sourceKind) => OTHER_SOURCE_KINDS.includes(sourceKind)))
  const [sourceValue, setSourceValue] = useState('')
  // 记录主体优先；主体 VPS 的实例读完仍没有匹配来源时，直接进入“其他 VPS”。
  const otherMode = otherAllowed && (sourceValue === OTHER_SOURCE || (sources.length === 0 && !sourcesLoading))
  const otherNeedsInstance = !kind?.sourceKinds.includes('vps')
  const other = useOtherEvidenceSource(otherMode, otherNeedsInstance, otherSources)
  const otherSource: EvidenceCaptureSubject | undefined = !otherMode || !other.selectedVPS
    ? undefined
    : otherNeedsInstance
      ? other.selectedInstance
        ? { kind: 'monitoring_instance', source_id: other.selectedInstance.id, label: `${other.selectedVPS.label} · ${other.selectedInstance.label}` }
        : undefined
      : { kind: 'vps', source_id: other.selectedVPS.id, label: other.selectedVPS.label }
  const source = otherMode
    ? otherSource
    : sources.find((subject) => `${subject.kind}/${subject.source_id}` === sourceValue) ?? sources[0]
  const [preset, setPreset] = useState<WindowPreset>('24h')
  const [customStart, setCustomStart] = useState('')
  const [customEnd, setCustomEnd] = useState('')
  const [month, setMonth] = useState('')
  const [metrics, setMetrics] = useState<string[]>(() => commonMetrics(kinds[0]))
  const [showAllMetrics, setShowAllMetrics] = useState(false)
  const [precision, setPrecision] = useState(0)
  const [sensitiveFields, setSensitiveFields] = useState<string[]>([])
  const [preview, setPreview] = useState<EvidenceCapturePreview | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [added, setAdded] = useState('')
  const [, setTick] = useState(0)
  const requestRef = useRef<AbortController | null>(null)

  const captureWindow = kind?.window === 'month'
    ? monthWindow(month)
    : preset === 'custom'
      ? (() => {
        const start = localInputToISO(customStart)
        const end = localInputToISO(customEnd)
        return start && end && start < end ? { start, end } : null
      })()
      : presetWindow(preset, now())
  const windowSeconds = captureWindow ? (Date.parse(captureWindow.end) - Date.parse(captureWindow.start)) / 1000 : 0
  const precisionOptions = kind?.adjustablePrecision ? precisionOptionsForWindow(windowSeconds) : []
  // 窗口变长后原先选的细精度可能不再允许：显示与提交都回到“自动”，不发出服务端会拒绝的值。
  const effectivePrecision = precisionOptions.some((option) => option.seconds === precision) ? precision : 0
  const metricsComplete = !kind || kind.metrics.length === 0 || metrics.length > 0
  const canPreview = Boolean(kind && source && captureWindow && metricsComplete) && !pending
  const stale = preview !== null && previewIsStale(preview, now())
  const confirmable = !disabled && preview !== null && !stale && previewQuotaIsValid(preview) &&
    (preview.quota.status === 'allowed' || preview.quota.status === 'warning')

  // 任何选择变化都作废现有预览：确认必须对应用户当前看到的选择。
  const invalidate = () => {
    requestRef.current?.abort()
    requestRef.current = null
    setPreview(null)
    setPending(false)
    setError('')
    setAdded('')
  }

  useEffect(() => () => requestRef.current?.abort(), [])

  useEffect(() => {
    if (!preview) return
    const remaining = new Date(preview.valid_until).getTime() - now().getTime()
    if (!Number.isFinite(remaining) || remaining <= 0) return
    const timer = globalThis.setTimeout(() => setTick((current) => current + 1), Math.min(remaining, 2_147_483_647))
    return () => globalThis.clearTimeout(timer)
  }, [now, preview])

  const changeKind = (value: string) => {
    setKindValue(value)
    setSourceValue('')
    setMetrics(commonMetrics(kinds.find((option) => option.kind === value)))
    setShowAllMetrics(false)
    setPrecision(0)
    setSensitiveFields([])
    invalidate()
  }

  const toggle = (list: string[], value: string, checked: boolean) =>
    checked ? [...list, value] : list.filter((entry) => entry !== value)

  const handlePreview = async () => {
    if (!kind || !source || !captureWindow || !canPreview) return
    requestRef.current?.abort()
    const controller = new AbortController()
    requestRef.current = controller
    setPreview(null)
    setPending(true)
    setError('')
    setAdded('')
    const input: EvidenceCapturePreviewInput = {
      kind: kind.kind,
      schema_version: kind.schema_version,
      source_type: source.kind,
      source_id: source.source_id,
      requested_window: captureWindow,
      metrics: kind.metrics.length > 0 ? sortedSelection(metrics) : [],
      precision_seconds: kind.adjustablePrecision ? effectivePrecision : 0,
      sensitive_topology_fields: sortedSelection(sensitiveFields),
    }
    if (recordId !== undefined) input.record_id = recordId
    try {
      const result = await requestPreview(input, controller.signal)
      if (controller.signal.aborted || requestRef.current !== controller) return
      if (result.kind !== input.kind || result.schema_version !== input.schema_version ||
        (recordId !== undefined && result.record_id !== recordId) ||
        result.source.type !== input.source_type || result.source.id !== input.source_id ||
        !sameUTCInstant(result.requested_window.start, input.requested_window.start) ||
        !sameUTCInstant(result.requested_window.end, input.requested_window.end) ||
        !result.record_id || !result.capture_intent_id || !result.snapshot_id || !previewQuotaIsValid(result)) {
        setError('预览结果与当前选择不一致，请重新生成')
        return
      }
      setPreview(result)
    } catch (reason: unknown) {
      if (!controller.signal.aborted) setError(previewErrorMessage(reason))
    } finally {
      if (requestRef.current === controller) {
        requestRef.current = null
        setPending(false)
      }
    }
  }

  const handleConfirm = () => {
    if (!preview || !kind || !source || !confirmable || previewIsStale(preview, now())) return
    onConfirm({
      record_id: preview.record_id,
      capture_intent_id: preview.capture_intent_id,
      snapshot_id: preview.snapshot_id,
      kind_label: kind.label,
      source_label: source.label,
      window_label: formatEvidenceWindow(preview.actual_window),
      valid_until: preview.valid_until,
    })
    // 同一预览只能加入一次：清掉它，避免重复点击把同一个采集意图加两遍。
    setPreview(null)
    setAdded(`已加入“${kind.label} · ${source.label}”，发布后保存`)
  }

  if (kinds.length === 0) {
    return <p className="record-muted">记录还没有可采集证据的主体。先在"属性"里添加 VPS、监控实例或探测目标。</p>
  }

  const shownMetrics = kind?.metrics.filter((metric) => showAllMetrics || metric.common || metrics.includes(metric.value)) ?? []

  return (
    <div className="evidence-capture">
      <div className="evidence-capture__row">
        <Select label="证据类型" value={kindValue} onChange={(event) => changeKind(event.target.value)}>
          {kinds.map((option) => <option key={option.kind} value={option.kind}>{option.label}</option>)}
        </Select>
        <Select
          label="来源"
          value={otherMode ? OTHER_SOURCE : source ? `${source.kind}/${source.source_id}` : ''}
          onChange={(event) => { setSourceValue(event.target.value); invalidate() }}
        >
          {sourcesLoading && sources.length === 0 ? <option value="">正在读取…</option> : null}
          {sources.map((subject) => (
            <option key={`${subject.kind}/${subject.source_id}`} value={`${subject.kind}/${subject.source_id}`}>{subject.label}</option>
          ))}
          {otherAllowed ? <option value={OTHER_SOURCE}>其他 VPS…</option> : null}
        </Select>
      </div>

      {otherMode ? (
        <div className="evidence-capture__row">
          <Select label="VPS" value={other.vpsId} onChange={(event) => { other.selectVPS(event.target.value); invalidate() }}>
            <option value="">{other.loading && other.vpsOptions.length === 0 ? '正在读取…' : '请选择 VPS'}</option>
            {other.vpsOptions.map((option) => <option key={option.id} value={option.id}>{option.label}</option>)}
          </Select>
          {otherNeedsInstance ? (
            <Select label="监控实例" value={other.instanceId} disabled={!other.vpsId} onChange={(event) => { other.selectInstance(event.target.value); invalidate() }}>
              <option value="">{other.vpsId && other.loading ? '正在读取…' : '请选择监控实例'}</option>
              {other.instanceOptions.map((option) => <option key={option.id} value={option.id}>{option.label}</option>)}
            </Select>
          ) : null}
          {other.failed ? <p className="record-material__stale" role="alert">列表读取失败，请稍后重试</p> : null}
        </div>
      ) : null}

      {kind?.window === 'month' ? (
        <Input label="账单月份（UTC）" type="month" value={month} onChange={(event) => { setMonth(event.target.value); invalidate() }} />
      ) : (
        <div className="evidence-capture__field">
          <SegmentedControl label="时间窗口" items={PRESET_ITEMS} value={preset} onChange={(value) => { setPreset(value); invalidate() }} />
          {preset === 'custom' ? (
            <div className="evidence-capture__row">
              <Input label="开始" type="datetime-local" value={customStart} onChange={(event) => { setCustomStart(event.target.value); invalidate() }} />
              <Input label="结束" type="datetime-local" value={customEnd} onChange={(event) => { setCustomEnd(event.target.value); invalidate() }} />
            </div>
          ) : captureWindow ? <p className="record-muted">{formatEvidenceWindow(captureWindow)}</p> : null}
        </div>
      )}

      {kind && kind.metrics.length > 0 ? (
        <fieldset className="evidence-capture__field">
          <legend>指标</legend>
          <div className="evidence-capture__chips">
            {shownMetrics.map((metric) => (
              <label key={metric.value} className="evidence-capture__chip">
                <input
                  type="checkbox"
                  checked={metrics.includes(metric.value)}
                  onChange={(event) => { setMetrics((current) => toggle(current, metric.value, event.target.checked)); invalidate() }}
                />
                <span>{metric.label}</span>
              </label>
            ))}
            {kind.metrics.some((metric) => !metric.common) && !showAllMetrics ? (
              <button type="button" className="text-link" onClick={() => setShowAllMetrics(true)}>更多指标</button>
            ) : null}
          </div>
        </fieldset>
      ) : null}

      {kind?.adjustablePrecision ? (
        <Select label="精度" value={String(effectivePrecision)} onChange={(event) => { setPrecision(Number(event.target.value)); invalidate() }}>
          {precisionOptions.map((option) => <option key={option.seconds} value={option.seconds}>{option.label}</option>)}
        </Select>
      ) : null}

      {kind && kind.sensitiveFields.length > 0 ? (
        <details className="evidence-capture__field">
          <summary>包含敏感拓扑字段（默认不含）</summary>
          <div className="evidence-capture__chips">
            {kind.sensitiveFields.map((field) => (
              <label key={field.value} className="evidence-capture__chip">
                <input
                  type="checkbox"
                  checked={sensitiveFields.includes(field.value)}
                  onChange={(event) => { setSensitiveFields((current) => toggle(current, field.value, event.target.checked)); invalidate() }}
                />
                <span>{field.label}</span>
              </label>
            ))}
          </div>
        </details>
      ) : null}

      <div className="evidence-capture__actions">
        <Button size="sm" variant="secondary" disabled={!canPreview} onClick={() => void handlePreview()}>
          {pending ? '正在生成预览…' : '生成预览'}
        </Button>
        {error ? <span className="record-material__stale" role="alert">{error}</span> : null}
        {added ? <span className="record-muted" role="status">{added}</span> : null}
      </div>

      {preview ? (
        <section className="evidence-capture__preview" aria-label="证据预览">
          <div className="evidence-capture__preview-head">
            <strong>{kind?.label} · {preview.source.display_name || source?.label}</strong>
            <Badge variant="state" tone={qualityTone(preview.quality.status)}>{QUALITY_BADGE_LABELS[preview.quality.status]}</Badge>
            <Badge variant="state" tone={QUOTA_TONES[preview.quota.status]}>{QUOTA_LABELS[preview.quota.status]}</Badge>
          </div>
          <p className="record-muted">
            实际窗口 {formatEvidenceWindow(preview.actual_window)} · 约 {formatBytes(preview.estimated_canonical_bytes)}
          </p>
          <p className={stale ? 'record-material__stale' : 'record-muted'}>
            {stale ? '预览已过期，请重新生成' : `预览在 ${formatLocalTime(preview.valid_until)} 前发布有效，过期需重新采集`}
          </p>
          <Button size="sm" disabled={!confirmable} onClick={handleConfirm}>加入记录</Button>
          {disabled ? <p className="record-muted">正在发布，完成后再加入证据</p> : null}
        </section>
      ) : null}
    </div>
  )
}

function commonMetrics(kind: EvidenceCaptureKind | undefined): string[] {
  return kind?.metrics.filter((metric) => metric.common).map((metric) => metric.value) ?? []
}

function previewErrorMessage(reason: unknown): string {
  const code = typeof reason === 'object' && reason !== null ? (reason as { code?: unknown }).code : undefined
  if (code === 'resource_not_found') return '来源不可访问或已删除'
  if (code === 'evidence_source_empty') return '所选窗口内没有数据，请调整时间窗口'
  if (code === 'evidence_window_too_large') return '时间窗口超出单份证据上限，请缩短窗口或调粗精度'
  if (code === 'evidence_invalid') return '当前选择无效，请检查后重试'
  if (code === 'evidence_kind_unavailable') return '该证据类型暂不可用'
  return '无法生成证据预览，请稍后重试'
}
