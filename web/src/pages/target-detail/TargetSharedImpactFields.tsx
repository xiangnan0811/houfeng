import { useEffect, useRef, useState } from 'react'

import { SharedImpactPanel } from '../../components/SharedImpactPanel'
import { getTargetLifecycleReview } from '../../lib/api'
import { requiresSharedImpactConfirmation } from '../../lib/assetLifecycle'
import type { DependencyImpact, GlobalActionConfirmation } from '../../lib/types'

type TargetSharedImpactFieldsProps = {
  targetId: string
  reviewGeneration?: number
  onChange: (confirmation: GlobalActionConfirmation | undefined, blocked: boolean) => void
}

export function TargetSharedImpactFields({
  targetId,
  reviewGeneration = 0,
  onChange,
}: TargetSharedImpactFieldsProps) {
  const [impacts, setImpacts] = useState<DependencyImpact[]>([])
  const [digest, setDigest] = useState('')
  const [confirmed, setConfirmed] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [seenGeneration, setSeenGeneration] = useState(reviewGeneration)
  if (seenGeneration !== reviewGeneration) {
    setSeenGeneration(reviewGeneration)
    setImpacts([])
    setDigest('')
    setConfirmed(false)
    setError(null)
    setLoaded(false)
  }
  const onChangeRef = useRef(onChange)
  useEffect(() => {
    onChangeRef.current = onChange
  }, [onChange])
  useEffect(() => {
    let live = true
    void getTargetLifecycleReview(targetId)
      .then((review) => {
        if (!live) return
        setImpacts(review.dependency_impacts ?? [])
        setDigest(review.preview_digest ?? '')
        setError(null)
        setLoaded(true)
      })
      .catch(() => {
        if (!live) return
        setError('共享影响预览加载失败，不能继续这个危险动作。')
        setLoaded(true)
      })
    return () => {
      live = false
    }
  }, [reviewGeneration, targetId])

  const sharedRequired = requiresSharedImpactConfirmation(impacts, 'target', targetId)
  const blocked = !loaded || Boolean(error) || (sharedRequired && !confirmed)

  useEffect(() => {
    onChangeRef.current(
      loaded && !error
        ? { preview_digest: digest, confirm_shared_impact: sharedRequired ? confirmed : false }
        : undefined,
      blocked,
    )
  }, [blocked, confirmed, digest, error, loaded, sharedRequired])

  if (error) return <p role="alert">{error}</p>
  if (!loaded || !sharedRequired) return null
  return (
    <>
      <p>共享影响摘要 {digest}</p>
      <SharedImpactPanel
        impacts={impacts.filter((impact) => impact.object_type === 'target' && impact.object_id === targetId)}
        confirmations={[{
          key: targetId,
          label: '确认此目标对多台 VPS 的当前或残留影响',
          checked: confirmed,
        }]}
        onToggle={(_key, checked) => setConfirmed(checked)}
      />
    </>
  )
}
