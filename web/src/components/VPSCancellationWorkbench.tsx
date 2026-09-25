import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import {
  objectAffectsAnotherVPS,
  sharedObjectKey,
} from '../lib/assetLifecycle'
import { renewalModeFromLegacy, renewalModeLabel } from '../lib/assetOptions'
import { formatDate, formatOptional } from '../lib/format'
import {
  ASSET_DOMAIN_STATUS_LABELS,
  ASSET_SERVICE_STATUS_LABELS,
  type ApplyCancellationInput,
  type CancellationPreview,
  type DependencyImpact,
  type LifecycleActionResult,
  type TargetRunStatus,
} from '../lib/types'
import { Badge, Button, Input, MonoDigits, Select } from './atoms'
import { LifecycleBadge, RenewalBadge, SubscriptionStatusBadge } from '../pages/assetPageBadges'
import { SharedImpactPanel } from './SharedImpactPanel'


const EMPTY_IMPACTS: DependencyImpact[] = []
type SubscriptionChoice = 'unset' | 'cancel' | 'retain'

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
  if (preview.vps.lifecycle_status === 'cancelled') return 'cancelled'
  const recommended = preview.recommended_steps.find((step) => step.object_type === 'vps')?.to_state
  if (recommended?.includes('cancelled')) return 'cancelled'
  if (recommended?.includes('to_cancel')) return 'to_cancel'
  return 'to_cancel'
}

function initialMonitoringChoices(preview: CancellationPreview): Record<string, WorkbenchMonitoringInstanceChoice> {
  const map: Record<string, WorkbenchMonitoringInstanceChoice> = {}
  for (const monitoringInstance of preview.monitoring_instance_links) {
    const retired = monitoringInstance.lifecycle_status === '已退役'
    map[monitoringInstance.monitoring_instance_id] = {
      enabled: false,
      lifecycleStatus: retired ? '已退役' : '不续费',
      pauseMonitoring: false,
    }
  }
  return map
}

function initialTargetChoices(preview: CancellationPreview): Record<string, WorkbenchTargetChoice> {
  const map: Record<string, WorkbenchTargetChoice> = {}
  for (const target of preview.target_links) {
    map[target.target_id] = {
      enabled: false,
      runStatus: '已归档',
    }
  }
  return map
}

