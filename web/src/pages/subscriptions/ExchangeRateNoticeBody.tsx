import type { ReactNode } from 'react'

import type { ExchangeRateNotice } from './exchangeRatePresentation'

type Props = {
  notice: ExchangeRateNotice
  className?: string
  children?: ReactNode
}

/** Shared status copy. Raw provider summaries stay inside a closed diagnostic. */
export function ExchangeRateNoticeBody({ notice, className, children }: Props) {
  const diagnostics = notice.diagnostics ?? []
  return (
    <div className={className} role={notice.tone === 'error' ? 'alert' : 'status'}>
      <span className="exchange-rate-notice__text">{notice.text}</span>
      {diagnostics.length > 0 ? (
        <details className="exchange-rate-notice__diagnostics">
          <summary>补取诊断</summary>
          <dl>
            {diagnostics.map((item, index) => (
              <div key={`${item.label}\0${index}`}>
                <dt>{item.label}</dt>
                <dd>{item.detail}</dd>
              </div>
            ))}
          </dl>
        </details>
      ) : null}
      {children}
    </div>
  )
}
