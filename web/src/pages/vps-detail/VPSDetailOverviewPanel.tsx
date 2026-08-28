import { Link } from 'react-router-dom'
import { useLayoutEffect, useRef } from 'react'

import { Badge, Button } from '../../components/atoms'
import type { VPSDetailOverviewModel, VPSOverviewAction } from './vpsDetailOverviewModel'

type VPSDetailOverviewPanelProps = {
  model: VPSDetailOverviewModel
  vpsId: string
  isArchived: boolean
  lifecycleSubmitting: boolean
  writeBlocked?: boolean
  onDecisionEdit: () => void
  onTimelineOpen: () => void
  onServicesOpen: () => void
  onDomainsOpen: () => void
  onCancellationOpen: () => void
  onFactEdit: () => void
  onFactsOpen: () => void
  onExperienceLog: () => void
  onMonitoringEvidence: () => void
  onMonitoringAgent: () => void
  onMonitoringLink: () => void
  onSubscriptionOpen: () => void
  onValidityExtend: () => void
  onServiceCreate: () => void
  onDomainCreate: () => void
  onArchiveStart: () => void
  onRestoreStart: () => void
}

function toneClass(tone?: string): string {
  return tone ? `vps-detail-overview__fact--${tone}` : ''
}

function judgementToneClass(tone?: string): string {
  return tone ? `vps-detail-overview__attention-item--${tone}` : ''
}

function actionKey(action: VPSOverviewAction): string {
  return `${action.kind}:${action.mode ?? action.to ?? action.label}`
}

