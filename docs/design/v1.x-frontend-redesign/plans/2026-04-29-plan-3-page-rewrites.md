# Plan 3 · 8 页重写 + 视觉证据 + 收尾 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Depends on:** Plan 1 (backend auth) and Plan 2 (frontend foundation) must be merged. This plan rewrites the 8 page components on top of the foundation atoms.

**Goal:** Replace every existing page component with a redesigned layout that strictly matches the spec §10. Update the dashboard backend payload to include sidebar-required fields. Re-capture screenshot evidence across 4 themes. Cross-link the new design back into the V1 baseline docs.

**Architecture:** Each page becomes a thin component composing atoms shipped in Plan 2. Page-specific styles live next to the component (`PageName.css`). API contracts stay unchanged except for `/api/dashboard`, which is extended to expose sidebar metadata (sync, version, anomaly counts) so the sidebar no longer needs defaults. Existing API hooks in `web/src/lib/api.ts` are reused; types in `web/src/lib/types.ts` get a small extension. No new pages are introduced beyond LoginPage (added in Plan 2).

**Tech Stack:** React 19, TypeScript, Vite, React Router 7, Vitest. Pure CSS variables (tokens from Plan 2). No new runtime deps.

**Out of scope:**
- New backend endpoints beyond extending `/api/dashboard`
- Live Telegram delivery validation (separate operations task)
- Mobile / phone layout (responsive only down to ~1024px)
- New ECharts / chart library — keep the div-based metric bars from the dashboard mockups

---

## File Structure

### Modified
```
web/src/pages/DashboardPage.tsx          + DashboardPage.test.tsx + DashboardPage.css
web/src/pages/NodesPage.tsx              + NodesPage.test.tsx + NodesPage.css
web/src/pages/NodeDetailPage.tsx         + NodeDetailPage.test.tsx + NodeDetailPage.css
web/src/pages/NodeOnboardingPage.tsx     + NodeOnboardingPage.test.tsx + NodeOnboardingPage.css
web/src/pages/TargetsPage.tsx            + TargetsPage.test.tsx + TargetsPage.css
web/src/pages/TargetDetailPage.tsx       + TargetDetailPage.test.tsx + TargetDetailPage.css
web/src/pages/EventsPage.tsx             + EventsPage.test.tsx + EventsPage.css
web/src/pages/SettingsPage.tsx           + SettingsPage.test.tsx + SettingsPage.css
web/src/lib/api.ts                       (extend types if needed)
web/src/lib/types.ts                     (DashboardSummary new fields)
internal/center/http/handlers/dashboard.go  (extend payload)
internal/center/store/dashboard.go          (extend query)
docs/operations/visual-evidence/manifest.json
docs/operations/visual-evidence/*.png       (regenerated)
docs/release/v1-gap-checklist.md
docs/design/v1-baseline/README.md           (status banner)
README.md                                   (Visual authority pointer)
```

### New (per-page styles)
- 8 `*.css` files alongside the page components (1 per page; LoginPage.css already exists from Plan 2)

---

### Task 1: Extend `/api/dashboard` payload (sync + counts for sidebar)

**Files:**
- Modify: `internal/center/store/dashboard.go`
- Modify: `internal/center/http/handlers/dashboard.go`
- Modify: corresponding `*_test.go` files
- Modify: `web/src/lib/types.ts`, `web/src/lib/api.ts`

The new payload fields the sidebar needs:

```json
{
  "fleet_summary": { ... existing ... },
  "sidebar": {
    "sync_state": "ok" | "degraded" | "down",
    "version": "v1.0.x",
    "last_sync_at": "2026-04-29T14:32:01Z",
    "anomaly_node_count": 3,
    "anomaly_target_count": 1
  }
}
```

- [ ] **Step 1: Failing test for the store query**

In `internal/center/store/dashboard_test.go`, add an assertion that the dashboard view now also returns `anomaly_node_count`, `anomaly_target_count`, `last_sync_at`, `sync_state`. Implement minimal SQL in the existing query, fed from `nodes` and `targets`:

