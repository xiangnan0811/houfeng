import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Badge, Button, DataTable, Input, MonoDigits, type DataTableColumn } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import {
  ASSET_DOMAIN_STATUS_LABELS,
  type AssetDomainRecord,
  type AssetDomainStatus,
} from '../../lib/types'
import { AssetLabels } from '../assetPageBadges'
import type { DomainDraftState } from './types'
import { DOMAIN_STATUS_OPTIONS } from './vpsDetailOptions'

type VPSDomainsSectionProps = {
  domains: AssetDomainRecord[]
  draft: DomainDraftState
  submitting: boolean
  error: string | null
  notice: string | null
  onDraftChange: (draft: DomainDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSDomainsSection({
  domains,
  draft,
  submitting,
  error,
  notice,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSDomainsSectionProps) {
  const domainColumns: DataTableColumn<AssetDomainRecord>[] = [
    {
      key: 'domain',
      label: '域名',
      render: (domain) => (
        <div className="asset-table__identity">
          <strong>{domain.domain_name}</strong>
          <span>{domain.domain_id}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态 / HTTPS',
      render: (domain) => (
        <span className="asset-status-stack">
          <Badge variant="count" tone={domain.status === 'active' ? 'normal' : 'neutral'}>
            {ASSET_DOMAIN_STATUS_LABELS[domain.status]}
          </Badge>
          <Badge variant="info" tone={domain.https_enabled ? 'normal' : 'neutral'}>
            {domain.https_enabled ? 'HTTPS' : '未记录 HTTPS'}
          </Badge>
        </span>
      ),
    },
    {
      key: 'purpose',
      label: '用途 / 注册商',
      render: (domain) => (
        <div className="asset-table__stack">
          <strong>{formatOptional(domain.purpose)}</strong>
          <span>{formatOptional(domain.registrar)}</span>
        </div>
      ),
    },
    {
      key: 'expires',
      label: '过期 / 续费',
      render: (domain) => (
        <div className="asset-table__stack">
          <strong>{formatOptional(domain.expires_at)}</strong>
          <span>{domain.auto_renew ? '自动续费' : '手工续费'}</span>
        </div>
      ),
    },
    {
      key: 'links',
      label: '关联',
      render: (domain) => (
        <div className="asset-table__stack">
          <span>{domain.service_id ? `服务 ${domain.service_id}` : '未关联服务'}</span>
          {domain.target_id ? (
            <Link className="text-link" to={`/targets/${domain.target_id}`}>
              Target {domain.target_id}
            </Link>
          ) : (
            <span className="text-muted">未关联 Target</span>
          )}
        </div>
      ),
    },
    {
      key: 'labels',
      label: '标签',
      render: (domain) => <AssetLabels labels={domain.labels} />,
    },
    {
      key: 'note',
      label: '备注',
      render: (domain) => formatOptional(domain.note),
    },
  ]

  return (
    <section className="page-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">DOMAINS</p>
          <h2>域名资产</h2>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{domains.length}</MonoDigits> 个手工记录域名
        </span>
      </div>
      <div className="asset-service-layout">
        <div className="asset-service-list">
          <DataTable
            className="asset-table vps-domain-table"
            columns={domainColumns}
            rows={domains}
            rowKey={(domain) => domain.domain_id}
            emptyContent={<span className="empty-inline">尚未记录域名</span>}
          />
        </div>
        <form className="asset-operation-form asset-service-form" onSubmit={onSubmit}>
          <div className="asset-operation-form__header">
            <div>
              <h3>新增域名</h3>
              <p>记录这台 VPS 承载、转发或观测关联的域名。</p>
            </div>
            <Badge variant="count" tone="neutral">手工维护</Badge>
          </div>
          <Input
            label="域名"
            value={draft.domainName}
            onChange={(event) => {
              onDraftChange({ ...draft, domainName: event.target.value })
              onFeedbackClear()
            }}
            placeholder="www.example.com"
          />
          <label className="asset-operation-field">
            <span>域名状态</span>
            <select
              value={draft.status}
              onChange={(event) => {
                onDraftChange({
                  ...draft,
                  status: event.target.value as AssetDomainStatus,
                })
                onFeedbackClear()
              }}
            >
              {DOMAIN_STATUS_OPTIONS.map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </select>
          </label>
          <Input
            label="用途"
            value={draft.purpose}
            onChange={(event) => {
              onDraftChange({ ...draft, purpose: event.target.value })
              onFeedbackClear()
            }}
            placeholder="官网 / API / 回源"
          />
          <Input
            label="Service ID"
            value={draft.serviceID}
            onChange={(event) => {
              onDraftChange({ ...draft, serviceID: event.target.value })
              onFeedbackClear()
            }}
            placeholder="svc_..."
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
            label="注册商"
            value={draft.registrar}
            onChange={(event) => {
              onDraftChange({ ...draft, registrar: event.target.value })
              onFeedbackClear()
            }}
            placeholder="NameSilo"
          />
          <Input
            label="过期日期"
            type="date"
            value={draft.expiresAt}
            onChange={(event) => {
              onDraftChange({ ...draft, expiresAt: event.target.value })
              onFeedbackClear()
            }}
          />
          <label className="asset-checkbox-line">
            <input
              type="checkbox"
              checked={draft.autoRenew}
              onChange={(event) => {
                onDraftChange({ ...draft, autoRenew: event.target.checked })
                onFeedbackClear()
              }}
            />
            <span>自动续费</span>
          </label>
          <label className="asset-checkbox-line">
            <input
              type="checkbox"
              checked={draft.httpsEnabled}
              onChange={(event) => {
                onDraftChange({ ...draft, httpsEnabled: event.target.checked })
                onFeedbackClear()
              }}
            />
            <span>已启用 HTTPS</span>
          </label>
          <Input
            label="域名标签"
            hint="用逗号分隔"
            value={draft.labels}
            onChange={(event) => {
              onDraftChange({ ...draft, labels: event.target.value })
              onFeedbackClear()
            }}
            placeholder="prod, public"
          />
          <label className="asset-operation-field asset-operation-field--wide">
            <span>域名备注</span>
            <textarea
              value={draft.note}
              onChange={(event) => {
                onDraftChange({ ...draft, note: event.target.value })
                onFeedbackClear()
              }}
              placeholder="例如：Cloudflare 代理到主站服务"
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
              {submitting ? '创建中…' : '创建域名记录'}
            </Button>
          </div>
        </form>
      </div>
    </section>
  )
}
