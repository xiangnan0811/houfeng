import type {
  CommandAuditOutcome,
  CommandAuditWindow,
} from '../../lib/types'

export type CommandAuditFilters = {
  window: CommandAuditWindow
  started_from: string
  started_to: string
  monitoring_instance: string
  command_id: string
  sensitivity: '' | 'standard' | 'sensitive'
  outcome: '' | CommandAuditOutcome
  actor: string
  action_id: string
}

export type CommandAuditPageState = {
  loading: boolean
  loadingMore: boolean
  error: string | null
}
