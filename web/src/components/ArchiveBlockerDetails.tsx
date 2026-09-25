import { Link } from 'react-router-dom'

import { blockerHandling } from '../lib/assetLifecycle'
import type { ArchiveBlockerDetail } from '../lib/types'

type ArchiveBlockerDetailsProps = {
  details: readonly ArchiveBlockerDetail[]
  vpsId: string
  onInline: (detail: ArchiveBlockerDetail, kind: 'service-status' | 'domain-status' | 'residual' | 'restore') => void
}

export function ArchiveBlockerDetails({ details, vpsId, onInline }: ArchiveBlockerDetailsProps) {
  if (details.length === 0) return null
  return (
    <ul className="asset-lifecycle-confirm__blockers">
      {details.map((detail) => {
        const handling = blockerHandling(detail, vpsId)
        return (
          <li key={`${detail.code}:${detail.object_type}:${detail.object_id}`}>
            <strong>{detail.display_name || detail.object_id}</strong>
            {' · '}
            {detail.current_state}
            {' · '}
            {detail.code}
            <div>
              {handling.href ? (
                <Link to={handling.href}>{handling.label}</Link>
              ) : handling.inline ? (
                <button type="button" className="btn sm ghost" onClick={() => onInline(detail, handling.inline!)}>
                  {handling.label}
                </button>
              ) : (
                <span>{handling.label}</span>
              )}
            </div>
          </li>
        )
      })}
    </ul>
  )
}
