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

export type EvidenceRenderDecision =
  | { status: 'rendered'; node: ReactNode }
  | { status: 'unsupported' }

function record(value: unknown): UnknownRecord | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null
  return value as UnknownRecord
}

function tupleKey(kind: string, schemaVersion: number, rendererVersion: string, readModelVersion: string): string {
  return `${kind}\u0000${schemaVersion}\u0000${rendererVersion}\u0000${readModelVersion}`
}

function buildRegistryIndex(registrations: readonly EvidenceRendererRegistration[]) {
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
  return { byTuple, hasDuplicate }
}

function decideEvidenceRender(
  byTuple: ReadonlyMap<string, EvidenceRendererRegistration>,
  hasDuplicate: boolean,
  evidence: unknown,
): EvidenceRenderDecision {
  if (hasDuplicate) return { status: 'unsupported' }
  const envelope = record(evidence)
  if (!envelope) return { status: 'unsupported' }
  const kind = envelope.kind
  const schemaVersion = envelope.schema_version
  const rendererVersion = envelope.renderer_version
  const readModel = record(envelope.read_model)
  const readModelVersion = readModel?.version
  if (typeof kind !== 'string' || typeof schemaVersion !== 'number' ||
    !Number.isInteger(schemaVersion) || typeof rendererVersion !== 'string' ||
    typeof readModelVersion !== 'string') return { status: 'unsupported' }
  const registration = byTuple.get(tupleKey(kind, schemaVersion, rendererVersion, readModelVersion))
  if (!registration) return { status: 'unsupported' }
  const decoded = registration.decode(readModel)
  if (decoded === null) return { status: 'unsupported' }
  return { status: 'rendered', node: registration.render(decoded) }
}

export function inspectEvidenceRenderability(
  registrations: readonly EvidenceRendererRegistration[],
  evidence: unknown,
): EvidenceRenderDecision {
  const { byTuple, hasDuplicate } = buildRegistryIndex(registrations)
  return decideEvidenceRender(byTuple, hasDuplicate, evidence)
}

export function createEvidenceRendererRegistry(
  registrations: readonly EvidenceRendererRegistration[],
): ComponentType<RegistryProps> {
  const { byTuple, hasDuplicate } = buildRegistryIndex(registrations)
  return function RegisteredEvidenceRenderer({ evidence }: RegistryProps): ReactNode {
    const decision = decideEvidenceRender(byTuple, hasDuplicate, evidence)
    return decision.status === 'rendered' ? decision.node : null
  }
}
