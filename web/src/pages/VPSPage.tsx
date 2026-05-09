import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { Button, DataTable, Input, MonoDigits, type DataTableColumn } from '../components/atoms'
import { FilterBar, FilterChip, FilterSelect, type FilterSelectOption } from '../components/filters'
import { ApiError, createVPSAsset, listProviders, listVPSAssets } from '../lib/api'
import { formatOptional } from '../lib/format'
import {
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  VPS_USAGE_STATUS_LABELS,
  type CreateVPSAssetInput,
  type ProviderRecord,
  type VPSAssetListFilter,
  type VPSAssetRecord,
  type VPSLifecycleStatus,
  type VPSRenewalDecision,
  type VPSUsageStatus,
} from '../lib/types'
import {
  AssetLabels,
  LifecycleBadge,
  RenewalBadge,
  UsageBadge,
} from './assetPageBadges'
import {
  lifecycleLabel,
  parseLabels,
  renewalLabel,
  usageLabel,
} from './assetPageUtils'

type PageState = {
  loading: boolean
  error: string | null
  vps: VPSAssetRecord[]
  providers: ProviderRecord[]
}

type CreateVPSFormState = {
  displayName: string
  providerID: string
  providerName: string
  productName: string
  orderRef: string
  country: string
  region: string
  city: string
  datacenter: string
  ipv4: string
  ipv6: string
  sshHost: string
  sshPort: string
  sshUser: string
  osName: string
  virtualization: string
  lifecycleStatus: VPSLifecycleStatus
  usageStatus: VPSUsageStatus
  renewalDecision: VPSRenewalDecision
  importance: string
  labels: string
  note: string
}

type FilterState = {
  provider_id: string | null
  lifecycle_status: VPSLifecycleStatus | null
  usage_status: VPSUsageStatus | null
  renewal_decision: VPSRenewalDecision | null
}

const INITIAL_PAGE_STATE: PageState = {
  loading: true,
  error: null,
  vps: [],
  providers: [],
}

const INITIAL_CREATE_FORM: CreateVPSFormState = {
  displayName: '',
  providerID: '',
  providerName: '',
  productName: '',
  orderRef: '',
  country: '',
  region: '',
  city: '',
  datacenter: '',
  ipv4: '',
  ipv6: '',
  sshHost: '',
  sshPort: '22',
  sshUser: 'root',
  osName: '',
  virtualization: '',
  lifecycleStatus: 'active',
  usageStatus: 'unknown',
  renewalDecision: 'unreviewed',
  importance: 'normal',
  labels: '',
  note: '',
}

const LIFECYCLE_OPTIONS = Object.entries(VPS_LIFECYCLE_STATUS_LABELS).map(([value, label]) => ({
  value,
  label,
}))
const USAGE_OPTIONS = Object.entries(VPS_USAGE_STATUS_LABELS).map(([value, label]) => ({
  value,
  label,
}))
const RENEWAL_OPTIONS = Object.entries(VPS_RENEWAL_DECISION_LABELS).map(([value, label]) => ({
  value,
  label,
}))

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function parseFilters(searchParams: URLSearchParams): FilterState {
  const lifecycle = searchParams.get('lifecycle_status') as VPSLifecycleStatus | null
  const usage = searchParams.get('usage_status') as VPSUsageStatus | null
  const renewal = searchParams.get('renewal_decision') as VPSRenewalDecision | null
  return {
    provider_id: searchParams.get('provider_id') || null,
    lifecycle_status: lifecycle && lifecycle in VPS_LIFECYCLE_STATUS_LABELS ? lifecycle : null,
    usage_status: usage && usage in VPS_USAGE_STATUS_LABELS ? usage : null,
    renewal_decision: renewal && renewal in VPS_RENEWAL_DECISION_LABELS ? renewal : null,
  }
}

function filterToQuery(filters: FilterState): URLSearchParams {
  const params = new URLSearchParams()
  if (filters.provider_id) params.set('provider_id', filters.provider_id)
  if (filters.lifecycle_status) params.set('lifecycle_status', filters.lifecycle_status)
  if (filters.usage_status) params.set('usage_status', filters.usage_status)
  if (filters.renewal_decision) params.set('renewal_decision', filters.renewal_decision)
  return params
}

