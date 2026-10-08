import { useState } from 'react'

import { Button } from '../components/atoms'
import { PageState } from '../components/PageState'
import { useAuth } from '../lib/auth-context'
import type { ComparisonPairwise } from '../lib/types'
import { ComparabilityReview } from './records/compare/ComparabilityReview'
import { comparisonEvidenceKindLabel, presentComparisonReason } from './records/compare/comparisonLabels'
import { ComparisonConditions } from './records/compare/ComparisonConditions'
import { ComparisonKindPanel } from './records/compare/ComparisonKindPanel'
import { ComparisonMatrix } from './records/compare/ComparisonMatrix'
import { ComparisonObjectDialog } from './records/compare/ComparisonObjectDialog'
import { ComparisonSaveRecord } from './records/compare/ComparisonSaveRecord'
import { ComparisonSelectionBasket } from './records/compare/ComparisonSelectionBasket'
import { ComparisonTrendChart } from './records/compare/ComparisonTrendChart'
import { defaultComparisonWindow, type ComparisonURLFixedItem } from './records/compare/comparisonQueryState'
import { useComparisonWorkbench } from './records/compare/useComparisonWorkbench'
import { formatDateTime } from '../lib/format'
import './records/RecordWorkspace.css'

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
      `${comparisonEvidenceKindLabel(entry.kind)}：${equality}`,
      `匹配 ${matched ?? 0} 桶`,
      `基准未匹配 ${unmatchedBaseline ?? 0}`,
      `候选项未匹配 ${unmatchedItem ?? 0}`,
      changed > 0 ? `差值 ${changed} 桶` : '',
    ].filter(Boolean).join('，')
  }
  const reason = presentComparisonReason(entry.reason).label
  return `${comparisonEvidenceKindLabel(entry.kind)}：${entry.compatible ? '兼容' : reason}`
}

