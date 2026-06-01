import { ActionConfirmationCard } from '../../components/ActionConfirmationCard'

type MonitoringInstancesBatchPanelProps = {
  hasActiveFilters: boolean
  filteredMonitoringInstanceCount: number
  selectAll: boolean
  batchSubmitting: boolean
  batchError: string | null
  commandOpen: boolean
  commandID: string
  pendingBatchAction: string | null
  onSelectAllChange: (checked: boolean) => void
  onBatchAction: (action: string) => void
  onCommandOpenChange: (open: boolean) => void
  onCommandIDChange: (commandID: string) => void
  onExecuteBatchCommand: () => void
  onConfirmBatchPause: () => void
  onCancelBatchPause: () => void
}

export function MonitoringInstancesBatchPanel({
  hasActiveFilters,
  filteredMonitoringInstanceCount,
  selectAll,
  batchSubmitting,
  batchError,
  commandOpen,
  commandID,
  pendingBatchAction,
  onSelectAllChange,
  onBatchAction,
  onCommandOpenChange,
  onCommandIDChange,
  onExecuteBatchCommand,
  onConfirmBatchPause,
  onCancelBatchPause,
}: MonitoringInstancesBatchPanelProps) {
  return (
    <>
      {filteredMonitoringInstanceCount > 0 ? (
        <div className={`batch-bar${selectAll ? ' batch-bar--active' : ''}`}>
          <label className="batch-bar__toggle">
            <input
              type="checkbox"
              checked={selectAll}
              onChange={(event) => onSelectAllChange(event.target.checked)}
            />
            全选 ({filteredMonitoringInstanceCount})
          </label>
          <span className="batch-bar__scope">
            批量范围：{hasActiveFilters ? '当前筛选范围内' : '完整列表中的'} {filteredMonitoringInstanceCount} 个监控实例
          </span>
          {selectAll ? (
            <div className="batch-bar__actions">
              <button
                className="btn sm secondary"
                disabled={batchSubmitting}
                onClick={() => onBatchAction('enter-maintenance')}
              >
                进入维护
              </button>
              <button
                className="btn sm secondary"
                disabled={batchSubmitting}
                onClick={() => onBatchAction('exit-maintenance')}
              >
                退出维护
              </button>
              <button
                className="btn sm secondary"
                disabled={batchSubmitting}
                onClick={() => onBatchAction('pause')}
              >
                暂停监控
              </button>
              <button
                className="btn sm secondary"
                disabled={batchSubmitting}
                onClick={() => onBatchAction('resume')}
              >
                恢复监控
              </button>
              <button
                className="btn sm secondary"
                disabled={batchSubmitting}
                onClick={() => onCommandOpenChange(true)}
              >
                执行命令…
              </button>
            </div>
          ) : null}
          {batchError ? <span className="batch-bar__error">{batchError}</span> : null}
          {batchSubmitting ? <span>批量操作中…</span> : null}
        </div>
      ) : null}

      {commandOpen ? (
        <div className="page-panel">
          <p className="page-panel__eyebrow">批量命令执行</p>
          <h3 className="page-panel__title">下发命令到已选监控实例</h3>
          <p className="page-panel__description">
            将对当前筛选范围内的 {filteredMonitoringInstanceCount} 个监控实例下发命令。请输入命令 ID。
          </p>
          <p>
            <label>
              命令 ID
              <input
                value={commandID}
                onChange={(event) => onCommandIDChange(event.target.value)}
                placeholder="例如：whoami"
              />
            </label>
          </p>
          <div className="batch-command__actions">
            <button
              className="btn md primary"
              disabled={!commandID.trim() || batchSubmitting}
              onClick={onExecuteBatchCommand}
            >
              下发命令
            </button>
            <button
              className="btn md ghost"
              onClick={() => {
                onCommandOpenChange(false)
                onCommandIDChange('')
              }}
            >
              取消
            </button>
          </div>
        </div>
      ) : null}

      {pendingBatchAction === 'pause' ? (
        <ActionConfirmationCard
          title="确认批量暂停监控实例监控"
          current={`将对当前筛选范围内的 ${filteredMonitoringInstanceCount} 个监控实例执行暂停操作。`}
          result="操作后：所有已选监控实例的监控运行状态变为暂停。"
          impact="会停止主机指标采集，并停止这些监控实例承担的探针执行。趋势图会从此开始出现数据空档。"
          unchanged="不会删除历史事件、观测记录或 agent 绑定关系。"
          confirmLabel="确认批量暂停监控"
          disabled={batchSubmitting}
          onConfirm={onConfirmBatchPause}
          onCancel={onCancelBatchPause}
        />
      ) : null}
    </>
  )
}
