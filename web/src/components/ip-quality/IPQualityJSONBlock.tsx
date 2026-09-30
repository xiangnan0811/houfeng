import { useMemo, useState } from 'react'

import { VPSCopyValueButton } from '../../pages/vps-detail/VPSCopyValueButton'

type IPQualityJSONBlockProps = {
  title: string
  value: unknown
}

function prettyJSON(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2) ?? '—'
  } catch {
    return '无法展示'
  }
}

function byteSize(value: unknown): string {
  let text = ''
  try {
    text = JSON.stringify(value) ?? ''
  } catch {
    return '—'
  }
  const bytes = new TextEncoder().encode(text).length
  if (bytes < 1024) return `${bytes} B`
  return `${(bytes / 1024).toFixed(1)} KB`
}

// 原始 JSON 默认折叠；展开后才格式化，并限制在固定高度的滚动区里，不撑开页面。
export function IPQualityJSONBlock({ title, value }: IPQualityJSONBlockProps) {
  const [open, setOpen] = useState(false)
  const size = useMemo(() => byteSize(value), [value])
  const pretty = useMemo(() => (open ? prettyJSON(value) : null), [open, value])
  return (
    <details className="ipq-json" onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary className="ipq-json__summary">
        <span className="ipq-json__title">{title}</span>
        <span className="ipq-json__size">{size}</span>
      </summary>
      {pretty != null ? (
        <div className="ipq-json__body">
          <pre className="ipq-json__code" tabIndex={0} aria-label={`${title}内容`}>{pretty}</pre>
          <div className="ipq-json__actions">
            <VPSCopyValueButton value={pretty} label={title} />
          </div>
        </div>
      ) : null}
    </details>
  )
}
