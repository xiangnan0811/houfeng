import { SegmentedControl } from '../atoms'
import type { IPQualityIPVersion } from '../../lib/types'

const FAMILY_ITEMS = [
  { value: '4', label: 'IPv4' },
  { value: '6', label: 'IPv6' },
] as const

type FamilyValue = (typeof FAMILY_ITEMS)[number]['value']

type IPQualityFamilySelectorProps = {
  // null：参数无效或尚未确定，任一按钮都不呈现为已选。
  value: IPQualityIPVersion | null
  onChange: (next: IPQualityIPVersion) => void
}

// 两个族始终可选：缺族也能查看该族状态与旧历史。
export function IPQualityFamilySelector({ value, onChange }: IPQualityFamilySelectorProps) {
  return (
    <SegmentedControl<FamilyValue | ''>
      label="IP 地址族"
      items={FAMILY_ITEMS}
      value={value == null ? '' : value === 4 ? '4' : '6'}
      onChange={(next) => {
        if (next === '4' || next === '6') onChange(next === '4' ? 4 : 6)
      }}
    />
  )
}
