import { Fragment, useState, type ReactNode } from 'react'

import { Badge, Button, ScrollRegion } from '../atoms'
import type { IPQualityProductSummary, IPQualityProviderResult, IPQualityScore } from '../../lib/types'
import {
  FLAG_COLUMNS,
  fieldCellState,
  flagCellState,
  providerFailureText,
  providerMethodLabel,
  riskFlagLabel,
  scoreDisplay,
  scoreLevel,
  scoreScopeLabel,
  usageTypeLabel,
  vendorGroups,
  type FieldCellState,
  type FlagCellState,
  type FlagColumn,
} from './ipQualityProductPresentation'
import { probeErrorLabel, providerSucceeded, riskLevelLabel, riskTone } from './ipQualityPresentation'

type IPQualityProviderTableProps = {
  results: IPQualityProviderResult[]
  // revision1：服务端事实摘要；缺失时为旧版报告，只显示旧版评级标记。
  productSummary: IPQualityProductSummary | null
  legacyRiskLevel?: string
}

const TITLE_ID = 'ip-quality-provider-table-title'
const SCORES_TITLE_ID = 'ip-quality-scores-title'
// 风险因子表标题：基础信息中使用/公司类型的分歧入口指向这里。
export const IP_QUALITY_FACTORS_ANCHOR = 'ip-quality-factors-title'
const COLUMN_COUNT = 3 + FLAG_COLUMNS.length

// 每种状态同时有字形与文字名称；颜色只是附加信号。
const CELL_TEXT: Record<FlagCellState, { glyph: string, label: string }> = {
  yes: { glyph: '是', label: '是' },
  no: { glyph: '否', label: '否' },
  unsupported: { glyph: '—', label: '来源不支持' },
  unknown: { glyph: '?', label: '未知' },
  error: { glyph: '!', label: '来源错误' },
}

// 负面因子：是=告警实心，否=正常实心；机房不是负面风险，是/否都用中性实心。
function FlagCell({ row, column }: { row: IPQualityProviderResult, column: FlagColumn }) {
  const state = flagCellState(row, column)
  const text = CELL_TEXT[state]
  const tone = state === 'yes' || state === 'no' ? (column.negative ? (state === 'yes' ? 'alert' : 'normal') : 'neutral') : null
  return (
    <span className={`ipq-cell ipq-cell--${state}${tone ? ` ipq-cell--${tone}` : ''}`} title={text.label}>
      <span aria-hidden="true">{text.glyph}</span>
      <span className="visually-hidden">{column.label} {text.label}</span>
    </span>
  )
}

// 非已知字段的紧凑标记（不支持—、未知?、错误!），定位表复用同一视觉词汇。
export function FieldStateMark({ state, label }: { state: Exclude<FieldCellState, 'known'>, label: string }) {
  const text = CELL_TEXT[state]
  return (
    <span className={`ipq-cell ipq-cell--${state}`} title={text.label}>
      <span aria-hidden="true">{text.glyph}</span>
      <span className="visually-hidden">{label} {text.label}</span>
    </span>
  )
}

function TypeCell({ row, field, label }: { row: IPQualityProviderResult, field: 'usage_type' | 'company_type', label: string }) {
  const raw = (row[field] ?? '').trim()
  const state = fieldCellState(row, field, raw)
  if (state !== 'known') return <FieldStateMark state={state} label={label} />
  const display = usageTypeLabel(raw) ?? raw
  return (
    <span className="ipq-type">
      {display}
      {display !== raw ? <span className="ipq-type__raw mono">{raw}</span> : null}
    </span>
  )
}

// 条形只表示该来源自身量程上的位置：0 就是空条，不设最小宽度；颜色只随来源等级，无等级时中性。
function ScoreBar({ fraction, tone }: { fraction: number, tone: string }) {
  return (
    <svg className="ipq-score__bar" viewBox="0 0 100 4" preserveAspectRatio="none" aria-hidden="true" focusable="false">
      <rect className="ipq-score__track" x="0" y="0" width="100" height="4" />
      {fraction > 0 ? <rect className={`ipq-score__fill ipq-score__fill--${tone}`} x="0" y="0" width={fraction * 100} height="4" /> : null}
    </svg>
  )
}

function SourceName({ row, sample }: { row: IPQualityProviderResult, sample?: boolean }) {
  const method = providerMethodLabel(row)
  return (
    <span className={sample ? 'ipq-source ipq-source--sample' : 'ipq-source'}>
      <span className="ipq-source__name">{row.provider}</span>
      {method ? <span className="ipq-source__method">{method}</span> : null}
    </span>
  )
}

