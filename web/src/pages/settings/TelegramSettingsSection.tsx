import { DetailSection } from '../../components/DetailSection'
import { MonoDigits } from '../../components/atoms'
import type { SettingsRecord } from '../../lib/types'
import type { SettingsFormState } from './types'
import { SectionIntro } from './SectionIntro'

type TelegramSettingsForm = Pick<
  SettingsFormState,
  'telegramBotToken' | 'telegramChatId' | 'telegramRuntimeManaged'
>

type TelegramSettingsSectionProps = {
  settings: SettingsRecord['telegram']
  form: TelegramSettingsForm
  onChange: (patch: Partial<TelegramSettingsForm>) => void
}

export function TelegramSettingsSection({ settings, form, onChange }: TelegramSettingsSectionProps) {
  return (
    <DetailSection
      eyebrow="Telegram"
      title="Telegram 通知设置"
      aside={settings.token_present ? '已存在持久化 Token' : '当前未配置'}
    >
      <div className="summary-grid">
        <label className="summary-card">
          <span className="summary-card__label">新的 Telegram Bot Token</span>
          <input
            aria-label="新的 Telegram Bot Token"
            type="password"
            autoComplete="off"
            value={form.telegramBotToken}
            onChange={(event) => onChange({ telegramBotToken: event.target.value })}
          />
        </label>

        <label className="summary-card">
          <span className="summary-card__label">Telegram Chat ID</span>
          <input
            aria-label="Telegram Chat ID"
            value={form.telegramChatId}
            onChange={(event) => onChange({ telegramChatId: event.target.value })}
          />
        </label>

        <article className="summary-card">
          <span className="summary-card__label">当前持久化状态</span>
          <strong className="summary-card__value summary-card__value--text">
            {settings.token_present && settings.token_masked_summary ? (
              <>
                已配置 Telegram Bot Token：
                <MonoDigits>{settings.token_masked_summary}</MonoDigits>
              </>
            ) : (
              '当前未保存 Telegram Bot Token'
            )}
          </strong>
        </article>

        <label className="summary-card">
          <span className="summary-card__label">运行时接管</span>
          <input
            aria-label="使用持久化 Telegram 配置接管运行中的通知器"
            type="checkbox"
            checked={form.telegramRuntimeManaged}
            onChange={(event) => onChange({ telegramRuntimeManaged: event.target.checked })}
          />
        </label>
      </div>

      <SectionIntro>
        {!settings.runtime_managed
          ? '当前仅保存 Telegram 持久化配置，尚未驱动正在运行的通知器。'
          : settings.runtime_apply_active
            ? '当前持久化配置已接入正在运行的通知路径。'
            : '当前持久化配置正在接管通知路径，并已显式停用 Telegram 投递。'}
      </SectionIntro>
      <SectionIntro>
        接口不会回显明文 Token。留空会继续保留当前已保存的 Token；只有在需要替换时才输入新的 Token。
      </SectionIntro>
    </DetailSection>
  )
}
