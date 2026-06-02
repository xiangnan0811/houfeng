import { type FormEvent, useState } from 'react'

import { Button, Input, Modal, Select } from './atoms'
import { CollapsibleSection } from './CollapsibleSection'
import { createProvider, createVPSAsset } from '../lib/api'
import {
  CUSTOM_OPTION_VALUE,
  countryOptionsWithExisting,
  displayOption,
  normalizeCountry,
} from '../lib/assetOptions'
import {
  type CreateVPSAssetInput,
  type ProviderRecord,
  type VPSAssetRecord,
} from '../lib/types'

function describeError(error: unknown, fallback: string): string {
  if (error instanceof Error) return error.message
  if (typeof error === 'string') return error
  return fallback
}

interface VPSCreateModalProps {
  open: boolean
  onClose: () => void
  providers: ProviderRecord[]
  existingCountries?: string[]
  onCreated: (vps: VPSAssetRecord) => void
  onProviderCreated: (provider: ProviderRecord) => void
}

type FormState = {
  displayName: string
  providerID: string
  productName: string
  orderRef: string
  country: string
  customCountry: string
  region: string
  city: string
  datacenter: string
  ipv4: string
  ipv6: string
  ipv6Enabled: boolean
  sshHostDiffers: boolean
  sshHost: string
  sshPort: string
  sshUser: string
  osName: string
  virtualization: string
  importance: string
  labels: string
  note: string
}

const INITIAL_FORM: FormState = {
  displayName: '',
  providerID: '',
  productName: '',
  orderRef: '',
  country: '',
  customCountry: '',
  region: '',
  city: '',
  datacenter: '',
  ipv4: '',
  ipv6: '',
  ipv6Enabled: false,
  sshHostDiffers: false,
  sshHost: '',
  sshPort: '22',
  sshUser: 'root',
  osName: '',
  virtualization: '',
  importance: 'normal',
  labels: '',
  note: '',
}

function parseLabels(raw: string): string[] {
  return [...new Set(raw.split(',').map((s) => s.trim()).filter(Boolean))]
}

function selectedCountry(form: FormState): string {
  return form.country === CUSTOM_OPTION_VALUE ? normalizeCountry(form.customCountry) : normalizeCountry(form.country)
}

function sshHostValue(form: FormState): string {
  return form.sshHostDiffers ? form.sshHost : form.ipv4
}

function selectedSSHHost(form: FormState): string {
  return sshHostValue(form).trim()
}

