import { type FormEvent, useState } from 'react'

import { Button, Modal } from './atoms'
import { createProvider, createVPSAsset } from '../lib/api'
import {
  type CreateVPSAssetInput,
  type ProviderRecord,
  type VPSAssetRecord,
} from '../lib/types'
import type { FactEditFormState } from '../pages/vps-detail/types'
import { VPSFactsEditForm } from '../pages/vps-detail/VPSFactsEditForm'
import { buildFactEditInput } from '../pages/vps-detail/vpsDetailHelpers'

function describeError(error: unknown, fallback: string): string {
  if (error instanceof Error) return error.message
  if (typeof error === 'string') return error
  return fallback
}

interface VPSCreateModalProps {
  open: boolean
  onClose: () => void
  providers: ProviderRecord[]
  providersLoading?: boolean
  providersError?: string | null
  onCreated: (vps: VPSAssetRecord) => void
  onProviderCreated: (provider: ProviderRecord) => void
}

const INITIAL_FORM: FactEditFormState = {
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
  usageStatus: 'unknown',
  importance: 'normal',
  labels: '',
  note: '',
}

function buildCreateInput(form: FactEditFormState): CreateVPSAssetInput {
  return {
    ...buildFactEditInput(form),
    lifecycle_status: 'active',
    renewal_decision: 'unreviewed',
  }
}

export function VPSCreateModal({
  open,
  onClose,
  providers,
  providersLoading = false,
  providersError = null,
  onCreated,
  onProviderCreated,
}: VPSCreateModalProps) {
  const [form, setForm] = useState<FactEditFormState>(INITIAL_FORM)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [showProviderCreate, setShowProviderCreate] = useState(false)
  const [newProviderName, setNewProviderName] = useState('')
  const [newProviderWebsite, setNewProviderWebsite] = useState('')
  const [providerCreating, setProviderCreating] = useState(false)
  const [providerError, setProviderError] = useState<string | null>(null)

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

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    let input: CreateVPSAssetInput
    try {
      input = buildCreateInput(form)
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
      .then((provider) => {
        onProviderCreated(provider)
        setForm((current) => ({
          ...current,
          providerID: provider.provider_id,
          providerName: provider.name,
        }))
        setShowProviderCreate(false)
        setNewProviderName('')
        setNewProviderWebsite('')
      })
      .catch((err: unknown) => setProviderError(describeError(err, '创建服务商失败')))
      .finally(() => setProviderCreating(false))
  }

  const actions = (
    <>
      <span className="modal-footer__hint">创建后进入详情页。</span>
      <Button type="button" variant="secondary" onClick={handleClose}>取消</Button>
      <Button type="submit" form="vps-create-form" disabled={submitting}>
        {submitting ? '创建中…' : '创建 VPS'}
      </Button>
    </>
  )
  const footer = error ? (
    <div className="vps-dialog-feedback">
      <p className="vps-form-error" role="alert">{error}</p>
      <div className="modal__actions">{actions}</div>
    </div>
  ) : actions

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title="添加 VPS"
      persistent
      contentClassName="vps-dialog vps-dialog--facts"
      footer={footer}
    >
      <VPSFactsEditForm
        formId="vps-create-form"
        draft={form}
        providers={providers}
        providersLoading={providersLoading}
        providersError={providersError}
        submitting={submitting}
        onDraftChange={(next) => {
          setForm(next)
          setError(null)
        }}
        onSubmit={handleSubmit}
        providerExtras={showProviderCreate ? (
          <div className="vps-facts-form__provider-create">
            <div className="row-2">
              <label className="field">
                <span className="field__label">服务商名称</span>
                <input
                  className="input"
                  value={newProviderName}
                  disabled={providerCreating || submitting}
                  onChange={(event) => setNewProviderName(event.target.value)}
                />
              </label>
              <label className="field">
                <span className="field__label">网站</span>
                <input
                  className="input"
                  value={newProviderWebsite}
                  disabled={providerCreating || submitting}
                  onChange={(event) => setNewProviderWebsite(event.target.value)}
                />
              </label>
            </div>
            {providerError ? <p className="create-form__error" role="alert">{providerError}</p> : null}
            <div className="vps-facts-form__provider-create-actions">
              <Button
                type="button"
                variant="secondary"
                onClick={() => { setShowProviderCreate(false); setProviderError(null) }}
              >
                取消
              </Button>
              <Button type="button" disabled={providerCreating || submitting} onClick={handleProviderCreate}>
                {providerCreating ? '创建中…' : '创建'}
              </Button>
            </div>
          </div>
        ) : (
          <button
            type="button"
            className="text-link vps-facts-form__provider-create-link"
            disabled={submitting}
            onClick={() => setShowProviderCreate(true)}
          >
            新建服务商
          </button>
        )}
      />
    </Modal>
  )
}
