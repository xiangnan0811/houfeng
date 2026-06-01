import { type FormEvent } from 'react'

import { Button, Input } from '../../components/atoms'
import type { VPSAssetDetail } from '../../lib/types'

type VPSMonitoringInstanceCreateFormProps = {
  detail: VPSAssetDetail
  submitting: boolean
  error: string | null
  notice: string | null
  onCancel: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSMonitoringInstanceCreateForm({
  detail,
  submitting,
  error,
  notice,
  onCancel,
  onSubmit,
}: VPSMonitoringInstanceCreateFormProps) {
  const inherited = [
    detail.display_name,
    detail.provider_name || '未关联服务商',
    [detail.region || detail.country, detail.city || detail.datacenter].filter(Boolean).join(' · ') || '位置未确认',
  ].join(' · ')

  return (
    <form className="asset-operation-form" onSubmit={onSubmit}>
      <div className="asset-operation-form__header">
        <h3>为 {detail.display_name} 创建监控实例</h3>
        <p>系统会继承 VPS 身份、服务商、位置、标签和备注，创建后直接进入接入抽屉生成安装命令。</p>
      </div>
      <Input label="继承字段" value={inherited} readOnly />
      {error ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p> : null}
      {notice ? <p className="asset-operation-feedback" role="status">{notice}</p> : null}
      <div className="page-form-actions">
        <Button variant="secondary" disabled={submitting} onClick={onCancel}>取消</Button>
        <Button type="submit" disabled={submitting}>{submitting ? '创建中…' : '创建并接入 agent'}</Button>
      </div>
    </form>
  )
}
