import { Link } from 'react-router-dom'

import { Badge, Button, Hostname, MonoDigits, StatusGlyph, Timestamp } from '../../components/atoms'
import { formatDate, formatMoney, formatOptional } from '../../lib/format'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  CancellationPreview,
  SubscriptionRecord,
  VPSAssetDetail,
  VPSNodeSummary,
  VPSTimeline,
} from '../../lib/types'
import { renewalTimingLabel, subscriptionStatusLabel } from '../assetPageUtils'
import { HealthBadge } from '../assetPageBadges'
import { buildVPSDecisionModel, countDecisionRecords, toneToGlyphState } from './vpsDecisionModel'

type VPSDecisionBoardProps = {
  detail: VPSAssetDetail
  timeline: VPSTimeline
  primarySubscription: SubscriptionRecord | null
  subscriptionLoadFailed: boolean
  subscriptionError: string | null
  services: AssetServiceRecord[]
  domains: AssetDomainRecord[]
  factNotice: string | null
  factError: string | null
  linkFeedback: string | null
  linkFeedbackIsError: boolean
  serviceNotice: string | null
  serviceError: string | null
  domainNotice: string | null
  domainError: string | null
  experienceNotice: string | null
  lifecycleNotice: string | null
  lifecycleError: string | null
  cancellationPreview: CancellationPreview | null
  cancellationPreviewError: string | null
  onDecisionEdit: () => void
  onCancellationOpen: () => void
  onFactEdit: () => void
  onExperienceLog: () => void
  onNodeLink: () => void
  onOpenFacts: () => void
  onOpenNodeEvidence: () => void
  onOpenServices: () => void
  onOpenDomains: () => void
  onOpenTimeline: () => void
}

type FeedbackItem = { message: string; error: boolean }

function buildFeedback(props: VPSDecisionBoardProps): FeedbackItem[] {
  return [
    props.factError
      ? { message: props.factError, error: true }
      : props.factNotice
        ? { message: props.factNotice, error: false }
        : null,
    props.linkFeedback ? { message: props.linkFeedback, error: props.linkFeedbackIsError } : null,
    props.serviceError
      ? { message: props.serviceError, error: true }
      : props.serviceNotice
        ? { message: props.serviceNotice, error: false }
        : null,
    props.domainError
      ? { message: props.domainError, error: true }
      : props.domainNotice
        ? { message: props.domainNotice, error: false }
        : null,
    props.experienceNotice ? { message: props.experienceNotice, error: false } : null,
    props.lifecycleError
      ? { message: props.lifecycleError, error: true }
      : props.lifecycleNotice
        ? { message: props.lifecycleNotice, error: false }
        : null,
  ].filter((item): item is FeedbackItem => item !== null)
}

function serviceDomainSummary(services: AssetServiceRecord[], domains: AssetDomainRecord[]): string {
  const svc = services.length > 0 ? services.slice(0, 2).map((s) => s.name).join(' · ') : '服务上下文待补录'
  const dom = domains.length > 0 ? domains.slice(0, 2).map((d) => d.domain_name).join(' · ') : '域名上下文待补录'
  return `${svc}；${dom}`
}

function nodeEvidenceSummary(node: VPSNodeSummary | null): string {
  if (!node) return '尚未关联 Node，缺少心跳、健康与异常证据。'
  return `${node.current_active_incident_count} 个活跃异常 · ${formatOptional(node.current_primary_issue_summary)}`
}

function latestHistorySummary(timeline: VPSTimeline): string {
  const experience = timeline.experience_logs[0]
  if (experience) return experience.summary
  const decision = timeline.renewal_decisions[0]
  if (decision) return decision.reason || '最近决策未记录理由'
  const price = timeline.price_histories[0]
  if (price) return `${formatMoney(price.from_monthly_price, price.from_currency)} -> ${formatMoney(price.to_monthly_price, price.to_currency)}`
  return '尚无资产历史记录'
}

function isCancellationRelevant(detail: VPSAssetDetail, preview: CancellationPreview | null): boolean {
  return detail.renewal_decision === 'cancel' ||
    detail.renewal_decision === 'auto_renew_cancelled' ||
    detail.lifecycle_status === 'to_cancel' ||
    detail.lifecycle_status === 'cancelled' ||
    Boolean(preview && ((preview.warnings ?? []).length > 0 || (preview.blockers ?? []).length > 0))
}

function lifecycleCoordinationTitle(detail: VPSAssetDetail, preview: CancellationPreview | null, error: string | null): string {
  if (error && !preview) return '取消上下文暂不可用'
  if (!preview) return '正在读取取消上下文'
  if ((preview.blockers ?? []).length > 0) return '取消动作存在阻塞'
  if ((preview.warnings ?? []).length > 0) return '需要处理资产联动'
  if (isCancellationRelevant(detail, preview)) return '取消状态已纳入工作台'
  return '资产联动状态正常'
}

