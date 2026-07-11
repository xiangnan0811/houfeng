import { type KeyboardEvent, type ReactNode, useLayoutEffect, useRef } from 'react'
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
}: TabsProps<V>) {
  const buttonRefs = useRef<Array<HTMLButtonElement | null>>([])
  const pendingScrollTargetRef = useRef<HTMLButtonElement | null>(null)
  const selectedIndex = items.findIndex((item) => item.value === value)
  const tabStopIndex = selectedIndex >= 0 ? selectedIndex : items.length > 0 ? 0 : -1
  const cls = ['tabs', `tabs--${variant}`].join(' ')

  useLayoutEffect(() => {
    const target = pendingScrollTargetRef.current
    pendingScrollTargetRef.current = null
    if (target?.isConnected) {
      target.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
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
    const nextButton = buttonRefs.current[nextIndex]
    pendingScrollTargetRef.current = nextButton
    nextButton?.focus()
    onChange(items[nextIndex].value)
  }

  return (
    <div className={cls} role="tablist" aria-label={label}>
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
            tabIndex={index === tabStopIndex ? 0 : -1}
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
