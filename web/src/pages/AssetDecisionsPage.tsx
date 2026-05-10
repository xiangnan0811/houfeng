import { type FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { AssetDecisionRenewalTable } from '../components/AssetDecisionRenewalTable'
import { AssetDecisionVPSQueueTable } from '../components/AssetDecisionVPSQueueTable'
import {
  AssetDecisionWorkPanel,
  type AssetDecisionDraft,
} from '../components/AssetDecisionWorkPanel'
import { Button, MonoDigits } from '../components/atoms'
import { ApiError, listSubscriptions, listVPSAssets, updateVPSAsset } from '../lib/api'
import {
  type VPSAssetRecord,
  type VPSRenewalDecision,
  type SubscriptionRecord,
} from '../lib/types'
import { renewalLabel } from './assetPageUtils'

type RenewalWindow = 30 | 60 | 90

type PageState = {
  renewalsLoading: boolean
  renewalsError: string | null
  vpsLoading: boolean
  vpsError: string | null
  renewals: SubscriptionRecord[]
  unreviewed: VPSAssetRecord[]
  migrate: VPSAssetRecord[]
  cancel: VPSAssetRecord[]
}

const RENEWAL_WINDOWS: RenewalWindow[] = [30, 60, 90]
const DECISION_QUEUE_VALUES: VPSRenewalDecision[] = ['unreviewed', 'migrate', 'cancel']
const INITIAL_DECISION_DRAFT: AssetDecisionDraft = {
  renewalDecision: 'unreviewed',
  reason: '',
}
const INITIAL_PAGE_STATE: PageState = {
  renewalsLoading: true,
  renewalsError: null,
  vpsLoading: true,
  vpsError: null,
  renewals: [],
  unreviewed: [],
  migrate: [],
  cancel: [],
}

function describeError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function parseRenewalWindow(value: string): RenewalWindow {
  const parsed = Number.parseInt(value, 10)
  return RENEWAL_WINDOWS.includes(parsed as RenewalWindow) ? (parsed as RenewalWindow) : 30
}

function updateDecisionQueues(
  state: PageState,
  updated: VPSAssetRecord,
): Pick<PageState, 'unreviewed' | 'migrate' | 'cancel'> {
  const next = {
    unreviewed: state.unreviewed.filter((vps) => vps.vps_id !== updated.vps_id),
    migrate: state.migrate.filter((vps) => vps.vps_id !== updated.vps_id),
    cancel: state.cancel.filter((vps) => vps.vps_id !== updated.vps_id),
  }

  if (updated.renewal_decision === 'unreviewed') next.unreviewed = [updated, ...next.unreviewed]
  if (updated.renewal_decision === 'migrate') next.migrate = [updated, ...next.migrate]
  if (updated.renewal_decision === 'cancel') next.cancel = [updated, ...next.cancel]

  return next
}

function renewalQueueLabel(value: VPSRenewalDecision): string {
  return DECISION_QUEUE_VALUES.includes(value) ? renewalLabel(value) : '已处理'
}

export function AssetDecisionsPage() {
  const [renewalWindow, setRenewalWindow] = useState<RenewalWindow>(30)
  const [state, setState] = useState<PageState>(INITIAL_PAGE_STATE)
  const [selectedVPS, setSelectedVPS] = useState<VPSAssetRecord | null>(null)
  const [decisionDraft, setDecisionDraft] = useState<AssetDecisionDraft>(INITIAL_DECISION_DRAFT)
  const [decisionSubmitting, setDecisionSubmitting] = useState(false)
  const [decisionError, setDecisionError] = useState<string | null>(null)
  const [decisionNotice, setDecisionNotice] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    listSubscriptions({
      renew_within_days: renewalWindow,
      sort: 'renew_at',
      order: 'asc',
    })
      .then((renewals) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          renewalsLoading: false,
          renewalsError: null,
          renewals,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          renewalsLoading: false,
          renewalsError: describeError(error, '加载续费候选失败'),
          renewals: [],
        }))
      })

    return () => {
      cancelled = true
    }
  }, [renewalWindow])

  useEffect(() => {
    let cancelled = false
    Promise.all([
      listVPSAssets({ renewal_decision: 'unreviewed' }),
      listVPSAssets({ renewal_decision: 'migrate' }),
      listVPSAssets({ renewal_decision: 'cancel' }),
    ])
      .then(([unreviewed, migrate, cancel]) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          vpsLoading: false,
          vpsError: null,
          unreviewed,
          migrate,
          cancel,
        }))
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setState((current) => ({
          ...current,
          vpsLoading: false,
          vpsError: describeError(error, '加载 VPS 决策队列失败'),
          unreviewed: [],
          migrate: [],
          cancel: [],
        }))
      })

    return () => {
      cancelled = true
    }
  }, [])

  function selectVPS(vps: VPSAssetRecord) {
    setSelectedVPS(vps)
    setDecisionDraft({ renewalDecision: vps.renewal_decision, reason: '' })
    setDecisionError(null)
    setDecisionNotice(null)
  }

  function changeRenewalWindow(value: string) {
    setState((current) => ({ ...current, renewalsLoading: true, renewalsError: null }))
    setRenewalWindow(parseRenewalWindow(value))
  }

  function handleDecisionSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedVPS) return
    setDecisionError(null)
    setDecisionNotice(null)

    if (decisionDraft.renewalDecision === selectedVPS.renewal_decision) {
      setDecisionError('请选择一个不同的续费决策')
      return
    }

    const reason = decisionDraft.reason.trim()
    setDecisionSubmitting(true)
    updateVPSAsset(selectedVPS.vps_id, {
      renewal_decision: decisionDraft.renewalDecision,
      ...(reason ? { renewal_reason: reason } : {}),
    })
      .then((updated) => {
        setState((current) => ({
          ...current,
          ...updateDecisionQueues(current, updated),
        }))
        setSelectedVPS(null)
        setDecisionDraft(INITIAL_DECISION_DRAFT)
        setDecisionNotice(`续费决策已保存：${updated.display_name} -> ${renewalQueueLabel(updated.renewal_decision)}`)
      })
      .catch((error: unknown) => {
        setDecisionError(describeError(error, '更新续费决策失败'))
      })
      .finally(() => setDecisionSubmitting(false))
  }

  const totalDecisionQueue = state.unreviewed.length + state.migrate.length + state.cancel.length

  return (
    <div className="page-stack asset-page asset-decisions-page">
      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">ASSET LEDGER</div>
          <h1 className="page-panel__title">资产决策</h1>
          <p className="page-panel__description">
            集中处理续费窗口、未评估 VPS、待迁移和待取消资产。需要查看历史、Node 关联或基础事实时再进入 VPS 详情。
          </p>
        </div>
        <div className="page-panel__actions">
          <Link className="btn btn--secondary btn--md" to="/vps">VPS 列表</Link>
          <Link className="btn btn--secondary btn--md" to="/subscriptions">订阅列表</Link>
        </div>
      </section>

      <dl className="asset-detail-grid" aria-label="资产决策指标">
        <div className="asset-detail-grid__item">
          <dt>{renewalWindow} 天续费</dt>
          <dd><MonoDigits>{state.renewals.length}</MonoDigits> 条订阅</dd>
        </div>
        <div className="asset-detail-grid__item">
          <dt>待决策</dt>
          <dd><MonoDigits>{state.unreviewed.length}</MonoDigits> 台 VPS</dd>
        </div>
        <div className="asset-detail-grid__item">
          <dt>迁移 / 取消</dt>
          <dd><MonoDigits>{state.migrate.length + state.cancel.length}</MonoDigits> 台 VPS</dd>
        </div>
        <div className="asset-detail-grid__item">
          <dt>工作台队列</dt>
          <dd><MonoDigits>{totalDecisionQueue}</MonoDigits> 台 VPS</dd>
        </div>
      </dl>

      <section className="page-panel">
        <div className="section-heading">
          <div>
            <p className="section-heading__eyebrow">RENEWAL WINDOW</p>
            <h2>续费候选</h2>
          </div>
          <label className="asset-decision-window">
            <span>续费窗口</span>
            <select
              className="input"
              value={String(renewalWindow)}
              onChange={(event) => changeRenewalWindow(event.target.value)}
            >
              {RENEWAL_WINDOWS.map((value) => (
                <option key={value} value={value}>未来 {value} 天</option>
              ))}
            </select>
          </label>
        </div>
        <AssetDecisionRenewalTable
          loading={state.renewalsLoading}
          error={state.renewalsError}
          renewals={state.renewals}
          renderActions={(subscription) => (
            <span className="asset-decision-actions">
              <Link className="text-link" to={`/vps/${subscription.vps_id}`}>VPS</Link>
              <Link className="text-link" to={`/subscriptions?renew_within_days=${renewalWindow}`}>订阅</Link>
            </span>
          )}
        />
      </section>

      <div className="asset-decision-layout">
        <div className="asset-decision-queues">
          <AssetDecisionVPSQueueTable
            title="待评估 VPS"
            eyebrow="UNREVIEWED"
            ariaLabel="待评估 VPS 队列"
            loading={state.vpsLoading}
            error={state.vpsError}
            rows={state.unreviewed}
            renderActions={(vps) => renderVPSQueueActions(vps, selectVPS)}
          />
          <AssetDecisionVPSQueueTable
            title="待迁移 VPS"
            eyebrow="MIGRATE"
            ariaLabel="待迁移 VPS 队列"
            loading={state.vpsLoading}
            error={state.vpsError}
            rows={state.migrate}
            renderActions={(vps) => renderVPSQueueActions(vps, selectVPS)}
          />
          <AssetDecisionVPSQueueTable
            title="待取消 VPS"
            eyebrow="CANCEL"
            ariaLabel="待取消 VPS 队列"
            loading={state.vpsLoading}
            error={state.vpsError}
            rows={state.cancel}
            renderActions={(vps) => renderVPSQueueActions(vps, selectVPS)}
          />
        </div>

        <AssetDecisionWorkPanel
          selectedVPS={selectedVPS}
          decisionDraft={decisionDraft}
          submitting={decisionSubmitting}
          error={decisionError}
          notice={decisionNotice}
          onDraftChange={setDecisionDraft}
          onSubmit={handleDecisionSubmit}
          onCancel={() => {
            setSelectedVPS(null)
            setDecisionDraft(INITIAL_DECISION_DRAFT)
            setDecisionError(null)
          }}
        />
      </div>
    </div>
  )
}

function renderVPSQueueActions(
  vps: VPSAssetRecord,
  onSelect: (vps: VPSAssetRecord) => void,
) {
  return (
    <span className="asset-decision-actions">
      <Link className="text-link" to={`/vps/${vps.vps_id}`}>详情</Link>
      <Button
        size="sm"
        variant="secondary"
        aria-label={`处理 ${vps.vps_id}`}
        onClick={() => onSelect(vps)}
      >
        处理
      </Button>
    </span>
  )
}
