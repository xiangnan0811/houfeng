import { useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { Link } from 'react-router-dom'

import { listMonitoringInstances } from '../../lib/api'
import type { MonitoringInstanceRecord } from '../../lib/types'
import {
  TARGET_RUN_STATUS_OPTIONS,
  TARGET_TYPE_OPTIONS,
  distinctSorted,
  parseLabels,
} from './targetHelpers'
import type { CreateTargetFormState } from './types'
import { ExecutorLabelAssign } from './ExecutorLabelAssign'

type CreateTargetPanelProps = {
  form: CreateTargetFormState
  submitting: boolean
  error: string | null
  onCancel: () => void
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
  onCancel,
  onSubmit,
  onFieldChange,
}: CreateTargetPanelProps) {
  const [instances, setInstances] = useState<MonitoringInstanceRecord[] | null>(null)
  const suggestionsRequested = useRef(false)
  const executionLabelsRef = useRef<HTMLInputElement>(null)
  const receivers = instances?.filter(canReceiveProbes) ?? null
  const executors = receivers?.map(executorCandidate) ?? null
  const labelSuggestions = distinctSorted((executors ?? []).flatMap((instance) => instance.labels))
  const executionLabels = parseLabels(form.executionMonitoringInstanceLabels)
  const targetPaused = form.runStatus === '暂停'
  const assignLabel = executionLabels[0]
  const canAssign = Boolean(receivers && receivers.length > 0 && assignLabel && !targetPaused)
    && !(executors ?? []).some((instance) => instance.labels.some((label) => executionLabels.includes(label)))

  function loadLabelSuggestions() {
    if (suggestionsRequested.current) return
    suggestionsRequested.current = true
    void listMonitoringInstances('active')
      .then((records) => {
        setInstances(records)
      })
      .catch(() => {
        setInstances(null)
      })
  }

  // 标签框里按 Enter 只整理已输入的标签，不提交整张表单。
  function commitLabelsOnEnter(
    event: KeyboardEvent<HTMLInputElement>,
    field: 'executionMonitoringInstanceLabels' | 'labels',
  ) {
    if (event.key !== 'Enter' || event.nativeEvent.isComposing) return
    event.preventDefault()
    onFieldChange(field, parseLabels(event.currentTarget.value).join(', '))
  }

  function appendExecutionLabel(label: string) {
    const current = parseLabels(form.executionMonitoringInstanceLabels)
    if (current.includes(label)) return
    onFieldChange('executionMonitoringInstanceLabels', [...current, label].join(', '))
  }

  return (
    <section className="target-create-drawer">
      <p className="target-create-drawer__description">
        填写入口、执行监控实例标签与运行状态，创建后进入目标详情页继续配置探测项。
      </p>
      <form className="target-create-drawer__form" onSubmit={onSubmit}>
        <p>
          <label>
            <span className="target-create-drawer__label--required">目标名称</span>
            <input
              name="name"
              // 星号由 CSS 生成；显式名称让读屏只念字段名，不把装饰星号算进去。
              aria-label="目标名称"
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
            <span className="target-create-drawer__label--required">主机地址</span>
            <input
              name="host"
              aria-label="主机地址"
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
            <span className="target-create-drawer__label--required">执行监控实例标签</span>
            <input
              ref={executionLabelsRef}
              name="executionMonitoringInstanceLabels"
              aria-label="执行监控实例标签"
              aria-required="true"
              value={form.executionMonitoringInstanceLabels}
              aria-describedby="target-execution-label-hint"
              onFocus={loadLabelSuggestions}
              onKeyDown={(event) => commitLabelsOnEnter(event, 'executionMonitoringInstanceLabels')}
              onChange={(event) => onFieldChange('executionMonitoringInstanceLabels', event.target.value)}
            />
          </label>
          <span id="target-execution-label-hint" className="sub">
            带有任一同名标签的监控实例会执行这个目标的探测。
          </span>
          {executors ? (
            <ExecutorPreview
              executors={executors}
              labels={executionLabels}
              targetPaused={targetPaused}
            />
          ) : null}
          {canAssign && receivers && assignLabel ? (
            <ExecutorLabelAssign
              label={assignLabel}
              candidates={receivers}
              onAssigned={(updated) => {
                setInstances((current) => current?.map((instance) => (
                  instance.monitoring_instance_id === updated.monitoring_instance_id ? updated : instance
                )) ?? current)
                // 加标签后这组按钮会消失，焦点回到执行标签输入框。
                executionLabelsRef.current?.focus()
              }}
            />
          ) : null}
          {labelSuggestions.length > 0 ? (
            <span role="group" aria-label="已有监控实例标签">
              {labelSuggestions.map((label) => (
                <button
                  key={label}
                  type="button"
                  className="btn sm secondary"
                  onClick={() => appendExecutionLabel(label)}
                >
                  {label}
                </button>
              ))}
            </span>
          ) : null}
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
            分组
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
              onKeyDown={(event) => commitLabelsOnEnter(event, 'labels')}
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
          <button
            type="button"
            className="btn md secondary"
            disabled={submitting}
            onClick={onCancel}
          >
            取消
          </button>
          <button type="submit" className="btn md primary" disabled={submitting}>
            {submitting ? '正在创建…' : '创建目标'}
          </button>
        </div>
      </form>
    </section>
  )
}

