import { type FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { DataTable, Input, Modal, MonoDigits, StatusGlyph, Timestamp } from '../components/atoms'
import type { DataTableColumn } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import { ApiError, createProvider, listProviders, listSubscriptions, listVPSAssets, updateProvider } from '../lib/api'
import type { CreateProviderInput, ProviderRecord, SubscriptionRecord, VPSAssetRecord } from '../lib/types'
import { parseLabels } from './assetPageUtils'

type PageState = {
  loading: boolean
  error: string | null
  contextError: string | null
  providers: ProviderRecord[]
  vps: VPSAssetRecord[]
  subscriptions: SubscriptionRecord[]
}

type FormState = {
  name: string
  website: string
  panelURL: string
  accountHint: string
  country: string
  rating: string
  labels: string
  note: string
}

type ProviderQuickView = 'all' | 'with-assets' | 'multi-account' | 'missing-metadata' | 'unrated' | 'low-rating'
type MetadataIssue = 'missing_panel' | 'missing_account' | 'missing_country' | 'unrated'
type ExternalReputationKind = 'community' | 'rating' | 'benchmark'

type ProviderDirectoryRow = {
  provider: ProviderRecord
  vpsCount: number
  subscriptionCount: number
  metadataIssues: MetadataIssue[]
  hasAssets: boolean
  accounts: string[]
  accountCount: number
  externalLinks: ExternalReputationLink[]
}

type ExternalReputationLink = {
  key: string
  label: string
  ariaLabel: string
  kind: ExternalReputationKind
  description: string
  href: string
}

type ProviderFormProps = {
  id: string
  form: FormState
  error: string | null
  submitting: boolean
  submitLabel: string
  onChange: (form: FormState) => void
  onCancel: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}

const INITIAL_PAGE_STATE: PageState = {
  loading: true,
  error: null,
  contextError: null,
  providers: [],
  vps: [],
  subscriptions: [],
}

const INITIAL_FORM: FormState = {
  name: '',
  website: '',
  panelURL: '',
  accountHint: '',
  country: '',
  rating: '',
  labels: '',
  note: '',
}

const QUICK_VIEW_OPTIONS: Array<{ value: ProviderQuickView; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'with-assets', label: '有资产' },
  { value: 'multi-account', label: '多账号' },
  { value: 'missing-metadata', label: '缺资料' },
  { value: 'unrated', label: '未评分' },
  { value: 'low-rating', label: '低评分' },
]

const EXTERNAL_REPUTATION_SOURCES: Array<{
  key: string
  label: string
  ariaLabel?: string
  kind: ExternalReputationKind
  description: string
  buildHref: (name: string) => string
}> = [
  {
    key: 'lowendtalk',
    label: 'LET',
    kind: 'community',
    description: '社区讨论',
    buildHref: (name) => `https://lowendtalk.com/search?Search=${encodeURIComponent(name)}`,
  },
  {
    key: 'trustpilot',
    label: 'Trust',
    ariaLabel: 'Trustpilot',
    kind: 'rating',
    description: '三方评价',
    buildHref: (name) => `https://www.trustpilot.com/search?query=${encodeURIComponent(name)}`,
  },
  {
    key: 'hostadvice',
    label: 'HostAdv',
    ariaLabel: 'HostAdvice',
    kind: 'rating',
    description: '主机评价',
    buildHref: (name) => `https://www.hostadvice.com/search/?q=${encodeURIComponent(name)}`,
  },
  {
    key: 'vpsbenchmarks',
    label: 'Bench',
    ariaLabel: 'VPSBenchmarks',
    kind: 'benchmark',
    description: '性能基准',
    buildHref: (name) => `https://www.vpsbenchmarks.com/search?search=${encodeURIComponent(name)}`,
  },
]

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function buildInput(form: FormState): CreateProviderInput {
  if (form.name.trim() === '') throw new Error('服务商名称不能为空。')
  const rating = form.rating.trim() === '' ? null : Number(form.rating.trim())
  if (rating != null && (!Number.isInteger(rating) || rating < 1 || rating > 5)) {
    throw new Error('评分必须为 1 到 5。')
  }
  return {
    name: form.name.trim(),
    website: form.website.trim(),
    panel_url: form.panelURL.trim(),
    account_hint: form.accountHint.trim(),
    country: form.country.trim(),
    rating,
    labels: parseLabels(form.labels),
    note: form.note.trim(),
  }
}

