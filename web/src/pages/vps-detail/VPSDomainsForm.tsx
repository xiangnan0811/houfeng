import { useId, type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Badge, Button, Input } from '../../components/atoms'
import type { AssetDomainStatus, AssetServiceRecord, TargetRecord } from '../../lib/types'
import type { DomainDraftState } from './types'
import { DOMAIN_STATUS_OPTIONS } from './vpsDetailOptions'

type VPSDomainsFormProps = {
  draft: DomainDraftState
  services: AssetServiceRecord[]
  targets: TargetRecord[]
  targetsLoading: boolean
  targetsError: string | null
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
  services,
  targets,
  targetsLoading,
  targetsError,
  submitting,
  error,
  notice,
  onCancel,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSDomainsFormProps) {
  const statusSelectId = useId()
  const serviceSelectId = useId()
  const targetSelectId = useId()
  const noteId = useId()

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
      <label className="asset-operation-field" htmlFor={statusSelectId}>
        <span>域名状态</span>
        <select
          id={statusSelectId}
          aria-label="域名状态"
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
      <label className="asset-operation-field" htmlFor={serviceSelectId}>
        <span>关联服务</span>
        <select
          id={serviceSelectId}
          aria-label="关联服务"
          value={draft.serviceID}
          disabled={services.length === 0}
          onChange={(event) => {
            onDraftChange({ ...draft, serviceID: event.target.value })
            onFeedbackClear()
          }}
        >
          <option value="">不关联服务</option>
          {services.map((service) => (
            <option key={service.service_id} value={service.service_id}>
              {service.name} · {service.service_id} · {service.status}
            </option>
          ))}
        </select>
        <small>
          {services.length === 0 ? '当前 VPS 还没有服务记录，可先创建服务或保留为空。' : '仅关联当前 VPS 的服务记录。'}
        </small>
      </label>
      <label className="asset-operation-field" htmlFor={targetSelectId}>
        <span>关联 Target</span>
        <select
          id={targetSelectId}
          aria-label="关联 Target"
          value={draft.targetID}
          disabled={targetsLoading || targets.length === 0}
          onChange={(event) => {
            onDraftChange({ ...draft, targetID: event.target.value })
            onFeedbackClear()
          }}
        >
          <option value="">不关联 Target</option>
          {targets.map((target) => (
            <option key={target.target_id} value={target.target_id}>
              {target.name} · {target.target_id} · {target.host || 'host 未填'} · {target.run_status}
            </option>
          ))}
        </select>
        <small>
          {targetsLoading
            ? '正在读取 Target 列表…'
            : targetsError
              ? `Target 列表不可用：${targetsError}`
              : targets.length === 0
                ? '没有可关联的 Target；可先创建观测入口，或保留为空。'
                : 'Target 仅用于跳转引用，不会创建或修改 ProbeItem。'}
          {' '}
          <Link className="text-link" to="/targets">Target 列表</Link>
        </small>
      </label>
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
      <label className="asset-operation-field asset-operation-field--wide" htmlFor={noteId}>
        <span>域名备注</span>
        <textarea
          id={noteId}
          aria-label="域名备注"
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
