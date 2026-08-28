import { createContext, type ReactNode, useContext, useState } from 'react'

import { createVPSWriteOwnerStore, type VPSWriteOwnerStore } from './vpsWriteRegistry'

const VPSWriteRegistryContext = createContext<VPSWriteOwnerStore | null>(null)

export function VPSWriteRegistryProvider({ children }: { children: ReactNode }) {
  const [registry] = useState(createVPSWriteOwnerStore)
  return (
    <VPSWriteRegistryContext.Provider value={registry}>
      {children}
    </VPSWriteRegistryContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components -- provider hooks must share its private context
export function useVPSWriteRegistry(): VPSWriteOwnerStore {
  const registry = useContext(VPSWriteRegistryContext)
  if (!registry) throw new Error('useVPSWriteRegistry must be used within VPSWriteRegistryProvider')
  return registry
}

// eslint-disable-next-line react-refresh/only-export-components -- provider hooks must share its private context
export function useOptionalVPSWriteRegistry(): VPSWriteOwnerStore | null {
  return useContext(VPSWriteRegistryContext)
}