```sql
select
  ...,
  (select count(*) from nodes  where current_health_status in ('关注','告警','严重')) as anomaly_node_count,
  (select count(*) from targets where current_health_status in ('关注','告警','严重')) as anomaly_target_count,
  (select max(last_heartbeat_at) from nodes) as last_sync_at,
  case
    when (select max(last_heartbeat_at) from nodes) > now() - interval '5 minutes' then 'ok'
    when (select max(last_heartbeat_at) from nodes) > now() - interval '15 minutes' then 'degraded'
    else 'down'
  end as sync_state
```

Run: `go test ./internal/center/store -run TestDashboard -v`. Expected: FAIL → implement → PASS.

- [ ] **Step 2: Failing test for the handler**

In `internal/center/http/handlers/dashboard_test.go`, assert the response JSON contains the new `sidebar` object. Update the handler to compose it from the store output. PASS.

- [ ] **Step 3: Update web types and hook**

`web/src/lib/types.ts`:

```ts
export interface DashboardSidebar {
  sync_state: 'ok' | 'degraded' | 'down'
  version: string
  last_sync_at: string
  anomaly_node_count: number
  anomaly_target_count: number
}

export interface DashboardSummary {
  // ...existing fields preserved...
  sidebar: DashboardSidebar
}
```

Update `useDashboard()` in `lib/api.ts` to return the sidebar block. Adjust `AppShell.tsx` (Plan 2) to read from `dashboard.sidebar.*` instead of `dashboard?.*` defaults.

- [ ] **Step 4: Run full verify**

Run: `make verify-go && cd web && npm run build && npx vitest run`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add internal/center/store/dashboard.go internal/center/store/dashboard_test.go \
        internal/center/http/handlers/dashboard.go internal/center/http/handlers/dashboard_test.go \
        web/src/lib/types.ts web/src/lib/api.ts web/src/app/layout/AppShell.tsx
git commit -m "Extend /api/dashboard with sidebar metadata block"
```

---

### Task 2: Dashboard page — types + hooks

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`

The Dashboard page needs richer summaries. Add types:

```ts
export interface DashboardKpi {
  anomaly_nodes_count: number
  total_nodes_count: number
  anomaly_targets_count: number
  total_targets_count: number
  critical_count: number
  maintenance_count: number
  delta_24h: { new_incidents: number; recovered: number }
}

export interface DashboardNodeSummary {
  node_id: string
  display_name: string
  region: string
  provider: string
  health_status: 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance' | 'offline'
  primary_issue_summary: string
  started_at: string
  hardware_brief: string  // e.g. "1C2G"
}

export interface DashboardTargetSummary {
  target_id: string
  name: string
  type: 'service' | 'china_reference'
  scheme_brief: string  // "https · :443"
  labels: string[]
  health_status: DashboardNodeSummary['health_status']
  primary_issue_summary: string
  anomaly_probe_count: number
  total_probe_count: number
  started_at: string
}

export interface DashboardEventRow {
  event_id: string
  occurred_at: string
  object_type: 'node' | 'target'
  object_name: string
  event_type: string
  severity_from: 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance' | 'offline'
  severity_to:   DashboardEventRow['severity_from']
  notification_status: 'sent' | 'silenced'
}

export interface DashboardSummary {
  kpi: DashboardKpi
  anomaly_nodes: DashboardNodeSummary[]
  anomaly_targets: DashboardTargetSummary[]
  recent_events: DashboardEventRow[]
  sidebar: DashboardSidebar
}
```

If the existing endpoint doesn't supply some of these, fall back to deriving on the client from `/api/nodes`, `/api/targets`, `/api/events?limit=8`.

- [ ] **Step 1: Update types and hook**

Replace `useDashboard` to return `DashboardSummary` shape. If backend doesn't yet provide every field, compose from multiple parallel `fetcher` calls inside the hook.

- [ ] **Step 2: Run lint + tsc**

