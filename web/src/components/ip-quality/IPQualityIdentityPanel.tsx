import type { ReactNode } from 'react'

import { Badge, Timestamp } from '../atoms'
import type { IPQualityProductView, IPQualityProviderResult, IPQualitySummary, VPSIPQualityReport } from '../../lib/types'
import {
  agreedCountry,
  agreedValue,
  enrichmentGap,
  groupDigits,
  ipTypeView,
  legacyGeoStatusLabel,
  locationRows,
  providerMethodLabel,
  safeReferenceURL,
  usageTypeLabel,
  vendorGroups,
  type AgreedValue,
} from './ipQualityProductPresentation'
import { providerSucceeded } from './ipQualityPresentation'
import { IP_QUALITY_FACTORS_ANCHOR } from './IPQualityProviderTable'

export const IP_QUALITY_LOCATION_ANCHOR = 'ip-quality-location-title'
const TITLE_ID = 'ip-quality-identity-title'

type IPQualityIdentityPanelProps = {
  report: VPSIPQualityReport
  summary: IPQualitySummary
  product: IPQualityProductView | null
}

function Fact({ label, children }: { label: string, children: ReactNode }) {
  return (
    <div className="ipq-fact">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  )
}

function Gap({ text }: { text: string }) {
  return <span className="ipq-gap">{text}</span>
}

// 一致时显示单值；分歧时显示调用方给出的该字段证据入口；都没有时显示占位。
function agreedFact(value: AgreedValue, evidence: ReactNode, fallback: ReactNode = <Gap text="—" />): ReactNode {
  if (value.discrepant) return evidence
  return value.value ?? fallback
}

// 到同页逐源对照的真实锚点：普通激活时把焦点移到目标标题（滚动仍由原生锚点完成）；修饰键/新窗口不劫持。
function EvidenceLink({ target, field, section }: { target: string, field: string, section: string }) {
  return (
    <a
      className="ipq-chip-link"
      href={`#${target}`}
      aria-label={`${field}来源有分歧，查看${section}`}
      onClick={(event) => {
        if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
        document.getElementById(target)?.focus({ preventScroll: true })
      }}
    >
      来源有分歧
    </a>
  )
}

type FieldEvidence = { provider: string, method: string | null, value: string }

// 就近展开的逐源对照：只列本次选出的成功主样本中该字段的非空值，与一致性判断的输入完全相同。
function DisagreementDetails({ label, entries }: { label: string, entries: FieldEvidence[] }) {
  return (
    <details className="ipq-disagreement">
      <summary className="ipq-chip-link" aria-label={`查看${label}来源分歧`}>
        <span className="ipq-disagreement__caret" aria-hidden="true">▸</span>
        来源有分歧
      </summary>
      <dl className="ipq-disagreement__list">
        {entries.map((entry) => (
          <div key={entry.provider}>
            <dt>{entry.provider}{entry.method ? <span className="ipq-muted"> · {entry.method}</span> : null}</dt>
            <dd className="ipq-wrap">{entry.value}</dd>
          </div>
        ))}
      </dl>
    </details>
  )
}

function fieldEvidence(rows: IPQualityProviderResult[], pick: (row: IPQualityProviderResult) => string): FieldEvidence[] {
  return rows
    .map((row) => ({ provider: row.provider, method: providerMethodLabel(row), value: pick(row).trim() }))
    .filter((entry) => entry.value !== '')
}

