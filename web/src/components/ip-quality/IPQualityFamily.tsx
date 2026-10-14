import { SegmentedControl, Timestamp } from '../atoms'
import type { IPQualityIPVersion, VPSIPQualityReport } from '../../lib/types'
import { familyStateNotice } from './ipQualityPresentation'

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

type IPQualityFamilyStateProps = {
  report: VPSIPQualityReport
  ipVersion: IPQualityIPVersion
}

// 所选族最新尝试状态：失败/缺族/过期与“上次有效结果”分开说明，不借另一族事实。
export function IPQualityFamilyState({ report, ipVersion }: IPQualityFamilyStateProps) {
  const notice = familyStateNotice(report, ipVersion)
  if (!notice) return null
  return (
    <aside className={`ipq-notice ipq-notice--${notice.tone}`} role="note" aria-label="地址族采集状态">
      <span className="ipq-notice__dot" aria-hidden="true" />
      <span className="ipq-notice__title">{notice.title}</span>
      {notice.detail ? <span className="ipq-notice__detail">{notice.detail}</span> : null}
      {notice.lastValidAt ? (
        <span className="ipq-notice__detail">
          以下为上次有效结果（<Timestamp value={notice.lastValidAt} mode="both" />）
        </span>
      ) : null}
    </aside>
  )
}
