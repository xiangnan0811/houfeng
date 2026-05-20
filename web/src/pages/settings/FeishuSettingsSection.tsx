import { DetailSection } from '../../components/DetailSection'
import { Input, Toggle } from '../../components/atoms'
import type { SettingsFormState } from './types'
import { SectionIntro } from './SectionIntro'

type FeishuSettingsForm = Pick<SettingsFormState, 'feishuEnabled' | 'feishuWebhookUrl'>

type FeishuSettingsSectionProps = {
  form: FeishuSettingsForm
  onChange: (patch: Partial<FeishuSettingsForm>) => void
  wrapper?: 'detail' | 'none'
  isExpanded?: boolean
  onToggleExpand?: () => void
}

export function FeishuSettingsSection({
  form,
  onChange,
  wrapper = 'detail',
  isExpanded = true,
  onToggleExpand,
}: FeishuSettingsSectionProps) {
  const innerContentClass = [
    'settings-section-body',
    wrapper === 'none' && 'settings-section-body--modal',
  ].filter(Boolean).join(' ')
  const innerContent = (
    <div className={innerContentClass}>
      <div className="settings-form-grid">
        <Input
          label="Webhook URL"
          type="text"
          placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..."
          value={form.feishuWebhookUrl}
          onChange={(event) => onChange({ feishuWebhookUrl: event.target.value })}
        />

        <div className="settings-fieldset settings-fieldset--wide">
          <Toggle
            label="启用飞书通知"
            checked={form.feishuEnabled}
            onChange={(checked) => onChange({ feishuEnabled: checked })}
          />
        </div>
      </div>

      <div className="settings-section-notes">
        <SectionIntro>
          {form.feishuEnabled && form.feishuWebhookUrl.trim()
            ? '飞书通知已启用，incident 发生时将同时通过飞书群机器人推送消息。'
            : '当前未配置飞书通知。填写 Webhook URL 并启用后，incident 推送将同时投递到飞书群。'}
        </SectionIntro>
      </div>
    </div>
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
          <span className={`badge ${form.feishuEnabled ? 'badge--success' : ''}`}>
            {form.feishuEnabled && form.feishuWebhookUrl.trim() ? '已配置' : '未配置'}
          </span>
          {onToggleExpand && (
            <button type="button" className="btn btn--secondary btn--sm" onClick={onToggleExpand}>
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
