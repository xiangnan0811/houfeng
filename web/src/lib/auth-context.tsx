import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import * as client from './auth-client'
import { setUnauthorizedHandler } from './apiRequest'

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

  const refresh = useCallback(async () => {
    try {
      const next = await client.me()
      userRef.current = next
      setUser(next)
      setError(null)
      setStatus(next ? 'ready' : 'anonymous')
    } catch {
      // Keep a prior identity snapshot, but do not treat this session as ready
      // and do not surface the underlying exception.
      if (!userRef.current) setUser(null)
      setStatus('error')
      setError(CAPABILITY_READ_ERROR)
    }
  }, [])

  useEffect(() => {
    const drop = () => {
      userRef.current = null
      setUser(null)
      setStatus('anonymous')
      setError(null)
    }
    setUnauthorizedHandler(drop)
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial-load gate: setLoading(false) runs in async .finally() after refresh() resolves, not synchronously in effect body
    void refresh().finally(() => setLoading(false))
    return () => {
      setUnauthorizedHandler(undefined)
    }
  }, [refresh])

  const login = useCallback(async (u: string, p: string) => {
    const fresh = await client.login(u, p)
    userRef.current = fresh
    setUser(fresh)
    setStatus('ready')
    setError(null)
  }, [])

  const logout = useCallback(async () => {
    const userId = userRef.current?.user_id ?? ''
    await client.logout()
    userRef.current = null
    setUser(null)
    setStatus('anonymous')
    setError(null)
    void import('../pages/records/draftBuffer').then((mod) => mod.discardUserDrafts(userId))
    void import('./recordSecurity').then((mod) => mod.broadcastRecordSessionEnd(userId, 'logout'))
  }, [])

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
