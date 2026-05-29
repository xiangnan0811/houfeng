import { DetailSection } from '../../components/DetailSection'
import { MonoDigits, Toggle } from '../../components/atoms'
import type { SettingsRecord } from '../../lib/types'
import type { SettingsFormState } from './types'

type TelegramSettingsForm = Pick<
  SettingsFormState,
  'telegramBotToken' | 'telegramChatId' | 'telegramRuntimeManaged'
>

type TelegramSettingsSectionProps = {
  settings: SettingsRecord['telegram']
  form: TelegramSettingsForm
  onChange: (patch: Partial<TelegramSettingsForm>) => void
  wrapper?: 'detail' | 'none'
  isExpanded?: boolean
  onToggleExpand?: () => void
}

export function TelegramSettingsSection({
  settings,
  form,
  onChange,
  wrapper = 'detail',
  isExpanded = true,
  onToggleExpand,
}: TelegramSettingsSectionProps) {
  const innerContent = (
    <>
      <div className="settings-row">
        <span className="sr-label">运行时接管</span>
        <Toggle
          label="运行时接管"
          checked={form.telegramRuntimeManaged}
          onChange={(checked) => onChange({ telegramRuntimeManaged: checked })}
        />
      </div>
      <div className="settings-row">
        <span className="sr-label">Bot Token</span>
        <span className="sr-value">
          <input
            className="input input--compact"
            aria-label="新的 Telegram Bot Token"
            type="password"
            autoComplete="off"
            value={form.telegramBotToken}
            onChange={(e) => onChange({ telegramBotToken: e.target.value })}
          />
        </span>
      </div>
      <div className="settings-row">
        <span className="sr-label">Chat ID</span>
        <span className="sr-value">
          <input
            className="input input--compact"
            aria-label="Telegram Chat ID"
            value={form.telegramChatId}
            onChange={(e) => onChange({ telegramChatId: e.target.value })}
          />
        </span>
      </div>
      <div className="settings-row">
        <span className="sr-label">当前持久化状态</span>
        <span className="sr-value">
          {settings.token_present && settings.token_masked_summary ? (
            <>已配置 Telegram Bot Token：<MonoDigits>{settings.token_masked_summary}</MonoDigits></>
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
      eyebrow="Telegram"
      title="Telegram 通知设置"
      ribbon="accent-2"
      aside={
        <div className="settings-section-aside">
          <span className={`badge ${settings.token_present ? 'badge-ok' : ''}`}>
            {settings.token_present ? '已配置持久化 Token' : '未配置'}
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
