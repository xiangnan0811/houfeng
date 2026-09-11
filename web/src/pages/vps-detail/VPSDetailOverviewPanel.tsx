import { Link, useLocation } from 'react-router-dom'
import { useCallback, useLayoutEffect, useRef } from 'react'

import { Badge, Button, Timestamp } from '../../components/atoms'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import type { AssetDomainRecord, AssetServiceRecord, SubscriptionRecord, VPSIPQualityReport, VPSMonitoringInstanceSummary } from '../../lib/types'
import { overviewLifecycleLabel, overviewMonitoringInstanceCountLabel, overviewSameAssetActionTitle } from '../../lib/vpsOverviewPresentation'

import { LifecycleBadge, RenewalBadge, UsageBadge } from '../assetPageBadges'
import { VPSAssetMark } from './VPSAssetMark'
import { VPSDetailResourceList } from './VPSDetailResourceList'
import { VPSFactList } from './VPSFactList'
import { legacyOverviewFactRows } from './vpsFactPresentation'
import { VPSObservationRows } from './VPSObservationRows'
import {
  domainResourceExtra,
  domainResourceName,
  domainResourceStatus,
  httpHref,
  monitoringInstanceDetailHref,
  monitoringInstanceName,
  readyResourceState,
  serviceResourceName,
  serviceResourceStatus,
  serviceResourceType,
  vpsIdentityMetaFields,
} from './vpsDetailResourcePresentation'



import { VPSDetailSectionNav } from './VPSDetailSectionNav'
import { VPSIPQualitySection } from './VPSIPQualitySection'
import { VPSSubscriptionOpsBody } from './VPSSubscriptionOpsBody'
import { VPSIdentityMeta } from './VPSOverviewIdentityHeader'

import type { VPSDetailModalMode } from './types'
import type {
  VPSContextAction,
  VPSDetailOverviewModel,
  VPSOverviewAction,
} from './vpsDetailOverviewModel'

