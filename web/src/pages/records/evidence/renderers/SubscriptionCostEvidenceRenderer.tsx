import { MonoDigits } from '../../../../components/atoms'
import type { SubscriptionCostEvidenceReadModel } from '../evidenceReadModels'

type Props = {
  model: SubscriptionCostEvidenceReadModel
}

export function SubscriptionCostEvidenceRenderer({ model }: Props) {
  return (
    <section className="page-panel evidence-renderer evidence-renderer--cost" aria-label="订阅成本证据">
      <header className="evidence-renderer__header">
        <h3>订阅成本</h3>
        <MonoDigits>{model.subscription_id}</MonoDigits>
      </header>
      <dl className="metadata-list evidence-renderer__facts">
        <div><dt>原币金额</dt><dd><MonoDigits>{model.original_amount} {model.original_currency}</MonoDigits></dd></div>
        <div><dt>基准金额</dt><dd><MonoDigits>{model.base_amount} {model.base_currency}</MonoDigits></dd></div>
        <div><dt>账期</dt><dd>{model.billing_period_length} {model.billing_period_unit}</dd></div>
        <div><dt>预算状态</dt><dd>{model.budget_status}</dd></div>
        <div><dt>覆盖状态</dt><dd>{model.coverage_status}</dd></div>
        <div><dt>覆盖窗口</dt><dd>{model.coverage_start} — {model.coverage_end}</dd></div>
      </dl>
    </section>
  )
}
