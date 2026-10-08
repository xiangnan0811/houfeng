import { Link } from 'react-router-dom'

import { Button, Input, SegmentedControl } from '../../../components/atoms'
import { Modal } from '../../../components/atoms/Modal'
import { PageState } from '../../../components/PageState'
import type { RecordSubjectKind } from '../../../lib/types'
import { ComparisonEvidenceChoice } from './ComparisonEvidenceChoice'
import { evidenceChoiceMeta } from './comparisonEvidenceCopy'
import { subjectEvidenceHref } from './comparisonSubjectHref'
import type { ComparisonURLFixedItem } from './comparisonQueryState'
import { useComparisonObjectPicker } from './useComparisonObjectPicker'

const SUBJECT_KINDS: { value: RecordSubjectKind; label: string }[] = [
  { value: 'vps', label: 'VPS' },
  { value: 'monitoring_instance', label: '监控实例' },
  { value: 'target', label: '探测目标' },
]

type Props = {
  open: boolean
  onClose: () => void
  from: string
  to: string
  basketSnapshotIds: readonly string[]
  selectionError: string | null
  onAdd: (item: ComparisonURLFixedItem) => void
}

export function ComparisonObjectDialog({
  open,
  onClose,
  from,
  to,
  basketSnapshotIds,
  selectionError,
  onAdd,
}: Props) {
  const { state, commands } = useComparisonObjectPicker({ open, from, to, onAdd })
  const inBasket = new Set(basketSnapshotIds)
  return (
    <Modal open={open} onClose={onClose} title="添加对象" size="lg">
      <div className="record-compare-picker">
        {selectionError ? <p className="record-compare-items__alert" role="alert">{selectionError}</p> : null}
        <p className="record-muted record-compare-picker__note">同一主体可以加入多份证据。加入前会先读取快照，读失败不会当作比较成功。</p>
        <SegmentedControl
          label="资产类型"
          items={SUBJECT_KINDS}
          value={state.subjectKind}
          onChange={commands.setSubjectKind}
        />
        <SubjectList
          status={state.subjectsStatus}
          message={state.subjectsMessage}
          subjects={state.subjects}
          selectedId={state.selected?.id ?? null}
          onSelect={commands.selectSubject}
          onRetry={commands.retrySubjects}
        />
        {state.selected ? (
          <EvidenceBrowser
            from={state.from}
            to={state.to}
            status={state.evidenceStatus}
            message={state.evidenceMessage}
            rows={state.rows}
            hasMore={state.hasMore}
            loadingMore={state.loadingMore}
            moreError={state.moreError}
            evidenceNotice={state.evidenceNotice}
            addingSnapshotId={state.addingSnapshotId}
            inBasket={inBasket}
            evidenceHref={subjectEvidenceHref(state.selected.kind, state.selected.id)}
            onWindow={commands.setWindow}
            onLoadMore={commands.loadMore}
            onRetry={commands.retryEvidence}
            onAdd={(snapshotId) => { void commands.addSnapshot(snapshotId) }}
          />
        ) : null}
      </div>
    </Modal>
  )
}

function SubjectList({
  status,
  message,
  subjects,
  selectedId,
  onSelect,
  onRetry,
}: {
  status: ReturnType<typeof useComparisonObjectPicker>['state']['subjectsStatus']
  message: string | null
  subjects: ReturnType<typeof useComparisonObjectPicker>['state']['subjects']
  selectedId: string | null
  onSelect: ReturnType<typeof useComparisonObjectPicker>['commands']['selectSubject']
  onRetry: () => void
}) {
  if (status === 'idle' || status === 'loading') {
    return <PageState compact kind="loading" title="正在读取资产" />
  }
  if (status === 'unreadable') {
    return (
      <PageState
        compact
        kind="error"
        title="资产无法读取"
        description={message ?? '这些资产无法读取。'}
        action={<Button size="sm" variant="secondary" onClick={onRetry}>重试</Button>}
      />
    )
  }
  if (status === 'error') {
    return (
      <PageState
        compact
        kind="error"
        title="无法读取资产"
        description={message ?? undefined}
        action={<Button size="sm" variant="secondary" onClick={onRetry}>重试</Button>}
      />
    )
  }
  if (status === 'empty') {
    return <PageState compact kind="empty" title={message ?? '暂无资产'} />
  }
  return (
    <ul className="record-compare-picker__subjects" aria-label="可加入的资产">
      {subjects.map((subject) => (
        <li key={`${subject.kind}:${subject.id}`} className="record-compare-picker__subject">
          <Button
            size="sm"
            variant={subject.id === selectedId ? 'primary' : 'secondary'}
            aria-pressed={subject.id === selectedId}
            onClick={() => onSelect(subject)}
          >
            {subject.label}
          </Button>
          {subject.detail ? <span className="record-muted">{subject.detail}</span> : null}
          <details className="record-disclosure record-compare-items__tech">
            <summary>技术标识</summary>
            <code>{subject.id}</code>
          </details>
        </li>
      ))}
    </ul>
  )
}

