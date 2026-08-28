export type VPSWriteOperation =
  | 'facts'
  | 'decision'
  | 'link'
  | 'monitoring-create'
  | 'subscription'
  | 'validity-extension'
  | 'monitoring-unlink'
  | 'lifecycle'
  | 'cancellation'
  | 'experience'
  | 'service'
  | 'domain'

export type VPSWriteOwner = {
  vpsId: string
  token: string
  generation: number
  operation: VPSWriteOperation
  monitoringInstanceId?: string
}

type BeginVPSWrite = Omit<VPSWriteOwner, 'token'>

export type VPSWriteOwnerStore = {
  getSnapshot: () => ReadonlyMap<string, VPSWriteOwner>
  subscribe: (listener: () => void) => () => void
  begin: (input: BeginVPSWrite) => VPSWriteOwner | null
  finish: (owner: VPSWriteOwner) => boolean
}

export function createVPSWriteOwnerStore(): VPSWriteOwnerStore {
  let snapshot: ReadonlyMap<string, VPSWriteOwner> = new Map()
  const listeners = new Set<() => void>()

  function publish(next: ReadonlyMap<string, VPSWriteOwner>) {
    snapshot = next
    for (const listener of listeners) listener()
  }

  return {
    getSnapshot: () => snapshot,
    subscribe(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    begin(input) {
      if (snapshot.has(input.vpsId)) return null
      const owner: VPSWriteOwner = {
        ...input,
        token: crypto.randomUUID(),
      }
      const next = new Map(snapshot)
      next.set(owner.vpsId, owner)
      publish(next)
      return owner
    },
    finish(owner) {
      if (snapshot.get(owner.vpsId)?.token !== owner.token) return false
      const next = new Map(snapshot)
      next.delete(owner.vpsId)
      publish(next)
      return true
    },
  }
}
