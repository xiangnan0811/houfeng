import type { HistoryTab, MonitoringInstanceCommand, TimeWindow } from './types'

export const MONITORING_INSTANCE_BINDING_CONFLICT_LOAD_ERROR = '绑定冲突详情暂不可用'
export const MONITORING_INSTANCE_BINDING_ACTION_ERROR = '更新绑定冲突状态失败'
export const MONITORING_INSTANCE_LIFECYCLE_ACTION_ERROR = '监控实例生命周期操作失败'
export const MONITORING_INSTANCE_BINDING_CONFLICT_STATUS = '指纹变更待确认'
export const MONITORING_INSTANCE_BINDING_CONFIRM_REBIND_LABEL = '确认重绑定'
export const MONITORING_INSTANCE_BINDING_REJECT_PENDING_LABEL = '拒绝新指纹'
export const MONITORING_INSTANCE_BINDING_RESET_LABEL = '重置绑定'
export const MONITORING_INSTANCE_LIFECYCLE_RETIRED = '已退役'
export const MONITORING_INSTANCE_LIFECYCLE_V1_LIMITATION_COPY =
  '已退役监控实例在 V1 中只能先恢复到观察中，不能直接恢复为在用。'

export const COMMAND_LIST: MonitoringInstanceCommand[] = [
  { id: 'df_h', name: 'df -h', description: '磁盘使用概览' },
  { id: 'free_m', name: 'free -m', description: '内存使用情况' },
  { id: 'uptime', name: 'uptime', description: '系统运行时间与负载' },
  { id: 'top_head', name: 'top -bn1', description: '进程 CPU/内存排序快照' },
  { id: 'journalctl_u', name: 'journalctl --lines=50', description: '系统日志最近 50 行' },
  { id: 'systemctl_status', name: 'systemctl status', description: '所有 systemd unit 状态概览' },
  { id: 'dmesg_err', name: 'dmesg --level=err', description: '内核错误日志' },
  { id: 'docker_ps', name: 'docker ps', description: '运行中的 Docker 容器' },
]

export const COMMAND_LABELS: Record<string, string> = Object.fromEntries(
  COMMAND_LIST.map((command) => [command.id, command.name]),
)

export const MAX_STDOUT_LINES = 20

export const TIME_WINDOW_ITEMS: Array<{ value: TimeWindow; label: string }> = [
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
]

export const HISTORY_TAB_ITEMS: Array<{ value: HistoryTab; label: string }> = [
  { value: 'events', label: '事件时间线' },
  { value: 'incidents', label: '历史异常' },
]
