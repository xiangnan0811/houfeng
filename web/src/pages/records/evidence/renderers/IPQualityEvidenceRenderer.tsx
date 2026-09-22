import { MonoDigits } from '../../../../components/atoms'
import type { IPQualityEvidenceReadModel } from '../evidenceReadModels'

type Props = {
  model: IPQualityEvidenceReadModel
}

const REPORT_STATUS_LABELS: Record<IPQualityEvidenceReadModel['status'], string> = {
  success: '成功',
  partial: '部分',
}

const SOURCE_STATUS_LABELS: Record<IPQualityEvidenceReadModel['providers'][number]['status'], string> = {
  success: '成功',
  failure: '失败',
  skipped: '已跳过',
  not_configured: '未配置',
}

const SERVICE_STATUS_LABELS: Record<IPQualityEvidenceReadModel['services'][number]['status'], string> = {
  unlocked: '已解锁',
  blocked: '已阻断',
  unknown: '未知',
}

const QUALITY_LABELS: Record<IPQualityEvidenceReadModel['quality']['status'], string> = {
  complete: '完整',
  partial: '部分',
  degraded: '降级',
  unknown: '未知',
}

function riskLabel(value: string): string {
  if (value === 'low') return '低'
  if (value === 'medium') return '中'
  if (value === 'high') return '高'
  return value
}

function sourceStatusLabel(value: string): string {
  if (value === 'success') return '成功'
  if (value === 'failure') return '失败'
  if (value === 'skipped') return '已跳过'
  if (value === 'not_configured') return '未配置'
  return value
}

export function IPQualityEvidenceRenderer({ model }: Props) {
  return (
    <section className="page-panel evidence-renderer evidence-renderer--ip-quality" aria-label="IP 质量证据">
      <header className="evidence-renderer__header">
        <h3>IP 质量报告</h3>
        <MonoDigits>{model.report_id}</MonoDigits>
      </header>
      <dl className="metadata-list evidence-renderer__facts">
        <div><dt>状态</dt><dd>{REPORT_STATUS_LABELS[model.status]}</dd></div>
        <div><dt>风险</dt><dd>{model.risk_level ? riskLabel(model.risk_level) : '未判定'}</dd></div>
        <div><dt>时效</dt><dd>{model.stale ? '已过期' : '有效'}</dd></div>
        <div>
          <dt>提供商覆盖</dt>
          <dd>{model.coverage.successful_provider_count}/{model.coverage.expected_provider_count}</dd>
        </div>
        <div>
          <dt>服务覆盖</dt>
          <dd>{model.coverage.successful_service_count}/{model.coverage.expected_service_count}</dd>
        </div>
        <div><dt>质量</dt><dd>{QUALITY_LABELS[model.quality.status]}</dd></div>
      </dl>
      <div className="evidence-renderer__columns">
        <section aria-label="提供商结果">
          <h4>提供商</h4>
          <ul className="evidence-renderer__list">
            {model.providers.map((provider) => (
              <li key={provider.provider}>
                <span className="evidence-renderer__name">{provider.provider}</span>
                <span className="evidence-renderer__status">
                  {SOURCE_STATUS_LABELS[provider.status]}
                  {provider.risk_level ? ` · ${riskLabel(provider.risk_level)}` : ''}
                </span>
              </li>
            ))}
          </ul>
        </section>
        <section aria-label="服务结果">
          <h4>服务</h4>
          <ul className="evidence-renderer__list">
            {model.services.map((service) => (
              <li key={`${service.service}-${service.source}`}>
                <span className="evidence-renderer__name">{service.service}</span>
                <span className="evidence-renderer__status">
                  {SERVICE_STATUS_LABELS[service.status]}
                  {service.probe_status ? ` · ${sourceStatusLabel(service.probe_status)}` : ''}
                </span>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </section>
  )
}
