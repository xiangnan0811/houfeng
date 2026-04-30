import { Badge } from './Badge'

export interface TabItem<V extends string = string> {
  value: V
  label: string
  count?: number
}

export interface TabsProps<V extends string = string> {
  items: TabItem<V>[]
  value: V
  onChange: (next: V) => void
  variant?: 'underline' | 'pill'
}

export function Tabs<V extends string = string>({
  items,
  value,
  onChange,
  variant = 'underline',
}: TabsProps<V>) {
  const cls = ['tabs', `tabs--${variant}`].join(' ')
  return (
    <div className={cls} role="tablist">
      {items.map((item) => {
        const selected = item.value === value
        return (
          <button
            key={item.value}
            type="button"
            role="tab"
            aria-selected={selected}
            className={['tab', selected && 'is-active'].filter(Boolean).join(' ')}
            onClick={() => onChange(item.value)}
          >
            <span>{item.label}</span>
            {typeof item.count === 'number' && item.count > 0 && (
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
