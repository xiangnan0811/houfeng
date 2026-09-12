import { Link } from 'react-router-dom'

import { Badge, Button, MonoDigits } from '../../components/atoms'
import type { AssetServiceRecord } from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'
import { VPSCopyValueButton } from './VPSCopyValueButton'
import { VPSObject } from './VPSDetailDialog'
import {
  httpHref,
  serviceResourceName,
  serviceResourceStatus,
  serviceResourceType,
} from './vpsDetailResourcePresentation'

type VPSServicesSectionProps = {
  services: AssetServiceRecord[]
  error: string | null
  notice: string | null
  readOnly?: boolean
  onCreate: () => void
}

export function VPSServicesSection({
  services,
  error,
  notice,
  readOnly = false,
  onCreate,
}: VPSServicesSectionProps) {
  return (
    <div className="vps-objects">
      <p className="vps-context">
        共<MonoDigits>{services.length}</MonoDigits>项
      </p>
      {!readOnly ? (
        <div>
          <Button variant="secondary" size="sm" onClick={onCreate}>新增服务</Button>
        </div>
      ) : null}
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      {services.length > 0 ? (
        <ul className="vps-object-list">
          {services.map((service) => {
            const url = service.url.trim()
            const href = httpHref(url)
            const probe = service.target_id?.trim() ?? ''
            const note = service.note.trim()
            return (
              <VPSObject
                key={service.service_id}
                name={serviceResourceName(service)}
                id={service.service_id}
                status={
                  <Badge variant="state" tone={service.status === 'active' ? 'normal' : 'offline'}>
                    {serviceResourceStatus(service)}
                  </Badge>
                }
              >
                <dl className="vps-object__facts">
                  <div className="vps-wide">
                    <dt>入口</dt>
                    <dd>
                      {url ? (
                        <span>
                          {href ? (
                            <a className="text-link" href={href} target="_blank" rel="noreferrer">{url}</a>
                          ) : url}
                          <VPSCopyValueButton value={url} label="入口" />
                        </span>
                      ) : '未记录'}
                    </dd>
                  </div>
                  <div>
                    <dt>类型</dt>
                    <dd>{serviceResourceType(service)}</dd>
                  </div>
                  <div>
                    <dt>端口</dt>
                    <dd>{service.port != null ? <MonoDigits>{service.port}</MonoDigits> : '未记录'}</dd>
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
                  {service.labels.length > 0 ? (
                    <div className="vps-wide">
                      <dt>标签</dt>
                      <dd><AssetLabels labels={service.labels} /></dd>
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
        <p className="empty-inline">尚未记录服务</p>
      )}
    </div>
  )
}