Run: `cd web && npx tsc --noEmit`
Expected: green.

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/types.ts web/src/lib/api.ts
git commit -m "Define dashboard view-model and hook"
```

---

### Task 3: Dashboard page rewrite

**Files:**
- Modify: `web/src/pages/DashboardPage.tsx`
- Replace: `web/src/pages/DashboardPage.test.tsx`
- Create: `web/src/pages/DashboardPage.css`

Implements spec §10.3.

- [ ] **Step 1: Failing test**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { DashboardPage } from './DashboardPage'
import * as api from '../lib/api'

const fixture = {
  kpi: {
    anomaly_nodes_count: 3, total_nodes_count: 22,
    anomaly_targets_count: 1, total_targets_count: 17,
    critical_count: 1, maintenance_count: 2,
    delta_24h: { new_incidents: 1, recovered: 0 },
  },
  anomaly_nodes: [
    { node_id: 'n1', display_name: 'api-gateway',  region: '东京', provider: 'Linode',
      health_status: 'critical', primary_issue_summary: '心跳丢失 4 分钟',
      started_at: '2026-04-29T14:28:14Z', hardware_brief: '2C2G' },
    { node_id: 'n2', display_name: 'cache-redis-02', region: '广州', provider: '腾讯云',
      health_status: 'alert', primary_issue_summary: '内存 92% 持续 28 分钟',
      started_at: '2026-04-29T14:04:12Z', hardware_brief: '4C8G' },
  ],
  anomaly_targets: [],
  recent_events: [
    { event_id: 'e1', occurred_at: '2026-04-29T14:31:42Z', object_type: 'node', object_name: 'api-gateway',
      event_type: 'heartbeat_missing', severity_from: 'alert', severity_to: 'critical',
      notification_status: 'sent' },
  ],
  sidebar: { sync_state: 'ok', version: 'v1.0', last_sync_at: '2026-04-29T14:32:01Z', anomaly_node_count: 3, anomaly_target_count: 0 },
}

describe('DashboardPage', () => {
  beforeEach(() => {
    vi.spyOn(api, 'useDashboard').mockReturnValue({ data: fixture, isLoading: false } as any)
  })

  it('renders the H1 and 4 KPI cards', () => {
    render(<MemoryRouter><DashboardPage /></MemoryRouter>)
    expect(screen.getByRole('heading', { level: 1, name: /首页/ })).toBeInTheDocument()
    expect(screen.getByText('异常节点 / 总数')).toBeInTheDocument()
    expect(screen.getByText('严重态 · 节点+目标')).toBeInTheDocument()
    expect(screen.getByText('维护中')).toBeInTheDocument()
  })

  it('lists anomaly nodes with state-coloured cards', () => {
    render(<MemoryRouter><DashboardPage /></MemoryRouter>)
    expect(screen.getByText('api-gateway')).toBeInTheDocument()
    expect(screen.getByText('心跳丢失 4 分钟')).toBeInTheDocument()
    expect(screen.getAllByText(/严重|告警/)).not.toHaveLength(0)
  })

  it('shows empty-state for anomaly targets when none', () => {
    render(<MemoryRouter><DashboardPage /></MemoryRouter>)
    expect(screen.getByText(/其余 \d+ 目标均正常/)).toBeInTheDocument()
  })

  it('renders the recent events table', () => {
    render(<MemoryRouter><DashboardPage /></MemoryRouter>)
    expect(screen.getByText('heartbeat_missing')).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Failing run**

Run: `cd web && npx vitest run src/pages/DashboardPage.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implementation**

Skeleton (full structure should mirror spec §10.3 / first dashboard mockup):

