import { useEffect, useState } from 'react'

import { DetailSection } from '../../components/DetailSection'
import { PageState } from '../../components/PageState'
import { ApiError, getDeploymentSettings } from '../../lib/api'
import type {
  DeploymentSettingApplication,
  DeploymentSettingGroup,
  DeploymentSettingItem,
} from '../../lib/types'

type LoadState = {
  loading: boolean
  error: string | null
  forbidden: boolean
  items: DeploymentSettingItem[] | null
}

const INITIAL_STATE: LoadState = {
  loading: true,
  error: null,
  forbidden: false,
  items: null,
}

const GROUP_ORDER: readonly DeploymentSettingGroup[] = [
  'process',
  'database',
  'notifications',
  'monitoring',
  'authentication',
  'records',
  'storage',
]

const GROUP_LABELS: Record<DeploymentSettingGroup, string> = {
  process: '进程',
  database: '数据库',
  notifications: '通知',
  monitoring: '监控',
  authentication: '认证',
  records: '记录',
  storage: '存储',
}

const KEY_LABELS: Record<string, string> = {
  HOUFENG_HTTP_ADDR: '监听地址',
  HOUFENG_WEB_DIST_DIR: '前端静态目录',
  HOUFENG_PUBLIC_BASE_URL: '公开访问地址',
  HOUFENG_LOG_FILE: '日志文件',
  HOUFENG_TRUSTED_PROXIES: '受信任代理',
  HOUFENG_DATABASE_URL: '数据库连接',
  HOUFENG_DATABASE_REQUIRE_TLS: '数据库要求 TLS',
  HOUFENG_TELEGRAM_BOT_TOKEN: 'Telegram Bot Token',
  HOUFENG_TELEGRAM_CHAT_ID: 'Telegram Chat ID',
  HOUFENG_INCIDENT_SWEEP_INTERVAL: '异常扫描间隔',
  HOUFENG_INITIAL_USERNAME: '初始用户名',
  HOUFENG_INITIAL_DISPLAY_NAME: '初始显示名',
  HOUFENG_INITIAL_PASSWORD: '初始密码',
  HOUFENG_SESSION_TTL: '会话有效期',
  HOUFENG_SESSION_HMAC_KEY: '会话签名密钥',
  HOUFENG_PASSWORD_BCRYPT_COST: '密码哈希成本',
  HOUFENG_RECORDS_ENABLED: '记录功能',
  HOUFENG_RECORD_PERMANENT_DELETE_ENABLED: '永久删除',
  HOUFENG_RECORD_INSTANCE_ID: '记录实例 ID',
  HOUFENG_RECORD_DEPLOYMENT_ID: '记录部署 ID',
  HOUFENG_RECORD_INSTANCE_KIND: '记录实例类型',
  HOUFENG_RECORD_INSTANCE_CAPABILITY: '记录实例能力',
  HOUFENG_PORTABILITY_ENABLED: '导入导出',
  HOUFENG_COMPARISON_ENABLED: '横向比较',
  HOUFENG_COMPARISON_INTENT_KEY_ID: '比较意图密钥 ID',
  HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES: '比较准入预算',
  HOUFENG_COMPARISON_INTENT_KEYRING: '比较意图密钥环',
  HOUFENG_ATTACHMENT_BLOB_BACKEND: '附件存储后端',
  HOUFENG_ATTACHMENT_BLOB_ROOT: '附件本地目录',
  HOUFENG_ATTACHMENT_S3_ENDPOINT: '附件 S3 端点',
  HOUFENG_ATTACHMENT_S3_BUCKET: '附件 S3 存储桶',
  HOUFENG_ATTACHMENT_S3_SECURE: '附件 S3 使用 TLS',
  HOUFENG_ATTACHMENT_S3_ACCESS_KEY: '附件 S3 访问密钥',
  HOUFENG_ATTACHMENT_S3_SECRET_KEY: '附件 S3 秘密密钥',
  HOUFENG_CLAMAV_NETWORK: 'ClamAV 网络',
  HOUFENG_CLAMAV_ADDRESS: 'ClamAV 地址',
  HOUFENG_CLAMAV_DIAL_TIMEOUT: 'ClamAV 连接超时',
  HOUFENG_CLAMAV_OPERATION_TIMEOUT: 'ClamAV 操作超时',
  HOUFENG_CLAMAV_CHUNK_SIZE: 'ClamAV 分块大小',
  HOUFENG_CLAMAV_RESPONSE_LIMIT: 'ClamAV 响应上限',
  HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS: '内容处理最大尝试',
  HOUFENG_DELETION_WITNESS_DATABASE_URL: '删除见证数据库',
  HOUFENG_RECORD_DELETION_KEY_FILE: '记录删除密钥文件',
  HOUFENG_BACKUP_HMAC_KEY_FILE: '备份校验密钥文件',
}

