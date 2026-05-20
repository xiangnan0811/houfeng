import type { ReactNode } from 'react'

type SectionIntroProps = {
  children: ReactNode
}

export function SectionIntro({ children }: SectionIntroProps) {
  return <div className="settings-section-intro">{children}</div>
}
