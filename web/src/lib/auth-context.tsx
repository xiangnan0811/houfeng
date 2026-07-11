import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from 'react'
import * as client from './auth-client'
import { setUnauthorizedHandler } from './api'

export interface AuthValue {
  user: client.User | null
  loading: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const Ctx = createContext<AuthValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<client.User | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    setUser(await client.me())
  }, [])

  useEffect(() => {
    const drop = () => setUser(null)
    setUnauthorizedHandler(drop)
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial-load gate: setLoading(false) runs in async .finally() after refresh() resolves, not synchronously in effect body
    void refresh()
      .catch(() => setUser(null))
      .finally(() => setLoading(false))
    return () => {
      setUnauthorizedHandler(undefined)
    }
  }, [refresh])

  const login = useCallback(async (u: string, p: string) => {
    const fresh = await client.login(u, p)
    setUser(fresh)
  }, [])

  const logout = useCallback(async () => {
    await client.logout()
    setUser(null)
  }, [])

  return <Ctx.Provider value={{ user, loading, login, logout, refresh }}>{children}</Ctx.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components -- early-stage Provider+hook colocation; split when stable
export function useAuth(): AuthValue {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAuth must be inside <AuthProvider>')
  return v
}
