import { type FormEvent, useEffect, useState } from 'react'

import { Modal, Input } from '../components/atoms'
import { PageState as PageStateView } from '../components/PageState'
import { ApiError, createProvider, listProviders, updateProvider } from '../lib/api'
import { formatOptional } from '../lib/format'
import type { CreateProviderInput, ProviderRecord } from '../lib/types'
import { parseLabels } from './assetPageUtils'

type PageState = {
  loading: boolean
  error: string | null
  providers: ProviderRecord[]
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

const INITIAL_PAGE_STATE: PageState = { loading: true, error: null, providers: [] }

const INITIAL_FORM: FormState = {
  name: '', website: '', panelURL: '', accountHint: '',
  country: '', rating: '', labels: '', note: '',
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function buildInput(form: FormState): CreateProviderInput {
  if (form.name.trim() === '') throw new Error('服务商名称不能为空。')
  const rating = form.rating.trim() === '' ? null : Number(form.rating.trim())
  if (rating != null && (!Number.isInteger(rating) || rating < 1 || rating > 5))
    throw new Error('评分必须为 1 到 5。')
  return {
    name: form.name.trim(), website: form.website.trim(),
    panel_url: form.panelURL.trim(), account_hint: form.accountHint.trim(),
    country: form.country.trim(), rating, labels: parseLabels(form.labels), note: form.note.trim(),
  }
}

function providerToForm(p: ProviderRecord): FormState {
  return {
    name: p.name, website: p.website, panelURL: p.panel_url,
    accountHint: p.account_hint, country: p.country,
    rating: p.rating == null ? '' : String(p.rating),
    labels: p.labels.join(', '), note: p.note,
  }
}

export function ProvidersPage() {
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [reloadKey, setReloadKey] = useState(0)
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
    listProviders()
      .then((providers) => { if (!cancelled) setState({ loading: false, error: null, providers }) })
      .catch((err: unknown) => { if (!cancelled) setState({ loading: false, error: describeError(err, '加载服务商失败'), providers: [] }) })
    return () => { cancelled = true }
  }, [reloadKey])

  function openCreate() { setCreateOpen(true); setCreateForm(INITIAL_FORM); setCreateError(null); cancelEdit() }
  function closeCreate() { setCreateOpen(false); setCreateForm(INITIAL_FORM); setCreateError(null) }

  function handleCreate(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); setCreateError(null)
    let input: CreateProviderInput
    try { input = buildInput(createForm) } catch (err: unknown) { setCreateError(describeError(err, '输入无效')); return }
    setCreateSubmitting(true)
    createProvider(input)
      .then((p) => { setState((s) => ({ loading: false, error: null, providers: [p, ...s.providers.filter((x) => x.provider_id !== p.provider_id)] })); closeCreate() })
      .catch((err: unknown) => setCreateError(describeError(err, '创建失败')))
      .finally(() => setCreateSubmitting(false))
  }

  function startEdit(p: ProviderRecord) { closeCreate(); setEditingId(p.provider_id); setEditForm(providerToForm(p)); setEditError(null) }
  function cancelEdit() { setEditingId(null); setEditForm(INITIAL_FORM); setEditError(null) }

  function handleEdit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); if (!editingId) return; setEditError(null)
    let input: CreateProviderInput
    try { input = buildInput(editForm) } catch (err: unknown) { setEditError(describeError(err, '输入无效')); return }
    setEditSubmitting(true)
    updateProvider(editingId, input)
      .then((p) => { setState((s) => ({ loading: false, error: null, providers: s.providers.map((x) => x.provider_id === p.provider_id ? p : x) })); cancelEdit() })
      .catch((err: unknown) => setEditError(describeError(err, '更新失败')))
      .finally(() => setEditSubmitting(false))
  }

  return (
    <div className="page-stack">
      <div className="page-header">
        <div>
          <h1 className="page-title">服务商</h1>
          <p className="page-sub">VPS 供应商汇总</p>
        </div>
        <div className="header-actions">
          <button className="btn md primary" onClick={openCreate}>
            <svg viewBox="0 0 16 16"><path d="M8 2v12M2 8h12" /></svg>
            新建服务商
          </button>
        </div>
      </div>

      {state.loading ? (
        <PageStateView kind="loading" title="正在加载…" surface="empty" compact />
      ) : state.error ? (
        <PageStateView
          kind="error"
          title="加载失败"
          description={state.error}
          action={<button className="btn sm secondary" onClick={() => { setState(INITIAL_PAGE_STATE); setReloadKey((k) => k + 1) }}>重试</button>}
          surface="empty"
          compact
        />
      ) : state.providers.length === 0 ? (
        <PageStateView
          kind="empty"
          title="尚未记录服务商"
          description="创建服务商后可在 VPS 表单中引用"
          action={<button className="btn sm primary" onClick={openCreate}>创建第一个服务商</button>}
          surface="empty"
          compact
        />
      ) : (
        <table className="table animate-in">
          <thead>
            <tr>
              <th>服务商</th>
              <th>国家</th>
              <th>评分</th>
              <th>标签</th>
              <th>更新时间</th>
              <th className="cell-end">操作</th>
            </tr>
          </thead>
          <tbody>
            {state.providers.map((p) => (
              <tr key={p.provider_id}>
                <td className="name">{p.name}</td>
                <td className="sub">{formatOptional(p.country)}</td>
                <td className="mono">{p.rating != null ? `${p.rating}/5` : '—'}</td>
                <td className="sub">{p.labels.length > 0 ? p.labels.join(', ') : '—'}</td>
                <td className="time">{new Date(p.updated_at).toLocaleDateString()}</td>
                <td className="cell-end"><button className="btn-text sm secondary" onClick={() => startEdit(p)}>编辑</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <Modal open={createOpen} onClose={closeCreate} title="新建服务商" ariaLabel="新建服务商表单">
        <form className="drawer-form" onSubmit={handleCreate}>
          <Input label="服务商名称" value={createForm.name} onChange={(e) => setCreateForm({ ...createForm, name: e.target.value })} />
          <Input label="网站" value={createForm.website} onChange={(e) => setCreateForm({ ...createForm, website: e.target.value })} />
          <Input label="面板地址" value={createForm.panelURL} onChange={(e) => setCreateForm({ ...createForm, panelURL: e.target.value })} />
          <Input label="账号提示" value={createForm.accountHint} onChange={(e) => setCreateForm({ ...createForm, accountHint: e.target.value })} />
          <Input label="国家 / 地区" value={createForm.country} onChange={(e) => setCreateForm({ ...createForm, country: e.target.value })} />
          <Input label="评分 (1-5)" type="number" min="1" max="5" value={createForm.rating} onChange={(e) => setCreateForm({ ...createForm, rating: e.target.value })} />
          <Input label="标签" hint="逗号分隔" value={createForm.labels} onChange={(e) => setCreateForm({ ...createForm, labels: e.target.value })} />
          <Input label="备注" value={createForm.note} onChange={(e) => setCreateForm({ ...createForm, note: e.target.value })} />
          {createError && <p className="create-form__error" role="alert">{createError}</p>}
          <div className="page-form-actions">
            <button type="button" className="btn md secondary" onClick={closeCreate} disabled={createSubmitting}>取消</button>
            <button type="submit" className="btn md primary" disabled={createSubmitting}>{createSubmitting ? '创建中…' : '创建'}</button>
          </div>
        </form>
      </Modal>

      <Modal open={editingId != null} onClose={cancelEdit} title="编辑服务商" ariaLabel="编辑服务商表单">
        <form className="drawer-form" onSubmit={handleEdit}>
          <Input label="服务商名称" value={editForm.name} onChange={(e) => setEditForm({ ...editForm, name: e.target.value })} />
          <Input label="网站" value={editForm.website} onChange={(e) => setEditForm({ ...editForm, website: e.target.value })} />
          <Input label="面板地址" value={editForm.panelURL} onChange={(e) => setEditForm({ ...editForm, panelURL: e.target.value })} />
          <Input label="账号提示" value={editForm.accountHint} onChange={(e) => setEditForm({ ...editForm, accountHint: e.target.value })} />
          <Input label="国家 / 地区" value={editForm.country} onChange={(e) => setEditForm({ ...editForm, country: e.target.value })} />
          <Input label="评分 (1-5)" type="number" min="1" max="5" value={editForm.rating} onChange={(e) => setEditForm({ ...editForm, rating: e.target.value })} />
          <Input label="标签" hint="逗号分隔" value={editForm.labels} onChange={(e) => setEditForm({ ...editForm, labels: e.target.value })} />
          <Input label="备注" value={editForm.note} onChange={(e) => setEditForm({ ...editForm, note: e.target.value })} />
          {editError && <p className="create-form__error" role="alert">{editError}</p>}
          <div className="page-form-actions">
            <button type="button" className="btn md secondary" onClick={cancelEdit} disabled={editSubmitting}>取消</button>
            <button type="submit" className="btn md primary" disabled={editSubmitting}>{editSubmitting ? '保存中…' : '保存'}</button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
