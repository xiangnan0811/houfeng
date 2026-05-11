import { EventList } from '../../components/EventList'
import { IncidentList } from '../../components/IncidentList'
import { Card } from '../../components/atoms/Card'
import { Button } from '../../components/atoms/Button'
import { Drawer, MonoDigits, Tabs } from '../../components/atoms'
import type { ActiveIncidentRecord, StateChangeEventRecord, TargetRecord } from '../../lib/types'
import { HISTORY_TAB_ITEMS } from './targetDetailConstants'
import type { HistoryTab } from './types'

type TargetHistoryDrawerProps = {
  target: TargetRecord
  open: boolean
  tab: HistoryTab
  events: StateChangeEventRecord[]
  eventsError: string | null
  historyIncidents: ActiveIncidentRecord[] | null
  historyIncidentsLoading: boolean
  historyIncidentsError: string | null
  onClose: () => void
  onTabChange: (tab: HistoryTab) => void
  onRetryHistoryIncidents: () => void
}

export function TargetHistoryDrawer({
  target,
  open,
  tab,
  events,
  eventsError,
  historyIncidents,
  historyIncidentsLoading,
  historyIncidentsError,
  onClose,
  onTabChange,
  onRetryHistoryIncidents,
}: TargetHistoryDrawerProps) {
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={`${target.name} · 历史`}
      ariaLabel="目标历史抽屉"
    >
      <Tabs<HistoryTab>
        variant="pill"
        value={tab}
        onChange={onTabChange}
        items={HISTORY_TAB_ITEMS}
      />
      {tab === 'events' ? (
        eventsError ? (
          <div className="empty-state">
            <h3>事件时间线暂不可用</h3>
            <p>{eventsError}</p>
          </div>
        ) : events.length === 0 ? (
          <div className="empty-state">
            <h3>近期无状态变更事件</h3>
            <p>该目标近期没有发生过被记录的状态变更事件。</p>
          </div>
        ) : (
          <EventList events={events} />
        )
      ) : historyIncidentsLoading ? (
        <p>正在加载历史异常…</p>
      ) : historyIncidentsError ? (
        <Card cardRole="warning">
          <p>
            加载历史异常失败：<MonoDigits>{historyIncidentsError}</MonoDigits>
          </p>
          <Button variant="secondary" size="sm" onClick={onRetryHistoryIncidents}>
            重试
          </Button>
        </Card>
      ) : historyIncidents && historyIncidents.length > 0 ? (
        <IncidentList
          incidents={historyIncidents}
          emptyTitle="近期无异常发生"
          emptyDescription="该目标近期没有触发过被记录的异常。"
        />
      ) : (
        <div className="empty-state">
          <h3>近期无异常发生</h3>
          <p>该目标近期没有触发过被记录的异常。</p>
        </div>
      )}
    </Drawer>
  )
}
