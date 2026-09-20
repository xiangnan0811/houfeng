import { MonoDigits } from '../../components/atoms'
import { PAGE_SIZE } from './eventsPageConstants'
import { eventPageRange, visiblePageItems } from './eventsPagination'

type EventsStreamPaginationProps = {
  page: number
  total: number
  exhausted: boolean
  loadingMore: boolean
  loadMoreError: string | null
  onPageChange: (page: number) => void
  onLoadMore: () => void
}

export function EventsStreamPagination({
  page,
  total,
  exhausted,
  loadingMore,
  loadMoreError,
  onPageChange,
  onLoadMore,
}: EventsStreamPaginationProps) {
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.min(Math.max(1, page), totalPages)
  const { start, end } = eventPageRange(currentPage, PAGE_SIZE, total)
  const pages = visiblePageItems(currentPage, totalPages)
  const showNav = totalPages > 1
  const showLoadMore = currentPage >= totalPages && !exhausted
  if (total === 0) return null

  return (
    <nav className="events-stream__pager" aria-label="事件分页">
      <p className="events-stream__pager-summary" aria-live="polite">
        第 <MonoDigits>{start}</MonoDigits>–<MonoDigits>{end}</MonoDigits> 条
        {exhausted ? (
          <>，共 <MonoDigits>{total}</MonoDigits> 条</>
        ) : (
          <>，已加载 <MonoDigits>{total}</MonoDigits> 条</>
        )}
      </p>
      {showNav ? (
        <div className="events-stream__pager-nav">
          <button
            type="button"
            className="btn sm ghost"
            disabled={currentPage <= 1}
            onClick={() => onPageChange(currentPage - 1)}
          >
            上一页
          </button>
          {pages.map((item, index) =>
            item === 'ellipsis' ? (
              <span key={`ellipsis-${index}`} className="events-stream__ellipsis" aria-hidden>
                …
              </span>
            ) : (
              <button
                key={item}
                type="button"
                className={item === currentPage ? 'btn sm ghost events-stream__page is-current' : 'btn sm ghost events-stream__page'}
                aria-label={`第 ${item} 页`}
                aria-current={item === currentPage ? 'page' : undefined}
                onClick={() => {
                  if (item !== currentPage) onPageChange(item)
                }}
              >
                <MonoDigits>{item}</MonoDigits>
              </button>
            ),
          )}
          <button
            type="button"
            className="btn sm ghost"
            disabled={currentPage >= totalPages}
            onClick={() => onPageChange(currentPage + 1)}
          >
            下一页
          </button>
        </div>
      ) : null}
      {showLoadMore ? (
        <div className="events-stream__pager-more">
          <button
            type="button"
            className="btn sm secondary"
            disabled={loadingMore}
            onClick={onLoadMore}
          >
            {loadingMore ? '正在加载后续…' : '加载后续'}
          </button>
          {loadMoreError ? (
            <p className="events-stream__pager-error" role="alert">
              {loadMoreError}
            </p>
          ) : (
            <span className="events-stream__pager-hint">当前窗口还有未载入的事件</span>
          )}
        </div>
      ) : null}
    </nav>
  )
}
