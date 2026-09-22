import { type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Input, Select } from '../../components/atoms'
import type { AssetServiceStatus, AssetServiceType, TargetRecord } from '../../lib/types'
import type { ServiceDraftState } from './types'
import { SERVICE_STATUS_OPTIONS, SERVICE_TYPE_OPTIONS } from './vpsDetailOptions'

type VPSServicesFormProps = {
  formId: string
  draft: ServiceDraftState
  targets: TargetRecord[]
  targetsLoading: boolean
  targetsError: string | null
  submitting: boolean
  onDraftChange: (draft: ServiceDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSServicesForm({
  formId,
  draft,
  targets,
  targetsLoading,
  targetsError,
  submitting,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSServicesFormProps) {
  function update<K extends keyof ServiceDraftState>(key: K, value: ServiceDraftState[K]) {
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
          label="服务名称"
          value={draft.name}
          onChange={(event) => update('name', event.target.value)}
          placeholder="例如：Blog"
        />
        <Select
          label="服务类型"
          value={draft.serviceType}
          onChange={(event) => update('serviceType', event.target.value as AssetServiceType)}
        >
          {SERVICE_TYPE_OPTIONS.map(([value, label]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </Select>
        <Select
          label="服务状态"
          value={draft.status}
          onChange={(event) => update('status', event.target.value as AssetServiceStatus)}
        >
          {SERVICE_STATUS_OPTIONS.map(([value, label]) => (
            <option key={value} value={value}>{label}</option>
          ))}
        </Select>
        <Input
          label="入口 URL"
          type="url"
          value={draft.url}
          onChange={(event) => update('url', event.target.value)}
          placeholder="https://example.com"
        />
        <Input
          label="端口"
          type="number"
          min="1"
          max="65535"
          value={draft.port}
          onChange={(event) => update('port', event.target.value)}
          placeholder="443"
        />
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
      </div>
      <Input
        label="服务标签"
        hint="用逗号分隔"
        value={draft.labels}
        onChange={(event) => update('labels', event.target.value)}
        placeholder="prod, public"
      />
      <label className="input-field">
        <span className="input-field__label">服务备注</span>
        <textarea
          className="input"
          aria-label="服务备注"
          value={draft.note}
          onChange={(event) => update('note', event.target.value)}
          placeholder="例如：主站反代到本机 3000 端口"
        />
      </label>
    </form>
  )
}
