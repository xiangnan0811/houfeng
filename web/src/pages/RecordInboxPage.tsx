import { useEffect, useRef, useState } from 'react'

import { PageState } from '../components/PageState'
import { Button, MonoDigits, Timestamp } from '../components/atoms'
import {
  dismissRecordNotification,
  getRecordNotificationTarget,
  listRecordNotifications,
  markRecordNotificationRead,
  markRecordNotificationUnread,
} from '../lib/recordCollaborationApi'
import { ApiError } from '../lib/apiRequest'
import { invalidateRecordNotificationUnreadCount } from '../lib/recordInboxUnreadApi'
import type { RecordNotification, RecordNotificationTarget } from '../lib/types'

type InboxState = 'loading' | 'ready' | 'empty' | 'error' | 'revoked' | 'deleted'

const eventLabels: Record<RecordNotification['event_kind'], string> = {
  record_owner_changed: '负责人更换',
  record_participant_changed: '参与人更换',
  record_follow_up_due: '跟进到期',
  action_assigned: '行动指派',
  action_completed: '行动完成',
  action_cancelled: '行动取消',
  comment_replied: '评论回复',
  comment_mentioned: '评论提及',
  security_access_revoked: '访问权限收紧',
}

const reasonLabels: Record<RecordNotification['reason'], string> = {
  owner: '负责人',
  participant: '参与人',
  assignee: '被指派',
  mention: '被提及',
  reply: '收到回复',
  follower: '关注更新',
  security: '安全提醒',
}

function closedFailureState(error: unknown): Extract<InboxState, 'error' | 'revoked' | 'deleted'> {
  if (error instanceof ApiError && error.status === 410) return 'deleted'
  if (error instanceof ApiError && (error.status === 403 || error.status === 404)) return 'revoked'
  return 'error'
}