type VPSDetailOverviewPanelProps = {
  model: VPSDetailOverviewModel
  vpsId: string
  isArchived: boolean
  lifecycleSubmitting: boolean
  writeBlocked?: boolean
  subscriptions?: SubscriptionRecord[]
  subscriptionsError?: string | null
  services?: AssetServiceRecord[]
  domains?: AssetDomainRecord[]
  recordsPending?: boolean
  ipQuality?: VPSIPQualityReport | null
  ipQualityError?: string | null
  monitoringInstances?: VPSMonitoringInstanceSummary[]

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


function judgementToneClass(tone?: string): string {
  return tone ? `vps-detail-overview__attention-item--${tone}` : ''
}

function actionKey(action: VPSOverviewAction): string {
  return `${action.kind}:${action.mode ?? action.to ?? action.label}`
}

const WRITE_MODES: Record<string, true> = {
  cancellation: true,
  decision: true,
  subscription: true,
  facts: true,
  experience: true,
  service: true,
  domain: true,
  'monitoring-instance-create': true,
  'monitoring-instance-link': true,
  'validity-extension': true,
}



function AttentionList({
  items,
  renderAction,
}: {
  items: VPSContextAction[]
  renderAction: (action: VPSOverviewAction, primary?: boolean) => React.ReactNode
}) {
  if (items.length === 0) return null
  return (
    <div className="vps-detail-overview__attention-list" aria-label="当前需要关注的状态">
      {items.map((item) => {
        const actions = [renderAction(item.primaryAction, true), ...item.secondaryActions.map((action) => renderAction(action))].filter(Boolean)
        return (
          <article key={`${item.title}:${item.primaryAction.label}`} className={['vps-detail-overview__attention-item', judgementToneClass(item.tone)].filter(Boolean).join(' ')}>
            <div>
              <h3>{item.title}</h3>
              <p>{item.reason}</p>
            </div>
            {actions.length > 0 ? <div className="vps-detail-overview__attention-actions">{actions}</div> : null}
          </article>
        )
      })}
    </div>
  )
}

export function VPSDetailOverviewPanel({
  model,
  vpsId,
  isArchived,
  lifecycleSubmitting,
  writeBlocked = false,
  subscriptions = [],
  subscriptionsError = null,
  services = [],
  domains = [],
  recordsPending = false,
  ipQuality = null,
  ipQualityError = null,
  monitoringInstances = [],
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
  const location = useLocation()
  const actionsMenuRef = useRef<HTMLDetailsElement | null>(null)
  const identityFacts = model.facts.filter((fact) => fact.domain === 'identity')
  const subscriptionRelated = model.relatedItems.find((item) => item.key === 'subscription')
  const monitoringRelated = model.relatedItems.find((item) => item.key === 'monitoring')
  const monitoringHealth = monitoringInstances[0]?.current_health_status ?? ''
  const monitoringCount = monitoringInstances.length
  const incidentDetail = monitoringRelated?.secondary?.trim() ?? ''
  const monitoringConclusion = monitoringHealth || monitoringRelated?.primary || ''
  const monitoringCountLabel = monitoringCount > 0 ? overviewMonitoringInstanceCountLabel(monitoringCount) : ''
  const distinctIncident = Boolean(
    incidentDetail
    && incidentDetail !== monitoringConclusion
    && incidentDetail !== monitoringRelated?.primary
    && !/^0\s*个活跃异常$/.test(incidentDetail)
  )
  const monitoringDescription = [monitoringCountLabel, distinctIncident ? incidentDetail : ''].filter(Boolean).join(' · ')
  const primarySubscription = subscriptions[0]
  const extraSubscriptions = subscriptions.slice(1)
  const plannedCancellation = model.badges[0] === overviewLifecycleLabel('to_cancel')
  const decisionRow = model.judgement.rows.find((row) => row.label === '决策')
  const renewalStatusRow = model.judgement.rows.find((row) => row.label === '续费')
  const providerFact = identityFacts.find((fact) => fact.label === '服务商')
  const locationFact = identityFacts.find((fact) => fact.label === '地区 / 数据中心')
  const ipv4Fact = identityFacts.find((fact) => fact.label === 'IPv4')



  const closeActionsMenu = useCallback((restoreFocus = false) => {
    const menu = actionsMenuRef.current
    if (!menu) return
    menu.open = false
    if (restoreFocus) menu.querySelector('summary')?.focus()
  }, [])

  useLayoutEffect(() => {
    function handleDocumentPointerDown(event: PointerEvent) {
      const menu = actionsMenuRef.current
      if (!menu?.open) return

      const target = event.target
      if (target instanceof Node && menu.contains(target)) return

      closeActionsMenu()
    }

    function handleDocumentKeyDown(event: KeyboardEvent) {
      if (event.key !== 'Escape' || !actionsMenuRef.current?.open) return
      event.preventDefault()
      event.stopPropagation()
      closeActionsMenu(true)
    }

    document.addEventListener('pointerdown', handleDocumentPointerDown)
    document.addEventListener('keydown', handleDocumentKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handleDocumentPointerDown)
      document.removeEventListener('keydown', handleDocumentKeyDown)
    }
  }, [closeActionsMenu])

  function runMenuAction(action: () => void) {
    closeActionsMenu(true)
    action()
  }

  function openRelated(mode: NonNullable<VPSDetailModalMode>) {
    if (mode === 'cancellation') {
      onCancellationOpen()
      return
    }
    if (mode === 'decision') {
      onDecisionEdit()
      return
    }
    if (mode === 'monitoring-instance-evidence') {
      onMonitoringEvidence()
      return
    }
    if (mode === 'monitoring-instance-create') {
      onMonitoringAgent()
      return
    }
    if (mode === 'monitoring-instance-link') {
      onMonitoringLink()
      return
    }
    if (mode === 'subscription') {
      onSubscriptionOpen()
      return
    }
    if (mode === 'validity-extension') {
      onValidityExtend()
      return
    }
    if (mode === 'facts' || mode === 'facts-detail') {
      onFactsOpen()
      return
    }
    if (mode === 'experience') {
      onExperienceLog()
      return
    }
    if (mode === 'service') {
      onServiceCreate()
      return
    }
    if (mode === 'domain') {
      onDomainCreate()
      return
    }
    if (mode === 'services-detail') {
      onServicesOpen()
      return
    }
    if (mode === 'domains-detail') {
      onDomainsOpen()
      return
    }
    if (mode === 'timeline-detail') {
      onTimelineOpen()
    }
  }

  function runOverviewAction(action: VPSOverviewAction) {
    if (READ_ONLY_PREVIEW && action.mode && WRITE_MODES[action.mode]) return
    if (action.mode) {
      openRelated(action.mode)
    }
  }

  function renderOverviewAction(action: VPSOverviewAction, primary = false) {
    if (action.kind === 'link' && action.to) {
      return (
        <Link
          key={actionKey(action)}
          className={['btn', 'sm', primary ? 'primary' : 'secondary'].join(' ')}
          to={action.to}
          {...(action.to.startsWith('/vps/') ? { state: location.state } : {})}
        >
          {action.label}
        </Link>
      )
    }
    if (READ_ONLY_PREVIEW && action.mode && WRITE_MODES[action.mode]) {
      return (
        <Button key={actionKey(action)} variant={primary ? 'primary' : 'secondary'} size="sm" disabled title="只读预览不可写入">
          {action.label}
        </Button>
      )
    }
    return (
      <Button key={actionKey(action)} variant={primary ? 'primary' : 'secondary'} size="sm" onClick={() => runOverviewAction(action)}>
        {action.label}
      </Button>
    )
  }

  function relatedTitle(item: NonNullable<typeof subscriptionRelated>) {
    if (item.titleAction.kind === 'link' && item.titleAction.to) {
      return (
        <Link
          className="text-link"
          to={item.titleAction.to}
          {...(item.titleAction.to.startsWith('/vps/') ? { state: location.state } : {})}
        >
          {item.title}
        </Link>
      )
    }
    const writeTitle = item.titleAction.kind === 'modal' && item.titleAction.mode && WRITE_MODES[item.titleAction.mode]
    if (READ_ONLY_PREVIEW && writeTitle) {
      return <span>{item.title}</span>
    }
    return (
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={() => {
          if (item.titleAction.kind === 'modal' && item.titleAction.mode) openRelated(item.titleAction.mode)
        }}
      >
        {item.title}
      </Button>
    )
  }

  function relatedQuickActions(item: NonNullable<typeof subscriptionRelated>) {
    return item.quickActions.map((action) => {
      if (action.kind === 'link' && action.to) {
        return <Link key={actionKey(action)} className="text-link" to={action.to}>{action.label}</Link>
      }
      if (READ_ONLY_PREVIEW && action.mode && WRITE_MODES[action.mode]) return null
      return (
        <Button
          key={actionKey(action)}
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => {
            if (action.mode === 'monitoring-instance-create') {
              onMonitoringAgent()
              return
            }
            if (action.mode) openRelated(action.mode)
          }}
        >
          {action.label}
        </Button>
      )
    })
  }

  function renderOpsAttentionAction(action: VPSOverviewAction, primary = false) {
    if (action.mode === 'validity-extension' || action.mode === 'subscription') return null
    return renderOverviewAction(action, primary)
  }

  function renderMonitoringAttentionAction(action: VPSOverviewAction, primary = false) {
    if (action.mode === 'monitoring-instance-create' || action.mode === 'monitoring-instance-link' || action.label === '查看 IP 质量' || action.label === '查看监控实例' || action.label === '监控观测') return null
    return renderOverviewAction(action, primary)
  }



  return (
    <>
      <section className="vps-detail-overview" aria-labelledby="vps-detail-overview-title">
        <div className="vps-detail-overview__header vps-overview-identity">
          <div className="vps-overview-identity__lead">
            <VPSAssetMark />
            <div className="vps-overview-identity__copy vps-detail-overview__identity">
              <div className="vps-overview-identity__title-row">
                <h1 id="vps-detail-overview-title">{model.title}</h1>
                {READ_ONLY_PREVIEW ? <Badge variant="info" className="vps-overview-identity__readonly">只读预览</Badge> : null}
              </div>
              <div className="vps-overview-identity__statuses" role="group" aria-label="VPS 当前状态">
                {model.badges[0] ? (
                  <span className="vps-overview-identity__status vps-overview-identity__status--lifecycle">
                    <LifecycleBadge value={model.badges[0]} />
                  </span>
                ) : null}
                {model.badges[1] ? (
                  <span className="vps-overview-identity__status vps-overview-identity__status--usage">
                    <UsageBadge value={model.badges[1]} />
                  </span>
                ) : null}
                {model.badges[2] ? (
                  <span className="vps-overview-identity__status vps-overview-identity__status--decision">
                    <RenewalBadge value={model.badges[2]} />
                  </span>
                ) : null}
              </div>
              <VPSIdentityMeta
                items={vpsIdentityMetaFields({
                  vpsId,
                  ...(providerFact?.value ? { providerName: providerFact.value } : {}),
                  ...(locationFact?.value ? { location: locationFact.value } : {}),
                  ...(ipv4Fact?.value ? { ipv4: ipv4Fact.value } : {}),
                  ...(model.updatedAt ? { updatedAt: model.updatedAt } : {}),
                })}
              />
            </div>
          </div>

          {READ_ONLY_PREVIEW ? null : (
          <div className="vps-detail-overview__actions">
            <details ref={actionsMenuRef} className="watchtower-actions-menu vps-detail-actions-menu">
              <summary className="btn sm primary" aria-label="VPS 详情操作：管理">管理</summary>
              <div className="watchtower-actions-menu__panel">
                <p className="vps-detail-actions-menu__group">业务 / 账单</p>
                <button type="button" onClick={() => runMenuAction(onDecisionEdit)}>调整决策</button>
                <button type="button" onClick={() => runMenuAction(onFactsOpen)}>基础资料</button>
                <button type="button" onClick={() => runMenuAction(onFactEdit)}>编辑基础资料</button>
                <button type="button" onClick={() => runMenuAction(onSubscriptionOpen)}>创建/更新订阅</button>
                <button type="button" onClick={() => runMenuAction(onValidityExtend)}>延长有效期</button>
                <Link
                  className="watchtower-actions-menu__item"
                  onClick={() => closeActionsMenu()}
                  to={`/asset-decisions?view=needs_decision&renew_within_days=30&vps_id=${encodeURIComponent(vpsId)}`}
                >
                  组合决策
                </Link>
                <p className="vps-detail-actions-menu__group">运行</p>
                <button type="button" onClick={() => runMenuAction(onMonitoringEvidence)}>监控观测</button>
                <button type="button" onClick={() => runMenuAction(onMonitoringAgent)}>接入/升级 agent</button>
                <button type="button" onClick={() => runMenuAction(onMonitoringLink)}>关联已有监控实例</button>
                <p className="vps-detail-actions-menu__group">关联</p>
                <button type="button" onClick={() => runMenuAction(onTimelineOpen)}>资产历史</button>
                <button type="button" onClick={() => runMenuAction(onServicesOpen)}>服务</button>
                <button type="button" onClick={() => runMenuAction(onDomainsOpen)}>域名</button>
                <button type="button" onClick={() => runMenuAction(onServiceCreate)}>新增服务</button>
                <button type="button" onClick={() => runMenuAction(onDomainCreate)}>新增域名</button>
                <button type="button" onClick={() => runMenuAction(onExperienceLog)}>记录经验</button>
                <p className="vps-detail-actions-menu__group">生命周期</p>
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
          </div>
          )}

        </div>
      </section>

      <div className="vps-detail-route-nav">
        <VPSDetailSectionNav />
      </div>

      <div className="vps-detail-workspace__lead">
        <section
          id="vps-section-identity"
          className="vps-detail-workspace__section vps-detail-workspace__region--facts"
          aria-labelledby="vps-section-identity-title"
        >
          <h2 id="vps-section-identity-title">资产信息</h2>
          <VPSFactList facts={legacyOverviewFactRows(identityFacts)} ariaLabel="VPS 综合基础信息" />
        </section>

        <section
          id="vps-section-ops"
          className="vps-detail-workspace__section vps-detail-workspace__region--billing"
          aria-label="当前判断"
        >
          <div className="vps-detail-workspace__section-head">
            <h2>订阅与续费</h2>
            <div className="vps-detail-workspace__section-actions">
              {subscriptionRelated ? relatedTitle(subscriptionRelated) : (
                <Link className="text-link" to={`/subscriptions?vps_id=${encodeURIComponent(vpsId)}`}>查看订阅列表</Link>
              )}
              {subscriptionRelated ? relatedQuickActions(subscriptionRelated) : null}
            </div>
          </div>
          <VPSSubscriptionOpsBody
            decisionLabel={decisionRow?.value || '—'}
            {...(primarySubscription ? { primary: primarySubscription } : {})}
            extras={extraSubscriptions}
            loading={recordsPending && subscriptions.length === 0}
            error={subscriptionsError}
            empty={!recordsPending
              ? <p className="vps-detail-resource-group__empty">{renewalStatusRow?.value}</p>
              : null}
            plannedCancellation={plannedCancellation}
            {...(plannedCancellation ? { cancellationPlanLabel: overviewLifecycleLabel('to_cancel') } : {})}
            trailing={<AttentionList items={model.judgement.attentionItems} renderAction={renderOpsAttentionAction} />}
          />
        </section>
      </div>
      <section id="vps-section-monitoring" className="vps-detail-workspace__section vps-detail-overview__monitoring" aria-labelledby="vps-section-monitoring-title">
        <div className="vps-detail-workspace__section-head">
          <h2 id="vps-section-monitoring-title">运行观测</h2>
        </div>
        <VPSObservationRows
          ariaLabel="监控关联"
          rows={[{
            key: 'monitoring',
            project: '监控关联',
            conclusion: monitoringConclusion,
            conclusionTone: monitoringHealth === '正常'
              ? 'ok'
              : monitoringHealth === '关注'
                ? 'notice'
                : monitoringHealth === '告警' || monitoringHealth === '严重'
                  ? 'alert'
                  : monitoringRelated?.tone === 'normal'
                    ? 'ok'
                    : monitoringRelated?.tone === 'notice'
                      ? 'notice'
                      : monitoringRelated?.tone === 'alert' || monitoringRelated?.tone === 'critical'
                        ? 'alert'
                        : 'unknown',
            description: monitoringDescription,
            time: model.monitoringFreshness?.lastHeartbeatAt ?? null,
            action: (
              <Button type="button" size="sm" variant="ghost" onClick={onMonitoringEvidence}>
                {monitoringCount > 0 ? '查看实例' : '查看关联状态'}
              </Button>
            ),
          }]}
        />
        {monitoringInstances.length > 0 ? (
          <VPSDetailResourceList
            state={readyResourceState(monitoringInstances, null)}
            groupLabel=""
            loadingLabel=""
            emptyLabel=""
            retryLabel="监控实例"
            getKey={(item) => item.monitoring_instance_id}
            getName={monitoringInstanceName}
            getStatus={(item) => item.current_health_status}
            getAddress={(item) => {
              const region = [item.region, item.city].filter(Boolean).join(' · ')
              return region
            }}
            getDetailsHref={(item) => monitoringInstanceDetailHref(vpsId, item.monitoring_instance_id)}
            detailsLabel="查看监控实例"
          />
        ) : null}
        {monitoringRelated ? relatedQuickActions({
          ...monitoringRelated,
          quickActions: monitoringRelated.quickActions.filter((action) => action.kind !== 'link'),
        }) : null}

        {model.monitoringFreshness || model.ipOverview.observedAt ? (
          <dl className="vps-detail-overview__freshness" aria-label="观测时间">
            {model.monitoringFreshness ? (
              <>
                <div><dt>最近心跳</dt><dd><Timestamp value={model.monitoringFreshness.lastHeartbeatAt} mode="absolute" /></dd></div>
                <div><dt>最近同步</dt><dd><Timestamp value={model.monitoringFreshness.lastSyncAt} mode="absolute" /></dd></div>
              </>
            ) : null}
            {model.ipOverview.observedAt ? (
              <div><dt>IP 观测</dt><dd><Timestamp value={model.ipOverview.observedAt} mode="absolute" /></dd></div>
            ) : null}
          </dl>
        ) : null}

        <AttentionList items={model.monitoringAttentionItems} renderAction={renderMonitoringAttentionAction} />
        <VPSIPQualitySection vpsId={vpsId} report={ipQuality} error={ipQualityError} />
      </section>

      <section
        id="vps-section-relations"
        className="vps-detail-workspace__section"
        aria-labelledby="vps-section-relations-title"
      >
        <div className="vps-detail-workspace__section-head">
          <h2 id="vps-section-relations-title">服务与域名</h2>
          <div className="vps-detail-workspace__section-actions">
            <Button type="button" size="sm" variant="secondary" onClick={onServicesOpen}>查看服务</Button>
            {READ_ONLY_PREVIEW ? null : (
              <Button type="button" size="sm" variant="ghost" onClick={onServiceCreate}>新增服务</Button>
            )}
            <Button type="button" size="sm" variant="secondary" onClick={onDomainsOpen}>查看域名</Button>
            {READ_ONLY_PREVIEW ? null : (
              <Button type="button" size="sm" variant="ghost" onClick={onDomainCreate}>新增域名</Button>
            )}
          </div>
        </div>
        <div className="vps-detail-workspace__resource-cols">
          <VPSDetailResourceList
            state={readyResourceState(services, null, recordsPending)}
            groupLabel="服务"
            loadingLabel="正在加载服务…"
            emptyLabel="暂无服务"
            retryLabel="服务"
            getKey={(item) => item.service_id}
            getName={serviceResourceName}
            getType={serviceResourceType}
            getStatus={serviceResourceStatus}
            getAddress={(item) => item.url.trim()}
            getSummary={(item) => item.port != null ? `端口 ${item.port}` : ''}
            getCopyValue={(item) => item.url}
            getCopyLabel={() => '入口'}
            getHref={(item) => httpHref(item.url)}
            onOpenDetails={onServicesOpen}
          />
          <VPSDetailResourceList
            state={readyResourceState(domains, null, recordsPending)}
            groupLabel="域名"
            loadingLabel="正在加载域名…"
            emptyLabel="暂无域名"
            retryLabel="域名"
            getKey={(item) => item.domain_id}
            getName={domainResourceName}
            getStatus={domainResourceStatus}
            getSummary={domainResourceExtra}
            onOpenDetails={onDomainsOpen}
          />
        </div>
      </section>

      <section
        id="vps-section-activity"
        className="vps-detail-workspace__section"
        aria-labelledby="vps-section-activity-title"
      >
        <div className="vps-detail-workspace__section-head">
          <h2 id="vps-section-activity-title">最近活动</h2>
          <Button type="button" size="sm" variant="ghost" onClick={onTimelineOpen}>查看全部</Button>
        </div>
        {recordsPending ? (
          <p className="vps-overview-recent__empty">正在加载最近活动…</p>
        ) : model.recentActivity.length === 0 ? (
          <p className="vps-overview-recent__empty">暂无最近活动</p>
        ) : (
          <ol className="vps-overview-recent__list">
            {model.recentActivity.map((item) => {
              const visible = overviewSameAssetActionTitle(item.summary, model.title)
              return (
                <li key={item.key} className="vps-overview-recent__item">
                  <span className="vps-overview-recent__marker" aria-hidden="true" />
                  <div className="vps-overview-recent__item-main">
                    <p
                      className="vps-overview-recent__item-title"
                      {...(visible !== item.summary ? { title: item.summary, 'aria-label': item.summary } : {})}
                    >
                      {visible}
                    </p>
                    <p className="vps-overview-recent__meta">
                      <span className="vps-overview-recent__clock">
                        <span className="vps-overview-recent__clock-label">事件时间</span>
                        {' '}
                        <Timestamp value={item.date} mode="absolute" />
                      </span>
                      <span className="vps-overview-recent__source">{item.kind}</span>
                    </p>
                  </div>
                </li>
              )
            })}
          </ol>
        )}
      </section>
    </>
  )
}
