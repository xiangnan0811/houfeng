import { Timestamp } from '../../../components/atoms'
import type { EvidenceFieldDecision, EvidenceSnapshotRead } from '../../../lib/types'
import { formatDuration } from './evidencePresentation'

const REDACTION_ACTION_LABELS: Record<EvidenceFieldDecision['action'], string> = {
  included: '保留',
  stripped: '已剥离',
  masked: '已遮罩',
  forbidden: '禁止',
}

const SENSITIVITY_LABELS: Record<EvidenceSnapshotRead['sensitivity'], string> = {
  normal: '普通',
  sensitive_topology: '敏感拓扑',
  forbidden: '禁止',
}

type Props = {
  snapshot: EvidenceSnapshotRead
}

function WindowText({ start, end }: { start: string; end: string }) {
  return <><Timestamp value={start} /> → <Timestamp value={end} /></>
}

/** 快照的来源、版本与字段处理明细：诊断用途，默认折叠。 */
export function EvidenceTechnicalDetails({ snapshot }: Props) {
  const redacted = snapshot.redaction.filter((item) => item.action !== 'included')
  return (
    <section className="record-section record-evidence__tech" aria-label="技术细节">
      <details className="record-disclosure">
        <summary>
          技术细节
          {redacted.length > 0 ? <span>· {redacted.length} 个字段已按策略处理</span> : null}
        </summary>
        <dl className="record-evidence__tech-list">
          <div><dt>快照</dt><dd className="mono">{snapshot.snapshot_id}</dd></div>
          <div><dt>类型</dt><dd className="mono">{snapshot.kind}/v{snapshot.schema_version} · {snapshot.renderer_version}</dd></div>
          <div><dt>观测时间</dt><dd><Timestamp value={snapshot.observed_at} /></dd></div>
          <div><dt>捕获时间</dt><dd><Timestamp value={snapshot.captured_at} /></dd></div>
          <div><dt>引用时间</dt><dd><Timestamp value={snapshot.referenced_at} /></dd></div>
          <div><dt>请求窗口</dt><dd><WindowText start={snapshot.requested_window.start} end={snapshot.requested_window.end} /></dd></div>
          <div><dt>实际窗口</dt><dd><WindowText start={snapshot.actual_window.start} end={snapshot.actual_window.end} /></dd></div>
          <div><dt>精度</dt><dd>{formatDuration(snapshot.actual_precision_seconds)}</dd></div>
          <div><dt>来源修订</dt><dd className="mono">{snapshot.source_revision || '—'} · {snapshot.source_watermark || '—'}</dd></div>
          <div><dt>生成版本</dt><dd className="mono">{snapshot.producer_version} · {snapshot.calculation_version}</dd></div>
          <div><dt>敏感级别</dt><dd>{SENSITIVITY_LABELS[snapshot.sensitivity]}</dd></div>
          <div>
            <dt>保留</dt>
            <dd>
              {snapshot.retention.immutable ? '不可变快照' : '可变'}
              {snapshot.retention.source_deletion === 'snapshot_retained_source_unavailable' ? ' · 来源删除后仍保留' : ''}
            </dd>
          </div>
        </dl>
        {redacted.length > 0 ? (
          <ul className="record-evidence__redaction" aria-label="字段处理">
            {redacted.map((item) => (
              <li key={`${item.path}-${item.action}`}>
                <code>{item.path}</code>
                <span>{REDACTION_ACTION_LABELS[item.action]}</span>
              </li>
            ))}
          </ul>
        ) : null}
      </details>
    </section>
  )
}
