import { useEffect, useState } from 'react'

import type { RecordActionCreateValues, RecordActionUpdateValues } from '../../../components/RecordActionPanel'
import type { RecordCommentSubmit } from '../../../components/RecordCommentThread'
import type { RecordCollaborationSurfaceState } from '../../../components/RecordCollaborationState'
import { ApiError } from '../../../lib/apiRequest'
import {
  createRecordAction,
  createRecordComment,
  editRecordComment,
  getRecordWatch,
  listRecordActions,
  listRecordComments,
  redactRecordComment,
  setRecordWatch,
  transitionRecordAction,
  updateRecordAction,
} from '../../../lib/recordCollaborationApi'
import type {
  RecordAction,
  RecordActionTransition,
  RecordComment,
  RecordFollowerPreference,
  RecordWatch,
} from '../../../lib/types'
import type { RecordWorkspaceMode } from '../hooks/useRecordDraft'

export type RecordCollaboration = {
  state: RecordCollaborationSurfaceState
  busy: boolean
  actions: RecordAction[]
  comments: RecordComment[]
  watch: RecordWatch | null
  createAction: (values: RecordActionCreateValues, onDone?: () => void) => void
  updateAction: (action: RecordAction, values: RecordActionUpdateValues) => void
  transitionAction: (action: RecordAction, transition: RecordActionTransition) => void
  submitComment: (input: RecordCommentSubmit) => void
  redactComment: (comment: RecordComment) => void
  setWatchPreference: (preference: RecordFollowerPreference) => void
}

/**
 * 记录工作区的协作状态与请求编排：组件保持受控，本 hook 只在已存在的记录上读取
 * 行动、评论与关注，并在每次写入后以服务端列表为准刷新。
 */
export function useRecordCollaboration(recordId: string | undefined, mode: RecordWorkspaceMode): RecordCollaboration {
  const [actions, setActions] = useState<RecordAction[]>([])
  const [comments, setComments] = useState<RecordComment[]>([])
  const [watch, setWatch] = useState<RecordWatch | null>(null)
  const [state, setState] = useState<RecordCollaborationSurfaceState>(
    !recordId || mode === 'new' ? 'empty' : 'loading',
  )
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!recordId || mode === 'new') {
      return
    }
    let active = true
    Promise.all([
      listRecordActions(recordId),
      listRecordComments(recordId),
      getRecordWatch(recordId),
    ]).then(([actionList, commentList, nextWatch]) => {
      if (!active) return
      setActions(actionList.items)
      setComments(commentList.comments)
      setWatch(nextWatch)
      setState('ready')
    }).catch((error: unknown) => {
      if (!active) return
      if (error instanceof ApiError && (error.status === 403 || error.status === 404)) setState('revoked')
      else setState('error')
    })
    return () => {
      active = false
    }
  }, [mode, recordId])

  function run<T>(request: () => Promise<T>, apply: (result: T) => void) {
    setBusy(true)
    void request()
      .then(apply)
      .catch(() => setState('error'))
      .finally(() => setBusy(false))
  }

  function refreshActions(mutate: () => Promise<unknown>, onDone?: () => void) {
    if (!recordId) return
    run(() => mutate().then(() => listRecordActions(recordId)), (response) => {
      setActions(response.items)
      onDone?.()
    })
  }

  function refreshComments(mutate: () => Promise<unknown>) {
    if (!recordId) return
    run(() => mutate().then(() => listRecordComments(recordId)), (response) => setComments(response.comments))
  }

  return {
    state,
    busy,
    actions,
    comments,
    watch,
    createAction: (values, onDone) => {
      if (!recordId) return
      refreshActions(() => createRecordAction(recordId, values, crypto.randomUUID()), onDone)
    },
    updateAction: (action, values) => {
      if (!recordId) return
      refreshActions(() => updateRecordAction(recordId, action.action_id, values, values.version, crypto.randomUUID()))
    },
    transitionAction: (action, transition) => {
      if (!recordId) return
      refreshActions(() => transitionRecordAction(recordId, action.action_id, transition, action.version, crypto.randomUUID()))
    },
    submitComment: (input) => {
      if (!recordId) return
      refreshComments(() => (input.mode === 'edit'
        ? editRecordComment(recordId, input.comment_id, {
          body_markdown: input.body_markdown,
          mention_user_ids: input.mention_user_ids,
        }, input.version, crypto.randomUUID())
        : createRecordComment(recordId, {
          body_markdown: input.body_markdown,
          reply_to_comment_id: input.reply_to_comment_id,
          mention_user_ids: input.mention_user_ids,
        }, crypto.randomUUID())))
    },
    redactComment: (comment) => {
      if (!recordId) return
      refreshComments(() => redactRecordComment(recordId, comment.comment_id, comment.version, crypto.randomUUID()))
    },
    setWatchPreference: (preference) => {
      if (!recordId || !watch) return
      run(() => setRecordWatch(recordId, preference, watch.version, crypto.randomUUID()), setWatch)
    },
  }
}