function providerToForm(p: ProviderRecord): FormState {
  return {
    name: p.name,
    website: p.website,
    panelURL: p.panel_url,
    accountHint: p.account_hint,
    country: p.country,
    rating: p.rating == null ? '' : String(p.rating),
    labels: p.labels.join(', '),
    note: p.note,
  }
}

function metadataIssues(provider: ProviderRecord): MetadataIssue[] {
  const issues: MetadataIssue[] = []
  if (!provider.panel_url.trim()) issues.push('missing_panel')
  if (!provider.account_hint.trim()) issues.push('missing_account')
  if (!provider.country.trim()) issues.push('missing_country')
  if (provider.rating == null) issues.push('unrated')
  return issues
}

function parseAccountHints(accountHint: string): string[] {
  return accountHint
    .split(/[\n,，;；]+/)
    .map((account) => account.trim())
    .filter(Boolean)
}

function buildExternalReputationLinks(provider: ProviderRecord): ExternalReputationLink[] {
  const providerName = provider.name.trim()
  if (!providerName) return []
  return EXTERNAL_REPUTATION_SOURCES.map((source) => ({
    key: source.key,
    label: source.label,
    ariaLabel: source.ariaLabel ?? source.label,
    kind: source.kind,
    description: source.description,
    href: source.buildHref(providerName),
  }))
}

function isLowRating(provider: ProviderRecord): boolean {
  return provider.rating != null && provider.rating <= 2
}

function buildProviderRows(
  providers: ProviderRecord[],
  vpsAssets: VPSAssetRecord[],
  subscriptions: SubscriptionRecord[],
): ProviderDirectoryRow[] {
  const vpsByID = new Map(vpsAssets.map((asset) => [asset.vps_id, asset]))
  const vpsByProvider = new Map<string, VPSAssetRecord[]>()
  const subscriptionsByProvider = new Map<string, SubscriptionRecord[]>()

  for (const asset of vpsAssets) {
    if (!asset.provider_id) continue
    const current = vpsByProvider.get(asset.provider_id) ?? []
    current.push(asset)
    vpsByProvider.set(asset.provider_id, current)
  }

  for (const subscription of subscriptions) {
    const asset = vpsByID.get(subscription.vps_id)
    if (!asset?.provider_id) continue
    const current = subscriptionsByProvider.get(asset.provider_id) ?? []
    current.push(subscription)
    subscriptionsByProvider.set(asset.provider_id, current)
  }

  return providers.map((provider) => {
    const linkedVPS = vpsByProvider.get(provider.provider_id) ?? []
    const linkedSubscriptions = subscriptionsByProvider.get(provider.provider_id) ?? []
    const accounts = parseAccountHints(provider.account_hint)
    return {
      provider,
      vpsCount: linkedVPS.length,
      subscriptionCount: linkedSubscriptions.length,
      metadataIssues: metadataIssues(provider),
      hasAssets: linkedVPS.length > 0 || linkedSubscriptions.length > 0,
      accounts,
      accountCount: accounts.length,
      externalLinks: buildExternalReputationLinks(provider),
    }
  })
}

function rowMatchesSearch(row: ProviderDirectoryRow, query: string): boolean {
  if (!query) return true
  const provider = row.provider
  const haystack = [
    provider.name,
    provider.country,
    provider.account_hint,
    ...row.accounts,
    ...provider.labels,
  ]
    .join(' ')
    .toLowerCase()
  return haystack.includes(query)
}

function rowMatchesQuickView(row: ProviderDirectoryRow, quickView: ProviderQuickView): boolean {
  if (quickView === 'with-assets') return row.hasAssets
  if (quickView === 'multi-account') return row.accountCount > 1
  if (quickView === 'missing-metadata') return row.metadataIssues.length > 0
  if (quickView === 'unrated') return row.provider.rating == null
  if (quickView === 'low-rating') return isLowRating(row.provider)
  return true
}

function contextErrorMessage(vpsError: string | null, subscriptionsError: string | null): string | null {
  const parts = [
    vpsError ? `VPS 上下文不可用：${vpsError}` : null,
    subscriptionsError ? `订阅上下文不可用：${subscriptionsError}` : null,
  ].filter((part): part is string => part != null)
  return parts.length > 0 ? parts.join('；') : null
}

