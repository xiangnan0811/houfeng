import { Link } from 'react-router-dom'

import { Badge, Button, MonoDigits } from '../../components/atoms'
import { formatDate } from '../../lib/format'
import type { AssetDomainRecord, AssetServiceRecord } from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'
import { VPSObject } from './VPSDetailDialog'
import { domainResourceName, domainResourceStatus, serviceResourceName } from './vpsDetailResourcePresentation'

type VPSDomainsSectionProps = {
  domains: AssetDomainRecord[]
  services: AssetServiceRecord[]
  error: string | null
  notice: string | null
  readOnly?: boolean
  onCreate: () => void
}

export function VPSDomainsSection({
  domains,
  services,
  error,
  notice,
  readOnly = false,
  onCreate,
}: VPSDomainsSectionProps) {
  return (
    <div className="vps-objects">
      <p className="vps-context">
        共<MonoDigits>{domains.length}</MonoDigits>项
      </p>
      {!readOnly ? (
        <div>
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
        <ul className="vps-object-list">
          {domains.map((domain) => {
            const probe = domain.target_id?.trim() ?? ''
            const purpose = domain.purpose.trim()
            const registrar = domain.registrar.trim()
            const expires = domain.expires_at?.trim() ?? ''
            const note = domain.note.trim()
            const serviceId = domain.service_id?.trim() ?? ''
            const linkedService = serviceId
              ? services.find((service) => service.service_id === serviceId)
              : undefined
            const linkedServiceName = linkedService ? serviceResourceName(linkedService) : null
            return (
              <VPSObject
                key={domain.domain_id}
                name={<span className="mono">{domainResourceName(domain)}</span>}
                id={domain.domain_id}
                status={
                  <Badge variant="state" tone={domain.status === 'active' ? 'normal' : 'offline'}>
                    {domainResourceStatus(domain)}
                  </Badge>
                }
              >
                <dl className="vps-object__facts">
                  <div>
                    <dt>HTTPS</dt>
                    <dd>{domain.https_enabled ? '已启用' : '未记录'}</dd>
                  </div>
                  <div>
                    <dt>用途</dt>
                    <dd>{purpose || '未记录'}</dd>
                  </div>
                  <div>
                    <dt>注册商</dt>
                    <dd>{registrar || '未记录'}</dd>
                  </div>
                  <div>
                    <dt>过期</dt>
                    <dd>{expires ? formatDate(expires) : '未记录'}</dd>
                  </div>
                  <div>
                    <dt>续费</dt>
                    <dd>{domain.auto_renew ? '自动续费' : '手工续费'}</dd>
                  </div>
                  <div>
                    <dt>关联服务</dt>
                    <dd>
                      {serviceId ? (
                        <>
                          {linkedServiceName ? <>{linkedServiceName} </> : null}
                          <span className="mono">{serviceId}</span>
                        </>
                      ) : '未关联'}
                    </dd>
                  </div>
                  <div className="vps-wide">
                    <dt>入口探测</dt>
                    <dd>
                      {probe ? (
                        <Link className="text-link mono" to={`/targets/${encodeURIComponent(probe)}`}>
                          {probe}
                        </Link>
                      ) : '未关联'}
                    </dd>
                  </div>
                  {domain.labels.length > 0 ? (
                    <div className="vps-wide">
                      <dt>标签</dt>
                      <dd><AssetLabels labels={domain.labels} /></dd>
                    </div>
                  ) : null}
                  {note ? (
                    <div className="vps-wide">
                      <dt>备注</dt>
                      <dd>{note}</dd>
                    </div>
                  ) : null}
                </dl>
              </VPSObject>
            )
          })}
        </ul>
      ) : (
        <p className="empty-inline">尚未记录域名</p>
      )}
    </div>
  )
}
