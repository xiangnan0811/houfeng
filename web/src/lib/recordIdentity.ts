// Public record IDs have two grammars: the legacy `rec_` domain kept for existing
// records and completed-result replay, and the `rec2_` domain issued by server
// reservations. Syntax only names a domain; it never proves allocation, ownership
// or birth certification, so callers must not infer authority from it.
const LEGACY_RECORD_ID = /^rec_[a-z0-9]{1,64}(?![\s\S])/
const RESERVED_RECORD_ID = /^rec2_[0-9a-f]{32}(?![\s\S])/

export function isRecordId(value: string): boolean {
  return LEGACY_RECORD_ID.test(value) || RESERVED_RECORD_ID.test(value)
}
