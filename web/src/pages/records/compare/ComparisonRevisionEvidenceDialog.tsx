import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { Button } from '../../../components/atoms'
import { Modal } from '../../../components/atoms/Modal'
import { PageState } from '../../../components/PageState'
import { ApiError } from '../../../lib/apiRequest'
import { getEvidenceSnapshot, getRecordRevision } from '../../../lib/recordsApi'
import type { RecordSubjectKind } from '../../../lib/types'
import { ComparisonEvidenceChoice } from './ComparisonEvidenceChoice'
import { evidenceChoiceMeta, factsFromSnapshot, isAbortError } from './comparisonEvidenceCopy'
import { subjectEvidenceHref } from './comparisonSubjectHref'

type RevisionItem = {
  record_id: string
  revision_id: string
  snapshot_ids?: string[]
}

type Row = {
  snapshotId: string
  title: string
  timeLabel: string
  kindLabel: string
  qualityLabel: string | null
  pending: boolean
  unreadable: boolean
}

type LoadState =
  | { status: 'loading' }
  | { status: 'unreadable'; message: string }
  | { status: 'empty'; evidenceHref: string | null }
  | { status: 'error'; message: string }
  | { status: 'ready'; rows: Row[]; evidenceHref: string | null }

type Props = {
  item: RevisionItem | null
  onClose: () => void
  onApply: (item: RevisionItem, snapshotIds: string[]) => void
}

function isPermission(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 404 || error.status === 403)
}

function pendingRow(snapshotId: string): Row {
  return {
    snapshotId,
    title: '未命名证据',
    timeLabel: '',
    kindLabel: '证据快照',
    qualityLabel: null,
    pending: true,
    unreadable: false,
  }
}

function revisionEvidenceKey(item: RevisionItem): string {
  const snapshots = (item.snapshot_ids ?? []).map((id) => id.trim()).filter(Boolean).join(',')
  return `${item.record_id}:${item.revision_id}:${snapshots}`
}

export function ComparisonRevisionEvidenceDialog({ item, onClose, onApply }: Props) {
  return (
    <Modal open={item != null} onClose={onClose} title="选择修订证据" size="lg">
      {item ? (
        <RevisionEvidenceBody
          key={revisionEvidenceKey(item)}
          item={item}
          onClose={onClose}
          onApply={onApply}
        />
      ) : null}
    </Modal>
  )
}