function ScoreRow({ row, score, first }: { row: IPQualityProviderResult, score: IPQualityScore, first: boolean }) {
  const display = scoreDisplay(score)
  const level = scoreLevel(score.source_label)
  const tone = level?.tone ?? 'neutral'
  const raw = score.raw_value.trim()
  return (
    <li className={first ? 'ipq-score-row ipq-score-row--first' : 'ipq-score-row'}>
      <span className="ipq-score-row__source">{first ? <SourceName row={row} /> : <span className="visually-hidden">{row.provider}</span>}</span>
      <span className="ipq-score-row__metric">
        <span className="ipq-score-row__label">{score.label || score.id}</span>
        <span className="ipq-score__scope">{scoreScopeLabel(score.scope)}</span>
      </span>
      <span className="ipq-score-row__value mono" title={raw && raw !== display.text ? `原始值 ${raw}` : undefined}>
        {display.text}
      </span>
      <span className="ipq-score-row__level">
        {level ? <Badge variant="info" tone={level.tone}>{level.label}</Badge> : null}
        {level && level.raw !== level.label ? <span className="ipq-score-row__raw">{level.raw}</span> : null}
      </span>
      <span className="ipq-score-row__bar">
        {display.fraction != null ? <ScoreBar fraction={display.fraction} tone={tone} /> : null}
      </span>
    </li>
  )
}

// 旧行没有 typed scores：只有真实原始分时才占一行，旧版评级随之作历史标注；只有评级没有分数不造评分行。
function LegacyScoreRow({ row, score }: { row: IPQualityProviderResult, score: string }) {
  const legacyLevel = (row.risk_level ?? '').trim()
  return (
    <li className="ipq-score-row ipq-score-row--first">
      <span className="ipq-score-row__source"><SourceName row={row} /></span>
      <span className="ipq-score-row__metric"><span className="ipq-score-row__label">旧版评分</span></span>
      <span className="ipq-score-row__value mono">{score}</span>
      <span className="ipq-score-row__level">
        {legacyLevel ? <Badge variant="info" tone={riskTone(legacyLevel)}>旧版评级 · {riskLevelLabel(legacyLevel)}</Badge> : null}
      </span>
      <span className="ipq-score-row__bar" />
    </li>
  )
}

// 逐源逐项列出来源给出的分数；没有分数的来源不占行，不合成总分、不跨源平均。
function ScoreRows({ rows }: { rows: IPQualityProviderResult[] }) {
  const items = rows.flatMap((row): ReactNode[] => {
    const scores = row.details?.scores ?? []
    if (scores.length > 0) {
      return scores.map((score, index) => <ScoreRow key={`${row.provider}:${score.id}:${score.scope}`} row={row} score={score} first={index === 0} />)
    }
    const legacyScore = (row.risk_score ?? '').trim()
    if (!row.details && legacyScore) {
      return [<LegacyScoreRow key={`${row.provider}:legacy`} row={row} score={legacyScore} />]
    }
    return []
  })
  if (items.length === 0) return <p className="ipq-empty">来源未返回风险评分。</p>
  return <ul className="ipq-score-rows" aria-labelledby={SCORES_TITLE_ID}>{items}</ul>
}

function ProviderRow({ row, sample = false, toggle, enhancementFailed = false }: { row: IPQualityProviderResult, sample?: boolean, toggle?: ReactNode, enhancementFailed?: boolean }) {
  const succeeded = providerSucceeded(row)
  return (
    <tr className={sample ? 'data-table__row ipq-provider-table__sample' : 'data-table__row'}>
      <td className="data-table__cell ipq-provider-table__name">
        <SourceName row={row} sample={sample} />
        {enhancementFailed ? <span className="ipq-source__failure">增强查询失败</span> : null}
        {toggle}
      </td>
      {succeeded ? (
        <>
          <td className="data-table__cell"><TypeCell row={row} field="usage_type" label="使用类型" /></td>
          <td className="data-table__cell"><TypeCell row={row} field="company_type" label="公司类型" /></td>
          {FLAG_COLUMNS.map((column) => (
            <td key={column.key} className="data-table__cell ipq-provider-table__flag"><FlagCell row={row} column={column} /></td>
          ))}
        </>
      ) : (
        <td className="data-table__cell ipq-provider-table__failure" colSpan={COLUMN_COUNT - 1}>
          <span className="ipq-cell ipq-cell--error" aria-hidden="true">!</span>
          <span className="ipq-provider-table__failure-text">{providerFailureText(row)}</span>
          {row.error_code ? <span className="ipq-muted">{probeErrorLabel(row.error_code) ?? row.error_code}</span> : null}
        </td>
      )}
    </tr>
  )
}

