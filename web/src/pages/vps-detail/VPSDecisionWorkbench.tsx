import { Link } from 'react-router-dom'

import { Badge, Button, Hostname, MonoDigits, Timestamp } from '../../components/atoms'
import { formatDate, formatMoney, formatOptional } from '../../lib/format'
import type { SubscriptionRecord, VPSAssetDetail, VPSTimeline } from '../../lib/types'
import {
  buildVPSQualityIssues,
  daysUntilDate,
  renewalLabel,
  renewalTimingLabel,
  subscriptionStatusLabel,
} from '../assetPageUtils'
import { HealthBadge, RenewalBadge } from '../assetPageBadges'

type VPSDecisionWorkbenchProps = {
  detail: VPSAssetDetail
  timeline: VPSTimeline
  primarySubscription: SubscriptionRecord | null
  subscriptionLoadFailed: boolean
  servicesCount: number
  domainsCount: number
  onDecisionEdit: () => void
  onExperienceLog: () => void
  onNodeLink: () => void
}

type WorkbenchMetricProps = {
  label: string
  value: string
  meta: string
  tone?: 'normal' | 'notice' | 'alert' | 'critical' | 'neutral'
}

function latestDecisionReason(timeline: VPSTimeline): string {
  return timeline.renewal_decisions[0]?.reason || '尚未记录决策理由'
}

function renewalTone(
  subscription: SubscriptionRecord | null,
  subscriptionLoadFailed: boolean,
): 'normal' | 'notice' | 'critical' {
  if (subscriptionLoadFailed) return 'notice'
  if (!subscription) return 'critical'
  const days = daysUntilDate(subscription.renew_at)
  if (days != null && days <= 7) return 'critical'
  if (days != null && days <= 30) return 'notice'
  return 'normal'
}

function primaryNode(detail: VPSAssetDetail) {
  return detail.node_links[0] ?? null
}

function WorkbenchMetric({ label, value, meta, tone = 'neutral' }: WorkbenchMetricProps) {
  return (
    <article className={['vps-workbench-metric', `vps-workbench-metric--${tone}`].join(' ')}>
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{meta}</small>
    </article>
  )
}

