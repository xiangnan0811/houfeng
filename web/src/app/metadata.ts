export interface NavItem {
  to: string
  label: string
  end?: boolean
}

export const PRODUCT_NAME_ZH = '候风'
export const PRODUCT_NAME_EN = 'Houfeng'
export const PRODUCT_FULL_NAME_ZH = '候风 · 服务器舰队控制面'
export const PRODUCT_FULL_NAME_EN = 'Houfeng Fleet Control Plane'

export const PRIMARY_NAV_ITEMS: NavItem[] = [
  { to: '/', label: '首页', end: true },
  { to: '/nodes', label: '节点' },
  { to: '/targets', label: '目标' },
  { to: '/events', label: '事件' },
  { to: '/settings', label: '设置' },
]
