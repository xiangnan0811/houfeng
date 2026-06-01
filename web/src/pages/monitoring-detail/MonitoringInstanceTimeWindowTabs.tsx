import { Tabs } from '../../components/atoms'
import { TIME_WINDOW_ITEMS } from './monitoringDetailConstants'
import type { TimeWindow } from './types'

type MonitoringInstanceTimeWindowTabsProps = {
  value: TimeWindow
  onChange: (value: TimeWindow) => void
}

export function MonitoringInstanceTimeWindowTabs({ value, onChange }: MonitoringInstanceTimeWindowTabsProps) {
  return (
    <div className="watchtower-window-tabs">
      <Tabs<TimeWindow>
        variant="pill"
        value={value}
        onChange={onChange}
        items={TIME_WINDOW_ITEMS}
      />
    </div>
  )
}
