import { DetailSection } from '../../components/DetailSection'
import { MonoDigits, Toggle } from '../../components/atoms'
import type { SettingsRecord } from '../../lib/types'
import type { SettingsFormState } from './types'

type FeishuSettingsForm = Pick<SettingsFormState, 'feishuEnabled' | 'feishuWebhookPresent' | 'feishuWebhookUrl'>

type FeishuSettingsSectionProps = {
  settings: SettingsRecord['feishu']
  form: FeishuSettingsForm
  onChange: (patch: Partial<FeishuSettingsForm>) => void
  wrapper?: 'detail' | 'none'
  isExpanded?: boolean
  onToggleExpand?: () => void
}

export function FeishuSettingsSection({
  settings,
  form,
  onChange,
  wrapper = 'detail',
  isExpanded = true,
  onToggleExpand,
}: FeishuSettingsSectionProps) {
  const innerContent = (
    <>
      <div className="settings-row">
        <span className="sr-label">启用飞书通知</span>
        <Toggle
          label="启用飞书通知"
          checked={form.feishuEnabled}
          onChange={(checked) => onChange({ feishuEnabled: checked })}
        />
      </div>
      <div className="settings-row">
        <span className="sr-label">Webhook URL</span>
        <span className="sr-value">
          <input
            className="input input--compact input--wide"
            aria-label="Webhook URL"
            type="text"
            autoComplete="off"
            placeholder="https://open.feishu.cn/..."
            value={form.feishuWebhookUrl}
            onChange={(e) => onChange({ feishuWebhookUrl: e.target.value })}
          />
        </span>
      </div>
      <div className="settings-row">
        <span className="sr-label">当前持久化状态</span>
        <span className="sr-value">
          {settings.webhook_url_present && settings.webhook_url_masked_summary ? (
            <>已配置飞书 Webhook：<MonoDigits>{settings.webhook_url_masked_summary}</MonoDigits></>
          ) : '未配置'}
        </span>
      </div>
    </>
  )

  if (wrapper === 'none') {
    return innerContent
  }

  return (
    <DetailSection
      eyebrow="飞书"
      title="飞书通知设置"
      ribbon="accent-2"
      aside={
        <div className="settings-section-aside">
          <span className={`badge ${settings.webhook_url_present ? 'badge-ok' : ''}`}>
            {settings.webhook_url_present ? '已配置持久化 Webhook' : '未配置'}
          </span>
          {onToggleExpand && (
            <button type="button" className="btn sm secondary" onClick={onToggleExpand}>
              {isExpanded ? '收起' : '编辑'}
            </button>
          )}
        </div>
      }
    >
      {isExpanded && innerContent}
    </DetailSection>
  )
}
