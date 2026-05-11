import { Tabs } from '../../components/atoms'
import { TIME_WINDOW_ITEMS } from './nodeDetailConstants'
import type { TimeWindow } from './types'

type NodeTimeWindowTabsProps = {
  value: TimeWindow
  onChange: (value: TimeWindow) => void
}

export function NodeTimeWindowTabs({ value, onChange }: NodeTimeWindowTabsProps) {
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
