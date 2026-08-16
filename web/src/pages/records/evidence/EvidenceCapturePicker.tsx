import { useEffect, useRef, useState } from 'react'

import { Button, Input, Select } from '../../../components/atoms'
import type {
  EvidenceCapturePreview,
  EvidenceCapturePreviewInput,
  EvidenceCaptureReference,
  EvidenceKindName,
} from '../../../lib/types'

export type EvidenceCaptureSourceOption = {
  source_type: string
  source_id: string
  label: string
}

export type EvidenceCaptureMetricOption = {
  value: string
  label: string
}

export type EvidenceCapturePrecisionOption = {
  seconds: number
  label: string
}

export type EvidenceCaptureSensitiveFieldOption = {
  value: string
  label: string
}

export type EvidenceCaptureKindOption = {
  kind: EvidenceKindName
  schema_version: number
  label: string
  sources: readonly EvidenceCaptureSourceOption[]
  metrics: readonly EvidenceCaptureMetricOption[]
  precision_options: readonly EvidenceCapturePrecisionOption[]
  sensitive_topology_fields: readonly EvidenceCaptureSensitiveFieldOption[]
}

type Props = {
  recordId?: string
  options: readonly EvidenceCaptureKindOption[]
  requestPreview: (
    input: EvidenceCapturePreviewInput,
    signal: AbortSignal,
  ) => Promise<EvidenceCapturePreview>
  onConfirm: (reference: EvidenceCaptureReference) => void
  now?: () => Date
}

function kindOptionValue(option: EvidenceCaptureKindOption): string {
  return `${option.kind}/${option.schema_version}`
}

function sourceOptionValue(option: EvidenceCaptureSourceOption): string {
  return `${option.source_type}/${option.source_id}`
}

function utcISOString(value: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2})?$/.test(value)) return null
  const normalized = value.length === 16 ? `${value}:00.000Z` : `${value}.000Z`
  const date = new Date(normalized)
  return Number.isNaN(date.getTime()) ? null : date.toISOString()
}

function previewIsStale(preview: EvidenceCapturePreview, now: Date): boolean {
  const validUntil = new Date(preview.valid_until).getTime()
  return Number.isNaN(validUntil) || validUntil <= now.getTime()
}

