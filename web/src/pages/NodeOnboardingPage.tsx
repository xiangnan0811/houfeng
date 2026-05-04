import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import {
  Card,
  Hostname,
  MonoDigits,
  Stepper,
  Timestamp,
  type StepperStep,
} from '../components/atoms'
import { ActionConfirmationCard } from '../components/ActionConfirmationCard'
import { DetailSection } from '../components/DetailSection'
import { StatusBadge } from '../components/StatusBadge'
import {
  ApiError,
  confirmNodeRebind,
  getNodeOnboarding,
  issueNodeEnrollmentToken,
  rejectPendingNodeBinding,
  resetNodeBinding,
} from '../lib/api'
import { formatLabelList } from '../lib/format'
import {
  clearOnboardingTokenCache,
  getOnboardingTokenCache,
  setOnboardingTokenCache,
} from '../lib/onboardingTokenCache'
import { useCopyToClipboard } from '../lib/useCopyToClipboard'
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

type TokenState = {
  action: 'issue' | null
  error: string | null
}

const TOKEN_PLACEHOLDER = '###TOKEN###'

type ConflictActionCopy = {
  title: string
  current: string
  result: string
  impact: string
  unchanged: string
  confirmLabel: string
}

const CONFLICT_ACTION_COPY: Record<ConflictAction, ConflictActionCopy> = {
  confirm: {
    title: '确认重新绑定到新指纹',
    current: '已绑定指纹与新指纹不一致，存在待确认的指纹尝试。',
    result: '操作后：以新指纹建立绑定，原指纹立即失效。',
    impact:
      '后续 sync 将以新指纹为准；之前以旧指纹接入的 agent 将被拒绝（如系误判，请检查 agent 部署）。',
    unchanged: '不会改变节点 ID、历史观测数据或 enrollment token。',
    confirmLabel: '确认重新绑定',
  },
  reject: {
    title: '拒绝该指纹接入',
    current: '有未确认的新指纹正在尝试以该节点接入。',
    result: '操作后：保留当前已绑定指纹，新指纹的接入请求将继续被拒绝。',
    impact: '使用新指纹的 agent 将持续无法 sync；如系误操作，请前往该机器检查 agent 部署。',
    unchanged: '不会改变当前已绑定指纹、节点状态或 enrollment token。',
    confirmLabel: '确认拒绝该指纹',
  },
  reset: {
    title: '重置绑定关系',
    current: '已绑定指纹存在但需要彻底解绑（例如该机器已彻底替换或回收）。',
    result: '操作后：节点回到未绑定状态，等待新的 agent 凭 token 接入。',
    impact:
      '需要重新生成 enrollment token 并下发给目标机器；此前的 agent 将无法继续 sync。',
    unchanged: '不会删除节点 ID 与历史观测数据。',
    confirmLabel: '确认重置绑定',
  },
}

const CONFLICT_ACTION_REQUESTS: Record<
  ConflictAction,
  (targetNodeId: string) => Promise<NodeOnboardingState>
> = {
  confirm: confirmNodeRebind,
  reject: rejectPendingNodeBinding,
  reset: resetNodeBinding,
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
  if (onboarding.current_binding_fingerprint_summary?.trim()) {
    return onboarding.current_binding_fingerprint_summary.trim()
  }
  return '服务端当前未提供已绑定指纹摘要'
}

const PHASE_STEP_LABELS = ['未开始接入', '等待绑定', '等待稳定观测', '接入完成'] as const

