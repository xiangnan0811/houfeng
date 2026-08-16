import { MonoDigits } from '../../../../components/atoms'
import type { IPQualityEvidenceReadModel } from '../evidenceReadModels'

type Props = {
  model: IPQualityEvidenceReadModel
}

export function IPQualityEvidenceRenderer({ model }: Props) {
  return (
    <section className="page-panel evidence-renderer evidence-renderer--ip-quality" aria-label="IP 质量证据">
      <header className="evidence-renderer__header">
        <h3>IP 质量报告</h3>
        <MonoDigits>{model.report_id}</MonoDigits>
      </header>
      <dl className="metadata-list evidence-renderer__facts">
        <div><dt>状态</dt><dd>{model.status}</dd></div>
        <div><dt>风险</dt><dd>{model.risk_level || '未判定'}</dd></div>
        <div><dt>时效</dt><dd>{model.stale ? '已过期' : '有效'}</dd></div>
        <div>
          <dt>提供商覆盖</dt>
          <dd>{model.coverage.successful_provider_count}/{model.coverage.expected_provider_count}</dd>
        </div>
        <div>
          <dt>服务覆盖</dt>
          <dd>{model.coverage.successful_service_count}/{model.coverage.expected_service_count}</dd>
        </div>
        <div><dt>质量</dt><dd>{model.quality.status}</dd></div>
      </dl>
      <div className="evidence-renderer__columns">
        <section aria-label="提供商结果">
          <h4>提供商</h4>
          <ul className="evidence-renderer__list">
            {model.providers.map((provider) => (
              <li key={provider.provider}>
                <span>{provider.provider}</span>
                <span>{provider.status}{provider.risk_level ? ` · ${provider.risk_level}` : ''}</span>
              </li>
            ))}
          </ul>
        </section>
        <section aria-label="服务结果">
          <h4>服务</h4>
          <ul className="evidence-renderer__list">
            {model.services.map((service) => (
              <li key={`${service.service}-${service.source}`}>
                <span>{service.service}</span>
                <span>{service.status}{service.probe_status ? ` · ${service.probe_status}` : ''}</span>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </section>
  )
}
