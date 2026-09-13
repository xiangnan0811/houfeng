import { Link, useLocation } from 'react-router-dom'

import { Button } from '../../components/atoms'
import { VPSCopyValueButton } from './VPSCopyValueButton'
import type { ResourceState } from './hooks/useVPSDetailResources'

type Props<T> = {
  state: ResourceState<T>
  groupLabel: string
  loadingLabel: string
  emptyLabel: string
  retryLabel: string
  getKey: (item: T) => string
  getName: (item: T) => string
  getId?: (item: T) => string
  getSummary?: (item: T) => string
  getType?: (item: T) => string
  getStatus?: (item: T) => string
  getAddress?: (item: T) => string
  getCopyValue?: (item: T) => string
  getCopyLabel?: (item: T) => string
  getHref?: (item: T) => string | null
  getDetailsHref?: (item: T) => string | null
  detailsLabel?: string
  onRetry?: () => void
  onOpenDetails?: () => void
}

export function VPSDetailResourceList<T>({
  state,
  groupLabel,
  loadingLabel,
  emptyLabel,
  retryLabel,
  getKey,
  getName,
  getId,
  getSummary,
  getType,
  getStatus,
  getAddress,
  getCopyValue,
  getCopyLabel,
  getHref,
  getDetailsHref,
  detailsLabel = '详情',
  onRetry,
  onOpenDetails,
}: Props<T>) {
  const location = useLocation()
  const pendingEmpty = state.status === 'loading' && state.items.length === 0
  const failedEmpty = state.status === 'error' && state.items.length === 0
  const readyEmpty = state.status === 'ready' && state.items.length === 0

  return (
    <div
      className={readyEmpty ? 'vps-detail-resource-group vps-detail-resource-group--empty' : 'vps-detail-resource-group'}
      aria-label={groupLabel || retryLabel}
    >
      {groupLabel && !readyEmpty ? <p className="vps-detail-resource-group__label">{groupLabel}</p> : null}

      {state.status === 'error' ? (
        <div className="vps-detail-resource-group__error" role="status">
          <p>{state.error || `${retryLabel}暂不可用，请稍后重试。`}</p>
          {onRetry ? (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              aria-label={`重试 ${retryLabel}`}
              onClick={onRetry}
            >
              重试
            </Button>
          ) : null}
        </div>
      ) : null}
      {pendingEmpty ? (
        <p className="vps-detail-resource-group__status">{loadingLabel}</p>
      ) : failedEmpty ? null : readyEmpty ? (
        emptyLabel ? (
          <p className="vps-detail-resource-group__empty">
            {groupLabel ? <span className="vps-detail-resource-group__label">{groupLabel}</span> : null}
            <span>{emptyLabel}</span>
          </p>
        ) : null
      ) : (
        <ul className="vps-detail-resource-list">
          {state.items.map((item) => {
            const name = getName(item)
            const id = getId?.(item)?.trim() ?? ''
            const type = getType?.(item) ?? ''
            const status = getStatus?.(item) ?? ''
            const address = getAddress?.(item) ?? ''
            const summary = getSummary?.(item) ?? ''
            const showType = Boolean(type) && type !== groupLabel
            const copyValue = getCopyValue?.(item)?.trim() ?? ''
            const href = getHref?.(item) ?? null
            const detailsHref = getDetailsHref?.(item) ?? null
            return (
              <li key={getKey(item)} className="vps-detail-resource">
                <div className="vps-detail-resource__primary">
                  <div className="vps-detail-resource__identity">
                    <span className="vps-detail-resource__name">{name}</span>
                    {id ? <span className="vps-detail-resource__id mono">{id}</span> : null}
                  </div>
                  {status ? <span className="vps-detail-resource__status">{status}</span> : null}
                  {detailsHref ? (
                    <Link className="btn sm ghost" to={detailsHref} state={location.state}>{detailsLabel}</Link>
                  ) : onOpenDetails ? (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      aria-label={`查看关联${retryLabel}：${name}`}
                      onClick={onOpenDetails}
                    >
                      {detailsLabel}
                    </Button>
                  ) : null}
                </div>
                {address || showType || summary ? (
                  <div className="vps-detail-resource__secondary">
                    {address ? (
                      <span className="vps-detail-resource__address">
                        {href ? (
                          <a className="text-link" href={href} target="_blank" rel="noreferrer">{address}</a>
                        ) : address}
                        {copyValue ? (
                          <VPSCopyValueButton value={copyValue} label={getCopyLabel?.(item) || groupLabel || retryLabel} />
                        ) : null}
                      </span>
                    ) : null}
                    {showType ? <span className="vps-detail-resource__details">{type}</span> : null}
                    {summary ? <span className="vps-detail-resource__details">{summary}</span> : null}
                  </div>
                ) : null}
              </li>
            )
          })}
        </ul>
      )}
      {state.status === 'loading' && state.items.length > 0 ? (
        <p className="vps-detail-resource-group__status">{loadingLabel}</p>
      ) : null}
    </div>
  )
}