function derivePhaseSteps(onboarding: NodeOnboardingState): StepperStep[] {
  const labels = PHASE_STEP_LABELS
  const binding = onboarding.binding_status
  const accepted = onboarding.has_accepted_observation

  // 异常分支：绑定冲突 → 第 2 步标 error
  if (binding === '指纹变更待确认') {
    return [
      { label: labels[0], state: 'done' },
      { label: labels[1], state: 'error' },
      { label: labels[2], state: 'pending' },
      { label: labels[3], state: 'pending' },
    ]
  }

  // 主路径：未绑定 → 第 1 步 current
  if (binding === '未绑定') {
    return [
      { label: labels[0], state: 'current' },
      { label: labels[1], state: 'pending' },
      { label: labels[2], state: 'pending' },
      { label: labels[3], state: 'pending' },
    ]
  }

  // binding === '已绑定'：根据 has_accepted_observation 判断停在第 3 步还是全部完成
  if (!accepted) {
    return [
      { label: labels[0], state: 'done' },
      { label: labels[1], state: 'done' },
      { label: labels[2], state: 'current' },
      { label: labels[3], state: 'pending' },
    ]
  }

  // 已绑定 + 已接收观测 = 接入完成
  return [
    { label: labels[0], state: 'done' },
    { label: labels[1], state: 'done' },
    { label: labels[2], state: 'done' },
    { label: labels[3], state: 'done' },
  ]
}

/**
 * Inline copy button used inside the token block and install snippets.
 * Not promoted to an atom: only this page composes it (see PRD §Technical Approach).
 */
function CopyButton({
  value,
  label = '复制',
  size = 'sm',
  ariaLabel,
}: {
  value: string
  label?: string
  size?: 'sm' | 'md'
  ariaLabel?: string
}) {
  const { copy, copied } = useCopyToClipboard()
  return (
    <button
      type="button"
      className={`btn btn--ghost btn--${size}`}
      onClick={() => {
        void copy(value)
      }}
      disabled={!value}
      aria-label={ariaLabel ?? label}
    >
      {copied ? '已复制 ✓' : label}
    </button>
  )
}

