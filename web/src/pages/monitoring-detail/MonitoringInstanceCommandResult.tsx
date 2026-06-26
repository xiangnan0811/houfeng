import { useState } from 'react'

import { StatusBadge } from '../../components/StatusBadge'
import type { MonitoringInstanceRecord } from '../../lib/types'
import { MAX_STDOUT_LINES } from './monitoringDetailConstants'

type MonitoringInstanceCommandResultProps = {
  action: NonNullable<MonitoringInstanceRecord['last_action']>
  labels: Record<string, string>
}

export function MonitoringInstanceCommandResult({ action, labels }: MonitoringInstanceCommandResultProps) {
  const [stdoutExpanded, setStdoutExpanded] = useState(false)
  const commandLabel = labels[action.command_id] ?? action.command_id

  if (action.status === 'pending') {
    return (
      <div className="command-result">
        <h4>{commandLabel} · 等待 agent 执行…</h4>
        <p className="command-pending">
          已下发，等待 agent 执行…（约 30s-60s，取决于 sync 间隔）
        </p>
      </div>
    )
  }

  const stdout = action.output_expired ? '' : (action.stdout ?? '')
  const stderr = action.output_expired ? '' : (action.stderr ?? '')
  const exitCode = action.exit_code
  const stdoutLines = stdout.split('\n')
  const stdoutTruncated = stdoutLines.length > MAX_STDOUT_LINES

  return (
    <div className="command-result">
      <h4>
        {commandLabel} · 已完成{' '}
        <StatusBadge
          label={exitCode === 0 ? 'exit 0' : `exit ${exitCode ?? '?'}`}
          tone={exitCode === 0 ? 'green' : 'red'}
        />
      </h4>

      {action.output_expired ? (
        <p className="command-output-expired">
          命令输出已过期。退出码和执行记录已保留，stdout / stderr 已按保留策略清理。
        </p>
      ) : null}

      {stdout ? (
        <div className="command-output-section">
          <p className="command-output-section__label">stdout</p>
          <pre className="command-output">
            <code>
              {(stdoutTruncated && !stdoutExpanded
                ? stdoutLines.slice(0, MAX_STDOUT_LINES)
                : stdoutLines
              ).join('\n')}
            </code>
          </pre>
          {stdoutTruncated ? (
            <button
              type="button"
              className="text-link command-output__expand"
              onClick={() => setStdoutExpanded(!stdoutExpanded)}
            >
              {stdoutExpanded
                ? '收起'
                : `展开全部（${stdoutLines.length} 行）`}
            </button>
          ) : null}
        </div>
      ) : null}

      {stderr ? (
        <div className="command-output-section command-output-section--stderr">
          <p className="command-output-section__label">stderr</p>
          <pre className="command-output command-output--stderr">
            <code>{stderr}</code>
          </pre>
        </div>
      ) : null}
    </div>
  )
}
