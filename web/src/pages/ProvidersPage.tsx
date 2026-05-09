import { type FormEvent, useEffect, useState } from 'react'

import { Button, DataTable, Input, MonoDigits, Timestamp, type DataTableColumn } from '../components/atoms'
import { ApiError, createProvider, listProviders } from '../lib/api'
import { formatOptional } from '../lib/format'
import type { CreateProviderInput, ProviderRecord } from '../lib/types'
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
  const rating = form.rating.trim() === '' ? null : Number.parseInt(form.rating.trim(), 10)
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

export function ProvidersPage() {
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [createOpen, setCreateOpen] = useState(false)
  const [createForm, setCreateForm] = useState<CreateProviderFormState>(INITIAL_CREATE_FORM)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

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
  }, [])

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
        setCreateForm(INITIAL_CREATE_FORM)
        setCreateOpen(false)
      })
      .catch((error: unknown) => {
        setCreateError(describeError(error, '创建服务商失败'))
      })
      .finally(() => setCreateSubmitting(false))
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
  ]

  return (
    <div className="page-stack asset-page providers-page">
      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">ASSET LEDGER</div>
          <h1 className="page-panel__title">服务商</h1>
          <p className="page-panel__description">
            管理 VPS 资产层的服务商主数据。这里记录面板入口、账号提示、国家和标签，不会同步修改 Node 的 provider hint。
          </p>
        </div>
        <div className="page-panel__actions">
          <Button variant={createOpen ? 'secondary' : 'primary'} onClick={toggleCreatePanel}>
            {createOpen ? '收起创建' : state.providers.length === 0 ? '创建第一个服务商' : '新建服务商'}
          </Button>
        </div>
      </section>

      {createOpen && (
        <section className="page-panel">
          <div className="page-panel__eyebrow">CREATE</div>
          <h2 className="page-panel__title">服务商创建</h2>
          <form onSubmit={handleCreateSubmit}>
            <Input label="服务商名称" value={createForm.name} onChange={(event) => setCreateForm({ ...createForm, name: event.target.value })} />
            <Input label="网站" value={createForm.website} onChange={(event) => setCreateForm({ ...createForm, website: event.target.value })} />
            <Input label="面板地址" value={createForm.panelURL} onChange={(event) => setCreateForm({ ...createForm, panelURL: event.target.value })} />
            <Input label="账号提示" value={createForm.accountHint} onChange={(event) => setCreateForm({ ...createForm, accountHint: event.target.value })} />
            <Input label="国家 / 地区" value={createForm.country} onChange={(event) => setCreateForm({ ...createForm, country: event.target.value })} />
            <Input label="评分" type="number" min="1" max="5" value={createForm.rating} onChange={(event) => setCreateForm({ ...createForm, rating: event.target.value })} />
            <Input label="标签" hint="用逗号分隔" value={createForm.labels} onChange={(event) => setCreateForm({ ...createForm, labels: event.target.value })} />
            <Input label="备注" value={createForm.note} onChange={(event) => setCreateForm({ ...createForm, note: event.target.value })} />
            {createError && (
              <p className="create-form__error" role="alert">
                {createError}
              </p>
            )}
            <div className="page-form-actions">
              <Button type="submit" disabled={createSubmitting}>
                {createSubmitting ? '创建中…' : '创建服务商'}
              </Button>
            </div>
          </form>
        </section>
      )}

      <section className="page-panel">
        <div className="section-heading">
          <div>
            <p className="section-heading__eyebrow">PROVIDERS</p>
            <h2>服务商列表</h2>
          </div>
          <span className="section-heading__meta">
            <MonoDigits>{state.providers.length}</MonoDigits> 个服务商
          </span>
        </div>

        {state.loading ? (
          <div className="empty-state">正在加载服务商…</div>
        ) : state.error ? (
          <div className="empty-state">{state.error}</div>
        ) : (
          <DataTable
            className="asset-table providers-table"
            columns={columns}
            rows={state.providers}
            rowKey={(provider) => provider.provider_id}
            emptyContent={<span className="empty-inline">暂无服务商</span>}
          />
        )}
      </section>
    </div>
  )
}