```tsx
import { Link } from 'react-router-dom'
import { Card, Badge } from '../components/atoms'
import { useDashboard } from '../lib/api'
import './DashboardPage.css'

const HEALTH_TONE = {
  normal: 'normal', notice: 'notice', alert: 'alert',
  critical: 'critical', maintenance: 'maintenance', offline: 'offline',
} as const

const HEALTH_LABEL = {
  normal: '正常', notice: '关注', alert: '告警',
  critical: '严重', maintenance: '维护中', offline: '离线',
} as const

export function DashboardPage() {
  const { data, isLoading } = useDashboard()
  if (isLoading || !data) return <div className="dashboard-page__loading">加载中…</div>

  const { kpi, anomaly_nodes, anomaly_targets, recent_events } = data

  return (
    <div className="dashboard-page">
      <header className="dashboard-page__header">
        <div>
          <h1>首页 / Dashboard</h1>
          <p className="dashboard-page__subtitle">当前问题分布 · 最近 24 小时变化</p>
        </div>
      </header>

      <section className="dashboard-page__kpi">
        <Card>
          <div className="kpi-eyebrow">异常节点 / 总数</div>
          <div className="kpi-number"><span className="tnum">{kpi.anomaly_nodes_count}</span><span className="kpi-divisor"> / {kpi.total_nodes_count}</span></div>
          <div className="kpi-delta">+{kpi.delta_24h.new_incidents} 24h 内</div>
        </Card>
        <Card role="accent">
          <div className="kpi-eyebrow">异常目标 / 总数</div>
          <div className="kpi-number"><span className="tnum">{kpi.anomaly_targets_count}</span><span className="kpi-divisor"> / {kpi.total_targets_count}</span></div>
        </Card>
        <Card role="warning">
          <div className="kpi-eyebrow">严重态 · 节点+目标</div>
          <div className="kpi-number tnum">{kpi.critical_count}</div>
          <div className="kpi-delta">需立即处理</div>
        </Card>
        <Card role="state" tone="maintenance">
          <div className="kpi-eyebrow">维护中</div>
          <div className="kpi-number tnum">{kpi.maintenance_count}</div>
          <div className="kpi-delta">不参与异常评估</div>
        </Card>
      </section>

      <section className="dashboard-page__grid">
        <section>
          <header className="dashboard-page__section-header"><h2>当前异常节点</h2><Link to="/nodes">全部节点 →</Link></header>
          <ul className="dashboard-page__list">
            {anomaly_nodes.map(n => (
              <li key={n.node_id}>
                <Card role="state" tone={HEALTH_TONE[n.health_status]}>
                  <div className="anomaly-row">
                    <div className="anomaly-row__head">
                      <span className={`anomaly-row__dot tone--${n.health_status}`} aria-hidden />
                      <Link to={`/nodes/${n.node_id}`} className="anomaly-row__name">{n.display_name}</Link>
                      <Badge variant="state" tone={HEALTH_TONE[n.health_status]}>{HEALTH_LABEL[n.health_status]}</Badge>
                    </div>
                    <div className="anomaly-row__meta">{n.region} · {n.provider} · {n.hardware_brief} · <span className={`tone--${n.health_status}`}>{n.primary_issue_summary}</span></div>
                  </div>
                </Card>
              </li>
            ))}
          </ul>
        </section>

        <section>
          <header className="dashboard-page__section-header"><h2>当前异常目标</h2><Link to="/targets">全部目标 →</Link></header>
          {anomaly_targets.length > 0 ? (
            <ul className="dashboard-page__list">
              {anomaly_targets.map(t => (
                <li key={t.target_id}>
                  <Card role="state" tone={HEALTH_TONE[t.health_status]}>
                    <div className="anomaly-row">
                      <div className="anomaly-row__head">
                        <span className={`anomaly-row__dot tone--${t.health_status}`} aria-hidden />
                        <Link to={`/targets/${t.target_id}`} className="anomaly-row__name">{t.name}</Link>
                        <Badge variant="state" tone={HEALTH_TONE[t.health_status]}>{HEALTH_LABEL[t.health_status]}</Badge>
                      </div>
                      <div className="anomaly-row__meta">{t.scheme_brief} · {t.anomaly_probe_count}/{t.total_probe_count} ProbeItem 异常</div>
                    </div>
                  </Card>
                </li>
              ))}
            </ul>
          ) : (
            <Card role="state" tone="normal">
              <div className="dashboard-page__empty">其余 {data.kpi.total_targets_count} 目标均正常</div>
            </Card>
          )}
        </section>
      </section>

      <section>
        <header className="dashboard-page__section-header"><h2>最近异常事件</h2><Link to="/events">完整事件页 →</Link></header>
        <table className="dashboard-events">
          <thead>
            <tr>
              <th>时间</th><th>对象</th><th>事件</th><th>变化</th><th>通知</th>
            </tr>
          </thead>
          <tbody>
            {recent_events.map(e => (
              <tr key={e.event_id}>
                <td className="tnum mono">{e.occurred_at.slice(11,19)}</td>
                <td>{e.object_name} <span className="event-meta">/ {e.object_type === 'node' ? 'Node' : 'Target'}</span></td>
                <td className="mono">{e.event_type}</td>
                <td>
                  <span className={`tone--${e.severity_from}`}>{HEALTH_LABEL[e.severity_from]}</span>
                  <span className="event-meta"> → </span>
                  <span className={`tone--${e.severity_to}`}>{HEALTH_LABEL[e.severity_to]}</span>
                </td>
                <td>
                  {e.notification_status === 'sent'
                    ? <span className="tone--normal">✓ 已发送</span>
                    : <span className="event-meta">— 静默</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  )
}
```