export function VPSDecisionWorkbench({
  detail,
  timeline,
  primarySubscription,
  subscriptionLoadFailed,
  servicesCount,
  domainsCount,
  onDecisionEdit,
  onExperienceLog,
  onNodeLink,
}: VPSDecisionWorkbenchProps) {
  const qualityIssues = subscriptionLoadFailed
    ? buildVPSQualityIssues(detail, primarySubscription).filter((issue) => issue.key !== 'missing-subscription')
    : buildVPSQualityIssues(detail, primarySubscription)
  const node = primaryNode(detail)
  const renewalDays = daysUntilDate(primarySubscription?.renew_at)
  const subscriptionTone = renewalTone(primarySubscription, subscriptionLoadFailed)

  return (
    <section className="page-panel vps-workbench" aria-labelledby="vps-workbench-title">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">ASSET DECISION</p>
          <h2 id="vps-workbench-title">资产判断</h2>
        </div>
        <div className="section-heading__actions">
          <Button variant="primary" size="sm" onClick={onDecisionEdit}>调整决策</Button>
          <Button variant="secondary" size="sm" onClick={onExperienceLog}>记录经验</Button>
        </div>
      </div>

      <div className="vps-workbench__grid">
        <article className="vps-workbench-card vps-workbench-card--decision">
          <div className="vps-workbench-card__header">
            <div>
              <p>当前决策</p>
              <h3>{renewalLabel(detail.renewal_decision)}</h3>
            </div>
            <RenewalBadge value={detail.renewal_decision} />
          </div>
          <p className="vps-workbench-card__summary">{latestDecisionReason(timeline)}</p>
          <div className="vps-workbench-card__footer">
            <span>
              <MonoDigits>{timeline.renewal_decisions.length}</MonoDigits> 条决策历史
            </span>
            <Button variant="ghost" size="sm" onClick={onDecisionEdit}>更新判断</Button>
          </div>
        </article>

        <article className={`vps-workbench-card vps-workbench-card--${subscriptionTone}`}>
          <div className="vps-workbench-card__header">
            <div>
              <p>续费与成本</p>
              <h3>
                {primarySubscription
                  ? formatMoney(primarySubscription.monthly_price, primarySubscription.currency)
                  : subscriptionLoadFailed
                    ? '订阅读取失败'
                    : '缺订阅'}
              </h3>
            </div>
            <Badge variant="state" tone={subscriptionTone}>
              {primarySubscription ? subscriptionStatusLabel(primarySubscription.status) : '需核对'}
            </Badge>
          </div>
          <p className="vps-workbench-card__summary">
            {primarySubscription
              ? `${renewalTimingLabel(renewalDays)} · 续费日 ${formatDate(primarySubscription.renew_at)} · ${
                primarySubscription.auto_renew ? '自动续费' : '手工续费'
              }`
              : subscriptionLoadFailed
                ? '当前无法读取订阅列表，页面不把它误判为缺订阅。'
                : '没有 VPS 绑定订阅，无法判断真实续费压力。'}
          </p>
          <div className="vps-workbench-card__footer">
            <span>
              {primarySubscription
                ? `${formatMoney(primarySubscription.price, primarySubscription.currency)} / ${primarySubscription.billing_cycle}`
                : `${timeline.price_histories.length} 条价格历史`}
            </span>
            <Link className="text-link" to="/subscriptions">订阅列表</Link>
          </div>
        </article>

        <article className="vps-workbench-card">
          <div className="vps-workbench-card__header">
            <div>
              <p>Node 证据</p>
              <h3>{node ? node.display_name : '尚未关联'}</h3>
            </div>
            {node ? <HealthBadge value={node.current_health_status} /> : <Badge variant="state" tone="alert">缺证据</Badge>}
          </div>
          <p className="vps-workbench-card__summary">
            {node
              ? `${node.current_active_incident_count} 个活跃异常 · ${formatOptional(node.current_primary_issue_summary)}`
              : '没有 linked Node，无法把资产决策和观测结果对齐。'}
          </p>
          <div className="vps-workbench-card__footer">
            <span>
              {node ? (
                <>
                  心跳 <Timestamp value={node.last_heartbeat_at} mode="relative" />
                </>
              ) : (
                '需要关联 Node'
              )}
            </span>
            <Button variant="ghost" size="sm" onClick={onNodeLink}>补 Node 证据</Button>
          </div>
        </article>

        <article className="vps-workbench-card">
          <div className="vps-workbench-card__header">
            <div>
              <p>上下文</p>
              <h3>
                <MonoDigits>{servicesCount}</MonoDigits> 服务 · <MonoDigits>{domainsCount}</MonoDigits> 域名
              </h3>
            </div>
            <Badge variant="count" tone="neutral">{qualityIssues.length} 个缺口</Badge>
          </div>
          <div className="vps-workbench-quality" aria-label={`${detail.display_name} 资料质量`}>
            {qualityIssues.length > 0 ? (
              qualityIssues.map((issue) => (
                <Badge
                  key={issue.key}
                  variant="info"
                  tone={issue.tone === 'critical' ? 'critical' : issue.tone === 'alert' ? 'alert' : 'notice'}
                >
                  {issue.label}
                </Badge>
              ))
            ) : (
              <Badge variant="info" tone="normal">资料可用</Badge>
            )}
          </div>
          <div className="vps-workbench-card__footer">
            <span>{formatOptional(detail.note) === '—' ? '尚无资产备注' : detail.note}</span>
          </div>
        </article>
      </div>

      <div className="vps-workbench__metrics" aria-label="资产判断关键指标">
        <WorkbenchMetric
          label="Provider"
          value={formatOptional(detail.provider_name)}
          meta={[detail.country, detail.region, detail.city].filter(Boolean).join(' · ') || '位置未确认'}
        />
        <WorkbenchMetric
          label="Access"
          value={detail.ssh_host || detail.ipv4 || detail.ipv6 || '入口缺失'}
          meta={`${detail.ssh_user || 'root'}@${detail.ssh_port}`}
          tone={detail.ssh_host || detail.ipv4 || detail.ipv6 ? 'normal' : 'notice'}
        />
        <WorkbenchMetric
          label="Linked Node"
          value={`${detail.active_node_link_count} 个`}
          meta={node ? `${node.monitoring_status || '未知监控'} · ${node.binding_status || '未知绑定'}` : '缺少观测证据'}
          tone={detail.active_node_link_count > 0 ? 'normal' : 'alert'}
        />
        <WorkbenchMetric
          label="Timeline"
          value={`${timeline.renewal_decisions.length + timeline.experience_logs.length} 条判断记录`}
          meta={`${timeline.price_histories.length} 条价格变化 · ${timeline.spec_snapshots.length} 条规格快照`}
        />
      </div>

      <div className="vps-workbench__access">
        <span>连接入口</span>
        <Hostname>{detail.ssh_host || detail.ipv4 || detail.ipv6 || detail.display_name}</Hostname>
        <MonoDigits>{detail.ssh_port}</MonoDigits>
      </div>
    </section>
  )
}