function ProviderForm({
  id,
  form,
  error,
  submitting,
  submitLabel,
  onChange,
  onCancel,
  onSubmit,
}: ProviderFormProps) {
  const noteID = `${id}-note`

  function update<K extends keyof FormState>(key: K, value: FormState[K]) {
    onChange({ ...form, [key]: value })
  }

  return (
    <form id={id} className="provider-form" onSubmit={onSubmit} noValidate>
      <section className="provider-form__section" aria-labelledby={`${id}-identity`}>
        <h4 id={`${id}-identity`} className="provider-form__section-title">身份</h4>
        <div className="provider-form__grid provider-form__grid--identity">
          <Input label="服务商名称" value={form.name} onChange={(event) => update('name', event.target.value)} required />
          <Input label="国家 / 地区" value={form.country} onChange={(event) => update('country', event.target.value)} />
        </div>
      </section>

      <section className="provider-form__section" aria-labelledby={`${id}-entry`}>
        <h4 id={`${id}-entry`} className="provider-form__section-title">入口</h4>
        <div className="provider-form__grid provider-form__grid--entry">
          <Input label="网站" type="url" value={form.website} onChange={(event) => update('website', event.target.value)} />
          <Input label="面板地址" type="url" value={form.panelURL} onChange={(event) => update('panelURL', event.target.value)} />
          <Input label="账号提示" hint="多个账号用逗号或换行分隔" value={form.accountHint} onChange={(event) => update('accountHint', event.target.value)} />
        </div>
      </section>

      <section className="provider-form__section" aria-labelledby={`${id}-review`}>
        <h4 id={`${id}-review`} className="provider-form__section-title">复盘</h4>
        <div className="provider-form__grid">
          <Input label="评分 (1-5)" type="number" min="1" max="5" value={form.rating} onChange={(event) => update('rating', event.target.value)} />
          <Input label="标签" hint="逗号分隔" value={form.labels} onChange={(event) => update('labels', event.target.value)} />
          <div className="input-field provider-form__wide">
            <label className="input-field__label" htmlFor={noteID}>备注</label>
            <div className="input-field__shell provider-form__textarea-shell">
              <textarea
                id={noteID}
                className="input provider-form__textarea"
                value={form.note}
                onChange={(event) => update('note', event.target.value)}
                rows={3}
              />
            </div>
          </div>
        </div>
      </section>

      {error && <p className="create-form__error" role="alert">{error}</p>}
      <div className="page-form-actions">
        <button type="button" className="btn md secondary" onClick={onCancel} disabled={submitting}>取消</button>
        <button type="submit" className="btn md primary" disabled={submitting}>{submitting ? '保存中…' : submitLabel}</button>
      </div>
    </form>
  )
}