type ExecutorCandidate = { id: string; name: string; labels: string[] }

// 与 agent plan 同口径：已归档、已退役或暂停监控的实例不接收探测任务。
function canReceiveProbes(instance: MonitoringInstanceRecord): boolean {
  return !instance.archived_at && instance.lifecycle_status !== '已退役' && instance.monitoring_status !== '暂停'
}

function executorCandidate(instance: MonitoringInstanceRecord): ExecutorCandidate {
  return { id: instance.monitoring_instance_id, name: instance.display_name, labels: instance.labels }
}

const EXECUTOR_PREVIEW_LIMIT = 3

// 与 agent plan 的标签交集（任一标签重合）同口径，提前告诉用户谁会执行，避免建出没人探测的目标。
function ExecutorPreview({ executors, labels, targetPaused }: { executors: ExecutorCandidate[]; labels: string[]; targetPaused: boolean }) {
  // 只有启用与维护中的目标会分配给实例；暂停目标不点名执行者。
  if (targetPaused) {
    return (
      <span className="target-create-drawer__executors" role="status">
        运行状态为暂停，创建后暂时不会被探测。
      </span>
    )
  }
  if (executors.length === 0) {
    return (
      <span className="target-create-drawer__executors" role="status">
        还没有可执行探测的监控实例（暂停、已归档或已退役的不算）。先<Link to="/monitoring">接入 agent</Link>，目标创建后才会被探测。
      </span>
    )
  }
  if (executors.every((instance) => instance.labels.length === 0)) {
    return (
      <span className="target-create-drawer__executors" role="status">
        现有 {executors.length} 台监控实例都还没有标签。先在这里填一个标签，再给其中一台加上同名标签，或到
        <Link to="/monitoring">监控实例</Link>上补。
      </span>
    )
  }
  if (labels.length === 0) return null
  const matched = executors.filter((instance) => instance.labels.some((label) => labels.includes(label)))
  if (matched.length === 0) {
    return (
      <span className="target-create-drawer__executors target-create-drawer__executors--warn" role="status">
        目前没有监控实例带这些标签，创建后暂时不会被探测。
      </span>
    )
  }
  const names = matched.slice(0, EXECUTOR_PREVIEW_LIMIT).map((instance) => instance.name || instance.id).join('、')
  const rest = matched.length > EXECUTOR_PREVIEW_LIMIT ? ` 等 ${matched.length} 台` : ''
  return (
    <span className="target-create-drawer__executors" role="status">
      将由 {names}{rest} 执行。
    </span>
  )
}