CSS: write `DashboardPage.css` with classes used above. Reference spec mockup `page-dashboard.html` for exact spacing.

- [ ] **Step 4: Pass**

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/DashboardPage.tsx web/src/pages/DashboardPage.test.tsx web/src/pages/DashboardPage.css
git commit -m "Rewrite Dashboard page on the new foundation"
```

---

### Task 4: Nodes list page rewrite

**Files:**
- Modify: `web/src/pages/NodesPage.tsx`
- Replace: `web/src/pages/NodesPage.test.tsx`
- Create: `web/src/pages/NodesPage.css`

Implements spec §10.4.

- [ ] **Step 1: Failing test**

Cover: page title with summary副字 (22 节点 · 3 异常 · 1 严重 · 2 维护中); search input present; "+ 新建节点" button; 6 filter dropdowns; rows render with status dot + name + health badge + relative time; offline rows have lower opacity; pagination renders.

- [ ] **Step 2: Implement**

Compose with `<Card>`, `<Badge>`, `<Input>`, `<Button>` from atoms. Use `useNodes()` from `lib/api.ts`. Filter state lives in URL query params (`useSearchParams`). Table is plain `<table>` styled by `NodesPage.css`.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/NodesPage.tsx web/src/pages/NodesPage.test.tsx web/src/pages/NodesPage.css
git commit -m "Rewrite Nodes list page"
```

---

### Task 5: Node detail page rewrite

**Files:**
- Modify: `web/src/pages/NodeDetailPage.tsx`
- Replace: `web/src/pages/NodeDetailPage.test.tsx`
- Create: `web/src/pages/NodeDetailPage.css`

Implements spec §10.5.

- [ ] **Step 1: Failing test**

Cover: breadcrumb 首页 / 节点 / `<节点名>`; identity card with status dot + 4 micro states + label chips + 主操作 (进入维护) + 次操作 (暂停监控) + ⋯ menu; 4-column metadata strip; 5 tabs with counter on 活跃异常; concept tab content with active incident card + 4 metric cards (with mini bar chart from div elements) + recent events mini table; right column with assigned probes + basic info + 危险区 (朱砂虚线 with 重置绑定 / 标记退役).

- [ ] **Step 2: Implement**

This is the most complex page. Compose with all atoms. The "mini chart" inside metric cards is a row of `<div>` elements with varying heights (no chart lib).

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/NodeDetailPage.tsx web/src/pages/NodeDetailPage.test.tsx web/src/pages/NodeDetailPage.css
git commit -m "Rewrite Node detail page"
```

---

### Task 6: Node onboarding page rewrite

**Files:**
- Modify: `web/src/pages/NodeOnboardingPage.tsx`
- Replace: `web/src/pages/NodeOnboardingPage.test.tsx`
- Create: `web/src/pages/NodeOnboardingPage.css`

Implements spec §10.6.

- [ ] **Step 1: Failing test**

Cover: breadcrumb ending in 接入; node name + 「等待绑定」鎏金 pill; 4-step indicator (已创建 ✓ → 等待绑定 current with glow → 等待稳定观测 dashed → 接入完成 dashed); token row (鎏金 accent card + 复制 button + 朱砂 warning); install command code block (深底, 注释 muted, token highlighted); auto-refresh attempt log table.

- [ ] **Step 2: Implement**

Use existing onboarding API hooks; the rest is composing atoms + tokens. The 4-step indicator is custom CSS (no atom).

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/NodeOnboardingPage.tsx web/src/pages/NodeOnboardingPage.test.tsx web/src/pages/NodeOnboardingPage.css
git commit -m "Rewrite Node onboarding page"
```

---

### Task 7: Targets list page rewrite