function RiskHeadline({ productSummary, legacyRiskLevel }: { productSummary: IPQualityProductSummary | null, legacyRiskLevel: string | undefined }) {
  if (!productSummary) {
    const level = (legacyRiskLevel ?? '').trim()
    return level ? <Badge variant="info" tone={riskTone(level)}>旧版评级 · {riskLevelLabel(level)}</Badge> : null
  }
  // 局部只报已有阳性：没有命中时不出现泛化的安全/阴性结论。
  if (productSummary.risk_flags.length === 0 && productSummary.high_risk_sources.length === 0) return null
  return (
    <>
      {productSummary.risk_flags.map((flag) => <Badge key={flag} variant="info" tone="alert">{riskFlagLabel(flag)}</Badge>)}
      {productSummary.high_risk_sources.length > 0 ? (
        <Badge variant="info" tone="alert">高风险来源 · {productSummary.high_risk_sources.join('、')}</Badge>
      ) : null}
    </>
  )
}

export function IPQualityProviderTable({ results, productSummary, legacyRiskLevel }: IPQualityProviderTableProps) {
  const groups = vendorGroups(results)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const scoredRows = groups.map((group) => group.primary).filter(providerSucceeded)

  return (
    <section className="vps-detail-workspace__section ipq-providers" aria-labelledby={TITLE_ID}>
      <div className="vps-detail-workspace__section-head">
        <h2 id={TITLE_ID}>风险评分与因子</h2>
        <div className="ipq-signal-hits" role="group" aria-label="风险命中">
          <RiskHeadline productSummary={productSummary} legacyRiskLevel={legacyRiskLevel} />
        </div>
      </div>
      {groups.length > 0 ? (
        <div className="ipq-risk">
          <div className="ipq-risk__block">
            <h3 id={SCORES_TITLE_ID} className="ipq-risk__title">风险评分</h3>
            <ScoreRows rows={scoredRows} />
          </div>
          <div className="ipq-risk__block">
            <h3 id={IP_QUALITY_FACTORS_ANCHOR} className="ipq-risk__title" tabIndex={-1}>风险因子</h3>
            <ScrollRegion
              className="ipq-table-scroll"
              labelledBy={IP_QUALITY_FACTORS_ANCHOR}
              hintId="ip-quality-factors-hint"
              hintClassName="ipq-table-hint"
              hint="横向滚动查看完整列"
            >
              <table className="data-table data-table--compact ipq-provider-table">
                <thead className="data-table__head">
                  <tr>
                    <th scope="col">来源</th>
                    <th scope="col">使用类型</th>
                    <th scope="col">公司类型</th>
                    {FLAG_COLUMNS.map((column) => <th key={column.key} scope="col" className="ipq-provider-table__flag">{column.label}</th>)}
                  </tr>
                </thead>
                <tbody>
                  {groups.map((group) => {
                    const open = expanded[group.provider] === true
                    const toggle = group.others.length > 0 ? (
                      <Button
                        size="sm"
                        variant="ghost"
                        className="ipq-provider-table__toggle"
                        aria-expanded={open}
                        aria-label={open ? `收起 ${group.provider} 的样本` : `展开 ${group.provider} 的另外 ${group.others.length} 个样本`}
                        onClick={() => setExpanded((current) => ({ ...current, [group.provider]: !open }))}
                      >
                        {open ? '收起样本' : `另 ${group.others.length} 个样本`}
                      </Button>
                    ) : null
                    return (
                      <Fragment key={group.provider}>
                        <ProviderRow row={group.primary} toggle={toggle} enhancementFailed={group.enhancementFailed} />
                        {open ? group.others.map((row, index) => <ProviderRow key={`${group.provider}:sample:${index}`} row={row} sample />) : null}
                      </Fragment>
                    )
                  })}
                </tbody>
              </table>
            </ScrollRegion>
          </div>
        </div>
      ) : (
        <p className="ipq-empty">暂无来源结果。</p>
      )}
    </section>
  )
}
