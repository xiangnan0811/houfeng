import { Link } from 'react-router-dom'

import { Badge, Button, Hostname, MonoDigits, StatusGlyph } from '../../components/atoms'
import { ObservabilityEvidenceFocus } from '../../components/ObservabilityEvidenceFocus'
import { ObservabilityEvidenceLead } from '../../components/ObservabilityEvidenceLead'
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
            用服务入口可达性和探测覆盖确认暴露面是否可信，异常入口再回到 VPS 与服务资产补证据。
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

      <ObservabilityEvidenceLead
        tone={evidenceLead.tone}
        eyebrow={evidenceLead.eyebrow}
        title={evidenceLead.title}
        description={evidenceLead.description}
        filterItems={filterContext}
        emptyFilterLabel="完整 Target 库存"
        filterAriaLabel="当前入口证据筛选"
        action={
          evidenceLead.actionKind === 'asset' ? (
            <Link className="btn md secondary" to="/asset-decisions">
              {evidenceLead.actionLabel}
            </Link>
          ) : (
            <Button variant="secondary" size="md" onClick={handleLeadAction}>
              {evidenceLead.actionLabel}
            </Button>
          )
        }
        secondaryAction={
          <Link className="observability-support-link" to="/vps">
            VPS 台账
          </Link>
        }
      />

      <div className="observability-support__grid" aria-label="入口探测证据摘要">
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
            <MonoDigits>{executionLabelCount}</MonoDigits> 个执行监控实例标签支撑探测边界；缺口目标需要补齐覆盖语义。
          </p>
          <div className="observability-support-lane__actions">
            <Link className="observability-support-link" to="/monitoring">
              监控实例覆盖
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
          <p>需要补齐服务归属时，从 VPS 台账回查资产；Target 侧只负责说明入口是否可达、由谁探测。</p>
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
          <ObservabilityEvidenceFocus
            glyph={
              <StatusGlyph
                state={targetEvidenceGlyphState(topEvidence.target)}
                ariaLabel={`${topEvidence.title} 入口证据状态`}
              />
            }
            eyebrow="优先核对入口"
            title={`优先核对：${topEvidence.title}`}
            description={topEvidence.reason}
            meta={
              <>
                <Hostname truncate maxChars={18}>{topEvidence.target.target_id}</Hostname>
                {' · '}
                {topEvidence.meta}
              </>
            }
            action={
              <Link className="btn sm ghost" to={topEvidence.route}>
                {topEvidence.actionLabel}
              </Link>
            }
          />
        ) : (
          <ObservabilityEvidenceFocus
            stable
            glyph={<StatusGlyph state="normal" ariaLabel="Target 入口证据稳定" />}
            eyebrow="入口证据"
            title="没有需要优先核对的 Target"
            description="当前列表没有异常入口、暂停归档对象或执行覆盖缺口。"
            meta="继续从 VPS 台账和资产决策队列核对资产侧事实。"
            action={
              <Link className="btn sm ghost" to="/asset-decisions">
                查看资产决策
              </Link>
            }
          />
        )}
      </div>
    </section>
  )
}
