import { type FocusEvent, type KeyboardEvent, type ReactNode, useLayoutEffect, useRef, useState } from 'react'
import { Badge } from './Badge'
import { tabId, tabPanelId } from './tabIds'

export interface TabItem<V extends string = string> {
  value: V
  label: string
  count?: number
}

export interface TabsProps<V extends string = string> {
  label: string
  idBase: string
  items: readonly TabItem<V>[]
  value: V
  onChange: (next: V) => void
  variant?: 'underline' | 'pill'
  /** Automatic (default) activates on arrow keys. Manual only moves focus; click/Enter/Space activate. */
  activation?: 'automatic' | 'manual'
}

export interface TabPanelProps<V extends string = string> {
  idBase: string
  value: V
  className?: string
  children: ReactNode
}

export function Tabs<V extends string = string>({
  label,
  idBase,
  items,
  value,
  onChange,
  variant = 'underline',
  activation = 'automatic',
}: TabsProps<V>) {
  const buttonRefs = useRef<Array<HTMLButtonElement | null>>([])
  const pendingScrollTargetRef = useRef<{ button: HTMLButtonElement | null; value: V } | null>(null)
  const selectedIndex = items.findIndex((item) => item.value === value)
  const tabStopIndex = selectedIndex >= 0 ? selectedIndex : items.length > 0 ? 0 : -1
  const [manualRover, setManualRover] = useState({ value, index: -1 })
  if (manualRover.value !== value) {
    setManualRover({ value, index: -1 })
  }
  const roverIndex = activation === 'manual' && manualRover.index >= 0 ? manualRover.index : tabStopIndex
  const cls = ['tabs', `tabs--${variant}`].join(' ')

  useLayoutEffect(() => {
    const pending = pendingScrollTargetRef.current
    if (!pending || pending.value !== value) return
    pendingScrollTargetRef.current = null
    if (pending.button?.isConnected) {
      pending.button.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
    }
  })

  function handleKeyDown(event: KeyboardEvent<HTMLButtonElement>, currentIndex: number) {
    let nextIndex: number
    switch (event.key) {
      case 'ArrowRight':
        nextIndex = (currentIndex + 1) % items.length
        break
      case 'ArrowLeft':
        nextIndex = (currentIndex - 1 + items.length) % items.length
        break
      case 'Home':
        nextIndex = 0
        break
      case 'End':
        nextIndex = items.length - 1
        break
      default:
        return
    }

    event.preventDefault()
    const nextItem = items[nextIndex]
    if (!nextItem) return
    const nextButton = buttonRefs.current[nextIndex] ?? null
    pendingScrollTargetRef.current = { button: nextButton, value: nextItem.value }
    nextButton?.focus()
    if (activation === 'manual') {
      nextButton?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
      setManualRover({ value, index: nextIndex })
      return
    }
    onChange(nextItem.value)
  }

  return (
    <div
      className={cls}
      role="tablist"
      aria-label={label}
      onBlur={(event: FocusEvent<HTMLDivElement>) => {
        if (activation !== 'manual') return
        const next = event.relatedTarget
        if (next instanceof Node && event.currentTarget.contains(next)) return
        setManualRover({ value, index: -1 })
      }}
    >
      {items.map((item, index) => {
        const selected = item.value === value
        const hasCount = typeof item.count === 'number' && item.count > 0
        return (
          <button
            key={item.value}
            ref={(node) => {
              buttonRefs.current[index] = node
            }}
            id={tabId(idBase, item.value)}
            type="button"
            role="tab"
            aria-controls={tabPanelId(idBase, item.value)}
            aria-label={hasCount ? `${item.label} ${item.count}` : item.label}
            aria-selected={selected}
            tabIndex={index === roverIndex ? 0 : -1}
            className={['tab', selected && 'is-active'].filter(Boolean).join(' ')}
            onClick={() => onChange(item.value)}
            onKeyDown={(event) => handleKeyDown(event, index)}
          >
            <span>{item.label}</span>
            {hasCount && (
              <Badge variant="count" tone="notice">
                {item.count}
              </Badge>
            )}
          </button>
        )
      })}
    </div>
  )
}

export function TabPanel<V extends string = string>({
  idBase,
  value,
  className,
  children,
}: TabPanelProps<V>) {
  return (
    <div
      id={tabPanelId(idBase, value)}
      role="tabpanel"
      aria-labelledby={tabId(idBase, value)}
      tabIndex={0}
      className={className}
    >
      {children}
    </div>
  )
}
