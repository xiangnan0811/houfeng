import { useId } from 'react'

import { Badge, type BadgeTone } from '../../../../components/atoms'
import { lookup, toneOf } from '../evidencePresentation'
import type { IPQualityEvidenceReadModel } from '../evidenceReadModels'

type Props = {
  model: IPQualityEvidenceReadModel
}

const RISK_LABELS: Record<string, string> = { low: '低风险', medium: '中风险', high: '高风险' }
const REPORT_STATUS_LABELS: Record<string, string> = { success: '成功', partial: '部分成功' }
const RISK_SHORT_LABELS: Record<string, string> = { low: '低', medium: '中', high: '高' }
const RISK_TONES: Record<string, BadgeTone> = { low: 'normal', medium: 'notice', high: 'alert' }

const SOURCE_STATUS_LABELS: Record<string, string> = {
  success: '成功',
  failure: '失败',
  skipped: '已跳过',
  not_configured: '未配置',
}

const SOURCE_STATUS_TONES: Record<string, BadgeTone> = { failure: 'alert' }

const SERVICE_STATUS_LABELS: Record<string, string> = {
  unlocked: '解锁',
  blocked: '受阻',
  unknown: '未知',
}

const SERVICE_STATUS_TONES: Record<string, BadgeTone> = { unlocked: 'normal', blocked: 'alert' }

export function IPQualityEvidenceRenderer({ model }: Props) {
  const titleId = useId()
  const providersId = useId()
  const servicesId = useId()
  const coverage = (key: string) => model.coverage[key] ?? 0
  return (
    <section className="record-section record-evidence__body" aria-labelledby={titleId}>
      <div className="record-section__head">
        <h2 className="record-section__title" id={titleId}>IP 质量报告</h2>
        <span className="record-muted mono">{model.report_id}</span>
      </div>
      <dl className="record-evidence__chips">
        <div>
          <dt>风险</dt>
          <dd>{model.risk_level
            ? <Badge variant="info" tone={toneOf(RISK_TONES, model.risk_level)}>{lookup(RISK_SHORT_LABELS, model.risk_level)}</Badge>
            : '未判定'}</dd>
        </div>
        <div><dt>采集</dt><dd>{lookup(REPORT_STATUS_LABELS, model.status)}{model.stale ? ' · 已过期' : ''}</dd></div>
        <div>
          <dt>数据库</dt>
          <dd className="mono">{coverage('successful_provider_count')}/{coverage('expected_provider_count')}</dd>
        </div>
        <div>
          <dt>服务</dt>
          <dd className="mono">{coverage('successful_service_count')}/{coverage('expected_service_count')}</dd>
        </div>
      </dl>
      <div className="record-evidence__columns">
        <section aria-labelledby={providersId}>
          <h3 className="record-evidence__subtitle" id={providersId}>数据库</h3>
          <ul className="record-evidence__rows">
            {model.providers.map((provider) => (
              <li key={provider.provider}>
                <span className="record-evidence__row-name">{provider.provider}</span>
                <span className="record-evidence__row-value">
                  {provider.status === 'success' && provider.risk_level
                    ? <Badge variant="info" tone={toneOf(RISK_TONES, provider.risk_level)}>{lookup(RISK_LABELS, provider.risk_level)}</Badge>
                    : <Badge variant="info" tone={toneOf(SOURCE_STATUS_TONES, provider.status)}>{lookup(SOURCE_STATUS_LABELS, provider.status)}</Badge>}
                </span>
              </li>
            ))}
          </ul>
        </section>
        <section aria-labelledby={servicesId}>
          <h3 className="record-evidence__subtitle" id={servicesId}>服务解锁</h3>
          <ul className="record-evidence__rows">
            {model.services.map((service) => (
              <li key={`${service.service}-${service.source}`}>
                <span className="record-evidence__row-name">{service.service}</span>
                <span className="record-evidence__row-value">
                  <Badge variant="info" tone={toneOf(SERVICE_STATUS_TONES, service.status)}>
                    {lookup(SERVICE_STATUS_LABELS, service.status)}
                  </Badge>
                  {service.probe_status && service.probe_status !== 'success'
                    ? <span className="record-muted">探测{lookup(SOURCE_STATUS_LABELS, service.probe_status)}</span>
                    : null}
                </span>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </section>
  )
}
