import type { ReactNode } from 'react'

import { Tabs, type TabItem } from '../../components/atoms'
import type { MonitoringInstanceQuickView } from './types'

export type QuickViewTabItem = TabItem<MonitoringInstanceQuickView>

export type MonitoringToolbarProps = {
  quickView: MonitoringInstanceQuickView
  quickViews: readonly QuickViewTabItem[]
  navigationLocked?: boolean
  onQuickViewChange: (view: MonitoringInstanceQuickView) => void
  filters: ReactNode
}

export function MonitoringToolbar({
  quickView,
  quickViews,
  navigationLocked = false,
  onQuickViewChange,
  filters,
}: MonitoringToolbarProps) {
  return (
    <div className="monitoring-page__tools">
      <div className="monitoring-page__views">
        <Tabs
          label="关注视图"
          idBase="monitoring-quick-view"
          activation="manual"
          value={quickView}
          onChange={(view) => {
            if (!navigationLocked) {
              onQuickViewChange(view)
            }
          }}
          items={quickViews}
        />
      </div>
      <div className="monitoring-page__controls">
        {filters}
      </div>
    </div>
  )
}
