import { type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Input, Select } from '../../components/atoms'
import type { AssetDomainStatus, AssetServiceRecord, TargetRecord } from '../../lib/types'
import type { DomainDraftState } from './types'
import { DOMAIN_STATUS_OPTIONS } from './vpsDetailOptions'

type VPSDomainsFormProps = {
  formId: string
  draft: DomainDraftState
  services: AssetServiceRecord[]
  targets: TargetRecord[]
  targetsLoading: boolean
  targetsError: string | null
  submitting: boolean
  onDraftChange: (draft: DomainDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSDomainsForm({
  formId,
  draft,
  services,
  targets,
  targetsLoading,
  targetsError,
  submitting,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSDomainsFormProps) {
  function update<K extends keyof DomainDraftState>(key: K, value: DomainDraftState[K]) {
    onDraftChange({ ...draft, [key]: value })
    onFeedbackClear()
  }

  const targetHint = targetsLoading
    ? '正在读取入口探测列表…'
    : targetsError
      ? `入口探测列表不可用：${targetsError}`
      : targets.length === 0
        ? '没有可关联的入口探测；可先创建观测入口，或保留为空。'
        : '入口探测仅用于跳转引用，不会创建或修改 ProbeItem。'

  return (
    <form id={formId} className="vps-form" onSubmit={onSubmit} aria-busy={submitting}>
      <div className="vps-form-grid">
        <Input
          label="域名"
          value={draft.domainName}
          onChange={(event) => update('domainName', event.target.value)}
          placeholder="www.example.com"
        />
        <Select
          label="域名状态"
          value={draft.status}
          onChange={(event) => update('status', event.target.value as AssetDomainStatus)}
        >
          {DOMAIN_STATUS_OPTIONS.map(([value, label]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </Select>
        <Input
          label="用途"
          value={draft.purpose}
          onChange={(event) => update('purpose', event.target.value)}
          placeholder="官网 / API / 回源"
        />
        <Select
          label="关联服务"
          value={draft.serviceID}
          disabled={services.length === 0}
          onChange={(event) => update('serviceID', event.target.value)}
          hint={services.length === 0 ? '当前 VPS 还没有服务记录，可先创建服务或保留为空。' : '仅关联当前 VPS 的服务记录。'}
        >
          <option value="">不关联服务</option>
          {services.map((service) => (
            <option key={service.service_id} value={service.service_id}>
              {service.name} · {service.service_id} · {service.status}
            </option>
          ))}
        </Select>
        <Select
          label="关联入口探测"
          value={draft.targetID}
          disabled={targetsLoading || targets.length === 0}
          onChange={(event) => update('targetID', event.target.value)}
          hint={(
            <>
              {targetHint}
              {' '}
              <Link className="text-link" to="/targets">入口探测列表</Link>
            </>
          )}
        >
          <option value="">不关联入口探测</option>
          {targets.map((target) => (
            <option key={target.target_id} value={target.target_id}>
              {target.name} · {target.target_id} · {target.host || 'host 未填'} · {target.run_status}
            </option>
          ))}
        </Select>
        <Input
          label="注册商"
          value={draft.registrar}
          onChange={(event) => update('registrar', event.target.value)}
          placeholder="NameSilo"
        />
        <Input
          label="过期日期"
          type="date"
          value={draft.expiresAt}
          onChange={(event) => update('expiresAt', event.target.value)}
        />
      </div>
      <div className="vps-inline">
        <label>
          <input
            type="checkbox"
            checked={draft.autoRenew}
            onChange={(event) => update('autoRenew', event.target.checked)}
          />
          自动续费
        </label>
        <label>
          <input
            type="checkbox"
            checked={draft.httpsEnabled}
            onChange={(event) => update('httpsEnabled', event.target.checked)}
          />
          已启用 HTTPS
        </label>
      </div>
      <Input
        label="域名标签"
        hint="用逗号分隔"
        value={draft.labels}
        onChange={(event) => update('labels', event.target.value)}
        placeholder="prod, public"
      />
      <label className="input-field">
        <span className="input-field__label">域名备注</span>
        <textarea
          className="input"
          aria-label="域名备注"
          value={draft.note}
          onChange={(event) => update('note', event.target.value)}
          placeholder="例如：Cloudflare 代理到主站服务"
        />
      </label>
    </form>
  )
}
