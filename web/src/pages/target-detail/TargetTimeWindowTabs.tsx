import { Tabs } from '../../components/atoms'
import { TIME_WINDOW_ITEMS } from './targetDetailConstants'
import type { TimeWindow } from './types'

type TargetTimeWindowTabsProps = {
  value: TimeWindow
  onChange: (value: TimeWindow) => void
}

export function TargetTimeWindowTabs({ value, onChange }: TargetTimeWindowTabsProps) {
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
