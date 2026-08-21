import { Button } from '../components/atoms'
import { PageState } from '../components/PageState'
import { useAuth } from '../lib/auth-context'
import type { ComparisonPairwise } from '../lib/types'
import { ComparabilityReview } from './records/compare/ComparabilityReview'
import { ComparisonConditions } from './records/compare/ComparisonConditions'
import { ComparisonKindPanel } from './records/compare/ComparisonKindPanel'
import { ComparisonMatrix } from './records/compare/ComparisonMatrix'
import { ComparisonSaveRecord } from './records/compare/ComparisonSaveRecord'
import { ComparisonSelectionBasket } from './records/compare/ComparisonSelectionBasket'
import { ComparisonTrendChart } from './records/compare/ComparisonTrendChart'
import { useComparisonWorkbench } from './records/compare/useComparisonWorkbench'

function pairwiseLabel(comparison: NonNullable<ReturnType<typeof useComparisonWorkbench>['state']['comparison']>): string {
  if (comparison.pairwise.length === 0) return '当前类型用精确比较结果展示，不绘制趋势。'
  return comparison.pairwise.map((entry) => formatPairwiseDifference(entry)).join('；')
}

function formatPairwiseDifference(entry: ComparisonPairwise): string {
  const values = entry.values
  const matched = numberish(values.matched)
  const unmatchedBaseline = numberish(values.unmatched_baseline)
  const unmatchedItem = numberish(values.unmatched_item)
  const deltas = Array.isArray(values.deltas) ? values.deltas : []
  if (matched != null || deltas.length > 0) {
    const changed = deltas.filter((item) => {
      if (!item || typeof item !== 'object') return false
      const delta = numberish((item as { delta?: unknown }).delta)
      return delta != null && delta !== 0
    }).length
    const equality = values.equal === true ? '相等' : '有差值'
    return [
      `${entry.kind}/v${entry.schema_version}：${equality}`,
      `匹配 ${matched ?? 0} 桶`,
      `基准未匹配 ${unmatchedBaseline ?? 0}`,
      `候选项未匹配 ${unmatchedItem ?? 0}`,
      changed > 0 ? `差值 ${changed} 桶` : '',
    ].filter(Boolean).join('，')
  }
  return `${entry.kind}/v${entry.schema_version}：${entry.compatible ? '兼容' : entry.reason || '不兼容'}`
}

function numberish(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

export function RecordComparisonPage() {
  const { user } = useAuth()
  const { state, commands } = useComparisonWorkbench({ userId: user?.user_id ?? '' })
  const query = state.query.ok ? state.query.state : null
  const activeKind = query?.kind
  const activeMetric = query?.metric
  const showSeries = Boolean(activeKind?.startsWith('monitoring.host') || activeKind?.startsWith('monitoring.probe'))

  return (
    <div className="page-stack">
      <div className="page-header">
        <div>
          <div className="page-eyebrow">运维知识 · COMPARE</div>
          <h1 className="page-title">横向比较工作台</h1>
          <p className="page-sub">先确认不可变选择与可比性，再看差异。人工结论与系统结果分开另存。</p>
        </div>
      </div>

      {!state.query.ok ? (
        <PageState
          kind="empty"
          title="从选择篮开始"
          description="分享链接损坏或版本未知时，可以重新选择 2–6 项修订或快照。"
        />
      ) : null}

      {state.loading ? (
        <PageState
          kind="loading"
          title="正在加载比较"
          action={(
            <div className="page-state__actions">
              <Button size="lg" variant="secondary" onClick={commands.cancel}>取消比较</Button>
            </div>
          )}
        />
      ) : null}
      {state.cancelled ? <p role="status">已取消比较。条件已更新，旧结果不会继续展示。</p> : null}
      {state.error ? (
        <PageState
          kind="error"
          title="比较不可用"
          description={state.error}
          {...(state.errorCode ? { technicalSummary: state.errorCode } : {})}
        />
      ) : null}

      <ComparisonSelectionBasket
        query={query}
        candidates={state.candidates}
        onConfirm={commands.confirmCandidates}
      />

      {query ? (
        <ComparisonConditions
          query={query}
          onBaseline={commands.setBaseline}
          onAlignment={commands.setAlignment}
          onWindow={commands.setWindow}
          onTolerance={commands.setToleranceSeconds}
          onBucket={commands.setBucketSeconds}
        />
      ) : null}

      {state.loading ? null : <ComparabilityReview comparison={state.comparison} />}

      {state.comparison ? (
        <>
          <ComparisonKindPanel
            comparison={state.comparison}
            {...(activeKind ? { activeKind } : {})}
            {...(activeMetric ? { metric: activeMetric } : {})}
            onSelect={(kind, metric) => commands.selectKind(kind, metric)}
          />
          {showSeries ? (
            <>
              <ComparisonTrendChart
                {...(activeKind ? { kind: activeKind } : {})}
                {...(activeMetric ? { metric: activeMetric } : {})}
                series={state.comparison.series}
              />
              <ComparisonMatrix
                {...(activeKind ? { kind: activeKind } : {})}
                {...(activeMetric ? { metric: activeMetric } : {})}
                comparison={state.comparison}
              />
            </>
          ) : (
            <section aria-labelledby="comparison-pairwise-heading">
              <h2 className="section-heading__title" id="comparison-pairwise-heading">系统差异</h2>
              <p>{pairwiseLabel(state.comparison)}</p>
            </section>
          )}
          {state.comparison.pairwise.length > 0 && showSeries ? (
            <section aria-labelledby="comparison-diff-heading">
              <h2 className="section-heading__title" id="comparison-diff-heading">系统差异</h2>
              <p>{pairwiseLabel(state.comparison)}</p>
            </section>
          ) : null}
        </>
      ) : null}

      <ComparisonSaveRecord
        blocked={state.saveBlocked}
        {...(state.saveBlocked ? {
          blockers: state.comparison?.save_eligibility.blockers ?? [],
        } : {})}
        title={state.title}
        conclusion={state.conclusion}
        saving={state.saving}
        savedRecordId={state.savedRecordId}
        onTitle={commands.setTitle}
        onConclusion={commands.setConclusion}
        onSave={() => { void commands.save() }}
      />
    </div>
  )
}
