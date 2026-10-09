import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { Modal, Hostname, MonoDigits, Timestamp } from '../../components/atoms'
import { CollapsibleSection } from '../../components/CollapsibleSection'
import { ApiError, issueMonitoringInstanceInstallCommand, listMonitoringInstancePhases, resetMonitoringInstanceBinding } from '../../lib/api'
import { useCopyToClipboard } from '../../lib/useCopyToClipboard'
import type { MonitoringInstanceInstallCommandIssue, MonitoringInstancePhase, MonitoringInstanceRecord } from '../../lib/types'

const MANUAL_TOKEN_PLACEHOLDER = '<30-minute enrollment token>'
const MANUAL_SERVER_PLACEHOLDER = '<center public base URL>'

const manualEnvSnippet = `HOUFENG_AGENT_SERVER_URL=${MANUAL_SERVER_PLACEHOLDER}
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=65536
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
HOUFENG_AGENT_BUFFER_MAX_BYTES=67108864`
const manualTokenSnippet = `printf '%s' '${MANUAL_TOKEN_PLACEHOLDER}' | sudo tee /etc/houfeng-agent/token >/dev/null`

const installSteps = [
  '生成 center 签发的一键命令',
  '在目标主机的 root 或 sudo shell 粘贴执行',
  '回到本页等待首次同步',
]

function describeInstallCommandError(error: unknown) {
  if (error instanceof ApiError) {
    if (error.code === 'install_command_unconfigured') {
      const detail = error.message.trim()
      return detail
        ? `中心一键安装配置不完整：${detail}。请检查 HOUFENG_PUBLIC_BASE_URL 与发布版本配置后重新生成。`
        : '中心一键安装配置不完整。请检查 HOUFENG_PUBLIC_BASE_URL 与发布版本配置后重新生成。'
    }
    if (error.code === 'monitoring_instance_archived') {
      return '所属 VPS 已归档，无法生成安装命令。'
    }
    return error.message
  }
  if (error instanceof Error) return error.message
  return '生成一键安装命令失败'
}

function CopyButton({ value, label, size = 'sm' }: { value: string; label: string; size?: 'sm' | 'md' }) {
  const { copy, copied } = useCopyToClipboard()
  return (
    <button
      type="button"
      className={`btn ${size} ghost`}
      onClick={() => void copy(value)}
      disabled={!value}
      aria-live="polite"
    >
      {copied ? '已复制' : label}
    </button>
  )
}

type Props = {
  monitoringInstance: MonitoringInstanceRecord
  open: boolean
  onClose: () => void
  returnVPSId?: string | null
  mode?: 'connect' | 'upgrade'
}

type IssueState = {
  issue: MonitoringInstanceInstallCommandIssue | null
  busy: boolean
  error: string | null
  hidden: boolean
  copyStatus: 'idle' | 'copied' | 'failed'
  /** 本次签发之后才开始、已连上的完整会话；与签发绑定，重新生成时清空。 */
  connectedSession: MonitoringInstancePhase | null
}

const ONBOARDING_POLL_INTERVAL_MS = 5000

const EMPTY_ISSUE_STATE: IssueState = {
  issue: null,
  busy: false,
  error: null,
  hidden: false,
  copyStatus: 'idle',
  connectedSession: null,
}

// 以会话身份判断接入：完整（采集与命令）会话、已收到可信在线信号、开始时间不早于签发时间。
// 调用方还会排除签发后基线里已有的会话（与签发并发提交的旧接入可能晚于 issued_at 开始）。
function isSessionFromIssue(phase: MonitoringInstancePhase, issuedAt: string): boolean {
  const started = Date.parse(phase.started_at)
  const issued = Date.parse(issuedAt)
  return phase.capability === 'full' && phase.ever_connected && Number.isFinite(started) && Number.isFinite(issued) && started >= issued
}

