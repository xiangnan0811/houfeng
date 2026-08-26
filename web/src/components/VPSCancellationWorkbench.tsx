import { useMemo, useState } from 'react'

import {
  ASSET_SERVICE_STATUS_LABELS,
  ASSET_DOMAIN_STATUS_LABELS,
  type ApplyCancellationInput,
  type CancellationPreview,
  type LifecycleActionResult,
  type TargetRunStatus,
} from '../lib/types'
import { renewalModeFromLegacy, renewalModeLabel } from '../lib/assetOptions'
import { formatDate, formatOptional } from '../lib/format'
import { Badge, Button, Input, MonoDigits, Select } from './atoms'
import { LifecycleBadge, RenewalBadge, SubscriptionStatusBadge } from '../pages/assetPageBadges'

type WorkbenchMonitoringInstanceChoice = {
  enabled: boolean
  lifecycleStatus: '' | '不续费' | '已退役'
  pauseMonitoring: boolean
}

type WorkbenchTargetChoice = {
  enabled: boolean
  runStatus: TargetRunStatus
}

type WorkbenchProps = {
  preview: CancellationPreview
  submitting: boolean
  error: string | null
  result?: LifecycleActionResult | null
  onSubmit: (input: ApplyCancellationInput) => Promise<void> | void
  onCancel?: () => void
}

function defaultVPSLifecycle(preview: CancellationPreview): 'to_cancel' | 'cancelled' {
  const recommended = preview.recommended_steps.find((step) => step.object_type === 'vps')?.to_state
  if (recommended?.includes('cancelled')) return 'cancelled'
  if (recommended?.includes('to_cancel')) return 'to_cancel'
  if (preview.vps.lifecycle_status === 'cancelled') return 'cancelled'
  return 'to_cancel'
}

function targetIDsFromPreview(preview: CancellationPreview): string[] {
  return preview.target_links.map((target) => target.target_id)
}

function buildInitialMonitoringInstanceChoices(preview: CancellationPreview): Record<string, WorkbenchMonitoringInstanceChoice> {
  const map: Record<string, WorkbenchMonitoringInstanceChoice> = {}
  const actualCancelled = defaultVPSLifecycle(preview) === 'cancelled'
  for (const monitoringInstance of preview.monitoring_instance_links) {
    map[monitoringInstance.monitoring_instance_id] = {
      enabled: false,
      lifecycleStatus: actualCancelled ? '已退役' : '不续费',
      pauseMonitoring: actualCancelled,
    }
  }
  return map
}

function buildInitialTargetChoices(preview: CancellationPreview): Record<string, WorkbenchTargetChoice> {
  const map: Record<string, WorkbenchTargetChoice> = {}
  for (const target of preview.target_links) {
    map[target.target_id] = {
      enabled: false,
      runStatus: '已归档',
    }
  }
  return map
}

