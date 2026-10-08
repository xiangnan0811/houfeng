import { useEffect, useLayoutEffect, useRef, useState } from 'react'

import { formatDateTime } from '../../../lib/format'
import { getEvidenceSnapshot, getRecordRevision } from '../../../lib/recordsApi'
import type { RecordRevision } from '../../../lib/types'
import { evidenceChoiceMeta, factsFromSnapshot, isAbortError, subjectFactLabel } from './comparisonEvidenceCopy'
import { comparisonFixedItemKey, type ComparisonURLFixedItem } from './comparisonQueryState'

export type BasketIdentity =
  | { status: 'loading' }
  | { status: 'unreadable'; technical: string[] }
  | { status: 'ready'; title: string; meta: string; technical: string[] }

function technicalFor(item: ComparisonURLFixedItem): string[] {
  if ('snapshot_id' in item) return [item.snapshot_id]
  return [item.record_id, item.revision_id, ...(item.snapshot_ids ?? [])]
}

function revisionMeta(revision: RecordRevision, snapshotCount: number): string {
  const subjects = revision.subjects
    .map((subject) => subject.identity.display_name.trim())
    .filter(Boolean)
  return [
    `修订 ${revision.revision_no}`,
    formatDateTime(revision.created_at),
    subjects.join('、'),
    snapshotCount > 0 ? `已选 ${snapshotCount} 份证据` : '',
  ].filter(Boolean).join(' · ')
}

async function readIdentity(item: ComparisonURLFixedItem, signal: AbortSignal): Promise<BasketIdentity> {
  const technical = technicalFor(item)
  try {
    if ('snapshot_id' in item) {
      const snapshot = await getEvidenceSnapshot(item.snapshot_id, signal)
      if (signal.aborted) return { status: 'loading' }
      const facts = factsFromSnapshot(snapshot)
      return {
        status: 'ready',
        title: facts.title,
        meta: evidenceChoiceMeta({
          timeLabel: facts.timeLabel,
          kindLabel: [facts.kindLabel, subjectFactLabel(snapshot.subject)].filter(Boolean).join(' · '),
          qualityLabel: facts.qualityLabel,
        }),
        technical: [snapshot.snapshot_id, snapshot.subject.id].filter(Boolean),
      }
    }
    const revision = await getRecordRevision(item.record_id, item.revision_id)
    if (signal.aborted) return { status: 'loading' }
    return {
      status: 'ready',
      title: revision.title.trim() || '未命名修订',
      meta: revisionMeta(revision, item.snapshot_ids?.length ?? 0),
      technical,
    }
  } catch (error) {
    if (signal.aborted || isAbortError(error)) return { status: 'loading' }
    return { status: 'unreadable', technical }
  }
}

function loadingIdentities(items: readonly ComparisonURLFixedItem[]): ReadonlyMap<string, BasketIdentity> {
  const loading = new Map<string, BasketIdentity>()
  for (const item of items) loading.set(comparisonFixedItemKey(item), { status: 'loading' })
  return loading
}

export function useComparisonBasketIdentity(
  items: readonly ComparisonURLFixedItem[],
): ReadonlyMap<string, BasketIdentity> {
  const signature = items.map((item) => {
    const key = comparisonFixedItemKey(item)
    if ('snapshot_id' in item) return key
    return `${key}:${(item.snapshot_ids ?? []).join(',')}`
  }).join('\n')
  const itemsRef = useRef(items)
  const signatureRef = useRef(signature)
  const generationRef = useRef(0)
  const [stored, setStored] = useState<{ signature: string; map: ReadonlyMap<string, BasketIdentity> } | null>(null)
  const identities = stored?.signature === signature ? stored.map : loadingIdentities(items)

  useLayoutEffect(() => {
    itemsRef.current = items
    if (signatureRef.current !== signature) {
      signatureRef.current = signature
      generationRef.current += 1
    }
  }, [items, signature])

  useEffect(() => {
    const generation = generationRef.current
    const current = itemsRef.current
    const requestSignature = signature
    const controller = new AbortController()
    let active = true
    void Promise.all(current.map(async (item) => {
      const key = comparisonFixedItemKey(item)
      const identity = await readIdentity(item, controller.signal)
      return [key, identity] as const
    })).then((entries) => {
      if (!active || generation !== generationRef.current) return
      setStored({ signature: requestSignature, map: new Map(entries) })
    })
    return () => {
      active = false
      controller.abort()
    }
  }, [signature])

  return identities
}
