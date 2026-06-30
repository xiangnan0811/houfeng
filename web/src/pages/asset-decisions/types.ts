import type { BadgeTone } from '../../components/atoms'

export type SecondaryWorkbench = 'records' | 'scenarios' | 'renewals' | 'single_queue'

export type AssetDecisionSecondaryNavItem = {
  value: SecondaryWorkbench
  eyebrow: string
  title: string
  summary: string
  meta: string
  actionLabel: string
  tone: BadgeTone
}
