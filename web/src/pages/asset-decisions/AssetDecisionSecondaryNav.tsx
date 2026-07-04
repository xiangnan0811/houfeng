import { Badge } from '../../components/atoms'
import type { AssetDecisionSecondaryNavItem, SecondaryWorkbench } from './types'

type AssetDecisionSecondaryNavProps = {
  items: AssetDecisionSecondaryNavItem[]
  active: SecondaryWorkbench | null
  onOpen: (value: SecondaryWorkbench) => void
}

export function AssetDecisionSecondaryNav({ items, active, onOpen }: AssetDecisionSecondaryNavProps) {
  return (
    <nav className="asset-decision-support-strip" aria-label="资产决策辅助入口">
      {items.map((item) => (
        <button
          key={item.value}
          type="button"
          className={`asset-decision-support-strip__item asset-decision-support-strip__item--${item.tone}${active === item.value ? ' asset-decision-support-strip__item--active' : ''}`}
          onClick={() => onOpen(item.value)}
          aria-label={item.title}
          aria-pressed={active === item.value}
        >
          <span className="asset-decision-support-strip__title">{item.title}</span>
          <Badge variant="state" tone={item.tone}>{item.meta}</Badge>
        </button>
      ))}
    </nav>
  )
}
