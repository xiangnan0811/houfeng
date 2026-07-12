import type { HistoryTab, TimeWindow } from './types'

export const MONITORING_INSTANCE_BINDING_CONFLICT_LOAD_ERROR = '绑定冲突详情暂不可用'
export const MONITORING_INSTANCE_BINDING_ACTION_ERROR = '更新绑定冲突状态失败'
export const MONITORING_INSTANCE_BINDING_CONFLICT_STATUS = '指纹变更待确认'
export const MONITORING_INSTANCE_BINDING_CONFIRM_REBIND_LABEL = '确认重绑定'
export const MONITORING_INSTANCE_BINDING_REJECT_PENDING_LABEL = '拒绝新指纹'
export const MONITORING_INSTANCE_BINDING_RESET_LABEL = '重置绑定'

export const MAX_STDOUT_LINES = 20

export const TIME_WINDOW_ITEMS: Array<{ value: TimeWindow; label: string }> = [
  { value: 'realtime', label: '实时' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
]

export const HISTORY_TAB_ITEMS: Array<{ value: HistoryTab; label: string }> = [
  { value: 'events', label: '事件时间线' },
  { value: 'incidents', label: '历史异常' },
]