// 默认只给六项：自治系统号、组织、城市、注册地、使用地、IP 类型；其余放进默认关闭的“更多信息”。
export function IPQualityIdentityPanel({ report, summary, product }: IPQualityIdentityPanelProps) {
  const rows = product ? product.providers : report.provider_results
  const locations = locationRows(rows)
  const primaries = vendorGroups(rows).map((group) => group.primary).filter(providerSucceeded)
  const legacyRegion = (summary.use_region_code || summary.use_region_name || '').trim()
  const country = agreedCountry(locations)
  const region = agreedValue(locations.map((row) => row.region))
  const city = agreedValue(locations.map((row) => row.city))
  const usage = agreedValue(primaries.map((row) => usageTypeLabel(row.usage_type) ?? ''))
  const company = agreedValue(primaries.map((row) => usageTypeLabel(row.company_type) ?? ''))
  const companyNamePick = (row: IPQualityProviderResult) => row.details?.company_name ?? ''
  const companyName = agreedValue(primaries.map(companyNamePick))
  const ipType = ipTypeView(product?.summary ?? null)
  const serverIPType = product?.summary.ip_type ?? null
  // 注册地只认真实注册国家：服务端按注册国比较时用其 comparison_country_code（country_code 是使用国），
  // 否则用来源明确给出的 registered_country_code；滥用联系国家、ASN/RIR/组织国家都不冒充注册地。
  const registeredPick = (row: IPQualityProviderResult) => (row.details?.registered_country_code ?? '').toUpperCase()
  const registeredCountry: AgreedValue = serverIPType?.basis === 'registered_country' && serverIPType.comparison_country_code
    ? { value: serverIPType.comparison_country_code, discrepant: false }
    : agreedValue(primaries.map(registeredPick))
  const registration = product?.registration
  const registrationKnown = registration?.status === 'success'
  const asn = product?.asn
  const asnKnown = asn?.status === 'success'
  const gap = enrichmentGap(product)
  const sourceURL = asnKnown ? safeReferenceURL(asn.source_url) : null
  // 自治系统号与组织取自同一份 ASN 证据；没有 Center ASN 资料时一起回退到报告自身摘要，不跨来源拼接。
  const asnNumber = asnKnown ? asn.number : (summary.asn ?? '')
  const asnOrganization = asnKnown ? asn.name : (summary.organization ?? '')
  const legacyGeo = product && !serverIPType ? legacyGeoStatusLabel(product.summary.geo_status) : null

  return (
    <section className="vps-detail-workspace__section ipq-identity" aria-labelledby={TITLE_ID}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={TITLE_ID}>基础信息</h2>
      </div>
      <dl className="ipq-identity__primary">
        <Fact label="自治系统号">
          {asnNumber ? <span className="mono">{asnNumber.toUpperCase().startsWith('AS') ? asnNumber : `AS${asnNumber}`}</span> : <Gap text={product && !asnKnown ? gap : '—'} />}
        </Fact>
        <Fact label="组织">
          {asnOrganization ? <span className="ipq-wrap">{asnOrganization}</span> : <Gap text={product && !asnKnown ? gap : '—'} />}
        </Fact>
        <Fact label="城市">{agreedFact(city, <EvidenceLink target={IP_QUALITY_LOCATION_ANCHOR} field="城市" section="多源定位" />)}</Fact>
        <Fact label="注册地">
          {agreedFact(registeredCountry, <DisagreementDetails label="注册地" entries={fieldEvidence(primaries, registeredPick)} />, <Gap text="未知" />)}
        </Fact>
        <Fact label="使用地">
          {agreedFact(country, <EvidenceLink target={IP_QUALITY_LOCATION_ANCHOR} field="使用地" section="多源定位" />, legacyRegion || <Gap text="—" />)}
        </Fact>
        <Fact label="IP 类型">
          <Badge variant="info" tone={ipType.tone}>{ipType.label}</Badge>
        </Fact>
      </dl>
      <details className="ipq-identity__more">
        <summary>更多信息</summary>
        <dl className="ipq-facts">
          <Fact label="省 / 州">{agreedFact(region, <EvidenceLink target={IP_QUALITY_LOCATION_ANCHOR} field="省 / 州" section="多源定位" />)}</Fact>
          <Fact label="使用类型">{agreedFact(usage, <EvidenceLink target={IP_QUALITY_FACTORS_ANCHOR} field="使用类型" section="风险因子" />)}</Fact>
          <Fact label="公司类型">{agreedFact(company, <EvidenceLink target={IP_QUALITY_FACTORS_ANCHOR} field="公司类型" section="风险因子" />)}</Fact>
          <Fact label="公司">{agreedFact(companyName, <DisagreementDetails label="公司名称" entries={fieldEvidence(primaries, companyNamePick)} />)}</Fact>
          <Fact label="PTR">
            {product
              ? (product.ptr_names.length > 0
                  ? <span className="mono ipq-wrap">{product.ptr_names.join(', ')}</span>
                  : <Gap text="无" />)
              : <Gap text="旧版报告无此项" />}
          </Fact>
          <Fact label="注册网络国家">
            {registrationKnown ? (registration.network_country_code || <Gap text="—" />) : <Gap text={gap} />}
          </Fact>
          {registrationKnown && (registration.start_address || registration.network_name) ? (
            <Fact label="注册网段">
              <span className="mono ipq-wrap">
                {[registration.start_address && registration.end_address ? `${registration.start_address} – ${registration.end_address}` : '', registration.network_name]
                  .filter(Boolean)
                  .join(' · ')}
              </span>
            </Fact>
          ) : null}
          {registrationKnown && registration.registrant_country_code ? (
            <Fact label="注册组织国家">{registration.registrant_country_code}</Fact>
          ) : null}
          {legacyGeo ? <Fact label="旧版地理比较">{legacyGeo}</Fact> : null}
          <Fact label="ASN 域名">{asnKnown ? (asn.domain ? <span className="mono ipq-wrap">{asn.domain}</span> : <Gap text="—" />) : <Gap text={gap} />}</Fact>
          <Fact label="ASN 类型">{asnKnown ? (usageTypeLabel(asn.type) ?? <Gap text="—" />) : <Gap text={gap} />}</Fact>
          <Fact label="ASN 管理国家">{asnKnown ? (asn.country_code || <Gap text="—" />) : <Gap text={gap} />}</Fact>
          <Fact label="RIR">{asnKnown ? (asn.rir ? asn.rir.toUpperCase() : <Gap text="—" />) : <Gap text={gap} />}</Fact>
          <Fact label="ASN 注册时间">{asnKnown && asn.registered_at ? <Timestamp value={asn.registered_at} /> : <Gap text={asnKnown ? '—' : gap} />}</Fact>
          <Fact label="ASN 更新时间">{asnKnown && asn.updated_at ? <Timestamp value={asn.updated_at} /> : <Gap text={asnKnown ? '—' : gap} />}</Fact>
          <Fact label="宣告 IPv4">
            {asnKnown && (asn.ipv4_prefixes || asn.ipv4_addresses)
              ? <span className="mono">{groupDigits(asn.ipv4_prefixes) ?? '—'} 前缀 · {groupDigits(asn.ipv4_addresses) ?? '—'} 地址</span>
              : <Gap text={asnKnown ? '—' : gap} />}
          </Fact>
          <Fact label="宣告 IPv6">
            {asnKnown && (asn.ipv6_prefixes || asn.ipv6_48s)
              ? <span className="mono">{groupDigits(asn.ipv6_prefixes) ?? '—'} 前缀 · {groupDigits(asn.ipv6_48s) ?? '—'} 个 /48</span>
              : <Gap text={asnKnown ? '—' : gap} />}
          </Fact>
          {asnKnown && asn.route ? <Fact label="路由前缀"><span className="mono ipq-wrap">{asn.route}</span></Fact> : null}
          {asnKnown && asn.origins.length > 1 ? (
            <Fact label="多起源">
              <span className="mono ipq-wrap">{asn.origins.join(', ')}</span>
            </Fact>
          ) : null}
          {sourceURL ? (
            <Fact label="ASN 资料">
              <a className="text-link" href={sourceURL} target="_blank" rel="noreferrer noopener">资料来源</a>
            </Fact>
          ) : null}
        </dl>
      </details>
    </section>
  )
}
