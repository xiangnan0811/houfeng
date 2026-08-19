import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { Badge, Button, DataTable, Timestamp, type DataTableColumn } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import { ApiError } from '../../lib/apiRequest'
import { discardRecordDraft, listRecordDrafts } from '../../lib/recordsApi'
import type { RecordDraft } from '../../lib/types'
import { RECORD_TYPE_LABELS } from './recordLabels'

function describeDraftError(error: unknown): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return '加载记录草稿失败'
}

function dedupeDrafts(current: RecordDraft[], incoming: RecordDraft[]): RecordDraft[] {
  const seen = new Set(current.map((item) => item.draft_id))
  const result = [...current]
  for (const item of incoming) {
    if (seen.has(item.draft_id)) continue
    seen.add(item.draft_id)
    result.push(item)
  }
  return result
}

/**
 * A draft either becomes a new record or a new revision of an existing one, and
 * that destination decides where the editor opens.
 */
function draftEditorPath(item: RecordDraft): string {
  return item.record_id ? `/records/${item.record_id}/edit` : '/records/new'
}

export function RecordDraftsPage() {
  const [drafts, setDrafts] = useState<RecordDraft[]>([])
  const [nextCursor, setNextCursor] = useState<string | undefined>()
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [discardError, setDiscardError] = useState<string | null>(null)
  const [discarding, setDiscarding] = useState<string | null>(null)
  const [reloadVersion, setReloadVersion] = useState(0)
  const requestGeneration = useRef(0)

  useEffect(() => {
    let active = true
    const generation = ++requestGeneration.current

    listRecordDrafts()
      .then((response) => {
        if (!active || generation !== requestGeneration.current) return
        setDrafts(Array.isArray(response.items) ? response.items : [])
        setNextCursor(response.next_cursor || undefined)
        setLoading(false)
        setError(null)
      })
      .catch((cause: unknown) => {
        if (!active || generation !== requestGeneration.current) return
        setDrafts([])
        setNextCursor(undefined)
        setLoading(false)
        setError(describeDraftError(cause))
      })

    return () => {
      active = false
    }
  }, [reloadVersion])

  const reload = useCallback(() => {
    requestGeneration.current++
    setDrafts([])
    setNextCursor(undefined)
    setLoading(true)
    setLoadingMore(false)
    setError(null)
    setDiscardError(null)
    setReloadVersion((value) => value + 1)
  }, [])

  function loadMore() {
    if (!nextCursor || loadingMore) return
    const cursor = nextCursor
    const generation = requestGeneration.current
    setLoadingMore(true)
    setError(null)
    listRecordDrafts({ cursor })
      .then((response) => {
        if (generation !== requestGeneration.current) return
        setDrafts((current) => dedupeDrafts(
          current,
          Array.isArray(response.items) ? response.items : [],
        ))
        setNextCursor(response.next_cursor || undefined)
        setLoadingMore(false)
      })
      .catch((cause: unknown) => {
        if (generation !== requestGeneration.current) return
        setLoadingMore(false)
        setError(describeDraftError(cause))
      })
  }

  function discard(item: RecordDraft) {
    if (discarding) return
    const generation = requestGeneration.current
    setDiscarding(item.draft_id)
    setDiscardError(null)
    discardRecordDraft(item.draft_id)
      .then(() => {
        if (generation !== requestGeneration.current) return
        // The removed draft is dropped locally rather than by refetching, so the
        // cursor already spent on later pages stays valid.
        setDrafts((current) => current.filter((entry) => entry.draft_id !== item.draft_id))
        setDiscarding(null)
      })
      .catch((cause: unknown) => {
        if (generation !== requestGeneration.current) return
        setDiscarding(null)
        setDiscardError(describeDraftError(cause))
      })
  }

  const columns: DataTableColumn<RecordDraft>[] = [
    {
      key: 'title',
      label: '标题',
      render: (item) => (
        <Link className="record-drafts__title" to={draftEditorPath(item)}>
          {item.payload.title.trim() || '未命名草稿'}
        </Link>
      ),
    },
    {
      key: 'type',
      label: '类型',
      render: (item) => RECORD_TYPE_LABELS[item.payload.record_type],
    },
    {
      key: 'destination',
      label: '发布目标',
      render: (item) => (item.record_id
        ? <Link to={`/records/${item.record_id}`}>{item.record_id}</Link>
        : <Badge variant="info" tone="neutral">新建记录</Badge>),
    },
    {
      key: 'updated_at',
      label: '最后编辑',
      render: (item) => <Timestamp value={item.updated_at} />,
    },
    {
      key: 'expires_at',
      label: '过期时间',
      render: (item) => <Timestamp value={item.expires_at} />,
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      render: (item) => (
        <Button
          size="sm"
          variant="ghost"
          onClick={() => discard(item)}
          disabled={discarding === item.draft_id}
        >
          {discarding === item.draft_id ? '正在丢弃…' : '丢弃'}
        </Button>
      ),
    },
  ]

  return (
    <div className="page-stack record-drafts-page">
      <div className="page-header">
        <div>
          <div className="page-eyebrow">运维知识 · DRAFTS</div>
          <h1 className="page-title">记录草稿</h1>
          <p className="page-sub">
            未发布的草稿只对作者可见，到期后会被自动清理。
          </p>
        </div>
        <div className="header-actions">
          <Link className="btn sm secondary" to="/records">返回记录</Link>
          <Link className="btn sm primary" to="/records/new">新建记录</Link>
        </div>
      </div>

      <section className="page-stack record-drafts-results" aria-label="记录草稿">
        {discardError ? <p role="alert">丢弃草稿失败：{discardError}</p> : null}
        {loading ? <PageState kind="loading" title="正在加载记录草稿" /> : null}
        {!loading && error && drafts.length === 0 ? (
          <PageState
            kind="error"
            eyebrow="记录草稿"
            title="草稿列表不可用"
            description={error}
            action={<Button size="sm" onClick={reload}>重试</Button>}
          />
        ) : null}
        {!loading && !error && drafts.length === 0 ? (
          <PageState
            kind="empty"
            eyebrow="记录草稿"
            title="没有未发布的草稿"
            description="编辑记录时会自动保存草稿，从右上角新建记录即可开始。"
          />
        ) : null}
        {drafts.length > 0 ? (
          <>
            <DataTable
              className="record-drafts__table"
              caption="记录草稿列表"
              columns={columns}
              rows={drafts}
              rowKey={(item) => item.draft_id}
              density="compact"
            />
            <div className="section-heading__actions record-drafts-results__footer">
              {error ? <p role="alert">加载更多失败：{error}</p> : null}
              {nextCursor ? (
                <Button variant="secondary" onClick={loadMore} disabled={loadingMore}>
                  {loadingMore ? '正在加载…' : '加载更多'}
                </Button>
              ) : <span>已加载全部草稿</span>}
            </div>
          </>
        ) : null}
      </section>
    </div>
  )
}
