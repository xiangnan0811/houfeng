import type { FormEvent } from 'react'

import { Badge, Button, Input } from '../../components/atoms'
import type { VPSAssetDetail } from '../../lib/types'
import type { LinkDraftState } from './types'

type VPSNodeLinkFormProps = {
  detail: VPSAssetDetail
  draft: LinkDraftState
  controlsDisabled: boolean
  submitting: boolean
  error: string | null
  notice: string | null
  onDraftChange: (draft: LinkDraftState) => void
  onFeedbackClear: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSNodeLinkForm({
  detail,
  draft,
  controlsDisabled,
  submitting,
  error,
  notice,
  onDraftChange,
  onFeedbackClear,
  onSubmit,
}: VPSNodeLinkFormProps) {
  return (
    <form className="asset-operation-form" onSubmit={onSubmit}>
      <div className="asset-operation-form__header">
        <div>
          <h3>关联 Node</h3>
          <p>把资产台账中的 VPS 与观测系统中的 Node 对齐。</p>
        </div>
        <Badge variant="count" tone="neutral">{detail.node_links.length} 个 Node</Badge>
      </div>
      <Input
        label="Node ID"
        value={draft.nodeId}
        onChange={(event) => {
          onDraftChange({ ...draft, nodeId: event.target.value })
          onFeedbackClear()
        }}
        placeholder="nd_..."
        disabled={controlsDisabled}
      />
      <label className="asset-operation-field asset-operation-field--wide">
        <span>关联备注</span>
        <textarea
          value={draft.note}
          onChange={(event) => {
            onDraftChange({ ...draft, note: event.target.value })
            onFeedbackClear()
          }}
          placeholder="例如：主业务 Node"
          disabled={controlsDisabled}
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
        <Button type="submit" disabled={controlsDisabled}>
          {submitting ? '关联中…' : '关联 Node'}
        </Button>
      </div>
    </form>
  )
}
