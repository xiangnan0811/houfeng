import { Tabs } from '../../components/atoms'
import { TIME_WINDOW_ITEMS } from './monitoringDetailConstants'
import type { RuntimeStreamStatus, TimeWindow } from './types'

type MonitoringInstanceTimeWindowTabsProps = {
  value: TimeWindow
  onChange: (value: TimeWindow) => void
  streamStatus?: RuntimeStreamStatus
  streamError?: string | null
}

const STREAM_STATUS_LABELS: Record<RuntimeStreamStatus, string> = {
  idle: '已断开',
  connecting: '连接中',
  connected: '已连接',
  reconnecting: '重连中',
  disconnected: '已断开',
}

export function MonitoringInstanceTimeWindowTabs({
  value,
  onChange,
  streamStatus = 'idle',
  streamError,
}: MonitoringInstanceTimeWindowTabsProps) {
  return (
    <div className="watchtower-window-tabs">
      <Tabs<TimeWindow>
        variant="pill"
        value={value}
        onChange={onChange}
        items={TIME_WINDOW_ITEMS}
      />
      {value === 'realtime' ? (
        <span className={`watchtower-stream-status watchtower-stream-status--${streamStatus}`}>
          {STREAM_STATUS_LABELS[streamStatus]}
          {streamError ? ` · ${streamError}` : ''}
        </span>
      ) : null}
    </div>
  )
}