function sourceLabel(source: string): string {
  if (source === 'environment') return '环境变量'
  if (source === 'file') return '文件'
  if (source === 'default') return '程序默认'
  if (source === 'unset') return '未设置'
  return source
}

function applicationLabel(application: string): string {
  if (application === 'startup') return '启动时生效'
  if (application === 'bootstrap_only') return '仅初始化'
  if (application === 'fallback') return '部署回退'
  if (application === 'inactive') return '当前未生效'
  if (application === 'reservation_only') return '仅保留'
  return application
}

function applicationNote(application: DeploymentSettingApplication): string | null {
  if (application === 'bootstrap_only') return '仅在用户库为空时初始化，不代表当前账号或密码。'
  if (application === 'fallback') return '这是启动时的部署回退值。页面覆盖或停用后，以通知和监控策略中的有效设置为准。'
  if (application === 'inactive') return '当前未加载，这里的值不代表正在生效。'
  if (application === 'reservation_only') return '仅标记保留路径，运行组件尚未加载这项密钥。'
  return null
}

function valueText(item: DeploymentSettingItem): string {
  if (item.sensitive || item.value == null || item.value.trim() === '') {
    return item.configured ? '已配置' : '未配置'
  }
  if (item.value === 'true') return '是'
  if (item.value === 'false') return '否'
  return item.value
}

function groupedItems(items: readonly DeploymentSettingItem[]): Array<{ group: string; label: string; items: DeploymentSettingItem[] }> {
  const known = new Set<string>(GROUP_ORDER)
  const groups: Array<{ group: string; label: string; items: DeploymentSettingItem[] }> = GROUP_ORDER.map((group) => ({
    group,
    label: GROUP_LABELS[group],
    items: items.filter((item) => item.group === group),
  })).filter((group) => group.items.length > 0)
  const extras = new Map<string, DeploymentSettingItem[]>()
  for (const item of items) {
    if (known.has(item.group)) continue
    const current = extras.get(item.group) ?? []
    current.push(item)
    extras.set(item.group, current)
  }
  for (const [group, groupItems] of extras) {
    groups.push({ group, label: group, items: groupItems })
  }
  return groups
}

export function DeploymentSettingsSection() {
  const [reloadKey, setReloadKey] = useState(0)
  const [state, setState] = useState<LoadState>(INITIAL_STATE)

  useEffect(() => {
    const controller = new AbortController()
    let cancelled = false
    getDeploymentSettings(controller.signal)
      .then((snapshot) => {
        if (cancelled) return
        setState({ loading: false, error: null, forbidden: false, items: snapshot.items })
      })
      .catch((error: unknown) => {
        if (cancelled || controller.signal.aborted) return
        if (error instanceof ApiError && error.status === 403) {
          setState({ loading: false, error: null, forbidden: true, items: null })
          return
        }
        setState({ loading: false, error: '部署配置暂时无法读取。', forbidden: false, items: null })
      })
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [reloadKey])

  if (state.loading) {
    return <PageState kind="loading" title="正在加载部署配置…" surface="empty" compact />
  }
  if (state.forbidden) {
    return (
      <section className="settings-section" aria-labelledby="deployment-forbidden-title">
        <h2 className="ss-title" id="deployment-forbidden-title">无权查看部署配置</h2>
        <p className="ss-desc">当前账号没有部署配置查看能力。部署概览不会打开。</p>
      </section>
    )
  }
  if (state.error || !state.items) {
    return (
      <PageState
        kind="error"
        title="部署配置不可用"
        description={state.error ?? '部署配置暂时无法读取。'}
        action={<button type="button" className="btn sm secondary" onClick={() => {
          setState(INITIAL_STATE)
          setReloadKey((key) => key + 1)
        }}>重试</button>}
        surface="empty"
        compact
      />
    )
  }

  const groups = groupedItems(state.items)

  return (
    <div className="deployment-settings">
      <p className="deployment-settings__intro">
        这里展示本次进程启动时读到的部署输入。修改部署文件或环境变量后，需要重启才会反映到这里。
      </p>
      {groups.length === 0 ? (
        <PageState kind="empty" title="没有可展示的部署输入" surface="empty" compact />
      ) : groups.map((group) => (
        <DetailSection key={group.group} eyebrow="部署输入" title={group.label}>
          {group.items.map((item) => {
            const note = applicationNote(item.application)
            return (
              <div className="settings-row settings-row--block" key={item.key}>
                <div className="sr-label-row">
                  <span className="sr-label">{KEY_LABELS[item.key] ?? item.key}</span>
                  <span className="sr-value">{valueText(item)}</span>
                </div>
                <p className="deployment-settings__meta">
                  <span>{item.key}</span>
                  <span>{sourceLabel(item.source)}</span>
                  <span>{applicationLabel(item.application)}</span>
                </p>
                {note ? <p className="telegram-settings__hint">{note}</p> : null}
              </div>
            )
          })}
        </DetailSection>
      ))}
      <p className="deployment-settings__footer">部署编排参数请在部署文件查看。</p>
    </div>
  )
}
