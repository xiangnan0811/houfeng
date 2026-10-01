import { useEffect, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'

import { Badge, Button, Timestamp } from '../../../components/atoms'
import { PageState } from '../../../components/PageState'
import { ApiError } from '../../../lib/apiRequest'
import { getEvidenceSnapshot } from '../../../lib/recordsApi'
import type { EvidenceSnapshotRead } from '../../../lib/types'
import { comparisonEntryHref } from '../compare/comparisonQueryState'
import { decideRegisteredEvidenceRender } from './EvidenceRendererRegistry'
import { EvidenceTechnicalDetails } from './EvidenceTechnicalDetails'
import { evidenceKindLabel, identityTypeLabel, QUALITY_BADGE_LABELS, qualityTone } from './evidencePresentation'
import '../RecordWorkspace.css'

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
      <div className="page record-page record-evidence">
        <PageState kind="loading" title="正在加载证据快照" />
      </div>
    )
  }

  if (load.status === 'not-found') {
    return (
      <div className="page record-page record-evidence">
        <PageState
          kind="empty"
          title="未找到证据"
          description="快照不存在，或当前账号无权查看。"
        />
      </div>
    )
  }

  if (load.status === 'error') {
    return (
      <div className="page record-page record-evidence">
        <PageState
          kind="error"
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

  return (
    <div className="page record-page record-evidence">
      <header className="page__head record-identity">
        <div className="record-identity__lead">
          <EvidenceMark />
          <div className="record-identity__copy">
            <div className="record-identity__title-row">
              <h1 className="page__title">{snapshot.title.trim() || '证据快照'}</h1>
              <div className="record-identity__badges">
                <Badge variant="info">{evidenceKindLabel(snapshot.kind)}</Badge>
                <Badge variant="info" tone={qualityTone(snapshot.quality.status)}>
                  {QUALITY_BADGE_LABELS[snapshot.quality.status]}
                  {snapshot.quality.truncated ? ' · 已截断' : ''}
                </Badge>
                {sourceUnavailable ? <Badge variant="info" tone="notice">来源已不可用</Badge> : null}
                {backfilled ? <Badge variant="info" tone="notice">含回填样本</Badge> : null}
              </div>
            </div>
            <dl className="record-identity__meta record-identity__facts" aria-label="证据身份">
              <div className="record-identity__meta-item">
                <dt>主体</dt>
                <dd>{identityTypeLabel(snapshot.subject.type)} · {identityLabel(snapshot.subject)}</dd>
              </div>
              <div className="record-identity__meta-item">
                <dt>来源</dt>
                <dd>{identityTypeLabel(snapshot.source.type)} · {identityLabel(snapshot.source)}</dd>
              </div>
              <div className="record-identity__meta-item">
                <dt>观测</dt>
                <dd><Timestamp value={snapshot.observed_at} mode="both" /></dd>
              </div>
            </dl>
          </div>
        </div>
        <div className="page__actions">
          {recordHref ? (
            <Link className="btn md secondary" to={recordHref} state={location.state}>打开记录</Link>
          ) : null}
          {subjectHref ? (
            <Link className="btn md secondary" to={subjectHref} state={location.state}>返回主体证据</Link>
          ) : null}
          <Link
            className="btn md secondary"
            to={comparisonEntryHref({ items: [{ snapshot_id: snapshot.snapshot_id }] })}
            state={location.state}
          >
            横向比较
          </Link>
        </div>
      </header>

      {renderDecision.status === 'rendered' ? renderDecision.node : (
        <PageState
          kind="empty"
          title="不支持的证据类型"
          description="当前客户端无法安全展示这种证据，已隐藏原始内容。"
        />
      )}
      <EvidenceTechnicalDetails snapshot={snapshot} />
    </div>
  )
}

function EvidenceMark() {
  return (
    <span className="record-identity__mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" focusable="false">
        <path d="M12 3.5 19 6.5v5c0 4.3-2.9 7.7-7 9-4.1-1.3-7-4.7-7-9v-5z" />
        <path d="m9 12 2 2 4-4" />
      </svg>
    </span>
  )
}
