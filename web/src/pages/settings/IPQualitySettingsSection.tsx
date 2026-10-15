import { Button, Toggle } from '../../components/atoms'
import type { IPQualitySettings, IPQualitySourceCredential } from '../../lib/types'
import type { SettingsIPQualityForm } from './types'

type IPQualitySettingsSectionProps = {
  value: SettingsIPQualityForm
  sources: IPQualitySettings['sources']
  onChange: (patch: Partial<SettingsIPQualityForm>) => void
}

type CredentialRowProps = {
  label: string
  credential: IPQualitySourceCredential
  draft: string
  clear: boolean
  onDraft: (value: string) => void
  onClear: (clear: boolean) => void
}

// 已保存的 Key 只显示掩码摘要；输入框从不预填，留空即保留，清除需显式操作。
function CredentialRow({ label, credential, draft, clear, onDraft, onClear }: CredentialRowProps) {
  const status = clear ? '保存后清除' : credential.configured ? `已配置 · ${credential.masked_summary}` : '未配置'
  return (
    <div className="settings-row">
      <span className="sr-label">{label}</span>
      <span className="sr-value">
        <input
          className="input"
          aria-label={`${label} API Key`}
          type="password"
          autoComplete="off"
          disabled={clear}
          value={draft}
          onChange={(event) => onDraft(event.target.value)}
          placeholder={credential.configured ? '输入新 Key 替换' : '输入 API Key'}
        />
        <span className="asset-table__muted">{status}</span>
        {clear ? (
          <Button size="sm" variant="ghost" onClick={() => onClear(false)}>撤销</Button>
        ) : credential.configured ? (
          <Button size="sm" variant="secondary" onClick={() => onClear(true)} aria-label={`清除 ${label} Key`}>清除</Button>
        ) : null}
      </span>
    </div>
  )
}

type PairedCredentialRowProps = {
  credential: IPQualitySourceCredential
  accountId: string
  licenseKey: string
  clear: boolean
  onChange: (patch: Pick<SettingsIPQualityForm, 'maxmindAccountId' | 'maxmindLicenseKey' | 'maxmindClear'>) => void
}

// GeoLite 官方查询：账号 ID 与 License Key 成对保存；只显示掩码摘要，清除时两者一并清除。
function GeoLiteCredentialRow({ credential, accountId, licenseKey, clear, onChange }: PairedCredentialRowProps) {
  const status = clear ? '保存后清除' : credential.configured ? `已配置 · ${credential.masked_summary}` : '未配置 · 不配置也有默认检测'
  return (
    <div className="settings-row settings-row--block">
      <div className="sr-label-row">
        <span className="sr-label">MaxMind GeoLite</span>
        <span className="asset-table__muted">{status}</span>
      </div>
      <span className="sr-value">
        <input
          className="input"
          aria-label="MaxMind GeoLite 账号 ID"
          type="text"
          inputMode="numeric"
          autoComplete="off"
          disabled={clear}
          value={accountId}
          onChange={(event) => onChange({ maxmindAccountId: event.target.value, maxmindLicenseKey: licenseKey, maxmindClear: false })}
          placeholder={credential.configured ? '新账号 ID' : '账号 ID'}
        />
        <input
          className="input"
          aria-label="MaxMind GeoLite License Key"
          type="password"
          autoComplete="off"
          disabled={clear}
          value={licenseKey}
          onChange={(event) => onChange({ maxmindAccountId: accountId, maxmindLicenseKey: event.target.value, maxmindClear: false })}
          placeholder={credential.configured ? '新 License Key' : 'License Key'}
        />
        {clear ? (
          <Button size="sm" variant="ghost" onClick={() => onChange({ maxmindAccountId: '', maxmindLicenseKey: '', maxmindClear: false })}>撤销</Button>
        ) : credential.configured ? (
          <Button
            size="sm"
            variant="secondary"
            onClick={() => onChange({ maxmindAccountId: '', maxmindLicenseKey: '', maxmindClear: true })}
            aria-label="清除 MaxMind GeoLite 账号 ID 与 License Key"
          >
            清除
          </Button>
        ) : null}
      </span>
    </div>
  )
}

