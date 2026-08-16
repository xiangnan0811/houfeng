import type { ComponentType, ReactNode } from 'react'

export type EvidenceRendererRegistration = {
  kind: string
  schema_version: number
  renderer_version: string
  read_model_version: string
  decode: (value: unknown) => unknown | null
  render: (value: unknown) => ReactNode
}

type RegistryProps = {
  evidence: unknown
}

type UnknownRecord = Record<string, unknown>

function record(value: unknown): UnknownRecord | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null
  return value as UnknownRecord
}

function tupleKey(kind: string, schemaVersion: number, rendererVersion: string, readModelVersion: string): string {
  return `${kind}\u0000${schemaVersion}\u0000${rendererVersion}\u0000${readModelVersion}`
}

export function createEvidenceRendererRegistry(
  registrations: readonly EvidenceRendererRegistration[],
): ComponentType<RegistryProps> {
  const byTuple = new Map<string, EvidenceRendererRegistration>()
  let hasDuplicate = false
  for (const registration of registrations) {
    const key = tupleKey(
      registration.kind,
      registration.schema_version,
      registration.renderer_version,
      registration.read_model_version,
    )
    if (byTuple.has(key)) hasDuplicate = true
    byTuple.set(key, registration)
  }

  return function RegisteredEvidenceRenderer({ evidence }: RegistryProps): ReactNode {
    if (hasDuplicate) return null
    const envelope = record(evidence)
    if (!envelope) return null
    const kind = envelope.kind
    const schemaVersion = envelope.schema_version
    const rendererVersion = envelope.renderer_version
    const readModel = record(envelope.read_model)
    const readModelVersion = readModel?.version
    if (typeof kind !== 'string' || typeof schemaVersion !== 'number' ||
      !Number.isInteger(schemaVersion) || typeof rendererVersion !== 'string' ||
      typeof readModelVersion !== 'string') return null
    const registration = byTuple.get(tupleKey(kind, schemaVersion, rendererVersion, readModelVersion))
    if (!registration) return null
    const decoded = registration.decode(readModel)
    return decoded === null ? null : registration.render(decoded)
  }
}
