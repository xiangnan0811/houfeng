import { createContext, useContext } from 'react'

type SidebarContextValue = {
  collapsed: boolean
  setCollapsed: (v: boolean) => void
}

export const SidebarContext = createContext<SidebarContextValue>({
  collapsed: false,
  setCollapsed: () => {},
})

export function useSidebar() {
  return useContext(SidebarContext)
}