export function NodeOnboardingPage() {
  const { nodeId } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const [state, setState] = useState<State>({
    requestedNodeId: null,
    onboarding: null,
    error: null,
  })
  const [conflictState, setConflictState] = useState<ConflictState>({
    action: null,
    error: null,
  })
  const [pendingConflictChoice, setPendingConflictChoice] = useState<ConflictAction | null>(null)
  const [tokenState, setTokenState] = useState<TokenState>({
    action: null,
    error: null,
  })
  const [tokenCollapsed, setTokenCollapsed] = useState(false)

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
        setPendingConflictChoice(null)
        setTokenState({
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
        setPendingConflictChoice(null)
        setTokenState({
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
  const locationTokenIssueError =
    typeof (location.state as { tokenIssueError?: string } | null)?.tokenIssueError === 'string'
      ? (location.state as { tokenIssueError?: string }).tokenIssueError
      : null
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
        <p className="page-panel__eyebrow">节点接入</p>
        <h2 className="page-panel__title">节点接入工作台不可用</h2>
        <p className="page-panel__description">{error ?? '未找到节点'}</p>
        <Link className="text-link" to="/nodes">
          返回节点列表
        </Link>
      </section>
    )
  }

  const pendingBinding = onboarding.pending_binding
  const showBindingConflict = onboarding.binding_status === '指纹变更待确认'
  const tokenErrorMessage = tokenState.error ?? locationTokenIssueError ?? null

  // Install snippet template values (frontend-derived; backend currently does
  // not provide a public server URL — see PRD ADR-lite).
  const serverUrl = typeof window !== 'undefined' ? window.location.origin : ''
  const tokenForTemplate =
    tokenIssue && !tokenCollapsed ? tokenIssue.token : TOKEN_PLACEHOLDER
  const tokenAvailableForTemplate = Boolean(tokenIssue && !tokenCollapsed)
  const envSnippet = `HOUFENG_AGENT_SERVER_URL=${serverUrl}\nHOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token`
  const tokenWriteSnippet = `echo '${tokenForTemplate}' > /etc/houfeng-agent/token`
  const startSnippet = 'systemctl enable --now houfeng-agent'

  function applyOnboardingState(targetNodeId: string, nextOnboarding: NodeOnboardingState) {
    setState({
      requestedNodeId: targetNodeId,
      onboarding: nextOnboarding,
      error: null,
    })
  }

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
      const nextOnboarding = await request(nodeId)
      applyOnboardingState(nodeId, nextOnboarding)
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

  async function handleIssueEnrollmentToken() {
    if (!nodeId) return

    setTokenState({
      action: 'issue',
      error: null,
    })

    try {
      const issue = await issueNodeEnrollmentToken(nodeId)
      setOnboardingTokenCache(nodeId, issue)
      // A freshly minted token should always be visible (un-collapse on success).
      setTokenCollapsed(false)

      try {
        const refreshed = await getNodeOnboarding(nodeId)
        applyOnboardingState(nodeId, refreshed)
        navigate(`/nodes/${nodeId}/onboarding`, { replace: true })
        setTokenState({
          action: null,
          error: null,
        })
      } catch (error: unknown) {
        setState((current) => {
          if (current.requestedNodeId !== nodeId || !current.onboarding) {
            return current
          }

          return {
            ...current,
            onboarding: {
              ...current.onboarding,
              enrollment_token_issued_at: issue.issued_at,
            },
          }
        })
        navigate(`/nodes/${nodeId}/onboarding`, { replace: true })
        setTokenState({
          action: null,
          error: describeError(error, '已重新生成 Token，但刷新接入状态失败'),
        })
      }
    } catch (error: unknown) {
      setTokenState({
        action: null,
        error: describeError(error, '重新生成接入 Token 失败'),
      })
    }
  }

  const tokenAside = tokenIssue ? (
    <Timestamp value={tokenIssue.issued_at} mode="both" />
  ) : onboarding.enrollment_token_issued_at ? (
    <span>
      上次签发：<Timestamp value={onboarding.enrollment_token_issued_at} mode="both" />
    </span>
  ) : (
    <span>尚未签发</span>
  )

  return (
    <div className="page-stack">
      <section className="hero-panel">
        <div className="hero-panel__content">
          <p className="hero-panel__eyebrow">节点接入</p>
          <h2 className="hero-panel__title">{onboarding.display_name}</h2>
          <p className="hero-panel__description">
            {onboarding.region} · {onboarding.city} · {onboarding.provider}
            {' · '}
            <Hostname truncate maxChars={18}>
              {onboarding.node_id}
            </Hostname>
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
            <strong>
              {onboarding.last_heartbeat_at ? (
                <Timestamp value={onboarding.last_heartbeat_at} mode="absolute" />
              ) : (
                '尚无'
              )}
            </strong>
          </div>
          <div className="hero-meta-card">
            <span>最近同步</span>
            <strong>
              {onboarding.last_sync_at ? (
                <Timestamp value={onboarding.last_sync_at} mode="absolute" />
              ) : (
                '尚无'
              )}
            </strong>
          </div>
          <div className="hero-meta-card">
            <span>当前主问题</span>
            <strong>{onboarding.current_primary_issue_summary || '暂无明显异常'}</strong>
          </div>
        </div>
      </section>

      <DetailSection eyebrow="接入进度" title="接入流程">
        <Stepper steps={derivePhaseSteps(onboarding)} ariaLabel="节点接入进度" />
        {onboarding.phase === '接入完成' ? (
          <p className="onboarding-completed-link">
            <Link className="text-link" to={`/nodes/${onboarding.node_id}`}>
              查看节点详情 →
            </Link>
          </p>
        ) : null}
      </DetailSection>

      <div className="summary-grid">
        <article className="summary-card">
          <p className="summary-card__label">首批样本</p>
          <p className="summary-card__value">{onboarding.has_host_sample ? '已到达' : '未到达'}</p>
        </article>
        <article className="summary-card">
          <p className="summary-card__label">已接收观测</p>
          <p className="summary-card__value">
            {onboarding.has_accepted_observation ? '已接收' : '未接收'}
          </p>
        </article>
      </div>

      {showBindingConflict ? (
        <DetailSection eyebrow="绑定冲突" title="绑定冲突处置" ribbon="critical" aside="高优先级">
          <article className="metric-card" aria-label="高优先级：绑定冲突待处理">
            <h3>高优先级：绑定冲突待处理</h3>
            <p>系统检测到新的指纹接入请求，请先确认是否接受新的绑定关系。</p>
            <dl>
              <div>
                <dt>当前已绑定指纹</dt>
                <dd>
                  {onboarding.current_binding_fingerprint_summary?.trim() ? (
                    <Hostname>{currentFingerprintSummary(onboarding)}</Hostname>
                  ) : (
                    currentFingerprintSummary(onboarding)
                  )}
                </dd>
              </div>
              <div>
                <dt>待确认指纹</dt>
                <dd>
                  {pendingBinding?.fingerprint ? (
                    <Hostname>{maskFingerprint(pendingBinding.fingerprint)}</Hostname>
                  ) : (
                    '尚无'
                  )}
                </dd>
              </div>
              <div>
                <dt>首次出现</dt>
                <dd>
                  <Timestamp value={pendingBinding?.first_seen_at} mode="absolute" />
                </dd>
              </div>
              <div>
                <dt>最近出现</dt>
                <dd>
                  <Timestamp value={pendingBinding?.last_seen_at} mode="absolute" />
                </dd>
              </div>
              <div>
                <dt>尝试次数</dt>
                <dd>
                  <MonoDigits>{pendingBinding?.attempt_count ?? 0}</MonoDigits>
                </dd>
              </div>
            </dl>
            {conflictState.error ? <p role="alert">{conflictState.error}</p> : null}
            {pendingConflictChoice === null ? (
              <div className="badge-row">
                <button
                  type="button"
                  className="btn btn--secondary btn--sm"
                  disabled={conflictState.action !== null}
                  onClick={() => setPendingConflictChoice('confirm')}
                >
                  确认重新绑定…
                </button>
                <button
                  type="button"
                  className="btn btn--secondary btn--sm"
                  disabled={conflictState.action !== null}
                  onClick={() => setPendingConflictChoice('reject')}
                >
                  拒绝新指纹…
                </button>
                <button
                  type="button"
                  className="btn btn--secondary btn--sm"
                  disabled={conflictState.action !== null}
                  onClick={() => setPendingConflictChoice('reset')}
                >
                  重置绑定…
                </button>
              </div>
            ) : null}
          </article>
          {pendingConflictChoice !== null ? (
            <ActionConfirmationCard
              title={CONFLICT_ACTION_COPY[pendingConflictChoice].title}
              current={CONFLICT_ACTION_COPY[pendingConflictChoice].current}
              result={CONFLICT_ACTION_COPY[pendingConflictChoice].result}
              impact={CONFLICT_ACTION_COPY[pendingConflictChoice].impact}
              unchanged={CONFLICT_ACTION_COPY[pendingConflictChoice].unchanged}
              confirmLabel={CONFLICT_ACTION_COPY[pendingConflictChoice].confirmLabel}
              disabled={conflictState.action !== null}
              onConfirm={() => {
                const choice = pendingConflictChoice
                if (!choice) return
                const request = CONFLICT_ACTION_REQUESTS[choice]
                void (async () => {
                  await handleBindingAction(choice, request)
                  setPendingConflictChoice((current) => (current === choice ? null : current))
                })()
              }}
              onCancel={() => setPendingConflictChoice(null)}
            />
          ) : null}
        </DetailSection>
      ) : null}

      <DetailSection eyebrow="接入 Token" title="接入凭证" ribbon="accent" aside={tokenAside}>
        {tokenIssue && !tokenCollapsed ? (
          <Card cardRole="warning">
            <p className="onboarding-token__hint">
              请在本次会话内完成安装。关闭后本会话仍可重新展开；离开页面后需要重新生成。
            </p>
            <div className="onboarding-token__row">
              <MonoDigits className="onboarding-token__value">{tokenIssue.token}</MonoDigits>
              <CopyButton value={tokenIssue.token} label="复制 token" />
            </div>
            <div className="onboarding-token__actions">
              <button
                type="button"
                className="btn btn--secondary btn--sm"
                onClick={() => setTokenCollapsed(true)}
              >
                已保存，关闭
              </button>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                disabled={tokenState.action !== null}
                onClick={handleIssueEnrollmentToken}
              >
                重新生成接入 Token
              </button>
            </div>
            {tokenErrorMessage ? (
              <p role="alert" className="onboarding-token__error-summary">
                <MonoDigits>{tokenErrorMessage}</MonoDigits>
              </p>
            ) : null}
          </Card>
        ) : tokenIssue && tokenCollapsed ? (
          <Card cardRole="dim">
            <p className="onboarding-token__hint onboarding-token__hint--critical">
              Token 明文已隐藏。本会话内可重新展开；离开页面后需要重新生成。
            </p>
            <div className="onboarding-token__actions">
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                onClick={() => setTokenCollapsed(false)}
              >
                重新展开 token 明文
              </button>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                disabled={tokenState.action !== null}
                onClick={handleIssueEnrollmentToken}
              >
                重新生成接入 Token
              </button>
            </div>
          </Card>
        ) : tokenErrorMessage ? (
          <Card cardRole="warning">
            <p>{tokenErrorMessage}</p>
            <div className="onboarding-token__actions">
              <button
                type="button"
                className="btn btn--primary btn--md"
                disabled={tokenState.action !== null}
                onClick={handleIssueEnrollmentToken}
              >
                {tokenState.action === 'issue' ? '正在生成…' : '重新生成接入 Token'}
              </button>
            </div>
          </Card>
        ) : (
          <div className="empty-state">
            <h3>当前会话里没有可显示的 Token 明文。</h3>
            <p>请重新生成接入 Token，再继续安装或核对配置。</p>
            <div className="onboarding-token__actions onboarding-token__actions--center">
              <button
                type="button"
                className="btn btn--primary btn--md"
                disabled={tokenState.action !== null}
                onClick={handleIssueEnrollmentToken}
              >
                {tokenState.action === 'issue' ? '正在生成…' : '重新生成接入 Token'}
              </button>
            </div>
          </div>
        )}
      </DetailSection>

      <DetailSection eyebrow="安装步骤" title="接入步骤">
        <ol className="onboarding-steps">
          <li>
            <p>
              在服务器上安装 <code>houfeng-agent</code>（参考部署文档）。
            </p>
          </li>
          <li>
            <p>
              创建 systemd 环境文件 <code>/etc/houfeng-agent/agent.env</code>：
            </p>
            <div className="onboarding-snippet">
              <pre>
                <code>{envSnippet}</code>
              </pre>
              <CopyButton value={envSnippet} label="复制配置" />
            </div>
            <p className="onboarding-steps__hint">
              <code>{serverUrl}</code> 取自当前页面地址。如经反向代理或公网访问与该地址不一致，请按实际外部 URL 调整。
            </p>
          </li>
          <li>
            <p>
              把 token 写入 <code>/etc/houfeng-agent/token</code>：
            </p>
            <div className="onboarding-snippet">
              <pre>
                <code>{tokenWriteSnippet}</code>
              </pre>
              <CopyButton value={tokenWriteSnippet} label="复制命令" />
            </div>
            {!tokenAvailableForTemplate ? (
              <p className="onboarding-steps__hint">
                先生成或展开上方 token 明文，此处会自动填入真值；当前显示占位 <code>{TOKEN_PLACEHOLDER}</code>。
              </p>
            ) : null}
          </li>
          <li>
            <p>启动服务：</p>
            <div className="onboarding-snippet">
              <pre>
                <code>{startSnippet}</code>
              </pre>
              <CopyButton value={startSnippet} label="复制命令" />
            </div>
          </li>
          <li>
            <p>返回本页面，等待首次同步与绑定完成（约 1-5 分钟）。</p>
          </li>
        </ol>
      </DetailSection>

      <p className="onboarding-snapshot-meta">
        数据快照时间：
        <Timestamp value={onboarding.updated_at} mode="absolute" />
        ，刷新页面获取最新。
      </p>
    </div>
  )
}
