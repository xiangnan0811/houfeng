import type { FormEvent } from 'react'

import { Badge, Button, Input } from '../../components/atoms'
import type { AssetDomainStatus } from '../../lib/types'
import type { DomainDraftState } from './types'
import { DOMAIN_STATUS_OPTIONS } from './vpsDetailOptions'

type VPSDomainsFormProps = {
  draft: DomainDraftState
  submitting: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onDraftChange: (draft: DomainDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSDomainsForm({
  draft,
  submitting,
  error,
  notice,
  onCancel,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSDomainsFormProps) {
  return (
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
        <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" disabled={submitting}>
          {submitting ? '创建中…' : '创建域名记录'}
        </Button>
      </div>
    </form>
  )
}
