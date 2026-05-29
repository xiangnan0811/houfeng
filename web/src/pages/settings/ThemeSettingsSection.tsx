import { Tabs } from '../../components/atoms'
import { useThemeOptional, type Mode, type Preset } from '../../lib/theme-context'

const PRESET_TABS = [
  { value: 'houfeng' as const, label: '候风原色' },
  { value: 'classic' as const, label: '经典' },
]

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
      <div className="ss-desc">本地浏览器偏好，不影响其他操作员</div>
      <div className="settings-row">
        <span className="sr-label">风格</span>
        <Tabs<Preset>
          variant="pill"
          value={preset}
          onChange={setPreset}
          items={PRESET_TABS}
        />
      </div>
      <div className="settings-row">
        <span className="sr-label">明暗</span>
        <Tabs<Mode>
          variant="pill"
          value={mode}
          onChange={setMode}
          items={MODE_TABS}
        />
      </div>
    </>
  )
}
