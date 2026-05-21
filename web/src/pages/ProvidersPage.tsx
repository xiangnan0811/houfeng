import { type FormEvent, useEffect, useState } from 'react'

import { Button, DataTable, Drawer, Input, MonoDigits, Timestamp, type DataTableColumn } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import { ApiError, createProvider, listProviders, updateProvider } from '../lib/api'
import { formatOptional } from '../lib/format'
import type { CreateProviderInput, ProviderRecord, UpdateProviderInput } from '../lib/types'
import { AssetLabels } from './assetPageBadges'
import { parseLabels } from './assetPageUtils'

type PageState = {
  loading: boolean
  error: string | null
  providers: ProviderRecord[]
}

type CreateProviderFormState = {
  name: string
  website: string
  panelURL: string
  accountHint: string
  country: string
  rating: string
  labels: string
  note: string
}

const INITIAL_PAGE_STATE: PageState = {
  loading: true,
  error: null,
  providers: [],
}

const INITIAL_CREATE_FORM: CreateProviderFormState = {
  name: '',
  website: '',
  panelURL: '',
  accountHint: '',
  country: '',
  rating: '',
  labels: '',
  note: '',
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function buildCreateProviderInput(form: CreateProviderFormState): CreateProviderInput {
  if (form.name.trim() === '') {
    throw new Error('服务商名称不能为空。')
  }
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

function providerToForm(provider: ProviderRecord): CreateProviderFormState {
  return {
    name: provider.name,
    website: provider.website,
    panelURL: provider.panel_url,
    accountHint: provider.account_hint,
    country: provider.country,
    rating: provider.rating == null ? '' : String(provider.rating),
    labels: provider.labels.join(', '),
    note: provider.note,
  }
}

function buildUpdateProviderInput(form: CreateProviderFormState): UpdateProviderInput {
  return buildCreateProviderInput(form)
}

export function ProvidersPage() {
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [reloadKey, setReloadKey] = useState(0)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<CreateProviderFormState>(INITIAL_CREATE_FORM)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [editingProviderId, setEditingProviderId] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<CreateProviderFormState>(INITIAL_CREATE_FORM)
  const [editSubmitting, setEditSubmitting] = useState(false)
  const [editError, setEditError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    listProviders()
      .then((providers) => {
        if (cancelled) return
        setState({ loading: false, error: null, providers })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState({
          loading: false,
          error: describeError(error, '加载服务商失败'),
          providers: [],
        })
      })

    return () => {
      cancelled = true
    }
  }, [reloadKey])

  function openCreateDrawer() {
    setCreateOpen(true)
    setCreateForm(INITIAL_CREATE_FORM)
    setCreateError(null)
    cancelEdit()
  }

  function closeCreateDrawer() {
    setCreateOpen(false)
    setCreateForm(INITIAL_CREATE_FORM)
    setCreateError(null)
  }

  function handleCreateSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreateError(null)

    let input: CreateProviderInput
    try {
      input = buildCreateProviderInput(createForm)
    } catch (error: unknown) {
      setCreateError(describeError(error, '服务商输入无效'))
      return
    }

    setCreateSubmitting(true)
    createProvider(input)
      .then((provider) => {
        setState((current) => ({
          loading: false,
          error: null,
          providers: [provider, ...current.providers.filter((item) => item.provider_id !== provider.provider_id)],
        }))
        closeCreateDrawer()
      })
      .catch((error: unknown) => {
        setCreateError(describeError(error, '创建服务商失败'))
      })
      .finally(() => setCreateSubmitting(false))
  }

  function startEdit(provider: ProviderRecord) {
    closeCreateDrawer()
    setEditingProviderId(provider.provider_id)
    setEditForm(providerToForm(provider))
    setEditError(null)
  }

  function cancelEdit() {
    setEditingProviderId(null)
    setEditForm(INITIAL_CREATE_FORM)
    setEditError(null)
  }

  function handleEditSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!editingProviderId) return
    setEditError(null)

    let input: UpdateProviderInput
    try {
      input = buildUpdateProviderInput(editForm)
    } catch (error: unknown) {
      setEditError(describeError(error, '服务商输入无效'))
      return
    }

    setEditSubmitting(true)
    updateProvider(editingProviderId, input)
      .then((provider) => {
        setState((current) => ({
          loading: false,
          error: null,
          providers: current.providers.map((item) =>
            item.provider_id === provider.provider_id ? provider : item,
          ),
        }))
        cancelEdit()
      })
      .catch((error: unknown) => {
        setEditError(describeError(error, '更新服务商失败'))
      })
      .finally(() => setEditSubmitting(false))
  }

  const columns: DataTableColumn<ProviderRecord>[] = [
    {
      key: 'provider',
      label: '服务商',
      render: (provider) => (
        <div className="asset-table__identity">
          <strong>{provider.name}</strong>
          <span>{provider.provider_id}</span>
        </div>
      ),
    },
    {
      key: 'account',
      label: '账号 / 国家',
      render: (provider) => (
        <div className="asset-table__stack">
          <strong>{formatOptional(provider.account_hint)}</strong>
          <span>{formatOptional(provider.country)}</span>
        </div>
      ),
    },
    {
      key: 'rating',
      label: '评分',
      render: (provider) =>
        provider.rating == null ? (
          <span className="empty-inline">未评</span>
        ) : (
          <MonoDigits>{provider.rating}/5</MonoDigits>
        ),
    },
    {
      key: 'labels',
      label: '标签',
      render: (provider) => <AssetLabels labels={provider.labels} />,
    },
    {
      key: 'updated',
      label: '更新时间',
      render: (provider) => <Timestamp value={provider.updated_at} />,
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      render: (provider) => (
        <Button
          variant="ghost"
          size="sm"
          aria-label={`编辑 ${provider.name}`}
          onClick={() => startEdit(provider)}
        >
          编辑
        </Button>
      ),
    },
  ]

  const providerCount = state.providers.length
  const countryCount = new Set(state.providers.map((provider) => provider.country.trim()).filter(Boolean)).size
  const accountCount = state.providers.filter((provider) => provider.account_hint.trim() !== '').length
  const lowRatedCount = state.providers.filter((provider) => provider.rating != null && provider.rating <= 2).length
  const labeledCount = state.providers.filter((provider) => provider.labels.length > 0).length

  return (
    <div className="page-stack asset-page providers-page">
      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">ASSET LEDGER</div>
          <h1 className="page-panel__title">服务商</h1>
          <p className="page-panel__description">
            服务商与账号主数据是 VPS 台账、续费证据和资产决策的引用底座。这里记录面板入口、账号提示、国家和标签，不会同步修改 Node 的 provider hint。
          </p>
        </div>
        <div className="page-panel__actions">
          <Button onClick={openCreateDrawer}>
            {providerCount === 0 ? '创建第一个服务商' : '新建服务商'}
          </Button>
        </div>
      </section>

      <section className="page-panel providers-command-panel">
        <div className="section-heading section-heading--inline">
          <div>
            <p className="section-heading__eyebrow">MASTER DATA CONTEXT</p>
            <h2>服务商主数据概览</h2>
            <p className="section-heading__description">
              服务商页只承载资产台账的账号证据和供应商归档口径；主路径仍是下方高密度列表，创建与编辑进入右侧抽屉。
            </p>
          </div>
        </div>
        <dl className="asset-workbench-summary">
          <div className="asset-workbench-summary__item">
            <dt>主数据记录</dt>
            <dd>{state.loading ? '正在读取' : state.error ? '不可用' : <><MonoDigits>{providerCount}</MonoDigits> 个服务商</>}</dd>
          </div>
          <div className="asset-workbench-summary__item">
            <dt>地区证据</dt>
            <dd>{state.loading || state.error ? '—' : <><MonoDigits>{countryCount}</MonoDigits> 个国家 / 地区</>}</dd>
          </div>
          <div className="asset-workbench-summary__item">
            <dt>账号证据覆盖</dt>
            <dd>{state.loading || state.error ? '—' : <>账号提示 <MonoDigits>{accountCount}</MonoDigits> · 标签 <MonoDigits>{labeledCount}</MonoDigits></>}</dd>
          </div>
          <div className="asset-workbench-summary__item">
            <dt>复核线索</dt>
            <dd>{state.loading || state.error ? '—' : <><MonoDigits>{lowRatedCount}</MonoDigits> 个低评分</>}</dd>
          </div>
        </dl>
      </section>

      <section className="page-panel">
        <div className="section-heading">
          <div>
            <p className="section-heading__eyebrow">PROVIDER EVIDENCE TABLE</p>
            <h2>服务商账号证据表</h2>
            <p className="section-heading__description">
              按服务商、账号提示、地区、评分和标签核对资产引用口径；表格是默认扫描路径，行内操作只打开本页编辑抽屉。
            </p>
          </div>
          <span className="section-heading__meta">
            <MonoDigits>{providerCount}</MonoDigits> 个主数据记录
          </span>
        </div>

        {state.loading ? (
          <PageStateView
            kind="loading"
            title="正在加载服务商…"
            surface="empty"
            compact
          />
        ) : state.error ? (
          <PageStateView
            kind="error"
            title="服务商证据读取失败"
            description="服务商主数据暂时不可用，VPS 资产页仍不会从这里自动改写任何 Node provider hint。"
            technicalSummary={state.error}
            action={(
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  setState((current) => ({ ...current, loading: true, error: null }))
                  setReloadKey((current) => current + 1)
                }}
              >
                重新读取服务商
              </Button>
            )}
            surface="empty"
            compact
          />
        ) : (
          <DataTable
            className="asset-table providers-table"
            columns={columns}
            rows={state.providers}
            rowKey={(provider) => provider.provider_id}
            emptyContent={(
              <PageStateView
                kind="empty"
                eyebrow="PROVIDER MASTER DATA"
                title="尚未记录服务商账号证据"
                description="先创建服务商主数据，再在 VPS 表单中作为人工选择的供应商引用；这里不会反向同步观测端字段。"
                action={(
                  <Button variant="secondary" size="sm" onClick={openCreateDrawer}>
                    补第一条服务商证据
                  </Button>
                )}
                surface="empty"
                compact
              />
            )}
          />
        )}
      </section>

      <Drawer
        open={createOpen}
        onClose={closeCreateDrawer}
        title="服务商创建"
        ariaLabel="服务商创建表单"
      >
        <div className="asset-create-drawer providers-drawer">
          <div className="asset-drawer-context">
            <p className="asset-drawer-context__eyebrow">MASTER DATA EVIDENCE</p>
            <p>
              新建服务商只补齐资产台账的供应商与账号证据；创建后可在 VPS 表单中选择，不会修改已有 Node 事实。
            </p>
          </div>
          <form className="asset-create-form" onSubmit={handleCreateSubmit}>
            <fieldset className="asset-create-form__group">
              <legend>基础资料</legend>
              <Input label="服务商名称" value={createForm.name} onChange={(event) => setCreateForm({ ...createForm, name: event.target.value })} />
              <Input label="网站" value={createForm.website} onChange={(event) => setCreateForm({ ...createForm, website: event.target.value })} />
              <Input label="面板地址" value={createForm.panelURL} onChange={(event) => setCreateForm({ ...createForm, panelURL: event.target.value })} />
              <Input label="账号提示" value={createForm.accountHint} onChange={(event) => setCreateForm({ ...createForm, accountHint: event.target.value })} />
              <Input label="国家 / 地区" value={createForm.country} onChange={(event) => setCreateForm({ ...createForm, country: event.target.value })} />
              <Input label="评分" type="number" min="1" max="5" value={createForm.rating} onChange={(event) => setCreateForm({ ...createForm, rating: event.target.value })} />
            </fieldset>
            <fieldset className="asset-create-form__group asset-create-form__group--wide">
              <legend>备注标签</legend>
              <Input label="标签" hint="用逗号分隔" value={createForm.labels} onChange={(event) => setCreateForm({ ...createForm, labels: event.target.value })} />
              <Input name="note" label="备注" value={createForm.note} onChange={(event) => setCreateForm({ ...createForm, note: event.target.value })} />
            </fieldset>
            {createError && (
              <p className="create-form__error" role="alert">
                {createError}
              </p>
            )}
            <div className="page-form-actions asset-create-form__actions">
              <span className="asset-create-form__hint">关闭抽屉会丢弃未提交草稿。</span>
              <Button type="button" variant="secondary" onClick={closeCreateDrawer} disabled={createSubmitting}>
                取消
              </Button>
              <Button type="submit" disabled={createSubmitting}>
                {createSubmitting ? '创建中…' : '创建服务商'}
              </Button>
            </div>
          </form>
        </div>
      </Drawer>

      <Drawer
        open={editingProviderId != null}
        onClose={cancelEdit}
        title="服务商编辑"
        ariaLabel="服务商编辑表单"
      >
        <div className="asset-create-drawer providers-drawer">
          <div className="asset-drawer-context">
            <p className="asset-drawer-context__eyebrow">EDIT MASTER DATA</p>
            <p>
              编辑服务商主数据只影响资产台账展示和后续选择器，不会回写观测端的 provider hint 或其他已关联资产事实。
            </p>
          </div>
          <form className="asset-create-form" onSubmit={handleEditSubmit}>
            <fieldset className="asset-create-form__group">
              <legend>基础资料</legend>
              <Input label="服务商名称" value={editForm.name} onChange={(event) => setEditForm({ ...editForm, name: event.target.value })} />
              <Input label="网站" value={editForm.website} onChange={(event) => setEditForm({ ...editForm, website: event.target.value })} />
              <Input label="面板地址" value={editForm.panelURL} onChange={(event) => setEditForm({ ...editForm, panelURL: event.target.value })} />
              <Input label="账号提示" value={editForm.accountHint} onChange={(event) => setEditForm({ ...editForm, accountHint: event.target.value })} />
              <Input label="国家 / 地区" value={editForm.country} onChange={(event) => setEditForm({ ...editForm, country: event.target.value })} />
              <Input label="评分" type="number" min="1" max="5" value={editForm.rating} onChange={(event) => setEditForm({ ...editForm, rating: event.target.value })} />
            </fieldset>
            <fieldset className="asset-create-form__group asset-create-form__group--wide">
              <legend>备注标签</legend>
              <Input label="标签" hint="用逗号分隔" value={editForm.labels} onChange={(event) => setEditForm({ ...editForm, labels: event.target.value })} />
              <Input name="note" label="备注" value={editForm.note} onChange={(event) => setEditForm({ ...editForm, note: event.target.value })} />
            </fieldset>
            {editError && (
              <p className="create-form__error" role="alert">
                {editError}
              </p>
            )}
            <div className="page-form-actions asset-create-form__actions">
              <span className="asset-create-form__hint">取消会恢复为当前已保存的服务商资料。</span>
              <Button type="button" variant="secondary" onClick={cancelEdit} disabled={editSubmitting}>
                取消编辑
              </Button>
              <Button type="submit" disabled={editSubmitting}>
                {editSubmitting ? '保存中…' : '保存服务商'}
              </Button>
            </div>
          </form>
        </div>
      </Drawer>
    </div>
  )
}
