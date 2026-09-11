import { Link } from 'react-router-dom'

import { Button, MonoDigits } from '../../components/atoms'
import type { AssetServiceRecord } from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'
import { VPSCopyValueButton } from './VPSCopyValueButton'
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
    <div className="vps-relation-modal">
      <p className="vps-relation-modal__count">
        <MonoDigits>{services.length}</MonoDigits> 个已关联服务
      </p>
      {!readOnly ? (
        <div className="vps-relation-modal__actions">
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
        <ul className="vps-relation-list">
          {services.map((service) => {
            const url = service.url.trim()
            const href = httpHref(url)
            const probe = service.target_id?.trim() ?? ''
            const note = service.note.trim()
            return (
              <li key={service.service_id} className="vps-relation-row vps-relation-row--service">
                <div className="vps-relation-row__group vps-relation-row__group--identity">
                  <div className="vps-relation-row__identity">
                    <strong>{serviceResourceName(service)}</strong>
                    <span className="mono">{service.service_id}</span>
                  </div>
                  <dl className="vps-relation-row__facts">
                    <div>
                      <dt>状态</dt>
                      <dd>{serviceResourceStatus(service)}</dd>
                    </div>
                    <div>
                      <dt>类型</dt>
                      <dd>{serviceResourceType(service)}</dd>
                    </div>
                  </dl>
                </div>
                <div className="vps-relation-row__group vps-relation-row__group--entry">
                  <dl className="vps-relation-row__facts">
                    <div>
                      <dt>入口</dt>
                      <dd>
                        {url ? (
                          <span className="vps-relation-row__entry-value">
                            {href ? (
                              <a className="text-link" href={href} target="_blank" rel="noreferrer">{url}</a>
                            ) : url}
                            <VPSCopyValueButton value={url} label="入口" />
                          </span>
                        ) : '未记录'}
                      </dd>
                    </div>
                  </dl>
                </div>
                <div className="vps-relation-row__group vps-relation-row__group--assoc">
                  <dl className="vps-relation-row__facts">
                    <div>
                      <dt>端口</dt>
                      <dd>{service.port != null ? service.port : '未记录'}</dd>
                    </div>
                    <div>
                      <dt>入口探测</dt>
                      <dd>
                        {probe ? (
                          <Link className="text-link" to={`/targets/${encodeURIComponent(probe)}`}>
                            {probe}
                          </Link>
                        ) : '未关联'}
                      </dd>
                    </div>
                    {service.labels.length > 0 ? (
                      <div>
                        <dt>标签</dt>
                        <dd><AssetLabels labels={service.labels} /></dd>
                      </div>
                    ) : null}
                  </dl>
                </div>
                {note ? (
                  <div className="vps-relation-row__group vps-relation-row__group--note">
                    <dl className="vps-relation-row__facts">
                      <div>
                        <dt>备注</dt>
                        <dd>{note}</dd>
                      </div>
                    </dl>
                  </div>
                ) : null}
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="empty-inline">尚未记录服务</p>
      )}
    </div>
  )
}