**Files:**
- Modify: `web/src/pages/TargetsPage.tsx`
- Replace: `web/src/pages/TargetsPage.test.tsx`
- Create: `web/src/pages/TargetsPage.css`

Implements spec §10.7. Mirrors NodesPage structure with target-specific columns.

- [ ] **Step 1: Failing test**

Cover: page title; search; "+ 新建目标"; filter row with 类型 / 运行状态 / 健康 / 标签 / 执行节点标签 / 仅看异常; table columns 复选框 / 名称 (带状态点 + 异常副字) / 类型 / host:port / 标签 / 运行 / 健康 / ProbeItem 数; offline rows lowered opacity.

- [ ] **Step 2: Implement** & **Step 3: Commit**

```bash
git commit -m "Rewrite Targets list page"
```

---

### Task 8: Target detail page rewrite

**Files:**
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Replace: `web/src/pages/TargetDetailPage.test.tsx`
- Create: `web/src/pages/TargetDetailPage.css`

Implements spec §10.8.

- [ ] **Step 1: Failing test**

Cover: breadcrumb 首页 / 目标 / `<名称>`; identity card (state-toned, host badge, labels, ProbeItem counter); ProbeItem inline list — each row with toggle (Toggle atom), kind chip, config text, frequency tier chip, status badge, ⋯ menu, sub-line stats (最近 100 次成功率 + latency); + 新增 ProbeItem button; 执行节点视角 list (each node row with 承担数 + 成功率, offline node "—不计入").

- [ ] **Step 2: Implement** & **Step 3: Commit**

```bash
git commit -m "Rewrite Target detail page"
```

---

### Task 9: Events page rewrite

**Files:**
- Modify: `web/src/pages/EventsPage.tsx`
- Replace: `web/src/pages/EventsPage.test.tsx`
- Create: `web/src/pages/EventsPage.css`

Implements spec §10.9.

- [ ] **Step 1: Failing test**

Cover: H1 「事件 / Events」 + 副字 (最近 24 小时 · N 条事件 · M 已通知); export button; 4-column filter card (时间 segmented / 对象类型 segmented / 严重度 multi-pill / 事件类型 dropdown); 4 boolean toggle row (仅看通知/恢复/维护/含 backfill); 「已应用 N 个筛选」+ 清除; 时间分组 (今天/昨天/本周); event row with timestamp / object+变化 / Telegram status; maintenance rows lowered opacity; recovery rows on faint emerald; "加载更早事件" pagination.

- [ ] **Step 2: Implement** & **Step 3: Commit**

```bash
git commit -m "Rewrite Events page"
```

---

### Task 10: Settings page rewrite (full)

**Files:**
- Modify: `web/src/pages/SettingsPage.tsx`
- Replace: `web/src/pages/SettingsPage.test.tsx`
- Create: `web/src/pages/SettingsPage.css`

Implements spec §10.10. The Theme tab was added in Plan 2 Task 25; this task rewrites the other 4 tabs (Telegram / 频率档位 / 默认规则 / 数据保留) on the new atom set, applying the form layout (left 280 px description + right form fields), the danger zone style, and the dirty-bar.

- [ ] **Step 1: Failing test**

Cover: H1; 5 Pill tabs; Telegram tab content (启用 toggle, Bot Token + Chat ID inputs, 测试投递 result card with 翡翠 surface, 危险区 with 清空 button, dirty-bar showing diff count, 放弃/保存 buttons); 频率档位 tab (4-tier list); 默认规则 tab (rule overrides for node labels / target types / target labels); 数据保留 tab (4 retention day inputs).

- [ ] **Step 2: Implement** & **Step 3: Commit**

```bash
git commit -m "Rewrite Settings page"
```

---

### Task 11: Verify all pages render against live data

**Files:** (none — verification only)

- [ ] **Step 1: Build**

Run: `cd web && npm run build && npm run lint && npx vitest run`
Expected: all green.

- [ ] **Step 2: Manual smoke**

Boot the center against a populated test DB. Walk every page in 4 themes, verify:
- No console errors
- No layout overflow at 1280×800
- No "单用户/全权限/个人系统" strings (`grep -r '单用户\|全权限\|个人系统' web/src` returns 0)
- Sidebar anomaly counts match dashboard KPI counts
- Theme switch survives reload (FOUC verified by hard-refresh in DevTools)

