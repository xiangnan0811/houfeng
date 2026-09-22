import { Link, useLocation } from 'react-router-dom'

import { Button, MonoDigits } from '../../components/atoms'
import type { AssetServiceRecord } from '../../lib/types'
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
        <ul className="vps-object-list vps-relation-dossiers">
          {services.map((service) => (
            <ServiceDossier key={service.service_id} service={service} />
          ))}
        </ul>
      ) : (
        <p className="empty-inline">尚未记录服务</p>
      )}
    </div>
  )
}

function ServiceDossier({ service }: { service: AssetServiceRecord }) {
  const location = useLocation()
  const url = service.url.trim()
  const href = httpHref(url)
  const probe = service.target_id?.trim() ?? ''
  const note = service.note.trim()

  return (
    <li className="vps-relation-dossier">
      <div className="vps-relation-dossier__head">
        <h4 className="vps-relation-dossier__title">{serviceResourceName(service)}</h4>
        <div className="vps-relation-dossier__mast">
          <span className="vps-relation-dossier__lead">
            <span className="vps-relation-badge">{serviceResourceStatus(service)}</span>
            <span>{serviceResourceType(service)}</span>
          </span>
        </div>
      </div>
      <section className="vps-relation-dossier__body">
        <div className="vps-relation-urlblock">
          <p className="vps-relation-urlblock__label">访问入口</p>
          {url ? (
            <code className="vps-relation-urlblock__url">{url}</code>
          ) : (
            <p className="vps-relation-urlblock__empty empty-inline">未记录</p>
          )}
          {url ? (
            <div className="vps-relation-urlblock__actions">
              <VPSCopyValueButton value={url} label="入口" />
              {href ? (
                <a
                  className="btn sm ghost"
                  href={href}
                  target="_blank"
                  rel="noreferrer"
                  aria-label="打开入口"
                >
                  打开
                </a>
              ) : null}
            </div>
          ) : null}
        </div>
        <div className="vps-relation-dossier__target">
          <p className="vps-relation-dossier__index">
            <span>
              端口 {service.port != null ? <MonoDigits>{service.port}</MonoDigits> : '未记录'}
            </span>
            <span className="vps-relation-sep" aria-hidden="true">·</span>
            <span>入口探测</span>
          </p>
          {probe ? (
            <p className="vps-relation-id">
              <Link to={`/targets/${encodeURIComponent(probe)}`} state={location.state}>{probe}</Link>
            </p>
          ) : (
            <p className="vps-relation-id">未关联</p>
          )}
        </div>
        {note ? <p className="vps-relation-dossier__note">{note}</p> : null}
        {service.labels.length > 0 ? (
          <div className="vps-relation-chips">
            {service.labels.map((label) => (
              <span key={label} className="vps-relation-chip">{label}</span>
            ))}
          </div>
        ) : null}
        <div className="vps-relation-dossier__meta">
          <span className="vps-relation-dossier__meta-label">记录 ID</span>
          <span className="vps-relation-id">{service.service_id}</span>
        </div>
      </section>
    </li>
  )
}