export function RecordInboxPage() {
  const [state, setState] = useState<InboxState>('loading')
  const [items, setItems] = useState<RecordNotification[]>([])
  const [busyIds, setBusyIds] = useState<ReadonlySet<string>>(() => new Set())
  const [target, setTarget] = useState<{ notificationId: string; value: RecordNotificationTarget } | null>(null)
  const mountedRef = useRef(true)
  const operationSerialRef = useRef(0)
  const operationTokensRef = useRef(new Map<string, number>())
  const targetGenerationRef = useRef(0)

  useEffect(() => {
    let active = true
    const operationTokens = operationTokensRef.current
    mountedRef.current = true
    listRecordNotifications(50)
      .then((response) => {
        if (!active) return
        const visible = response.items.filter((item) => item.dismissed_at === null)
        setItems(visible)
        setState(visible.length === 0 ? 'empty' : 'ready')
      })
      .catch((error: unknown) => {
        if (active) failClosed(error)
      })
    return () => {
      active = false
      mountedRef.current = false
      targetGenerationRef.current += 1
      operationTokens.clear()
    }
  }, [])

  function beginBusy(notificationId: string): number {
    const token = operationSerialRef.current + 1
    operationSerialRef.current = token
    operationTokensRef.current.set(notificationId, token)
    setBusyIds((current) => new Set(current).add(notificationId))
    return token
  }

  function finishBusy(notificationId: string, token: number) {
    if (!mountedRef.current || operationTokensRef.current.get(notificationId) !== token) return
    operationTokensRef.current.delete(notificationId)
    setBusyIds((current) => {
      const next = new Set(current)
      next.delete(notificationId)
      return next
    })
  }

  function failClosed(error: unknown) {
    if (!mountedRef.current) return
    targetGenerationRef.current += 1
    operationTokensRef.current.clear()
    setBusyIds(new Set())
    setItems([])
    setTarget(null)
    setState(closedFailureState(error))
  }

  function invalidateTargetFor(notificationId: string) {
    setTarget((current) => current?.notificationId === notificationId ? null : current)
  }

  function replaceItem(next: RecordNotification) {
    setItems((current) => current.map((item) => item.notification_id === next.notification_id ? next : item))
  }

  async function changeReadState(item: RecordNotification) {
    const token = beginBusy(item.notification_id)
    invalidateTargetFor(item.notification_id)
    try {
      const next = item.read_at === null
        ? await markRecordNotificationRead(item.notification_id)
        : await markRecordNotificationUnread(item.notification_id)
      if (!sameNotificationIdentity(item, next)) throw new Error('record_notification_identity_mismatch')
      if (mountedRef.current && operationTokensRef.current.get(item.notification_id) === token) {
        invalidateTargetFor(item.notification_id)
        replaceItem(next)
        invalidateRecordNotificationUnreadCount()
      }
    } catch (error: unknown) {
      if (operationTokensRef.current.get(item.notification_id) === token) failClosed(error)
    } finally {
      finishBusy(item.notification_id, token)
    }
  }

  async function dismiss(item: RecordNotification) {
    const token = beginBusy(item.notification_id)
    invalidateTargetFor(item.notification_id)
    try {
      const next = await dismissRecordNotification(item.notification_id)
      if (!sameNotificationIdentity(item, next) || next.dismissed_at === null) {
        throw new Error('record_notification_identity_mismatch')
      }
      if (!mountedRef.current || operationTokensRef.current.get(item.notification_id) !== token) return
      invalidateTargetFor(item.notification_id)
      setItems((current) => {
        const next = current.filter((candidate) => candidate.notification_id !== item.notification_id)
        if (next.length === 0) setState('empty')
        return next
      })
      invalidateRecordNotificationUnreadCount()
    } catch (error: unknown) {
      if (operationTokensRef.current.get(item.notification_id) === token) failClosed(error)
    } finally {
      finishBusy(item.notification_id, token)
    }
  }

  async function resolveTarget(item: RecordNotification) {
    const token = beginBusy(item.notification_id)
    const generation = targetGenerationRef.current + 1
    targetGenerationRef.current = generation
    try {
      const value = await getRecordNotificationTarget(item.notification_id)
      if (value.record_id !== item.record_id || value.subject_kind !== item.subject_kind || value.subject_id !== item.subject_id) {
        throw new Error('record_notification_target_mismatch')
      }
      if (mountedRef.current && targetGenerationRef.current === generation &&
        operationTokensRef.current.get(item.notification_id) === token) {
        setTarget({ notificationId: item.notification_id, value })
      }
    } catch (error: unknown) {
      if (mountedRef.current && operationTokensRef.current.get(item.notification_id) === token) failClosed(error)
    } finally {
      finishBusy(item.notification_id, token)
    }
  }

  return (
    <div className="page-stack record-inbox-page animate-in">
      <div className="page-header">
        <div>
          <div className="page-eyebrow">协作值班 · RECORD INBOX</div>
          <h1 className="page-title">记录协作收件箱</h1>
          <p className="page-subtitle">只显示当前授权下的行动与评论送达，不展开记录正文。</p>
        </div>
        <div className="record-inbox-page__summary hero-meta-card" aria-label="通知摘要">
          <span>可见通知</span><MonoDigits>{items.length}</MonoDigits>
        </div>
      </div>

      {state === 'loading' ? <PageState kind="loading" title="正在读取记录通知" /> : null}
      {state === 'empty' ? <PageState kind="empty" surface="empty" title="当前没有待处理通知" description="新的行动指派、回复或提及会出现在这里。" /> : null}
      {state === 'revoked' ? <PageState kind="empty" title="收件箱访问已撤销" description="当前通知内容已收起。" /> : null}
      {state === 'deleted' ? <PageState kind="empty" title="通知目标已删除" description="关联内容不再可用。" /> : null}
      {state === 'error' ? <PageState kind="error" title="记录通知暂不可用" description="请稍后重试；当前内容未展示。" /> : null}

      {state === 'ready' ? (
        <ol className="record-inbox-list vps-create-form" aria-label="记录通知">
          {items.map((item) => {
            const label = eventLabels[item.event_kind]
            const busy = busyIds.has(item.notification_id)
            const selectedTarget = target?.notificationId === item.notification_id ? target.value : null
            return (
              <li key={item.notification_id} className={`record-inbox-item card card--state card--ribbon-left metric-card${item.read_at === null ? ' tone--notice record-inbox-item--unread' : ''}`}>
                <div className="record-inbox-item__signal" aria-hidden="true" />
                <div className="record-inbox-item__body vps-create-form__section">
                  <header>
                    <strong>{label}</strong>
                    <span className={`badge badge--info ${item.mandatory ? 'tone--notice record-inbox-item__mandatory' : 'record-inbox-item__optional'}`}>
                      {item.mandatory ? '必要送达' : '关注送达'}
                    </span>
                  </header>
                  <p><span>{reasonLabels[item.reason]}</span><code>{item.record_id}</code></p>
                  <div className="record-inbox-item__meta">
                    <Timestamp value={item.event_at} mode="both" />
                    <span>{item.read_at === null ? '未读' : '已读'}</span>
                    <span>源版本 <MonoDigits>{item.source_version}</MonoDigits></span>
                  </div>
                  {selectedTarget ? (
                    <p className="record-inbox-item__target inline-alert info" role="status">
                      目标：{selectedTarget.subject_kind === 'comment' ? '评论' : selectedTarget.subject_kind === 'action' ? '行动' : '记录'} {selectedTarget.subject_id}
                    </p>
                  ) : null}
                </div>
                <div className="record-inbox-item__commands page-form-actions">
                  <Button size="sm" variant="secondary" disabled={busy}
                    aria-label={`查看“${label}”的对象`} onClick={() => void resolveTarget(item)}>查看对象</Button>
                  <Button size="sm" variant="ghost" disabled={busy}
                    aria-label={`标记“${label}”为${item.read_at === null ? '已读' : '未读'}`}
                    onClick={() => void changeReadState(item)}>{item.read_at === null ? '标记已读' : '标记未读'}</Button>
                  <Button size="sm" variant="ghost" disabled={busy}
                    aria-label={`移除“${label}”`} onClick={() => void dismiss(item)}>移除</Button>
                </div>
              </li>
            )
          })}
        </ol>
      ) : null}
    </div>
  )
}

function sameNotificationIdentity(current: RecordNotification, next: RecordNotification): boolean {
  return current.notification_id === next.notification_id && current.record_id === next.record_id &&
    current.event_kind === next.event_kind && current.subject_kind === next.subject_kind &&
    current.subject_id === next.subject_id && current.source_version === next.source_version &&
    current.reason === next.reason && current.mandatory === next.mandatory && current.event_at === next.event_at
}
