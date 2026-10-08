import type { RecordSubjectKind } from '../../../lib/types'

/** Evidence workspace for a comparison subject. Ids stay in the path, not in the link label. */
export function subjectEvidenceHref(kind: RecordSubjectKind, id: string): string {
  const encoded = encodeURIComponent(id.trim())
  if (kind === 'vps') return `/vps/${encoded}/evidence`
  if (kind === 'monitoring_instance') return `/monitoring/${encoded}/evidence`
  return `/targets/${encoded}/evidence`
}
