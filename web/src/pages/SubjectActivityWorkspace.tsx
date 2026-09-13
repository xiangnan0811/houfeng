import { useMemo } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'

import { Button } from '../components/atoms'
import { PageState } from '../components/PageState'
import { SubjectIdentityBar } from '../components/SubjectIdentityBar'
import { SUBJECT_KIND_LABELS } from '../components/timelineChannel'
import { UnifiedTimeline } from '../components/UnifiedTimeline'
import type { SubjectActivityView } from '../lib/types'
import { SubjectActivityFilters } from './records/activity/SubjectActivityFilters'
import { SubjectLocalNavigation } from './records/activity/SubjectLocalNavigation'
import { comparisonEntryHref } from './records/compare/comparisonQueryState'
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

function workspaceCopy(view: SubjectActivityView, filtered: boolean) {
  if (view === 'records') {
    return {
      loading: '正在加载记录',
      unavailable: '记录投影不可用',
      error: '无法加载记录',
      emptyTitle: filtered ? '没有匹配的记录' : '主体尚无记录',
      emptyDescription: filtered
        ? '当前筛选条件下没有可见记录，可放宽来源或类型。'
        : '投影中还没有与该主体相关的可见记录。',
      refresh: '有新记录，刷新',
    }
  }
  if (view === 'evidence') {
    return {
      loading: '正在加载证据',
      unavailable: '证据投影不可用',
      error: '无法加载证据',
      emptyTitle: filtered ? '没有匹配的证据' : '主体尚无证据',
      emptyDescription: filtered
        ? '当前筛选条件下没有可见证据，可放宽来源或版本范围。'
        : '投影中还没有与该主体相关的可见证据。',
      refresh: '有新证据，刷新',
    }
  }
  return {
    loading: '正在加载活动',
    unavailable: '活动投影不可用',
    error: '无法加载活动',
    emptyTitle: filtered ? '没有匹配的活动' : '主体尚无活动',
    emptyDescription: filtered
      ? '当前筛选条件下没有可见活动，可放宽来源或时间范围。'
      : '投影中还没有与该主体相关的可见事件。',
    refresh: '有新活动，刷新',
  }
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
      <div className="page">
        <PageState
          kind="error"
          title="未找到主体活动页"
          description="仅支持 VPS、监控实例与入口探测的活动 / 记录 / 证据路由。"
        />
      </div>
    )
  }

  const navigationState = route.kind === 'vps' ? location.state : undefined
  const writeFilters = (next: ActivityFilters) => {
    // Filter changes clear the cursor so a watermark from another query is never reused.
    setSearchParams(subjectActivityParamsFromState(next), { replace: true, state: navigationState })
  }

  const overviewHref = route.kind === 'vps' ? route.basePath : undefined
  const newRecordHref = subjectNewRecordHref(route)
  const filterSearch = subjectActivityParamsFromState(filters).toString()
  const navSearch = filterSearch ? `?${filterSearch}` : ''
  const copy = workspaceCopy(view, Object.keys(filters).length > 0)

  return (
    <div className="page subject-activity-page">
      {state.subject ? (
        <SubjectIdentityBar
          subject={state.subject}
          returnHref={route.basePath}
          returnLabel="返回详情"
          actions={(
            <>
              {view === 'evidence' ? (
                <Link className="btn sm secondary" to="/records/compare" state={navigationState}>横向比较</Link>
              ) : null}
              <Link className="btn sm primary" to={newRecordHref} state={navigationState}>新建记录</Link>
              {state.freshness?.new_items_available ? (
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  onClick={() => commands.refresh()}
                >
                  {copy.refresh}
                </Button>
              ) : null}
            </>
          )}
        />
      ) : (
        <header className="page__head subject-identity-bar">
          <div className="subject-identity-bar__main">
            <p className="subject-identity-bar__kind">{SUBJECT_KIND_LABELS[route.kind]}</p>
            <h1 className="page__title">{route.sourceId}</h1>
          </div>
        </header>
      )}

      <SubjectLocalNavigation
        subject={route}
        activeView={view}
        {...(overviewHref ? { overviewHref } : {})}
        search={navSearch}
      />

      <div className="subject-activity-page__work">
        <SubjectActivityFilters
          value={filters}
          onChange={writeFilters}
          disabled={state.status === 'loading'}
          view={view}
        />

        {state.sourceStatuses.some((status) => status.state !== 'ready') ? (
          <p className="subject-activity-page__source-note" role="status">
            部分来源暂不可用；时间线只包含已知条目，不代表完整投影。
          </p>
        ) : null}

        {state.status === 'loading' && state.items.length === 0 ? (
          <PageState kind="loading" title={copy.loading} />
        ) : null}

        {state.status === 'unavailable' ? (
          <PageState
            kind="error"
            title={copy.unavailable}
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
            title={copy.error}
            description={state.errorMessage ?? undefined}
            technicalSummary={state.errorCode}
            action={(
              <Button
                type="button"
                size="sm"
                onClick={() => {
                  setSearchParams(subjectActivityParamsFromState(filters), { replace: true, state: navigationState })
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
            title={copy.emptyTitle}
            description={copy.emptyDescription}
          />
        ) : null}

        {state.status === 'ready' || (state.status === 'error' && state.items.length > 0) ? (
          <>
            <UnifiedTimeline
              items={state.items}
              sourceStatuses={state.sourceStatuses}
              emptyTitle={copy.emptyTitle}
              emptyDescription={copy.emptyDescription}
              {...(view === 'evidence' ? {
                itemActions: (item) => item.evidence_snapshot_id ? (
                  <Link
                    className="text-link"
                    to={comparisonEntryHref({ items: [{ snapshot_id: item.evidence_snapshot_id }] })}
                    state={navigationState}
                  >
                    加入横向比较
                  </Link>
                ) : null,
              } : {})}
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
    </div>
  )
}
