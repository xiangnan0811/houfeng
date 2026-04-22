export const PRODUCT_NAME_ZH = '候风'
export const PRODUCT_FULL_NAME_ZH = '候风 · 服务器舰队控制面'
export const PRODUCT_NAME_EN = 'Houfeng Fleet Control Plane'

export type PrimaryNavItem = {
  to: string
  label: string
  end?: boolean
}

export const PRIMARY_NAV_ITEMS: PrimaryNavItem[] = [
  { to: '/', label: '集群概览', end: true },
  { to: '/nodes', label: '节点' },
  { to: '/targets', label: '目标' },
  { to: '/events', label: '事件' },
  { to: '/settings', label: '设置' },
]