export function VPSCancellationWorkbench({
  preview,
  submitting,
  error,
  result,
  onSubmit,
  onCancel,
}: WorkbenchProps) {
  const [reason, setReason] = useState('')
  const [effectiveDate, setEffectiveDate] = useState(() => new Date().toISOString().slice(0, 10))
  const [vpsLifecycleStatus, setVpsLifecycleStatus] = useState<'to_cancel' | 'cancelled'>(() => defaultVPSLifecycle(preview))
  const [subscriptionIDs, setSubscriptionIDs] = useState<string[]>([])
  const [monitoringInstanceChoices, setMonitoringInstanceChoices] = useState<Record<string, WorkbenchMonitoringInstanceChoice>>(() => buildInitialMonitoringInstanceChoices(preview))
  const [targetChoices, setTargetChoices] = useState<Record<string, WorkbenchTargetChoice>>(() => buildInitialTargetChoices(preview))
  const [validationError, setValidationError] = useState<string | null>(null)
  const targetIDs = useMemo(() => targetIDsFromPreview(preview), [preview])
  const activeSubscriptions = preview.subscriptions.filter((impact) => impact.record.status === 'active')
  const inactiveSubscriptions = preview.subscriptions.filter((impact) => impact.record.status !== 'active')

  function toggleSubscription(subscriptionID: string, checked: boolean) {
    setSubscriptionIDs((current) =>
      checked
        ? Array.from(new Set([...current, subscriptionID]))
        : current.filter((id) => id !== subscriptionID),
    )
  }

  function updateMonitoringInstanceChoice(monitoringInstanceID: string, patch: Partial<WorkbenchMonitoringInstanceChoice>) {
    setMonitoringInstanceChoices((current) => {
      const existing = current[monitoringInstanceID]
      if (!existing) return current
      return {
        ...current,
        [monitoringInstanceID]: { ...existing, ...patch },
      }
    })
  }

  function updateTargetChoice(targetID: string, patch: Partial<WorkbenchTargetChoice>) {
    setTargetChoices((current) => {
      const existing = current[targetID]
      if (!existing) return current
      return {
        ...current,
        [targetID]: { ...existing, ...patch },
      }
    })
  }

  function buildInput(): ApplyCancellationInput {
    const cleanReason = reason.trim()
    if (!cleanReason) {
      throw new Error('需要填写取消/退役原因。')
    }
    if (activeSubscriptions.length > 0 && subscriptionIDs.length === 0) {
      throw new Error('请显式选择要取消自动续费的 active 订阅。')
    }
    const monitoringInstanceActions: ApplyCancellationInput['monitoring_instance_actions'] = []
    for (const monitoringInstance of preview.monitoring_instance_links) {
      const choice = monitoringInstanceChoices[monitoringInstance.monitoring_instance_id]
      if (!choice?.enabled) continue
      monitoringInstanceActions.push({
        monitoring_instance_id: monitoringInstance.monitoring_instance_id,
        ...(choice.lifecycleStatus ? { lifecycle_status: choice.lifecycleStatus } : {}),
        ...(choice.pauseMonitoring ? { monitoring_status: '暂停' } : {}),
      })
    }
    const targetActions: ApplyCancellationInput['target_actions'] = []
    for (const targetID of targetIDs) {
      const choice = targetChoices[targetID]
      if (!choice?.enabled) continue
      targetActions.push({
        target_id: targetID,
        run_status: choice.runStatus,
      })
    }
    return {
      reason: cleanReason,
      effective_date: effectiveDate || null,
      subscription_ids: subscriptionIDs,
      vps_lifecycle_status: vpsLifecycleStatus,
      monitoring_instance_actions: monitoringInstanceActions,
      target_actions: targetActions,
      preview_digest: preview.preview_digest,
    }
  }

  async function submit() {
    setValidationError(null)
    let input: ApplyCancellationInput
    try {
      input = buildInput()
    } catch (err: unknown) {
      setValidationError(err instanceof Error ? err.message : '输入无效')
      return
    }
    await onSubmit(input)
  }

  const blocking = preview.blockers.length > 0
  const selectedMonitoringInstanceCount = Object.values(monitoringInstanceChoices).filter((choice) => choice.enabled).length
  const selectedTargetCount = Object.values(targetChoices).filter((choice) => choice.enabled).length
  const selectedStepCount = subscriptionIDs.length + selectedMonitoringInstanceCount + selectedTargetCount + 1
  const vpsLifecycleLabel = vpsLifecycleStatus === 'cancelled' ? '已取消' : '待取消'

  return (
    <div className="asset-cancel-workbench">
      <section className="asset-cancel-workbench__summary" aria-label="取消/退役影响范围摘要">
        <div className="asset-cancel-workbench__summary-item asset-cancel-workbench__summary-item--identity">
          <span className="summary-card__label">VPS</span>
          <strong className="summary-card__value--text">{preview.vps.display_name}</strong>
          <small>{preview.vps.vps_id}</small>
        </div>
        <div className="asset-cancel-workbench__summary-item">
          <span className="summary-card__label">订阅</span>
          <strong className="summary-card__value"><MonoDigits>{preview.subscriptions.length}</MonoDigits></strong>
          <small>active {activeSubscriptions.length} · 非活跃 {inactiveSubscriptions.length}</small>
        </div>
        <div className="asset-cancel-workbench__summary-item">
          <span className="summary-card__label">监控实例</span>
          <strong className="summary-card__value"><MonoDigits>{preview.monitoring_instance_links.length}</MonoDigits></strong>
          <small>已选择 {selectedMonitoringInstanceCount} 个变更</small>
        </div>
        <div className="asset-cancel-workbench__summary-item">
          <span className="summary-card__label">Target/实例</span>
          <strong className="summary-card__value"><MonoDigits>{preview.target_links.length}</MonoDigits></strong>
          <small>已选择 {selectedTargetCount} 个变更</small>
        </div>
      </section>

      {preview.warnings.length > 0 || preview.blockers.length > 0 ? (
        <section className="asset-cancel-workbench__notices" aria-label="生命周期提示">
          {preview.blockers.map((item) => (
            <p key={item} className="asset-operation-feedback asset-operation-feedback--error" role="alert">{item}</p>
          ))}
          {preview.warnings.map((item) => (
            <p key={item} className="asset-operation-feedback asset-operation-feedback--notice" role="status">{item}</p>
          ))}
        </section>
      ) : null}

      <div className="asset-cancel-workbench__body">
        <div className="asset-cancel-workbench__rail asset-cancel-workbench__rail--decision">
          <section className="asset-cancel-workbench__section asset-cancel-workbench__section--vps">
            <div className="asset-cancel-workbench__section-head">
              <div>
                <p className="asset-cancel-workbench__eyebrow">VPS 状态</p>
                <h3>取消/退役目标</h3>
              </div>
              <Badge variant="state" tone={vpsLifecycleStatus === 'cancelled' ? 'critical' : 'notice'}>
                {vpsLifecycleLabel}
              </Badge>
            </div>
            <div className="asset-cancel-workbench__fact-strip" aria-label="当前 VPS 状态">
              <div>
                <span>当前生命周期</span>
                <LifecycleBadge value={preview.vps.lifecycle_status} />
              </div>
              <div>
                <span>续费决策</span>
                <RenewalBadge value={preview.vps.renewal_decision} />
              </div>
            </div>
            <div className="asset-cancel-workbench__field-grid asset-cancel-workbench__field-grid--vps">
              <Select
                label="VPS 生命周期"
                value={vpsLifecycleStatus}
                onChange={(event) => setVpsLifecycleStatus(event.target.value as 'to_cancel' | 'cancelled')}
                options={[
                  { value: 'cancelled', label: '已取消' },
                  { value: 'to_cancel', label: '待取消' },
                ]}
              />
              <Input
                label="生效日期"
                type="date"
                value={effectiveDate}
                onChange={(event) => setEffectiveDate(event.target.value)}
              />
            </div>
          </section>

          <section className="asset-cancel-workbench__section asset-cancel-workbench__section--audit">
            <div className="asset-cancel-workbench__section-head">
              <div>
                <p className="asset-cancel-workbench__eyebrow">确认执行</p>
                <h3>确认执行</h3>
              </div>
              <span className="asset-cancel-workbench__step-count">
                <MonoDigits>{selectedStepCount}</MonoDigits> steps
              </span>
            </div>
            <Input
              label="原因"
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder="例如: 已过期且不准备续费"
              disabled={submitting}
            />
            {validationError ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{validationError}</p> : null}
            {error ? <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">{error}</p> : null}
            {result ? (
              <p className="asset-operation-feedback" role="status">
                已完成生命周期动作 {result.action.action_id}，写入 {result.steps.length} 个步骤。
              </p>
            ) : null}
            <div className="asset-cancel-workbench__actions">
              {onCancel ? (
                <Button variant="secondary" onClick={onCancel} disabled={submitting}>关闭</Button>
              ) : null}
              <Button variant="danger" onClick={() => void submit()} disabled={submitting || blocking}>
                {submitting ? '执行中…' : '确认取消/退役'}
              </Button>
            </div>
          </section>
        </div>

        <div className="asset-cancel-workbench__rail asset-cancel-workbench__rail--confirm">
          <section className="asset-cancel-workbench__section">
            <div className="asset-cancel-workbench__section-head">
              <div>
                <p className="asset-cancel-workbench__eyebrow">订阅</p>
                <h3>订阅处理</h3>
              </div>
              <span className="asset-cancel-workbench__step-count">
                <MonoDigits>{subscriptionIDs.length}</MonoDigits> selected
              </span>
            </div>
            {preview.subscriptions.length === 0 ? (
              <p className="asset-cancel-workbench__empty">没有订阅记录。</p>
            ) : (
              <div className="asset-cancel-workbench__list">
                {preview.subscriptions.map((impact) => {
                  const subscription = impact.record
                  const selectable = subscription.status === 'active'
                  const checked = subscriptionIDs.includes(subscription.subscription_id)
                  return (
                    <label
                      key={subscription.subscription_id}
                      className={[
                        'asset-cancel-workbench__row',
                        'asset-cancel-workbench__choice',
                        checked && 'asset-cancel-workbench__choice--selected',
                        !selectable && 'asset-cancel-workbench__choice--disabled',
                      ].filter(Boolean).join(' ')}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={!selectable || submitting}
                        onChange={(event) => toggleSubscription(subscription.subscription_id, event.target.checked)}
                      />
                      <span className="asset-cancel-workbench__choice-main">
                        <span className="asset-cancel-workbench__choice-title">
                          <strong>{subscription.subscription_id}</strong>
                          <SubscriptionStatusBadge value={subscription.status} />
                        </span>
                        <small>{formatDate(subscription.renew_at)} · {renewalModeLabel(subscription.renewal_mode ?? renewalModeFromLegacy(subscription))}</small>
                        <span className="asset-cancel-workbench__choice-note">{impact.message}</span>
                      </span>
                    </label>
                  )
                })}
              </div>
            )}
          </section>

          <section className="asset-cancel-workbench__section">
            <div className="asset-cancel-workbench__section-head">
              <div>
                <p className="asset-cancel-workbench__eyebrow">监控实例</p>
                <h3>监控实例确认</h3>
              </div>
              <span className="asset-cancel-workbench__step-count">
                <MonoDigits>{selectedMonitoringInstanceCount}</MonoDigits> selected
              </span>
            </div>
            {preview.monitoring_instance_links.length === 0 ? (
              <p className="asset-cancel-workbench__empty">没有活跃监控实例关联。</p>
            ) : (
              <div className="asset-cancel-workbench__list">
                {preview.monitoring_instance_links.map((monitoringInstance) => {
                  const choice = monitoringInstanceChoices[monitoringInstance.monitoring_instance_id]
                  return (
                    <div
                      key={monitoringInstance.monitoring_instance_id}
                      className={[
                        'asset-cancel-workbench__row',
                        'asset-cancel-workbench__choice',
                        choice?.enabled && 'asset-cancel-workbench__choice--selected',
                      ].filter(Boolean).join(' ')}
                    >
                      <label className="asset-checkbox-line asset-cancel-workbench__choice-toggle">
                        <input
                          type="checkbox"
                          checked={choice?.enabled ?? false}
                          disabled={submitting}
                          onChange={(event) => updateMonitoringInstanceChoice(monitoringInstance.monitoring_instance_id, { enabled: event.target.checked })}
                        />
                        <span className="asset-cancel-workbench__choice-main">
                          <span className="asset-cancel-workbench__choice-title">
                            <strong>{monitoringInstance.display_name}</strong>
                            <Badge variant="state" tone={monitoringInstance.lifecycle_status === '已退役' ? 'offline' : 'normal'}>
                              {monitoringInstance.lifecycle_status}
                            </Badge>
                          </span>
                          <small>{formatOptional(monitoringInstance.provider)} · 监控 {monitoringInstance.monitoring_status}</small>
                        </span>
                      </label>
                      <div className="asset-cancel-workbench__controls">
                        <Select
                          label="生命周期"
                          value={choice?.lifecycleStatus ?? ''}
                          disabled={!choice?.enabled || submitting}
                          onChange={(event) => updateMonitoringInstanceChoice(monitoringInstance.monitoring_instance_id, { lifecycleStatus: event.target.value as WorkbenchMonitoringInstanceChoice['lifecycleStatus'] })}
                          options={[
                            { value: '不续费', label: '不续费' },
                            { value: '已退役', label: '已退役' },
                          ]}
                        />
                        <label className="asset-cancel-workbench__inline-check">
                          <input
                            type="checkbox"
                            checked={choice?.pauseMonitoring ?? false}
                            disabled={!choice?.enabled || submitting}
                            onChange={(event) => updateMonitoringInstanceChoice(monitoringInstance.monitoring_instance_id, { pauseMonitoring: event.target.checked })}
                          />
                          <span>暂停监控</span>
                        </label>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </section>

          <section className="asset-cancel-workbench__section">
            <div className="asset-cancel-workbench__section-head">
              <div>
                <p className="asset-cancel-workbench__eyebrow">入口探测</p>
                <h3>Target/实例确认</h3>
              </div>
              <span className="asset-cancel-workbench__step-count">
                <MonoDigits>{selectedTargetCount}</MonoDigits> selected
              </span>
            </div>
            {preview.target_links.length === 0 ? (
              <p className="asset-cancel-workbench__empty">没有关联的 Target/实例。</p>
            ) : (
              <div className="asset-cancel-workbench__list">
                {preview.target_links.map((target) => {
                  const choice = targetChoices[target.target_id]
                  return (
                    <div
                      key={target.target_id}
                      className={[
                        'asset-cancel-workbench__row',
                        'asset-cancel-workbench__choice',
                        choice?.enabled && 'asset-cancel-workbench__choice--selected',
                      ].filter(Boolean).join(' ')}
                    >
                      <label className="asset-checkbox-line asset-cancel-workbench__choice-toggle">
                        <input
                          type="checkbox"
                          checked={choice?.enabled ?? false}
                          disabled={submitting}
                          onChange={(event) => updateTargetChoice(target.target_id, { enabled: event.target.checked })}
                        />
                        <span className="asset-cancel-workbench__choice-main">
                          <span className="asset-cancel-workbench__choice-title">
                            <strong>{target.name || target.target_id}</strong>
                            <Badge variant="state" tone={target.run_status === '已归档' ? 'offline' : 'notice'}>
                              {target.run_status}
                            </Badge>
                          </span>
                          <small>服务 {target.service_ids.length} · 域名 {target.domain_ids.length}</small>
                        </span>
                      </label>
                      <div className="asset-cancel-workbench__controls asset-cancel-workbench__controls--target">
                        <Select
                          label="运行状态"
                          value={choice?.runStatus ?? '已归档'}
                          disabled={!choice?.enabled || submitting}
                          onChange={(event) => updateTargetChoice(target.target_id, { runStatus: event.target.value as TargetRunStatus })}
                          options={[
                            { value: '已归档', label: '已归档' },
                            { value: '暂停', label: '暂停' },
                          ]}
                        />
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </section>
        </div>
      </div>

      {preview.services.length > 0 || preview.domains.length > 0 ? (
        <details className="asset-cancel-workbench__details">
          <summary>服务与域名上下文</summary>
          <div className="asset-cancel-workbench__context-grid">
            {preview.services.map((service) => (
              <div key={service.service_id}>
                <span>服务</span>
                <strong>{service.name}</strong>
                <small>{ASSET_SERVICE_STATUS_LABELS[service.status] ?? service.status}</small>
              </div>
            ))}
            {preview.domains.map((domain) => (
              <div key={domain.domain_id}>
                <span>域名</span>
                <strong>{domain.domain_name}</strong>
                <small>{ASSET_DOMAIN_STATUS_LABELS[domain.status] ?? domain.status}</small>
              </div>
            ))}
          </div>
        </details>
      ) : null}
    </div>
  )
}
