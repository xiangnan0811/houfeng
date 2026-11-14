import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import * as client from './auth-client'
import { setUnauthorizedHandler } from './apiRequest'
import { clearComparisonSaveAttempts } from './comparisonSaveAttempts'

export type AuthSnapshotStatus = 'loading' | 'anonymous' | 'ready' | 'error'

export interface AuthValue {
  user: client.User | null
  loading: boolean
  status: AuthSnapshotStatus
  error: string | null
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
  retry: () => Promise<void>
}

const Ctx = createContext<AuthValue | null>(null)

const CAPABILITY_READ_ERROR = '能力读取失败'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<client.User | null>(null)
  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState<AuthSnapshotStatus>('loading')
  const [error, setError] = useState<string | null>(null)
  const userRef = useRef<client.User | null>(null)
  // A 401 suspends access but does not establish whether a write committed.
  const suspendedUserRef = useRef<string | null>(null)
  const pendingLogoutUserRef = useRef<string | null>(null)
  const pendingComparisonUserRef = useRef<string | null>(null)
  const logoutCleanupRef = useRef<Promise<boolean> | null>(null)
  const sessionGenerationRef = useRef(0)
  const cleanupFailedRef = useRef(false)
  const [cleanupRevision, setCleanupRevision] = useState(0)

  const finishLogoutCleanup = useCallback((): Promise<boolean> => {
    if (logoutCleanupRef.current) return logoutCleanupRef.current
    const userId = pendingLogoutUserRef.current ?? pendingComparisonUserRef.current
    if (!userId) return Promise.resolve(true)
    const cleanup = (async () => {
      try {
        clearComparisonSaveAttempts(userId)
        if (pendingLogoutUserRef.current) {
          const [drafts, security] = await Promise.all([
            import('../pages/records/draftBuffer'),
            import('./recordSecurity'),
          ])
          security.broadcastRecordSessionEnd(userId, 'logout')
          await drafts.discardUserDrafts(userId)
          pendingLogoutUserRef.current = null
        }
        pendingComparisonUserRef.current = null
        if (suspendedUserRef.current === userId) suspendedUserRef.current = null
        setStatus('anonymous')
        setError(null)
        return true
      } catch {
        setStatus('error')
        setError(pendingLogoutUserRef.current
          ? '会话已退出，但本地草稿清理失败，请重试'
          : '会话已结束，但本地比较重试状态清理失败，请重试')
        return false
      }
    })().finally(() => { logoutCleanupRef.current = null })
    logoutCleanupRef.current = cleanup
    return cleanup
  }, [])

  const refresh = useCallback(async () => {
    if (pendingLogoutUserRef.current || pendingComparisonUserRef.current) {
      await finishLogoutCleanup()
      return
    }
    const generation = sessionGenerationRef.current
    try {
      const next = await client.me()
      if (generation !== sessionGenerationRef.current) return
      const previousUserId = userRef.current?.user_id ?? suspendedUserRef.current
      if (previousUserId && next && previousUserId !== next.user_id) {
        pendingComparisonUserRef.current = previousUserId
        userRef.current = null
        setUser(null)
        if (!await finishLogoutCleanup()) return
        if (generation !== sessionGenerationRef.current) return
      }
      if (next) suspendedUserRef.current = null
      userRef.current = next
      setUser(next)
      setError(null)
      setStatus(next ? 'ready' : 'anonymous')
      if (cleanupFailedRef.current) {
        cleanupFailedRef.current = false
        setCleanupRevision((value) => value + 1)
      }
    } catch {
      if (generation !== sessionGenerationRef.current) return
      // Keep a prior identity snapshot, but do not treat this session as ready
      // and do not surface the underlying exception.
      if (!userRef.current) setUser(null)
      setStatus('error')
      setError(CAPABILITY_READ_ERROR)
    }
  }, [finishLogoutCleanup])

  useEffect(() => {
    const drop = () => {
      sessionGenerationRef.current += 1
      const previousUserId = userRef.current?.user_id
      userRef.current = null
      setUser(null)
      if (pendingLogoutUserRef.current) return
      setStatus('anonymous')
      setError(null)
      if (previousUserId) suspendedUserRef.current = previousUserId
    }
    setUnauthorizedHandler(drop)
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial-load gate: setLoading(false) runs in async .finally() after refresh() resolves, not synchronously in effect body
    void refresh().finally(() => setLoading(false))
    return () => {
      setUnauthorizedHandler(undefined)
    }
  }, [refresh])

  const cleanupUserId = user?.user_id
  const cleanupEnabled = user?.runtime_capabilities?.records === true && status === 'ready'
  useEffect(() => {
    if (!cleanupUserId || !cleanupEnabled) return
    let disposed = false
    let stop: (() => void) | undefined
    const generation = sessionGenerationRef.current
    const onError = () => {
      if (disposed || generation !== sessionGenerationRef.current || userRef.current?.user_id !== cleanupUserId) return
      cleanupFailedRef.current = true
      setError('记录客户端清理同步已暂停，请重试；本地草稿未因连接中断删除')
    }
    void import('./recordClientCleanupConsumer').then(({ startRecordClientCleanup }) => {
      if (disposed) return
      stop = startRecordClientCleanup(cleanupUserId, { onError }).stop
    }).catch(onError)
    return () => { disposed = true; stop?.() }
  }, [cleanupEnabled, cleanupRevision, cleanupUserId])

  const login = useCallback(async (u: string, p: string) => {
    if (!await finishLogoutCleanup()) throw new Error('本地草稿清理尚未完成')
    const generation = sessionGenerationRef.current
    const fresh = await client.login(u, p)
    if (generation !== sessionGenerationRef.current) return
    const previousUserId = userRef.current?.user_id ?? suspendedUserRef.current
    if (previousUserId && previousUserId !== fresh.user_id) {
      pendingComparisonUserRef.current = previousUserId
      userRef.current = null
      setUser(null)
      if (!await finishLogoutCleanup()) throw new Error('本地比较重试状态清理尚未完成')
      if (generation !== sessionGenerationRef.current) return
    }
    suspendedUserRef.current = null
    sessionGenerationRef.current += 1
    userRef.current = fresh
    setUser(fresh)
    setStatus('ready')
    setError(null)
  }, [finishLogoutCleanup])

  const logout = useCallback(async () => {
    const userId = userRef.current?.user_id ?? suspendedUserRef.current ?? ''
    await client.logout()
    sessionGenerationRef.current += 1
    pendingLogoutUserRef.current = userId || null
    userRef.current = null
    setUser(null)
    setStatus(userId ? 'loading' : 'anonymous')
    setError(null)
    // Keep cleanup failure visible and retryable; never report successful
    // logout cleanup after a rejected IndexedDB transaction.
    await finishLogoutCleanup()
  }, [finishLogoutCleanup])

  const retry = useCallback(async () => {
    await refresh()
  }, [refresh])

  return (
    <Ctx.Provider value={{ user, loading, status, error, login, logout, refresh, retry }}>
      {children}
    </Ctx.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components -- early-stage Provider+hook colocation; split when stable
export function useAuth(): AuthValue {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAuth must be inside <AuthProvider>')
  return v
}
