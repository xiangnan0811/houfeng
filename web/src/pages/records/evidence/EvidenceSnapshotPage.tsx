import { useEffect, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'

import { Button, Timestamp } from '../../../components/atoms'
import { PageState } from '../../../components/PageState'
import { ApiError } from '../../../lib/apiRequest'
import { getEvidenceSnapshot } from '../../../lib/recordsApi'
import type { EvidenceFieldDecision, EvidenceQuality, EvidenceSnapshotRead } from '../../../lib/types'
import { comparisonEntryHref } from '../compare/comparisonQueryState'
import { decideRegisteredEvidenceRender } from './EvidenceRendererRegistry'

const QUALITY_LABELS: Record<EvidenceQuality['status'], string> = {
  complete: '完整',
  partial: '部分',
  degraded: '降级',
  unknown: '未知',
}

const REDACTION_ACTION_LABELS: Record<EvidenceFieldDecision['action'], string> = {
  included: '保留',
  stripped: '已剥离',
  masked: '已遮罩',
  forbidden: '禁止',
}

type LoadState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | { status: 'error'; message: string; code: string | null }
  | { status: 'ready'; snapshot: EvidenceSnapshotRead }

type SettledLoad = Exclude<LoadState, { status: 'loading' }>

function describeLoadFailure(error: unknown): SettledLoad {
  if (error instanceof ApiError) {
    const code = typeof error.code === 'string' ? error.code : null
    if (error.status === 404 || error.status === 403 || code === 'resource_not_found') {
      return { status: 'not-found' }
    }
    return {
      status: 'error',
      message: error.message || '无法加载证据快照。',
      code,
    }
  }
  return { status: 'error', message: '无法加载证据快照。', code: null }
}

function subjectEvidenceHref(type: string, id: string): string | null {
  const trimmed = id.trim()
  if (!trimmed) return null
  if (type === 'vps') return `/vps/${encodeURIComponent(trimmed)}/evidence`
  if (type === 'monitoring_instance') return `/monitoring/${encodeURIComponent(trimmed)}/evidence`
  if (type === 'target') return `/targets/${encodeURIComponent(trimmed)}/evidence`
  return null
}

function identityLabel(identity: EvidenceSnapshotRead['subject']): string {
  return identity.display_name?.trim() || identity.id
}

export function EvidenceSnapshotPage() {
  const { evidenceId } = useParams()
  const location = useLocation()
  const snapshotId = evidenceId?.trim() ?? ''
  const [retryToken, setRetryToken] = useState(0)
  const requestKey = `${snapshotId}:${retryToken}`
  const [result, setResult] = useState<{ key: string; load: SettledLoad } | null>(null)

  useEffect(() => {
    if (!snapshotId) return
    const controller = new AbortController()
    let active = true
    getEvidenceSnapshot(snapshotId, controller.signal)
      .then((snapshot) => {
        if (!active) return
        setResult({ key: requestKey, load: { status: 'ready', snapshot } })
      })
      .catch((error: unknown) => {
        if (!active) return
        if (error instanceof DOMException && error.name === 'AbortError') return
        setResult({ key: requestKey, load: describeLoadFailure(error) })
      })
    return () => {
      active = false
      controller.abort()
    }
  }, [snapshotId, requestKey])

  const load: LoadState = !snapshotId
    ? { status: 'not-found' }
    : result?.key === requestKey
      ? result.load
      : { status: 'loading' }

  if (load.status === 'loading') {
    return (
      <div className="page evidence-snapshot-page">
        <PageState kind="loading" title="正在加载证据快照" />
      </div>
    )
  }

  if (load.status === 'not-found') {
    return (
      <div className="page evidence-snapshot-page">
        <PageState
          kind="empty"
          eyebrow="证据快照"
          title="未找到证据"
          description="快照不存在，或当前账号无权查看。"
        />
      </div>
    )
  }

  if (load.status === 'error') {
    return (
      <div className="page evidence-snapshot-page">
        <PageState
          kind="error"
          eyebrow="证据快照"
          title="无法加载证据"
          description={load.message}
          technicalSummary={load.code}
          action={(
            <Button type="button" size="sm" onClick={() => setRetryToken((value) => value + 1)}>
              重试
            </Button>
          )}
        />
      </div>
    )
  }

  const snapshot = load.snapshot
  const renderDecision = decideRegisteredEvidenceRender(snapshot)
  const sourceUnavailable = !snapshot.source_available
  const backfilled = snapshot.quality.backfilled_count > 0
  const recordHref = snapshot.record_id.trim()
    ? `/records/${encodeURIComponent(snapshot.record_id)}`
    : null
  const subjectHref = subjectEvidenceHref(snapshot.subject.type, snapshot.subject.id)
  const redacted = snapshot.redaction.filter((item) => item.action !== 'included')

  return (
    <div className="page evidence-snapshot-page">
      <header className="page__head">
        <div>
          <p className="page-sub evidence-snapshot-page__kind">不可变证据</p>
          <h1 className="page__title">{snapshot.title.trim() || '证据快照'}</h1>
          <p className="page-sub evidence-snapshot-page__meta">
            <span className="mono">{snapshot.snapshot_id}</span>
            {' · '}
            <span>{snapshot.kind}</span>
          </p>
        </div>
        <div className="page__actions">
          {recordHref ? (
            <Link className="btn sm secondary" to={recordHref} state={location.state}>打开记录</Link>
          ) : null}
          {subjectHref ? (
            <Link className="btn sm secondary" to={subjectHref} state={location.state}>返回主体证据</Link>
          ) : null}
          <Link
            className="btn sm secondary"
            to={comparisonEntryHref({ items: [{ snapshot_id: snapshot.snapshot_id }] })}
            state={location.state}
          >
            横向比较
          </Link>
        </div>
      </header>

      {sourceUnavailable ? (
        <p className="evidence-snapshot-page__notice" role="status">
          来源已不可用。以下为快照保留内容，不是实时数据。
        </p>
      ) : null}
      {backfilled ? (
        <p className="evidence-snapshot-page__notice" role="status">
          质量统计含回填样本，不能当作实时观测。
        </p>
      ) : null}

      <dl className="metadata-list evidence-snapshot-page__facts">
        <div>
          <dt>主体</dt>
          <dd>{snapshot.subject.type} · {identityLabel(snapshot.subject)}</dd>
        </div>
        <div>
          <dt>来源</dt>
          <dd>{snapshot.source.type} · {identityLabel(snapshot.source)}</dd>
        </div>
        <div>
          <dt>观测时间</dt>
          <dd><Timestamp value={snapshot.observed_at} mode="absolute" /></dd>
        </div>
        <div>
          <dt>捕获时间</dt>
          <dd><Timestamp value={snapshot.captured_at} mode="absolute" /></dd>
        </div>
        <div>
          <dt>引用时间</dt>
          <dd><Timestamp value={snapshot.referenced_at} mode="absolute" /></dd>
        </div>
        <div>
          <dt>质量</dt>
          <dd>
            {QUALITY_LABELS[snapshot.quality.status]}
            {snapshot.quality.partial ? ' · 部分覆盖' : ''}
            {snapshot.quality.truncated ? ' · 已截断' : ''}
          </dd>
        </div>
        <div>
          <dt>保留</dt>
          <dd>
            {snapshot.retention.immutable ? '不可变快照' : '可变'}
            {snapshot.retention.source_deletion === 'snapshot_retained_source_unavailable'
              ? ' · 来源删除后仍保留快照'
              : ''}
          </dd>
        </div>
        <div>
          <dt>来源状态</dt>
          <dd>{sourceUnavailable ? '来源不可用' : '来源仍可读'}</dd>
        </div>
      </dl>

      {redacted.length > 0 ? (
        <p className="evidence-snapshot-page__redaction">
          已按策略处理 {redacted.length} 个字段（
          {redacted.map((item) => `${item.path} ${REDACTION_ACTION_LABELS[item.action]}`).join('、')}
          ），不展示原始内容。
        </p>
      ) : null}
      {renderDecision.status === 'rendered' ? (
        <div className="evidence-snapshot-page__body">
          {renderDecision.node}
        </div>
      ) : (
        <PageState
          kind="empty"
          eyebrow="证据快照"
          title="不支持的证据类型"
          description="当前客户端没有匹配的读模型，已关闭展示，避免输出原始载荷。"
        />
      )}
    </div>
  )
}