function buildCreateInput(form: FormState, providers: ProviderRecord[]): CreateVPSAssetInput {
  if (form.displayName.trim() === '') {
    throw new Error('VPS 名称不能为空。')
  }
  if (!form.ipv4.trim() && !selectedSSHHost(form)) {
    throw new Error('IPv4 或 SSH Host 至少需要填写一个。')
  }
  const selectedProvider = providers.find((p) => p.provider_id === form.providerID)
  const sshPort = form.sshPort.trim() === '' ? undefined : Number.parseInt(form.sshPort.trim(), 10)
  if (sshPort != null && (!Number.isInteger(sshPort) || sshPort < 1 || sshPort > 65535)) {
    throw new Error('SSH 端口必须为 1 到 65535。')
  }
  return {
    display_name: form.displayName.trim(),
    provider_id: form.providerID || null,
    provider_name: selectedProvider?.name ?? '',
    product_name: form.productName.trim(),
    order_ref: form.orderRef.trim(),
    country: selectedCountry(form),
    region: form.region.trim(),
    city: form.city.trim(),
    datacenter: form.datacenter.trim(),
    ipv4: form.ipv4.trim(),
    ipv6: form.ipv6Enabled ? form.ipv6.trim() : '',
    ssh_host: selectedSSHHost(form),
    ...(sshPort == null ? {} : { ssh_port: sshPort }),
    ssh_user: form.sshUser.trim(),
    os_name: form.osName.trim(),
    virtualization: form.virtualization.trim(),
    lifecycle_status: 'active',
    usage_status: 'unknown',
    renewal_decision: 'unreviewed',
    importance: form.importance.trim() || 'normal',
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}

export function VPSCreateModal({ open, onClose, providers, existingCountries = [], onCreated, onProviderCreated }: VPSCreateModalProps) {
  const [form, setForm] = useState<FormState>(INITIAL_FORM)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [showProviderCreate, setShowProviderCreate] = useState(false)
  const [newProviderName, setNewProviderName] = useState('')
  const [newProviderWebsite, setNewProviderWebsite] = useState('')
  const [providerCreating, setProviderCreating] = useState(false)
  const [providerError, setProviderError] = useState<string | null>(null)
  const countryOptions = countryOptionsWithExisting(existingCountries)

  function reset() {
    setForm(INITIAL_FORM)
    setError(null)
    setShowProviderCreate(false)
    setNewProviderName('')
    setNewProviderWebsite('')
    setProviderError(null)
  }

  function handleClose() {
    reset()
    onClose()
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    let input: CreateVPSAssetInput
    try {
      input = buildCreateInput(form, providers)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : '输入无效')
      return
    }
    setSubmitting(true)
    createVPSAsset(input)
      .then((vps) => { reset(); onCreated(vps) })
      .catch((err: unknown) => setError(describeError(err, '创建 VPS 失败')))
      .finally(() => setSubmitting(false))
  }

  function handleProviderCreate() {
    if (newProviderName.trim() === '') {
      setProviderError('服务商名称不能为空')
      return
    }
    setProviderCreating(true)
    setProviderError(null)
    createProvider({
      name: newProviderName.trim(),
      website: newProviderWebsite.trim(),
      panel_url: '',
      account_hint: '',
      country: '',
      note: '',
      labels: [],
    })
      .then((p) => {
        onProviderCreated(p)
        setForm((f) => ({ ...f, providerID: p.provider_id }))
        setShowProviderCreate(false)
        setNewProviderName('')
        setNewProviderWebsite('')
      })
      .catch((err: unknown) => setProviderError(describeError(err, '创建服务商失败')))
      .finally(() => setProviderCreating(false))
  }

  function updateIPv4(value: string) {
    setForm((current) => ({
      ...current,
      ipv4: value,
      sshHost: current.sshHostDiffers ? current.sshHost : value,
    }))
  }

  function updateSSHHostDiffers(checked: boolean) {
    setForm((current) => ({
      ...current,
      sshHostDiffers: checked,
      sshHost: checked ? current.sshHost : current.ipv4,
    }))
  }

  function updateIPv6Enabled(checked: boolean) {
    setForm((current) => ({
      ...current,
      ipv6Enabled: checked,
      ipv6: checked ? current.ipv6 : '',
    }))
  }

  const footer = (
    <>
      <span className="modal-footer__hint">创建后进入详情页。</span>
      <Button type="button" variant="secondary" onClick={handleClose}>取消</Button>
      <Button type="submit" form="vps-create-form" disabled={submitting}>
        {submitting ? '创建中…' : '创建 VPS'}
      </Button>
    </>
  )

  return (
    <Modal open={open} onClose={handleClose} title="添加 VPS" persistent size="lg" footer={footer}>
      <form id="vps-create-form" className="vps-create-form" onSubmit={handleSubmit}>
        <div className="vps-create-form__section">
          <div className="vps-create-form__section-title">核心信息</div>
          <div className="input-field">
            <label className="input-field__label input-field__label--required" htmlFor="vps-name">VPS 名称</label>
            <div className="input-field__shell">
              <input id="vps-name" className="input" value={form.displayName} onChange={(e) => setForm((f) => ({ ...f, displayName: e.target.value }))} placeholder="例如：hk-prod-01" />
            </div>
          </div>

          {!showProviderCreate ? (
            <div className="vps-create-form__provider-row">
              <Select label="服务商" required value={form.providerID} onChange={(e) => setForm((f) => ({ ...f, providerID: e.target.value }))}>
                <option value="">未关联服务商</option>
                {providers.map((p) => <option key={p.provider_id} value={p.provider_id}>{p.name}</option>)}
              </Select>
              <button type="button" className="vps-create-form__provider-link" onClick={() => setShowProviderCreate(true)}>+ 新建服务商</button>
            </div>
          ) : (
            <div className="vps-create-form__inline-provider">
              <div className="vps-create-form__inline-provider-row">
                <Input label="服务商名称" value={newProviderName} onChange={(e) => setNewProviderName(e.target.value)} placeholder="必填" />
                <Input label="网站" value={newProviderWebsite} onChange={(e) => setNewProviderWebsite(e.target.value)} placeholder="选填" />
              </div>
              {providerError && <p className="create-form__error" role="alert">{providerError}</p>}
              <div className="vps-create-form__inline-provider-actions">
                <Button type="button" variant="secondary" onClick={() => { setShowProviderCreate(false); setProviderError(null) }}>取消</Button>
                <Button type="button" disabled={providerCreating} onClick={handleProviderCreate}>{providerCreating ? '创建中…' : '创建'}</Button>
              </div>
            </div>
          )}
        </div>

        <div className="vps-create-form__section">
          <div className="vps-create-form__section-title">网络入口</div>
          <div className="vps-create-form__row">
            <Input label="IPv4 / 主入口" value={form.ipv4} onChange={(e) => updateIPv4(e.target.value)} placeholder="例如：1.2.3.4" />
            <Select label="国家 / 地区" value={form.country} onChange={(e) => setForm((f) => ({ ...f, country: e.target.value }))}>
              <option value="">未选择</option>
              {countryOptions.map((option) => (
                <option key={option.value} value={option.value}>{displayOption(option)}</option>
              ))}
              <option value={CUSTOM_OPTION_VALUE}>自定义 / 其他</option>
            </Select>
            {form.country === CUSTOM_OPTION_VALUE ? (
              <Input label="自定义国家 / 地区" value={form.customCountry} onChange={(e) => setForm((f) => ({ ...f, customCountry: e.target.value }))} placeholder="例如：MO / Macau" />
            ) : null}
          </div>
          <div className="vps-create-form__toggle-row">
            <div>
              <div className="input-field__label">SSH Host 与 IP 不一致</div>
              <p className="input-field__hint">
                默认使用 IPv4 作为 SSH Host；只有 NAT、跳板或域名入口不同才需要单独填写。
              </p>
            </div>
            <label className="tg">
              <input type="checkbox" checked={form.sshHostDiffers} onChange={(e) => updateSSHHostDiffers(e.target.checked)} />
              <span className="tg-track" />
              <span>单独填写</span>
            </label>
          </div>
          <div className="vps-create-form__row--3col">
            <Input
              label="SSH Host"
              value={sshHostValue(form)}
              onChange={(e) => setForm((f) => ({ ...f, sshHost: e.target.value }))}
              disabled={!form.sshHostDiffers}
              placeholder={form.sshHostDiffers ? '例如：ssh.example.com' : '默认跟随 IPv4'}
            />
            <Input label="端口" type="number" value={form.sshPort} onChange={(e) => setForm((f) => ({ ...f, sshPort: e.target.value }))} />
            <Input label="用户" value={form.sshUser} onChange={(e) => setForm((f) => ({ ...f, sshUser: e.target.value }))} />
          </div>
          <div className="vps-create-form__toggle-row">
            <div>
              <div className="input-field__label">IPv6</div>
              <p className="input-field__hint">默认不录入 IPv6；开启后才提交 IPv6 地址。</p>
            </div>
            <label className="tg">
              <input type="checkbox" checked={form.ipv6Enabled} onChange={(e) => updateIPv6Enabled(e.target.checked)} />
              <span className="tg-track" />
              <span>启用</span>
            </label>
          </div>
          {form.ipv6Enabled ? (
            <Input label="IPv6" value={form.ipv6} onChange={(e) => setForm((f) => ({ ...f, ipv6: e.target.value }))} placeholder="例如：2001:db8::1" />
          ) : null}
        </div>

        <CollapsibleSection title="补充信息">
          <div className="vps-create-form__section">
            <div className="vps-create-form__row">
              <Input label="产品名" value={form.productName} onChange={(e) => setForm((f) => ({ ...f, productName: e.target.value }))} />
              <Input label="订单号" value={form.orderRef} onChange={(e) => setForm((f) => ({ ...f, orderRef: e.target.value }))} />
            </div>
            <div className="vps-create-form__row">
              <Input label="区域" value={form.region} onChange={(e) => setForm((f) => ({ ...f, region: e.target.value }))} />
              <Input label="城市" value={form.city} onChange={(e) => setForm((f) => ({ ...f, city: e.target.value }))} />
            </div>
            <div className="vps-create-form__row">
              <Input label="数据中心" value={form.datacenter} onChange={(e) => setForm((f) => ({ ...f, datacenter: e.target.value }))} />
              <Input label="访问入口预览" value={selectedSSHHost(form) || form.ipv4} readOnly />
            </div>
            <div className="vps-create-form__row">
              <Input label="操作系统" value={form.osName} onChange={(e) => setForm((f) => ({ ...f, osName: e.target.value }))} />
              <Input label="虚拟化" value={form.virtualization} onChange={(e) => setForm((f) => ({ ...f, virtualization: e.target.value }))} />
            </div>
            <Input label="重要性" value={form.importance} onChange={(e) => setForm((f) => ({ ...f, importance: e.target.value }))} placeholder="normal" />
            <Input label="标签" hint="用逗号分隔" value={form.labels} onChange={(e) => setForm((f) => ({ ...f, labels: e.target.value }))} />
            <Input label="备注" value={form.note} onChange={(e) => setForm((f) => ({ ...f, note: e.target.value }))} />
          </div>
        </CollapsibleSection>

        {error && <p className="create-form__error" role="alert">{error}</p>}
      </form>
    </Modal>
  )
}
