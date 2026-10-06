import { useEffect, useMemo, useRef, useState } from 'react'

export type IdLookupState<T> =
  | { status: 'loading' }
  | { status: 'ready'; metadata: T }
  | { status: 'unavailable' }

type SettledLookup<T> = Exclude<IdLookupState<T>, { status: 'loading' }>

export type IdLookupLoader<T> = (id: string, signal: AbortSignal) => Promise<T>

const LOADING = { status: 'loading' } as const

// 记录只给出附件 / 证据的 ID 列表，没有批量接口：逐个读取元数据并按 ID 缓存。
// 读不到（无权、已删除、服务异常）一律显示为不可用，不区分原因，避免泄露存在性。
// loader 必须引用稳定（模块级函数或 useCallback），否则每次渲染都会重新读取。
// 尚无结果即视为读取中：状态只在读取结束的异步回调里写入，effect 与 cleanup 不写状态。
export function useIdLookup<T>(
  ids: readonly string[],
  loadMetadata: IdLookupLoader<T>,
): ReadonlyMap<string, IdLookupState<T>> {
  const [settled, setSettled] = useState<ReadonlyMap<string, SettledLookup<T>>>(() => new Map())
  // 已发起或已有结果的 ID；中断的读取会移出，交给下一次 effect 重新读取。
  const requestedRef = useRef(new Set<string>())
  const key = [...new Set(ids)].sort().join('\n')

  useEffect(() => {
    const requested = requestedRef.current
    const missing = (key ? key.split('\n') : []).filter((id) => !requested.has(id))
    if (missing.length === 0) return
    const controller = new AbortController()
    const finished = new Set<string>()
    for (const id of missing) {
      requested.add(id)
      // 先进入 Promise 链：加载函数同步抛错或返回非 Promise 时也只记为不可用。
      Promise.resolve().then(() => loadMetadata(id, controller.signal)).then(
        (metadata): SettledLookup<T> => ({ status: 'ready', metadata }),
        (): SettledLookup<T> => ({ status: 'unavailable' }),
      ).then((state) => {
        if (controller.signal.aborted) return
        finished.add(id)
        setSettled((current) => new Map(current).set(id, state))
      })
    }
    return () => {
      controller.abort()
      for (const id of missing) {
        if (!finished.has(id)) requested.delete(id)
      }
    }
  }, [key, loadMetadata])

  return useMemo(() => {
    const entries = new Map<string, IdLookupState<T>>()
    for (const id of key ? key.split('\n') : []) entries.set(id, settled.get(id) ?? LOADING)
    return entries
  }, [key, settled])
}
