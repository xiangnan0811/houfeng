export type ModalStackEntry = {
  id: string
  container: HTMLElement
  restoreTarget: HTMLElement | null
  parentId?: string | null
}

type RegisteredModal = ModalStackEntry & {
  token: symbol
  registrationOrder: number
}

const listeners = new Set<() => void>()
let stack: RegisteredModal[] = []
let bodyScrollLockCount = 0
let previousBodyOverflow = ''
let nextRegistrationOrder = 0

export function registerModal(entry: ModalStackEntry): () => void {
  const token = Symbol(entry.id)
  const registration: RegisteredModal = {
    ...entry,
    parentId: entry.parentId ?? null,
    token,
    registrationOrder: nextRegistrationOrder,
  }
  nextRegistrationOrder += 1
  stack = orderModalStack([...stack.filter((modal) => modal.id !== entry.id), registration])
  notifyModalStack()

  let registered = true
  return () => {
    if (!registered) return
    registered = false

    const registrationIndex = stack.findIndex((modal) => modal.token === token)
    if (registrationIndex < 0) return

    stack = orderModalStack(stack.filter((_, index) => index !== registrationIndex))
    notifyModalStack()
  }
}

export function isTopModal(id: string): boolean {
  return stack.at(-1)?.id === id
}

export function getModalDepth(id: string): number {
  const index = stack.findIndex((modal) => modal.id === id)
  return index < 0 ? 0 : index + 1
}

export function subscribeModalStack(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function acquireBodyScrollLock(): () => void {
  if (bodyScrollLockCount === 0) {
    previousBodyOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
  bodyScrollLockCount += 1

  let acquired = true
  return () => {
    if (!acquired) return
    acquired = false
    bodyScrollLockCount = Math.max(0, bodyScrollLockCount - 1)
    if (bodyScrollLockCount === 0) {
      document.body.style.overflow = previousBodyOverflow
      previousBodyOverflow = ''
    }
  }
}

function notifyModalStack() {
  listeners.forEach((listener) => listener())
}

function orderModalStack(entries: RegisteredModal[]) {
  const remaining = [...entries].sort(
    (left, right) => left.registrationOrder - right.registrationOrder,
  )
  const registeredIds = new Set(remaining.map((entry) => entry.id))
  const orderedIds = new Set<string>()
  const ordered: RegisteredModal[] = []

  while (remaining.length > 0) {
    const nextIndex = remaining.findIndex(
      (entry) =>
        !entry.parentId ||
        !registeredIds.has(entry.parentId) ||
        orderedIds.has(entry.parentId),
    )

    if (nextIndex < 0) return [...ordered, ...remaining]

    const [nextEntry] = remaining.splice(nextIndex, 1)
    if (!nextEntry) return [...ordered, ...remaining]
    ordered.push(nextEntry)
    orderedIds.add(nextEntry.id)
  }

  return ordered
}