function initialSubscriptionChoices(preview: CancellationPreview): Record<string, SubscriptionChoice> {
  const map: Record<string, SubscriptionChoice> = {}
  for (const impact of preview.subscriptions) {
    if (impact.record.status === 'active') {
      map[impact.record.subscription_id] = 'unset'
    } else if (impact.record.auto_renew) {
      map[impact.record.subscription_id] = 'retain'
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
  const lifecycleLocked = preview.vps.lifecycle_status === 'cancelled'
  const [reason, setReason] = useState('')
  const [effectiveDate, setEffectiveDate] = useState(() => new Date().toISOString().slice(0, 10))
  const [vpsLifecycleStatus, setVpsLifecycleStatus] = useState<'to_cancel' | 'cancelled'>(() => defaultVPSLifecycle(preview))
  const [subscriptionChoices, setSubscriptionChoices] = useState<Record<string, SubscriptionChoice>>(() => initialSubscriptionChoices(preview))
  const [monitoringInstanceChoices, setMonitoringInstanceChoices] = useState(() => initialMonitoringChoices(preview))
  const [targetChoices, setTargetChoices] = useState(() => initialTargetChoices(preview))
  const [confirmedShared, setConfirmedShared] = useState<Record<string, boolean>>({})
  const [scopeNotice, setScopeNotice] = useState<string | null>(null)
  const [validationError, setValidationError] = useState<string | null>(null)
  const seenDigest = useRef<string | null>(null)

  useEffect(() => {
    if (seenDigest.current === null) {
      seenDigest.current = preview.preview_digest
      return
    }
    if (seenDigest.current === preview.preview_digest) return
    seenDigest.current = preview.preview_digest
    const removed: string[] = []
    const added: string[] = []
    const nextSubscriptions = initialSubscriptionChoices(preview)
    for (const [id, choice] of Object.entries(subscriptionChoices)) {
      if (!(id in nextSubscriptions)) removed.push(id)
      else nextSubscriptions[id] = choice
    }
    for (const id of Object.keys(nextSubscriptions)) {
      if (!(id in subscriptionChoices)) added.push(id)
    }
    const nextMonitoring = initialMonitoringChoices(preview)
    for (const [id, choice] of Object.entries(monitoringInstanceChoices)) {
      const link = preview.monitoring_instance_links.find((item) => item.monitoring_instance_id === id)
      if (!link || link.archived_at) {
        if (choice.enabled) removed.push(link?.display_name || id)
        continue
      }
      nextMonitoring[id] = choice
    }
    for (const link of preview.monitoring_instance_links) {
      if (!monitoringInstanceChoices[link.monitoring_instance_id]) added.push(link.display_name || link.monitoring_instance_id)
    }
    const nextTargets = initialTargetChoices(preview)
    for (const [id, choice] of Object.entries(targetChoices)) {
      const target = preview.target_links.find((item) => item.target_id === id)
      if (!target || target.run_status === '已归档') {
        if (choice.enabled) removed.push(target?.name || id)
        continue
      }
      nextTargets[id] = choice
    }
    for (const target of preview.target_links) {
      if (!targetChoices[target.target_id]) added.push(target.name || target.target_id)
    }
    setSubscriptionChoices(nextSubscriptions)
    setMonitoringInstanceChoices(nextMonitoring)
    setTargetChoices(nextTargets)
    setConfirmedShared({})
    const parts = []
    if (removed.length > 0) parts.push(`已从提交范围移除不可操作或已消失的对象：${removed.join('、')}`)
    if (added.length > 0) parts.push(`预览新增了对象，未自动选中：${added.join('、')}`)
    parts.push('影响范围已变化，共享确认已清除，不会自动重新提交。')
    setScopeNotice(parts.join(' '))
  }, [lifecycleLocked, monitoringInstanceChoices, preview, subscriptionChoices, targetChoices])

  const activeSubscriptions = preview.subscriptions.filter((impact) => impact.record.status === 'active')
  const historicalSubscriptions = preview.subscriptions.filter((impact) => impact.record.status !== 'active')
  const impacts = preview.dependency_impacts ?? EMPTY_IMPACTS
  const sharedObjects = useMemo(() => {
    const objects: Array<{ key: string; label: string; objectType: string; objectId: string }> = []
    for (const link of preview.monitoring_instance_links) {
      const choice = monitoringInstanceChoices[link.monitoring_instance_id]
      if (!choice?.enabled || link.archived_at) continue
      if (!objectAffectsAnotherVPS(impacts, 'monitoring_instance', link.monitoring_instance_id, preview.vps.vps_id)) continue
      objects.push({
        key: sharedObjectKey('monitoring_instance', link.monitoring_instance_id),
        label: link.display_name || link.monitoring_instance_id,
        objectType: 'monitoring_instance',
        objectId: link.monitoring_instance_id,
      })
    }
    for (const target of preview.target_links) {
      const choice = targetChoices[target.target_id]
      if (!choice?.enabled || target.run_status === '已归档') continue
      if (!objectAffectsAnotherVPS(impacts, 'target', target.target_id, preview.vps.vps_id)) continue
      objects.push({
        key: sharedObjectKey('target', target.target_id),
        label: target.name || target.target_id,
        objectType: 'target',
        objectId: target.target_id,
      })
    }
    return objects
  }, [impacts, monitoringInstanceChoices, preview.monitoring_instance_links, preview.target_links, preview.vps.vps_id, targetChoices])

  function buildInput(): ApplyCancellationInput {
    const cleanReason = reason.trim()
    if (!cleanReason) throw new Error('需要填写取消/退役原因。')
    const missingSubscription = activeSubscriptions.find((impact) => subscriptionChoices[impact.record.subscription_id] !== 'cancel' && subscriptionChoices[impact.record.subscription_id] !== 'retain')
    if (missingSubscription) throw new Error('每条生效中订阅都要明确选择本次取消或保留。')
    const unconfirmed = sharedObjects.find((object) => !confirmedShared[object.key])
    if (unconfirmed) throw new Error(`请确认 ${unconfirmed.label} 对其他 VPS 的影响后再提交。`)

    const subscriptionIDs = preview.subscriptions
      .filter((impact) => impact.record.status === 'active' || impact.record.auto_renew)
      .filter((impact) => subscriptionChoices[impact.record.subscription_id] === 'cancel')
      .map((impact) => impact.record.subscription_id)
    const monitoringInstanceActions: ApplyCancellationInput['monitoring_instance_actions'] = []
    for (const monitoringInstance of preview.monitoring_instance_links) {
      if (monitoringInstance.archived_at) continue
      const choice = monitoringInstanceChoices[monitoringInstance.monitoring_instance_id]
      if (!choice?.enabled) continue
      const retired = monitoringInstance.lifecycle_status === '已退役'
      const lifecycleStatus = retired ? '已退役' : choice.lifecycleStatus
      monitoringInstanceActions.push({
        monitoring_instance_id: monitoringInstance.monitoring_instance_id,
        ...(lifecycleStatus ? { lifecycle_status: lifecycleStatus } : {}),
        ...((retired ? choice.pauseMonitoring : lifecycleStatus !== '已退役' && choice.pauseMonitoring) ? { monitoring_status: '暂停' } : {}),
      })
    }
    const targetActions: ApplyCancellationInput['target_actions'] = []
    for (const target of preview.target_links) {
      if (target.run_status === '已归档') continue
      const choice = targetChoices[target.target_id]
      if (!choice?.enabled) continue
      targetActions.push({ target_id: target.target_id, run_status: choice.runStatus })
    }
    return {
      reason: cleanReason,
      effective_date: effectiveDate || null,
      subscription_ids: subscriptionIDs,
      vps_lifecycle_status: lifecycleLocked ? 'cancelled' : vpsLifecycleStatus,
      monitoring_instance_actions: monitoringInstanceActions,
      target_actions: targetActions,
      preview_digest: preview.preview_digest,
      confirmed_shared_objects: sharedObjects
        .filter((object) => confirmedShared[object.key])
        .map((object) => ({ object_type: object.objectType, object_id: object.objectId })),
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

  const selectedMonitoringInstanceCount = Object.values(monitoringInstanceChoices).filter((choice) => choice.enabled).length
  const selectedTargetCount = Object.values(targetChoices).filter((choice) => choice.enabled).length
  const cancelledSubscriptionCount = Object.values(subscriptionChoices).filter((choice) => choice === 'cancel').length
  const vpsLifecycleLabel = (lifecycleLocked ? 'cancelled' : vpsLifecycleStatus) === 'cancelled' ? '已取消' : '待取消'

  return (
    <div className="asset-cancel-workbench">
      <section className="asset-cancel-workbench__summary" aria-label="取消/退役影响范围摘要">
        <div className="asset-cancel-workbench__summary-item asset-cancel-workbench__summary-item--identity">
          <span className="summary-card__label">VPS</span>
          <strong className="summary-card__value--text">{preview.vps.display_name}</strong>
          <small>{preview.vps.vps_id}</small>
        </div>
        <div className="asset-cancel-workbench__summary-item">
          <span className="summary-card__label">评估日期</span>
          <strong className="summary-card__value--text">{preview.evaluated_on || '未提供'}</strong>
          <small>只记录建议，不自动推进终态</small>
        </div>
        <div className="asset-cancel-workbench__summary-item">
          <span className="summary-card__label">订阅</span>
          <strong className="summary-card__value"><MonoDigits>{cancelledSubscriptionCount}</MonoDigits></strong>
          <small>生效中 {activeSubscriptions.length} 条需逐条确认</small>
        </div>
        <div className="asset-cancel-workbench__summary-item">
          <span className="summary-card__label">监控 / 入口</span>
          <strong className="summary-card__value"><MonoDigits>{selectedMonitoringInstanceCount + selectedTargetCount}</MonoDigits></strong>
          <small>未勾选的对象不会进入请求</small>
        </div>
      </section>

      {scopeNotice ? <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">{scopeNotice}</p> : null}
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
                <h3>{lifecycleLocked ? '处理已取消残留' : '取消/退役目标'}</h3>
              </div>
              <Badge variant="state" tone={vpsLifecycleLabel === '已取消' ? 'critical' : 'notice'}>{vpsLifecycleLabel}</Badge>
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
            {lifecycleLocked ? (
              <p>来源已是已取消。本次只能同态处理残留，不能回到待取消。</p>
            ) : (
              <Select
                label="VPS 生命周期"
                value={vpsLifecycleStatus}
                onChange={(event) => setVpsLifecycleStatus(event.target.value as 'to_cancel' | 'cancelled')}
                options={[
                  { value: 'cancelled', label: '已取消' },
                  { value: 'to_cancel', label: '待取消' },
                ]}
              />
            )}
            <Input label="生效日期" type="date" value={effectiveDate} onChange={(event) => setEffectiveDate(event.target.value)} />
          </section>
          <section className="asset-cancel-workbench__section asset-cancel-workbench__section--audit">
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
                取消意向已记录，动作 {result.action.action_id} 写入 {result.steps.length} 个步骤。这不表示下线已完成，也不表示已经可以归档。
              </p>
            ) : null}
            <div className="asset-cancel-workbench__actions">
              {onCancel ? <Button variant="secondary" onClick={onCancel} disabled={submitting}>关闭</Button> : null}
              <Button variant="danger" onClick={() => void submit()} disabled={submitting || preview.blockers.length > 0}>
                {submitting ? '执行中…' : lifecycleLocked ? '确认处理残留' : '确认取消/退役'}
              </Button>
            </div>
          </section>
        </div>

        <div className="asset-cancel-workbench__rail asset-cancel-workbench__rail--confirm">
          <section className="asset-cancel-workbench__section">
            <div className="asset-cancel-workbench__section-head">
              <div>
                <p className="asset-cancel-workbench__eyebrow">订阅</p>
                <h3>逐条确认账单处理</h3>
              </div>
            </div>
            {activeSubscriptions.length === 0 ? <p className="asset-cancel-workbench__empty">没有生效中订阅。</p> : (
              <div className="asset-cancel-workbench__list">
                {activeSubscriptions.map((impact) => {
                  const subscription = impact.record
                  const choice = subscriptionChoices[subscription.subscription_id] ?? 'unset'
                  return (
                    <fieldset key={subscription.subscription_id} className="asset-cancel-workbench__row">
                      <legend>{subscription.subscription_id}</legend>
                      <SubscriptionStatusBadge value={subscription.status} />
                      <small>{formatDate(subscription.renew_at)} · {renewalModeLabel(subscription.renewal_mode ?? renewalModeFromLegacy(subscription))}</small>
                      <p>{impact.message}</p>
                      <label>
                        <input
                          type="radio"
                          name={`subscription-${subscription.subscription_id}`}
                          checked={choice === 'cancel'}
                          disabled={submitting}
                          onChange={() => setSubscriptionChoices((current) => ({ ...current, [subscription.subscription_id]: 'cancel' }))}
                        />
                        本次取消
                      </label>
                      <label>
                        <input
                          type="radio"
                          name={`subscription-${subscription.subscription_id}`}
                          checked={choice === 'retain'}
                          disabled={submitting}
                          onChange={() => setSubscriptionChoices((current) => ({ ...current, [subscription.subscription_id]: 'retain' }))}
                        />
                        保留
                      </label>
                      {choice === 'retain' ? <p>保留后继续计费，并且仍会阻止归档。</p> : null}
                    </fieldset>
                  )
                })}
              </div>
            )}
            {historicalSubscriptions.length > 0 ? (
              <div className="asset-cancel-workbench__list">
                <p className="asset-cancel-workbench__eyebrow">历史 / 待确认账单</p>
                {historicalSubscriptions.map((impact) => {
                  const subscription = impact.record
                  const choice = subscriptionChoices[subscription.subscription_id] ?? 'retain'
                  const canCancel = Boolean(subscription.auto_renew)
                  return (
                    <fieldset key={subscription.subscription_id} className="asset-cancel-workbench__row asset-cancel-workbench__row--historical">
                      <legend>{subscription.subscription_id}</legend>
                      <SubscriptionStatusBadge value={subscription.status} />
                      <small>{formatDate(subscription.renew_at)} · {renewalModeLabel(subscription.renewal_mode ?? renewalModeFromLegacy(subscription))}</small>
                      <p>{impact.message}</p>
                      {canCancel ? (
                        <>
                          <label>
                            <input
                              type="radio"
                              name={`subscription-${subscription.subscription_id}`}
                              checked={choice === 'cancel'}
                              disabled={submitting}
                              onChange={() => setSubscriptionChoices((current) => ({ ...current, [subscription.subscription_id]: 'cancel' }))}
                            />
                            本次取消
                          </label>
                          <label>
                            <input
                              type="radio"
                              name={`subscription-${subscription.subscription_id}`}
                              checked={choice === 'retain'}
                              disabled={submitting}
                              onChange={() => setSubscriptionChoices((current) => ({ ...current, [subscription.subscription_id]: 'retain' }))}
                            />
                            保留
                          </label>
                          {choice === 'cancel' ? <p>将显式取消该历史记录上的自动续费。</p> : <p>仍开启自动续费，未勾选取消前保持现状。</p>}
                        </>
                      ) : (
                        <p>历史账单只展示，不会进入本次请求。</p>
                      )}
                    </fieldset>
                  )
                })}
              </div>
            ) : null}
          </section>

          <section className="asset-cancel-workbench__section">
            <div className="asset-cancel-workbench__section-head">
              <div>
                <p className="asset-cancel-workbench__eyebrow">监控实例</p>
                <h3>监控实例确认</h3>
              </div>
            </div>
            {preview.monitoring_instance_links.length === 0 ? <p className="asset-cancel-workbench__empty">没有监控实例关联。</p> : (
              <div className="asset-cancel-workbench__list">
                {preview.monitoring_instance_links.map((monitoringInstance) => {
                  const retired = monitoringInstance.lifecycle_status === '已退役'
                  const choice = monitoringInstanceChoices[monitoringInstance.monitoring_instance_id]
                  const archived = Boolean(monitoringInstance.archived_at)
                  return (
                    <div key={monitoringInstance.monitoring_instance_id} className="asset-cancel-workbench__row">
                      <label className="asset-checkbox-line">
                        <input
                          type="checkbox"
                          checked={Boolean(choice?.enabled) && !archived}
                          disabled={submitting || archived}
                          onChange={(event) => setMonitoringInstanceChoices((current) => ({
                            ...current,
                            [monitoringInstance.monitoring_instance_id]: {
                              ...(current[monitoringInstance.monitoring_instance_id] ?? { enabled: false, lifecycleStatus: '不续费', pauseMonitoring: false }),
                              enabled: event.target.checked,
                            },
                          }))}
                        />
                        <span>
                          <strong>{monitoringInstance.display_name}</strong>
                          <Badge variant="state">{monitoringInstance.lifecycle_status}</Badge>
                          <small>{formatOptional(monitoringInstance.provider)} · 监控 {monitoringInstance.monitoring_status}</small>
                        </span>
                      </label>
                      {archived ? (
                        <p>
                          已归档，工作台不能改写。
                          <Link to={`/monitoring/${encodeURIComponent(monitoringInstance.monitoring_instance_id)}`}>打开监控实例，先恢复再接入</Link>
                        </p>
                      ) : (
                        <div className="asset-cancel-workbench__controls">
                          <Select
                            label="生命周期"
                            value={retired ? '已退役' : (choice?.lifecycleStatus || '不续费')}
                            disabled={!choice?.enabled || submitting || retired}
                            onChange={(event) => {
                              if (retired) return
                              setMonitoringInstanceChoices((current) => ({
                              ...current,
                              [monitoringInstance.monitoring_instance_id]: {
                                ...(current[monitoringInstance.monitoring_instance_id] ?? { enabled: false, lifecycleStatus: '不续费', pauseMonitoring: false }),
                                lifecycleStatus: event.target.value as WorkbenchMonitoringInstanceChoice['lifecycleStatus'],
                              },
                            }))}}
                            options={retired
                              ? [{ value: '已退役', label: '已退役' }]
                              : [
                              { value: '不续费', label: '不续费' },
                              { value: '已退役', label: '已退役' },
                            ]}
                          />
                          {retired ? (
                            <>
                              <p>已退役不能在工作台改为不续费，那会绕过专用恢复。这里只能保持退役并整理残留；要重新接入，请先在监控实例详情恢复到观察中。</p>
                              <label className="asset-cancel-workbench__inline-check">
                                <input
                                  type="checkbox"
                                  checked={Boolean(choice?.pauseMonitoring)}
                                  disabled={!choice?.enabled || submitting}
                                  onChange={(event) => setMonitoringInstanceChoices((current) => ({
                                    ...current,
                                    [monitoringInstance.monitoring_instance_id]: {
                                      ...(current[monitoringInstance.monitoring_instance_id] ?? { enabled: false, lifecycleStatus: '已退役', pauseMonitoring: false }),
                                      lifecycleStatus: '已退役',
                                      pauseMonitoring: event.target.checked,
                                    },
                                  }))}
                                />
                                <span>暂停监控</span>
                              </label>
                            </>
                          ) : choice?.lifecycleStatus === '已退役' ? (
                            <p>选择退役后，监控会必然暂停，接入和同步凭据会被撤销。这不会自动勾选本行。</p>
                          ) : (
                            <label className="asset-cancel-workbench__inline-check">
                              <input
                                type="checkbox"
                                checked={Boolean(choice?.pauseMonitoring)}
                                disabled={!choice?.enabled || submitting}
                                onChange={(event) => setMonitoringInstanceChoices((current) => ({
                                  ...current,
                                  [monitoringInstance.monitoring_instance_id]: {
                                    ...(current[monitoringInstance.monitoring_instance_id] ?? { enabled: false, lifecycleStatus: '不续费', pauseMonitoring: false }),
                                    pauseMonitoring: event.target.checked,
                                  },
                                }))}
                              />
                              <span>暂停监控</span>
                            </label>
                          )}
                        </div>
                      )}
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
                <h3>Target 确认</h3>
              </div>
            </div>
            {preview.target_links.length === 0 ? <p className="asset-cancel-workbench__empty">没有关联的 Target。</p> : (
              <div className="asset-cancel-workbench__list">
                {preview.target_links.map((target) => {
                  const choice = targetChoices[target.target_id]
                  const archived = target.run_status === '已归档'
                  return (
                    <div key={target.target_id} className="asset-cancel-workbench__row">
                      <label className="asset-checkbox-line">
                        <input
                          type="checkbox"
                          checked={Boolean(choice?.enabled) && !archived}
                          disabled={submitting || archived}
                          onChange={(event) => setTargetChoices((current) => ({
                            ...current,
                            [target.target_id]: {
                              ...(current[target.target_id] ?? { enabled: false, runStatus: '已归档' }),
                              enabled: event.target.checked,
                            },
                          }))}
                        />
                        <span>
                          <strong>{target.name || target.target_id}</strong>
                          <Badge variant="state">{target.run_status}</Badge>
                        </span>
                      </label>
                      {archived ? (
                        <p>
                          已归档的入口探测不能在工作台用暂停恢复。
                          <Link to={`/targets/${encodeURIComponent(target.target_id)}`}>到入口探测详情执行恢复为暂停</Link>
                        </p>
                      ) : (
                        <Select
                          label="运行状态"
                          value={choice?.runStatus ?? '已归档'}
                          disabled={!choice?.enabled || submitting}
                          onChange={(event) => setTargetChoices((current) => ({
                            ...current,
                            [target.target_id]: {
                              ...(current[target.target_id] ?? { enabled: false, runStatus: '已归档' }),
                              runStatus: event.target.value as TargetRunStatus,
                            },
                          }))}
                          options={[
                            { value: '已归档', label: '已归档' },
                            { value: '暂停', label: '暂停' },
                          ]}
                        />
                      )}
                    </div>
                  )
                })}
              </div>
            )}
          </section>

          <SharedImpactPanel
            impacts={impacts}
            currentVPSID={preview.vps.vps_id}
            confirmations={sharedObjects.map((object) => ({
              key: object.key,
              label: object.label,
              checked: Boolean(confirmedShared[object.key]),
              disabled: submitting,
            }))}
            onToggle={(key, checked) => setConfirmedShared((current) => ({ ...current, [key]: checked }))}
          />
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