function filterToAPI(filters: FilterState): VPSAssetListFilter {
  return {
    provider_id: filters.provider_id,
    lifecycle_status: filters.lifecycle_status,
    usage_status: filters.usage_status,
    renewal_decision: filters.renewal_decision,
  }
}

function hasActiveFilters(filters: FilterState): boolean {
  return Boolean(
    filters.provider_id ||
      filters.lifecycle_status ||
      filters.usage_status ||
      filters.renewal_decision,
  )
}

function buildCreateInput(form: CreateVPSFormState, providers: ProviderRecord[]): CreateVPSAssetInput {
  if (form.displayName.trim() === '') {
    throw new Error('VPS 名称不能为空。')
  }
  const selectedProvider = providers.find((provider) => provider.provider_id === form.providerID)
  const sshPort = form.sshPort.trim() === '' ? undefined : Number.parseInt(form.sshPort.trim(), 10)
  if (sshPort != null && (!Number.isInteger(sshPort) || sshPort < 1 || sshPort > 65535)) {
    throw new Error('SSH 端口必须为 1 到 65535。')
  }

  return {
    display_name: form.displayName.trim(),
    provider_id: form.providerID || null,
    provider_name: selectedProvider?.name ?? form.providerName.trim(),
    product_name: form.productName.trim(),
    order_ref: form.orderRef.trim(),
    country: form.country.trim(),
    region: form.region.trim(),
    city: form.city.trim(),
    datacenter: form.datacenter.trim(),
    ipv4: form.ipv4.trim(),
    ipv6: form.ipv6.trim(),
    ssh_host: form.sshHost.trim(),
    ...(sshPort == null ? {} : { ssh_port: sshPort }),
    ssh_user: form.sshUser.trim(),
    os_name: form.osName.trim(),
    virtualization: form.virtualization.trim(),
    lifecycle_status: form.lifecycleStatus,
    usage_status: form.usageStatus,
    renewal_decision: form.renewalDecision,
    importance: form.importance.trim() || 'normal',
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}

function providerOptions(providers: ProviderRecord[]): FilterSelectOption[] {
  return providers.map((provider) => ({
    value: provider.provider_id,
    label: provider.name,
  }))
}

export function VPSPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => parseFilters(searchParams), [searchParams])
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<CreateVPSFormState>(INITIAL_CREATE_FORM)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    Promise.all([listVPSAssets(filterToAPI(filters)), listProviders()])
      .then(([vps, providers]) => {
        if (cancelled) return
        setState({ loading: false, error: null, vps, providers })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({
          loading: false,
          error: describeError(error, '加载 VPS 资产失败'),
          vps: [],
          providers: [],
        })
      })

    return () => {
      cancelled = true
    }
  }, [filters])

  function setFilter<K extends keyof FilterState>(key: K, value: FilterState[K]) {
    const next = { ...filters, [key]: value }
    setSearchParams(filterToQuery(next), { replace: true })
  }

  function clearFilters() {
    setSearchParams(new URLSearchParams(), { replace: true })
  }

  function toggleCreatePanel() {
    setCreateOpen((open) => {
      const next = !open
      if (!next) {
        setCreateForm(INITIAL_CREATE_FORM)
        setCreateError(null)
      }
      return next
    })
  }

  function handleCreateSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateError(null)

    let input: CreateVPSAssetInput
    try {
      input = buildCreateInput(createForm, state.providers)
    } catch (error: unknown) {
      setCreateError(describeError(error, 'VPS 输入无效'))
      return
    }

    setCreateSubmitting(true)
    createVPSAsset(input)
      .then((vps) => {
        navigate(`/vps/${vps.vps_id}`)
      })
      .catch((error: unknown) => {
        setCreateError(describeError(error, '创建 VPS 失败'))
      })
      .finally(() => setCreateSubmitting(false))
  }

  function providerName(providerID: string | null): string {
    if (!providerID) return ''
    return state.providers.find((provider) => provider.provider_id === providerID)?.name ?? providerID
  }

  const columns: DataTableColumn<VPSAssetRecord>[] = [
    {
      key: 'identity',
      label: '名称',
      width: '22%',
      render: (vps) => (
        <div className="asset-table__identity">
          <strong>{vps.display_name}</strong>
          <span>{vps.vps_id}</span>
        </div>
      ),
    },
    {
      key: 'provider',
      label: '服务商 / 区域',
      render: (vps) => (
        <div className="asset-table__stack">
          <strong>{formatOptional(vps.provider_name)}</strong>
          <span>{[vps.country, vps.region, vps.city].filter(Boolean).join(' · ') || '—'}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态',
      render: (vps) => (
        <span className="asset-status-stack">
          <LifecycleBadge value={vps.lifecycle_status} />
          <UsageBadge value={vps.usage_status} />
          <RenewalBadge value={vps.renewal_decision} />
        </span>
      ),
    },
    {
      key: 'nodes',
      label: '关联 Node',
      align: 'center',
      render: (vps) => <MonoDigits>{vps.active_node_link_count}</MonoDigits>,
    },
    {
      key: 'labels',
      label: '标签',
      render: (vps) => <AssetLabels labels={vps.labels} />,
    },
  ]

  const providerSelectOptions = providerOptions(state.providers)
  const active = hasActiveFilters(filters)

  return (
    <div className="page-stack asset-page vps-page">
      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">ASSET LEDGER</div>
          <h1 className="page-panel__title">VPS</h1>
          <p className="page-panel__description">
            面向续费、迁移和监控关联的 VPS 资产列表。列表优先展示决策字段，IP、SSH 和系统信息进入详情页。
          </p>
        </div>
        <div className="page-panel__actions">
          <Button variant={createOpen ? 'secondary' : 'primary'} onClick={toggleCreatePanel}>
            {createOpen ? '收起创建' : state.vps.length === 0 ? '创建第一台 VPS' : '新建 VPS'}
          </Button>
        </div>
      </section>

      {createOpen && (
        <section className="page-panel">
          <div className="page-panel__eyebrow">CREATE</div>
          <h2 className="page-panel__title">VPS 创建</h2>
          <form onSubmit={handleCreateSubmit}>
            <Input label="VPS 名称" value={createForm.displayName} onChange={(event) => setCreateForm({ ...createForm, displayName: event.target.value })} />
            <label className="input-field">
              <span className="input-field__label">资产服务商</span>
              <select className="input" value={createForm.providerID} onChange={(event) => setCreateForm({ ...createForm, providerID: event.target.value })}>
                <option value="">未关联服务商</option>
                {providerSelectOptions.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
            <Input label="服务商名称快照" value={createForm.providerName} onChange={(event) => setCreateForm({ ...createForm, providerName: event.target.value })} />
            <Input label="产品名" value={createForm.productName} onChange={(event) => setCreateForm({ ...createForm, productName: event.target.value })} />
            <Input label="订单号" value={createForm.orderRef} onChange={(event) => setCreateForm({ ...createForm, orderRef: event.target.value })} />
            <Input label="国家" value={createForm.country} onChange={(event) => setCreateForm({ ...createForm, country: event.target.value })} />
            <Input label="区域" value={createForm.region} onChange={(event) => setCreateForm({ ...createForm, region: event.target.value })} />
            <Input label="城市" value={createForm.city} onChange={(event) => setCreateForm({ ...createForm, city: event.target.value })} />
            <Input label="数据中心" value={createForm.datacenter} onChange={(event) => setCreateForm({ ...createForm, datacenter: event.target.value })} />
            <Input label="IPv4" value={createForm.ipv4} onChange={(event) => setCreateForm({ ...createForm, ipv4: event.target.value })} />
            <Input label="IPv6" value={createForm.ipv6} onChange={(event) => setCreateForm({ ...createForm, ipv6: event.target.value })} />
            <Input label="SSH Host" value={createForm.sshHost} onChange={(event) => setCreateForm({ ...createForm, sshHost: event.target.value })} />
            <Input label="SSH 端口" type="number" value={createForm.sshPort} onChange={(event) => setCreateForm({ ...createForm, sshPort: event.target.value })} />
            <Input label="SSH 用户" value={createForm.sshUser} onChange={(event) => setCreateForm({ ...createForm, sshUser: event.target.value })} />
            <Input label="操作系统" value={createForm.osName} onChange={(event) => setCreateForm({ ...createForm, osName: event.target.value })} />
            <Input label="虚拟化" value={createForm.virtualization} onChange={(event) => setCreateForm({ ...createForm, virtualization: event.target.value })} />
            <label className="input-field">
              <span className="input-field__label">生命周期</span>
              <select className="input" value={createForm.lifecycleStatus} onChange={(event) => setCreateForm({ ...createForm, lifecycleStatus: event.target.value as VPSLifecycleStatus })}>
                {LIFECYCLE_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
              </select>
            </label>
            <label className="input-field">
              <span className="input-field__label">用途状态</span>
              <select className="input" value={createForm.usageStatus} onChange={(event) => setCreateForm({ ...createForm, usageStatus: event.target.value as VPSUsageStatus })}>
                {USAGE_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
              </select>
            </label>
            <label className="input-field">
              <span className="input-field__label">续费决策</span>
              <select className="input" value={createForm.renewalDecision} onChange={(event) => setCreateForm({ ...createForm, renewalDecision: event.target.value as VPSRenewalDecision })}>
                {RENEWAL_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
              </select>
            </label>
            <Input label="重要性" value={createForm.importance} onChange={(event) => setCreateForm({ ...createForm, importance: event.target.value })} />
            <Input label="标签" hint="用逗号分隔" value={createForm.labels} onChange={(event) => setCreateForm({ ...createForm, labels: event.target.value })} />
            <Input label="备注" value={createForm.note} onChange={(event) => setCreateForm({ ...createForm, note: event.target.value })} />
            {createError && <p className="create-form__error" role="alert">{createError}</p>}
            <div className="page-form-actions">
              <Button type="submit" disabled={createSubmitting}>
                {createSubmitting ? '创建中…' : '创建 VPS'}
              </Button>
            </div>
          </form>
        </section>
      )}

      <section className="page-panel">
        <FilterBar
          hasActiveFilters={active}
          onClearAll={clearFilters}
          activeChips={
            <>
              {filters.provider_id && <FilterChip label={`服务商: ${providerName(filters.provider_id)}`} onRemove={() => setFilter('provider_id', null)} />}
              {filters.lifecycle_status && <FilterChip label={`生命周期: ${lifecycleLabel(filters.lifecycle_status)}`} onRemove={() => setFilter('lifecycle_status', null)} />}
              {filters.usage_status && <FilterChip label={`用途: ${usageLabel(filters.usage_status)}`} onRemove={() => setFilter('usage_status', null)} />}
              {filters.renewal_decision && <FilterChip label={`续费: ${renewalLabel(filters.renewal_decision)}`} onRemove={() => setFilter('renewal_decision', null)} />}
            </>
          }
        >
          <FilterSelect label="服务商" value={filters.provider_id} options={providerSelectOptions} onChange={(value) => setFilter('provider_id', value)} />
          <FilterSelect label="生命周期" value={filters.lifecycle_status} options={LIFECYCLE_OPTIONS} onChange={(value) => setFilter('lifecycle_status', value as VPSLifecycleStatus | null)} />
          <FilterSelect label="用途状态" value={filters.usage_status} options={USAGE_OPTIONS} onChange={(value) => setFilter('usage_status', value as VPSUsageStatus | null)} />
          <FilterSelect label="续费决策" value={filters.renewal_decision} options={RENEWAL_OPTIONS} onChange={(value) => setFilter('renewal_decision', value as VPSRenewalDecision | null)} />
        </FilterBar>
      </section>

      <section className="page-panel">
        <div className="section-heading">
          <div>
            <p className="section-heading__eyebrow">VPS ASSETS</p>
            <h2>VPS 列表</h2>
          </div>
          <span className="section-heading__meta">
            <MonoDigits>{state.vps.length}</MonoDigits> 台 VPS
          </span>
        </div>

        {state.loading ? (
          <div className="empty-state">正在加载 VPS…</div>
        ) : state.error ? (
          <div className="empty-state">{state.error}</div>
        ) : (
          <DataTable
            className="asset-table vps-table"
            columns={columns}
            rows={state.vps}
            rowKey={(vps) => vps.vps_id}
            onRowClick={(vps) => navigate(`/vps/${vps.vps_id}`)}
            emptyContent={<span className="empty-inline">暂无 VPS 资产</span>}
          />
        )}
      </section>
    </div>
  )
}
