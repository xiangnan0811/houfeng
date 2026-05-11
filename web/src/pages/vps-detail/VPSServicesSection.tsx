import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Badge, Button, DataTable, Input, MonoDigits, type DataTableColumn } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import {
  ASSET_SERVICE_STATUS_LABELS,
  ASSET_SERVICE_TYPE_LABELS,
  type AssetServiceRecord,
  type AssetServiceStatus,
  type AssetServiceType,
} from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'
import type { ServiceDraftState } from './types'
import { SERVICE_STATUS_OPTIONS, SERVICE_TYPE_OPTIONS } from './vpsDetailOptions'

type VPSServicesSectionProps = {
  services: AssetServiceRecord[]
  draft: ServiceDraftState
  submitting: boolean
  error: string | null
  notice: string | null
  onDraftChange: (draft: ServiceDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSServicesSection({
  services,
  draft,
  submitting,
  error,
  notice,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSServicesSectionProps) {
  const serviceColumns: DataTableColumn<AssetServiceRecord>[] = [
    {
      key: 'service',
      label: '服务',
      render: (service) => (
        <div className="asset-table__identity">
          <strong>{service.name}</strong>
          <span>{service.service_id}</span>
        </div>
      ),
    },
    {
      key: 'type',
      label: '类型 / 状态',
      render: (service) => (
        <span className="asset-status-stack">
          <Badge variant="info" tone="neutral">
            {ASSET_SERVICE_TYPE_LABELS[service.service_type]}
          </Badge>
          <Badge variant="count" tone={service.status === 'active' ? 'normal' : 'neutral'}>
            {ASSET_SERVICE_STATUS_LABELS[service.status]}
          </Badge>
        </span>
      ),
    },
    {
      key: 'endpoint',
      label: '入口',
      render: (service) => (
        <div className="asset-table__stack">
          <strong>{formatOptional(service.url)}</strong>
          <span>{service.port ? `端口 ${service.port}` : '端口未记录'}</span>
        </div>
      ),
    },
    {
      key: 'target',
      label: 'Target',
      render: (service) =>
        service.target_id ? (
          <Link className="text-link" to={`/targets/${service.target_id}`}>
            {service.target_id}
          </Link>
        ) : (
          <span className="text-muted">未关联</span>
        ),
    },
    {
      key: 'labels',
      label: '标签',
      render: (service) => <AssetLabels labels={service.labels} />,
    },
    {
      key: 'note',
      label: '备注',
      render: (service) => formatOptional(service.note),
    },
  ]

  return (
    <section className="page-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">SERVICES</p>
          <h2>服务资产</h2>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{services.length}</MonoDigits> 个手工记录服务
        </span>
      </div>
      <div className="asset-service-layout">
        <div className="asset-service-list">
          <DataTable
            className="asset-table vps-service-table"
            columns={serviceColumns}
            rows={services}
            rowKey={(service) => service.service_id}
            emptyContent={<span className="empty-inline">尚未记录服务</span>}
          />
        </div>
        <form className="asset-operation-form asset-service-form" onSubmit={onSubmit}>
          <div className="asset-operation-form__header">
            <div>
              <h3>新增服务</h3>
              <p>记录部署在这台 VPS 上的 Web、API、数据库、Worker 或代理服务。</p>
            </div>
            <Badge variant="count" tone="neutral">手工维护</Badge>
          </div>
          <Input
            label="服务名称"
            value={draft.name}
            onChange={(event) => {
              onDraftChange({ ...draft, name: event.target.value })
              onFeedbackClear()
            }}
            placeholder="例如：Blog"
          />
          <label className="asset-operation-field">
            <span>服务类型</span>
            <select
              value={draft.serviceType}
              onChange={(event) => {
                onDraftChange({
                  ...draft,
                  serviceType: event.target.value as AssetServiceType,
                })
                onFeedbackClear()
              }}
            >
              {SERVICE_TYPE_OPTIONS.map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </select>
          </label>
          <label className="asset-operation-field">
            <span>服务状态</span>
            <select
              value={draft.status}
              onChange={(event) => {
                onDraftChange({
                  ...draft,
                  status: event.target.value as AssetServiceStatus,
                })
                onFeedbackClear()
              }}
            >
              {SERVICE_STATUS_OPTIONS.map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </select>
          </label>
          <Input
            label="入口 URL"
            type="url"
            value={draft.url}
            onChange={(event) => {
              onDraftChange({ ...draft, url: event.target.value })
              onFeedbackClear()
            }}
            placeholder="https://example.com"
          />
          <Input
            label="端口"
            type="number"
            min="1"
            max="65535"
            value={draft.port}
            onChange={(event) => {
              onDraftChange({ ...draft, port: event.target.value })
              onFeedbackClear()
            }}
            placeholder="443"
          />
          <Input
            label="Target ID"
            value={draft.targetID}
            onChange={(event) => {
              onDraftChange({ ...draft, targetID: event.target.value })
              onFeedbackClear()
            }}
            placeholder="tg_..."
          />
          <Input
            label="服务标签"
            hint="用逗号分隔"
            value={draft.labels}
            onChange={(event) => {
              onDraftChange({ ...draft, labels: event.target.value })
              onFeedbackClear()
            }}
            placeholder="prod, public"
          />
          <label className="asset-operation-field asset-operation-field--wide">
            <span>服务备注</span>
            <textarea
              value={draft.note}
              onChange={(event) => {
                onDraftChange({ ...draft, note: event.target.value })
                onFeedbackClear()
              }}
              placeholder="例如：主站反代到本机 3000 端口"
            />
          </label>
          {error ? (
            <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
              {error}
            </p>
          ) : notice ? (
            <p className="asset-operation-feedback" role="status">{notice}</p>
          ) : null}
          <div className="asset-operation-actions">
            <Button type="submit" disabled={submitting}>
              {submitting ? '创建中…' : '创建服务记录'}
            </Button>
          </div>
        </form>
      </div>
    </section>
  )
}
