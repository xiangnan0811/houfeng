import { Badge } from '../../components/atoms'
import type { AssetDecisionSecondaryNavItem, SecondaryWorkbench } from './types'

type AssetDecisionSecondaryNavProps = {
  items: AssetDecisionSecondaryNavItem[]
  active: SecondaryWorkbench | null
  onOpen: (value: SecondaryWorkbench) => void
}

export function AssetDecisionSecondaryNav({ items, active, onOpen }: AssetDecisionSecondaryNavProps) {
  return (
    <nav className="asset-decision-secondary-nav" aria-label="资产决策次级工作区">
      {items.map((item) => (
        <article
          key={item.value}
          className={`asset-decision-secondary-nav__item asset-decision-secondary-nav__item--${item.tone}${active === item.value ? ' asset-decision-secondary-nav__item--active' : ''}`}
        >
          <div>
            <span>{item.eyebrow}</span>
            <strong>{item.title}</strong>
            <small>{item.summary}</small>
          </div>
          <div className="asset-decision-secondary-nav__meta">
            <Badge variant="state" tone={item.tone}>{item.meta}</Badge>
            <button
              className={`btn sm ${active === item.value ? 'primary' : 'secondary'}`}
              type="button"
              onClick={() => onOpen(item.value)}
            >
              {item.actionLabel}
            </button>
          </div>
        </article>
      ))}
    </nav>
  )
}
