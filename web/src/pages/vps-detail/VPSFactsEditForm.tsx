import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'

import type { ProviderRecord, VPSUsageStatus } from '../../lib/types'
import { CountryCombo } from './CountryCombo'
import type { FactEditFormState } from './types'
import { USAGE_OPTIONS } from './vpsDetailOptions'
import './VPSFactsEditForm.css'

const IMPORTANCE_OPTIONS: Array<[string, string]> = [
  ['low', '低'],
  ['normal', '普通'],
  ['high', '高'],
]

const OPTIONAL_COUNT = 10

type VPSFactsEditFormProps = {
  formId: string
  draft: FactEditFormState
  providers: ProviderRecord[]
  providersLoading: boolean
  providersError: string | null
  submitting: boolean
  onDraftChange: (draft: FactEditFormState) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

function hasCustomSSH(draft: FactEditFormState) {
  const host = draft.sshHost.trim()
  const port = draft.sshPort.trim()
  const ipv4 = draft.ipv4.trim()
  return (Boolean(host) && host !== ipv4) || (Boolean(port) && port !== '22')
}

function scrollIntoPanelBody(el: HTMLElement | null) {
  if (!el || el.hidden) return
  const body = el.closest('.modal-body')
  if (!(body instanceof HTMLElement)) return
  const bodyRect = body.getBoundingClientRect()
  const rect = el.getBoundingClientRect()
  if (rect.height >= bodyRect.height - 16) {
    body.scrollTop += rect.top - bodyRect.top - 8
    return
  }
  if (rect.top < bodyRect.top + 4) {
    body.scrollTop += rect.top - bodyRect.top - 8
  } else if (rect.bottom > bodyRect.bottom - 4) {
    body.scrollTop += rect.bottom - bodyRect.bottom + 12
  }
}

export function VPSFactsEditForm({
  formId,
  draft,
  providers,
  providersLoading,
  providersError,
  submitting,
  onDraftChange,
  onSubmit,
}: VPSFactsEditFormProps) {
  const providerSelectId = useId()
  const countryId = useId()
  const usageId = useId()
  const importanceId = useId()
  const noteId = useId()
  const ipv6EnabledId = useId()
  const sshDiffersId = useId()
  const ipv6FieldRef = useRef<HTMLLabelElement>(null)
  const sshFieldsRef = useRef<HTMLDivElement>(null)
  const [ipv6Enabled, setIPv6Enabled] = useState(() => Boolean(draft.ipv6.trim()))
  const [sshHostDiffers, setSSHHostDiffers] = useState(() => hasCustomSSH(draft))
  const [scrollTarget, setScrollTarget] = useState<'ipv6' | 'ssh' | null>(null)

  const knownImportance = IMPORTANCE_OPTIONS.some(([value]) => value === draft.importance)
  const importanceValue = knownImportance ? draft.importance : (draft.importance || 'normal')

  useEffect(() => {
    if (!scrollTarget) return
    const el = scrollTarget === 'ipv6' ? ipv6FieldRef.current : sshFieldsRef.current
    const frame = window.requestAnimationFrame(() => {
      scrollIntoPanelBody(el)
      setScrollTarget(null)
    })
    return () => window.cancelAnimationFrame(frame)
  }, [scrollTarget])

  function handleProviderChange(providerID: string) {
    const provider = providers.find((item) => item.provider_id === providerID)
    onDraftChange({
      ...draft,
      providerID,
      providerName: provider ? provider.name : draft.providerName,
    })
  }

  function updateIPv4(value: string) {
    onDraftChange({
      ...draft,
      ipv4: value,
      sshHost: sshHostDiffers || hasCustomSSH(draft) ? draft.sshHost : value,
    })
  }

  function updateIPv6Enabled(checked: boolean) {
    setIPv6Enabled(checked)
    if (checked) setScrollTarget('ipv6')
  }

  function updateSSHHostDiffers(checked: boolean) {
    setSSHHostDiffers(checked)
    if (!checked) return
    const host = draft.sshHost.trim()
    const port = String(draft.sshPort).trim()
    if (!host || !port) {
      onDraftChange({
        ...draft,
        sshHost: host ? draft.sshHost : draft.ipv4.trim(),
        sshPort: port ? draft.sshPort : '22',
      })
    }
    setScrollTarget('ssh')
  }

  const providerHint = providersLoading
    ? '正在读取服务商…'
    : providersError
      ? `服务商不可用：${providersError}`
      : providers.length === 0
        ? '还没有服务商主数据，请先创建或保留名称快照。'
        : '选择服务商会同步更新名称快照，仍可手动修正快照。'

  return (
    <form id={formId} className="vps-facts-form" autoComplete="off" onSubmit={onSubmit}>
      <div className="stack">
        <label className="field">
          <span className="field__label">VPS 名称</span>
          <input
            className="input input--name"
            value={draft.displayName}
            disabled={submitting}
            onChange={(event) => onDraftChange({ ...draft, displayName: event.target.value })}
          />
        </label>

        <label className="field">
          <span className="field__label-row">
            <span className="field__label">资产服务商</span>
            <span className="id-sink" hidden={!draft.providerID}>{draft.providerID}</span>
          </span>
          <select
            id={providerSelectId}
            className="input"
            aria-label="资产服务商"
            value={draft.providerID}
            disabled={providersLoading || submitting}
            onChange={(event) => handleProviderChange(event.target.value)}
          >
            <option value="">未关联服务商</option>
            {providers.map((provider) => (
              <option key={provider.provider_id} value={provider.provider_id}>
                {provider.name} · {provider.country || '地区未填'} · {provider.provider_id}
              </option>
            ))}
          </select>
          <span className="field__hint">
            {providerHint}
            {' '}
            <Link className="text-link" to="/providers">服务商列表</Link>
          </span>
        </label>

        <div className="access">
          <CountryCombo
            id={countryId}
            value={draft.country}
            disabled={submitting}
            onChange={(country) => onDraftChange({ ...draft, country })}
          />
          <div className="access__pair">
            <label className="field">
              <span className="field__label">城市</span>
              <input
                className="input"
                value={draft.city}
                disabled={submitting}
                onChange={(event) => onDraftChange({ ...draft, city: event.target.value })}
              />
            </label>
            <label className="field field--ipv4">
              <span className="field__label">IPv4</span>
              <input
                className="input mono-input"
                value={draft.ipv4}
                disabled={submitting}
                spellCheck={false}
                onChange={(event) => updateIPv4(event.target.value)}
              />
            </label>
          </div>
        </div>

        <div className="row-2">
          <div className="field">
            <label className="field__label" htmlFor={usageId}>使用状态</label>
            <select
              id={usageId}
              className="input"
              aria-label="使用状态"
              value={draft.usageStatus}
              disabled={submitting}
              onChange={(event) => onDraftChange({
                ...draft,
                usageStatus: event.target.value as VPSUsageStatus,
              })}
            >
              {USAGE_OPTIONS.map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </select>
          </div>
          <div className="field">
            <label className="field__label" htmlFor={importanceId}>重要性</label>
            <select
              id={importanceId}
              className="input"
              aria-label="重要性"
              value={importanceValue}
              disabled={submitting}
              onChange={(event) => onDraftChange({ ...draft, importance: event.target.value })}
            >
              {IMPORTANCE_OPTIONS.map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
              {draft.importance && !knownImportance ? (
                <option value={draft.importance}>{draft.importance}</option>
              ) : null}
            </select>
          </div>
        </div>

        <label className="field">
          <span className="field__label">标签</span>
          <input
            className="input"
            value={draft.labels}
            disabled={submitting}
            placeholder="prod, edge"
            onChange={(event) => onDraftChange({ ...draft, labels: event.target.value })}
          />
          <span className="field__hint">用逗号分隔</span>
        </label>

        <label className="field">
          <span className="field__label">备注</span>
          <textarea
            id={noteId}
            className="input"
            rows={3}
            value={draft.note}
            disabled={submitting}
            onChange={(event) => onDraftChange({ ...draft, note: event.target.value })}
          />
        </label>
      </div>

      <details className="opt">
        <summary>
          <span className="opt__name">可选设置</span>
          <span className="opt__count">{OPTIONAL_COUNT} 项</span>
        </summary>
        <div className="opt__body">
          <div className="switches">
            <label className="tg" htmlFor={ipv6EnabledId}>
              <input
                id={ipv6EnabledId}
                type="checkbox"
                checked={ipv6Enabled}
                disabled={submitting}
                onChange={(event) => updateIPv6Enabled(event.target.checked)}
              />
              <span className="tg-track" />
              <span>启用 IPv6</span>
            </label>
          </div>
          <label className="field" ref={ipv6FieldRef} hidden={!ipv6Enabled}>
            <span className="field__label">IPv6 地址</span>
            <input
              className="input mono-input"
              value={draft.ipv6}
              disabled={submitting}
              spellCheck={false}
              onChange={(event) => onDraftChange({ ...draft, ipv6: event.target.value })}
            />
          </label>
          <div className="switches">
            <label className="tg" htmlFor={sshDiffersId}>
              <input
                id={sshDiffersId}
                type="checkbox"
                checked={sshHostDiffers}
                disabled={submitting}
                onChange={(event) => updateSSHHostDiffers(event.target.checked)}
              />
              <span className="tg-track" />
              <span>单独填写 SSH</span>
            </label>
          </div>
          <div className="access__pair" ref={sshFieldsRef} hidden={!sshHostDiffers}>
            <label className="field">
              <span className="field__label">SSH Host</span>
              <input
                className="input mono-input"
                value={draft.sshHost}
                disabled={submitting}
                spellCheck={false}
                onChange={(event) => onDraftChange({ ...draft, sshHost: event.target.value })}
              />
            </label>
            <label className="field field--port">
              <span className="field__label">SSH 端口</span>
              <input
                className="input mono-input"
                type="number"
                min={1}
                max={65535}
                value={draft.sshPort}
                disabled={submitting}
                onChange={(event) => onDraftChange({ ...draft, sshPort: event.target.value })}
              />
            </label>
          </div>
          <div className="row-2">
            <label className="field">
              <span className="field__label">产品名</span>
              <input
                className="input"
                value={draft.productName}
                disabled={submitting}
                onChange={(event) => onDraftChange({ ...draft, productName: event.target.value })}
              />
            </label>
            <label className="field">
              <span className="field__label">订单号</span>
              <input
                className="input mono-input"
                value={draft.orderRef}
                disabled={submitting}
                spellCheck={false}
                onChange={(event) => onDraftChange({ ...draft, orderRef: event.target.value })}
              />
            </label>
          </div>
          <label className="field">
            <span className="field__label">服务商名称快照</span>
            <input
              className="input"
              value={draft.providerName}
              disabled={submitting}
              onChange={(event) => onDraftChange({ ...draft, providerName: event.target.value })}
            />
          </label>
          <div className="row-2">
            <label className="field">
              <span className="field__label">区域</span>
              <input
                className="input"
                value={draft.region}
                disabled={submitting}
                onChange={(event) => onDraftChange({ ...draft, region: event.target.value })}
              />
            </label>
            <label className="field">
              <span className="field__label">数据中心</span>
              <input
                className="input"
                value={draft.datacenter}
                disabled={submitting}
                onChange={(event) => onDraftChange({ ...draft, datacenter: event.target.value })}
              />
            </label>
          </div>
          <div className="row-2">
            <label className="field">
              <span className="field__label">SSH 用户</span>
              <input
                className="input mono-input"
                value={draft.sshUser}
                disabled={submitting}
                spellCheck={false}
                onChange={(event) => onDraftChange({ ...draft, sshUser: event.target.value })}
              />
            </label>
            <label className="field">
              <span className="field__label">操作系统</span>
              <input
                className="input"
                value={draft.osName}
                disabled={submitting}
                onChange={(event) => onDraftChange({ ...draft, osName: event.target.value })}
              />
            </label>
          </div>
          <label className="field">
            <span className="field__label">虚拟化</span>
            <input
              className="input"
              value={draft.virtualization}
              disabled={submitting}
              onChange={(event) => onDraftChange({ ...draft, virtualization: event.target.value })}
            />
          </label>
        </div>
      </details>
    </form>
  )
}
