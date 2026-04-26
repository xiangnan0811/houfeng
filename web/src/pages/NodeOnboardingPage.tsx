import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { DetailSection } from '../components/DetailSection'
import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  confirmNodeRebind,
  getNodeOnboarding,
  rejectPendingNodeBinding,
  resetNodeBinding,
} from '../lib/api'
import { formatDateTime, formatLabelList } from '../lib/format'
import { clearOnboardingTokenCache, getOnboardingTokenCache } from '../lib/onboardingTokenCache'
import type { NodeOnboardingState } from '../lib/types'

type State = {
  requestedNodeId: string | null
  onboarding: NodeOnboardingState | null
  error: string | null
}

type ConflictAction = 'confirm' | 'reject' | 'reset'

type ConflictState = {
  action: ConflictAction | null
  error: string | null
}

type NodeOnboardingFingerprintFields = NodeOnboardingState & {
  current_binding_fingerprint?: string
  current_binding_fingerprint_summary?: string
}

function describeError(error: unknown, fallback: string) {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

function maskFingerprint(value?: string | null) {
  if (!value) return '尚无'
  const normalized = value.trim()
  if (!normalized) return '尚无'
  if (normalized.length <= 14) return normalized
  return `${normalized.slice(0, 8)}…${normalized.slice(-6)}`
}

function currentFingerprintSummary(onboarding: NodeOnboardingState) {
  const state = onboarding as NodeOnboardingFingerprintFields
  if (state.current_binding_fingerprint_summary?.trim()) {
    return state.current_binding_fingerprint_summary.trim()
  }
  if (state.current_binding_fingerprint?.trim()) {
    return maskFingerprint(state.current_binding_fingerprint)
  }
  return '服务端当前未提供已绑定指纹摘要'
}

function describePhase(state: NodeOnboardingState) {
  switch (state.phase) {
    case '未开始接入':
      return {
        title: '等待首次接入',
        description: '当前节点尚未与 agent 建立绑定。请完成安装，并等待首次同步进入系统。',
      }
    case '已绑定，等待稳定观测':
      return {
        title: '已完成指纹绑定，等待稳定观测',
        description: '绑定已经建立，系统正在等待首批 HostSample 或 accepted observation 到达。',
      }
    case '接入完成':
      return {
        title: '接入已完成，可以转入日常观察。',
        description: '该节点已经完成绑定，并已进入稳定观测路径。',
      }
    case '绑定冲突待处理':
      return {
        title: '检测到新的指纹接入请求',
        description: '请优先处理上方的绑定冲突卡片，再继续观察后续接入状态。',
      }
    default:
      return {
        title: state.phase,
        description: '当前阶段的处理动作将在后续任务接入，先以现有状态为准。',
      }
  }
}

export function NodeOnboardingPage() {
  const { nodeId } = useParams()
  const [state, setState] = useState<State>({
    requestedNodeId: null,
    onboarding: null,
    error: null,
  })
  const [conflictState, setConflictState] = useState<ConflictState>({
    action: null,
    error: null,
  })

  useEffect(() => {
    let cancelled = false
    if (!nodeId) return

    getNodeOnboarding(nodeId)
      .then((onboarding) => {
        if (cancelled) return
        setConflictState({
          action: null,
          error: null,
        })
        setState({
          requestedNodeId: nodeId,
          onboarding,
          error: null,
        })
      })
      .catch((error: unknown) => {
        if (cancelled) return
        setConflictState({
          action: null,
          error: null,
        })
        setState({
          requestedNodeId: nodeId,
          onboarding: null,
          error:
            error instanceof ApiError && error.status === 404
              ? '节点不存在'
              : describeError(error, '加载节点接入状态失败'),
        })
      })

    return () => {
      cancelled = true
    }
  }, [nodeId])

  const onboarding = state.requestedNodeId === nodeId ? state.onboarding : null
  const error = state.requestedNodeId === nodeId ? state.error : null
  const cachedTokenIssue = nodeId ? getOnboardingTokenCache(nodeId) : null
  const tokenIssue =
    cachedTokenIssue &&
    (!onboarding?.enrollment_token_issued_at ||
      onboarding.enrollment_token_issued_at === cachedTokenIssue.issued_at)
      ? cachedTokenIssue
      : null

  useEffect(() => {
    if (!nodeId || !cachedTokenIssue || !onboarding?.enrollment_token_issued_at) {
      return
    }

    if (onboarding.enrollment_token_issued_at !== cachedTokenIssue.issued_at) {
      clearOnboardingTokenCache(nodeId)
    }
  }, [cachedTokenIssue, nodeId, onboarding?.enrollment_token_issued_at])

  if (nodeId && state.requestedNodeId !== nodeId) {
    return <section className="page-panel">正在加载节点接入状态…</section>
  }

  if (!nodeId || error || !onboarding) {
    return (
      <section className="page-panel">
        <p className="page-panel__eyebrow">Node Onboarding</p>
        <h2 className="page-panel__title">节点接入工作台不可用</h2>
        <p className="page-panel__description">{error ?? '未找到节点'}</p>
        <Link className="text-link" to="/nodes">
          返回节点列表
        </Link>
      </section>
    )
  }

  const phase = describePhase(onboarding)
  const pendingBinding = onboarding.pending_binding
  const showBindingConflict = onboarding.binding_status === '指纹变更待确认'

  async function handleBindingAction(
    action: ConflictAction,
    request: (targetNodeId: string) => Promise<NodeOnboardingState>,
  ) {
    if (!nodeId) return

    setConflictState({
      action,
      error: null,
    })

    try {
      await request(nodeId)
      const refreshed = await getNodeOnboarding(nodeId)
      setState({
        requestedNodeId: nodeId,
        onboarding: refreshed,
        error: null,
      })
      setConflictState({
        action: null,
        error: null,
      })
    } catch (error: unknown) {
      setConflictState({
        action: null,
        error: describeError(error, '更新绑定冲突状态失败'),
      })
    }
  }

  return (
    <div className="page-stack">
      <section className="hero-panel">
        <div className="hero-panel__content">
          <p className="hero-panel__eyebrow">Node Onboarding</p>
          <h2 className="hero-panel__title">{onboarding.display_name}</h2>
          <p className="hero-panel__description">
            {onboarding.region} · {onboarding.city} · {onboarding.provider}
          </p>
          <div className="badge-row">
            <StatusBadge label={onboarding.lifecycle_status} />
            <StatusBadge label={onboarding.monitoring_status} />
            <StatusBadge label={onboarding.binding_status} />
            <StatusBadge label={onboarding.phase} />
          </div>
        </div>
        <div className="hero-panel__meta">
          <div className="hero-meta-card">
            <span>标签</span>
            <strong>{formatLabelList(onboarding.labels)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近心跳</span>
            <strong>{formatDateTime(onboarding.last_heartbeat_at)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>最近同步</span>
            <strong>{formatDateTime(onboarding.last_sync_at)}</strong>
          </div>
          <div className="hero-meta-card">
            <span>当前主问题</span>
            <strong>{onboarding.current_primary_issue_summary || '暂无明显异常'}</strong>
          </div>
        </div>
      </section>

      <div className="summary-grid">
        <article className="summary-card">
          <p className="summary-card__label">当前阶段</p>
          <p className="summary-card__value summary-card__value--text">{onboarding.phase}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">首批样本</p>
          <p className="summary-card__value">{onboarding.has_host_sample ? '已到达' : '未到达'}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">accepted observation</p>
          <p className="summary-card__value">{onboarding.has_accepted_observation ? '已接收' : '未接收'}</p>
        </article>
      </div>

      {showBindingConflict ? (
        <DetailSection eyebrow="Binding Conflict" title="绑定冲突处置" aside="高优先级">
          <article className="metric-card" aria-label="高优先级：绑定冲突待处理">
            <h3>高优先级：绑定冲突待处理</h3>
            <p>系统检测到新的指纹接入请求，请先确认是否接受新的绑定关系。</p>
            <dl>
              <div>
                <dt>当前已绑定指纹</dt>
                <dd>{currentFingerprintSummary(onboarding)}</dd>
              </div>
              <div>
                <dt>待确认指纹</dt>
                <dd>{maskFingerprint(pendingBinding?.fingerprint)}</dd>
              </div>
              <div>
                <dt>首次出现</dt>
                <dd>{formatDateTime(pendingBinding?.first_seen_at)}</dd>
              </div>
              <div>
                <dt>最近出现</dt>
                <dd>{formatDateTime(pendingBinding?.last_seen_at)}</dd>
              </div>
              <div>
                <dt>尝试次数</dt>
                <dd>{pendingBinding?.attempt_count ?? 0}</dd>
              </div>
            </dl>
            {conflictState.error ? <p role="alert">{conflictState.error}</p> : null}
            <div className="badge-row">
              <button
                type="button"
                disabled={conflictState.action !== null}
                onClick={() => handleBindingAction('confirm', confirmNodeRebind)}
              >
                confirm rebind
              </button>
              <button
                type="button"
                disabled={conflictState.action !== null}
                onClick={() => handleBindingAction('reject', rejectPendingNodeBinding)}
              >
                reject fingerprint
              </button>
              <button
                type="button"
                disabled={conflictState.action !== null}
                onClick={() => handleBindingAction('reset', resetNodeBinding)}
              >
                reset binding
              </button>
            </div>
          </article>
        </DetailSection>
      ) : null}

      <DetailSection eyebrow="Enrollment Token" title="接入凭证" aside={tokenIssue ? `最近生成：${formatDateTime(tokenIssue.issued_at)}` : onboarding.enrollment_token_issued_at ? `上次签发：${formatDateTime(onboarding.enrollment_token_issued_at)}` : '尚未签发'}>
        {tokenIssue ? (
          <article className="metric-card">
            <h3>当前会话 Token</h3>
            <p>{tokenIssue.token}</p>
            <p>请在本次会话内完成安装或妥善保存，离开后系统不会重新展示明文。</p>
          </article>
        ) : (
          <div className="empty-state">
            <h3>当前会话里没有可显示的 Token 明文。</h3>
            <p>请重新生成接入 Token，再继续安装或核对配置。</p>
          </div>
        )}
      </DetailSection>

      <DetailSection eyebrow="Install Steps" title="接入步骤">
        <article className="metric-card">
          <h3>建议顺序</h3>
          <ol>
            <li>在服务器上安装 agent</li>
            <li>写入该节点专属 token</li>
            <li>启动 systemd 服务</li>
            <li>等待首次同步与绑定完成</li>
          </ol>
        </article>
      </DetailSection>

      <DetailSection eyebrow="Current Status" title="状态反馈">
        <div className="empty-state">
          <h3>{phase.title}</h3>
          <p>{phase.description}</p>
          <p>首批样本：{onboarding.has_host_sample ? '已到达' : '未到达'}</p>
          <p>
            accepted observation：
            {onboarding.has_accepted_observation ? '已接收' : '未接收'}
          </p>
          {onboarding.phase === '接入完成' ? (
            <Link className="text-link" to={`/nodes/${onboarding.node_id}`}>
              查看节点详情
            </Link>
          ) : null}
        </div>
      </DetailSection>
    </div>
  )
}
