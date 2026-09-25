import { useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Button, MonoDigits } from '../../components/atoms'
import { DependencyStatusCorrection } from '../../components/DependencyStatusCorrection'
import { formatDate } from '../../lib/format'
import type { AssetDomainRecord, AssetServiceRecord } from '../../lib/types'
import { domainResourceName, domainResourceStatus, serviceResourceName } from './vpsDetailResourcePresentation'

type VPSDomainsSectionProps = {
  domains: AssetDomainRecord[]
  services: AssetServiceRecord[]
  error: string | null
  notice: string | null
  parentLifecycle?: string
  readOnly?: boolean
  onCreate: () => void
  onRetryServices?: (() => void) | undefined
  onChanged?: () => void
}

export function VPSDomainsSection({
  domains,
  services,
  error,
  notice,
  parentLifecycle = 'active',
  readOnly = false,
  onCreate,
  onRetryServices,
  onChanged,
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
        <>
          <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
            {error}
          </p>
          {onRetryServices ? (
            <div>
              <Button size="sm" onClick={onRetryServices}>重试加载服务</Button>
            </div>
          ) : null}
        </>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      {domains.length > 0 ? (
        <ul className="vps-object-list vps-relation-dossiers">
          {domains.map((domain) => (
            <DomainDossier
              key={domain.domain_id}
              domain={domain}
              services={services}
              readOnly={readOnly}
              parentLifecycle={parentLifecycle}
              onChanged={onChanged}
            />
          ))}
        </ul>
      ) : (
        <p className="empty-inline">尚未记录域名</p>
      )}
    </div>
  )
}

function DomainDossier({
  domain,
  services,
  readOnly = false,
  parentLifecycle,
  onChanged,
}: {
  domain: AssetDomainRecord
  services: AssetServiceRecord[]
  readOnly?: boolean
  parentLifecycle: string
  onChanged?: (() => void) | undefined
}) {
  const [open, setOpen] = useState(false)
  const location = useLocation()
  const purpose = domain.purpose.trim()
  const registrar = domain.registrar.trim()
  const expires = domain.expires_at?.trim() ?? ''
  const note = domain.note.trim()
  const serviceId = domain.service_id?.trim() ?? ''
  const probe = domain.target_id?.trim() ?? ''
  const linkedService = serviceId
    ? services.find((service) => service.service_id === serviceId)
    : undefined
  const linkedServiceName = linkedService ? serviceResourceName(linkedService) : null

  return (
    <li className="vps-relation-dossier">
      <div className="vps-relation-dossier__head">
        <h4 className="vps-relation-dossier__title mono">{domainResourceName(domain)}</h4>
        <div className="vps-relation-dossier__mast">
          <span className="vps-relation-dossier__lead">
            <span className="vps-relation-badge">{domainResourceStatus(domain)}</span>
            <span>{purpose || '未记录'}</span>
          </span>
          {!readOnly ? (
            <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>更正状态</Button>
          ) : null}
        </div>
      </div>
      {open ? (
        <DependencyStatusCorrection
          open
          kind="domain"
          objectId={domain.domain_id}
          displayName={domainResourceName(domain)}
          currentStatus={domain.status}
          parentLifecycle={parentLifecycle}
          onClose={() => setOpen(false)}
          onCompleted={() => {
            setOpen(false)
            onChanged?.()
          }}
        />
      ) : null}
      <section className="vps-relation-dossier__body">
        <div className="vps-relation-fields">
          <div className="vps-relation-field">
            {serviceId ? (
              <>
                <div className="vps-relation-field__label-row">
                  <span className="vps-relation-field__label">关联服务</span>
                  {linkedServiceName ? <span className="vps-relation-id">{serviceId}</span> : null}
                </div>
                <p className="vps-relation-field__value">
                  {linkedServiceName ?? <span className="vps-relation-id">{serviceId}</span>}
                </p>
              </>
            ) : (
              <>
                <span className="vps-relation-field__label">关联服务</span>
                <p className="vps-relation-field__value">未关联</p>
              </>
            )}
          </div>
          <div className="vps-relation-dossier__target">
            <p className="vps-relation-dossier__index">
              <span>关联入口探测</span>
            </p>
            {probe ? (
              <p className="vps-relation-id">
                <Link to={`/targets/${encodeURIComponent(probe)}`} state={location.state}>{probe}</Link>
              </p>
            ) : (
              <p className="vps-relation-id">未关联</p>
            )}
          </div>
          <div className="vps-relation-fields__row">
            <div className="vps-relation-field">
              <span className="vps-relation-field__label">注册商</span>
              <p className="vps-relation-field__value">{registrar || '未记录'}</p>
            </div>
            <div className="vps-relation-field">
              <span className="vps-relation-field__label">过期日期</span>
              <p className="vps-relation-field__value">{expires ? formatDate(expires) : '未记录'}</p>
            </div>
            <div className="vps-relation-field">
              <span className="vps-relation-field__label">续费</span>
              <p className="vps-relation-field__value">{domain.auto_renew ? '自动续费' : '手工续费'}</p>
            </div>
            <div className="vps-relation-field">
              <span className="vps-relation-field__label">HTTPS</span>
              <p className="vps-relation-field__value">{domain.https_enabled ? '已启用' : '未记录'}</p>
            </div>
          </div>
        </div>
        {note ? <p className="vps-relation-dossier__note">{note}</p> : null}
        {domain.labels.length > 0 ? (
          <div className="vps-relation-chips">
            {domain.labels.map((label) => (
              <span key={label} className="vps-relation-chip">{label}</span>
            ))}
          </div>
        ) : null}
        <div className="vps-relation-dossier__meta">
          <span className="vps-relation-dossier__meta-label">记录 ID</span>
          <span className="vps-relation-id">{domain.domain_id}</span>
        </div>
      </section>
    </li>
  )
}
