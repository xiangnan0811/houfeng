import type { FormEvent } from 'react'

import { Button } from '../../components/atoms'
import { formatLabelList } from '../../lib/format'
import type { MonitoringInstanceRecord } from '../../lib/types'

type MonitoringInstanceMetadataSectionProps = {
  monitoringInstance: MonitoringInstanceRecord
  editing: boolean
  groupDraft: string
  labelDraft: string
  noteDraft: string
  submitting: boolean
  error: string | null
  onGroupDraftChange: (value: string) => void
  onLabelDraftChange: (value: string) => void
  onNoteDraftChange: (value: string) => void
  onStartEdit: () => void
  onCancelEdit: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function MonitoringInstanceMetadataSection({
  monitoringInstance,
  editing,
  groupDraft,
  labelDraft,
  noteDraft,
  submitting,
  error,
  onGroupDraftChange,
  onLabelDraftChange,
  onNoteDraftChange,
  onStartEdit,
  onCancelEdit,
  onSubmit,
}: MonitoringInstanceMetadataSectionProps) {
  const itemClass = [
    'watchtower-property-item',
    editing && 'watchtower-property-item--editing',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <details className="watchtower-secondary">
      <summary>标签与备注</summary>
      <div className="watchtower-secondary__body">
        <div className={itemClass}>
          {editing ? (
            <form onSubmit={onSubmit} className="page-stack watchtower-property-item__form">
              <label>
                Group
                <input
                  name="metadata-group"
                  value={groupDraft}
                  onChange={(event) => onGroupDraftChange(event.target.value)}
                />
              </label>
              <label>
                标签
                <input
                  name="metadata-labels"
                  value={labelDraft}
                  onChange={(event) => onLabelDraftChange(event.target.value)}
                />
              </label>
              <label>
                备注
                <textarea
                  name="metadata-note"
                  value={noteDraft}
                  onChange={(event) => onNoteDraftChange(event.target.value)}
                  rows={3}
                />
              </label>
              <div className="badge-row badge-row--wrap">
                <Button type="submit" variant="primary" disabled={submitting}>
                  {submitting ? '正在保存…' : '保存标签与备注'}
                </Button>
                <Button type="button" variant="ghost" disabled={submitting} onClick={onCancelEdit}>
                  取消
                </Button>
              </div>
              {error ? (
                <p role="alert" aria-live="assertive" className="watchtower-property-item__error">
                  {error}
                </p>
              ) : null}
            </form>
          ) : (
            <>
              <div className="watchtower-property-item__main">
                <h3 className="watchtower-property-item__title">标签与备注</h3>
                {monitoringInstance.group ? (
                  <span className="watchtower-property-item__desc">Group：{monitoringInstance.group}</span>
                ) : null}
                <span className="watchtower-property-item__desc">
                  标签：{formatLabelList(monitoringInstance.labels)}
                </span>
                <span className="watchtower-property-item__desc">
                  备注：{monitoringInstance.note.trim() || '暂无备注'}
                </span>
                {error ? (
                  <span role="alert" aria-live="assertive" className="watchtower-property-item__error">
                    {error}
                  </span>
                ) : null}
              </div>
              <div className="watchtower-property-item__actions">
                <Button variant="secondary" onClick={onStartEdit}>
                  编辑标签与备注
                </Button>
              </div>
            </>
          )}
        </div>
      </div>
    </details>
  )
}
