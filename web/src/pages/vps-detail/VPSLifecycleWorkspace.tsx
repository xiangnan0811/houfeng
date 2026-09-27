import './VPSLifecycleWorkspace.css'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import { useEffect, useRef, useState } from 'react'
import { Button, Input } from '../../components/atoms'
import { createVPSFollowup, endVPSAssociation, linkVPSAssociation, listAssetDomains, listAssetServices, listVPSAssociations, listVPSFollowups, listTargets, resolveVPSFollowup } from '../../lib/api'
import type { VPSAssociationRecord, VPSFollowupRecord } from '../../lib/types'

type Props = { vpsId: string; archived?: boolean; kind: 'service' | 'domain' | 'followups'; onChanged?: () => void }

/** Each mounted workspace owns one VPS; late responses cannot cross that boundary. */
export function VPSLifecycleWorkspace(props: Props) {
  return <OwnedWorkspace key={`${props.vpsId}:${props.kind}`} {...props} />
}

function OwnedWorkspace({ vpsId, archived = false, kind, onChanged }: Props) {
  const [rows, setRows] = useState<VPSAssociationRecord[]>([])
  const [followups, setFollowups] = useState<VPSFollowupRecord[]>([])
  const [options, setOptions] = useState<Array<{ id: string; name: string }>>([])
  const [selected, setSelected] = useState('')
  const [targets, setTargets] = useState<Array<{ id: string; name: string }>>([])
  const [services, setServices] = useState<Array<{ id: string; name: string }>>([])
  const [targetId, setTargetId] = useState('')
  const [serviceId, setServiceId] = useState('')
  const [address, setAddress] = useState('')
  const [port, setPort] = useState('')
  const [reason, setReason] = useState('')
  const [summary, setSummary] = useState('')
  const [migrationTarget, setMigrationTarget] = useState('')
  const [error, setError] = useState('')
  const [catalogWarning, setCatalogWarning] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [revision, setRevision] = useState(0)
  const live = useRef(true)
  const writeLock = useRef(false)
  useEffect(() => { live.current = true; return () => { live.current = false } }, [])
  useEffect(() => {
    let active = true
    const load = async () => {
      try {
        if (kind === 'followups') {
          const result = await listVPSFollowups(vpsId)
          if (active) setFollowups(result)
        } else {
          const [associationResult, objectResult, targetResult, serviceResult] = await Promise.allSettled([
            listVPSAssociations(vpsId, kind),
            kind === 'service' ? listAssetServices() : listAssetDomains(),
            archived ? Promise.resolve([]) : listTargets(),
            !archived && kind === 'domain' ? listAssetServices({ vps_id: vpsId }) : Promise.resolve([]),
          ])
          if (!active) return
          if (associationResult.status === 'rejected') throw associationResult.reason
          const objects = objectResult.status === 'fulfilled' ? objectResult.value : []
          const availableTargets = targetResult.status === 'fulfilled' ? targetResult.value : []
          const hostedServices = serviceResult.status === 'fulfilled' ? serviceResult.value : []
          setRows(associationResult.value)
          setCatalogWarning([objectResult, targetResult, serviceResult].some((result) => result.status === 'rejected') ? '部分对象或探测目录暂不可用；关联历史已保留，可重试补全。' : '')
          setTargets(availableTargets.map((item) => ({ id: item.target_id, name: item.name })))
          setServices(hostedServices.map((item) => ({ id: item.service_id, name: item.name })))
          setOptions(objects.map((item) => 'domain_id' in item
            ? { id: item.domain_id, name: item.domain_name }
            : { id: item.service_id, name: item.name }))
        }
        if (active) setError('')
      } catch (err) { if (active) setError(err instanceof Error ? err.message : '加载失败') }
      finally { if (active) setLoading(false) }
    }
    void load()
    return () => { active = false }
  }, [archived, kind, revision, vpsId])

  async function mutate(action: () => Promise<unknown>) {
    if (READ_ONLY_PREVIEW || writeLock.current) return
    writeLock.current = true
    setBusy(true)
    setError('')
    try {
      await action()
      if (!live.current) return
      setReason(''); setSummary(''); setSelected(''); setAddress(''); setPort(''); setTargetId(''); setServiceId('')
      setRevision((value) => value + 1)
      onChanged?.()
    } catch (err) { if (live.current) setError(err instanceof Error ? err.message : '操作失败') }
    finally { writeLock.current = false; if (live.current) setBusy(false) }
  }

  if (loading) return <p role="status">正在加载关联与跟进记录…</p>
  return <section className="vps-lifecycle-workspace" aria-label={kind === 'followups' ? '跟进事项' : '当前及历史关联'}>
    {catalogWarning ? <p role="status">{catalogWarning}<Button onClick={() => setRevision((value) => value + 1)}>重试目录</Button></p> : null}
    {error ? <p role="alert">{error}<Button onClick={() => setRevision((value) => value + 1)}>重试</Button></p> : null}
    {kind === 'followups' ? <>
      <p>待核对事项可在归档后继续处理。解决和忽略均保留原因、操作者及时间。</p>
      {followups.length === 0 ? <p>暂无跟进事项。</p> : followups.map((item) => <article key={item.followup_id}>
        <h4>{item.summary}</h4>{item.kind === 'migration' ? <p>来源：{String(item.details.source_vps_id ?? vpsId)} · 目标：{String(item.details.target_vps ?? '待记录')} · 结果：{String(item.details.result ?? '待跟进')}</p> : null}<p>{item.status === 'pending' ? '待核对' : item.status === 'resolved' ? '已解决' : '已忽略'} · {item.created_at}</p>
        {item.resolution_reason ? <p>{item.resolution_reason} · {item.resolved_by} · {item.resolved_at}</p> : null}
        {item.status === 'pending' && !READ_ONLY_PREVIEW ? <><Button disabled={busy || !reason.trim()} onClick={() => void mutate(() => resolveVPSFollowup(vpsId, item.followup_id, 'resolved', reason.trim()))}>解决</Button><Button disabled={busy || !reason.trim()} onClick={() => void mutate(() => resolveVPSFollowup(vpsId, item.followup_id, 'ignored', reason.trim()))}>忽略</Button></> : null}
      </article>)}
      {!READ_ONLY_PREVIEW ? <>
      <Input label="处理原因 / 迁移来源、目标及结果" value={reason} disabled={busy} onChange={(event) => setReason(event.target.value)} />
      <Input label="迁移目标 VPS（名称或标识）" value={migrationTarget} disabled={busy} onChange={(event) => setMigrationTarget(event.target.value)} />
      <Input label="新增跟进事项" value={summary} disabled={busy} onChange={(event) => setSummary(event.target.value)} />
      <Button disabled={busy || !summary.trim()} onClick={() => void mutate(() => createVPSFollowup(vpsId, { kind: 'migration', summary: summary.trim(), details: { source_vps_id: vpsId, target_vps: migrationTarget.trim(), result: reason.trim() } }))}>记录迁移跟进</Button>
      </> : null}
    </> : <>
      <p>对象身份独立于 VPS。结束关联保留历史快照，共享对象及其他 VPS 的关联继续存在。</p>
      {rows.length === 0 ? <p>暂无关联。</p> : rows.map((item) => <article key={item.association_id}>
        <h4>{options.find((option) => option.id === item.object_id)?.name ?? item.object_id}</h4>
        <p>{item.address}{item.port ? `:${item.port}` : ''} · {item.started_at} — {item.ended_at ?? '当前'}</p>
        {item.ended_at ? <><p>{item.end_reason} · {item.ended_by}</p><details><summary>关联结束时的事实</summary><pre>{JSON.stringify(item.snapshot, null, 2)}</pre></details></> : !archived && !READ_ONLY_PREVIEW ? <Button disabled={busy || !reason.trim()} onClick={() => void mutate(() => endVPSAssociation(vpsId, kind, item.association_id, reason.trim()))}>结束此关联</Button> : null}
      </article>)}
      {!archived && !READ_ONLY_PREVIEW ? <>
        <Input label="结束关联原因" value={reason} disabled={busy} onChange={(event) => setReason(event.target.value)} />
        <label>关联已有{kind === 'service' ? '服务' : '域名'}<select className="input" value={selected} disabled={busy} onChange={(event) => setSelected(event.target.value)}><option value="">请选择对象</option>{options.map((option) => <option key={option.id} value={option.id}>{option.name}</option>)}</select></label>
        <label>此关联的入口探测<select className="input" value={targetId} disabled={busy} onChange={(event) => setTargetId(event.target.value)}><option value="">不关联探测</option>{targets.map((target) => <option key={target.id} value={target.id}>{target.name}</option>)}</select></label>
        {kind === 'domain' ? <label>此 VPS 上的服务<select className="input" value={serviceId} disabled={busy} onChange={(event) => setServiceId(event.target.value)}><option value="">不关联服务</option>{services.map((service) => <option key={service.id} value={service.id}>{service.name}</option>)}</select></label> : null}
        <Input label="此 VPS 上的承载地址" value={address} disabled={busy} onChange={(event) => setAddress(event.target.value)} />
        {kind === 'service' ? <Input label="端口" type="number" min={1} max={65535} value={port} disabled={busy} onChange={(event) => setPort(event.target.value)} /> : null}
        <Button disabled={busy || !selected || Boolean(port && (!Number.isInteger(Number(port)) || Number(port) < 1 || Number(port) > 65535))} onClick={() => void mutate(() => linkVPSAssociation(vpsId, kind, { object_id: selected, address: address.trim(), ...(port ? { port: Number(port) } : {}), ...(targetId ? { target_id: targetId } : {}), ...(serviceId ? { service_id: serviceId } : {}) }))}>关联到此 VPS</Button>
      </> : null}
    </>}
  </section>
}
