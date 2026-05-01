import { useEffect, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { ChangePasswordModal } from './ChangePasswordModal'
import { TopBar } from './TopBar'
import { useAuth } from '../../lib/auth-context'
import { PRODUCT_FULL_NAME_ZH } from '../metadata'

export function AppShell() {
  const { user, logout } = useAuth()
  const [changePwOpen, setChangePwOpen] = useState(false)

  useEffect(() => {
    document.title = PRODUCT_FULL_NAME_ZH
  }, [])

  if (!user) return null

  return (
    <div className="app-shell">
      <Sidebar
        user={user}
        sync={{
          state: 'ok',
          version: 'v1.0',
          lastSync: new Date().toISOString(),
        }}
        anomalyCounts={{ nodes: 0, targets: 0 }}
        onLogout={() => {
          void logout()
        }}
        onChangePassword={() => setChangePwOpen(true)}
      />
      <main className="app-shell__main">
        <TopBar />
        <Outlet />
      </main>
      {changePwOpen && <ChangePasswordModal onClose={() => setChangePwOpen(false)} />}
    </div>
  )
}
