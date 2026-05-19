import { Link } from 'react-router-dom'

import { Badge, Button, Hostname, MonoDigits, Timestamp } from '../../components/atoms'
import { formatDate, formatMoney, formatOptional } from '../../lib/format'
import type { AssetDomainRecord, AssetServiceRecord, SubscriptionRecord, VPSAssetDetail, VPSNodeSummary, VPSTimeline } from '../../lib/types'
import { HealthBadge } from '../assetPageBadges'
import { daysUntilDate, renewalTimingLabel, subscriptionStatusLabel } from '../assetPageUtils'
import { countDecisionRecords, latestDecisionReason, primaryNode, renewalTone } from './vpsDecisionModel'

type VPSOperationsSummaryProps = {
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
  onOpenFacts: () => void
  onOpenNodeEvidence: () => void
  onOpenServices: () => void
  onOpenDomains: () => void
  onOpenTimeline: () => void
}

function latestHistorySummary(timeline: VPSTimeline): string {
  const latestExperience = timeline.experience_logs[0]
  if (latestExperience) return latestExperience.summary
  const latestDecision = timeline.renewal_decisions[0]
  if (latestDecision) return latestDecision.reason || '最近决策未记录理由'
  const latestPrice = timeline.price_histories[0]
  if (latestPrice) return `${formatMoney(latestPrice.from_monthly_price, latestPrice.from_currency)} -> ${formatMoney(latestPrice.to_monthly_price, latestPrice.to_currency)}`
  const latestIP = timeline.ip_histories[0]
  if (latestIP) return `${formatOptional(latestIP.from_ipv4)} -> ${formatOptional(latestIP.to_ipv4)}`
  const latestSpec = timeline.spec_snapshots[0]
  if (latestSpec) return latestSpec.product_name || '规格快照已记录'
  return '尚无资产历史记录'
}

function serviceSummary(services: AssetServiceRecord[]): string {
  if (services.length === 0) return '服务上下文待补录'
  return services.slice(0, 2).map((service) => service.name).join(' · ')
}

function domainSummary(domains: AssetDomainRecord[]): string {
  if (domains.length === 0) return '域名上下文待补录'
  return domains.slice(0, 2).map((domain) => domain.domain_name).join(' · ')
}

function nodeSummary(node: VPSNodeSummary | null): string {
  if (!node) return '尚未关联 Node，缺少运行侧证据。'
  return `${node.current_active_incident_count} 个活跃异常 · ${formatOptional(node.current_primary_issue_summary)}`
}

