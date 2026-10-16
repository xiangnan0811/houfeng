import { useLayoutEffect, useRef, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Badge, Button, Timestamp } from '../atoms'
import { VPSCopyValueButton } from '../../pages/vps-detail/VPSCopyValueButton'
import type { IPQualityIPVersion, IPQualitySummary } from '../../lib/types'
import { IPQualityCollectButton } from './IPQualityCollectControls'
import type { CollectReportContext } from './ipQualityCollectPresentation'
import type { FamilyStateNotice } from './ipQualityPresentation'
import type { IPQualityCollectController } from './useIPQualityCollect'

export type IPQualityEnrichmentView = {
  // pending/running：Center 资料仍在补充；只显示单一短状态。
  active: boolean
  // 页面可见轮询失败：保留已显示事实，给一次重试入口。
  failed: boolean
  retry: () => void
}

export type IPQualityHeaderMark = { label: string, tone: 'notice' | 'neutral', title?: string }

// 报告身份区的显式状态：由页面视图决定，不用“summary 是否为空”猜测。
export type IPQualityIdentityState = 'loading' | 'ready' | 'empty' | 'error' | 'invalid'

type IPQualityHeaderProps = {
  detailPath: string
  collect: IPQualityCollectController
  identityState: IPQualityIdentityState
  summary?: IPQualitySummary | null
  // 非 ready 时身份区显示的目标族；历史视图在报告解析前一律“待确定”。
  targetIPVersion?: IPQualityIPVersion | null
  viewingHistory?: boolean
  // 地址族选择器：位于上下文首行，切族时不随数据高度移动、不重挂载。
  familyControl?: ReactNode
  // 只要展示报告（含历史报告）页头就提供采集；历史视图采集后仍停在该历史 ID。空态由正文承载唯一采集入口。
  showCollect?: boolean
  // 旧版、部分采集、归属复核等短标记；不写解释段落。
  marks?: IPQualityHeaderMark[]
  enrichment?: IPQualityEnrichmentView | null
  // 所选族最新尝试（非历史视图）；与报告观察时间分开标注。
  familyNotice?: FamilyStateNotice | null
  // 本轮请求与当前展示报告的关系。
  reportContext?: CollectReportContext | undefined
}

function IPQualityMark() {
  return (
    <span className="vps-overview-identity__mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" focusable="false">
        <circle cx="12" cy="12" r="8.5" />
        <path d="M3.5 12h17M12 3.5c2.4 2.3 3.6 5.1 3.6 8.5s-1.2 6.2-3.6 8.5c-2.4-2.3-3.6-5.1-3.6-8.5s1.2-6.2 3.6-8.5z" />
      </svg>
    </span>
  )
}

export const IP_QUALITY_HISTORY_ANCHOR = 'ip-quality-history-title'

// 非 ready 时身份区的状态文本：失败不冒充无报告。
const IDENTITY_PLACEHOLDER: Record<string, string> = {
  empty: '暂无报告',
  error: '加载失败，报告状态未知',
  invalid: '地址族参数无效',
}

// 上下文高度写到页面根节点，供正文锚点/焦点的 scroll-margin 避开吸顶区。
function useContextHeight() {
  const ref = useRef<HTMLElement>(null)
  useLayoutEffect(() => {
    const element = ref.current
    const page = element?.closest<HTMLElement>('.ipq-page')
    if (!element || !page) return undefined
    const measure = () => {
      page.style.setProperty('--ipq-context-height', `${Math.ceil(element.getBoundingClientRect().height)}px`)
    }
    measure()
    let observer: ResizeObserver | undefined
    if (typeof globalThis.ResizeObserver !== 'undefined') {
      observer = new globalThis.ResizeObserver(measure)
      observer.observe(element)
    } else {
      window.addEventListener('resize', measure)
    }
    return () => {
      if (observer) observer.disconnect()
      else window.removeEventListener('resize', measure)
      page.style.removeProperty('--ipq-context-height')
    }
  }, [])
  return ref
}

