import { Link } from 'react-router-dom'

import { Badge, MonoDigits } from '../../components/atoms'
import { formatDate, formatMoney } from '../../lib/format'
import type { SubscriptionRecord, VPSTimeline } from '../../lib/types'
import {
  daysUntilDate,
  renewalTimingLabel,
  subscriptionStatusLabel,
} from '../assetPageUtils'
import { VPSDetailEvidenceCard } from './VPSDetailEvidenceCard'

type VPSRenewalEvidenceSectionProps = {
  vpsID: string
  primarySubscription: SubscriptionRecord | null
  subscriptionLoadFailed: boolean
  subscriptionError: string | null
  timeline: VPSTimeline
}

export function VPSRenewalEvidenceSection({
  vpsID,
  primarySubscription,
  subscriptionLoadFailed,
  subscriptionError,
  timeline,
}: VPSRenewalEvidenceSectionProps) {
  const renewalDays = daysUntilDate(primarySubscription?.renew_at)

  return (
    <section className="page-panel vps-detail-section vps-renewal-evidence" aria-labelledby="vps-renewal-evidence-title">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">RENEWAL EVIDENCE</p>
          <h2 id="vps-renewal-evidence-title">续费与成本证据</h2>
          <p className="section-heading__description">
            只展示当前 VPS scoped subscription 的可信事实；读取失败时保持未知，不推断缺订阅。
          </p>
        </div>
        <span className="section-heading__meta">
          {subscriptionLoadFailed ? '订阅证据未知' : primarySubscription ? '订阅证据已读取' : '订阅证据为空'}
        </span>
        <div className="section-heading__actions">
          <Link className="btn btn--secondary btn--sm" to={`/subscriptions?vps_id=${encodeURIComponent(vpsID)}`}>订阅列表</Link>
          {!primarySubscription && !subscriptionLoadFailed ? (
            <Link className="btn btn--ghost btn--sm" to={`/subscriptions?vps_id=${encodeURIComponent(vpsID)}&create=1`}>创建订阅</Link>
          ) : null}
        </div>
      </div>

      <div className="vps-detail-evidence-grid vps-detail-evidence-grid--renewal">
        {subscriptionLoadFailed ? (
          <VPSDetailEvidenceCard
            label="订阅状态"
            value="订阅读取失败"
            meta={subscriptionError ?? '当前无法判断续费日、月化成本或缺订阅事实。'}
            tone="notice"
          />
        ) : primarySubscription ? (
          <>
            <VPSDetailEvidenceCard
              label="月化成本"
              value={formatMoney(primarySubscription.monthly_price, primarySubscription.currency)}
              meta={`${formatMoney(primarySubscription.price, primarySubscription.currency)} / ${primarySubscription.billing_cycle}`}
              tone={renewalDays != null && renewalDays <= 30 ? 'notice' : 'normal'}
            />
            <VPSDetailEvidenceCard
              label="续费窗口"
              value={renewalTimingLabel(renewalDays)}
              meta={`续费日 ${formatDate(primarySubscription.renew_at)}`}
              tone={renewalDays != null && renewalDays <= 7 ? 'critical' : renewalDays != null && renewalDays <= 30 ? 'notice' : 'normal'}
            />
            <VPSDetailEvidenceCard
              label="续费方式"
              value={primarySubscription.auto_renew ? '自动续费' : '手工续费'}
              meta={primarySubscription.auto_renew_cancelled ? '已取消自动续费' : `订阅状态 ${subscriptionStatusLabel(primarySubscription.status)}`}
              tone={primarySubscription.auto_renew_cancelled ? 'notice' : 'normal'}
            />
            <VPSDetailEvidenceCard
              label="订阅记录"
              value={primarySubscription.subscription_id}
              meta={primarySubscription.note || '无订阅备注'}
            >
              <Badge variant="info" tone="neutral">{primarySubscription.payment_method || '付款方式未记录'}</Badge>
            </VPSDetailEvidenceCard>
          </>
        ) : (
          <VPSDetailEvidenceCard
            label="订阅状态"
            value="缺订阅"
            meta="订阅接口已成功返回空结果，当前缺少真实续费日和月化成本。"
            tone="critical"
          />
        )}

        <VPSDetailEvidenceCard
          label="价格历史"
          value={<><MonoDigits>{timeline.price_histories.length}</MonoDigits> 条</>}
          meta="来自资产历史，不替代当前订阅证据。"
        />
      </div>

      {subscriptionLoadFailed ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          订阅读取失败：{subscriptionError ?? '未知错误'}
        </p>
      ) : null}
    </section>
  )
}