export function VPSDetailOverviewPanel({
  model,
  vpsId,
  isArchived,
  lifecycleSubmitting,
  writeBlocked = false,
  onDecisionEdit,
  onTimelineOpen,
  onServicesOpen,
  onDomainsOpen,
  onCancellationOpen,
  onFactEdit,
  onFactsOpen,
  onExperienceLog,
  onMonitoringEvidence,
  onMonitoringAgent,
  onMonitoringLink,
  onSubscriptionOpen,
  onValidityExtend,
  onServiceCreate,
  onDomainCreate,
  onArchiveStart,
  onRestoreStart,
}: VPSDetailOverviewPanelProps) {
  const actionsMenuRef = useRef<HTMLDetailsElement | null>(null)

  useLayoutEffect(() => {
    function handleDocumentPointerDown(event: PointerEvent) {
      const menu = actionsMenuRef.current
      if (!menu?.open) return

      const target = event.target
      if (target instanceof Node && menu.contains(target)) return

      menu.open = false
    }

    document.addEventListener('pointerdown', handleDocumentPointerDown)
    return () => document.removeEventListener('pointerdown', handleDocumentPointerDown)
  }, [])

  function closeActionsMenu() {
    if (actionsMenuRef.current) {
      actionsMenuRef.current.open = false
    }
  }

  function runMenuAction(action: () => void) {
    closeActionsMenu()
    action()
  }

  function runOverviewAction(action: VPSOverviewAction) {
    if (action.mode === 'cancellation') {
      onCancellationOpen()
      return
    }
    if (action.mode === 'decision') {
      onDecisionEdit()
      return
    }
    if (action.mode === 'monitoring-instance-evidence') {
      onMonitoringEvidence()
      return
    }
    if (action.mode === 'monitoring-instance-create') {
      onMonitoringAgent()
      return
    }
    if (action.mode === 'monitoring-instance-link') {
      onMonitoringLink()
      return
    }
    if (action.mode === 'subscription') {
      onSubscriptionOpen()
      return
    }
    if (action.mode === 'validity-extension') {
      onValidityExtend()
    }
  }

  function renderOverviewAction(action: VPSOverviewAction, primary = false) {
    if (action.kind === 'link' && action.to) {
      return (
        <Link key={actionKey(action)} className={['btn', 'sm', primary ? 'primary' : 'secondary'].join(' ')} to={action.to}>
          {action.label}
        </Link>
      )
    }
    return (
      <Button key={actionKey(action)} variant={primary ? 'primary' : 'secondary'} size="sm" onClick={() => runOverviewAction(action)}>
        {action.label}
      </Button>
    )
  }

  return (
    <section className="page-panel vps-detail-overview" aria-labelledby="vps-detail-overview-title">
      <div className="vps-detail-overview__header">
        <div className="vps-detail-overview__identity">
          <p className="section-heading__eyebrow">VPS DETAIL</p>
          <h1 id="vps-detail-overview-title">{model.title}</h1>
          <div className="badge-row" aria-label="VPS 当前状态">
            {model.badges.map((badge) => (
              <Badge key={badge} variant="info" tone="neutral">{badge}</Badge>
            ))}
          </div>
        </div>
        <div className="vps-detail-overview__actions">
          <Button variant="secondary" size="sm" onClick={onTimelineOpen}>资产历史</Button>
          <Button variant="secondary" size="sm" onClick={onServicesOpen}>服务</Button>
          <Button variant="secondary" size="sm" onClick={onDomainsOpen}>域名</Button>
          <Button variant="primary" size="sm" onClick={onDecisionEdit}>调整决策</Button>
          <Button variant="secondary" size="sm" onClick={onFactsOpen}>基础资料</Button>
          <details ref={actionsMenuRef} className="watchtower-actions-menu vps-detail-actions-menu">
            <summary aria-label="VPS 详情操作">…</summary>
            <div className="watchtower-actions-menu__panel">
              <button type="button" onClick={() => runMenuAction(onFactEdit)}>编辑基础资料</button>
              <button type="button" onClick={() => runMenuAction(onExperienceLog)}>记录经验</button>
              <Link
                className="watchtower-actions-menu__item"
                onClick={closeActionsMenu}
                to={`/asset-decisions?view=needs_decision&renew_within_days=30&vps_id=${encodeURIComponent(vpsId)}`}
              >
                组合决策
              </Link>
              <button type="button" onClick={() => runMenuAction(onSubscriptionOpen)}>创建/更新订阅</button>
              <button type="button" onClick={() => runMenuAction(onValidityExtend)}>延长有效期</button>
              <button type="button" onClick={() => runMenuAction(onMonitoringEvidence)}>监控观测</button>
              <button type="button" onClick={() => runMenuAction(onMonitoringAgent)}>接入/升级 agent</button>
              <button type="button" onClick={() => runMenuAction(onMonitoringLink)}>关联已有监控实例</button>
              <button type="button" onClick={() => runMenuAction(onServiceCreate)}>新增服务</button>
              <button type="button" onClick={() => runMenuAction(onDomainCreate)}>新增域名</button>
              {isArchived ? (
                <button type="button" disabled={writeBlocked} onClick={() => runMenuAction(onRestoreStart)}>
                  {lifecycleSubmitting ? '恢复中…' : '恢复为闲置'}
                </button>
              ) : (
                <button
                  type="button"
                  className="watchtower-actions-menu__danger"
                  disabled={writeBlocked}
                  onClick={() => runMenuAction(onArchiveStart)}
                >
                  {lifecycleSubmitting ? '归档中…' : '归档 VPS'}
                </button>
              )}
            </div>
          </details>
          <Link className="btn sm ghost" to="/vps">VPS 列表</Link>
        </div>
      </div>

      <div className="vps-detail-overview__body">
        <dl className="vps-detail-overview__facts" aria-label="VPS 综合基础信息">
          {model.facts.map((fact) => (
            <div key={fact.label} className={['vps-detail-overview__fact', toneClass(fact.tone)].filter(Boolean).join(' ')}>
              <dt>{fact.label}</dt>
              <dd>{fact.value}</dd>
              {fact.meta ? <small>{fact.meta}</small> : null}
            </div>
          ))}
        </dl>
        <aside className={['vps-detail-overview__judgement', `vps-detail-overview__judgement--${model.judgement.tone}`].join(' ')} aria-label="当前判断">
          <h2>当前判断</h2>
          <dl>
            {model.judgement.rows.map((row) => (
              <div key={row.label}>
                <dt>{row.label}</dt>
                <dd>{row.value}</dd>
              </div>
            ))}
          </dl>
          {model.judgement.attentionItems.length > 0 ? (
            <div className="vps-detail-overview__attention-list" aria-label="当前需要关注的状态">
              {model.judgement.attentionItems.map((item) => (
                <article key={`${item.title}:${item.primaryAction.label}`} className={['vps-detail-overview__attention-item', judgementToneClass(item.tone)].filter(Boolean).join(' ')}>
                  <div>
                    <h3>{item.title}</h3>
                    <p>{item.reason}</p>
                  </div>
                  <div className="vps-detail-overview__attention-actions">
                    {renderOverviewAction(item.primaryAction, true)}
                    {item.secondaryActions.map((action) => renderOverviewAction(action))}
                  </div>
                </article>
              ))}
            </div>
          ) : null}
        </aside>
      </div>
    </section>
  )
}
