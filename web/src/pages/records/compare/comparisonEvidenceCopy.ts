import { formatDateTime } from '../../../lib/format'
import type { EvidenceSnapshotRead } from '../../../lib/types'
import { evidenceKindLabel, identityTypeLabel, presentGeneratedEvidenceTitle, QUALITY_BADGE_LABELS } from '../evidence/evidencePresentation'

export type EvidenceChoiceFacts = {
  title: string
  timeLabel: string
  kindLabel: string
  qualityLabel: string | null
}

export function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError'
}

export function evidenceQualityLabel(status: string): string | null {
  if (
    status === 'complete'
    || status === 'partial'
    || status === 'degraded'
    || status === 'unknown'
  ) {
    return QUALITY_BADGE_LABELS[status]
  }
  return null
}

export function evidenceChoiceMeta(input: {
  timeLabel: string
  kindLabel: string
  qualityLabel?: string | null
  pending?: boolean
  unreadable?: boolean
}): string {
  const quality = input.unreadable
    ? '不可读'
    : input.pending
      ? '正在读取质量'
      : input.qualityLabel
  return [input.timeLabel, input.kindLabel, quality].filter(Boolean).join(' · ')
}

export function factsFromSnapshot(
  snapshot: EvidenceSnapshotRead,
  fallbackTitle = '未命名证据',
): EvidenceChoiceFacts {
  return {
    title: presentGeneratedEvidenceTitle(snapshot.title) || fallbackTitle,
    timeLabel: formatDateTime(snapshot.observed_at),
    kindLabel: evidenceKindLabel(snapshot.kind),
    qualityLabel: evidenceQualityLabel(snapshot.quality.status),
  }
}

export function subjectFactLabel(identity: { type: string; display_name?: string }): string {
  const name = identity.display_name?.trim()
  const type = identityTypeLabel(identity.type)
  return name ? `${type} ${name}` : type
}
