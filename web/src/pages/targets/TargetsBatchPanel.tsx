import { ActionConfirmationCard } from '../../components/ActionConfirmationCard'

type TargetsBatchPanelProps = {
  show: boolean
  filteredTargetCount: number
  selectAll: boolean
  batchSubmitting: boolean
  batchError: string | null
  pendingBatchAction: string | null
  onSelectAllChange: (checked: boolean) => void
  onBatchAction: (action: string) => void
  onConfirmBatchPause: () => void
  onCancelBatchPause: () => void
}

export function TargetsBatchPanel({
  show,
  filteredTargetCount,
  selectAll,
  batchSubmitting,
  batchError,
  pendingBatchAction,
  onSelectAllChange,
  onBatchAction,
  onConfirmBatchPause,
  onCancelBatchPause,
}: TargetsBatchPanelProps) {
  return (
    <>
      {show ? (
        <div className={`batch-bar${selectAll ? ' batch-bar--active' : ''}`}>
          <label className="batch-bar__toggle">
            <input
              type="checkbox"
              checked={selectAll}
              onChange={(event) => onSelectAllChange(event.target.checked)}
            />
            全选 ({filteredTargetCount})
          </label>
          <span className="batch-bar__scope">批量范围：当前筛选范围内的 {filteredTargetCount} 个目标</span>
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
                暂停
              </button>
              <button
                className="btn sm secondary"
                disabled={batchSubmitting}
                onClick={() => onBatchAction('resume')}
              >
                恢复
              </button>
            </div>
          ) : null}
          {batchError ? <span className="batch-bar__error">{batchError}</span> : null}
          {batchSubmitting ? <span>批量操作中…</span> : null}
        </div>
      ) : null}
      {pendingBatchAction === 'pause' ? (
        <ActionConfirmationCard
          title="确认批量暂停目标"
          current={`将对当前筛选范围内的 ${filteredTargetCount} 个目标执行暂停操作。`}
          result="操作后：所有已选目标运行状态变为暂停。"
          impact="会停止这些目标下所有 ProbeItem 的执行，不再产生新的目标观测记录。"
          unchanged="不会删除历史事件、观测记录或 ProbeItem 配置。"
          confirmLabel="确认批量暂停"
          disabled={batchSubmitting}
          onConfirm={onConfirmBatchPause}
          onCancel={onCancelBatchPause}
        />
      ) : null}
    </>
  )
}
