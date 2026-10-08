import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

import { SOURCE_KIND_LABELS, SOURCE_STATE_LABELS } from '../../../components/timelineChannel'
import { listMonitoringInstances, listTargets, listVPSAssets } from '../../../lib/api'
import { ApiError } from '../../../lib/apiRequest'
import { formatDateTime } from '../../../lib/format'
import { getEvidenceSnapshot, listSubjectActivity } from '../../../lib/recordsApi'
import type {
  MonitoringInstanceRecord,
  RecordSubjectKind,
  SubjectActivityItem,
  SubjectActivityListResponse,
  TargetRecord,
  VPSAssetRecord,
} from '../../../lib/types'
import { factsFromSnapshot, isAbortError } from './comparisonEvidenceCopy'
import type { ComparisonURLFixedItem } from './comparisonQueryState'

export type ComparisonPickerSubject = {
  kind: RecordSubjectKind
  id: string
  label: string
  detail: string
}

export type ComparisonPickerRow = {
  snapshotId: string
  title: string
  timeLabel: string
  kindLabel: string
  qualityLabel: string | null
  pending: boolean
  unreadable: boolean
}

export type ComparisonSubjectListStatus = 'idle' | 'loading' | 'ready' | 'empty' | 'unreadable' | 'error'
export type ComparisonEvidenceStatus =
  | 'idle'
  | 'loading'
  | 'ready'
  | 'empty'
  | 'unavailable'
  | 'unreadable'
  | 'error'

export type ComparisonObjectPickerState = {
  subjectKind: RecordSubjectKind
  subjectsStatus: ComparisonSubjectListStatus
  subjectsMessage: string | null
  subjects: ComparisonPickerSubject[]
  selected: ComparisonPickerSubject | null
  from: string
  to: string
  evidenceStatus: ComparisonEvidenceStatus
  evidenceMessage: string | null
  rows: ComparisonPickerRow[]
  hasMore: boolean
  loadingMore: boolean
  moreError: string | null
  evidenceNotice: string | null
  addingSnapshotId: string | null
}

type Options = {
  open: boolean
  from: string
  to: string
  onAdd: (item: ComparisonURLFixedItem) => void
}

type PickedSubject = {
  cycle: number
  kind: RecordSubjectKind
  retry: number
  subject: ComparisonPickerSubject
}

type WindowOverride = {
  cycle: number
  propsFrom: string
  propsTo: string
  from: string
  to: string
}

type AddingRequest = {
  cycle: number
  snapshotId: string
}

type SubjectPage = {
  key: string
  status: Exclude<ComparisonSubjectListStatus, 'idle' | 'loading'>
  message: string | null
  subjects: ComparisonPickerSubject[]
}

type EvidencePage = {
  key: string
  status: Exclude<ComparisonEvidenceStatus, 'idle' | 'loading'>
  message: string | null
  rows: ComparisonPickerRow[]
  hasMore: boolean
  loadingMore: boolean
  moreError: string | null
  notice: string | null
}

function named(value: string, fallback: string): string {
  const trimmed = value.trim()
  return trimmed || fallback
}

function vpsSubject(record: VPSAssetRecord): ComparisonPickerSubject {
  return {
    kind: 'vps',
    id: record.vps_id,
    label: named(record.display_name, '未命名 VPS'),
    detail: [record.provider_name, record.region].filter(Boolean).join(' · '),
  }
}

function instanceSubject(record: MonitoringInstanceRecord): ComparisonPickerSubject {
  return {
    kind: 'monitoring_instance',
    id: record.monitoring_instance_id,
    label: named(record.display_name, '未命名监控实例'),
    detail: [record.provider, record.region].filter(Boolean).join(' · '),
  }
}

function targetSubject(record: TargetRecord): ComparisonPickerSubject {
  return {
    kind: 'target',
    id: record.target_id,
    label: named(record.name, '未命名探测目标'),
    detail: record.host.trim(),
  }
}

function emptySubjectMessage(kind: RecordSubjectKind): string {
  if (kind === 'vps') return '暂无 VPS。'
  if (kind === 'monitoring_instance') return '暂无监控实例。'
  return '暂无探测目标。'
}

async function loadAuthorizedSubjects(kind: RecordSubjectKind): Promise<ComparisonPickerSubject[]> {
  if (kind === 'vps') return (await listVPSAssets()).map(vpsSubject)
  if (kind === 'monitoring_instance') return (await listMonitoringInstances()).map(instanceSubject)
  return (await listTargets()).map(targetSubject)
}