export function IPQualitySettingsSection({ value, sources, onChange }: IPQualitySettingsSectionProps) {
  return (
    <>
      <div className="ss-title">IP 质量采集</div>
      <div className="ss-desc">低频采集出口 IP 归属、风险信号、服务解锁与邮件连通性；独立于主机实时采样</div>
      <div className="settings-row-group settings-row-group--2">
        <div className="settings-row">
          <span className="sr-label">采集开关</span>
          <Toggle
            label="启用 IP 质量采集"
            checked={value.enabled}
            onChange={(enabled) => onChange({ enabled })}
          />
        </div>
        <div className="settings-row">
          <span className="sr-label">邮件连通性</span>
          <Toggle
            label="检测邮局 25 端口连通性"
            checked={value.mailEnabled}
            onChange={(mailEnabled) => onChange({ mailEnabled })}
          />
        </div>
      </div>
      <div className="settings-row-group settings-row-group--3">
        <div className="settings-row">
          <span className="sr-label">采集周期</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="IP 质量采集周期秒数"
              inputMode="numeric"
              value={value.frequencySeconds}
              onChange={(e) => onChange({ frequencySeconds: e.target.value })}
            /> 秒
            <DurationHint seconds={value.frequencySeconds} />
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">过期窗口</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="IP 质量过期窗口秒数"
              inputMode="numeric"
              value={value.staleAfterSeconds}
              onChange={(e) => onChange({ staleAfterSeconds: e.target.value })}
            /> 秒
            <DurationHint seconds={value.staleAfterSeconds} />
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">请求超时</span>
          <span className="sr-value">
            <input
              className="input input--compact"
              aria-label="IP 质量请求超时秒数"
              inputMode="numeric"
              value={value.timeoutSeconds}
              onChange={(e) => onChange({ timeoutSeconds: e.target.value })}
            /> 秒
          </span>
        </div>
      </div>
      <div className="ss-desc">IP 质量报告及脱敏后的原始结果长期保留。</div>
      <div className="settings-row settings-row--block">
        <div className="sr-label-row">
          <span className="sr-label">服务集合</span>
          <span className="asset-table__muted">netflix, chatgpt, youtube-premium, amazon-prime-video, disney-plus, tiktok, reddit</span>
        </div>
        <textarea
          className="input settings-textarea"
          aria-label="IP 质量采集服务集合"
          value={value.servicesText}
          onChange={(e) => onChange({ servicesText: e.target.value })}
          rows={2}
        />
      </div>
      <div className="ss-title">数据源凭据</div>
      <div className="settings-row-group settings-row-group--2">
        <CredentialRow
          label="ipapi.is"
          credential={sources.ipapi_is}
          draft={value.ipapiKey}
          clear={value.ipapiClear}
          onDraft={(ipapiKey) => onChange({ ipapiKey, ipapiClear: false })}
          onClear={(ipapiClear) => onChange({ ipapiClear, ipapiKey: '' })}
        />
        <CredentialRow
          label="AbuseIPDB"
          credential={sources.abuseipdb}
          draft={value.abuseipdbKey}
          clear={value.abuseipdbClear}
          onDraft={(abuseipdbKey) => onChange({ abuseipdbKey, abuseipdbClear: false })}
          onClear={(abuseipdbClear) => onChange({ abuseipdbClear, abuseipdbKey: '' })}
        />
      </div>
      <GeoLiteCredentialRow
        credential={sources.maxmind}
        accountId={value.maxmindAccountId}
        licenseKey={value.maxmindLicenseKey}
        clear={value.maxmindClear}
        onChange={onChange}
      />
    </>
  )
}

// 采集周期、过期窗口按秒保存（与 API 一致），旁边给出换算后的天/小时/分钟，避免手算 86400、604800。
function DurationHint({ seconds }: { seconds: string }) {
  const value = Number(seconds.trim())
  if (!Number.isInteger(value) || value < 60) return null
  const units: Array<[number, string]> = [[86400, '天'], [3600, '小时'], [60, '分钟']]
  const [size, unit] = units.find(([unitSeconds]) => value >= unitSeconds) ?? units[2]!
  const amount = value / size
  const text = Number.isInteger(amount) ? `${amount} ${unit}` : `约 ${amount.toFixed(1)} ${unit}`
  return <span className="settings-duration-hint">（{text}）</span>
}
