export interface NavItem {
  to: string
  label: string
  end?: boolean
}

export interface NavGroup {
  label: string
  items: NavItem[]
}

export const PRODUCT_NAME_ZH = '候风'
export const PRODUCT_NAME_EN = 'Houfeng'
export const PRODUCT_FULL_NAME_ZH = '候风 · 服务器舰队控制面'
export const PRODUCT_FULL_NAME_EN = 'Houfeng Fleet Control Plane'

export const PRIMARY_NAV_GROUPS: NavGroup[] = [
  {
    label: '总览',
    items: [{ to: '/', label: '工作台', end: true }],
  },
  {
    label: '资产',
    items: [
      { to: '/asset-decisions', label: '资产决策' },
      { to: '/vps', label: 'VPS' },
      { to: '/providers', label: '服务商' },
      { to: '/subscriptions', label: '订阅' },
    ],
  },
  {
    label: '观测',
    items: [
      { to: '/nodes', label: '节点' },
      { to: '/targets', label: '目标' },
      { to: '/events', label: '事件' },
    ],
  },
  {
    label: '系统',
    items: [{ to: '/settings', label: '设置' }],
  },
]

export const PRIMARY_NAV_ITEMS: NavItem[] = PRIMARY_NAV_GROUPS.flatMap((group) => group.items)
