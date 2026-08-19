import { createElement, lazy, Suspense, type ComponentType } from 'react'
import { createBrowserRouter, Navigate, type RouteObject } from 'react-router-dom'

import { AppShell } from './layout/AppShell'
import { RequireAuth } from './RequireAuth'
import { RouteErrorPage } from './RouteErrorPage'
import { RouteModuleFallback } from './RouteModuleFallback'

const assetDecisionsPage = lazy(() =>
  import('../pages/AssetDecisionsPage').then((module) => ({ default: module.AssetDecisionsPage })),
)
const archivePage = lazy(() =>
  import('../pages/ArchivePage').then((module) => ({ default: module.ArchivePage })),
)
const archiveDetailPage = lazy(() =>
  import('../pages/ArchiveDetailPage').then((module) => ({ default: module.ArchiveDetailPage })),
)
const dashboardPage = lazy(() =>
  import('../pages/DashboardPage').then((module) => ({ default: module.DashboardPage })),
)
const eventsPage = lazy(() =>
  import('../pages/EventsPage').then((module) => ({ default: module.EventsPage })),
)
const commandAuditPage = lazy(() =>
  import('../pages/CommandAuditPage').then((module) => ({ default: module.CommandAuditPage })),
)
const loginPage = lazy(() =>
  import('../pages/LoginPage').then((module) => ({ default: module.LoginPage })),
)
const monitoringComparePage = lazy(() =>
  import('../pages/MonitoringComparePage').then((module) => ({ default: module.MonitoringComparePage })),
)
const monitoringDetailPage = lazy(() =>
  import('../pages/MonitoringDetailPage').then((module) => ({ default: module.MonitoringDetailPage })),
)
const monitoringPage = lazy(() =>
  import('../pages/MonitoringPage').then((module) => ({ default: module.MonitoringPage })),
)
const providersPage = lazy(() =>
  import('../pages/ProvidersPage').then((module) => ({ default: module.ProvidersPage })),
)
const recordInboxPage = lazy(() =>
  import('../pages/RecordInboxPage').then((module) => ({ default: module.RecordInboxPage })),
)
const recordSearchPage = lazy(() =>
  import('../pages/records/RecordSearchPage').then((module) => ({ default: module.RecordSearchPage })),
)
const recordDraftsPage = lazy(() =>
  import('../pages/records/RecordDraftsPage').then((module) => ({ default: module.RecordDraftsPage })),
)
const recordNewPage = lazy(() =>
  import('../pages/records/RecordNewPage').then((module) => ({ default: module.RecordNewPage })),
)
const recordDetailPage = lazy(() =>
  import('../pages/records/RecordDetailPage').then((module) => ({ default: module.RecordDetailPage })),
)
const recordEditPage = lazy(() =>
  import('../pages/records/RecordEditPage').then((module) => ({ default: module.RecordEditPage })),
)
const recordRevisionPage = lazy(() =>
  import('../pages/records/RecordRevisionPage').then((module) => ({ default: module.RecordRevisionPage })),
)
const settingsPage = lazy(() =>
  import('../pages/SettingsPage').then((module) => ({ default: module.SettingsPage })),
)
const subscriptionsPage = lazy(() =>
  import('../pages/SubscriptionsPage').then((module) => ({ default: module.SubscriptionsPage })),
)
const targetDetailPage = lazy(() =>
  import('../pages/TargetDetailPage').then((module) => ({ default: module.TargetDetailPage })),
)
const targetsPage = lazy(() =>
  import('../pages/TargetsPage').then((module) => ({ default: module.TargetsPage })),
)
const vpsDetailPage = lazy(() =>
  import('../pages/VPSDetailPage').then((module) => ({ default: module.VPSDetailPage })),
)
const vpsIPQualityPage = lazy(() =>
  import('../pages/VPSIPQualityPage').then((module) => ({ default: module.VPSIPQualityPage })),
)
const vpsPage = lazy(() =>
  import('../pages/VPSPage').then((module) => ({ default: module.VPSPage })),
)

function routeElement(Component: ComponentType, loadingLabel: string) {
  return (
    <Suspense fallback={<RouteModuleFallback label={loadingLabel} />}>
      {createElement(Component)}
    </Suspense>
  )
}

export const appRoutes: RouteObject[] = [
  {
    path: '/login',
    element: routeElement(loginPage, '正在加载登录页'),
    errorElement: <RouteErrorPage />,
  },
  {
    element: <RequireAuth />,
    errorElement: <RouteErrorPage />,
    children: [
      {
        path: '/',
        element: <AppShell />,
        children: [
          { index: true, element: routeElement(dashboardPage, '正在加载工作台') },
          { path: 'vps', element: routeElement(vpsPage, '正在加载 VPS 库存') },
          { path: 'vps/:vpsId/ip-quality', element: routeElement(vpsIPQualityPage, '正在加载 IP 质量报告') },
          { path: 'vps/:vpsId', element: routeElement(vpsDetailPage, '正在加载 VPS 详情') },
          { path: 'archive', element: routeElement(archivePage, '正在加载归档资产') },
          { path: 'archive/:vpsId', element: routeElement(archiveDetailPage, '正在加载归档详情') },
          { path: 'providers', element: routeElement(providersPage, '正在加载服务商') },
          { path: 'record-inbox', element: routeElement(recordInboxPage, '正在加载记录通知') },
          { path: 'records', element: routeElement(recordSearchPage, '正在加载运维记录') },
          { path: 'records/drafts', element: routeElement(recordDraftsPage, '正在加载记录草稿') },
          { path: 'records/new', element: routeElement(recordNewPage, '正在加载新建记录') },
          { path: 'records/:recordId/edit', element: routeElement(recordEditPage, '正在加载记录编辑') },
          { path: 'records/:recordId/revisions/:revisionId', element: routeElement(recordRevisionPage, '正在加载历史修订') },
          { path: 'records/:recordId', element: routeElement(recordDetailPage, '正在加载运维记录') },
          { path: 'subscriptions', element: routeElement(subscriptionsPage, '正在加载订阅') },
          {
            path: 'asset-decisions',
            element: routeElement(assetDecisionsPage, '正在加载资产决策'),
          },
          { path: 'monitoring', element: routeElement(monitoringPage, '正在加载监控') },
          {
            path: 'monitoring/compare',
            element: routeElement(monitoringComparePage, '正在加载监控实例对比'),
          },
          {
            path: 'monitoring/:monitoringInstanceId',
            element: routeElement(monitoringDetailPage, '正在加载监控实例详情'),
          },
          { path: 'targets', element: routeElement(targetsPage, '正在加载入口探测') },
          {
            path: 'targets/:targetId',
            element: routeElement(targetDetailPage, '正在加载目标详情'),
          },
          { path: 'events', element: routeElement(eventsPage, '正在加载事件时间线') },
          { path: 'command-audit', element: routeElement(commandAuditPage, '加载命令审计') },
          { path: 'settings', element: routeElement(settingsPage, '正在加载设置') },
          { path: '*', element: <Navigate to="/" replace /> },
        ],
      },
    ],
  },
]

export const router = createBrowserRouter(appRoutes)
