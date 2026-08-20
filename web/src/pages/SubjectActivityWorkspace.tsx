import { useMemo } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'

import { Button } from '../components/atoms'
import { PageState } from '../components/PageState'
import { SubjectIdentityBar } from '../components/SubjectIdentityBar'
import { UnifiedTimeline } from '../components/UnifiedTimeline'
import type { SubjectActivityView } from '../lib/types'
import { SubjectActivityFilters } from './records/activity/SubjectActivityFilters'
import { SubjectLocalNavigation } from './records/activity/SubjectLocalNavigation'
import {
  parseSubjectActivityRoute,
  subjectActivityCursorFromSearchParams,
  subjectActivityFiltersFromSearchParams,
  subjectActivityParamsFromState,
  subjectNewRecordHref,
  type SubjectActivityFilters as ActivityFilters,
} from './records/activity/activityQueryState'
import { useSubjectActivity } from './records/activity/useSubjectActivity'

type Props = {
  view: SubjectActivityView
}

export function SubjectActivityWorkspace({ view }: Props) {
  const location = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const route = useMemo(
    () => parseSubjectActivityRoute(location.pathname),
    [location.pathname],
  )

  const filters = useMemo(
    () => subjectActivityFiltersFromSearchParams(searchParams),
    [searchParams],
  )
  const cursor = useMemo(
    () => subjectActivityCursorFromSearchParams(searchParams),
    [searchParams],
  )

  const subject = route ?? {
    kind: 'vps' as const,
    sourceId: '',
    view,
    basePath: '',
  }

  const { state, commands } = useSubjectActivity({
    kind: subject.kind,
    sourceId: subject.sourceId,
    view,
    filters,
    ...(cursor ? { cursor } : {}),
  })

  if (!route || route.view !== view) {
    return (
      <div className="page-stack">
        <PageState
          kind="error"
          title="未找到主体活动页"
          description="仅支持 VPS、监控实例与入口探测的活动 / 记录 / 证据路由。"
        />
      </div>
    )
  }

  const writeFilters = (next: ActivityFilters) => {
    // Filter changes clear the cursor so a watermark from another query is never reused.
    setSearchParams(subjectActivityParamsFromState(next), { replace: true })
  }

  const overviewHref = route.kind === 'vps' ? route.basePath : undefined
  const newRecordHref = subjectNewRecordHref(route)
  const filterSearch = subjectActivityParamsFromState(filters).toString()
  const navSearch = filterSearch ? `?${filterSearch}` : ''

  const emptyTitle = state.status === 'empty' && Object.keys(filters).length > 0
    ? '没有匹配的活动'
    : '主体尚无活动'
  const emptyDescription = state.status === 'empty' && Object.keys(filters).length > 0
    ? '当前筛选条件下没有可见活动，可放宽来源或时间范围。'
    : '投影中还没有与该主体相关的可见事件。'

  return (
    <div className="page-stack subject-activity-page">
      {state.subject ? (
        <SubjectIdentityBar
          subject={state.subject}
          returnHref={route.basePath}
          returnLabel="返回详情"
          actions={(
            <>
              <Link className="btn sm primary" to={newRecordHref}>新建记录</Link>
              {state.freshness?.new_items_available ? (
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  onClick={() => commands.refresh()}
                >
                  有新活动，刷新
                </Button>
              ) : null}
            </>
          )}
        />
      ) : (
        <header className="subject-identity-bar subject-identity-bar--pending">
          <h1 className="subject-identity-bar__title">{route.sourceId}</h1>
        </header>
      )}

      <SubjectLocalNavigation
        subject={route}
        activeView={view}
        {...(overviewHref ? { overviewHref } : {})}
        search={navSearch}
      />

      <SubjectActivityFilters
        value={filters}
        onChange={writeFilters}
        disabled={state.status === 'loading'}
      />

      {state.sourceStatuses.some((status) => status.state !== 'ready') ? (
        <p className="subject-activity-page__source-note" role="status">
          部分来源暂不可用；时间线只包含已知条目，不代表完整投影。
        </p>
      ) : null}

      {state.status === 'loading' && state.items.length === 0 ? (
        <PageState kind="loading" title="正在加载活动" />
      ) : null}

      {state.status === 'unavailable' ? (
        <PageState
          kind="error"
          title="活动投影不可用"
          description={state.errorMessage ?? undefined}
          action={(
            <Button type="button" size="sm" onClick={() => commands.refresh()}>
              重试
            </Button>
          )}
        />
      ) : null}

      {state.status === 'error' ? (
        <PageState
          kind="error"
          title="无法加载活动"
          description={state.errorMessage ?? undefined}
          technicalSummary={state.errorCode}
          action={(
            <Button
              type="button"
              size="sm"
              onClick={() => {
                setSearchParams(subjectActivityParamsFromState(filters), { replace: true })
                commands.refresh()
              }}
            >
              重置分页并重试
            </Button>
          )}
        />
      ) : null}

      {state.status === 'empty' ? (
        <PageState
          kind="empty"
          title={emptyTitle}
          description={emptyDescription}
        />
      ) : null}

      {state.status === 'ready' || (state.status === 'error' && state.items.length > 0) ? (
        <>
          <UnifiedTimeline
            items={state.items}
            sourceStatuses={state.sourceStatuses}
            emptyTitle={emptyTitle}
            emptyDescription={emptyDescription}
          />
          {state.nextCursor ? (
            <div className="subject-activity-page__more">
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={state.loadingMore}
                onClick={() => commands.append()}
              >
                {state.loadingMore ? '加载中…' : '加载更多'}
              </Button>
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
