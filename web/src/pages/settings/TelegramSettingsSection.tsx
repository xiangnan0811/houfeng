import { DetailSection } from '../../components/DetailSection'
import { MonoDigits, Input, Toggle } from '../../components/atoms'
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
  const innerContentClass = [
    'settings-section-body',
    wrapper === 'none' && 'settings-section-body--modal',
  ].filter(Boolean).join(' ')
  const innerContent = (
    <div className={innerContentClass}>
      <div className="settings-form-grid">
        <Input
          label="新的 Telegram Bot Token"
          type="password"
          autoComplete="off"
          value={form.telegramBotToken}
          onChange={(event) => onChange({ telegramBotToken: event.target.value })}
        />

        <Input
          label="Telegram Chat ID"
          value={form.telegramChatId}
          onChange={(event) => onChange({ telegramChatId: event.target.value })}
        />

        <article className="summary-card settings-summary-card--wide">
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

        <div className="settings-fieldset settings-fieldset--wide">
          <Toggle
            label="运行时接管"
            checked={form.telegramRuntimeManaged}
            onChange={(checked) => onChange({ telegramRuntimeManaged: checked })}
          />
          <SectionIntro>使用持久化 Telegram 配置接管运行中的通知器</SectionIntro>
        </div>
      </div>
      <div className="settings-section-notes">
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
      </div>
    </div>
  )

  if (wrapper === 'none') {
    return innerContent
  }

  return (
    <DetailSection
      eyebrow="Telegram"
      title="Telegram 通知设置"
      aside={
        <div className="settings-section-aside">
          <span className={`badge ${settings.token_present ? 'badge--success' : ''}`}>
            {settings.token_present ? '已配置持久化 Token' : '未配置'}
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