- [ ] **Step 3: Commit (if any fix)**

```bash
git commit -m "Tighten cross-page consistency"
```

---

### Task 12: Regenerate visual evidence (4 themes × representative pages)

**Files:**
- Update: `docs/operations/visual-evidence/manifest.json`
- Replace: `docs/operations/visual-evidence/*.png` (16 files)
- Create: `docs/operations/visual-evidence/v1.x/` directory holding the new captures (preserve old V1 baseline screenshots in a `legacy/` sibling)

Reuse the existing screenshot tooling. We capture **4 themes × 4 representative pages = 16 screenshots**:
- Pages: dashboard, node-detail, events, settings
- Themes: houfeng-dark, houfeng-light, classic-dark, classic-light

- [ ] **Step 1: Move legacy captures aside**

```bash
mkdir -p docs/operations/visual-evidence/legacy
git mv docs/operations/visual-evidence/dashboard.png      docs/operations/visual-evidence/legacy/
git mv docs/operations/visual-evidence/events.png         docs/operations/visual-evidence/legacy/
git mv docs/operations/visual-evidence/manifest.json      docs/operations/visual-evidence/legacy/
git mv docs/operations/visual-evidence/node-detail.png    docs/operations/visual-evidence/legacy/
git mv docs/operations/visual-evidence/node-onboarding.png docs/operations/visual-evidence/legacy/
git mv docs/operations/visual-evidence/nodes.png          docs/operations/visual-evidence/legacy/
git mv docs/operations/visual-evidence/settings.png       docs/operations/visual-evidence/legacy/
git mv docs/operations/visual-evidence/target-detail.png  docs/operations/visual-evidence/legacy/
```

- [ ] **Step 2: Capture new shots**

Use the same approach the previous capture used (likely Playwright or Chromium DevTools). For each theme:
1. Set localStorage `houfeng.theme.preset` and `houfeng.theme.mode`.
2. Visit each of the 4 pages, viewport 1440×1024.
3. Save as `docs/operations/visual-evidence/v1.x/<theme>/<page>.png`.

Theme keys produce 4 directories:
```
v1.x/houfeng-dark/
v1.x/houfeng-light/
v1.x/classic-dark/
v1.x/classic-light/
```

- [ ] **Step 3: Write the new manifest**

`docs/operations/visual-evidence/manifest.json`:

```json
{
  "captured_at": "<ISO timestamp>",
  "viewport": { "width": 1440, "height": 1024, "device_scale_factor": 1 },
  "data_source": { "...same as before, updated where applicable..." },
  "themes": ["houfeng-dark", "houfeng-light", "classic-dark", "classic-light"],
  "pages": [
    { "route": "/",                        "file": "dashboard.png" },
    { "route": "/nodes/<seed_node_id>",    "file": "node-detail.png" },
    { "route": "/events",                  "file": "events.png" },
    { "route": "/settings",                "file": "settings.png" }
  ],
  "notes": [
    "Captures grouped under v1.x/<theme>/<page>.png",
    "Legacy V1 captures preserved under legacy/"
  ]
}
```

- [ ] **Step 4: Commit**

```bash
git add docs/operations/visual-evidence/
git commit -m "Regenerate visual evidence for 4 themes × 4 representative pages"
```

---

### Task 13: Update gap-checklist (mark V1 visual replaced by V1.x)

**Files:**
- Modify: `docs/release/v1-gap-checklist.md`

- [ ] **Step 1: Update the "UI and interaction surfaces" section**

Mark all visual rows as **"Superseded by V1.x"** with a pointer to `docs/design/v1.x-frontend-redesign/`. Add a new section "V1.x visual baseline":

