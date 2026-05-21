import { DetailSection } from '../../components/DetailSection'
import { Tabs } from '../../components/atoms'
import { useThemeOptional, type Mode, type Preset } from '../../lib/theme-context'
import { SectionIntro } from './SectionIntro'

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
    <DetailSection
      eyebrow="主题"
      title="主题"
      ribbon="notice"
      aside="本地浏览器偏好"
    >
      <div className="settings-cluster">
        <SectionIntro>
          主题只写入当前浏览器的本地偏好，不会提交到中心设置，也不会影响其他操作员或运行时策略。
        </SectionIntro>
        <div className="settings-fieldset">
          <p className="section-heading__eyebrow">风格</p>
          <Tabs<Preset>
            variant="pill"
            value={preset}
            onChange={setPreset}
            items={PRESET_TABS}
          />
        </div>
        <div className="settings-fieldset">
          <p className="section-heading__eyebrow">明暗</p>
          <Tabs<Mode>
            variant="pill"
            value={mode}
            onChange={setMode}
            items={MODE_TABS}
          />
        </div>
      </div>
    </DetailSection>
  )
}