export function IPQualityHeader({
  detailPath,
  collect,
  identityState,
  summary = null,
  targetIPVersion = null,
  viewingHistory = false,
  familyControl = null,
  showCollect = true,
  marks = [],
  enrichment = null,
  familyNotice = null,
  reportContext,
}: IPQualityHeaderProps) {
  const location = useLocation()
  const contextRef = useContextHeight()
  const loading = identityState === 'loading'
  const ready = identityState === 'ready' && summary != null

  // 目标族由外壳给出；历史报告未解析时外壳传 null，这里写“待确定”，不把 query 族冒充历史报告族。
  const familyText = ready
    ? `IPv${summary.ip_version}`
    : identityState !== 'invalid' && targetIPVersion != null ? `IPv${targetIPVersion}` : '待确定'
  const latestFamily = ready && (summary.ip_version === 4 || summary.ip_version === 6) ? summary.ip_version : null

  const previousValid = ready && !viewingHistory && (Boolean(familyNotice?.lastValidAt) || reportContext === 'previous_valid')
  const beforeRefresh = ready && !viewingHistory && reportContext === 'before_refresh'
  const notCurrent = ready && !viewingHistory && reportContext === 'not_current_collection'
  const selectedOnly = ready && !viewingHistory && !previousValid && !beforeRefresh && !notCurrent
  const attempt = ready && !viewingHistory && familyNotice?.attemptedAt ? familyNotice : null

  return (
    <>
      <header className="page__head vps-overview-identity ipq-header">
        <div className="vps-overview-identity__lead">
          <IPQualityMark />
          <div className="vps-overview-identity__copy">
            <div className="vps-overview-identity__title-row">
              <h1 className="page__title">IP 质量报告</h1>
            </div>
            {/* 标记行常驻：没有标记时保留一行等高占位，加载/切族时页头高度不变、选择器不被推动。 */}
            <div className="ipq-header__marks">
              {marks.map((mark) => (
                <Badge key={mark.label} variant="info" tone={mark.tone}>
                  <span title={mark.title}>{mark.label}</span>
                </Badge>
              ))}
              {enrichment?.failed ? (
                <span className="ipq-header__enrichment" role="status" aria-label="补充资料状态">
                  <span>资料刷新失败</span>
                  <Button size="sm" variant="ghost" onClick={enrichment.retry}>重试</Button>
                </span>
              ) : enrichment?.active ? (
                <span className="ipq-header__enrichment" role="status" aria-label="补充资料状态">
                  <span className="ipq-pulse" aria-hidden="true" />
                  查询中
                </span>
              ) : null}
              {marks.length === 0 && !enrichment?.active && !enrichment?.failed ? (
                <span className="ipq-header__marks-placeholder" aria-hidden="true">
                  <Badge variant="info" tone="neutral">{'\u00a0'}</Badge>
                </span>
              ) : null}
            </div>
          </div>
        </div>
        <div className="page__actions">
          {loading ? (
            // 加载时与真实动作同尺寸但不可交互、对辅助技术隐藏；不强造可点按钮。
            <span className="ipq-header__action-placeholder" aria-hidden="true">
              <span className="btn md primary">立即采集</span>
              <span className="btn md ghost">历史</span>
            </span>
          ) : (
            <>
              {showCollect ? <IPQualityCollectButton collect={collect} /> : null}
              {summary ? (
                // 真实锚点：普通激活同时把焦点移到历史标题（滚动仍由原生锚点完成）；修饰键/新窗口不劫持。
                <a
                  className="btn md ghost"
                  href={`#${IP_QUALITY_HISTORY_ANCHOR}`}
                  onClick={(event) => {
                    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
                    document.getElementById(IP_QUALITY_HISTORY_ANCHOR)?.focus({ preventScroll: true })
                  }}
                >
                  历史
                </a>
              ) : null}
            </>
          )}
          <Link className="btn md secondary" to={detailPath} state={location.state}>返回 VPS 详情</Link>
        </div>
      </header>
      <section ref={contextRef} className="ipq-report-context" aria-label="报告上下文">
        {/* 独立首行：选择器与“查看最新报告”；身份数据高度变化只影响下方各行。 */}
        <div className="ipq-report-context__bar">
          {familyControl ? <div className="ipq-report-context__family">{familyControl}</div> : null}
          {viewingHistory ? (
            latestFamily != null ? (
              <Link
                className="btn sm secondary ipq-report-context__latest"
                to={`${location.pathname}?ip_version=${latestFamily}`}
                state={location.state}
              >
                查看最新报告
              </Link>
            ) : (
              <span className="btn sm secondary ipq-report-context__latest" aria-disabled="true">查看最新报告</span>
            )
          ) : null}
        </div>
        <div className="ipq-report-context__facts">
          <dl className="ipq-report-context__identity" aria-label="报告身份" aria-busy={loading || undefined}>
            <div className="ipq-report-context__item ipq-report-context__ip">
              <dt>出口 IP</dt>
              <dd>
                {ready ? (
                  <>
                    <span className="mono ipq-report-context__address">{summary.ip_address}</span>
                    <VPSCopyValueButton value={summary.ip_address} label="出口 IP" />
                  </>
                ) : loading ? (
                  <>
                    <span className="ipq-skeleton ipq-skeleton--address" aria-hidden="true" />
                    <span className="visually-hidden">正在加载</span>
                  </>
                ) : (
                  <span className="ipq-report-context__state">{IDENTITY_PLACEHOLDER[identityState === 'ready' ? 'empty' : identityState]}</span>
                )}
              </dd>
            </div>
            <div className="ipq-report-context__item">
              <dt>地址族</dt>
              <dd className="mono">{familyText}</dd>
            </div>
            <div className="ipq-report-context__item">
              <dt>观察时间</dt>
              <dd>
                {ready ? (
                  <Timestamp value={summary.observed_at} mode="both" />
                ) : loading ? (
                  <span className="ipq-skeleton" aria-hidden="true" />
                ) : (
                  <span className="ipq-gap">—</span>
                )}
              </dd>
            </div>
          </dl>
          {ready ? (
            <div className="ipq-report-context__marks" role="group" aria-label="报告状态">
              {viewingHistory ? <Badge variant="info" tone="neutral">历史报告</Badge> : null}
              {summary.stale ? (
                <Badge variant="info" tone="notice"><span title="报告超过过期窗口，仅作历史参考">已过期</span></Badge>
              ) : null}
              {previousValid ? <Badge variant="info" tone="notice">上次有效结果</Badge> : null}
              {beforeRefresh ? <Badge variant="info" tone="notice">刷新前报告</Badge> : null}
              {notCurrent ? <Badge variant="info" tone="neutral">非本轮结果</Badge> : null}
              {selectedOnly ? <Badge variant="info" tone="neutral">所选报告</Badge> : null}
            </div>
          ) : null}
        </div>
        {attempt?.attemptedAt ? (
          <p className={`ipq-report-context__attempt ipq-report-context__attempt--${attempt.tone}`} role="note" aria-label="地址族采集状态">
            <span className="ipq-report-context__attempt-title">{attempt.title}</span>
            <span className="ipq-report-context__attempt-time">最近尝试 <Timestamp value={attempt.attemptedAt} mode="both" /></span>
            {attempt.detail ? <span className="ipq-report-context__attempt-detail">{attempt.detail}</span> : null}
          </p>
        ) : null}
      </section>
    </>
  )
}