```md
## V1.x visual baseline (replaces frozen V1 visual portion)

| Area | Status | Evidence |
| --- | --- | --- |
| Dual-preset × dark/light theme system | Closed | `web/src/styles/tokens.css`, `web/src/lib/theme-context.tsx` |
| 5 component atoms with tests | Closed | `web/src/components/atoms/*` |
| Login flow with backend auth (方案 2) | Closed | `web/src/pages/LoginPage.tsx`, Plan 1 backend |
| 8 page rewrites + login page | Closed | `web/src/pages/*` |
| Visual evidence (4 themes × 4 pages) | Closed | `docs/operations/visual-evidence/v1.x/manifest.json` |
| WCAG AA contrast verified per theme | Partial | Manual check pending; automate in follow-up |
| Strict visual-fidelity acceptance against V1 baseline | Withdrawn | V1 visual baseline officially unfrozen on 2026-04-29 |
```

- [ ] **Step 2: Commit**

```bash
git add docs/release/v1-gap-checklist.md
git commit -m "Record V1.x visual baseline in gap checklist"
```

---

### Task 14: Cross-link v1-baseline README + project README

**Files:**
- Modify: `docs/design/v1-baseline/README.md`
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: v1-baseline README banner**

At the very top of `docs/design/v1-baseline/README.md` (under the YAML frontmatter), insert:

```md
> **VISUAL PORTION UNFROZEN 2026-04-29.** The V1.x frontend redesign supersedes the visual sections of this document (Stitch screens, `ui-ux-spec.md`, `visual-review-round2.md`, `baseline-screens.md`). The structural sections (`architecture-data-model.md`, `rules-and-interaction.md`, `tech-selection.md`) remain frozen and authoritative. Current visual baseline lives at [`docs/design/v1.x-frontend-redesign/README.md`](../v1.x-frontend-redesign/README.md).
```

- [ ] **Step 2: Project README update**

In `README.md`, replace the "Visual authority stays: Unified / Baseline Stitch screens only" line with:

```md
- Visual authority stays: V1.x frontend redesign at `docs/design/v1.x-frontend-redesign/`
- Earlier Stitch / Unified / Baseline screens are historical (filed under `docs/design/v1-baseline/`) and no longer the development reference
```

- [ ] **Step 3: CLAUDE.md update**

In the "Project identity" paragraph and any other place mentioning Stitch/Baseline, update to point to v1.x. Add a note: "The V1 visual baseline (`docs/design/v1-baseline/ui-ux-spec.md`) was unfrozen 2026-04-29; current visual authority is `docs/design/v1.x-frontend-redesign/`."

- [ ] **Step 4: Commit**

```bash
git add docs/design/v1-baseline/README.md README.md CLAUDE.md
git commit -m "Cross-link V1.x as the active visual baseline"
```

---

### Task 15: Final verification + tag

**Files:** (none — verification only)

- [ ] **Step 1: Full repo verify**

```bash
make verify          # = verify-go + verify-web
```
Expected: all green.

- [ ] **Step 2: Manual cross-theme smoke**

Open the SPA in each of the 4 themes and confirm:
- Login → dashboard render
- Sidebar counters reflect data
- Each protected page accessible & free of layout glitches
- Telegram test (in settings) returns success when configured
- Logout returns to /login

- [ ] **Step 3: PR / final commit**

If all clean, the plan is complete. Otherwise commit per-fix and re-verify.

```bash
# nothing to commit; the plan ends here
git log --oneline -25
```

---

## Acceptance criteria

- All 8 page components rewritten + login page (Plan 2) + theme system fully wired
- `make verify` green
- `cd web && npm run build && npm run lint && npx vitest run` green
- 4 theme combinations switch live and survive reload (no FOUC, no flash)
- Login → dashboard flow works against Plan 1 backend
- 401 from any /api/* route triggers redirect to /login
- New visual evidence committed under `docs/operations/visual-evidence/v1.x/<theme>/<page>.png` (16 files)
- `grep -r '单用户\|全权限\|个人系统' web/src` returns 0 matches
- v1-baseline README has the unfrozen banner; project README + CLAUDE.md point to v1.x
- gap-checklist marks visual portion superseded by V1.x

## Cross-plan handoff

There is no Plan 4 — V1.x scope is closed once Plan 3 lands. Follow-up backlog (each separate work item, not in scope here):

- Automated WCAG AA contrast tests across 4 themes
- Playwright E2E for the protected flows (login → dashboard → node detail → events filter)
- Telegram delivery proof captured in operations docs
- Optional: more pages added to visual-evidence (currently 4 representative; full set is 8 pages × 4 themes = 32)
- Optional: i18n English copy track if multi-user / multi-language scope is later opened
