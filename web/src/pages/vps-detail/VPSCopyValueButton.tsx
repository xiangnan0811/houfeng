import { useState } from 'react'

import { Button } from '../../components/atoms'
import { useCopyToClipboard } from '../../lib/useCopyToClipboard'

type Props = {
  value: string
  label: string
}

type Attempt = 'idle' | 'pending' | 'copied' | 'failed'

function rawCopyValue(value: string): string | null {
  const raw = value.trim()
  if (!raw || raw === '—') return null
  return raw
}

export function VPSCopyValueButton({ value, label }: Props) {
  const raw = rawCopyValue(value)
  if (!raw) return null
  return <VPSCopyValueButtonLeaf key={raw} value={raw} label={label} />
}

function VPSCopyValueButtonLeaf({ value, label }: { value: string; label: string }) {
  const { copy } = useCopyToClipboard()
  const [attempt, setAttempt] = useState<Attempt>('idle')

  async function onCopy() {
    setAttempt('pending')
    const ok = await copy(value)
    setAttempt(ok ? 'copied' : 'failed')
  }

  const buttonLabel = attempt === 'pending'
    ? '复制中…'
    : attempt === 'failed'
      ? '复制失败'
      : attempt === 'copied'
        ? '已复制'
        : '复制'
  const live = attempt === 'failed' ? `${label}复制失败` : attempt === 'copied' ? `${label}已复制` : ''

  return (
    <span className="vps-copy-value">
      <Button
        type="button"
        size="sm"
        variant="ghost"
        aria-label={`复制${label}`}
        disabled={attempt === 'pending'}
        onClick={() => void onCopy()}
      >
        {buttonLabel}
      </Button>
      <span className="visually-hidden" aria-live="polite">
        {live}
      </span>
    </span>
  )
}
