import type { OnlineEvidenceStreamMessage } from '../../lib/types'

// A stream timestamp must carry its timezone; locale dates would silently use
// the browser clock's timezone and are not valid server evidence.
function validTimestamp(value: unknown): value is string {
  if (typeof value !== 'string') return false
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-](\d{2}):(\d{2}))$/.exec(value)
  if (!parts || !Number.isFinite(Date.parse(value))) return false
  const [, year, month, day, hour, minute, second, offsetHour, offsetMinute] = parts
  const date = new Date(0)
  date.setUTCFullYear(Number(year), Number(month) - 1, Number(day))
  return date.getUTCFullYear() === Number(year) && date.getUTCMonth() === Number(month) - 1 && date.getUTCDate() === Number(day) &&
    Number(hour) < 24 && Number(minute) < 60 && Number(second) < 60 && Number(offsetHour ?? 0) < 24 && Number(offsetMinute ?? 0) < 60
}

export function acceptOnlineEvidence(
  current: OnlineEvidenceStreamMessage | null,
  incoming: unknown,
  monitoringInstanceId: string,
): OnlineEvidenceStreamMessage | null {
  if (!incoming || typeof incoming !== 'object') return current
  const message = incoming as Partial<OnlineEvidenceStreamMessage>
  if (message.type !== 'online_evidence' || message.monitoring_instance_id !== monitoringInstanceId) return current
  if (!validTimestamp(message.read_at)) return current
  if (message.last_trusted_online_at !== null && !validTimestamp(message.last_trusted_online_at)) return current
  if (message.last_trusted_online_at !== null && Date.parse(message.last_trusted_online_at) > Date.parse(message.read_at)) return current
  if (current && Date.parse(message.read_at) <= Date.parse(current.read_at)) return current
  return message as OnlineEvidenceStreamMessage
}