function isPermissionFailure(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 404 || error.status === 403)
}

function describeSubjectFailure(error: unknown): { status: 'unreadable' | 'error'; message: string } {
  if (isPermissionFailure(error)) return { status: 'unreadable', message: '这些资产无法读取。' }
  return { status: 'error', message: '无法读取资产，请重试' }
}

function describeEvidenceFailure(error: unknown): {
  status: 'unavailable' | 'unreadable' | 'error'
  message: string
} {
  if (error instanceof ApiError && (error.status === 503 || error.code === 'activity_projection_unavailable')) {
    return { status: 'unavailable', message: '活动投影暂不可用。' }
  }
  if (isPermissionFailure(error)) return { status: 'unreadable', message: '当前主体的证据无法读取。' }
  if (
    error instanceof ApiError
    && (error.code === 'cursor_invalid' || error.code === 'cursor_expired' || error.code === 'query_invalid')
  ) {
    return { status: 'error', message: '时间范围或分页无法被接受，请调整后重试。' }
  }
  return { status: 'error', message: '无法读取证据，请重试' }
}

function rowFromActivity(item: SubjectActivityItem): ComparisonPickerRow | null {
  const snapshotId = item.evidence_snapshot_id?.trim() ?? ''
  if (!snapshotId) return null
  return {
    snapshotId,
    title: item.presentation.title.trim() || '未命名证据',
    timeLabel: formatDateTime(item.event_at),
    kindLabel: '证据快照',
    qualityLabel: null,
    pending: true,
    unreadable: false,
  }
}

function mergeRows(current: readonly ComparisonPickerRow[], incoming: readonly ComparisonPickerRow[]): ComparisonPickerRow[] {
  const seen = new Set(current.map((row) => row.snapshotId))
  const next = current.slice()
  for (const row of incoming) {
    if (seen.has(row.snapshotId)) continue
    seen.add(row.snapshotId)
    next.push(row)
  }
  return next
}

// Freshness.state stays "ready" when projector source health is bad. An empty
// evidence page is authoritative only when evidence_snapshot itself is not a
// known stale or unavailable source. Reason codes stay off the primary copy.
function evidenceSourceNotice(response: SubjectActivityListResponse): string | null {
  const incomplete = (response.source_statuses ?? []).find((status) =>
    status.source_kind === 'evidence_snapshot'
    && (status.state === 'stale' || status.state === 'unavailable'),
  )
  if (!incomplete) return null
  const stateLabel = SOURCE_STATE_LABELS[incomplete.state]
  if (!stateLabel) return null
  return `${SOURCE_KIND_LABELS.evidence_snapshot}：${stateLabel}。当前列表可能不完整。`
}

function evidenceRequestKey(
  cycle: number,
  subject: ComparisonPickerSubject | null,
  from: string,
  to: string,
  retry: number,
): string {
  if (!subject) return ''
  return `${cycle}\n${subject.kind}\n${subject.id}\n${from}\n${to}\n${retry}`
}

function samePickerSubject(
  left: ComparisonPickerSubject | null,
  right: ComparisonPickerSubject | null,
): boolean {
  if (left === right) return true
  if (!left || !right) return false
  return left.kind === right.kind && left.id === right.id
}

function subjectRequestKey(cycle: number, kind: RecordSubjectKind, retry: number): string {
  return `${cycle}:${kind}:${retry}`
}

function activityRows(response: SubjectActivityListResponse): ComparisonPickerRow[] {
  return response.items.flatMap((item) => {
    const row = rowFromActivity(item)
    return row ? [row] : []
  })
}

