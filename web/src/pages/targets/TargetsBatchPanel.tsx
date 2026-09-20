import { type KeyboardEvent, useEffect, useId, useRef, useState } from 'react'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'

type BatchMenuItem = {
  key: string
  label: string
  onSelect: () => void
}

type TargetsBatchPanelProps = {
  selectedCount: number
  batchSubmitting: boolean
  batchError: string | null
  pendingBatchAction: string | null
  onBatchAction: (action: string) => void
  onConfirmBatchPause: () => void
  onConfirmBatchArchive: () => void
  onCancelBatchConfirm: () => void
}

export function TargetsBatchPanel({
  selectedCount,
  batchSubmitting,
  batchError,
  pendingBatchAction,
  onBatchAction,
  onConfirmBatchPause,
  onConfirmBatchArchive,
  onCancelBatchConfirm,
}: TargetsBatchPanelProps) {
  const [open, setOpen] = useState(false)
  const baseId = useId()
  const menuId = `${baseId}-menu`
  const triggerId = `${baseId}-trigger`
  const containerRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const triggerDisabled = selectedCount === 0 || batchSubmitting
  const menuOpen = open && selectedCount > 0

  const items: BatchMenuItem[] = [
    { key: 'enter-maintenance', label: '进入维护', onSelect: () => onBatchAction('enter-maintenance') },
    { key: 'exit-maintenance', label: '退出维护', onSelect: () => onBatchAction('exit-maintenance') },
    { key: 'pause', label: '暂停', onSelect: () => onBatchAction('pause') },
    { key: 'resume', label: '恢复', onSelect: () => onBatchAction('resume') },
    { key: 'archive', label: '归档', onSelect: () => onBatchAction('archive') },
  ]

  useEffect(() => {
    if (selectedCount === 0 && open) setOpen(false)
  }, [selectedCount, open])

  useEffect(() => {
    if (!menuOpen) return
    function handlePointerDown(event: MouseEvent) {
      if (!(event.target instanceof Node) || !containerRef.current?.contains(event.target)) {
        setOpen(false)
      }
    }
    function handleKeyDown(event: globalThis.KeyboardEvent) {
      if (event.key !== 'Escape') return
      event.preventDefault()
      setOpen(false)
      triggerRef.current?.focus()
    }
    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [menuOpen])

  function handleTriggerKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (triggerDisabled) return
    if (event.key === 'ArrowDown' || event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      setOpen(true)
    }
  }

  return (
    <>
      <div className="monitoring-batch" ref={containerRef}>
        <button
          ref={triggerRef}
          id={triggerId}
          type="button"
          className="btn sm ghost"
          aria-label="批量操作"
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          aria-controls={menuOpen ? menuId : undefined}
          disabled={triggerDisabled}
          onClick={() => setOpen((current) => !current)}
          onKeyDown={handleTriggerKeyDown}
        >
          {selectedCount > 0 ? `批量操作 (${selectedCount})` : '批量操作'}
        </button>
        {menuOpen ? (
          <div className="monitoring-batch__menu" id={menuId} role="menu" aria-labelledby={triggerId}>
            {items.map((item) => (
              <button
                key={item.key}
                type="button"
                role="menuitem"
                className="monitoring-batch__item"
                disabled={batchSubmitting}
                onClick={() => {
                  setOpen(false)
                  item.onSelect()
                }}
              >
                {item.label}
              </button>
            ))}
          </div>
        ) : null}
        {batchError ? <span className="batch-bar__error">{batchError}</span> : null}
      </div>
      {pendingBatchAction === 'pause' ? (
        <ActionConfirmationModal
          open
          title="确认批量暂停目标"
          current={`将对已选的 ${selectedCount} 个目标执行暂停。`}
          result="操作后：已选目标运行状态变为暂停。"
          impact="会停止这些目标下所有 ProbeItem 的执行，不再产生新的入口探测记录。"
          unchanged="不会删除历史事件、观测记录或 ProbeItem 配置。"
          confirmLabel="确认批量暂停"
          disabled={batchSubmitting}
          onConfirm={onConfirmBatchPause}
          onCancel={onCancelBatchConfirm}
        />
      ) : null}
      {pendingBatchAction === 'archive' ? (
        <ActionConfirmationModal
          open
          title="确认批量归档目标"
          current={`将对已选的 ${selectedCount} 个目标执行归档。`}
          result="操作后：已选目标退出默认工作集，变为归档对象。"
          impact="归档后不再作为活跃入口探测，需要恢复后才能继续观测。"
          unchanged="不会删除历史观测、事件或 ProbeItem 配置。"
          confirmLabel="确认批量归档"
          disabled={batchSubmitting}
          onConfirm={onConfirmBatchArchive}
          onCancel={onCancelBatchConfirm}
        />
      ) : null}
    </>
  )
}
