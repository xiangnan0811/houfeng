import { useCallback, useMemo, useState } from 'react'

export type VPSManagementPanel =
  | null
  | 'menu'
  | 'facts'
  | 'decision'
  | 'subscription'
  | 'validity-extension'
  | 'cancellation'
  | 'archive'
  | 'monitoring-instance-create'
  | 'monitoring-instance-evidence'
  | 'monitoring-instance-link'
  | 'services-detail'
  | 'service'
  | 'domains-detail'
  | 'domain'

export type VPSManagementController = {
  panel: VPSManagementPanel
  menuOpen: boolean
  openMenu: () => void
  closeMenu: () => void
  openPanel: (panel: Exclude<VPSManagementPanel, null | 'menu'>) => void
  closePanel: () => void
}

/**
 * Owns management menu / modal visibility for the overview composition.
 * Mutation payloads stay in the modal owners; this hook only routes focus.
 */
export function useVPSManagementController(): VPSManagementController {
  const [panel, setPanel] = useState<VPSManagementPanel>(null)

  const openMenu = useCallback(() => setPanel('menu'), [])
  const closeMenu = useCallback(() => {
    setPanel((current) => (current === 'menu' ? null : current))
  }, [])
  const openPanel = useCallback((next: Exclude<VPSManagementPanel, null | 'menu'>) => {
    setPanel(next)
  }, [])
  const closePanel = useCallback(() => setPanel(null), [])

  return useMemo(() => ({
    panel,
    menuOpen: panel === 'menu',
    openMenu,
    closeMenu,
    openPanel,
    closePanel,
  }), [panel, openMenu, closeMenu, openPanel, closePanel])

}
