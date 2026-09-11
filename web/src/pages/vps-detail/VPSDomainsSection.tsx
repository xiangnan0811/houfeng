import { Link } from 'react-router-dom'

import { Badge, Button, MonoDigits } from '../../components/atoms'
import { formatDate } from '../../lib/format'
import type { AssetDomainRecord } from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'
import { domainResourceName, domainResourceStatus } from './vpsDetailResourcePresentation'

type VPSDomainsSectionProps = {
  domains: AssetDomainRecord[]
  error: string | null
  notice: string | null
  readOnly?: boolean
  onCreate: () => void
}

export function VPSDomainsSection({
  domains,
  error,
  notice,
  readOnly = false,
  onCreate,
}: VPSDomainsSectionProps) {
  return (
    <div className="vps-relation-modal">
      <p className="vps-relation-modal__count">
        <MonoDigits>{domains.length}</MonoDigits> 个已关联域名
      </p>
      {!readOnly ? (
        <div className="vps-relation-modal__actions">
          <Button variant="secondary" size="sm" onClick={onCreate}>新增域名</Button>
        </div>
      ) : null}
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      {domains.length > 0 ? (
        <ul className="vps-relation-list">
          {domains.map((domain) => {
            const probe = domain.target_id?.trim() ?? ''
            const purpose = domain.purpose.trim()
            const registrar = domain.registrar.trim()
            const expires = domain.expires_at?.trim() ?? ''
            const note = domain.note.trim()
            const serviceId = domain.service_id?.trim() ?? ''
            return (
              <li key={domain.domain_id} className="vps-relation-row vps-relation-row--resource vps-relation-row--domain">
                <div className="vps-detail-resource__primary">
                  <div className="vps-detail-resource__identity">
                    <strong className="vps-detail-resource__name">{domainResourceName(domain)}</strong>
                    <span className="vps-detail-resource__id mono">{domain.domain_id}</span>
                  </div>
                  <span className="badge-row badge-row--wrap">
                    <Badge variant="state" tone={domain.status === 'active' ? 'normal' : 'offline'}>
                      {domainResourceStatus(domain)}
                    </Badge>
                  </span>
                </div>
                <dl className="vps-relation-row__facts">
                  <div>
                    <dt>HTTPS</dt>
                    <dd>{domain.https_enabled ? 'HTTPS' : '未记录 HTTPS'}</dd>
                  </div>
                  <div>
                    <dt>用途</dt>
                    <dd>{purpose || '用途未记录'}</dd>
                  </div>
                  <div>
                    <dt>注册商</dt>
                    <dd>{registrar || '注册商未记录'}</dd>
                  </div>
                  <div>
                    <dt>过期</dt>
                    <dd>{expires ? formatDate(expires) : '过期日未记录'}</dd>
                  </div>
                  <div>
                    <dt>续费</dt>
                    <dd>{domain.auto_renew ? '自动续费' : '手工续费'}</dd>
                  </div>
                  <div>
                    <dt>关联服务</dt>
                    <dd>{serviceId ? <>服务 <span className="mono">{serviceId}</span></> : '未关联服务'}</dd>
                  </div>
                  <div className="vps-relation-row__wide">
                    <dt>入口探测</dt>
                    <dd>
                      {probe ? (
                        <Link className="text-link mono" to={`/targets/${encodeURIComponent(probe)}`}>
                          {probe}
                        </Link>
                      ) : '未关联入口探测'}
                    </dd>
                  </div>
                  {domain.labels.length > 0 ? (
                    <div className="vps-relation-row__wide">
                      <dt>标签</dt>
                      <dd><AssetLabels labels={domain.labels} /></dd>
                    </div>
                  ) : null}
                  {note ? (
                    <div className="vps-relation-row__wide vps-relation-row__note">
                      <dt>备注</dt>
                      <dd>{note}</dd>
                    </div>
                  ) : null}
                </dl>
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="empty-inline">尚未记录域名</p>
      )}
    </div>
  )
}
