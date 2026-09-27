import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import { Link } from 'react-router-dom'
import { useEffect, useRef, useState } from 'react'
import { Button, Input } from '../../components/atoms'
import { updateVPSAsset } from '../../lib/api'
import type { VPSAssetRecord, VPSAutoRenewCheck } from '../../lib/types'

export function VPSArchivedAmendment({ vps, onChanged }: { vps: VPSAssetRecord; onChanged: () => void }) {
  if (READ_ONLY_PREVIEW) return null
  return <OwnedAmendment key={`${vps.vps_id}:${vps.updated_at}`} vps={vps} onChanged={onChanged} />
}
function OwnedAmendment({ vps, onChanged }: { vps: VPSAssetRecord; onChanged: () => void }) {
  const [check, setCheck] = useState<VPSAutoRenewCheck>(vps.auto_renew_check ?? 'unchecked')
  const [note, setNote] = useState(vps.note)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const live = useRef(true)
  const lock = useRef(false)
  useEffect(() => { live.current = true; return () => { live.current = false } }, [])
  async function save() {
    if (lock.current) return
    lock.current = true; setBusy(true); setError('')
    try {
      await updateVPSAsset(vps.vps_id, { auto_renew_check: check, auto_renew_checked_at: check === 'unchecked' ? null : new Date().toISOString(), note }, { expectedUpdatedAt: vps.updated_at })
      if (live.current) onChanged()
    } catch (err) { if (live.current) setError(err instanceof Error ? err.message : '保存修订失败') }
    finally { lock.current = false; if (live.current) setBusy(false) }
  }
  return <section className="page-panel archive-detail-card">
    <h2>归档后核对与补充</h2>
    <p>核对服务商自动续费并补充备注；每次保存保留修订记录。</p>
    <p><Link to={`/subscriptions?vps_id=${encodeURIComponent(vps.vps_id)}&view=details&create=1`}>补录账单事实</Link> · <Link to={`/subscriptions?vps_id=${encodeURIComponent(vps.vps_id)}&view=details`}>修订账单 / 退款备注</Link></p>
    <label>服务商自动续费核对<select className="input" value={check} disabled={busy} onChange={(event) => setCheck(event.target.value as VPSAutoRenewCheck)}>
      <option value="unchecked">尚未核对</option><option value="enabled">仍然开启</option><option value="disabled">已关闭</option><option value="never_enabled">从未开启</option><option value="unsupported">服务商不支持</option>
    </select></label>
    <Input label="归档后备注 / 账单、退款与证据说明" value={note} disabled={busy} onChange={(event) => setNote(event.target.value)} />
    {error ? <p role="alert">{error}</p> : null}
    <Button disabled={busy} onClick={() => void save()}>保存归档后修订</Button>
  </section>
}
