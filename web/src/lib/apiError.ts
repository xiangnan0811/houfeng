import type { ApiErrorDetails, ApiFieldError } from './apiRequest'

export default function allowlistedApiError(
  errorBody: unknown,
  rawBody: string,
): [message: string, details: ApiErrorDetails<unknown>] {
  if (typeof errorBody !== 'object' || errorBody === null) return [rawBody, {}]

  const body = errorBody as Record<string, unknown>
  const candidateMessage = body.error ?? body.message
  const fieldErrors = Array.isArray(body.field_errors)
    ? body.field_errors.filter((item): item is ApiFieldError => (
        typeof item === 'object' && item !== null
        && typeof (item as Record<string, unknown>).field === 'string'
        && typeof (item as Record<string, unknown>).message === 'string'
      )).map((item) => ({ field: item.field, message: item.message }))
    : []
  return [typeof candidateMessage === 'string' ? candidateMessage : rawBody, {
    code: typeof body.code === 'string' ? body.code : undefined,
    field_errors: fieldErrors,
    recovery: body.recovery,
  }]
}
