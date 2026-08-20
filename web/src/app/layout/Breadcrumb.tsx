import { Fragment } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'

interface Crumb {
  label: string
  to?: string
}

const SECTION_LABELS: Record<string, string> = {
  'asset-decisions': '资产决策',
  providers: '服务商',
  subscriptions: '订阅',
  vps: 'VPS',
  monitoring: '监控',
  targets: '入口探测',
  events: '事件',
  settings: '设置',
}

const NESTED_LABELS: Record<string, string> = {
  activity: '活动',
  records: '记录',
  evidence: '证据',
  'ip-quality': 'IP 质量',
}

/**
 * Build a breadcrumb trail from the current location.
 *
 * Returns null on the root and any level-1 route ("/", "/vps", "/monitoring",
 * "/targets", "/events", "/settings"). At those depths the sidebar nav already conveys
 * "where you are" — adding a breadcrumb that re-renders the same anchor text
 * would create duplicate links and break test contracts that assume a single
 * `getByRole('link', { name: '监控' })` etc.
 *
 * On detail routes (`/monitoring/:id`, `/targets/:id`, `/vps/:id`) the trail looks
 * like: `监控 / mi_abcd...` or `VPS / vps_abcd...`.
 * The IDs are not display names — the breadcrumb stays page-rendering-free
 * (no extra fetch). Page hero panels remain the canonical place for the
 * full display_name.
 */
export function Breadcrumb() {
  const location = useLocation()
  const params = useParams()

  const segments = location.pathname.split('/').filter(Boolean)
  if (segments.length < 2) return null

  const crumbs: Crumb[] = []
  const sectionKey = segments[0]
  if (!sectionKey) return null
  const sectionLabel = SECTION_LABELS[sectionKey]
  if (!sectionLabel) return null
  crumbs.push({ label: sectionLabel, to: `/${sectionKey}` })

  // segments[1] is an ID for /monitoring/:id and /targets/:id
  if (segments[1]) {
    const detailId = segments[1]
    const isCurrent = segments.length === 2
    crumbs.push({
      label: truncateId(detailId),
      ...(isCurrent ? {} : { to: `/${sectionKey}/${detailId}` }),
    })
  }

  // Third crumb for subject activity / records / evidence (and other nested pages).
  if (segments[2]) {
    const nested = segments[2]
    const nestedLabel = NESTED_LABELS[nested] ?? nested
    crumbs.push({ label: nestedLabel })
  }

  // Touch params so React keeps this re-rendering on route change.
  void params

  return (
    <nav className="breadcrumb" aria-label="面包屑导航">
      {crumbs.map((crumb, i) => {
        const isLast = i === crumbs.length - 1
        return (
          <Fragment key={`${crumb.label}-${i}`}>
            {i > 0 && <span className="breadcrumb__sep" aria-hidden>/</span>}
            {crumb.to && !isLast ? (
              <Link className="breadcrumb__link" to={crumb.to}>
                {crumb.label}
              </Link>
            ) : (
              <span className="breadcrumb__current" aria-current="page">
                {crumb.label}
              </span>
            )}
          </Fragment>
        )
      })}
    </nav>
  )
}

function truncateId(id: string, max = 14): string {
  if (id.length <= max) return id
  return id.slice(0, max - 1) + '…'
}
