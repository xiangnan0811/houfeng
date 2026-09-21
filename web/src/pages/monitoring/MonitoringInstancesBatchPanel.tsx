import { type KeyboardEvent, useEffect, useId, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { ActionConfirmationModal } from '../../components/ActionConfirmationModal'
import { Button, Modal } from '../../components/atoms'
import { COMMAND_LIST, type MonitoringInstanceCommand } from '../../config/commands'
import './MonitoringCommands.css'

type BatchMenuItem = {
  key: string
  label: string
  href?: string
  onSelect?: () => void
}

type MonitoringInstancesBatchPanelProps = {
  selectedCount: number
  confirmedBatchCount: number
  batchSubmitting: boolean
  batchError: string | null
  commandOpen: boolean
  commandID: string
  pendingBatchAction: string | null
  navigationLocked: boolean
  compareHref: string
  compareState: object
  onBatchAction: (action: string) => void
  onCommandOpenChange: (open: boolean) => void
  onCommandIDChange: (commandID: string) => void
  onExecuteBatchCommand: (commandId: string, options?: { confirmedSensitive?: boolean }) => void
  onConfirmBatchPause: () => void
  onCancelBatchPause: () => void
}

export function MonitoringInstancesBatchPanel({
  selectedCount,
  confirmedBatchCount,
  batchSubmitting,
  batchError,
  commandOpen,
  commandID,
  pendingBatchAction,
  navigationLocked,
  compareHref,
  compareState,
  onBatchAction,
  onCommandOpenChange,
  onCommandIDChange,
  onExecuteBatchCommand,
  onConfirmBatchPause,
  onCancelBatchPause,
}: MonitoringInstancesBatchPanelProps) {
  const [pendingSensitiveCommand, setPendingSensitiveCommand] = useState<MonitoringInstanceCommand | null>(null)
  const [open, setOpen] = useState(false)
  const baseId = useId()
  const menuId = `${baseId}-menu`
  const triggerId = `${baseId}-trigger`
  const containerRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const itemRefs = useRef<Array<HTMLButtonElement | HTMLAnchorElement | null>>([])
  const pendingFocusIndex = useRef(0)
  const actionsDisabled = batchSubmitting || navigationLocked
  const triggerDisabled = selectedCount === 0 || batchSubmitting

  const items: BatchMenuItem[] = [
    { key: 'enter-maintenance', label: '进入维护', onSelect: () => onBatchAction('enter-maintenance') },
    { key: 'exit-maintenance', label: '退出维护', onSelect: () => onBatchAction('exit-maintenance') },
    { key: 'pause', label: '暂停监控', onSelect: () => onBatchAction('pause') },
    { key: 'resume', label: '恢复监控', onSelect: () => onBatchAction('resume') },
    { key: 'command', label: '执行命令…', onSelect: () => onCommandOpenChange(true) },
    ...(selectedCount === 2 ? [{ key: 'compare', label: '对比', href: compareHref }] : []),
  ]

  if (selectedCount === 0 && open) setOpen(false)
  const menuOpen = open && selectedCount > 0

  useEffect(() => {
    if (!menuOpen) return
    itemRefs.current[pendingFocusIndex.current]?.focus()
  }, [menuOpen])

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

  function openMenu(index: number) {
    pendingFocusIndex.current = index
    setOpen(true)
  }

  function closeAndRestoreFocus() {
    setOpen(false)
    triggerRef.current?.focus()
  }

  function handleTriggerKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (triggerDisabled) return
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    event.preventDefault()
    openMenu(event.key === 'ArrowDown' ? 0 : items.length - 1)
  }

  function handleItemKeyDown(event: KeyboardEvent<HTMLElement>, index: number) {
    let nextIndex: number
    switch (event.key) {
      case 'ArrowDown':
        nextIndex = (index + 1) % items.length
        break
      case 'ArrowUp':
        nextIndex = (index - 1 + items.length) % items.length
        break
      case 'Home':
        nextIndex = 0
        break
      case 'End':
        nextIndex = items.length - 1
        break
      case 'Escape':
        event.preventDefault()
        closeAndRestoreFocus()
        return
      case 'Tab':
        setOpen(false)
        return
      default:
        return
    }
    event.preventDefault()
    itemRefs.current[nextIndex]?.focus()
  }

  function runCommand(command: () => void) {
    setOpen(false)
    command()
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
          aria-controls={menuId}
          aria-expanded={menuOpen}
          disabled={triggerDisabled}
          onClick={() => {
            if (triggerDisabled) return
            if (menuOpen) setOpen(false)
            else openMenu(0)
          }}
          onKeyDown={handleTriggerKeyDown}
        >
          批量操作{selectedCount > 0 ? ` (${selectedCount})` : ''}
        </button>
        {menuOpen ? (
          <div id={menuId} className="monitoring-batch__menu" role="menu" aria-labelledby={triggerId}>
            {items.map((item, index) => {
              if (item.href) {
                return (
                  <Link
                    key={item.key}
                    ref={(node) => {
                      itemRefs.current[index] = node
                    }}
                    className="monitoring-batch__item"
                    role="menuitem"
                    to={item.href}
                    state={compareState}
                    onClick={() => setOpen(false)}
                    onKeyDown={(event) => handleItemKeyDown(event, index)}
                  >
                    {item.label}
                  </Link>
                )
              }
              return (
                <button
                  key={item.key}
                  ref={(node) => {
                    itemRefs.current[index] = node
                  }}
                  type="button"
                  className="monitoring-batch__item"
                  role="menuitem"
                  disabled={actionsDisabled}
                  onClick={() => {
                    if (item.onSelect) runCommand(item.onSelect)
                  }}
                  onKeyDown={(event) => handleItemKeyDown(event, index)}
                >
                  {item.label}
                </button>
              )
            })}
          </div>
        ) : null}
        {batchError ? <span className="monitoring-batch__error" role="alert">{batchError}</span> : null}
        {batchSubmitting ? <span className="monitoring-batch__status">批量操作中…</span> : null}
      </div>

      <Modal
        open={commandOpen}
        onClose={() => {
          if (batchSubmitting) return
          setPendingSensitiveCommand(null)
          onCommandOpenChange(false)
          onCommandIDChange('')
        }}
        title="下发命令到已选监控实例"
        ariaLabel="批量执行命令"
        size="md"
        persistent={batchSubmitting}
      >
        <div className="monitoring-detail-commands">
          <p className="monitoring-detail-dialog__subject">
            将对确认时选中的 {confirmedBatchCount} 个监控实例下发命令。命令由 agent 编译期白名单执行，不接受自定义参数。
          </p>
          <div className="monitoring-detail-commands__list">
            {COMMAND_LIST.map((command) => (
              <button
                key={command.id}
                type="button"
                className="monitoring-detail-commands__item"
                aria-pressed={command.id === commandID}
                disabled={batchSubmitting}
                onClick={() => {
                  onCommandIDChange(command.id)
                  if (command.sensitivity === 'sensitive') {
                    setPendingSensitiveCommand(command)
                    return
                  }
                  onExecuteBatchCommand(command.id)
                }}
              >
                <span className="monitoring-detail-commands__name">
                  {command.name}
                  {command.sensitivity === 'sensitive' ? (
                    <span className="monitoring-detail-commands__sensitive">敏感</span>
                  ) : null}
                </span>
                <span className="monitoring-detail-commands__desc">{command.description}</span>
              </button>
            ))}
          </div>
          <div className="action-confirm__actions">
            <Button
              variant="secondary"
              disabled={batchSubmitting}
              onClick={() => {
                if (batchSubmitting) return
                setPendingSensitiveCommand(null)
                onCommandOpenChange(false)
                onCommandIDChange('')
              }}
            >
              取消
            </Button>
          </div>
        </div>
      </Modal>

      {pendingSensitiveCommand ? (
        <ActionConfirmationModal
          open
          title={`确认批量执行 ${pendingSensitiveCommand.name}`}
          current={`将对确认时选中的 ${confirmedBatchCount} 个监控实例执行 ${pendingSensitiveCommand.name}。`}
          result="操作后：center 会把该白名单命令下发到已选实例的 agent。"
          impact={pendingSensitiveCommand.description}
          unchanged="命令仍由 agent 编译期白名单执行，不接受自定义参数。"
          confirmLabel="确认下发"
          disabled={batchSubmitting}
          cancelDisabled={batchSubmitting}
          onConfirm={() => {
            const command = pendingSensitiveCommand
            setPendingSensitiveCommand(null)
            onExecuteBatchCommand(command.id, { confirmedSensitive: true })
          }}
          onCancel={() => {
            if (batchSubmitting) return
            setPendingSensitiveCommand(null)
          }}
        />
      ) : null}

      {pendingBatchAction === 'pause' ? (
        <ActionConfirmationModal
          open
          title="确认批量暂停监控实例监控"
          current={`将对确认时选中的 ${confirmedBatchCount} 个监控实例执行暂停操作。`}
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
