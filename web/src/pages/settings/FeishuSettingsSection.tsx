import { DetailSection } from '../../components/DetailSection'
import type { SettingsFormState } from './types'
import { SectionIntro } from './SectionIntro'

type FeishuSettingsForm = Pick<SettingsFormState, 'feishuEnabled' | 'feishuWebhookUrl'>

type FeishuSettingsSectionProps = {
  form: FeishuSettingsForm
  onChange: (patch: Partial<FeishuSettingsForm>) => void
}

export function FeishuSettingsSection({ form, onChange }: FeishuSettingsSectionProps) {
  return (
    <DetailSection
      eyebrow="飞书"
      title="飞书通知设置"
      ribbon="accent-2"
      aside={form.feishuEnabled && form.feishuWebhookUrl.trim() ? '已配置' : '未配置'}
    >
      <div className="summary-grid">
        <label className="summary-card">
          <span className="summary-card__label">启用飞书通知</span>
          <input
            aria-label="启用飞书通知"
            type="checkbox"
            checked={form.feishuEnabled}
            onChange={(event) => onChange({ feishuEnabled: event.target.checked })}
          />
        </label>

        <label className="summary-card">
          <span className="summary-card__label">Webhook URL</span>
          <input
            aria-label="飞书 Webhook URL"
            type="text"
            placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..."
            value={form.feishuWebhookUrl}
            onChange={(event) => onChange({ feishuWebhookUrl: event.target.value })}
          />
        </label>
      </div>

      <SectionIntro>
        {form.feishuEnabled && form.feishuWebhookUrl.trim()
          ? '飞书通知已启用，incident 发生时将同时通过飞书群机器人推送消息。'
          : '当前未配置飞书通知。填写 Webhook URL 并启用后，incident 推送将同时投递到飞书群。'}
      </SectionIntro>
    </DetailSection>
  )
}