function lifecycleCoordinationSummary(detail: VPSAssetDetail, preview: CancellationPreview | null, error: string | null): string {
  void detail
  if (error && !preview) return error
  if (!preview) return '取消/退役工作台会统一展示订阅、VPS、Node、服务、域名与 Target 影响范围。'
  const blockers = preview.blockers ?? []
  const warnings = preview.warnings ?? []
  const subscriptions = preview.subscriptions ?? []
  const nodeLinks = preview.node_links ?? []
  const targetLinks = preview.target_links ?? []
  if (blockers.length > 0) return blockers[0]
  if (warnings.length > 0) return warnings[0]
  const activeSubscriptions = subscriptions.filter((impact) => impact.record.status === 'active').length
  return `订阅 ${activeSubscriptions}/${subscriptions.length} active · Node ${nodeLinks.length} · Target ${targetLinks.length}，普通 CRUD 不会隐式联动。`
}

export function VPSDecisionBoard(props: VPSDecisionBoardProps) {
  const {
    detail,
    timeline,
    primarySubscription,
    subscriptionLoadFailed,
    subscriptionError,
    services,
    domains,
    cancellationPreview,
    cancellationPreviewError,
    onDecisionEdit,
    onCancellationOpen,
    onFactEdit,
    onExperienceLog,
    onNodeLink,
    onOpenFacts,
    onOpenNodeEvidence,
    onOpenServices,
    onOpenDomains,
    onOpenTimeline,
  } = props
  const { node, renewalDays, subscriptionTone, nextAction, evidenceItems } = buildVPSDecisionModel({
    detail,
    timeline,
    primarySubscription,
    subscriptionLoadFailed,
    subscriptionError,
    servicesCount: services.length,
    domainsCount: domains.length,
    onDecisionEdit,
    onFactEdit,
    onExperienceLog,
    onNodeLink,
  })
  const feedbackItems = buildFeedback(props)
  const accessHost = detail.ssh_host || detail.ipv4 || detail.ipv6 || detail.display_name
  const lifecycleAttention = isCancellationRelevant(detail, cancellationPreview)

  return (
    <section className="page-panel vps-decision-board" aria-labelledby="vps-decision-board-title">
      <div className="section-heading section-heading--inline">
        <div>
          <p className="section-heading__eyebrow">ASSET DECISION</p>
          <h2 id="vps-decision-board-title">资产判断</h2>
          <p className="section-heading__description">先看下一步动作，再核对续费、Node、服务域名与历史证据。</p>
        </div>
        <div className="section-heading__actions">
          <Button variant="primary" size="sm" onClick={onDecisionEdit}>调整决策</Button>
        </div>
      </div>

      {feedbackItems.length > 0 ? (
        <div className="vps-decision-board__feedback" aria-label="VPS 操作反馈">
          {feedbackItems.map((item) => (
            <p
              key={item.message}
              className={['asset-operation-feedback', item.error && 'asset-operation-feedback--error']
                .filter(Boolean)
                .join(' ')}
              role={item.error ? 'alert' : 'status'}
            >
              {item.message}
            </p>
          ))}
        </div>
      ) : null}

      <div className={`vps-decision-board__lead vps-decision-board__lead--${nextAction.tone}`}>
        <article className="vps-decision-next">
          <p>下一步动作</p>
          <h3>{nextAction.title}</h3>
          <span>{nextAction.summary}</span>
          <div className="vps-decision-next__actions">
            {nextAction.to && nextAction.linkLabel ? (
              <Link className="btn sm primary" to={nextAction.to}>{nextAction.linkLabel}</Link>
            ) : nextAction.onAction && nextAction.buttonLabel ? (
              <Button variant="primary" size="sm" onClick={nextAction.onAction}>{nextAction.buttonLabel}</Button>
            ) : (
              <span className="vps-decision-next__hint">在右上角操作菜单处理</span>
            )}
          </div>
        </article>
        <div className="vps-decision-evidence" aria-label="资产判断证据状态">
          {evidenceItems.map((item) => (
            <article key={item.label} className={`vps-decision-evidence__item vps-decision-evidence__item--${item.tone}`}>
              <div className="vps-decision-evidence__label">
                <span aria-hidden="true"><StatusGlyph state={toneToGlyphState(item.tone)} size="sm" /></span>
                <span>{item.label}</span>
              </div>
              <strong>{item.value}</strong>
              <small>{item.meta}</small>
            </article>
          ))}
        </div>
      </div>

      <div className="vps-decision-board__coordination">
        <div className="vps-decision-board__coordination-head">
          <div>
            <p className="asset-cancel-workbench__eyebrow">LIFECYCLE COORDINATION</p>
            <h3>{lifecycleCoordinationTitle(detail, cancellationPreview, cancellationPreviewError)}</h3>
            <span>{lifecycleCoordinationSummary(detail, cancellationPreview, cancellationPreviewError)}</span>
          </div>
          <Badge variant="state" tone={lifecycleAttention ? 'notice' : 'normal'}>
            {lifecycleAttention ? '需核对' : '已同步'}
          </Badge>
        </div>
        <div className="vps-decision-board__coordination-metrics" aria-label="生命周期影响范围">
          <span>订阅 <MonoDigits>{cancellationPreview?.subscriptions?.length ?? 0}</MonoDigits></span>
          <span>Node <MonoDigits>{cancellationPreview?.node_links?.length ?? detail.active_node_link_count}</MonoDigits></span>
          <span>Target <MonoDigits>{cancellationPreview?.target_links?.length ?? 0}</MonoDigits></span>
        </div>
        <div className="vps-decision-board__coordination-actions">
          <Button variant={lifecycleAttention ? 'danger' : 'secondary'} size="sm" onClick={onCancellationOpen}>
            打开取消/退役工作台
          </Button>
        </div>
      </div>

      <div className="vps-decision-board__grid">
        <article className={`vps-decision-card vps-decision-card--${subscriptionTone}`}>
          <div className="vps-decision-card__header">
            <div>
              <p>续费与成本</p>
              <h3>
                {primarySubscription
                  ? renewalTimingLabel(renewalDays)
                  : subscriptionLoadFailed
                    ? '订阅读取失败'
                    : '缺订阅'}
              </h3>
            </div>
            <Badge variant="state" tone={subscriptionTone}>
              {primarySubscription ? subscriptionStatusLabel(primarySubscription.status) : '需核对'}
            </Badge>
          </div>
          <p className="vps-decision-card__summary">
            {primarySubscription
              ? `${formatMoney(primarySubscription.monthly_price, primarySubscription.currency)} / 月 · 续费日 ${formatDate(primarySubscription.renew_at)} · ${primarySubscription.auto_renew ? '自动续费' : '手工续费'}`
              : subscriptionLoadFailed
                ? subscriptionError ?? '当前无法读取订阅，页面不把它误判为缺订阅。'
                : '订阅接口已成功返回空结果，需要补录成本与续费日。'}
          </p>
          <div className="vps-decision-card__footer">
            <Link className="text-link" to={`/subscriptions?vps_id=${encodeURIComponent(detail.vps_id)}`}>订阅列表</Link>
            {!primarySubscription && !subscriptionLoadFailed ? (
              <Link className="text-link" to={`/subscriptions?vps_id=${encodeURIComponent(detail.vps_id)}&create=1`}>创建订阅</Link>
            ) : null}
          </div>
        </article>

        <article className="vps-decision-card">
          <div className="vps-decision-card__header">
            <div>
              <p>Node 证据</p>
              <h3>{node ? node.display_name : '尚未关联'}</h3>
            </div>
            {node ? <HealthBadge value={node.current_health_status} /> : <Badge variant="state" tone="alert">缺证据</Badge>}
          </div>
          <p className="vps-decision-card__summary">{nodeEvidenceSummary(node)}</p>
          <div className="vps-decision-card__footer">
            {node ? (
              <span>心跳 <Timestamp value={node.last_heartbeat_at} mode="relative" /></span>
            ) : (
              <span>需要关联 Node</span>
            )}
            <Button variant="ghost" size="sm" onClick={onOpenNodeEvidence}>查看 Node 详情</Button>
          </div>
        </article>

        <article className="vps-decision-card">
          <div className="vps-decision-card__header">
            <div>
              <p>服务与域名</p>
              <h3><MonoDigits>{services.length}</MonoDigits> 服务 · <MonoDigits>{domains.length}</MonoDigits> 域名</h3>
            </div>
            <Badge variant="count" tone={services.length + domains.length > 0 ? 'normal' : 'notice'}>上下文</Badge>
          </div>
          <p className="vps-decision-card__summary">{serviceDomainSummary(services, domains)}</p>
          <div className="vps-decision-card__footer">
            <Button variant="ghost" size="sm" onClick={onOpenServices}>服务详情</Button>
            <Button variant="ghost" size="sm" onClick={onOpenDomains}>域名详情</Button>
          </div>
        </article>

        <article className="vps-decision-card">
          <div className="vps-decision-card__header">
            <div>
              <p>最近历史</p>
              <h3><MonoDigits>{countDecisionRecords(timeline)}</MonoDigits> 条判断记录</h3>
            </div>
            <Badge variant="count" tone="neutral">Timeline</Badge>
          </div>
          <p className="vps-decision-card__summary">{latestHistorySummary(timeline)}</p>
          <div className="vps-decision-card__footer">
            <Button variant="ghost" size="sm" onClick={onOpenTimeline}>查看资产历史</Button>
          </div>
        </article>
      </div>

      <div className="vps-decision-board__facts">
        <div className="vps-decision-board__fact-main">
          <span>资料摘要</span>
          <strong>{formatOptional(detail.product_name)} · {formatOptional(detail.datacenter)}</strong>
          <small>
            <Hostname>{accessHost}</Hostname>
            {' · '}
            <MonoDigits>{detail.ssh_port}</MonoDigits>
            {' · '}
            {formatOptional(detail.os_name)}
          </small>
        </div>
        <Button variant="ghost" size="sm" onClick={onOpenFacts}>查看基础资料</Button>
      </div>
    </section>
  )
}