export function ProvidersPage() {
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [reloadKey, setReloadKey] = useState(0)
  const [quickView, setQuickView] = useState<ProviderQuickView>('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<FormState>(INITIAL_FORM)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<FormState>(INITIAL_FORM)
  const [editSubmitting, setEditSubmitting] = useState(false)
  const [editError, setEditError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function loadProviders() {
      const [providersResult, vpsResult, subscriptionsResult] = await Promise.allSettled([
        listProviders(),
        listVPSAssets(),
        listSubscriptions(),
      ])
      if (cancelled) return

      if (providersResult.status === 'rejected') {
        setState({
          ...INITIAL_PAGE_STATE,
          loading: false,
          error: describeError(providersResult.reason, '加载服务商失败'),
        })
        return
      }

      const vpsError = vpsResult.status === 'rejected'
        ? describeError(vpsResult.reason, '加载 VPS 上下文失败')
        : null
      const subscriptionsError = subscriptionsResult.status === 'rejected'
        ? describeError(subscriptionsResult.reason, '加载订阅上下文失败')
        : null

      setState({
        loading: false,
        error: null,
        contextError: contextErrorMessage(vpsError, subscriptionsError),
        providers: providersResult.value,
        vps: vpsResult.status === 'fulfilled' ? vpsResult.value : [],
        subscriptions: subscriptionsResult.status === 'fulfilled' ? subscriptionsResult.value : [],
      })
    }

    void loadProviders()
    return () => { cancelled = true }
  }, [reloadKey])

  function openCreate() {
    setCreateOpen(true)
    setCreateForm(INITIAL_FORM)
    setCreateError(null)
    cancelEdit()
  }

  function closeCreate() {
    setCreateOpen(false)
    setCreateForm(INITIAL_FORM)
    setCreateError(null)
  }

  function handleCreate(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setCreateError(null)
    let input: CreateProviderInput
    try {
      input = buildInput(createForm)
    } catch (err: unknown) {
      setCreateError(describeError(err, '输入无效'))
      return
    }
    setCreateSubmitting(true)
    createProvider(input)
      .then((provider) => {
        setState((current) => ({
          ...current,
          loading: false,
          error: null,
          providers: [provider, ...current.providers.filter((item) => item.provider_id !== provider.provider_id)],
        }))
        closeCreate()
      })
      .catch((err: unknown) => setCreateError(describeError(err, '创建失败')))
      .finally(() => setCreateSubmitting(false))
  }

  function startEdit(provider: ProviderRecord) {
    closeCreate()
    setEditingId(provider.provider_id)
    setEditForm(providerToForm(provider))
    setEditError(null)
  }

  function cancelEdit() {
    setEditingId(null)
    setEditForm(INITIAL_FORM)
    setEditError(null)
  }

  function handleEdit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!editingId) return
    setEditError(null)
    let input: CreateProviderInput
    try {
      input = buildInput(editForm)
    } catch (err: unknown) {
      setEditError(describeError(err, '输入无效'))
      return
    }
    setEditSubmitting(true)
    updateProvider(editingId, input)
      .then((provider) => {
        setState((current) => ({
          ...current,
          loading: false,
          error: null,
          providers: current.providers.map((item) => item.provider_id === provider.provider_id ? provider : item),
        }))
        cancelEdit()
      })
      .catch((err: unknown) => setEditError(describeError(err, '更新失败')))
      .finally(() => setEditSubmitting(false))
  }

  const rows = buildProviderRows(state.providers, state.vps, state.subscriptions)
  const effectiveQuickView = state.contextError && quickView === 'with-assets' ? 'all' : quickView
  const normalizedQuery = searchQuery.trim().toLowerCase()
  const filteredRows = rows.filter((row) =>
    rowMatchesQuickView(row, effectiveQuickView) && rowMatchesSearch(row, normalizedQuery),
  )
  const hasAssetsCount = state.contextError ? null : rows.filter((row) => row.hasAssets).length
  const multiAccountCount = rows.filter((row) => row.accountCount > 1).length
  const missingMetadataCount = rows.filter((row) => row.metadataIssues.length > 0).length
  const unratedCount = rows.filter((row) => row.provider.rating == null).length
  const lowRatingCount = rows.filter((row) => isLowRating(row.provider)).length

  const columns: DataTableColumn<ProviderDirectoryRow>[] = [
    {
      key: 'identity',
      label: '服务商',
      width: '160px',
      render: (row) => {
        const visibleAccounts = row.accounts.slice(0, 2)
        const hiddenAccountCount = Math.max(row.accounts.length - visibleAccounts.length, 0)
        const metaItems = [
          row.provider.country.trim(),
          row.accountCount > 0 ? `${row.accountCount} 个账号` : null,
        ].filter((item): item is string => item != null && item !== '')
        return (
          <div className="provider-directory-identity">
            <button
              type="button"
              className="provider-directory-name-button"
              onClick={() => startEdit(row.provider)}
              aria-label={`编辑服务商 ${row.provider.name}`}
            >
              {row.provider.name}
            </button>
            {metaItems.length > 0 ? <span className="provider-directory-meta">{metaItems.join(' · ')}</span> : null}
            {row.accounts.length > 0 ? (
              <span className="provider-directory-account-list" aria-label={`${row.provider.name} 账号提示`}>
                {visibleAccounts.map((account) => (
                  <span className="provider-directory-account-chip" key={account}>{account}</span>
                ))}
                {hiddenAccountCount > 0 ? <span className="provider-directory-account-chip provider-directory-account-chip--more">+{hiddenAccountCount}</span> : null}
              </span>
            ) : null}
          </div>
        )
      },
    },
    {
      key: 'assets',
      label: '资产上下文',
      width: '122px',
      render: (row) => state.contextError ? (
        <span className="provider-directory-asset-count provider-directory-asset-count--muted">—</span>
      ) : (
        <span className="provider-directory-asset-count">
          <MonoDigits>{row.vpsCount}</MonoDigits> VPS · <MonoDigits>{row.subscriptionCount}</MonoDigits> 订阅
        </span>
      ),
    },
    {
      key: 'entry',
      label: '服务入口',
      width: '176px',
      render: (row) => {
        const website = row.provider.website.trim()
        const panelURL = row.provider.panel_url.trim()
        const providerID = encodeURIComponent(row.provider.provider_id)
        return (
          <div className="provider-directory-entry-links">
            {website ? (
              <a
                className="provider-directory-entry-link"
                href={website}
                target="_blank"
                rel="noreferrer"
                aria-label={`打开官网 ${row.provider.name}`}
              >
                官网
              </a>
            ) : null}
            {panelURL ? (
              <a
                className="provider-directory-entry-link"
                href={panelURL}
                target="_blank"
                rel="noreferrer"
                aria-label={`打开面板 ${row.provider.name}`}
              >
                面板
              </a>
            ) : null}
            {!state.contextError && row.vpsCount > 0 ? (
              <Link className="provider-directory-entry-link" to={`/vps?provider_id=${providerID}`} aria-label={`查看 ${row.provider.name} VPS`}>
                VPS
              </Link>
            ) : null}
            {!state.contextError && row.subscriptionCount > 0 ? (
              <Link className="provider-directory-entry-link" to={`/subscriptions?provider_id=${providerID}`} aria-label={`查看 ${row.provider.name} 订阅`}>
                订阅
              </Link>
            ) : null}
            {!state.contextError && row.vpsCount > 0 ? (
              <Link className="provider-directory-entry-link" to={`/asset-decisions?view=provider&renew_within_days=30&provider_id=${providerID}`} aria-label={`查看 ${row.provider.name} 服务商组合决策`}>
                组合决策
              </Link>
            ) : null}
          </div>
        )
      },
    },
    {
      key: 'rating',
      label: '我的评分',
      width: '70px',
      render: (row) => {
        if (row.provider.rating == null) {
          return null
        }
        return (
          <span className={`provider-directory-rating ${isLowRating(row.provider) ? 'provider-directory-rating--low' : ''}`}>
            <StatusGlyph state={isLowRating(row.provider) ? 'alert' : 'normal'} size="sm" />
            <MonoDigits>{row.provider.rating}/5</MonoDigits>
          </span>
        )
      },
    },
    {
      key: 'reputation',
      label: '外部口碑',
      width: '166px',
      render: (row) => (
        <div className="provider-directory-reputation">
          <span className="provider-directory-reputation-links">
            {row.externalLinks.map((link) => (
              <a
                className={`provider-directory-reputation-link provider-directory-reputation-link--${link.kind}`}
                href={link.href}
                target="_blank"
                rel="noreferrer"
                aria-label={`在 ${link.ariaLabel} 搜索 ${row.provider.name} 外部口碑`}
                title={link.description}
                key={link.key}
              >
                {link.label}
              </a>
            ))}
          </span>
        </div>
      ),
    },
    {
      key: 'notes',
      label: '标签 / 备注',
      width: '154px',
      render: (row) => {
        const note = row.provider.note.trim()
        if (row.provider.labels.length === 0 && !note) return null
        return (
          <div className="provider-directory-notes">
            {row.provider.labels.length > 0 ? (
              <span className="provider-directory-tags">
                {row.provider.labels.map((label) => <span className="provider-directory-tag" key={label}>{label}</span>)}
              </span>
            ) : null}
            {note ? <p className="provider-directory-note" title={note}>{note}</p> : null}
          </div>
        )
      },
    },
    {
      key: 'updated',
      label: '更新时间',
      width: '88px',
      cellClassName: 'mono',
      render: (row) => <Timestamp value={row.provider.updated_at} />,
    },
  ]

  return (
    <div className="page-stack provider-directory">
      <div className="watchtower-header">
        <div className="watchtower-header__row1">
          <div className="watchtower-header__title-block">
            <h1>服务商目录</h1>
            <div className="badge-row">
              <span className="badge badge--state tone--normal"><span className="badge__dot" />{state.providers.length} 个服务商</span>
              <span className="badge badge--state tone--maintenance"><span className="badge__dot" />{multiAccountCount} 个多账号</span>
              <span className="badge badge--state tone--notice"><span className="badge__dot" />{missingMetadataCount} 个待补事实</span>
            </div>
          </div>
          <div className="watchtower-header__actions">
            <button className="btn md primary" onClick={openCreate}>
              <svg viewBox="0 0 16 16"><path d="M8 2v12M2 8h12" /></svg>
              新建服务商
            </button>
          </div>
        </div>
        <div className="watchtower-header__row2">
          <span className="watchtower-header__meta-item">供 VPS 与订阅引用的低频资产事实</span>
          <span className="watchtower-header__meta-sep">·</span>
          <span className="watchtower-header__meta-item">我的评分与外部口碑入口分离</span>
          <span className="watchtower-header__meta-sep">·</span>
          <span className="watchtower-header__meta-item">不声明外部账号、账单或服务商状态真相</span>
        </div>
      </div>

      {state.loading ? (
        <PageStateView kind="loading" title="正在加载服务商目录…" surface="empty" compact />
      ) : state.error ? (
        <PageStateView
          kind="error"
          title="加载失败"
          description={state.error}
          action={<button className="btn sm secondary" onClick={() => { setState(INITIAL_PAGE_STATE); setReloadKey((key) => key + 1) }}>重试</button>}
          surface="empty"
          compact
        />
      ) : state.providers.length === 0 ? (
        <PageStateView
          kind="empty"
          title="尚未记录服务商"
          description="先记录供应商名称和面板入口，创建 VPS 时就能复用这份资产事实。"
          action={<button className="btn sm primary" onClick={openCreate}>创建第一个服务商</button>}
          surface="empty"
          compact
        />
      ) : (
        <>
          <div className="provider-directory-summary-rail animate-in" aria-label="服务商目录摘要">
            <span><strong><MonoDigits>{rows.length}</MonoDigits></strong> 服务商</span>
            <span><strong><MonoDigits>{hasAssetsCount == null ? '—' : hasAssetsCount}</MonoDigits></strong> 有资产</span>
            <span><strong><MonoDigits>{multiAccountCount}</MonoDigits></strong> 多账号</span>
            <span><strong><MonoDigits>{missingMetadataCount}</MonoDigits></strong> 待补资料</span>
            <span><strong><MonoDigits>{unratedCount}</MonoDigits>/<MonoDigits>{lowRatingCount}</MonoDigits></strong> 未评分 / 低评分</span>
            <span><strong><MonoDigits>{EXTERNAL_REPUTATION_SOURCES.length}</MonoDigits></strong> 外部口碑源入口</span>
          </div>

          <section className="page-panel page-panel--scroll-x provider-directory-panel">
            <div className="section-heading section-heading--inline">
              <div>
                <p className="section-heading__eyebrow">Providers</p>
                <h2 className="section-heading__title">服务商与入口</h2>
              </div>
              <span className="section-heading__meta">{filteredRows.length} / {rows.length} 条</span>
            </div>

            <div className="provider-directory-toolbar">
              <div className="provider-directory-search">
                <Input
                  label="搜索服务商"
                  value={searchQuery}
                  onChange={(event) => setSearchQuery(event.target.value)}
                  placeholder="名称 / 国家 / 账号 / 标签"
                />
              </div>
              <div className="provider-directory-quick-views" role="group" aria-label="服务商视图">
                {QUICK_VIEW_OPTIONS.map((option) => (
                  <button
                    key={option.value}
                    type="button"
                    className={`provider-directory-view-button ${effectiveQuickView === option.value ? 'provider-directory-view-button--active' : ''}`}
                    onClick={() => setQuickView(option.value)}
                    disabled={state.contextError != null && option.value === 'with-assets'}
                    aria-pressed={effectiveQuickView === option.value}
                  >
                    {option.label}
                  </button>
                ))}
              </div>
            </div>

            {state.contextError ? (
              <p className="asset-operation-feedback asset-operation-feedback--notice" role="status">
                {state.contextError}
              </p>
            ) : null}

            <DataTable<ProviderDirectoryRow>
              className="provider-directory-table"
              density="compact"
              columns={columns}
              rows={filteredRows}
              rowKey={(row) => row.provider.provider_id}
              emptyContent={<span className="provider-directory-empty-inline">没有匹配的服务商</span>}
            />
          </section>
        </>
      )}

      <Modal open={createOpen} onClose={closeCreate} title="新建服务商" ariaLabel="新建服务商表单" size="md">
        <ProviderForm
          id="provider-create-form"
          form={createForm}
          error={createError}
          submitting={createSubmitting}
          submitLabel="创建"
          onChange={setCreateForm}
          onCancel={closeCreate}
          onSubmit={handleCreate}
        />
      </Modal>

      <Modal open={editingId != null} onClose={cancelEdit} title="编辑服务商" ariaLabel="编辑服务商表单" size="md">
        <ProviderForm
          id="provider-edit-form"
          form={editForm}
          error={editError}
          submitting={editSubmitting}
          submitLabel="保存"
          onChange={setEditForm}
          onCancel={cancelEdit}
          onSubmit={handleEdit}
        />
      </Modal>
    </div>
  )
}
