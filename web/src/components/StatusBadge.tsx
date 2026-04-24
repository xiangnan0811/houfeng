type StatusBadgeProps = {
  label: string
  tone?: 'cyan' | 'green' | 'yellow' | 'red' | 'slate'
}

function inferTone(label: string): StatusBadgeProps['tone'] {
  if (['正常', '启用', '已绑定', '在用'].includes(label)) return 'green'
  if (['维护中', '观察中', '待接入'].includes(label)) return 'yellow'
  if (['暂停', '已归档', '已退役', '不续费', '指纹变更待确认'].includes(label)) {
    return 'red'
  }
  if (['service', 'china_reference'].includes(label)) return 'cyan'
  return 'slate'
}

export function StatusBadge({ label, tone = inferTone(label) }: StatusBadgeProps) {
  return <span className={`status-badge status-badge--${tone}`}>{label}</span>
}