function EvidenceBrowser({
  from,
  to,
  status,
  message,
  rows,
  hasMore,
  loadingMore,
  moreError,
  evidenceNotice,
  addingSnapshotId,
  inBasket,
  evidenceHref,
  onWindow,
  onLoadMore,
  onRetry,
  onAdd,
}: {
  from: string
  to: string
  status: ReturnType<typeof useComparisonObjectPicker>['state']['evidenceStatus']
  message: string | null
  rows: ReturnType<typeof useComparisonObjectPicker>['state']['rows']
  hasMore: boolean
  loadingMore: boolean
  moreError: string | null
  evidenceNotice: string | null
  addingSnapshotId: string | null
  inBasket: ReadonlySet<string>
  evidenceHref: string
  onWindow: (from: string, to: string) => void
  onLoadMore: () => void
  onRetry: () => void
  onAdd: (snapshotId: string) => void
}) {
  return (
    <div className="record-compare-picker">
      <div className="record-form-grid">
        <Input label="证据开始" className="mono" value={from} onChange={(event) => onWindow(event.target.value, to)} />
        <Input label="证据结束" className="mono" value={to} onChange={(event) => onWindow(from, event.target.value)} />
      </div>
      {evidenceNotice ? (
        <p className="record-compare-picker__source" role="status">
          <span>{evidenceNotice}</span>
          <Button size="sm" variant="secondary" onClick={onRetry}>重试</Button>
        </p>
      ) : null}
      {status === 'loading' ? <PageState compact kind="loading" title="正在读取证据" /> : null}
      {status === 'unavailable' ? (
        <PageState
          compact
          kind="error"
          title="活动投影暂不可用"
          description={message ?? '活动投影暂不可用。'}
          action={<Button size="sm" variant="secondary" onClick={onRetry}>重试</Button>}
        />
      ) : null}
      {status === 'unreadable' ? (
        <PageState
          compact
          kind="error"
          title="证据无法读取"
          description={message ?? '当前主体的证据无法读取。'}
          action={<Button size="sm" variant="secondary" onClick={onRetry}>重试</Button>}
        />
      ) : null}
      {status === 'error' ? (
        <PageState
          compact
          kind="error"
          title="无法读取证据"
          description={message ?? undefined}
          action={<Button size="sm" variant="secondary" onClick={onRetry}>重试</Button>}
        />
      ) : null}
      {status === 'empty' ? (
        <PageState
          compact
          kind="empty"
          title="当前窗口没有证据"
          description="可以调整时间范围，或到该主体的证据工作区查看。"
          action={<Link className="btn md secondary" to={evidenceHref}>打开证据工作区</Link>}
        />
      ) : null}
      {status === 'ready' ? (
        <div>
          {rows.map((row) => {
            const added = inBasket.has(row.snapshotId)
            return (
              <ComparisonEvidenceChoice
                key={row.snapshotId}
                title={row.title}
                snapshotId={row.snapshotId}
                meta={evidenceChoiceMeta({
                  timeLabel: row.timeLabel,
                  kindLabel: row.kindLabel,
                  qualityLabel: row.qualityLabel,
                  pending: row.pending,
                  unreadable: row.unreadable,
                })}
              >
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={added || addingSnapshotId === row.snapshotId}
                  onClick={() => onAdd(row.snapshotId)}
                >
                  {added ? '已在比较篮' : row.unreadable ? '重试读取' : '加入比较'}
                </Button>
              </ComparisonEvidenceChoice>
            )
          })}
          {moreError ? <p className="record-compare-items__alert" role="alert">{moreError}</p> : null}
          {hasMore || moreError ? (
            <Button size="sm" variant="secondary" disabled={loadingMore} onClick={onLoadMore}>
              {loadingMore ? '加载中…' : '加载更多'}
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