function RevisionEvidenceBody({
  item,
  onClose,
  onApply,
}: {
  item: RevisionItem
  onClose: () => void
  onApply: (item: RevisionItem, snapshotIds: string[]) => void
}) {
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [checked, setChecked] = useState<string[]>([])
  const requestRef = useRef(0)
  const touchedRef = useRef(new Set<string>())
  const recordId = item.record_id
  const revisionId = item.revision_id
  const snapshotKey = (item.snapshot_ids ?? []).map((id) => id.trim()).filter(Boolean).join('\n')

  useEffect(() => {
    const requestId = ++requestRef.current
    const controller = new AbortController()
    touchedRef.current = new Set()
    const initial = new Set(snapshotKey.split('\n').filter(Boolean))
    void getRecordRevision(recordId, revisionId)
      .then(async (revision) => {
        if (requestId !== requestRef.current) return
        const ids = [...new Set((revision.evidence_snapshot_ids ?? []).map((id) => id.trim()).filter(Boolean))]
        const subject = revision.subjects.find((entry) => entry.primary) ?? revision.subjects[0]
        const evidenceHref = subject
          ? subjectEvidenceHref(subject.kind as RecordSubjectKind, subject.source_id)
          : null
        if (ids.length === 0) {
          setLoad({ status: 'empty', evidenceHref })
          return
        }
        const rows = ids.map(pendingRow)
        setLoad({ status: 'ready', rows, evidenceHref })
        await Promise.all(ids.map(async (snapshotId) => {
          try {
            const snapshot = await getEvidenceSnapshot(snapshotId, controller.signal)
            if (requestId !== requestRef.current || controller.signal.aborted) return
            const facts = factsFromSnapshot(snapshot)
            setLoad((current) => {
              if (current.status !== 'ready') return current
              return {
                ...current,
                rows: current.rows.map((row) => row.snapshotId === snapshotId
                  ? {
                    ...row,
                    title: facts.title,
                    timeLabel: facts.timeLabel,
                    kindLabel: facts.kindLabel,
                    qualityLabel: facts.qualityLabel,
                    pending: false,
                    unreadable: false,
                  }
                  : row),
              }
            })
            if (!touchedRef.current.has(snapshotId) && initial.has(snapshotId)) {
              setChecked((current) => current.includes(snapshotId) ? current : [...current, snapshotId])
            }
          } catch (error) {
            if (requestId !== requestRef.current || controller.signal.aborted || isAbortError(error)) return
            setLoad((current) => {
              if (current.status !== 'ready') return current
              return {
                ...current,
                rows: current.rows.map((row) => row.snapshotId === snapshotId
                  ? { ...row, pending: false, unreadable: true, qualityLabel: null }
                  : row),
              }
            })
            setChecked((current) => current.filter((id) => id !== snapshotId))
          }
        }))
      })
      .catch((error: unknown) => {
        if (requestId !== requestRef.current) return
        if (isPermission(error)) {
          setLoad({ status: 'unreadable', message: '该修订无法读取。' })
          return
        }
        setLoad({ status: 'error', message: '无法读取该修订，请重试' })
      })
    return () => {
      requestRef.current += 1
      controller.abort()
    }
  }, [recordId, revisionId, snapshotKey])

  function toggle(snapshotId: string) {
    touchedRef.current.add(snapshotId)
    setChecked((current) => (
      current.includes(snapshotId)
        ? current.filter((id) => id !== snapshotId)
        : [...current, snapshotId]
    ))
  }

  const rows = load.status === 'ready' ? load.rows : []
  const readableIds = new Set(rows.filter((row) => !row.pending && !row.unreadable).map((row) => row.snapshotId))
  const selectedIds = checked.filter((id) => readableIds.has(id))
  const pending = rows.some((row) => row.pending)

  return (
      <div className="record-compare-picker">
        <p className="record-muted record-compare-picker__note">只更新这一项修订上的证据，不新开比较对象。不可读的快照不会写入。</p>
        {load.status === 'loading' ? <PageState compact kind="loading" title="正在读取修订" /> : null}
        {load.status === 'unreadable' ? (
          <PageState compact kind="error" title="修订无法读取" description={load.message} />
        ) : null}
        {load.status === 'error' ? (
          <PageState compact kind="error" title="无法读取修订" description={load.message} />
        ) : null}
        {load.status === 'empty' ? (
          <PageState
            compact
            kind="empty"
            title="该修订没有关联证据"
            description="比较篮里的这项修订仍保留。"
            {...(load.evidenceHref ? {
              action: <Link className="btn md secondary" to={load.evidenceHref}>打开证据工作区</Link>,
            } : {})}
          />
        ) : null}
        {load.status === 'ready' ? (
          <>
            {rows.map((row) => (
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
                <label className="record-compare-items__check">
                  <input
                    type="checkbox"
                    aria-label={`${row.unreadable ? '不可读' : '选用'} ${row.title}`}
                    checked={checked.includes(row.snapshotId)}
                    disabled={row.pending || row.unreadable}
                    onChange={() => toggle(row.snapshotId)}
                  />
                  <span>{row.unreadable ? '不可读' : '选用'}</span>
                </label>
              </ComparisonEvidenceChoice>
            ))}
            {pending ? <p role="status">正在读取证据，读完后再保存。</p> : null}
            {!pending && readableIds.size === 0 ? (
              <p role="status">没有可读证据。比较篮里的这项修订仍保留。</p>
            ) : null}
            <Button
              size="md"
              disabled={pending || selectedIds.length === 0}
              onClick={() => {
                onApply(item, selectedIds)
                onClose()
              }}
            >
              保存证据选择
            </Button>
          </>
        ) : null}
      </div>
  )
}
