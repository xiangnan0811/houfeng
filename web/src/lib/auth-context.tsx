import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from 'react'
import * as client from './auth-client'
import { setUnauthorizedHandler } from './fetcher'

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
    setUnauthorizedHandler(() => setUser(null))
    refresh().finally(() => setLoading(false))
    return () => setUnauthorizedHandler(undefined)
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

export function useAuth(): AuthValue {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAuth must be inside <AuthProvider>')
  return v
}
