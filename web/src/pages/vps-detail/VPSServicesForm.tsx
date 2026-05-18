import { useId, type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { Badge, Button, Input } from '../../components/atoms'
import type { AssetServiceStatus, AssetServiceType, TargetRecord } from '../../lib/types'
import type { ServiceDraftState } from './types'
import { SERVICE_STATUS_OPTIONS, SERVICE_TYPE_OPTIONS } from './vpsDetailOptions'

type VPSServicesFormProps = {
  draft: ServiceDraftState
  targets: TargetRecord[]
  targetsLoading: boolean
  targetsError: string | null
  submitting: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onDraftChange: (draft: ServiceDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSServicesForm({
  draft,
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
}: VPSServicesFormProps) {
  const serviceTypeSelectId = useId()
  const serviceStatusSelectId = useId()
  const targetSelectId = useId()
  const noteId = useId()

  return (
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
      <label className="asset-operation-field" htmlFor={serviceTypeSelectId}>
        <span>服务类型</span>
        <select
          id={serviceTypeSelectId}
          aria-label="服务类型"
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
      <label className="asset-operation-field" htmlFor={serviceStatusSelectId}>
        <span>服务状态</span>
        <select
          id={serviceStatusSelectId}
          aria-label="服务状态"
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
        label="服务标签"
        hint="用逗号分隔"
        value={draft.labels}
        onChange={(event) => {
          onDraftChange({ ...draft, labels: event.target.value })
          onFeedbackClear()
        }}
        placeholder="prod, public"
      />
      <label className="asset-operation-field asset-operation-field--wide" htmlFor={noteId}>
        <span>服务备注</span>
        <textarea
          id={noteId}
          aria-label="服务备注"
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
        <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>
          取消
        </Button>
        <Button type="submit" disabled={submitting}>
          {submitting ? '创建中…' : '创建服务记录'}
        </Button>
      </div>
    </form>
  )
}
