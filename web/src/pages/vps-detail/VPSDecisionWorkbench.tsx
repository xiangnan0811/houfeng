import { Link } from 'react-router-dom'

import { Badge, Button, Hostname, MonoDigits, StatusGlyph, Timestamp } from '../../components/atoms'
import { formatDate, formatMoney, formatOptional } from '../../lib/format'
import type { SubscriptionRecord, VPSAssetDetail, VPSTimeline } from '../../lib/types'
import {
  renewalLabel,
  renewalTimingLabel,
  subscriptionStatusLabel,
} from '../assetPageUtils'
import { HealthBadge, RenewalBadge } from '../assetPageBadges'
import {
  buildVPSDecisionModel,
  countDecisionRecords,
  latestDecisionReason,
  toneToGlyphState,
  type WorkbenchTone,
} from './vpsDecisionModel'

type VPSDecisionWorkbenchProps = {
  detail: VPSAssetDetail
  timeline: VPSTimeline
  primarySubscription: SubscriptionRecord | null
  subscriptionLoadFailed: boolean
  subscriptionError: string | null
  servicesCount: number
  domainsCount: number
  onDecisionEdit: () => void
  onFactEdit: () => void
  onExperienceLog: () => void
  onNodeLink: () => void
}

type WorkbenchMetricProps = {
  label: string
  value: string
  meta: string
  tone?: WorkbenchTone
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
  subscriptionError,
  servicesCount,
  domainsCount,
  onDecisionEdit,
  onFactEdit,
  onExperienceLog,
  onNodeLink,
}: VPSDecisionWorkbenchProps) {
  const {
    qualityIssues,
    node,
    renewalDays,
    subscriptionTone,
    nextAction,
    evidenceItems,
  } = buildVPSDecisionModel({
    detail,
    timeline,
    primarySubscription,
    subscriptionLoadFailed,
    subscriptionError,
    servicesCount,
    domainsCount,
    onDecisionEdit,
    onFactEdit,
    onExperienceLog,
    onNodeLink,
  })

  return (
    <section className="page-panel vps-workbench" aria-labelledby="vps-workbench-title">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">ASSET DECISION</p>
          <h2 id="vps-workbench-title">资产判断</h2>
          <p className="section-heading__description">先看下一步动作，再核对订阅、Node、服务域名和资料质量证据。</p>
        </div>
        <div className="section-heading__actions">
          <Button variant="primary" size="sm" onClick={onDecisionEdit}>调整决策</Button>
          <Button variant="secondary" size="sm" onClick={onExperienceLog}>记录经验</Button>
        </div>
      </div>

      <div className={`vps-workbench__lead vps-workbench__lead--${nextAction.tone}`}>
        <article className="vps-workbench-next">
          <p>下一步动作</p>
          <h3>{nextAction.title}</h3>
          <span>{nextAction.summary}</span>
          <div className="vps-workbench-next__actions">
            {'onAction' in nextAction ? (
              <Button variant="primary" size="sm" onClick={nextAction.onAction}>
                {nextAction.buttonLabel}
              </Button>
            ) : (
              <Link className="btn btn--primary btn--sm" to={nextAction.to}>
                {nextAction.linkLabel}
              </Link>
            )}
          </div>
        </article>

        <div className="vps-workbench-evidence" aria-label="资产判断证据状态">
          {evidenceItems.map((item) => (
            <article key={item.label} className={`vps-workbench-evidence__item vps-workbench-evidence__item--${item.tone}`}>
              <div className="vps-workbench-evidence__label">
                <span aria-hidden="true">
                  <StatusGlyph state={toneToGlyphState(item.tone)} size="sm" />
                </span>
                <span>{item.label}</span>
              </div>
              <strong>{item.value}</strong>
              <small>{item.meta}</small>
            </article>
          ))}
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
                ? `当前无法读取订阅列表，页面不把它误判为缺订阅。${subscriptionError ? `错误：${subscriptionError}` : ''}`
                : '没有 VPS 绑定订阅，无法判断真实续费压力。'}
          </p>
          <div className="vps-workbench-card__footer">
            <span>
              {primarySubscription
                ? `${formatMoney(primarySubscription.price, primarySubscription.currency)} / ${primarySubscription.billing_cycle}`
                : `${timeline.price_histories.length} 条价格历史`}
            </span>
            <Link className="text-link" to={`/subscriptions?vps_id=${encodeURIComponent(detail.vps_id)}`}>订阅列表</Link>
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
          value={`${countDecisionRecords(timeline)} 条判断记录`}
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