export function MonitoringInstanceOnboardingDrawer({ monitoringInstance, open, onClose, returnVPSId, mode = 'connect' }: Props) {
  const navigate = useNavigate()
  const location = useLocation()
  const { copy } = useCopyToClipboard()
  const [state, setState] = useState<IssueState>(EMPTY_ISSUE_STATE)
  const issueRequestRef = useRef(0)
  const busyRef = useRef(false)
  const mountedRef = useRef(true)
  const openRef = useRef(open)
  const subjectRef = useRef(monitoringInstance.monitoring_instance_id)
  const subjectId = monitoringInstance.monitoring_instance_id
  const retired = monitoringInstance.lifecycle_status === '已退役'
  const requiresNewSession = retired || monitoringInstance.lifecycle_status === '已接入'
  const enrollmentBlocked = Boolean(monitoringInstance.archived_at) || monitoringInstance.vps_lifecycle_status === 'archived' || (retired && monitoringInstance.vps_lifecycle_status !== 'active')
  const resetSubjectRef = useRef<string | null>(null)
  const [seenIdentity, setSeenIdentity] = useState({ open, subjectId })

  if (openRef.current !== open) {
    openRef.current = open
    if (!open) {
      issueRequestRef.current += 1
      busyRef.current = false
      resetSubjectRef.current = null
    }
  }
  if (subjectRef.current !== subjectId) {
    subjectRef.current = subjectId
    issueRequestRef.current += 1
    busyRef.current = false
    resetSubjectRef.current = null
  }
  if (seenIdentity.open !== open || seenIdentity.subjectId !== subjectId) {
    setSeenIdentity({ open, subjectId })
    if (!open || seenIdentity.subjectId !== subjectId) {
      setState(EMPTY_ISSUE_STATE)
    }
  }

  useLayoutEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      issueRequestRef.current += 1
      busyRef.current = false
    }
  }, [])

  function isLiveIssue(requestId: number, subjectId: string) {
    return (
      mountedRef.current &&
      openRef.current &&
      issueRequestRef.current === requestId &&
      subjectRef.current === subjectId
    )
  }

  // 抽屉打开且已签发命令时轮询会话列表；找到新会话即停止。在途标记按每次签发独立：例行轮询在本次签发的请求
  // 在途时跳过；上一次签发遗留的请求即使挂起也不阻塞新签发，其迟到响应因已取消而被忽略。
  // 签发后立即读一次作为基线：基线里已有的会话（包括与签发并发、在签发返回前提交的旧接入）一律不算本次接入。
  // 基线读取失败时以第一次成功读取为准——宁可不自动推进，也不把旧会话当成新接入。
  const pollIssue = open && state.connectedSession === null ? state.issue : null
  useEffect(() => {
    if (!pollIssue) return
    let cancelled = false
    let inFlight = false
    let baseline: Set<string> | null = null
    const read = () => {
      if (cancelled || inFlight) return
      inFlight = true
      listMonitoringInstancePhases(subjectId)
        .then((phases) => {
          if (cancelled) return
          if (baseline === null) {
            baseline = new Set(phases.map((phase) => phase.session_id))
            return
          }
          const known = baseline
          const session = phases.find((phase) => !known.has(phase.session_id) && isSessionFromIssue(phase, pollIssue.issued_at))
          if (session) {
            setState((current) => (current.issue === pollIssue ? { ...current, connectedSession: session } : current))
          }
        })
        .catch(() => undefined)
        .finally(() => { inFlight = false })
    }
    read()
    const interval = setInterval(read, ONBOARDING_POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [pollIssue, subjectId])

  function handleRequestClose() {
    if (busyRef.current) return
    onClose()
  }

  async function handleIssue() {
    if (busyRef.current) return
    if (enrollmentBlocked) {
      setState((current) => ({
        ...current,
        error: '所属 VPS 未处于管理中，不能重新接入。请从 VPS 详情核对生命周期。',
      }))
      return
    }
    const subjectId = monitoringInstance.monitoring_instance_id
    const requestId = issueRequestRef.current + 1
    issueRequestRef.current = requestId
    busyRef.current = true
    setState((current) => ({ ...current, busy: true, error: null }))
    try {
      if (requiresNewSession && resetSubjectRef.current !== subjectId) {
        await resetMonitoringInstanceBinding(subjectId)
        if (!isLiveIssue(requestId, subjectId)) return
        resetSubjectRef.current = subjectId
      }
      const issue = await issueMonitoringInstanceInstallCommand(subjectId)
      if (!isLiveIssue(requestId, subjectId)) return
      const copied = await copy(issue.command)
      if (!isLiveIssue(requestId, subjectId)) return
      busyRef.current = false
      setState({ issue, busy: false, error: null, hidden: false, copyStatus: copied ? 'copied' : 'failed', connectedSession: null })
    } catch (error: unknown) {
      if (!isLiveIssue(requestId, subjectId)) return
      busyRef.current = false
      setState((current) => ({
        ...current,
        busy: false,
        error: describeInstallCommandError(error),
        copyStatus: 'idle',
      }))
    }
  }

  function handleComplete() {
    if (busyRef.current) return
    if (returnVPSId) {
      navigate(`/vps/${encodeURIComponent(returnVPSId)}`, { state: location.state })
      return
    }
    onClose()
  }

  const { issue, busy, error, hidden, copyStatus } = state
  const isUpgrade = mode === 'upgrade'
  const taskTitle = isUpgrade ? '升级/重新接入 agent' : '接入 agent'
  const subjectName = monitoringInstance.display_name.trim() || monitoringInstance.monitoring_instance_id
  const title = `${subjectName} · ${taskTitle}`
  const primaryLabel = issue
    ? isUpgrade ? '重新生成升级/重新接入命令' : '重新生成安装命令'
    : isUpgrade ? '生成升级/重新接入命令' : '生成一键安装命令'
  // 接入完成后不再提供“重新生成”，避免误点重置刚建立的绑定。
  const connectedSinceIssue = issue !== null && state.connectedSession !== null
  const canShowCommand = issue !== null && !hidden && !connectedSinceIssue
  const completeLabel = returnVPSId ? '完成并返回 VPS' : '完成并查看监控实例'

  return (
    <Modal
      open={open}
      onClose={handleRequestClose}
      persistent={busy}
      title={title}
      ariaLabel="监控实例接入抽屉"
      size="lg"
      footer={
        <div className="monitoring-detail-drawer__footer">
          {error ? (
            <p role="alert" className="monitoring-detail-drawer__error">
              <MonoDigits>{error}</MonoDigits>
            </p>
          ) : null}
          {connectedSinceIssue ? null : (
            <button
              type="button"
              className={`btn md ${issue ? 'secondary' : 'primary'}`}
              disabled={busy || enrollmentBlocked}
              onClick={() => void handleIssue()}
            >
              {busy ? '正在生成…' : primaryLabel}
            </button>
          )}
          {issue ? (
            // 命令生成后主操作是“完成”；重新生成会使刚复制的命令失效，降为次操作。
            <button type="button" className="btn md primary" disabled={busy} onClick={handleComplete}>
              {completeLabel}
            </button>
          ) : null}
        </div>
      }
    >
      <div className="monitoring-detail-onboarding">
        {requiresNewSession ? <p>生成重新接入命令将开始新的接入阶段。已有会话仅保留在线证据权限；采集与命令需要新凭据。</p> : null}
        {/* 标题已写明实例名称；内部 ID 不在接入步骤里外露，完成后的去向写在主操作上。 */}
        {enrollmentBlocked ? (
          <p role="alert">所属 VPS 未处于管理中，不能重新接入。请从 VPS 详情核对生命周期。</p>
        ) : null}
        <ol className="monitoring-detail-onboarding__steps">
          {installSteps.map((step, index) => (
            <li key={step}>
              <span className="monitoring-detail-onboarding__index">{index + 1}</span>
              {step}
            </li>
          ))}
        </ol>
        <p className="monitoring-detail-onboarding__secret">
          {isUpgrade
            ? '命令由 center 签发，用于在已接入主机上升级或重新接入，不会新建监控实例。'
            : '命令已包含本系统的公开访问地址，在目标主机上直接执行即可。'}
          {' '}命令含 30 分钟一次性接入令牌，不要写入工单、聊天、日志或截图。重新生成会使上一条立即失效。
        </p>
        {connectedSinceIssue ? (
          <p className="monitoring-detail-onboarding__status" role="status">
            已收到新 agent 会话的心跳（<Timestamp value={state.connectedSession?.last_trusted_online_at ?? state.connectedSession?.started_at ?? null} mode="both" />），接入完成。
          </p>
        ) : issue ? (
          <p className="monitoring-detail-onboarding__hint" role="status">
            执行命令后，本窗口会自动检测首次心跳。
          </p>
        ) : null}
        {issue && hidden && !connectedSinceIssue ? (
          <button
            type="button"
            className="btn sm secondary"
            onClick={() => setState((current) => ({ ...current, hidden: false }))}
          >
            重新展开命令
          </button>
        ) : null}

        {canShowCommand && issue ? (
          <div className="monitoring-detail-onboarding__command" aria-label="一键安装命令">
            {copyStatus === 'copied' ? (
              <p className="monitoring-detail-onboarding__status" role="status">安装命令已自动复制到剪贴板。</p>
            ) : copyStatus === 'failed' ? (
              <p className="monitoring-detail-onboarding__status monitoring-detail-onboarding__status--error" role="alert">
                自动复制失败，请使用手动复制按钮。
              </p>
            ) : null}
            <div className="onboarding-snippet">
              <pre>
                <code>{issue.command}</code>
              </pre>
              <CopyButton value={issue.command} label="复制安装命令" size="md" />
            </div>
            <dl className="monitoring-detail-onboarding__facts">
              <div>
                <dt>过期时间</dt>
                <dd>
                  <Timestamp value={issue.expires_at} mode="both" />
                </dd>
              </div>
              <div>
                <dt>Center</dt>
                <dd>
                  <Hostname>{issue.public_base_url}</Hostname>
                </dd>
              </div>
              <div>
                <dt>Agent</dt>
                <dd>
                  <MonoDigits>{issue.agent_version}</MonoDigits>
                  {' · '}
                  <MonoDigits>{issue.release_repo}</MonoDigits>
                </dd>
              </div>
            </dl>
            <button
              type="button"
              className="btn sm ghost"
              onClick={() => setState((current) => ({ ...current, hidden: true }))}
              aria-label="隐藏安装命令"
            >
              已保存，隐藏命令
            </button>
          </div>
        ) : issue && hidden && !connectedSinceIssue ? (
          <p className="monitoring-detail-onboarding__secret">
            安装命令已隐藏。本抽屉会话内可重新展开；关闭或过期后请重新生成。
          </p>
        ) : null}

        <CollapsibleSection title="手工安装（排障回退）">
          <p className="monitoring-detail-onboarding__hint">
            仅在安装器、下载或 systemd 写入失败时使用。不要用浏览器地址推导生产 Center URL。
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
        </CollapsibleSection>
      </div>
    </Modal>
  )
}
