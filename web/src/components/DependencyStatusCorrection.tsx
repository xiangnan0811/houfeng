import { useState } from 'react'

import { dependencyStatusChoices } from '../lib/assetLifecycle'
import { updateAssetDomainStatus, updateAssetServiceStatus } from '../lib/api'
import { ApiError } from '../lib/apiRequest'
import type { DependencyCorrectionStatus } from '../lib/types'
import { Button, Input, Modal, Select } from './atoms'

type DependencyStatusCorrectionProps = {
  open: boolean
  kind: 'service' | 'domain'
  objectId: string
  displayName: string
  currentStatus: string
  parentLifecycle: string
  onClose: () => void
  onCompleted: (message: string) => void
}

export function DependencyStatusCorrection({
  open,
  kind,
  objectId,
  displayName,
  currentStatus,
  parentLifecycle,
  onClose,
  onCompleted,
}: DependencyStatusCorrectionProps) {
  const name = displayName
  const choices = dependencyStatusChoices(parentLifecycle)
  const [status, setStatus] = useState<DependencyCorrectionStatus | ''>(
    () => choices.find((choice) => choice.value === currentStatus && !choice.disabled)?.value ?? '',
  )
  const [reason, setReason] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const selected = choices.find((choice) => choice.value === status)

  async function submit() {
    if (status === '') {
      setError('请选择纠正后的状态。')
      return
    }
    const cleanReason = reason.trim()
    if (!cleanReason) {
      setError('需要填写纠正原因。')
      return
    }
    if (selected?.disabled) {
      setError(selected.reason ?? '当前父节点不允许这个状态。')
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      if (kind === 'service') {
        await updateAssetServiceStatus(objectId, { status, reason: cleanReason })
      } else {
        await updateAssetDomainStatus(objectId, { status, reason: cleanReason })
      }
      onCompleted(`${name} 的状态已纠正为 ${selected?.label ?? status}。归属和入口探测没有改动。`)
      onClose()
    } catch (caught: unknown) {
      setError(caught instanceof ApiError ? caught.message : '纠正依赖状态失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title={kind === 'service' ? '纠正服务状态' : '纠正域名状态'} size="md">
      <div className="asset-lifecycle-confirm">
        <p>{name}</p>
        <p className="asset-lifecycle-confirm__callouts">只改状态，不改归属，也不改关联的入口探测。</p>
        <Select
          label="状态"
          value={status}
          disabled={submitting}
          onChange={(event) => setStatus(event.target.value as DependencyCorrectionStatus)}
        >
          <option value="" disabled>请选择纠正后的状态</option>
          {choices.map((choice) => (
            <option key={choice.value} value={choice.value} disabled={choice.disabled}>
              {choice.label}{choice.disabled ? `（${choice.reason}）` : ''}
            </option>
          ))}
        </Select>
        {selected?.disabled ? <p role="status">{selected.reason}</p> : null}
        <Input label="原因" value={reason} disabled={submitting} onChange={(event) => setReason(event.target.value)} />
        {error ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p> : null}
        <div className="page-form-actions">
          <Button variant="secondary" onClick={onClose} disabled={submitting}>取消</Button>
          <Button onClick={() => void submit()} disabled={submitting || status === '' || Boolean(selected?.disabled)}>确认纠正</Button>
        </div>
      </div>
    </Modal>
  )
}
