import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../lib/auth-context'
import { CapabilityReadError } from './RuntimeCapabilityGate'

export function RequireAuth() {
  const { user, loading, status, error, retry } = useAuth()
  const location = useLocation()
  if (loading || status === 'loading') return null
  if (status === 'error') return <CapabilityReadError message={error} onRetry={retry} />
  if (!user) {
    const next = encodeURIComponent(location.pathname + location.search)
    return <Navigate to={`/login?next=${next}`} replace />
  }
  return <Outlet />
}
