import { createBrowserRouter } from 'react-router-dom'

import { AppShell } from './layout/AppShell'
import { DashboardPage } from '../pages/DashboardPage'
import { EventsPage } from '../pages/EventsPage'
import { NodeDetailPage } from '../pages/NodeDetailPage'
import { NodesPage } from '../pages/NodesPage'
import { SettingsPage } from '../pages/SettingsPage'
import { TargetDetailPage } from '../pages/TargetDetailPage'
import { TargetsPage } from '../pages/TargetsPage'

export const router = createBrowserRouter([
  {
    path: '/',
    element: <AppShell />,
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'nodes', element: <NodesPage /> },
      { path: 'nodes/:nodeId', element: <NodeDetailPage /> },
      { path: 'targets', element: <TargetsPage /> },
      { path: 'targets/:targetId', element: <TargetDetailPage /> },
      { path: 'events', element: <EventsPage /> },
      { path: 'settings', element: <SettingsPage /> },
    ],
  },
])
