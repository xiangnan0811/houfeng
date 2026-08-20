import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError } from '../../../lib/apiRequest'
import { listSubjectActivity } from '../../../lib/recordsApi'
import type {
  SubjectActivityFilter,
  SubjectActivityFreshness,
  SubjectActivityHeader,
  SubjectActivityItem,
  SubjectActivitySourceStatus,
  SubjectActivityView,
  RecordSubjectKind,
} from '../../../lib/types'
import {
  subjectActivityFilterKey,
  subjectActivityToAPIQuery,
  type SubjectActivityFilters,
} from './activityQueryState'

export type SubjectActivityLoadStatus =
  | 'loading'
  | 'ready'
  | 'empty'
  | 'error'
  | 'unavailable'

export type SubjectActivityState = {
  status: SubjectActivityLoadStatus
  subject: SubjectActivityHeader | null
  view: SubjectActivityView
  items: SubjectActivityItem[]
  sourceStatuses: SubjectActivitySourceStatus[]
  freshness: SubjectActivityFreshness | null
  snapshotCursor: string
  nextCursor: string | null
  errorMessage: string | null
  errorCode: string | null
  loadingMore: boolean
}

export type SubjectActivityCommands = {
  refresh: () => void
  append: () => void
  refreshAtSnapshot: () => void
}

type Options = {
  kind: RecordSubjectKind
  sourceId: string
  view: SubjectActivityView
  filters: SubjectActivityFilters
  cursor?: string
}

function describeFailure(error: unknown): {
  message: string
  code: string | null
  unavailable: boolean
} {
  if (error instanceof ApiError) {
    const code = typeof error.code === 'string' ? error.code : null
    if (code === 'activity_projection_unavailable' || error.status === 503) {
      return { message: '活动投影尚未就绪。', code, unavailable: true }
    }
    if (code === 'cursor_invalid' || code === 'cursor_expired' || code === 'query_invalid') {
      return {
        message: '筛选或分页条件无法被服务端接受，请重置后重试。',
        code,
        unavailable: false,
      }
    }
    if (error.status === 404) {
      return { message: '主体不存在或无权查看。', code, unavailable: false }
    }
    return { message: error.message, code, unavailable: false }
  }
  return { message: '加载活动失败。', code: null, unavailable: false }
}

export function useSubjectActivity(options: Options): {
  state: SubjectActivityState
  commands: SubjectActivityCommands
} {
  const { kind, sourceId, view, filters, cursor } = options
  const filterKey = subjectActivityFilterKey(filters)
  const [state, setState] = useState<SubjectActivityState>({
    status: 'loading',
    subject: null,
    view,
    items: [],
    sourceStatuses: [],
    freshness: null,
    snapshotCursor: '',
    nextCursor: null,
    errorMessage: null,
    errorCode: null,
    loadingMore: false,
  })

  const requestIdRef = useRef(0)
  const snapshotRef = useRef('')
  const nextCursorRef = useRef<string | null>(null)
  const itemsRef = useRef<SubjectActivityItem[]>([])
  const filtersRef = useRef(filters)

  useEffect(() => {
    filtersRef.current = filters
  }, [filters])

  const run = useCallback(async (
    mode: 'replace' | 'append' | 'snapshot',
    pageCursor?: string,
  ) => {
    const requestId = ++requestIdRef.current
    setState((prev) => ({
      ...prev,
      status: mode === 'append' ? prev.status : 'loading',
      loadingMore: mode === 'append',
      errorMessage: mode === 'append' ? prev.errorMessage : null,
      errorCode: mode === 'append' ? prev.errorCode : null,
    }))

    const query: SubjectActivityFilter = subjectActivityToAPIQuery(
      view,
      filtersRef.current,
      pageCursor,
    )

    try {
      const response = await listSubjectActivity(kind, sourceId, query)
      if (requestId !== requestIdRef.current) return

      const nextItems = mode === 'append'
        ? [...itemsRef.current, ...response.items]
        : response.items
      itemsRef.current = nextItems
      snapshotRef.current = response.snapshot_cursor
      nextCursorRef.current = response.next_cursor ?? null

      setState({
        status: nextItems.length === 0 ? 'empty' : 'ready',
        subject: response.subject,
        view: response.view,
        items: nextItems,
        sourceStatuses: response.source_statuses,
        freshness: response.freshness,
        snapshotCursor: response.snapshot_cursor,
        nextCursor: response.next_cursor ?? null,
        errorMessage: null,
        errorCode: null,
        loadingMore: false,
      })
    } catch (error) {
      if (requestId !== requestIdRef.current) return
      const failure = describeFailure(error)
      setState((prev) => ({
        ...prev,
        status: failure.unavailable ? 'unavailable' : 'error',
        errorMessage: failure.message,
        errorCode: failure.code,
        loadingMore: false,
        ...(mode === 'append'
          ? {}
          : {
              items: [],
              subject: null,
              freshness: null,
              nextCursor: null,
              snapshotCursor: '',
            }),
      }))
    }
  }, [kind, sourceId, view])

  useEffect(() => {
    itemsRef.current = []
    nextCursorRef.current = null
    snapshotRef.current = ''
    // eslint-disable-next-line react-hooks/set-state-in-effect -- subject query reload: run() sets loading then awaits fetch; cascade is the intended network gate
    void run('replace', cursor)
  }, [kind, sourceId, view, filterKey, cursor, run])

  const commands: SubjectActivityCommands = {
    refresh: () => {
      void run('replace', cursor)
    },
    append: () => {
      const next = nextCursorRef.current
      if (!next || state.loadingMore) return
      void run('append', next)
    },
    refreshAtSnapshot: () => {
      const snapshot = snapshotRef.current
      if (!snapshot) {
        void run('replace', cursor)
        return
      }
      void run('snapshot', snapshot)
    },
  }

  return { state, commands }
}