export function VPSOperationsSummary({
  detail,
  timeline,
  primarySubscription,
  subscriptionLoadFailed,
  subscriptionError,
  services,
  domains,
  factNotice,
  factError,
  linkFeedback,
  linkFeedbackIsError,
  serviceNotice,
  serviceError,
  domainNotice,
  domainError,
  experienceNotice,
  lifecycleNotice,
  lifecycleError,
  onOpenFacts,
  onOpenNodeEvidence,
  onOpenServices,
  onOpenDomains,
  onOpenTimeline,
}: VPSOperationsSummaryProps) {
  const node = primaryNode(detail)
  const renewalDays = daysUntilDate(primarySubscription?.renew_at)
  const subscriptionTone = renewalTone(primarySubscription, subscriptionLoadFailed)
  const feedbackItems = [
    factError ? { message: factError, error: true } : factNotice ? { message: factNotice, error: false } : null,
    linkFeedback ? { message: linkFeedback, error: linkFeedbackIsError } : null,
    serviceError ? { message: serviceError, error: true } : serviceNotice ? { message: serviceNotice, error: false } : null,
    domainError ? { message: domainError, error: true } : domainNotice ? { message: domainNotice, error: false } : null,
    experienceNotice ? { message: experienceNotice, error: false } : null,
    lifecycleError ? { message: lifecycleError, error: true } : lifecycleNotice ? { message: lifecycleNotice, error: false } : null,
  ].filter((item): item is { message: string; error: boolean } => item !== null)

  return (
    <section className="vps-operations-summary" aria-labelledby="vps-operations-summary-title">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">OPERATIONS CONTEXT</p>
          <h2 id="vps-operations-summary-title">判断证据摘要</h2>
          <p className="section-heading__description">
            主页面只保留能支撑续费/保留判断的证据摘要；全量 Node、服务、域名、历史和低价值资料进入详情入口。
          </p>
        </div>
      </div>

      {feedbackItems.length > 0 ? (
        <div className="vps-operations-summary__feedback" aria-label="VPS 操作反馈">
          {feedbackItems.map((item) => (
            <p
              key={item.message}
              className={[
                'asset-operation-feedback',
                item.error && 'asset-operation-feedback--error',
              ].filter(Boolean).join(' ')}
              role={item.error ? 'alert' : 'status'}
            >
              {item.message}
            </p>
          ))}
        </div>
      ) : null}

      <div className="vps-operations-summary__grid">
        <article className={`vps-operations-card vps-operations-card--${subscriptionTone}`}>
          <div className="vps-operations-card__header">
            <div>
              <p>续费窗口</p>
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
          <p>
            {primarySubscription
              ? `${formatMoney(primarySubscription.monthly_price, primarySubscription.currency)} / 月 · 续费日 ${formatDate(primarySubscription.renew_at)}`
              : subscriptionLoadFailed
                ? subscriptionError ?? '当前无法判断续费日和月化成本。'
                : '订阅接口已成功返回空结果，需要补录成本与续费日。'}
          </p>
          <div className="vps-operations-card__footer">
            <Link className="text-link" to={`/subscriptions?vps_id=${encodeURIComponent(detail.vps_id)}`}>订阅列表</Link>
            {!primarySubscription && !subscriptionLoadFailed ? (
              <Link className="text-link" to={`/subscriptions?vps_id=${encodeURIComponent(detail.vps_id)}&create=1`}>创建订阅</Link>
            ) : null}
          </div>
        </article>

        <article className="vps-operations-card">
          <div className="vps-operations-card__header">
            <div>
              <p>Node 证据</p>
              <h3>{node ? node.display_name : '尚未关联'}</h3>
            </div>
            {node ? <HealthBadge value={node.current_health_status} /> : <Badge variant="state" tone="alert">缺证据</Badge>}
          </div>
          <p>{nodeSummary(node)}</p>
          <div className="vps-operations-card__footer">
            {node ? (
              <span>心跳 <Timestamp value={node.last_heartbeat_at} mode="relative" /></span>
            ) : (
              <span>需要关联 Node</span>
            )}
            <Button variant="ghost" size="sm" onClick={onOpenNodeEvidence}>查看 Node 详情</Button>
          </div>
        </article>

        <article className="vps-operations-card">
          <div className="vps-operations-card__header">
            <div>
              <p>服务与域名</p>
              <h3><MonoDigits>{services.length}</MonoDigits> 服务 · <MonoDigits>{domains.length}</MonoDigits> 域名</h3>
            </div>
            <Badge variant="count" tone={services.length + domains.length > 0 ? 'normal' : 'notice'}>
              上下文
            </Badge>
          </div>
          <p>{serviceSummary(services)}；{domainSummary(domains)}</p>
          <div className="vps-operations-card__footer">
            <Button variant="ghost" size="sm" onClick={onOpenServices}>服务详情</Button>
            <Button variant="ghost" size="sm" onClick={onOpenDomains}>域名详情</Button>
          </div>
        </article>

        <article className="vps-operations-card">
          <div className="vps-operations-card__header">
            <div>
              <p>最近历史</p>
              <h3><MonoDigits>{countDecisionRecords(timeline)}</MonoDigits> 条判断记录</h3>
            </div>
            <Badge variant="count" tone="neutral">Timeline</Badge>
          </div>
          <p>{latestHistorySummary(timeline)} · {latestDecisionReason(timeline)}</p>
          <div className="vps-operations-card__footer">
            <Button variant="ghost" size="sm" onClick={onOpenTimeline}>查看资产历史</Button>
          </div>
        </article>
      </div>

      <div className="vps-operations-summary__facts">
        <div className="vps-operations-summary__fact-main">
          <span>资料摘要</span>
          <strong>{formatOptional(detail.product_name)} · {formatOptional(detail.datacenter)}</strong>
          <small>
            <Hostname>{detail.ssh_host || detail.ipv4 || detail.ipv6 || detail.display_name}</Hostname>
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
