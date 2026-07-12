export type CommandSensitivity = 'standard' | 'sensitive'

export type MonitoringInstanceCommand = {
  id: string
  name: string
  description: string
  sensitivity: CommandSensitivity
}

export const COMMAND_LIST: MonitoringInstanceCommand[] = [
  { id: 'df_h', name: 'df -h', description: '磁盘使用概览', sensitivity: 'standard' },
  { id: 'free_m', name: 'free -m', description: '内存使用情况', sensitivity: 'standard' },
  { id: 'uptime', name: 'uptime', description: '系统运行时间与负载', sensitivity: 'standard' },
  { id: 'top_head', name: 'top -bn1', description: '进程 CPU/内存排序快照', sensitivity: 'sensitive' },
  { id: 'journalctl_u', name: 'journalctl --lines=50', description: '系统日志最近 50 行', sensitivity: 'sensitive' },
  { id: 'systemctl_status', name: 'systemctl status', description: '所有 systemd unit 状态概览', sensitivity: 'sensitive' },
  { id: 'dmesg_err', name: 'dmesg --level=err', description: '内核错误日志', sensitivity: 'sensitive' },
  { id: 'docker_ps', name: 'docker ps', description: '运行中的 Docker 容器', sensitivity: 'sensitive' },
]

export const COMMAND_LABELS: Record<string, string> = Object.fromEntries(
  COMMAND_LIST.map((command) => [command.id, command.name]),
)

export const COMMAND_OPTIONS = [
  { value: '', label: '全部命令' },
  ...COMMAND_LIST.map((command) => ({ value: command.id, label: command.name })),
]

const COMMAND_SENSITIVITY: Record<string, CommandSensitivity> = Object.fromEntries(
  COMMAND_LIST.map((command) => [command.id, command.sensitivity]),
)

export function commandSensitivity(commandId: string): CommandSensitivity | undefined {
  return COMMAND_SENSITIVITY[commandId]
}
