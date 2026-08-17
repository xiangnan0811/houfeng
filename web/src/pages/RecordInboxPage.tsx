import { useEffect, useState } from 'react'

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
import type { RecordNotification, RecordNotificationTarget } from '../lib/types'

type InboxState = 'loading' | 'ready' | 'empty' | 'error' | 'revoked' | 'deleted'

const eventLabels: Record<RecordNotification['event_kind'], string> = {
  record_action_assigned: '行动指派',
  record_action_completed: '行动完成',
  record_action_cancelled: '行动取消',
  record_comment_replied: '评论回复',
  record_comment_mentioned: '评论提及',
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
  const [busyId, setBusyId] = useState('')
  const [target, setTarget] = useState<{ notificationId: string; value: RecordNotificationTarget } | null>(null)

  useEffect(() => {
    let active = true
    listRecordNotifications(50)
      .then((response) => {
        if (!active) return
        const visible = response.items.filter((item) => item.dismissed_at === null)
        setItems(visible)
        setState(visible.length === 0 ? 'empty' : 'ready')
      })
      .catch((error: unknown) => {
        if (active) setState(closedFailureState(error))
      })
    return () => { active = false }
  }, [])

  function replaceItem(next: RecordNotification) {
    setItems((current) => current.map((item) => item.notification_id === next.notification_id ? next : item))
  }

  async function changeReadState(item: RecordNotification) {
    setBusyId(item.notification_id)
    try {
      replaceItem(item.read_at === null
        ? await markRecordNotificationRead(item.notification_id)
        : await markRecordNotificationUnread(item.notification_id))
    } catch {
      setState('error')
    } finally {
      setBusyId('')
    }
  }

  async function dismiss(item: RecordNotification) {
    setBusyId(item.notification_id)
    try {
      await dismissRecordNotification(item.notification_id)
      setItems((current) => {
        const next = current.filter((candidate) => candidate.notification_id !== item.notification_id)
        if (next.length === 0) setState('empty')
        return next
      })
      if (target?.notificationId === item.notification_id) setTarget(null)
    } catch {
      setState('error')
    } finally {
      setBusyId('')
    }
  }

  async function resolveTarget(item: RecordNotification) {
    setBusyId(item.notification_id)
    try {
      const value = await getRecordNotificationTarget(item.notification_id)
      setTarget({ notificationId: item.notification_id, value })
    } catch (error: unknown) {
      setState(closedFailureState(error))
    } finally {
      setBusyId('')
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
            const busy = busyId === item.notification_id
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
                      目标：{selectedTarget.subject_kind === 'comment' ? '评论' : '行动'} {selectedTarget.subject_id}
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
