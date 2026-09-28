import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import { Link } from 'react-router-dom'
import { useEffect, useRef, useState } from 'react'
import { Button, MonoDigits } from '../../components/atoms'
import { updateVPSAsset } from '../../lib/api'
import { formatDateTime } from '../../lib/format'
import { VPS_AUTO_RENEW_CHECK_LABELS, type VPSAssetRecord, type VPSAutoRenewCheck } from '../../lib/types'

export function VPSArchivedAmendment({ vps, onChanged }: { vps: VPSAssetRecord; onChanged: () => void }) {
  if (READ_ONLY_PREVIEW) return null
  return <OwnedAmendment key={`${vps.vps_id}:${vps.updated_at}`} vps={vps} onChanged={onChanged} />
}

function OwnedAmendment({ vps, onChanged }: { vps: VPSAssetRecord; onChanged: () => void }) {
  const savedCheck = vps.auto_renew_check ?? 'unchecked'
  const [check, setCheck] = useState<VPSAutoRenewCheck>(savedCheck)
  const [note, setNote] = useState(vps.note)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const live = useRef(true)
  const lock = useRef(false)
  useEffect(() => { live.current = true; return () => { live.current = false } }, [])
  const dirty = check !== savedCheck || note !== vps.note
  async function save() {
    if (lock.current) return
    lock.current = true; setBusy(true); setError('')
    try {
      await updateVPSAsset(vps.vps_id, { auto_renew_check: check, auto_renew_checked_at: check === 'unchecked' ? null : new Date().toISOString(), note }, { expectedUpdatedAt: vps.updated_at })
      if (live.current) onChanged()
    } catch (err) { if (live.current) setError(err instanceof Error ? err.message : '保存修订失败') }
    finally { lock.current = false; if (live.current) setBusy(false) }
  }
  const billingHref = `/subscriptions?vps_id=${encodeURIComponent(vps.vps_id)}&view=details`
  return <section className="archive-amend" aria-label="归档后核对与补充">
    <label className="archive-amend__field archive-amend__field--inline">
      <span>服务商自动续费核对</span>
      <select className="input" value={check} disabled={busy} onChange={(event) => setCheck(event.target.value as VPSAutoRenewCheck)}>
        {Object.entries(VPS_AUTO_RENEW_CHECK_LABELS).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select>
    </label>
    <p className="archive-amend__hint">
      {vps.auto_renew_checked_at
        ? <>上次核对 <MonoDigits>{formatDateTime(vps.auto_renew_checked_at)}</MonoDigits></>
        : '归档不代表服务商已停止扣费，请登录服务商后台确认。'}
    </p>
    <label className="archive-amend__field">
      <span>归档后备注</span>
      <textarea className="input" rows={2} value={note} disabled={busy} placeholder="账单、退款与证据说明" onChange={(event) => setNote(event.target.value)} />
    </label>
    {error ? <p className="archive-amend__error" role="alert">{error}</p> : null}
    <div className="archive-amend__actions">
      <span className="archive-amend__links">
        <Link to={`${billingHref}&create=1`}>补录账单</Link>
        <Link to={billingHref}>修订账单 / 退款</Link>
      </span>
      <Button size="sm" disabled={busy || (!dirty && check === 'unchecked')} onClick={() => void save()}>
        {busy ? '保存中…' : dirty ? '保存修订' : '确认仍为此结果'}
      </Button>
    </div>
  </section>
}
