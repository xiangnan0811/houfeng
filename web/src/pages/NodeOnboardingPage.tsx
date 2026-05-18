import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

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
  issueNodeInstallCommand,
  rejectPendingNodeBinding,
  resetNodeBinding,
} from '../lib/api'
import { formatLabelList } from '../lib/format'
import { useCopyToClipboard } from '../lib/useCopyToClipboard'
import type { NodeInstallCommandIssue, NodeOnboardingState } from '../lib/types'

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

type InstallCommandState = {
  issue: NodeInstallCommandIssue | null
  action: 'issue' | null
  error: string | null
  hidden: boolean
}

const MANUAL_TOKEN_PLACEHOLDER = '<30-minute enrollment token>'
const MANUAL_SERVER_PLACEHOLDER = '<center public base URL>'

const manualEnvSnippet = `HOUFENG_AGENT_SERVER_URL=${MANUAL_SERVER_PLACEHOLDER}
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=65536
HOUFENG_AGENT_BUFFER_MAX_AGE=72h`
const manualTokenSnippet = `printf '%s' '${MANUAL_TOKEN_PLACEHOLDER}' | sudo tee /etc/houfeng-agent/token >/dev/null`

const installChecklist = [
  '复制一键安装命令。',
  '在目标 VPS 的 root shell 或具备 sudo 的账号中粘贴执行。',
  '安装器会校验 linux/amd64 或 linux/arm64、systemd、下载工具和 checksum 工具。',
  '安装器下载 GitHub Release 中的 houfeng-agent，并用 sha256sums.txt 校验后再写入本机。',
  '安装完成后 systemd 会启动 agent，回到本页等待首次同步和绑定。',
]

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
      '后续 sync 将以新指纹为准；之前以旧指纹接入的 agent 将被拒绝。触发待确认的安装命令已消耗一次性 token，确认后请重新生成安装命令并在目标机器执行。',
    unchanged: '不会改变节点 ID 与历史观测数据。',
    confirmLabel: '确认重新绑定',
  },
  reject: {
    title: '拒绝该指纹接入',
    current: '有未确认的新指纹正在尝试以该节点接入。',
    result: '操作后：保留当前已绑定指纹，新指纹的接入请求将继续被拒绝。',
    impact:
      '使用新指纹的 agent 将持续无法 sync；该次接入已消耗一次性 token，如后续确认为误操作，需要重新生成安装命令。',
    unchanged: '不会改变当前已绑定指纹、节点状态或历史观测数据。',
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

function describeInstallCommandError(error: unknown) {
  if (error instanceof ApiError && error.status === 409) {
    return `中心一键安装配置不完整：${error.message}。请检查 HOUFENG_PUBLIC_BASE_URL 与发布版本配置后重新生成。`
  }
  return describeError(error, '生成一键安装命令失败')
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

  if (binding === '指纹变更待确认') {
    return [
      { label: labels[0], state: 'done' },
      { label: labels[1], state: 'error' },
      { label: labels[2], state: 'pending' },
      { label: labels[3], state: 'pending' },
    ]
  }

  if (binding === '未绑定') {
    return [
      { label: labels[0], state: 'current' },
      { label: labels[1], state: 'pending' },
      { label: labels[2], state: 'pending' },
      { label: labels[3], state: 'pending' },
    ]
  }

  if (!accepted) {
    return [
      { label: labels[0], state: 'done' },
      { label: labels[1], state: 'done' },
      { label: labels[2], state: 'current' },
      { label: labels[3], state: 'pending' },
    ]
  }

  return [
    { label: labels[0], state: 'done' },
    { label: labels[1], state: 'done' },
    { label: labels[2], state: 'done' },
    { label: labels[3], state: 'done' },
  ]
}

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
      {copied ? '已复制' : label}
    </button>
  )
}

