import { Badge } from '../atoms'
import type { IPQualityProductSummary, IPQualityServiceCheck, IPQualityServiceUnlock } from '../../lib/types'
import {
  effectiveUnlockStatus,
  observedServiceRegion,
  probeErrorLabel,
  serviceCheckStatusText,
  serviceChecks,
  serviceEndpointDiagnostics,
  serviceEvidenceKind,
  serviceLabel,
  serviceRowUsesAnonymousProbe,
  serviceScriptRule,
  serviceTileDetail,
  serviceUnlockCounts,
  unlockStatusLabel,
  unlockTone,
  type ServiceRuleMark,
} from './ipQualityPresentation'

type IPQualityServiceGridProps = {
  unlocks: IPQualityServiceUnlock[]
  // 所选报告 diagnostics.service_probe_revision 为免登录探测规则。
  anonymousProbe?: boolean
  // 所选报告自身的地址族；页面地区证据必须与之匹配。
  ipVersion: 4 | 6 | null
  // revision1 报告由服务端按服务计数；旧报告从行状态计数。
  productSummary?: IPQualityProductSummary | null
  ruleMark?: ServiceRuleMark | null
}

const TITLE_ID = 'ip-quality-services-title'

function checkLine(check: IPQualityServiceCheck): string {
  const error = check.error_code ? (probeErrorLabel(check.error_code, check.http_status) ?? check.error_code) : null
  return [
    check.label || check.id,
    serviceCheckStatusText(check.status),
    check.region || null,
    check.http_status != null ? `HTTP ${check.http_status}` : null,
    check.rule || null,
    check.status === 'available' ? null : error,
  ].filter(Boolean).join(' · ')
}

// 卡片默认只给服务名、短结论与已知地区；范围、原因、规则、入口与证据版本都在折叠详情里。
function ServiceDetails({ unlock, checks }: { unlock: IPQualityServiceUnlock, checks: IPQualityServiceCheck[] }) {
  const endpoints = checks.length > 0 ? [] : serviceEndpointDiagnostics(unlock)
  const kind = effectiveUnlockStatus(unlock)
  const scope = serviceTileDetail(unlock)
  const rule = serviceScriptRule(unlock)
  const script = serviceEvidenceKind(unlock) === 'ipquality_script'
  const lines = [
    scope ? (kind === 'unknown' ? `原因：${scope}` : `范围：${scope}`) : null,
    rule ? `规则：${rule}` : null,
    script ? '证据：IPQuality · 匿名地区准入' : null,
  ].filter((line): line is string => line != null)
  if (lines.length === 0 && checks.length === 0 && endpoints.length === 0) return null
  return (
    <details className="ipq-service__more">
      <summary aria-label={`${serviceLabel(unlock.service)} 详情`}>详情</summary>
      <ul className="ipq-service__lines">
        {lines.map((line) => <li key={line}>{line}</li>)}
        {checks.map((check) => <li key={check.id}>{checkLine(check)}</li>)}
        {endpoints.map((endpoint, index) => (
          <li key={`${endpoint.endpoint}:${index}`}>
            {[
              endpoint.endpoint,
              endpoint.httpStatus != null ? `HTTP ${endpoint.httpStatus}` : null,
              endpoint.outcome ? serviceCheckStatusText(endpoint.outcome) : null,
              endpoint.matchedRule,
              endpoint.errorCode ? (probeErrorLabel(endpoint.errorCode, endpoint.httpStatus) ?? endpoint.errorCode) : null,
            ].filter(Boolean).join(' · ')}
          </li>
        ))}
      </ul>
    </details>
  )
}

export function IPQualityServiceGrid({ unlocks, anonymousProbe = false, ipVersion, productSummary = null, ruleMark = null }: IPQualityServiceGridProps) {
  const rowCounts = serviceUnlockCounts(unlocks)
  const counts = productSummary
    ? {
        unlocked: productSummary.service_available_count,
        partial: productSummary.service_partial_count,
        blocked: productSummary.service_restricted_count,
        unknown: productSummary.service_unknown_count,
      }
    : rowCounts
  return (
    <section className="vps-detail-workspace__section ipq-services" aria-labelledby={TITLE_ID}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={TITLE_ID}>服务解锁</h2>
        <p className="ipq-services__stats" aria-label="服务状态统计">
          <span className={counts.unlocked > 0 ? 'ipq-services__stat--unlocked' : undefined}>{counts.unlocked} 可用</span>
          {counts.partial > 0 ? <span className="ipq-services__stat--partial">{counts.partial} 部分</span> : null}
          <span className={counts.blocked > 0 ? 'ipq-services__stat--blocked' : undefined}>{counts.blocked} 受限</span>
          <span>{counts.unknown} 未知</span>
          {ruleMark ? <Badge variant="info" tone="notice"><span title={ruleMark.title}>{ruleMark.label}</span></Badge> : null}
        </p>
      </div>
      {unlocks.length > 0 ? (
        <ul className="ipq-services__grid">
          {unlocks.map((unlock) => {
            const kind = effectiveUnlockStatus(unlock)
            const checks = serviceChecks(unlock)
            const observed = kind === 'unknown' ? observedServiceRegion(unlock, ipVersion) : null
            return (
              <li key={`${unlock.service}:${unlock.source ?? ''}`} className={`ipq-service ipq-service--${kind}`}>
                <div className="ipq-service__head">
                  <span className="ipq-service__name">{serviceLabel(unlock.service)}</span>
                  <Badge variant="info" tone={unlockTone(kind)}>
                    {unlockStatusLabel(unlock, anonymousProbe || serviceRowUsesAnonymousProbe(unlock))}
                  </Badge>
                </div>
                {observed ? <span className="ipq-service__detail">页面地区 {observed.region}</span> : null}
                <ServiceDetails unlock={unlock} checks={checks} />
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="ipq-empty">本次未检测服务。</p>
      )}
    </section>
  )
}
