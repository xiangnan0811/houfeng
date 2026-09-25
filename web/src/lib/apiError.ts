import type { ApiErrorDetails, ApiFieldError } from './apiRequest'

export default function allowlistedApiError(
  errorBody: unknown,
  rawBody: string,
): [message: string, details: ApiErrorDetails<unknown>] {
  if (typeof errorBody !== 'object' || errorBody === null) return [rawBody, {}]

  const body = errorBody as Record<string, unknown>
  const candidateMessage = body.error ?? body.message
  const fieldErrors = readFieldErrors(body.field_errors)
  return [typeof candidateMessage === 'string' ? candidateMessage : rawBody, {
    code: typeof body.code === 'string' ? body.code : undefined,
    field_errors: fieldErrors,
    recovery: body.recovery,
  }]
}

function readFieldErrors(value: unknown): ApiFieldError[] {
  if (Array.isArray(value)) {
    return value.flatMap((item) => {
      if (typeof item !== 'object' || item === null) return []
      const record = item as Record<string, unknown>
      return typeof record.field === 'string' && typeof record.message === 'string'
        ? [{ field: record.field, message: record.message }]
        : []
    })
  }
  if (typeof value !== 'object' || value === null) return []
  return Object.entries(value).flatMap(([field, message]) => (
    typeof message === 'string' ? [{ field, message }] : []
  ))
}