function InstallCommandPanel({
  nodeId,
  issue,
  hidden,
  busy,
  error,
  onGenerate,
  onHide,
  onReveal,
}: {
  nodeId: string
  issue: NodeInstallCommandIssue | null
  hidden: boolean
  busy: boolean
  error: string | null
  onGenerate: () => void
  onHide: () => void
  onReveal: () => void
}) {
  const canShowCommand = issue !== null && !hidden
  const primaryLabel = issue ? '重新生成安装命令' : '生成一键安装命令'

  return (
    <div className="page-stack">
      <Card cardRole="warning">
        <p className="onboarding-token__hint onboarding-token__hint--critical">
          安装命令包含 30 分钟有效的一次性 enrollment token。请把它当作敏感信息处理，不要粘贴到工单、聊天、日志或截图里。
        </p>
        <div className="onboarding-token__actions">
          <button
            type="button"
            className="btn btn--primary btn--md"
            disabled={busy}
            onClick={onGenerate}
          >
            {busy ? '正在生成…' : primaryLabel}
          </button>
          {issue && hidden ? (
            <button type="button" className="btn btn--ghost btn--md" onClick={onReveal}>
              重新展开命令
            </button>
          ) : null}
        </div>
        {issue ? (
          <p className="onboarding-steps__hint">
            重新生成会立即使上一条安装命令里的 enrollment token 失效；如果命令过期、丢失或已经被隐藏，请重新生成。
          </p>
        ) : (
          <p className="onboarding-steps__hint">
            命令由 center 后端生成，使用 HOUFENG_PUBLIC_BASE_URL，不会从浏览器地址猜测生产 URL。
          </p>
        )}
        {error ? (
          <p role="alert" className="onboarding-token__error-summary">
            <MonoDigits>{error}</MonoDigits>
          </p>
        ) : null}
      </Card>

      {canShowCommand && issue ? (
        <Card cardRole="accent" aria-label="一键安装命令">
          <div className="onboarding-snippet">
            <pre>
              <code>{issue.command}</code>
            </pre>
            <CopyButton value={issue.command} label="复制安装命令" size="md" />
          </div>
          <dl className="metadata-list">
            <div>
              <dt>签发时间</dt>
              <dd>
                <Timestamp value={issue.issued_at} mode="both" />
              </dd>
            </div>
            <div>
              <dt>过期时间</dt>
              <dd>
                <Timestamp value={issue.expires_at} mode="both" />
              </dd>
            </div>
            <div>
              <dt>Center URL</dt>
              <dd>
                <Hostname>{issue.public_base_url}</Hostname>
              </dd>
            </div>
            <div>
              <dt>Installer</dt>
              <dd>
                <Hostname>{issue.installer_url}</Hostname>
              </dd>
            </div>
            <div>
              <dt>Agent Release</dt>
              <dd>
                <MonoDigits>{issue.agent_version}</MonoDigits>
                {' · '}
                <MonoDigits>{issue.release_repo}</MonoDigits>
              </dd>
            </div>
          </dl>
          <div className="onboarding-token__actions">
            <button
              type="button"
              className="btn btn--secondary btn--sm"
              onClick={onHide}
              aria-label="隐藏安装命令"
            >
              已保存，隐藏命令
            </button>
            <Link className="text-link" to={`/nodes/${nodeId}`}>
              安装后查看节点详情 →
            </Link>
          </div>
        </Card>
      ) : issue && hidden ? (
        <Card cardRole="dim">
          <p className="onboarding-token__hint onboarding-token__hint--critical">
            安装命令已隐藏。本页会话内可重新展开；如果已离开页面或命令过期，请重新生成。
          </p>
        </Card>
      ) : (
        <div className="empty-state">
          <h3>尚未生成一键安装命令。</h3>
          <p>点击上方按钮后，center 会签发新的 30 分钟一次性 token 并返回可复制命令。</p>
        </div>
      )}
    </div>
  )
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
  const [pendingConflictChoice, setPendingConflictChoice] = useState<ConflictAction | null>(null)
  const [installCommandState, setInstallCommandState] = useState<InstallCommandState>({
    issue: null,
    action: null,
    error: null,
    hidden: false,
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
        setPendingConflictChoice(null)
        setInstallCommandState({
          issue: null,
          action: null,
          error: null,
          hidden: false,
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
        setInstallCommandState({
          issue: null,
          action: null,
          error: null,
          hidden: false,
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
  const installIssue = installCommandState.issue
  const installAside = installIssue ? (
    <span>
      有效至：<Timestamp value={installIssue.expires_at} mode="both" />
    </span>
  ) : onboarding.enrollment_token_issued_at ? (
    <span>
      上次签发：<Timestamp value={onboarding.enrollment_token_issued_at} mode="both" />
    </span>
  ) : (
    <span>尚未签发</span>
  )

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

  async function handleIssueInstallCommand() {
    if (!nodeId) return

    setInstallCommandState((current) => ({
      ...current,
      action: 'issue',
      error: null,
    }))

    try {
      const issue = await issueNodeInstallCommand(nodeId)
      setInstallCommandState({
        issue,
        action: null,
        error: null,
        hidden: false,
      })
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
    } catch (error: unknown) {
      setInstallCommandState((current) => ({
        ...current,
        action: null,
        error: describeInstallCommandError(error),
      }))
    }
  }

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

      <DetailSection eyebrow="一键安装" title="复制一条命令完成安装" ribbon="accent" aside={installAside}>
        <InstallCommandPanel
          nodeId={onboarding.node_id}
          issue={installCommandState.issue}
          hidden={installCommandState.hidden}
          busy={installCommandState.action === 'issue'}
          error={installCommandState.error}
          onGenerate={handleIssueInstallCommand}
          onHide={() =>
            setInstallCommandState((current) => ({
              ...current,
              hidden: true,
            }))
          }
          onReveal={() =>
            setInstallCommandState((current) => ({
              ...current,
              hidden: false,
            }))
          }
        />
      </DetailSection>

      <DetailSection eyebrow="安装器行为" title="命令执行后会做什么">
        <ol className="onboarding-steps">
          {installChecklist.map((item) => (
            <li key={item}>
              <p>{item}</p>
            </li>
          ))}
        </ol>
      </DetailSection>

      <DetailSection eyebrow="手工回退" title="手工安装仍可作为排障路径" ribbon="notice">
        <Card cardRole="dim">
          <p className="onboarding-token__hint">
            优先使用上方一键命令。若需要排查安装器问题，可按部署文档手工安装二进制、写入 systemd 与以下配置；不要使用浏览器地址推导生产 Center URL。
          </p>
          <div className="onboarding-snippet">
            <pre>
              <code>{manualEnvSnippet}</code>
            </pre>
            <CopyButton value={manualEnvSnippet} label="复制环境模板" />
          </div>
          <div className="onboarding-snippet">
            <pre>
              <code>{manualTokenSnippet}</code>
            </pre>
            <CopyButton value={manualTokenSnippet} label="复制 token 写入模板" />
          </div>
          <p className="onboarding-steps__hint">
            手工 token 仍应从 center 生成，30 分钟内使用且只使用一次；不要把 token 明文写入日志。
          </p>
        </Card>
      </DetailSection>

      <p className="onboarding-snapshot-meta">
        数据快照时间：
        <Timestamp value={onboarding.updated_at} mode="absolute" />
        ，刷新页面获取最新。
      </p>
    </div>
  )
}
