import { Badge } from './Badge'

export interface SegmentedItem<V extends string = string> {
  value: V
  label: string
  count?: number
}

export interface SegmentedControlProps<V extends string = string> {
  label: string
  items: readonly SegmentedItem<V>[]
  value: V
  onChange: (next: V) => void
}

export function SegmentedControl<V extends string = string>({
  label,
  items,
  value,
  onChange,
}: SegmentedControlProps<V>) {
  return (
    <div className="tabs tabs--pill" role="group" aria-label={label}>
      {items.map((item) => {
        const selected = item.value === value
        const hasCount = typeof item.count === 'number' && item.count > 0
        return (
          <button
            key={item.value}
            type="button"
            aria-label={hasCount ? `${item.label} ${item.count}` : item.label}
            aria-pressed={selected}
            className={['tab', selected && 'is-active'].filter(Boolean).join(' ')}
            onClick={() => onChange(item.value)}
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
