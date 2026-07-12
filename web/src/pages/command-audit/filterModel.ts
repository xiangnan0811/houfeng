import { COMMAND_LIST } from '../../config/commands'
import type {
  CommandAuditListFilter,
  CommandAuditOutcome,
  CommandAuditWindow,
} from '../../lib/types'
import type { CommandAuditFilters } from './types'

const ALLOWED_WINDOWS = new Set<CommandAuditWindow>(['24h', '7d', '30d', 'all', 'custom'])
const ALLOWED_OUTCOMES = new Set<CommandAuditOutcome>([
  'rejected',
  'queued',
  'dispatched',
  'succeeded',
  'failed',
])
const ALLOWED_COMMANDS = new Set(COMMAND_LIST.map((command) => command.id))
const DATE_TIME_PATTERN = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2})(?:\.\d{1,9})?)?(?:Z|[+-]\d{2}:\d{2})?$/

export const DEFAULT_COMMAND_AUDIT_FILTERS: CommandAuditFilters = {
  window: '30d',
  started_from: '',
  started_to: '',
  monitoring_instance: '',
  command_id: '',
  sensitivity: '',
  outcome: '',
  actor: '',
  action_id: '',
}

function isWindow(value: string): value is CommandAuditWindow {
  return ALLOWED_WINDOWS.has(value as CommandAuditWindow)
}

function isOutcome(value: string): value is CommandAuditOutcome {
  return ALLOWED_OUTCOMES.has(value as CommandAuditOutcome)
}

function validDate(value: string): boolean {
  const match = DATE_TIME_PATTERN.exec(value)
  if (!match) return false

  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const hour = Number(match[4])
  const minute = Number(match[5])
  const second = Number(match[6] ?? 0)
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
  const daysInMonth = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1]

  return year >= 1
    && daysInMonth !== undefined
    && day >= 1
    && day <= daysInMonth
    && hour >= 0
    && hour <= 23
    && minute >= 0
    && minute <= 59
    && second >= 0
    && second <= 59
    && !Number.isNaN(Date.parse(value))
}

function normalizeFilters(filters: CommandAuditFilters): CommandAuditFilters {
  const requestedWindow = isWindow(filters.window) ? filters.window : '30d'
  const startedFrom = filters.started_from.trim()
  const startedTo = filters.started_to.trim()
  const validCustom = requestedWindow === 'custom'
    && validDate(startedFrom)
    && validDate(startedTo)
    && Date.parse(startedFrom) < Date.parse(startedTo)
  const window = requestedWindow === 'custom' && !validCustom ? '30d' : requestedWindow
  const commandID = filters.command_id.trim()
  const sensitivity = filters.sensitivity === 'standard' || filters.sensitivity === 'sensitive'
    ? filters.sensitivity
    : ''
  const outcome = isOutcome(filters.outcome) ? filters.outcome : ''

  return {
    window,
    started_from: validCustom ? startedFrom : '',
    started_to: validCustom ? startedTo : '',
    monitoring_instance: filters.monitoring_instance.trim(),
    command_id: ALLOWED_COMMANDS.has(commandID) ? commandID : '',
    sensitivity,
    outcome,
    actor: filters.actor.trim(),
    action_id: filters.action_id.trim(),
  }
}

export function commandAuditFiltersFromSearchParams(searchParams: URLSearchParams): CommandAuditFilters {
  const rawWindow = (searchParams.get('window') ?? '').trim()
  return normalizeFilters({
    window: isWindow(rawWindow) ? rawWindow : '30d',
    started_from: searchParams.get('started_from') ?? '',
    started_to: searchParams.get('started_to') ?? '',
    monitoring_instance: searchParams.get('monitoring_instance') ?? '',
    command_id: searchParams.get('command_id') ?? '',
    sensitivity: (searchParams.get('sensitivity') ?? '') as CommandAuditFilters['sensitivity'],
    outcome: (searchParams.get('outcome') ?? '') as CommandAuditFilters['outcome'],
    actor: searchParams.get('actor') ?? '',
    action_id: searchParams.get('action_id') ?? '',
  })
}

export function commandAuditSearchParamsFromFilters(filters: CommandAuditFilters): URLSearchParams {
  const normalized = normalizeFilters(filters)
  const params = new URLSearchParams()
  if (normalized.window !== '30d') params.set('window', normalized.window)
  if (normalized.window === 'custom') {
    params.set('started_from', normalized.started_from)
    params.set('started_to', normalized.started_to)
  }
  if (normalized.monitoring_instance) params.set('monitoring_instance', normalized.monitoring_instance)
  if (normalized.command_id) params.set('command_id', normalized.command_id)
  if (normalized.outcome) params.set('outcome', normalized.outcome)
  if (normalized.sensitivity) params.set('sensitivity', normalized.sensitivity)
  if (normalized.actor) params.set('actor', normalized.actor)
  if (normalized.action_id) params.set('action_id', normalized.action_id)
  return params
}

export function commandAuditFilterKey(filters: CommandAuditFilters): string {
  return commandAuditSearchParamsFromFilters(filters).toString()
}

export function commandAuditToAPIQuery(filters: CommandAuditFilters): CommandAuditListFilter {
  const normalized = normalizeFilters(filters)
  const query: CommandAuditListFilter = {}
  if (normalized.window !== '30d') query.window = normalized.window
  if (normalized.window === 'custom') {
    query.started_from = new Date(normalized.started_from).toISOString()
    query.started_to = new Date(normalized.started_to).toISOString()
  }
  if (normalized.monitoring_instance) query.monitoring_instance = normalized.monitoring_instance
  if (normalized.command_id) query.command_id = normalized.command_id
  if (normalized.outcome) query.outcome = normalized.outcome
  if (normalized.sensitivity) query.sensitivity = normalized.sensitivity
  if (normalized.actor) query.actor = normalized.actor
  if (normalized.action_id) query.action_id = normalized.action_id
  return query
}

export function normalizeCommandAuditFilters(filters: CommandAuditFilters): CommandAuditFilters {
  return normalizeFilters(filters)
}
