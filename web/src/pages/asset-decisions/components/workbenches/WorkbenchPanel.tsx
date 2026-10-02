import { type ReactNode, useId } from 'react'

import { isInteractiveRowTarget } from '../../../../components/atoms'

type WorkbenchPanelProps = {
  title: string
  actions?: ReactNode
  className?: string
  id?: string
  children: ReactNode
}

/** 次级工作区卡片：标题与工具条入口同名，不带眉题与重复计数。 */
export function WorkbenchPanel({ title, actions, className = '', id, children }: WorkbenchPanelProps) {
  const headingId = useId()
  return (
    <section id={id} className={`page-panel asset-workbench ${className}`.trim()} aria-labelledby={headingId}>
      <div className="asset-workbench__head">
        <h2 className="asset-workbench__title" id={headingId}>{title}</h2>
        {actions ? <div className="asset-workbench__actions">{actions}</div> : null}
      </div>
      {children}
    </section>
  )
}

type ScanRowProps = {
  /** 整行可点：鼠标点行时先聚焦行内主入口（data-row-primary）再触发它，弹窗关闭后焦点回到该入口；不额外增加 Tab 停靠。 */
  clickable?: boolean
  className?: string
  children: ReactNode
}

export function ScanRow({ clickable = false, className = '', children }: ScanRowProps) {
  return (
    <>
    {/* a11y-allow-nonsemantic-click: primary-link-row-enhancement */}
    <li
      className={`asset-scan-row ${className}`.trim()}
      data-clickable={clickable ? 'true' : undefined}
      onClick={clickable ? (event) => {
        if (isInteractiveRowTarget(event.target)) return
        const primary = event.currentTarget.querySelector<HTMLElement>('[data-row-primary]')
        if (!primary) return
        primary.focus()
        primary.click()
      } : undefined}
    >
      {children}
    </li>
    </>
  )
}

/** 行首身份：主名称 + 弱化的次要信息，放不下时次要信息换行而不是被压成零宽。 */
export function ScanName({ name, meta }: { name: ReactNode; meta?: ReactNode }) {
  return (
    <span className="asset-scan-row__name">
      <strong>{name}</strong>
      {meta ? <small>{meta}</small> : null}
    </span>
  )
}
