import { Link } from 'react-router-dom'

import { Badge, Button, MonoDigits } from '../../components/atoms'

type TargetsSupportSurfaceProps = {
  totalTargetCount: number
  displayedTargetCount: number
  abnormalTargetCount: number
  pausedTargetCount: number
  archivedTargetCount: number
  executionLabelCount: number
  serviceTargetCount: number
  hasActiveFilters: boolean
  onAbnormalClick: () => void
  onPausedClick: () => void
  onArchivedClick: () => void
}

export function TargetsSupportSurface({
  totalTargetCount,
  displayedTargetCount,
  abnormalTargetCount,
  pausedTargetCount,
  archivedTargetCount,
  executionLabelCount,
  serviceTargetCount,
  hasActiveFilters,
  onAbnormalClick,
  onPausedClick,
  onArchivedClick,
}: TargetsSupportSurfaceProps) {
  const inactiveCount = pausedTargetCount + archivedTargetCount

  return (
    <section className="page-panel observability-support observability-support--targets">
      <div className="observability-support__header">
        <div>
          <p className="observability-support__eyebrow">ENTRY OBSERVABILITY</p>
          <h2 className="observability-support__title">服务入口支撑</h2>
          <p className="observability-support__description">
            Target 页面用于核对服务入口和探测覆盖，帮助判断资产暴露面与可用性风险。
          </p>
        </div>
        <div className="observability-support__scope" aria-label="当前目标筛选范围">
          <span>{hasActiveFilters ? '当前筛选' : '入口库存'}</span>
          <strong>
            <MonoDigits>{displayedTargetCount}</MonoDigits>
            <small>/</small>
            <MonoDigits>{totalTargetCount}</MonoDigits>
          </strong>
        </div>
      </div>

      <div className="observability-support__grid" aria-label="目标观测证据摘要">
        <article className="observability-support-lane observability-support-lane--alert">
          <div className="observability-support-lane__head">
            <span>异常入口</span>
            <Badge variant="count" tone={abnormalTargetCount > 0 ? 'alert' : 'normal'}>
              <MonoDigits>{abnormalTargetCount}</MonoDigits>
            </Badge>
          </div>
          <p>异常目标代表服务入口或跨地域探测路径正在影响资产判断。</p>
          <div className="observability-support-lane__actions">
            <Button
              variant="secondary"
              size="sm"
              onClick={onAbnormalClick}
              disabled={abnormalTargetCount === 0}
            >
              仅看异常
            </Button>
            <Link className="observability-support-link" to="/events?object_type=target&severity=严重">
              严重事件
            </Link>
          </div>
        </article>

        <article className="observability-support-lane observability-support-lane--maintenance">
          <div className="observability-support-lane__head">
            <span>暂停 / 归档</span>
            <Badge variant="count" tone={inactiveCount > 0 ? 'maintenance' : 'normal'}>
              <MonoDigits>{inactiveCount}</MonoDigits>
            </Badge>
          </div>
          <p>暂停和归档解释观测缺口，也帮助区分真实故障与主动下线。</p>
          <div className="observability-support-lane__actions">
            <Button
              variant="secondary"
              size="sm"
              onClick={onPausedClick}
              disabled={pausedTargetCount === 0}
            >
              暂停目标
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={onArchivedClick}
              disabled={archivedTargetCount === 0}
            >
              已归档
            </Button>
          </div>
        </article>

        <article className="observability-support-lane observability-support-lane--normal">
          <div className="observability-support-lane__head">
            <span>执行覆盖</span>
            <Badge variant="count" tone={executionLabelCount > 0 ? 'normal' : 'notice'}>
              <MonoDigits>{executionLabelCount}</MonoDigits>
            </Badge>
          </div>
          <p>执行节点标签决定探测从哪里发出，是服务入口可用性证据的边界。</p>
          <div className="observability-support-lane__actions">
            <Link className="observability-support-link" to="/nodes">
              节点覆盖
            </Link>
            <Link className="observability-support-link" to="/events?object_type=target&time_range=24h">
              24h 事件
            </Link>
          </div>
        </article>

        <article className="observability-support-lane observability-support-lane--asset">
          <div className="observability-support-lane__head">
            <span>资产服务上下文</span>
            <Badge variant="count" tone="neutral">
              <MonoDigits>{serviceTargetCount}</MonoDigits>
            </Badge>
          </div>
          <p>服务记录回到 VPS 详情核对，Target 保留入口观测证据和探测覆盖。</p>
          <div className="observability-support-lane__actions">
            <Link className="observability-support-link" to="/vps">
              VPS 台账
            </Link>
            <Link className="observability-support-link" to="/asset-decisions">
              资产决策
            </Link>
          </div>
        </article>
      </div>
    </section>
  )
}
