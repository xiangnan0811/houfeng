import { useId, type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Button, Input } from '../../components/atoms'
import type { ProviderRecord, VPSUsageStatus } from '../../lib/types'
import type { FactEditFormState } from './types'
import { USAGE_OPTIONS } from './vpsDetailOptions'

type VPSFactsEditFormProps = {
  draft: FactEditFormState
  providers: ProviderRecord[]
  providersLoading: boolean
  providersError: string | null
  submitting: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onDraftChange: (draft: FactEditFormState) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSFactsEditForm({
  draft,
  providers,
  providersLoading,
  providersError,
  submitting,
  error,
  notice,
  onCancel,
  onDraftChange,
  onSubmit,
}: VPSFactsEditFormProps) {
  const providerSelectId = useId()
  const usageSelectId = useId()
  const noteId = useId()

  function handleProviderChange(providerID: string) {
    const provider = providers.find((item) => item.provider_id === providerID)
    onDraftChange({
      ...draft,
      providerID,
      providerName: provider ? provider.name : draft.providerName,
    })
  }

  return (
    <form className="asset-facts-edit-form" onSubmit={onSubmit}>
      <Input label="VPS 名称" value={draft.displayName} onChange={(event) => onDraftChange({ ...draft, displayName: event.target.value })} />
      <label className="input-field asset-facts-edit-form__wide" htmlFor={providerSelectId}>
        <span className="input-field__label">资产服务商</span>
        <select
          id={providerSelectId}
          aria-label="资产服务商"
          className="input"
          value={draft.providerID}
          disabled={providersLoading}
          onChange={(event) => handleProviderChange(event.target.value)}
        >
          <option value="">未关联服务商</option>
          {providers.map((provider) => (
            <option key={provider.provider_id} value={provider.provider_id}>
              {provider.name} · {provider.country || '地区未填'} · {provider.provider_id}
            </option>
          ))}
        </select>
        <span className="input-field__hint">
          {providersLoading
            ? '正在读取服务商…'
            : providersError
              ? `服务商不可用：${providersError}`
              : providers.length === 0
                ? '还没有服务商主数据，请先创建或保留名称快照。'
                : '选择服务商会同步更新名称快照，仍可手动修正快照。'}
          {' '}
          <Link className="text-link" to="/providers">服务商列表</Link>
        </span>
      </label>
      <Input label="服务商名称快照" value={draft.providerName} onChange={(event) => onDraftChange({ ...draft, providerName: event.target.value })} />
      <Input label="产品名" value={draft.productName} onChange={(event) => onDraftChange({ ...draft, productName: event.target.value })} />
      <Input label="订单号" value={draft.orderRef} onChange={(event) => onDraftChange({ ...draft, orderRef: event.target.value })} />
      <Input label="国家 / 地区" value={draft.country} onChange={(event) => onDraftChange({ ...draft, country: event.target.value })} />
      <Input label="区域" value={draft.region} onChange={(event) => onDraftChange({ ...draft, region: event.target.value })} />
      <Input label="城市" value={draft.city} onChange={(event) => onDraftChange({ ...draft, city: event.target.value })} />
      <Input label="数据中心" value={draft.datacenter} onChange={(event) => onDraftChange({ ...draft, datacenter: event.target.value })} />
      <Input label="IPv4" value={draft.ipv4} onChange={(event) => onDraftChange({ ...draft, ipv4: event.target.value })} />
      <Input label="IPv6" value={draft.ipv6} onChange={(event) => onDraftChange({ ...draft, ipv6: event.target.value })} />
      <Input label="SSH Host" value={draft.sshHost} onChange={(event) => onDraftChange({ ...draft, sshHost: event.target.value })} />
      <Input label="SSH 端口" type="number" min="1" max="65535" value={draft.sshPort} onChange={(event) => onDraftChange({ ...draft, sshPort: event.target.value })} />
      <Input label="SSH 用户" value={draft.sshUser} onChange={(event) => onDraftChange({ ...draft, sshUser: event.target.value })} />
      <Input label="操作系统" value={draft.osName} onChange={(event) => onDraftChange({ ...draft, osName: event.target.value })} />
      <Input label="虚拟化" value={draft.virtualization} onChange={(event) => onDraftChange({ ...draft, virtualization: event.target.value })} />
      <label className="input-field" htmlFor={usageSelectId}>
        <span className="input-field__label">用途状态</span>
        <select
          id={usageSelectId}
          aria-label="用途状态"
          className="input"
          value={draft.usageStatus}
          onChange={(event) => onDraftChange({
            ...draft,
            usageStatus: event.target.value as VPSUsageStatus,
          })}
        >
          {USAGE_OPTIONS.map(([value, label]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </select>
      </label>
      <Input label="重要性" value={draft.importance} onChange={(event) => onDraftChange({ ...draft, importance: event.target.value })} />
      <Input label="标签" hint="用逗号分隔" value={draft.labels} onChange={(event) => onDraftChange({ ...draft, labels: event.target.value })} />
      <label className="input-field asset-facts-edit-form__wide" htmlFor={noteId}>
        <span className="input-field__label">备注</span>
        <div className="input-field__shell">
          <input
            id={noteId}
            className="input"
            value={draft.note}
            onChange={(event) => onDraftChange({ ...draft, note: event.target.value })}
          />
        </div>
      </label>
      {error ? (
        <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
          {error}
        </p>
      ) : notice ? (
        <p className="asset-operation-feedback" role="status">{notice}</p>
      ) : null}
      <div className="page-form-actions">
        <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>
          取消编辑
        </Button>
        <Button type="submit" disabled={submitting}>
          {submitting ? '保存中…' : '保存基础信息'}
        </Button>
      </div>
    </form>
  )
}
