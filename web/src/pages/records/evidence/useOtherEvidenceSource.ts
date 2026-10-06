import { useCallback, useEffect, useRef, useState } from 'react'

import { useIdLookup } from '../hooks/useIdLookup'

type MonitoringInstanceItems = readonly { monitoring_instance_id: string; display_name: string }[]

/** 记录主体之外的来源：任选一台 VPS，必要时再选它的监控实例，用于跨主机对比。 */
export type OtherEvidenceSourceLoaders = Readonly<{
  listVPS: () => Promise<readonly { vps_id: string; display_name: string }[]>
  listMonitoringInstances: (vpsId: string) => Promise<MonitoringInstanceItems>
}>

export type OtherEvidenceSourceOption = Readonly<{ id: string; label: string }>

type Loaded = { items: readonly OtherEvidenceSourceOption[] } | { failed: true }

// 只有切到“其他 VPS”时才读取列表；监控实例按所选 VPS 读取。尚无结果即视为读取中，
// effect 里只在异步回调中写状态，ref 防止重复请求；卸载或停用时丢弃未完成的结果并允许重新读取。
export function useOtherEvidenceSource(
  active: boolean,
  needsInstance: boolean,
  loaders: OtherEvidenceSourceLoaders | undefined,
) {
  const [vpsList, setVpsList] = useState<Loaded | null>(null)
  const [vpsId, setVpsId] = useState('')
  const [instances, setInstances] = useState<ReadonlyMap<string, Loaded>>(() => new Map())
  const [instanceId, setInstanceId] = useState('')
  const vpsRequestedRef = useRef(false)
  const instanceRequestsRef = useRef(new Set<string>())

  useEffect(() => {
    if (!active || !loaders || vpsRequestedRef.current) return
    vpsRequestedRef.current = true
    let current = true
    let settled = false
    Promise.resolve()
      .then(() => loaders.listVPS())
      .then(
        (items): Loaded => ({ items: items.map((item) => ({ id: item.vps_id, label: item.display_name || item.vps_id })) }),
        (): Loaded => ({ failed: true }),
      )
      .then((loaded) => {
        if (!current) return
        settled = true
        // 失败后允许下次切回时重试。
        if ('failed' in loaded) vpsRequestedRef.current = false
        setVpsList(loaded)
      })
    return () => {
      current = false
      if (!settled) vpsRequestedRef.current = false
    }
  }, [active, loaders])

  useEffect(() => {
    if (!active || !needsInstance || !loaders || !vpsId || instanceRequestsRef.current.has(vpsId)) return
    const requests = instanceRequestsRef.current
    const requested = vpsId
    requests.add(requested)
    let current = true
    let settled = false
    Promise.resolve()
      .then(() => loaders.listMonitoringInstances(requested))
      .then(
        (items): Loaded => ({ items: items.map((item) => ({ id: item.monitoring_instance_id, label: item.display_name || item.monitoring_instance_id })) }),
        (): Loaded => ({ failed: true }),
      )
      .then((loaded) => {
        if (!current) return
        settled = true
        if ('failed' in loaded) requests.delete(requested)
        setInstances((existing) => new Map(existing).set(requested, loaded))
      })
    return () => {
      current = false
      if (!settled) requests.delete(requested)
    }
  }, [active, loaders, needsInstance, vpsId])

  const vpsOptions = vpsList && 'items' in vpsList ? vpsList.items : []
  const instanceLoad = vpsId ? instances.get(vpsId) : undefined
  const instanceOptions = instanceLoad && 'items' in instanceLoad ? instanceLoad.items : []
  return {
    vpsOptions,
    vpsId,
    selectVPS: (id: string) => {
      setVpsId(id)
      setInstanceId('')
    },
    instanceOptions,
    instanceId,
    selectInstance: setInstanceId,
    selectedVPS: vpsOptions.find((option) => option.id === vpsId),
    selectedInstance: instanceOptions.find((option) => option.id === instanceId),
    loading: active && (vpsList === null || (needsInstance && Boolean(vpsId) && !instanceLoad)),
    failed: Boolean(vpsList && 'failed' in vpsList) || Boolean(needsInstance && instanceLoad && 'failed' in instanceLoad),
  }
}

export type SubjectMonitoringInstance = Readonly<{ vpsId: string; id: string; label: string }>

// 记录主体里的 VPS 通常不直接带监控实例：读取这些 VPS 名下的实例，作为监控类证据的首选来源。
export function useSubjectMonitoringInstances(
  vpsIds: readonly string[],
  loaders: OtherEvidenceSourceLoaders | undefined,
): { instances: readonly SubjectMonitoringInstance[]; loading: boolean } {
  const listMonitoringInstances = loaders?.listMonitoringInstances
  const load = useCallback(
    // 结果不是列表时按读取失败处理：该 VPS 不提供首选来源，仍可通过“其他 VPS”选择。
    (vpsId: string) => Promise.resolve(listMonitoringInstances?.(vpsId) ?? []).then((items: unknown) => {
      if (!Array.isArray(items)) throw new Error('monitoring instance list unavailable')
      return items as MonitoringInstanceItems
    }),
    [listMonitoringInstances],
  )
  const ids = listMonitoringInstances ? vpsIds.filter((vpsId) => vpsId !== '') : []
  const lookups = useIdLookup(ids, load)
  const instances: SubjectMonitoringInstance[] = []
  let loading = false
  for (const vpsId of ids) {
    const entry = lookups.get(vpsId)
    if (!entry || entry.status === 'loading') loading = true
    if (entry?.status !== 'ready') continue
    for (const item of entry.metadata) {
      instances.push({ vpsId, id: item.monitoring_instance_id, label: item.display_name || item.monitoring_instance_id })
    }
  }
  return { instances, loading }
}