function previewQuotaIsConfirmable(preview: EvidenceCapturePreview): boolean {
  if (!previewQuotaIsValid(preview)) return false
  return preview.quota.status === 'allowed' || preview.quota.status === 'warning'
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

function utcInstantMicros(value: string): number {
  const match = /^(.*?)(?:\.(\d{1,6}))?Z$/.exec(value)
  if (!match?.[1]) return Number.NaN
  const seconds = Date.parse(`${match[1]}Z`)
  const fraction = Number((match[2] ?? '').padEnd(6, '0') || '0')
  return seconds * 1_000 + fraction
}

function sameUTCInstant(left: string, right: string): boolean {
  const leftMicros = utcInstantMicros(left)
  const rightMicros = utcInstantMicros(right)
  return Number.isFinite(leftMicros) && leftMicros === rightMicros
}

const systemNow = () => new Date()

export function EvidenceCapturePicker({
  recordId,
  options,
  requestPreview,
  onConfirm,
  now = systemNow,
}: Props) {
  const [kindValue, setKindValue] = useState('')
  const [sourceValue, setSourceValue] = useState('')
  const [windowStart, setWindowStart] = useState('')
  const [windowEnd, setWindowEnd] = useState('')
  const [metrics, setMetrics] = useState<string[]>([])
  const [precision, setPrecision] = useState('')
  const [sensitiveFields, setSensitiveFields] = useState<string[]>([])
  const [preview, setPreview] = useState<EvidenceCapturePreview | null>(null)
  const [previewPending, setPreviewPending] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [, setExpirationTick] = useState(0)
  const previewRequestRef = useRef<AbortController | null>(null)

  const selectedKind = options.find((option) => kindOptionValue(option) === kindValue)
  const selectedSource = selectedKind?.sources.find((option) => sourceOptionValue(option) === sourceValue)
  const startISO = utcISOString(windowStart)
  const endISO = utcISOString(windowEnd)
  const validWindow = startISO !== null && endISO !== null && startISO < endISO
  const metricsComplete = selectedKind !== undefined && (selectedKind.metrics.length === 0 || metrics.length > 0)
  const precisionComplete = selectedKind !== undefined && selectedKind.precision_options.some(
    (option) => String(option.seconds) === precision,
  )
  const canRequestPreview = selectedSource !== undefined && validWindow && metricsComplete && precisionComplete
  const stalePreview = preview === null || previewIsStale(preview, now())
  const confirmablePreview = preview !== null && !stalePreview && previewQuotaIsConfirmable(preview)

  const invalidatePreview = () => {
    previewRequestRef.current?.abort()
    previewRequestRef.current = null
    setPreview(null)
    setPreviewPending(false)
    setPreviewError('')
  }

  useEffect(() => () => previewRequestRef.current?.abort(), [])

  useEffect(() => {
    if (!preview) return
    const remaining = new Date(preview.valid_until).getTime() - now().getTime()
    if (!Number.isFinite(remaining) || remaining <= 0) return
    const timer = globalThis.setTimeout(
      () => setExpirationTick((current) => current + 1),
      Math.min(remaining, 2_147_483_647),
    )
    return () => globalThis.clearTimeout(timer)
  }, [now, preview])

  const handleKindChange = (value: string) => {
    setKindValue(value)
    setSourceValue('')
    setWindowStart('')
    setWindowEnd('')
    setMetrics([])
    setPrecision('')
    setSensitiveFields([])
    invalidatePreview()
  }

  const handleSourceChange = (value: string) => {
    setSourceValue(value)
    setWindowStart('')
    setWindowEnd('')
    setMetrics([])
    setPrecision('')
    setSensitiveFields([])
    invalidatePreview()
  }

  const handleWindowChange = (part: 'start' | 'end', value: string) => {
    if (part === 'start') setWindowStart(value)
    else setWindowEnd(value)
    setMetrics([])
    setPrecision('')
    setSensitiveFields([])
    invalidatePreview()
  }

  const handleMetricChange = (value: string, checked: boolean) => {
    setMetrics((current) => checked
      ? [...current, value]
      : current.filter((metric) => metric !== value))
    setPrecision('')
    setSensitiveFields([])
    invalidatePreview()
  }

  const handlePrecisionChange = (value: string) => {
    setPrecision(value)
    setSensitiveFields([])
    invalidatePreview()
  }

  const handleSensitiveFieldChange = (value: string, checked: boolean) => {
    setSensitiveFields((current) => checked
      ? [...current, value]
      : current.filter((field) => field !== value))
    invalidatePreview()
  }

  const handlePreview = async () => {
    if (!selectedKind || !selectedSource || !startISO || !endISO || !canRequestPreview) return
    previewRequestRef.current?.abort()
    const controller = new AbortController()
    previewRequestRef.current = controller
    setPreview(null)
    setPreviewPending(true)
    setPreviewError('')
    const input: EvidenceCapturePreviewInput = {
      kind: selectedKind.kind,
      schema_version: selectedKind.schema_version,
      source_type: selectedSource.source_type,
      source_id: selectedSource.source_id,
      requested_window: { start: startISO, end: endISO },
      metrics: [...metrics],
      precision_seconds: Number(precision),
      sensitive_topology_fields: [...sensitiveFields],
    }
    if (recordId !== undefined) input.record_id = recordId
    try {
      const result = await requestPreview(input, controller.signal)
      if (controller.signal.aborted || previewRequestRef.current !== controller) return
      if (result.kind !== input.kind || result.schema_version !== input.schema_version ||
        (recordId !== undefined && result.record_id !== recordId) ||
        result.source.type !== input.source_type || result.source.id !== input.source_id ||
        !sameUTCInstant(result.requested_window.start, input.requested_window.start) ||
        !sameUTCInstant(result.requested_window.end, input.requested_window.end) ||
        result.record_id === '' || result.capture_intent_id === '' || !previewQuotaIsValid(result)) {
        setPreviewError('预览响应与当前选择不一致。')
        return
      }
      setPreview(result)
    } catch {
      if (!controller.signal.aborted) setPreviewError('无法生成证据预览。')
    } finally {
      if (previewRequestRef.current === controller) {
        previewRequestRef.current = null
        setPreviewPending(false)
      }
    }
  }

  const handleConfirm = () => {
    if (!preview || previewIsStale(preview, now()) || !previewQuotaIsConfirmable(preview)) return
    onConfirm({
      record_id: preview.record_id,
      capture_intent_id: preview.capture_intent_id,
    })
  }

  return (
    <section className="page-stack evidence-picker" aria-label="证据采集选择器">
      <fieldset className="page-panel evidence-picker__step">
        <legend>1. 证据类型</legend>
        <Select label="证据类型" value={kindValue} onChange={(event) => handleKindChange(event.target.value)}>
          <option value="">请选择证据类型</option>
          {options.map((option) => (
            <option key={kindOptionValue(option)} value={kindOptionValue(option)}>{option.label}</option>
          ))}
        </Select>
      </fieldset>

      <fieldset className="page-panel evidence-picker__step" disabled={!selectedKind}>
        <legend>2. 数据来源</legend>
        <Select label="数据来源" value={sourceValue} onChange={(event) => handleSourceChange(event.target.value)}>
          <option value="">请选择数据来源</option>
          {selectedKind?.sources.map((option) => (
            <option key={sourceOptionValue(option)} value={sourceOptionValue(option)}>{option.label}</option>
          ))}
        </Select>
      </fieldset>

      <fieldset className="page-panel evidence-picker__step" disabled={!selectedSource}>
        <legend>3. 绝对时间窗口</legend>
        <div className="evidence-picker__window">
          <Input
            label="开始时间（UTC）"
            type="datetime-local"
            value={windowStart}
            onChange={(event) => handleWindowChange('start', event.target.value)}
          />
          <Input
            label="结束时间（UTC）"
            type="datetime-local"
            value={windowEnd}
            onChange={(event) => handleWindowChange('end', event.target.value)}
          />
        </div>
      </fieldset>

      <fieldset className="page-panel evidence-picker__step" disabled={!validWindow}>
        <legend>4. 指标</legend>
        {selectedKind?.metrics.length === 0 ? <p>该证据类型无需选择指标。</p> : (
          <div className="evidence-picker__options">
            {selectedKind?.metrics.map((metric) => (
              <label key={metric.value} className="evidence-picker__option">
                <input
                  type="checkbox"
                  checked={metrics.includes(metric.value)}
                  onChange={(event) => handleMetricChange(metric.value, event.target.checked)}
                />
                <span>{metric.label}</span>
              </label>
            ))}
          </div>
        )}
      </fieldset>

      <fieldset className="page-panel evidence-picker__step" disabled={!validWindow || !metricsComplete}>
        <legend>5. 精度</legend>
        <Select label="采样精度" value={precision} onChange={(event) => handlePrecisionChange(event.target.value)}>
          <option value="">请选择采样精度</option>
          {selectedKind?.precision_options.map((option) => (
            <option key={option.seconds} value={option.seconds}>{option.label}</option>
          ))}
        </Select>
      </fieldset>

      <fieldset className="page-panel evidence-picker__step" disabled={!precisionComplete}>
        <legend>6. 敏感字段</legend>
        {selectedKind?.sensitive_topology_fields.length === 0 ? <p>无可选敏感字段。</p> : (
          <div className="evidence-picker__options">
            {selectedKind?.sensitive_topology_fields.map((field) => (
              <label key={field.value} className="evidence-picker__option">
                <input
                  type="checkbox"
                  checked={sensitiveFields.includes(field.value)}
                  onChange={(event) => handleSensitiveFieldChange(field.value, event.target.checked)}
                />
                <span>{field.label}</span>
              </label>
            ))}
          </div>
        )}
      </fieldset>

      <fieldset className="page-panel evidence-picker__step">
        <legend>7. 预览</legend>
        <Button onClick={() => void handlePreview()} disabled={!canRequestPreview || previewPending}>
          {previewPending ? '正在生成预览…' : '生成预览'}
        </Button>
        {previewError ? <p className="evidence-picker__error" role="alert">{previewError}</p> : null}
        {preview ? (
          <section className="page-panel evidence-picker__preview" aria-label="证据预览">
            <h3>{preview.kind}</h3>
            <dl className="metadata-list evidence-picker__preview-facts">
              <div><dt>来源</dt><dd>{preview.source.display_name || preview.source.id}</dd></div>
              <div><dt>实际窗口</dt><dd>{preview.actual_window.start} — {preview.actual_window.end}</dd></div>
              <div><dt>质量</dt><dd>{preview.quality.status}</dd></div>
              <div><dt>预计大小</dt><dd>{preview.estimated_canonical_bytes} bytes</dd></div>
              <div><dt>证据容量</dt><dd>{preview.quota.status}</dd></div>
            </dl>
            {preview.quota.reason ? <p>{preview.quota.reason}</p> : null}
            <p>预览有效至 {preview.valid_until}</p>
            {stalePreview ? <p className="evidence-picker__error">该预览已过期，请重新生成。</p> : null}
          </section>
        ) : null}
      </fieldset>

      <fieldset className="page-panel evidence-picker__step" aria-label="8. 确认">
        <legend>8. 确认</legend>
        <Button onClick={handleConfirm} disabled={!confirmablePreview}>确认引用</Button>
      </fieldset>
    </section>
  )
}
