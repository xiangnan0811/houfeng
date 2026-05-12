import type { FormEvent } from 'react'

import {
  TARGET_RUN_STATUS_OPTIONS,
  TARGET_TYPE_OPTIONS,
} from './targetHelpers'
import type { CreateTargetFormState } from './types'

type CreateTargetPanelProps = {
  form: CreateTargetFormState
  submitting: boolean
  error: string | null
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onFieldChange: <K extends keyof CreateTargetFormState>(
    field: K,
    value: CreateTargetFormState[K],
  ) => void
}

export function CreateTargetPanel({
  form,
  submitting,
  error,
  onSubmit,
  onFieldChange,
}: CreateTargetPanelProps) {
  return (
    <section className="page-panel">
      <p className="page-panel__eyebrow">目标创建</p>
      <h3 className="page-panel__title">创建目标</h3>
      <p className="page-panel__description">
        填写入口、执行节点标签与运行状态，创建后进入目标详情页继续配置 ProbeItem。
      </p>
      <form onSubmit={onSubmit}>
        <p>
          <label>
            目标名称
            <input
              name="name"
              value={form.name}
              onChange={(event) => onFieldChange('name', event.target.value)}
              required
            />
          </label>
        </p>
        <p>
          <label>
            目标类型
            <select
              name="targetType"
              value={form.targetType}
              onChange={(event) =>
                onFieldChange(
                  'targetType',
                  event.target.value as CreateTargetFormState['targetType'],
                )
              }
            >
              {TARGET_TYPE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
        </p>
        <p>
          <label>
            主机地址
            <input
              name="host"
              value={form.host}
              onChange={(event) => onFieldChange('host', event.target.value)}
              required
            />
          </label>
        </p>
        <p>
          <label>
            基础端口
            <input
              name="basePort"
              inputMode="numeric"
              value={form.basePort}
              onChange={(event) => onFieldChange('basePort', event.target.value)}
            />
          </label>
        </p>
        <p>
          <label>
            执行节点标签
            <input
              name="executionNodeLabels"
              value={form.executionNodeLabels}
              onChange={(event) => onFieldChange('executionNodeLabels', event.target.value)}
            />
          </label>
        </p>
        <p>
          <label>
            运行状态
            <select
              name="runStatus"
              value={form.runStatus}
              onChange={(event) =>
                onFieldChange(
                  'runStatus',
                  event.target.value as CreateTargetFormState['runStatus'],
                )
              }
            >
              {TARGET_RUN_STATUS_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
        </p>
        <p>
          <label>
            Group
            <input
              name="group"
              value={form.group}
              onChange={(event) => onFieldChange('group', event.target.value)}
            />
          </label>
        </p>
        <p>
          <label>
            目标标签
            <input
              name="labels"
              value={form.labels}
              onChange={(event) => onFieldChange('labels', event.target.value)}
            />
          </label>
        </p>
        <p>
          <label>
            备注
            <textarea
              name="note"
              value={form.note}
              onChange={(event) => onFieldChange('note', event.target.value)}
              rows={3}
            />
          </label>
        </p>
        {error ? (
          <p className="create-form__error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="page-form-actions">
          <button type="submit" className="btn btn--primary btn--md" disabled={submitting}>
            {submitting ? '正在创建…' : '创建目标'}
          </button>
        </div>
      </form>
    </section>
  )
}
