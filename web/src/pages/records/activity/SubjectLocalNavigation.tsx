import { Link } from 'react-router-dom'

import type { SubjectActivityView } from '../../../lib/types'
import {
  subjectActivityViewLabel,
  type SubjectRouteRef,
} from './activityQueryState'

type Props = {
  subject: SubjectRouteRef
  activeView: SubjectActivityView
  /** Optional overview link for VPS (Task 6). Omitted on monitoring/target. */
  overviewHref?: string
  /** When true, marks the overview link as the current page. */
  overviewCurrent?: boolean
  search?: string
}

const VIEWS: SubjectActivityView[] = ['activity', 'records', 'evidence']

export function SubjectLocalNavigation({
  subject,
  activeView,
  overviewHref,
  overviewCurrent = false,
  search = '',
}: Props) {
  const query = search && search !== '?' ? search : ''
  return (
    <nav className="subject-local-nav" aria-label="主体局部导航">
      {overviewHref ? (
        <Link
          className={overviewCurrent
            ? 'subject-local-nav__link subject-local-nav__link--active'
            : 'subject-local-nav__link'}
          to={overviewHref}
          aria-current={overviewCurrent ? 'page' : undefined}
        >
          概览
        </Link>
      ) : null}
      {VIEWS.map((view) => {
        const to = `${subject.basePath}/${view}${query}`
        const active = !overviewCurrent && view === activeView
        return (
          <Link
            key={view}
            className={active
              ? 'subject-local-nav__link subject-local-nav__link--active'
              : 'subject-local-nav__link'}
            to={to}
            aria-current={active ? 'page' : undefined}
          >
            {subjectActivityViewLabel(view)}
          </Link>
        )
      })}
    </nav>
  )
}
