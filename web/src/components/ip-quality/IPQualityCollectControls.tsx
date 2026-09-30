import { Link } from 'react-router-dom'

import { Button } from '../atoms'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import { collectNotice, collectUnavailableLabel } from './ipQualityCollectPresentation'
import type { IPQualityCollectController } from './useIPQualityCollect'

type CollectButtonProps = {
  collect: IPQualityCollectController
  variant?: 'primary' | 'secondary'
}

export function IPQualityCollectButton({ collect, variant = 'primary' }: CollectButtonProps) {
  // 只读预览不提供写操作入口。
  if (READ_ONLY_PREVIEW) return null
  const unavailable = collect.status != null && !collect.status.available
  const busy = collect.submitting || collect.active
  const label = collect.submitting ? '正在发起…' : collect.active ? '采集中…' : '立即采集'
  return (
    <Button
      size="md"
      variant={variant}
      className={busy ? 'ipq-collect-button ipq-collect-button--busy' : 'ipq-collect-button'}
      onClick={collect.start}
      disabled={busy || unavailable}
      aria-busy={busy || undefined}
      title={unavailable ? collectUnavailableLabel(collect.status?.unavailable_reason) : undefined}
    >
      {busy ? <span className="ipq-pulse" aria-hidden="true" /> : null}
      {label}
    </Button>
  )
}

type CollectNoticeProps = {
  collect: IPQualityCollectController
  now?: Date
}

// 采集进度与结果条：只在有事可说时出现，不常驻解释文字。
export function IPQualityCollectNotice({ collect, now = new Date() }: CollectNoticeProps) {
  const notice = collect.error
    ? { tone: 'warning' as const, title: collect.error }
    : collectNotice(collect.status, collect.watchedRequestId, now)
  if (!notice) return <div className="ipq-notice-slot" role="status" aria-live="polite" aria-label="IP 质量采集状态" />
  const disabled = collect.status?.unavailable_reason === 'disabled'
  return (
    <div className="ipq-notice-slot" role="status" aria-live="polite" aria-label="IP 质量采集状态">
      <div className={`ipq-notice ipq-notice--${notice.tone}`}>
        {notice.tone === 'progress' ? <span className="ipq-pulse" aria-hidden="true" /> : <span className="ipq-notice__dot" aria-hidden="true" />}
        <span className="ipq-notice__title">{notice.title}</span>
        {notice.detail ? <span className="ipq-notice__detail">{notice.detail}</span> : null}
        {disabled ? <Link className="text-link ipq-notice__action" to="/settings?tab=monitoring">前往设置开启</Link> : null}
      </div>
    </div>
  )
}
