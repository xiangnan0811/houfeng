import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import { useEffect, useRef, useState } from 'react'
import { Button, Input } from '../../components/atoms'
import { requestJSON } from '../../lib/apiRequest'

type Review = { vps_id: string; active_action_id: string; preview_digest: string; resources: Array<{ kind: string; resource_id: string; name: string; control: string; shared: boolean; shared_vps_ids?: string[]; eligible: boolean }> }
export function VPSMaintenancePanel({ vpsId, onChanged }: { vpsId: string; onChanged: () => void }) {
  if (READ_ONLY_PREVIEW) return null
  return <OwnedMaintenance key={vpsId} vpsId={vpsId} onChanged={onChanged} />
}
function OwnedMaintenance({ vpsId, onChanged }: { vpsId: string; onChanged: () => void }) {
  const [review, setReview] = useState<Review | null>(null)
  const [shared, setShared] = useState<string[]>([])
  const [reason, setReason] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [revision, setRevision] = useState(0)
  const mounted = useRef(true)
  const lock = useRef(false)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useEffect(() => {
    let active = true
    void requestJSON<Review>(`/api/vps/${encodeURIComponent(vpsId)}/maintenance-review`).then((result) => {
      if (active) { setReview(result); setShared([]) }
    }).catch((err: unknown) => { if (active) setError(err instanceof Error ? err.message : '加载失败') })
    return () => { active = false }
  }, [vpsId, revision])
  async function submit() {
    if (!review || !reason.trim() || lock.current) return
    lock.current = true; setBusy(true); setError('')
    try {
      const result = await requestJSON<Review>(`/api/vps/${encodeURIComponent(vpsId)}/maintenance`, {
        method: review.active_action_id ? 'DELETE' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ reason: reason.trim(), ...(!review.active_action_id ? { preview_digest: review.preview_digest, confirmed_shared_target_ids: shared } : {}) }),
      })
      if (!mounted.current) return
      setReview(result); setReason(''); setShared([]); onChanged()
    } catch (err) { if (mounted.current) { setError(err instanceof Error ? err.message : '维护操作失败'); setReview(null); setRevision((value) => value + 1) } }
    finally { lock.current = false; if (mounted.current) setBusy(false) }
  }
  return <section>
    <p>维护期间继续采集并接收最小心跳，抑制相关告警。结束维护只解除本次操作，保留其他暂停和维护设置。</p>
    {error ? <p role="alert">{error}</p> : null}
    {!review ? <Button onClick={() => setRevision((value) => value + 1)}>重新加载维护范围</Button> : <>
      {review.resources.map((resource) => <p key={`${resource.kind}:${resource.resource_id}`}>
        {resource.shared && resource.eligible && !review.active_action_id ? <input aria-label={`同时维护共享探测 ${resource.name}`} type="checkbox" checked={shared.includes(resource.resource_id)} disabled={busy} onChange={(event) => setShared((values) => event.target.checked ? [...values, resource.resource_id] : values.filter((value) => value !== resource.resource_id))} /> : null}
        {resource.name} · {resource.control} {resource.shared ? `（共享，需单独确认；涉及 VPS：${(resource.shared_vps_ids ?? []).join('、')}）` : '（专属）'}
      </p>)}
      <Input label="维护原因" value={reason} disabled={busy} onChange={(event) => setReason(event.target.value)} />
      <Button disabled={busy || !reason.trim()} onClick={() => void submit()}>{review.active_action_id ? '结束本次维护' : '开始维护'}</Button>
    </>}
  </section>
}
