import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { Card, Modal, Hostname, MonoDigits, Timestamp } from '../../components/atoms'
import { CollapsibleSection } from '../../components/CollapsibleSection'
import { ApiError, issueMonitoringInstanceInstallCommand } from '../../lib/api'
import { useCopyToClipboard } from '../../lib/useCopyToClipboard'
import type { MonitoringInstanceInstallCommandIssue, MonitoringInstanceRecord } from '../../lib/types'

const MANUAL_TOKEN_PLACEHOLDER = '<30-minute enrollment token>'
const MANUAL_SERVER_PLACEHOLDER = '<center public base URL>'

const manualEnvSnippet = `HOUFENG_AGENT_SERVER_URL=${MANUAL_SERVER_PLACEHOLDER}
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=65536
HOUFENG_AGENT_BUFFER_MAX_AGE=72h`
const manualTokenSnippet = `printf '%s' '${MANUAL_TOKEN_PLACEHOLDER}' | sudo tee /etc/houfeng-agent/token >/dev/null`

const installChecklist = [
  '复制下方 center 生成的一键安装命令。',
  '在目标 VPS 的 root shell 或具备 sudo 的账号中粘贴执行。',
  '安装器会校验 linux/amd64 或 linux/arm64、systemd、下载工具和 checksum 工具。',
  '安装器下载 GitHub Release 中的 houfeng-agent，并用 sha256sums.txt 校验后再写入本机。',
  '安装完成后 systemd 会启动 agent，回到本页等待首次同步和绑定。',
]

function describeInstallCommandError(error: unknown) {
  if (error instanceof ApiError && error.status === 409) {
    return `中心一键安装配置不完整：${error.message}。请检查 HOUFENG_PUBLIC_BASE_URL 与发布版本配置后重新生成。`
  }
  if (error instanceof ApiError || error instanceof Error) return error.message
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
}

export function MonitoringInstanceOnboardingDrawer({ monitoringInstance, open, onClose, returnVPSId, mode = 'connect' }: Props) {
  const navigate = useNavigate()
  const { copy } = useCopyToClipboard()
  const [state, setState] = useState<IssueState>({
    issue: null,
    busy: false,
    error: null,
    hidden: false,
    copyStatus: 'idle',
  })

  useEffect(() => {
    if (open) return
    setState({ issue: null, busy: false, error: null, hidden: false, copyStatus: 'idle' })
  }, [open])

  async function handleIssue() {
    setState((current) => ({ ...current, busy: true, error: null }))
    try {
      const issue = await issueMonitoringInstanceInstallCommand(monitoringInstance.monitoring_instance_id)
      const copied = await copy(issue.command)
      setState({ issue, busy: false, error: null, hidden: false, copyStatus: copied ? 'copied' : 'failed' })
    } catch (error: unknown) {
      setState((current) => ({
        ...current,
        busy: false,
        error: describeInstallCommandError(error),
        copyStatus: 'idle',
      }))
    }
  }

  function handleComplete() {
    if (returnVPSId) {
      navigate(`/vps/${encodeURIComponent(returnVPSId)}`)
      return
    }
    onClose()
  }

  const { issue, busy, error, hidden, copyStatus } = state
  const isUpgrade = mode === 'upgrade'
  const title = isUpgrade ? '升级/重新接入 agent' : '接入 agent'
  const primaryLabel = issue
    ? isUpgrade ? '重新生成升级/重新接入命令' : '重新生成安装命令'
    : isUpgrade ? '生成升级/重新接入命令' : '生成一键安装命令'
  const canShowCommand = issue !== null && !hidden
  const completeLabel = returnVPSId ? '完成并返回 VPS' : '完成并查看监控实例'

  return (
    <Modal open={open} onClose={onClose} title={title} ariaLabel="监控实例接入抽屉" size="xl">
      <div className="onboarding-drawer">
        <Card cardRole="warning" className="onboarding-drawer__brief">
          <p className="onboarding-token__hint onboarding-token__hint--critical">
            安装命令包含 30 分钟有效的一次性 enrollment token。请把它当作敏感信息处理，不要粘贴到工单、聊天、日志或截图里。
          </p>
          <p className="onboarding-steps__hint">
            {issue
              ? '重新生成会立即使上一条命令里的 enrollment token 失效；如果命令过期、丢失或已隐藏，请重新生成。'
              : isUpgrade
                ? '命令由 center 后端生成，用于在已接入或已观测的服务器上升级/重新接入 agent，不会新建监控实例。'
                : '命令由 center 后端生成，使用 HOUFENG_PUBLIC_BASE_URL，不会从浏览器地址猜测生产 URL。'}
          </p>
          <div className="onboarding-token__actions">
            <button type="button" className="btn md primary" disabled={busy} onClick={() => void handleIssue()}>
              {busy ? '正在生成…' : primaryLabel}
            </button>
            {issue && hidden ? (
              <button
                type="button"
                className="btn md ghost"
                onClick={() => setState((current) => ({ ...current, hidden: false }))}
              >
                重新展开命令
              </button>
            ) : null}
          </div>
          {error ? (
            <p role="alert" className="onboarding-token__error-summary">
              <MonoDigits>{error}</MonoDigits>
            </p>
          ) : null}
        </Card>

        {canShowCommand && issue ? (
          <Card cardRole="accent" aria-label="一键安装命令">
            {copyStatus === 'copied' ? (
              <p className="asset-operation-feedback" role="status">安装命令已自动复制到剪贴板。</p>
            ) : copyStatus === 'failed' ? (
              <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
                自动复制失败，请使用手动复制按钮。
              </p>
            ) : null}
            <div className="onboarding-snippet">
              <pre>
                <code>{issue.command}</code>
              </pre>
              <CopyButton value={issue.command} label="复制安装命令" size="md" />
            </div>
            <dl className="metadata-list">
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
                <dt>Agent Release</dt>
                <dd>
                  <MonoDigits>{issue.agent_version}</MonoDigits>
                  {' · '}
                  <MonoDigits>{issue.release_repo}</MonoDigits>
                </dd>
              </div>
            </dl>
            <div className="onboarding-token__actions">
              <button type="button" className="btn sm primary" onClick={handleComplete}>
                {completeLabel}
              </button>
              <button
                type="button"
                className="btn sm secondary"
                onClick={() => setState((current) => ({ ...current, hidden: true }))}
                aria-label="隐藏安装命令"
              >
                已保存，隐藏命令
              </button>
            </div>
          </Card>
        ) : issue && hidden ? (
          <Card cardRole="dim">
            <p className="onboarding-token__hint onboarding-token__hint--critical">
              安装命令已隐藏。本抽屉会话内可重新展开；如果已关闭或命令过期，请重新生成。
            </p>
          </Card>
        ) : null}

        <CollapsibleSection title="命令执行后会做什么" className="onboarding-drawer__section">
          <ol className="onboarding-steps">
            {installChecklist.map((item) => (
              <li key={item}>
                <p>{item}</p>
              </li>
            ))}
          </ol>
        </CollapsibleSection>

        <CollapsibleSection title="手工安装（排障回退）" className="onboarding-drawer__section">
          <Card cardRole="dim" className="onboarding-manual-fallback">
            <p className="onboarding-token__hint">
              优先使用上方一键命令。仅在排查安装器、下载或 systemd 写入问题时，按部署文档手工写入以下配置；不要用浏览器地址推导生产 Center URL。
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
          </Card>
        </CollapsibleSection>
      </div>
    </Modal>
  )
}
