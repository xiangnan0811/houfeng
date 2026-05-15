import type { FormEvent } from 'react'

import { Badge, Button, Input } from '../../components/atoms'
import type { AssetServiceStatus, AssetServiceType } from '../../lib/types'
import type { ServiceDraftState } from './types'
import { SERVICE_STATUS_OPTIONS, SERVICE_TYPE_OPTIONS } from './vpsDetailOptions'

type VPSServicesFormProps = {
  draft: ServiceDraftState
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
  submitting,
  error,
  notice,
  onCancel,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSServicesFormProps) {
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
