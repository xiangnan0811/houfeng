import { useSyncExternalStore } from 'react'

import { SegmentedControl } from '../../components/atoms'
import { isDarkOnlyPreset, resolveScheme, subscribeSystemScheme, type Scheme } from '../../lib/theme'
import { useThemeOptional, type Mode, type Preset } from '../../lib/theme-context'

const PRESET_TABS = [
  { value: 'houfeng' as const, label: '候风' },
  { value: 'precision' as const, label: '精密' },
  { value: 'observatory' as const, label: '观测台' },
]

// 预览用各风格自己的主题类限定 token，展示真实配色；仅深色的风格始终以深色预览。
function previewClass(preset: Preset, scheme: Scheme): string {
  return `theme-${preset}-${isDarkOnlyPreset(preset) ? 'dark' : scheme}`
}

function subscribeScheme(onChange: () => void): () => void {
  return subscribeSystemScheme(() => onChange())
}

function PresetPicker({ preset, mode, onChange }: { preset: Preset; mode: Mode; onChange: (next: Preset) => void }) {
  // 跟随系统时订阅系统明暗变化，页面打开期间系统切换也会同步刷新预览。
  const systemScheme = useSyncExternalStore(subscribeScheme, () => resolveScheme('system'), () => 'dark' as const)
  const scheme = mode === 'system' ? systemScheme : mode
  return (
    <div className="theme-presets" role="group" aria-label="主题风格">
      {PRESET_TABS.map((item) => {
        const selected = item.value === preset
        const darkOnly = isDarkOnlyPreset(item.value)
        const noteId = `theme-preset-${item.value}-note`
        return (
          <button
            key={item.value}
            type="button"
            className={selected ? 'theme-preset is-active' : 'theme-preset'}
            aria-label={item.label}
            aria-describedby={darkOnly ? noteId : undefined}
            aria-pressed={selected}
            onClick={() => onChange(item.value)}
          >
            <span className={`theme-preset__preview ${previewClass(item.value, scheme)}`} aria-hidden="true">
              <span className="theme-preset__side" />
              <span className="theme-preset__canvas">
                <span className="theme-preset__panel">
                  <span className="theme-preset__line theme-preset__line--accent" />
                  <span className="theme-preset__line" />
                  <span className="theme-preset__line theme-preset__line--short" />
                </span>
                <span className="theme-preset__states">
                  <span className="theme-preset__dot theme-preset__dot--ok" />
                  <span className="theme-preset__dot theme-preset__dot--warn" />
                  <span className="theme-preset__dot theme-preset__dot--err" />
                </span>
              </span>
            </span>
            {/* 按钮名称来自 aria-label；“仅深色”作为按钮描述暴露给读屏。 */}
            <span className="theme-preset__name">
              {item.label}
              {darkOnly ? <span id={noteId} className="theme-preset__note">仅深色</span> : null}
            </span>
          </button>
        )
      })}
    </div>
  )
}

const MODE_TABS = [
  { value: 'dark' as const, label: '深色' },
  { value: 'light' as const, label: '浅色' },
  { value: 'system' as const, label: '跟随系统' },
]

export function ThemeSettingsSection() {
  const theme = useThemeOptional()
  if (!theme) return null
  const { preset, mode, setPreset, setMode } = theme
  return (
    <>
      <div className="ss-title">主题</div>
      <div className="ss-desc">本地浏览器偏好，选择后立即生效，无需保存，不影响其他操作员</div>
      <div className="settings-row">
        <span className="sr-label">风格</span>
        <PresetPicker preset={preset} mode={mode} onChange={setPreset} />
      </div>
      <div className="settings-row">
        <span className="sr-label">明暗</span>
        <SegmentedControl<Mode>
          label="主题明暗"
          value={mode}
          onChange={setMode}
          items={MODE_TABS}
        />
      </div>
      {isDarkOnlyPreset(preset) ? (
        <p className="ss-desc">观测台仅提供深色；浅色或跟随系统为浅色时使用候风浅色。</p>
      ) : null}
    </>
  )
}
