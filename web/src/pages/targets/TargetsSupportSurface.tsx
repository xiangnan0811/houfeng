import { Link } from 'react-router-dom'

import { Badge, Button, Hostname, MonoDigits, StatusGlyph } from '../../components/atoms'
import { targetEvidenceGlyphState } from './targetHelpers'
import type { TargetEvidenceItem, TargetEvidenceLead } from './types'

type TargetsSupportSurfaceProps = {
  totalTargetCount: number
  displayedTargetCount: number
  abnormalTargetCount: number
  pausedTargetCount: number
  archivedTargetCount: number
  coverageGapTargetCount: number
  executionLabelCount: number
  serviceTargetCount: number
  evidenceLead: TargetEvidenceLead
  topEvidence: TargetEvidenceItem | null
  filterContext: string[]
  hasActiveFilters: boolean
  onAbnormalClick: () => void
  onPausedClick: () => void
  onArchivedClick: () => void
  onCoverageClick: () => void
  onClearFilters: () => void
  onCreateClick: () => void
}

export function TargetsSupportSurface({
  totalTargetCount,
  displayedTargetCount,
  abnormalTargetCount,
  pausedTargetCount,
  archivedTargetCount,
  coverageGapTargetCount,
  executionLabelCount,
  serviceTargetCount,
  evidenceLead,
  topEvidence,
  filterContext,
  hasActiveFilters,
  onAbnormalClick,
  onPausedClick,
  onArchivedClick,
  onCoverageClick,
  onClearFilters,
  onCreateClick,
}: TargetsSupportSurfaceProps) {
  const inactiveCount = pausedTargetCount + archivedTargetCount

  function handleLeadAction() {
    if (evidenceLead.actionKind === 'abnormal') {
      onAbnormalClick()
    } else if (evidenceLead.actionKind === 'paused') {
      onPausedClick()
    } else if (evidenceLead.actionKind === 'archived') {
      onArchivedClick()
    } else if (evidenceLead.actionKind === 'coverage') {
      onCoverageClick()
    } else if (evidenceLead.actionKind === 'clear') {
      onClearFilters()
    } else if (evidenceLead.actionKind === 'create') {
      onCreateClick()
    }
  }

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

      <div className={`targets-evidence-lead targets-evidence-lead--${evidenceLead.tone}`}>
        <div className="targets-evidence-lead__main">
          <p className="targets-evidence-lead__eyebrow">{evidenceLead.eyebrow}</p>
          <h3>{evidenceLead.title}</h3>
          <p>{evidenceLead.description}</p>
          {filterContext.length > 0 ? (
            <div className="targets-evidence-lead__filters" aria-label="当前入口证据筛选">
              {filterContext.map((item) => (
                <span key={item}>{item}</span>
              ))}
            </div>
          ) : (
            <div className="targets-evidence-lead__filters" aria-label="当前入口证据筛选">
              <span>完整 Target 库存</span>
            </div>
          )}
        </div>
        <div className="targets-evidence-lead__action">
          {evidenceLead.actionKind === 'asset' ? (
            <Link className="btn btn--secondary btn--md" to="/asset-decisions">
              {evidenceLead.actionLabel}
            </Link>
          ) : (
            <Button variant="secondary" size="md" onClick={handleLeadAction}>
              {evidenceLead.actionLabel}
            </Button>
          )}
          <Link className="observability-support-link" to="/vps">
            VPS 台账
          </Link>
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
            <Badge variant="count" tone={coverageGapTargetCount > 0 ? 'notice' : 'normal'}>
              <MonoDigits>{coverageGapTargetCount}</MonoDigits>
            </Badge>
          </div>
          <p>
            <MonoDigits>{executionLabelCount}</MonoDigits> 个执行节点标签支撑探测边界；缺口目标需要补齐覆盖语义。
          </p>
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
          <p>服务记录回到 VPS 详情核对，Target 只保留入口观测证据和探测覆盖。</p>
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

      <div className="targets-evidence-context" aria-label="目标证据下一步">
        {topEvidence ? (
          <article className="targets-evidence-focus">
            <div className="targets-evidence-focus__glyph">
              <StatusGlyph
                state={targetEvidenceGlyphState(topEvidence.target)}
                ariaLabel={`${topEvidence.title} 入口证据状态`}
              />
            </div>
            <div className="targets-evidence-focus__body">
              <p className="targets-evidence-focus__eyebrow">优先核对入口</p>
              <h3>优先核对：{topEvidence.title}</h3>
              <p>{topEvidence.reason}</p>
              <span>
                <Hostname truncate maxChars={18}>{topEvidence.target.target_id}</Hostname>
                {' · '}
                {topEvidence.meta}
              </span>
            </div>
            <Link className="btn btn--ghost btn--sm" to={topEvidence.route}>
              {topEvidence.actionLabel}
            </Link>
          </article>
        ) : (
          <article className="targets-evidence-focus targets-evidence-focus--stable">
            <div className="targets-evidence-focus__glyph">
              <StatusGlyph state="normal" ariaLabel="Target 入口证据稳定" />
            </div>
            <div className="targets-evidence-focus__body">
              <p className="targets-evidence-focus__eyebrow">入口证据</p>
              <h3>没有需要优先核对的 Target</h3>
              <p>当前列表没有异常入口、暂停归档对象或执行覆盖缺口。</p>
              <span>继续从 VPS 台账和资产决策队列核对资产侧事实。</span>
            </div>
            <Link className="btn btn--ghost btn--sm" to="/asset-decisions">
              查看资产决策
            </Link>
          </article>
        )}
      </div>
    </section>
  )
}
