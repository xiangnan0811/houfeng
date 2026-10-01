import { useId } from 'react'

import { Badge, type BadgeTone } from '../../../../components/atoms'
import { formatMoney } from '../../../../lib/format'
import { lookup, toneOf } from '../evidencePresentation'
import type { SubscriptionCostEvidenceReadModel } from '../evidenceReadModels'

type Props = {
  model: SubscriptionCostEvidenceReadModel
}

const PERIOD_UNIT_LABELS: Record<string, string> = { day: '天', week: '周', month: '月', year: '年' }
const BUDGET_STATUS_LABELS: Record<string, string> = { ok: '正常', warning: '预警', over: '超支', unknown: '未知' }
const BUDGET_STATUS_TONES: Record<string, BadgeTone> = { ok: 'normal', warning: 'notice', over: 'critical' }
const COVERAGE_STATUS_LABELS: Record<string, string> = { complete: '完整', partial: '部分', missing_rate: '缺汇率' }
const PROVIDER_LABELS: Record<string, string> = { identity: '同币种', frankfurter: 'Frankfurter', fixer: 'Fixer' }

function periodText(model: SubscriptionCostEvidenceReadModel): string {
  const unit = lookup(PERIOD_UNIT_LABELS, model.billing_period_unit)
  return model.billing_period_length === 1 ? `每${unit}` : `每 ${model.billing_period_length} ${unit}`
}

export function SubscriptionCostEvidenceRenderer({ model }: Props) {
  const titleId = useId()
  const sameCurrency = model.original_currency === model.base_currency
  const budgetKnown = model.budget_status !== 'unknown' && model.budget_monthly_limit > 0
  return (
    <section className="record-section record-evidence__body" aria-labelledby={titleId}>
      <div className="record-section__head">
        <h2 className="record-section__title" id={titleId}>订阅成本</h2>
        <span className="record-muted mono">{model.subscription_id}</span>
      </div>
      <dl className="record-evidence__tiles">
        <div className="record-evidence__tile">
          <dt>账单金额</dt>
          <dd>
            <strong className="mono">{formatMoney(model.original_amount, model.original_currency)}</strong>
            <span>{periodText(model)}</span>
          </dd>
        </div>
        <div className="record-evidence__tile">
          <dt>折算金额</dt>
          <dd>
            <strong className="mono">{formatMoney(model.base_amount, model.base_currency)}</strong>
            <span>
              {sameCurrency ? '同币种' : (
                <>
                  汇率 <span className="mono">{Number(model.conversion_rate.toFixed(4))}</span>
                  {' · '}{lookup(PROVIDER_LABELS, model.conversion_provider)} {model.rate_date}
                  {model.rate_stale ? ' · 汇率过期' : ''}
                </>
              )}
            </span>
          </dd>
        </div>
        <div className="record-evidence__tile">
          <dt>{model.budget_month} 预算</dt>
          {budgetKnown ? (
            <dd>
              <strong className="mono">
                {formatMoney(model.budget_actual_spend, model.budget_currency)}
                <span className="record-evidence__tile-limit"> / {formatMoney(model.budget_monthly_limit, model.budget_currency)}</span>
              </strong>
              <progress
                className="record-evidence__budget"
                value={Math.min(model.budget_actual_spend, model.budget_monthly_limit)}
                max={model.budget_monthly_limit}
                aria-label="预算使用"
              />
              <span>
                <Badge variant="info" tone={toneOf(BUDGET_STATUS_TONES, model.budget_status)}>
                  {lookup(BUDGET_STATUS_LABELS, model.budget_status)}
                </Badge>
                {' '}预警线 {model.budget_warning_pct}%
              </span>
            </dd>
          ) : (
            // 缺汇率或未设预算时没有可信比例，只说明无法判定，不画进度。
            <dd>
              <strong>无法判定</strong>
              <span>{model.missing_rate_count > 0 ? '有订阅缺少汇率' : '未设置月度预算'}</span>
            </dd>
          )}
        </div>
        <div className="record-evidence__tile">
          <dt>覆盖</dt>
          <dd>
            <strong className="mono">{model.covered_days}/{model.total_days} 天</strong>
            <span>
              {lookup(COVERAGE_STATUS_LABELS, model.coverage_status)}
              {model.missing_rate_count > 0 ? ` · ${model.missing_rate_count} 个订阅缺汇率` : ''}
            </span>
          </dd>
        </div>
      </dl>
    </section>
  )
}