function numberish(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function ComparisonMark() {
  return (
    <span className="record-identity__mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" focusable="false">
        <path d="M4 19V9M10 19V5M16 19v-7M22 19H2" />
      </svg>
    </span>
  )
}

function windowLabel(from: string, to: string): string {
  const valid = Number.isFinite(Date.parse(from)) && Number.isFinite(Date.parse(to))
  return valid ? `${formatDateTime(from)} → ${formatDateTime(to)}` : `${from} → ${to}`
}

export function RecordComparisonPage() {
  const { user } = useAuth()
  const { state, commands } = useComparisonWorkbench({ userId: user?.user_id ?? '' })
  const [pickerOpen, setPickerOpen] = useState(false)
  const query = state.query.ok ? state.query.state : null
  const linkProblem = state.query.ok ? null : state.query.reason
  const activeKind = query?.kind
  const activeMetric = query?.metric
  const showSeries = Boolean(activeKind?.startsWith('monitoring.host') || activeKind?.startsWith('monitoring.probe'))
  const itemCount = query?.mode === 'fixed' ? query.items?.length ?? 0 : 0
  const comparison = state.comparison
  const fallbackWindow = defaultComparisonWindow()
  const evidenceFrom = query?.requested_from ?? fallbackWindow.requested_from
  const evidenceTo = query?.requested_to ?? fallbackWindow.requested_to
  const basketSnapshotIds = query?.mode === 'fixed'
    ? (query.items ?? []).flatMap((item) => 'snapshot_id' in item ? [item.snapshot_id] : [])
    : []

  return (
    <div className="page record-page record-compare">
      <header className="page__head record-identity">
        <div className="record-identity__lead">
          <ComparisonMark />
          <div className="record-identity__copy">
            <div className="record-identity__title-row">
              <h1 className="page__title">横向比较</h1>
            </div>
            {query ? (
              <dl className="record-identity__meta record-identity__facts" aria-label="比较范围">
                {query.mode === 'fixed' ? (
                  <div className="record-identity__meta-item"><dt>对象</dt><dd>{itemCount} 项</dd></div>
                ) : (
                  <div className="record-identity__meta-item"><dt>主体</dt><dd>{query.subjects?.length ?? 0} 个</dd></div>
                )}
                {query.mode === 'fixed' && itemCount > 1 ? (
                  <div className="record-identity__meta-item"><dt>基准</dt><dd>第 {(query.baseline ?? 0) + 1} 项</dd></div>
                ) : null}
                <div className="record-identity__meta-item">
                  <dt>窗口</dt>
                  <dd className="mono">{windowLabel(query.requested_from, query.requested_to)}</dd>
                </div>
              </dl>
            ) : null}
          </div>
        </div>
      </header>

      {linkProblem ? (
        <ComparisonLinkState reason={linkProblem} onAdd={() => setPickerOpen(true)} />
      ) : null}

      <div className="record-compare__layout">
        <div className="record-compare__side">
          <ComparisonSelectionBasket
            query={query}
            linkProblem={linkProblem}
            candidates={state.candidates}
            selectionError={pickerOpen ? null : state.selectionError}
            onConfirm={commands.confirmCandidates}
            onRemove={commands.removeFixedItem}
            onClear={commands.clearFixedItems}
            {...(query ? { onAdd: () => setPickerOpen(true) } : {})}
            onReviseSnapshots={(item, snapshotIds) => {
              commands.addFixedItem(revisionWithSnapshots(item, snapshotIds))
            }}
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
        </div>

        <div className="record-compare__main">
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
          {state.cancelled ? <p className="record-compare__notice" role="status">已取消比较。条件已更新，旧结果不会继续展示。</p> : null}
          {state.error ? (
            <PageState
              kind="error"
              title="比较不可用"
              description={state.error}
              {...(state.errorCode ? { technicalSummary: state.errorCode } : {})}
            />
          ) : null}

          {state.loading ? null : <ComparabilityReview comparison={comparison} />}

          {comparison ? (
            <section className="record-section record-compare__result" aria-labelledby="comparison-result-heading">
              <div className="record-section__head">
                <h2 className="record-section__title" id="comparison-result-heading">比较结果</h2>
                <ComparisonKindPanel
                  comparison={comparison}
                  {...(activeKind ? { activeKind } : {})}
                  {...(activeMetric ? { metric: activeMetric } : {})}
                  onSelect={(kind, metric) => commands.selectKind(kind, metric)}
                />
              </div>
              {showSeries ? (
                <>
                  <ComparisonTrendChart
                    {...(activeKind ? { kind: activeKind } : {})}
                    {...(activeMetric ? { metric: activeMetric } : {})}
                    series={comparison.series}
                  />
                  {comparison.pairwise.length > 0 ? <PairwiseSummary comparison={comparison} /> : null}
                  <ComparisonMatrix
                    {...(activeKind ? { kind: activeKind } : {})}
                    {...(activeMetric ? { metric: activeMetric } : {})}
                    {...(query?.baseline != null ? { baseline: query.baseline } : {})}
                    comparison={comparison}
                  />
                </>
              ) : (
                <PairwiseSummary comparison={comparison} />
              )}
              <details className="record-disclosure record-compare__tech">
                <summary>技术细节</summary>
                <dl className="record-compare__tech-list">
                  {comparison.items.map((item, index) => (
                    <div key={`${item.snapshot_id}-${index}`}>
                      <dt>第 {index + 1} 项</dt>
                      <dd>
                        <code>{item.snapshot_id}</code>
                        <code>{item.canonical_hash}</code>
                      </dd>
                    </div>
                  ))}
                  <div>
                    <dt>比较摘要</dt>
                    <dd><code>{comparison.digest}</code></dd>
                  </div>
                </dl>
              </details>
            </section>
          ) : null}

          <ComparisonObjectDialog
            open={pickerOpen}
            onClose={() => setPickerOpen(false)}
            from={evidenceFrom}
            to={evidenceTo}
            basketSnapshotIds={basketSnapshotIds}
            selectionError={state.selectionError}
            onAdd={commands.addFixedItem}
          />
          <ComparisonSaveRecord
            blocked={state.saveBlocked}
            {...(state.saveBlocked ? {
              blockers: comparison?.save_eligibility.blockers ?? [],
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
      </div>
    </div>
  )
}

function revisionWithSnapshots(
  item: Extract<ComparisonURLFixedItem, { record_id: string }>,
  snapshotIds: string[],
): ComparisonURLFixedItem {
  return snapshotIds.length
    ? { record_id: item.record_id, revision_id: item.revision_id, snapshot_ids: snapshotIds }
    : { record_id: item.record_id, revision_id: item.revision_id }
}

function ComparisonLinkState({
  reason,
  onAdd,
}: {
  reason: 'missing' | 'invalid' | 'unknown_version'
  onAdd: () => void
}) {
  const action = <Button size="lg" onClick={onAdd}>添加对象</Button>
  if (reason === 'missing') {
    return (
      <PageState
        kind="empty"
        title="比较篮是空的"
        description="添加 2–6 个证据后再比较。"
        action={action}
      />
    )
  }
  if (reason === 'unknown_version') {
    return (
      <PageState
        kind="error"
        title="不支持的比较链接版本"
        description="当前客户端打不开这个版本的比较链接。可以重新添加对象。"
        action={action}
      />
    )
  }
  return (
    <PageState
      kind="error"
      title="比较链接已损坏"
      description="这条比较链接无法读取。可以重新添加对象。"
      action={action}
    />
  )
}

function PairwiseSummary({ comparison }: { comparison: NonNullable<ReturnType<typeof useComparisonWorkbench>['state']['comparison']> }) {
  return (
    <div className="record-compare__block">
      <div className="record-compare__block-head">
        <h3 className="record-compare__block-title" id="comparison-diff-heading">系统差异</h3>
      </div>
      <p className="record-compare__pairwise">{pairwiseLabel(comparison)}</p>
    </div>
  )
}
