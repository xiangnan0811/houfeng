import { useId } from 'react'

import { DetailSection } from '../../components/DetailSection'
import { MonoDigits, SegmentedControl } from '../../components/atoms'
import type { ConfigSource, SettingsRecord, TelegramSettingsMode } from '../../lib/types'
import type { SettingsFormState } from './types'

type TelegramSettingsForm = Pick<SettingsFormState, 'telegramBotToken' | 'telegramChatId' | 'telegramMode'>

type TelegramSettingsSectionProps = {
  settings: SettingsRecord['telegram']
  form: TelegramSettingsForm
  onChange: (patch: Partial<TelegramSettingsForm>) => void
  onRequestRestore: () => void
  onExpand?: () => void
  wrapper?: 'detail' | 'none'
  isExpanded?: boolean
  onToggleExpand?: () => void
}

const MODE_ITEMS = [
  { value: 'inherit' as const, label: '继承部署配置' },
  { value: 'override' as const, label: '页面覆盖' },
  { value: 'disabled' as const, label: '停用' },
]

function sourceLabel(source: ConfigSource): string {
  if (source === 'deployment') return '部署配置'
  if (source === 'database') return '页面设置'
  return '程序默认'
}

export function TelegramSettingsSection({
  settings,
  form,
  onChange,
  onRequestRestore,
  onExpand,
  wrapper = 'detail',
  isExpanded = true,
  onToggleExpand,
}: TelegramSettingsSectionProps) {
  const tokenHintId = useId()
  const showCredentials = isExpanded && form.telegramMode === 'override'
  const tokenHint = settings.token_present
    ? '留空保留当前有效 Token'
    : '当前没有可保留的 Token，需要填写新的 Bot Token'

  function chooseMode(next: TelegramSettingsMode) {
    if (next === form.telegramMode) return
    if (next === 'inherit') {
      onRequestRestore()
      return
    }
    onChange({ telegramMode: next, telegramBotToken: '' })
    if (next === 'override') onExpand?.()
  }

  const innerContent = (
    <>
      <p className="telegram-settings__status">
        当前生效：{sourceLabel(settings.source)}，{settings.active ? '已启用' : '未启用'}
        {settings.chat_id ? <>，Chat ID <MonoDigits>{settings.chat_id}</MonoDigits></> : null}
        {settings.token_present && settings.token_masked_summary ? (
          <>，Token <MonoDigits>{settings.token_masked_summary}</MonoDigits></>
        ) : null}
      </p>
      <p className="telegram-settings__baseline">
        {settings.deployment.configured ? (
          <>
            部署基线：已配置
            {settings.deployment.chat_id ? <>，Chat ID <MonoDigits>{settings.deployment.chat_id}</MonoDigits></> : null}
            {settings.deployment.token_present && settings.deployment.token_masked_summary ? (
              <>，Token <MonoDigits>{settings.deployment.token_masked_summary}</MonoDigits></>
            ) : null}
          </>
        ) : '部署基线：未配置'}
      </p>
      <div className="settings-row">
        <span className="sr-label">配置方式</span>
        <span className="sr-value settings-mode-control">
          <SegmentedControl
            label="Telegram 配置方式"
            value={form.telegramMode}
            onChange={chooseMode}
            items={MODE_ITEMS}
          />
          {form.telegramMode !== 'inherit' ? (
            <button type="button" className="btn sm secondary" onClick={onRequestRestore}>
              恢复部署配置
            </button>
          ) : null}
        </span>
      </div>
      {isExpanded && form.telegramMode === 'inherit' ? (
        <p className="telegram-settings__hint">当前沿用部署配置。点编辑可改为页面覆盖，并保留现在生效的 Chat ID。</p>
      ) : null}
      {isExpanded && form.telegramMode === 'disabled' ? (
        <p className="telegram-settings__hint">已停用。保存后不会使用部署配置发送通知，此渠道仍保留在这里以便恢复。</p>
      ) : null}
      {showCredentials ? (
        <>
          <div className="settings-row">
            <span className="sr-label">Bot Token</span>
            <span className="sr-value">
              <input
                className="input settings-secret-input"
                aria-label="新的 Telegram Bot Token"
                aria-describedby={tokenHintId}
                type="password"
                autoComplete="off"
                value={form.telegramBotToken}
                onChange={(event) => onChange({ telegramBotToken: event.target.value })}
              />
            </span>
          </div>
          <p id={tokenHintId} className="telegram-settings__hint">{tokenHint}</p>
          <div className="settings-row">
            <span className="sr-label">Chat ID</span>
            <span className="sr-value">
              <input
                className="input settings-secret-input"
                aria-label="Telegram Chat ID"
                value={form.telegramChatId}
                onChange={(event) => onChange({ telegramChatId: event.target.value })}
              />
            </span>
          </div>
        </>
      ) : null}
    </>
  )

  if (wrapper === 'none') return innerContent

  return (
    <div className="telegram-settings">
      <DetailSection
        eyebrow="Telegram"
        title="Telegram 通知设置"
        ribbon="accent-2"
        aside={
          <div className="settings-section-aside">
            <span className={`badge ${settings.active ? 'badge-ok' : ''}`}>
              {sourceLabel(settings.source)} · {settings.active ? '已启用' : '未启用'}
            </span>
            {onToggleExpand ? (
              <button type="button" className="btn sm secondary" onClick={onToggleExpand}>
                {isExpanded ? '收起' : '编辑'}
              </button>
            ) : null}
          </div>
        }
      >
        {innerContent}
      </DetailSection>
    </div>
  )
}
