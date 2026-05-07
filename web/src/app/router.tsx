import { createBrowserRouter, type RouteObject, Navigate } from 'react-router-dom'

import { AppShell } from './layout/AppShell'
import { RequireAuth } from './RequireAuth'
import { DashboardPage } from '../pages/DashboardPage'
import { EventsPage } from '../pages/EventsPage'
import { LoginPage } from '../pages/LoginPage'
import { NodeComparePage } from '../pages/NodeComparePage'
import { NodeDetailPage } from '../pages/NodeDetailPage'
import { NodeOnboardingPage } from '../pages/NodeOnboardingPage'
import { NodesPage } from '../pages/NodesPage'
import { SettingsPage } from '../pages/SettingsPage'
import { TargetDetailPage } from '../pages/TargetDetailPage'
import { TargetsPage } from '../pages/TargetsPage'

export const appRoutes: RouteObject[] = [
  { path: '/login', element: <LoginPage /> },
  {
    element: <RequireAuth />,
    children: [
      {
        path: '/',
        element: <AppShell />,
        children: [
          { index: true, element: <DashboardPage /> },
          { path: 'nodes', element: <NodesPage /> },
          { path: 'nodes/compare', element: <NodeComparePage /> },
          { path: 'nodes/:nodeId', element: <NodeDetailPage /> },
          { path: 'nodes/:nodeId/onboarding', element: <NodeOnboardingPage /> },
          { path: 'targets', element: <TargetsPage /> },
          { path: 'targets/:targetId', element: <TargetDetailPage /> },
          { path: 'events', element: <EventsPage /> },
          { path: 'settings', element: <SettingsPage /> },
          { path: '*', element: <Navigate to="/" replace /> },
        ],
      },
    ],
  },
]

export const router = createBrowserRouter(appRoutes)
