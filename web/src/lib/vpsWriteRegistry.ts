export type VPSWriteOperation =
  | 'facts'
  | 'decision'
  | 'link'
  | 'monitoring-create'
  | 'subscription'
  | 'validity-extension'
  | 'monitoring-unlink'
  | 'lifecycle'
  | 'cancellation'
  | 'experience'
  | 'service'
  | 'domain'

export type VPSWriteOwner = {
  vpsId: string
  token: string
  startedAt: string
  viewToken: string
  generation: number
  operation: VPSWriteOperation
  monitoringInstanceId?: string
}

export type VPSPreparedCreateOwner = VPSWriteOwner & {
  requestDigest: string
  idempotencyKey: string
}

type BeginVPSWrite = Pick<
  VPSWriteOwner,
  'vpsId' | 'viewToken' | 'generation' | 'operation' | 'monitoringInstanceId'
>

export type VPSCreateSettleOutcome =
  | 'confirmed'
  | 'unknown'
  | 'idempotency_key_reused'
  | 'not_sent'

export type VPSWriteOwnerStore = {
  getSnapshot: () => ReadonlyMap<string, VPSWriteOwner>
  subscribe: (listener: () => void) => () => void
  begin: (input: BeginVPSWrite) => VPSWriteOwner | null
  prepareCreate: (owner: VPSWriteOwner, wireBody: unknown) => Promise<VPSPreparedCreateOwner | null>
  finishCreate: (owner: VPSPreparedCreateOwner, outcome: VPSCreateSettleOutcome) => boolean
  finish: (owner: VPSWriteOwner) => boolean
}

type StableCreateAttempt = {
  requestDigest: string
  idempotencyKey: string
}

type PreparedCreateAttempt = {
  attemptKey: string
  attempt: StableCreateAttempt
  previousAttempt: StableCreateAttempt | undefined
  introducedAttempt: boolean
}

function canonicalizeJSON(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalizeJSON)
  if (value !== null && typeof value === 'object') {
    const record = value as Record<string, unknown>
    return Object.fromEntries(
      Object.keys(record)
        .sort()
        .map((key) => [key, canonicalizeJSON(record[key])]),
    )
  }
  return value
}

async function createRequestDigest(owner: VPSWriteOwner, wireBody: unknown): Promise<string> {
  const serializedWireBody = JSON.stringify(wireBody)
  const JSONWireBody = serializedWireBody === undefined ? null : JSON.parse(serializedWireBody) as unknown
  const canonicalRequest = JSON.stringify(canonicalizeJSON({
    vpsId: owner.vpsId,
    operation: owner.operation,
    body: JSONWireBody,
  }))
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(canonicalRequest))
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('')
}

function attemptKey(owner: Pick<VPSWriteOwner, 'vpsId' | 'operation'>): string {
  return `${owner.vpsId}\u0000${owner.operation}`
}

export function createVPSWriteOwnerStore(): VPSWriteOwnerStore {
  let snapshot: ReadonlyMap<string, VPSWriteOwner> = new Map()
  const attempts = new Map<string, StableCreateAttempt>()
  const preparedAttempts = new Map<string, PreparedCreateAttempt>()
  const listeners = new Set<() => void>()

  function publish(next: ReadonlyMap<string, VPSWriteOwner>) {
    snapshot = next
    for (const listener of listeners) listener()
  }

  function ownsCurrentWrite(owner: VPSWriteOwner): boolean {
    return snapshot.get(owner.vpsId)?.token === owner.token
  }

  return {
    getSnapshot: () => snapshot,
    subscribe(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    begin(input) {
      if (snapshot.has(input.vpsId)) return null
      const owner: VPSWriteOwner = {
        vpsId: input.vpsId,
        token: crypto.randomUUID(),
        startedAt: new Date().toISOString(),
        viewToken: input.viewToken,
        generation: input.generation,
        operation: input.operation,
        ...(input.monitoringInstanceId === undefined
          ? {}
          : { monitoringInstanceId: input.monitoringInstanceId }),
      }
      const next = new Map(snapshot)
      next.set(owner.vpsId, owner)
      publish(next)
      return owner
    },
    async prepareCreate(owner, wireBody) {
      const requestDigest = await createRequestDigest(owner, wireBody)
      if (!ownsCurrentWrite(owner)) return null

      const key = attemptKey(owner)
      const previousAttempt = attempts.get(key)
      const attempt = previousAttempt?.requestDigest === requestDigest
        ? previousAttempt
        : { requestDigest, idempotencyKey: crypto.randomUUID() }
      attempts.set(key, attempt)
      preparedAttempts.set(owner.token, {
        attemptKey: key,
        attempt,
        previousAttempt,
        introducedAttempt: attempt !== previousAttempt,
      })

      const preparedOwner: VPSPreparedCreateOwner = {
        ...owner,
        ...attempt,
      }
      return preparedOwner
    },
    finishCreate(owner, outcome) {
      if (!ownsCurrentWrite(owner)) return false

      const key = attemptKey(owner)
      const attempt = attempts.get(key)
      const preparedAttempt = preparedAttempts.get(owner.token)
      const settlesPreparedAttempt = Boolean(
        preparedAttempt?.attemptKey === key
        && preparedAttempt.attempt.requestDigest === owner.requestDigest
        && preparedAttempt.attempt.idempotencyKey === owner.idempotencyKey
        && attempt?.requestDigest === owner.requestDigest
        && attempt.idempotencyKey === owner.idempotencyKey,
      )
      if (settlesPreparedAttempt && outcome === 'confirmed') {
        attempts.delete(key)
      } else if (settlesPreparedAttempt && outcome === 'not_sent' && preparedAttempt?.introducedAttempt) {
        if (preparedAttempt.previousAttempt) {
          attempts.set(key, preparedAttempt.previousAttempt)
        } else {
          attempts.delete(key)
        }
      } else if (settlesPreparedAttempt && outcome === 'idempotency_key_reused') {
        attempts.set(key, {
          requestDigest: owner.requestDigest,
          idempotencyKey: crypto.randomUUID(),
        })
      }
      preparedAttempts.delete(owner.token)

      const next = new Map(snapshot)
      next.delete(owner.vpsId)
      publish(next)
      return true
    },
    finish(owner) {
      if (!ownsCurrentWrite(owner)) return false
      preparedAttempts.delete(owner.token)
      const next = new Map(snapshot)
      next.delete(owner.vpsId)
      publish(next)
      return true
    },
  }
}