export function useComparisonObjectPicker(options: Options): {
  state: ComparisonObjectPickerState
  commands: {
    setSubjectKind: (kind: RecordSubjectKind) => void
    selectSubject: (subject: ComparisonPickerSubject) => void
    setWindow: (from: string, to: string) => void
    loadMore: () => void
    retrySubjects: () => void
    retryEvidence: () => void
    addSnapshot: (snapshotId: string) => Promise<void>
  }
} {
  const { open, from: initialFrom, to: initialTo, onAdd } = options
  const [sessionOpen, setSessionOpen] = useState(open)
  const [openCycle, setOpenCycle] = useState(0)
  const [subjectKind, setSubjectKindState] = useState<RecordSubjectKind>('vps')
  const [subjectRetry, setSubjectRetry] = useState(0)
  const [picked, setPicked] = useState<PickedSubject | null>(null)
  const [windowOverride, setWindowOverride] = useState<WindowOverride | null>(null)
  const [evidenceRetry, setEvidenceRetry] = useState(0)
  const [subjectPage, setSubjectPage] = useState<SubjectPage | null>(null)
  const [evidencePage, setEvidencePage] = useState<EvidencePage | null>(null)
  const [adding, setAdding] = useState<AddingRequest | null>(null)

  // Reopen starts a new session. The hook instance stays mounted, so the cycle
  // is the key; selection and local window are tagged with it and fall back
  // when it changes. Conditional update is the supported render path for that.
  if (sessionOpen !== open) {
    setSessionOpen(open)
    if (open) setOpenCycle((value) => value + 1)
  }

  const selected = picked
    && picked.cycle === openCycle
    && picked.kind === subjectKind
    && picked.retry === subjectRetry
    ? picked.subject
    : null
  const windowFrom = windowOverride
    && windowOverride.cycle === openCycle
    && windowOverride.propsFrom === initialFrom
    && windowOverride.propsTo === initialTo
    ? windowOverride.from
    : initialFrom
  const windowTo = windowOverride
    && windowOverride.cycle === openCycle
    && windowOverride.propsFrom === initialFrom
    && windowOverride.propsTo === initialTo
    ? windowOverride.to
    : initialTo
  const evidenceKey = open ? evidenceRequestKey(openCycle, selected, windowFrom, windowTo, evidenceRetry) : ''
  const subjectKey = subjectRequestKey(openCycle, subjectKind, subjectRetry)
  const subjectsMatch = open && subjectPage?.key === subjectKey
  const subjectsStatus: ComparisonSubjectListStatus = !open
    ? 'idle'
    : subjectsMatch
      ? subjectPage.status
      : 'loading'
  const subjects = subjectsMatch ? subjectPage.subjects : []
  const subjectsMessage = subjectsMatch ? subjectPage.message : null
  const evidenceMatch = evidenceKey !== '' && evidencePage?.key === evidenceKey
  const evidenceStatus: ComparisonEvidenceStatus = !open || !selected
    ? 'idle'
    : evidenceMatch
      ? evidencePage.status
      : 'loading'
  const rows = evidenceMatch ? evidencePage.rows : []
  const hasMore = evidenceMatch ? evidencePage.hasMore : false
  const loadingMore = evidenceMatch ? evidencePage.loadingMore : false
  const moreError = evidenceMatch ? evidencePage.moreError : null
  const evidenceMessage = evidenceMatch ? evidencePage.message : null
  const evidenceNotice = evidenceMatch ? evidencePage.notice : null
  const addingSnapshotId = open && adding?.cycle === openCycle ? adding.snapshotId : null

  const subjectRequestRef = useRef(0)
  const evidenceRequestRef = useRef(0)
  const nextCursorRef = useRef<string | null>(null)
  const rowsRef = useRef<ComparisonPickerRow[]>([])
  const loadingMoreRef = useRef(false)
  const evidenceAbortRef = useRef<AbortController | null>(null)
  const addAbortRef = useRef<AbortController | null>(null)
  const openRef = useRef(open)
  const onAddRef = useRef(onAdd)
  const selectedRef = useRef<ComparisonPickerSubject | null>(null)
  const fromRef = useRef(initialFrom)
  const toRef = useRef(initialTo)
  const wasOpenRef = useRef(open)
  const evidenceKeyRef = useRef('')
  const subjectKindRef = useRef<RecordSubjectKind>('vps')
  const subjectRetryRef = useRef(0)
  const evidenceRetryRef = useRef(0)
  const openCycleRef = useRef(0)

  const invalidateInFlight = useCallback(() => {
    evidenceRequestRef.current += 1
    evidenceAbortRef.current?.abort()
    evidenceAbortRef.current = null
    addAbortRef.current?.abort()
    addAbortRef.current = null
    loadingMoreRef.current = false
    nextCursorRef.current = null
    rowsRef.current = []
  }, [])

  const patchRow = useCallback((
    key: string,
    snapshotId: string,
    patch: (row: ComparisonPickerRow) => ComparisonPickerRow,
  ) => {
    setEvidencePage((current) => {
      if (!current || current.key !== key) return current
      const nextRows = current.rows.map((row) => row.snapshotId === snapshotId ? patch(row) : row)
      rowsRef.current = nextRows
      return { ...current, rows: nextRows }
    })
  }, [])

  const verifySnapshots = useCallback((
    ids: readonly string[],
    requestId: number,
    signal: AbortSignal,
    key: string,
  ) => {
    for (const snapshotId of ids) {
      void getEvidenceSnapshot(snapshotId, signal)
        .then((snapshot) => {
          if (requestId !== evidenceRequestRef.current || signal.aborted || !openRef.current) return
          const facts = factsFromSnapshot(snapshot)
          patchRow(key, snapshotId, (row) => ({
            ...row,
            title: snapshot.title.trim() ? facts.title : row.title,
            timeLabel: facts.timeLabel,
            kindLabel: facts.kindLabel,
            qualityLabel: facts.qualityLabel,
            pending: false,
            unreadable: false,
          }))
        })
        .catch((error: unknown) => {
          if (requestId !== evidenceRequestRef.current || signal.aborted || !openRef.current || isAbortError(error)) return
          patchRow(key, snapshotId, (row) => ({
            ...row,
            qualityLabel: null,
            pending: false,
            unreadable: true,
          }))
        })
    }
  }, [patchRow])

  useLayoutEffect(() => {
    const closing = wasOpenRef.current && !open
    const windowChanged = fromRef.current !== windowFrom || toRef.current !== windowTo
    const selectedChanged = !samePickerSubject(selectedRef.current, selected)
    openRef.current = open
    onAddRef.current = onAdd
    evidenceRetryRef.current = evidenceRetry
    subjectKindRef.current = subjectKind
    subjectRetryRef.current = subjectRetry
    openCycleRef.current = openCycle
    wasOpenRef.current = open
    if (closing) {
      subjectRequestRef.current += 1
      selectedRef.current = selected
      fromRef.current = windowFrom
      toRef.current = windowTo
      evidenceKeyRef.current = evidenceKey
      invalidateInFlight()
      return
    }
    if (open && (windowChanged || selectedChanged)) {
      selectedRef.current = selected
      fromRef.current = windowFrom
      toRef.current = windowTo
      evidenceKeyRef.current = evidenceKey
      invalidateInFlight()
      return
    }
    selectedRef.current = selected
    fromRef.current = windowFrom
    toRef.current = windowTo
    evidenceKeyRef.current = evidenceKey
  }, [
    evidenceKey,
    evidenceRetry,
    invalidateInFlight,
    onAdd,
    open,
    openCycle,
    selected,
    subjectKind,
    subjectRetry,
    windowFrom,
    windowTo,
  ])

  useEffect(() => {
    if (!open) return
    const requestId = ++subjectRequestRef.current
    const key = subjectKey
    const kind = subjectKind
    let active = true
    void loadAuthorizedSubjects(kind)
      .then((items) => {
        if (!active || requestId !== subjectRequestRef.current) return
        if (items.length === 0) {
          setSubjectPage({ key, status: 'empty', message: emptySubjectMessage(kind), subjects: [] })
          return
        }
        setSubjectPage({ key, status: 'ready', message: null, subjects: items })
      })
      .catch((error: unknown) => {
        if (!active || requestId !== subjectRequestRef.current) return
        const failure = describeSubjectFailure(error)
        setSubjectPage({ key, status: failure.status, message: failure.message, subjects: [] })
      })
    return () => {
      active = false
      subjectRequestRef.current += 1
    }
  }, [open, subjectKey, subjectKind])

  useEffect(() => {
    const subject = selected
    if (!open || !subject || !evidenceKey) {
      evidenceAbortRef.current?.abort()
      evidenceAbortRef.current = null
      nextCursorRef.current = null
      rowsRef.current = []
      loadingMoreRef.current = false
      return
    }
    const controller = new AbortController()
    evidenceAbortRef.current?.abort()
    evidenceAbortRef.current = controller
    const requestId = ++evidenceRequestRef.current
    const key = evidenceKey
    const from = windowFrom
    const to = windowTo
    nextCursorRef.current = null
    rowsRef.current = []
    loadingMoreRef.current = false
    const signal = controller.signal
    void listSubjectActivity(subject.kind, subject.id, {
      view: 'evidence',
      source: ['evidence_snapshot'],
      versions: 'history',
      from,
      to,
    }, signal)
      .then((response) => {
        if (signal.aborted || requestId !== evidenceRequestRef.current || !openRef.current) return
        if (!samePickerSubject(selectedRef.current, subject)) return
        if (fromRef.current !== from || toRef.current !== to) return
        const incoming = activityRows(response)
        const rows = mergeRows([], incoming)
        const nextCursor = response.next_cursor?.trim() || null
        const notice = evidenceSourceNotice(response)
        const authoritativeEmpty = rows.length === 0 && notice == null
        const page: EvidencePage = {
          key,
          status: authoritativeEmpty ? 'empty' : 'ready',
          message: authoritativeEmpty ? '当前窗口没有证据。' : null,
          rows,
          hasMore: Boolean(nextCursor),
          loadingMore: false,
          moreError: null,
          notice,
        }
        nextCursorRef.current = nextCursor
        rowsRef.current = rows
        loadingMoreRef.current = false
        setEvidencePage(page)
        if (incoming.length > 0) verifySnapshots(incoming.map((row) => row.snapshotId), requestId, signal, key)
      })
      .catch((error: unknown) => {
        if (signal.aborted || requestId !== evidenceRequestRef.current || !openRef.current || isAbortError(error)) return
        if (!samePickerSubject(selectedRef.current, subject) || fromRef.current !== from || toRef.current !== to) return
        const failure = describeEvidenceFailure(error)
        nextCursorRef.current = null
        rowsRef.current = []
        loadingMoreRef.current = false
        setEvidencePage({
          key,
          status: failure.status,
          message: failure.message,
          rows: [],
          hasMore: false,
          loadingMore: false,
          moreError: null,
          notice: null,
        })
      })
    return () => {
      evidenceRequestRef.current += 1
      controller.abort()
    }
  }, [evidenceKey, open, selected, verifySnapshots, windowFrom, windowTo])

  useEffect(() => () => {
    addAbortRef.current?.abort()
    addAbortRef.current = null
    evidenceAbortRef.current?.abort()
    evidenceAbortRef.current = null
  }, [])

  const appendEvidence = useCallback(() => {
    const subject = selectedRef.current
    const signal = evidenceAbortRef.current?.signal
    const cursor = nextCursorRef.current
    const key = evidenceKeyRef.current
    if (!openRef.current || !subject || !signal || signal.aborted || !cursor || !key) {
      loadingMoreRef.current = false
      return
    }
    const requestId = evidenceRequestRef.current
    const from = fromRef.current
    const to = toRef.current
    void listSubjectActivity(subject.kind, subject.id, {
      view: 'evidence',
      source: ['evidence_snapshot'],
      versions: 'history',
      from,
      to,
      cursor,
    }, signal)
      .then((response) => {
        if (signal.aborted || requestId !== evidenceRequestRef.current || !openRef.current) return
        if (!samePickerSubject(selectedRef.current, subject)) return
        if (fromRef.current !== from || toRef.current !== to || evidenceKeyRef.current !== key) return
        const incoming = activityRows(response)
        const nextCursor = response.next_cursor?.trim() || null
        const notice = evidenceSourceNotice(response)
        nextCursorRef.current = nextCursor
        loadingMoreRef.current = false
        setEvidencePage((current) => {
          if (!current || current.key !== key) return current
          const merged = mergeRows(current.rows, incoming)
          rowsRef.current = merged
          const authoritativeEmpty = merged.length === 0 && notice == null
          return {
            ...current,
            rows: merged,
            hasMore: Boolean(nextCursor),
            loadingMore: false,
            moreError: null,
            notice,
            status: authoritativeEmpty ? 'empty' : 'ready',
            message: authoritativeEmpty ? '当前窗口没有证据。' : null,
          }
        })
        if (incoming.length > 0) verifySnapshots(incoming.map((row) => row.snapshotId), requestId, signal, key)
      })
      .catch((error: unknown) => {
        if (signal.aborted || requestId !== evidenceRequestRef.current || isAbortError(error) || !openRef.current) return
        loadingMoreRef.current = false
        const message = describeEvidenceFailure(error).message
        setEvidencePage((current) => {
          if (!current || current.key !== key) return current
          return { ...current, loadingMore: false, moreError: message }
        })
      })
  }, [verifySnapshots])

  const addSnapshot = useCallback(async (snapshotId: string) => {
    const id = snapshotId.trim()
    if (!id || !openRef.current) return
    addAbortRef.current?.abort()
    const controller = new AbortController()
    addAbortRef.current = controller
    const key = evidenceKeyRef.current
    const cycle = openCycleRef.current
    setAdding({ cycle, snapshotId: id })
    try {
      const snapshot = await getEvidenceSnapshot(id, controller.signal)
      if (controller.signal.aborted || !openRef.current) return
      const facts = factsFromSnapshot(snapshot)
      patchRow(key, id, (row) => ({
        ...row,
        title: snapshot.title.trim() ? facts.title : row.title,
        timeLabel: facts.timeLabel,
        kindLabel: facts.kindLabel,
        qualityLabel: facts.qualityLabel,
        pending: false,
        unreadable: false,
      }))
      onAddRef.current({ snapshot_id: snapshot.snapshot_id.trim() || id })
    } catch (error) {
      if (controller.signal.aborted || isAbortError(error) || !openRef.current) return
      patchRow(key, id, (row) => ({
        ...row,
        qualityLabel: null,
        pending: false,
        unreadable: true,
      }))
    } finally {
      if (addAbortRef.current === controller) {
        addAbortRef.current = null
        setAdding((current) => current?.snapshotId === id && current.cycle === cycle ? null : current)
      }
    }
  }, [patchRow])

  return {
    state: {
      subjectKind,
      subjectsStatus,
      subjectsMessage,
      subjects,
      selected,
      from: windowFrom,
      to: windowTo,
      evidenceStatus,
      evidenceMessage,
      rows,
      hasMore,
      loadingMore,
      moreError,
      evidenceNotice,
      addingSnapshotId,
    },
    commands: {
      // Refs and generation move before setState so a response that resumes
      // before the next effect cannot match the previous kind, subject, or window.
      setSubjectKind(kind) {
        if (kind === subjectKindRef.current) return
        subjectKindRef.current = kind
        selectedRef.current = null
        evidenceKeyRef.current = ''
        subjectRequestRef.current += 1
        invalidateInFlight()
        setAdding(null)
        setSubjectKindState(kind)
      },
      selectSubject(subject) {
        const current = selectedRef.current
        if (current?.kind === subject.kind && current.id === subject.id) return
        selectedRef.current = subject
        evidenceKeyRef.current = evidenceRequestKey(
          openCycleRef.current,
          subject,
          fromRef.current,
          toRef.current,
          evidenceRetryRef.current,
        )
        invalidateInFlight()
        setAdding(null)
        setPicked({
          cycle: openCycleRef.current,
          kind: subjectKindRef.current,
          retry: subjectRetryRef.current,
          subject,
        })
      },
      setWindow(from, to) {
        if (from === fromRef.current && to === toRef.current) return
        fromRef.current = from
        toRef.current = to
        evidenceKeyRef.current = evidenceRequestKey(
          openCycleRef.current,
          selectedRef.current,
          from,
          to,
          evidenceRetryRef.current,
        )
        invalidateInFlight()
        setAdding(null)
        setWindowOverride({
          cycle: openCycleRef.current,
          propsFrom: initialFrom,
          propsTo: initialTo,
          from,
          to,
        })
      },
      loadMore() {
        if (!nextCursorRef.current || loadingMoreRef.current) return
        const key = evidenceKeyRef.current
        if (!key) return
        loadingMoreRef.current = true
        setEvidencePage((current) => {
          if (!current || current.key !== key) return current
          return { ...current, loadingMore: true, moreError: null }
        })
        appendEvidence()
      },
      retrySubjects() {
        selectedRef.current = null
        evidenceKeyRef.current = ''
        subjectRetryRef.current += 1
        subjectRequestRef.current += 1
        invalidateInFlight()
        setAdding(null)
        setSubjectRetry((value) => value + 1)
      },
      retryEvidence() {
        const nextRetry = evidenceRetryRef.current + 1
        evidenceRetryRef.current = nextRetry
        evidenceKeyRef.current = evidenceRequestKey(
          openCycleRef.current,
          selectedRef.current,
          fromRef.current,
          toRef.current,
          nextRetry,
        )
        invalidateInFlight()
        setEvidenceRetry(nextRetry)
      },
      addSnapshot,
    },
  }
}
