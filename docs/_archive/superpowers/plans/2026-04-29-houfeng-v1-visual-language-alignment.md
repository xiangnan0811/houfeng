# Houfeng V1 Visual Language Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the implementation-controlled V1 visual-language gaps by strengthening the shared shell, dashboard risk hierarchy, Chinese-first copy, and release evidence docs.

**Architecture:** Keep the existing React/Vite SPA and API contracts. Modify only presentational React/CSS/tests plus the release/visual verification docs. No backend schema/API changes and no new dependencies.

**Tech Stack:** React, TypeScript, Vite, Vitest, Testing Library, CSS, Markdown

---

## Scope Guard

Do not:

- change backend behavior or API payloads;
- add new dependencies or browser screenshot tooling;
- redesign V1 navigation or create new product areas;
- claim screenshot comparison, live PostgreSQL smoke, or Telegram delivery evidence was completed.

## Planned File Structure

- Modify: `web/src/app/layout/AppShell.tsx`
- Modify: `web/src/app/layout/AppShell.test.tsx`
- Modify: `web/src/pages/DashboardPage.tsx`
- Modify: `web/src/pages/DashboardPage.test.tsx`
- Modify: `web/src/pages/NodeOnboardingPage.tsx`
- Modify: `web/src/pages/NodeOnboardingPage.test.tsx`
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`
- Modify: `web/src/index.css`
- Modify: `docs/operations/v1-visual-verification.md`
- Modify: `docs/release/v1-gap-checklist.md`

---

### Task 1: Strengthen the shared app shell baseline

**Files:**
- Modify: `web/src/app/layout/AppShell.tsx`
- Modify: `web/src/app/layout/AppShell.test.tsx`
- Modify: `web/src/index.css`

- [ ] **Step 1: Add test coverage for shell operational context**

Update `web/src/app/layout/AppShell.test.tsx` to assert these user-visible shell labels:

```tsx
expect(screen.getByText('单体中心')).toBeInTheDocument()
expect(screen.getByText('PostgreSQL')).toBeInTheDocument()
expect(screen.getByText('systemd agent')).toBeInTheDocument()
expect(screen.getByText('当前视图')).toBeInTheDocument()
expect(screen.getByText('V1 冻结基线')).toBeInTheDocument()
```

Run:

```bash
cd web && npm test -- --run AppShell
```

Expected: fails before implementation because the new shell labels are missing.

- [ ] **Step 2: Implement the shell context blocks**

In `web/src/app/layout/AppShell.tsx`, add a sidebar context block below nav:

```tsx
<div className="app-shell__sidebar-context" aria-label="系统形态">
  <p>运行形态</p>
  <span>单体中心</span>
  <span>PostgreSQL</span>
  <span>systemd agent</span>
</div>
```

Update the header to include:

```tsx
<p className="app-shell__section-label">当前视图</p>
...
<div className="app-shell__status-strip" aria-label="V1 基线状态">
  <span>V1 冻结基线</span>
  <span>中文主界面</span>
  <span>Unified / Baseline</span>
</div>
```

- [ ] **Step 3: Add shell CSS refinements**

In `web/src/index.css`, add styles for:

```css
.app-shell__sidebar-context { ... }
.app-shell__sidebar-context p { ... }
.app-shell__sidebar-context span { ... }
.app-shell__header { justify-content: space-between; gap: 24px; }
.app-shell__section-label { ... }
.app-shell__status-strip { ... }
.app-shell__status-strip span { ... }
```

Keep the dark-first palette and existing class naming.

- [ ] **Step 4: Verify and commit**

Run:

```bash
cd web && npm test -- --run AppShell
cd web && npm run build
```

Commit:

```bash
git add web/src/app/layout/AppShell.tsx web/src/app/layout/AppShell.test.tsx web/src/index.css
git commit -m "Align the app shell with the V1 baseline frame" -m "The frozen V1 visual baseline requires a unified shell with clear top-header hierarchy and stable system context. This strengthens the existing shell without changing routes or product scope.

Constraint: Unified / Baseline Stitch screens remain authoritative; this is an implementation alignment pass.
Confidence: high
Scope-risk: narrow
Tested: cd web && npm test -- --run AppShell; cd web && npm run build.
Not-tested: Screenshot comparison remains tracked separately."
```

---

### Task 2: Reweight dashboard toward current risk hierarchy

**Files:**
- Modify: `web/src/pages/DashboardPage.tsx`
- Modify: `web/src/pages/DashboardPage.test.tsx`

- [ ] **Step 1: Add dashboard hierarchy test assertions**

In `web/src/pages/DashboardPage.test.tsx`, add assertions after overview load:

```tsx
expect(screen.getByText('当前风险总览')).toBeInTheDocument()
expect(screen.getByText('先处理当前异常，再查看趋势与事件历史。')).toBeInTheDocument()
expect(screen.getByText('风险对象')).toBeInTheDocument()
```

Run:

```bash
cd web && npm test -- --run DashboardPage
```

Expected: fails before implementation because the risk hierarchy labels are missing.

- [ ] **Step 2: Update dashboard hero copy and top summary**

In `web/src/pages/DashboardPage.tsx`:

- change the page panel eyebrow from `Dashboard` to `当前风险总览`;
- change description to `先处理当前异常，再查看趋势与事件历史。`;
- add a top summary card label `风险对象` with `abnormalTotal`;
- keep existing abnormal node/target/event sections and data.

- [ ] **Step 3: Verify and commit**

Run:

```bash
cd web && npm test -- --run DashboardPage
cd web && npm run build
```

Commit:

```bash
git add web/src/pages/DashboardPage.tsx web/src/pages/DashboardPage.test.tsx
git commit -m "Prioritize current risk on the dashboard" -m "The Unified dashboard baseline emphasizes current risk before secondary history. This adjusts the dashboard copy and summary hierarchy while preserving the existing data contract.

Constraint: No backend dashboard contract changes.
Confidence: high
Scope-risk: narrow
Tested: cd web && npm test -- --run DashboardPage; cd web && npm run build.
Not-tested: Screenshot comparison remains tracked separately."
```

---

### Task 3: Remove ordinary English UI copy from onboarding and ProbeItem forms

**Files:**
- Modify: `web/src/pages/NodeOnboardingPage.tsx`
- Modify: `web/src/pages/NodeOnboardingPage.test.tsx`
- Modify: `web/src/pages/TargetDetailPage.tsx`
- Modify: `web/src/pages/TargetDetailPage.test.tsx`

- [ ] **Step 1: Add/adjust Chinese-first tests**

Update tests to assert:

```tsx
expect(screen.getByText('节点接入')).toBeInTheDocument()
expect(screen.getByText('已接收观测')).toBeInTheDocument()
expect(screen.getByRole('button', { name: '确认重新绑定' })).toBeInTheDocument()
expect(screen.getByRole('button', { name: '拒绝该指纹' })).toBeInTheDocument()
expect(screen.getByRole('button', { name: '重置绑定关系' })).toBeInTheDocument()
```

For target detail form tests, replace label queries:

```tsx
screen.getByLabelText('HTTP 协议')
screen.getByLabelText('HTTP 路径')
screen.getByLabelText('HTTP 方法')
```

Run:

```bash
cd web && npm test -- --run NodeOnboardingPage TargetDetailPage
```

Expected: fails before implementation where old English labels/buttons remain.

- [ ] **Step 2: Update onboarding labels and actions**

In `web/src/pages/NodeOnboardingPage.tsx`, replace:

- `Node Onboarding` → `节点接入`
- `accepted observation` → `已接收观测`
- `Binding Conflict` → `绑定冲突`
- `confirm rebind` → `确认重新绑定`
- `reject fingerprint` → `拒绝该指纹`
- `reset binding` → `重置绑定关系`
- `Enrollment Token` → `接入 Token`
- `Install Steps` → `安装步骤`
- `Current Status` → `当前状态`

Keep `Token`, `agent`, and `systemd` where they are technical terms.

- [ ] **Step 3: Update ProbeItem form labels**

In `web/src/pages/TargetDetailPage.tsx`, replace:

- `HTTP Scheme` → `HTTP 协议`
- `HTTP Path` → `HTTP 路径`
- `HTTP Method` → `HTTP 方法`

Do not rename protocol option values `http`, `https`, `GET`, or `HEAD`.

- [ ] **Step 4: Verify and commit**

Run:

```bash
cd web && npm test -- --run NodeOnboardingPage TargetDetailPage
cd web && npm run build
```

Commit:

```bash
git add web/src/pages/NodeOnboardingPage.tsx web/src/pages/NodeOnboardingPage.test.tsx web/src/pages/TargetDetailPage.tsx web/src/pages/TargetDetailPage.test.tsx
git commit -m "Make V1 operational copy Chinese-first" -m "The frozen visual review allows English only for technical identifiers. This converts ordinary onboarding and ProbeItem form labels to Chinese-first copy while preserving technical terms such as HTTP, Token, ProbeItem, agent, and systemd.

Constraint: Copy-only UI alignment; no API or behavior changes.
Confidence: high
Scope-risk: narrow
Tested: cd web && npm test -- --run NodeOnboardingPage TargetDetailPage; cd web && npm run build.
Not-tested: Screenshot comparison remains tracked separately."
```

---

### Task 4: Update V1 visual evidence docs and run full verification

**Files:**
- Modify: `docs/operations/v1-visual-verification.md`
- Modify: `docs/release/v1-gap-checklist.md`

- [ ] **Step 1: Update visual verification record**

Add a note to `docs/operations/v1-visual-verification.md`:

```markdown
## Implementation alignment pass

The implementation-controlled alignment pass has been applied:

- shell context and top-header hierarchy were strengthened;
- dashboard copy now prioritizes current risk;
- ordinary onboarding and ProbeItem form labels are Chinese-first.

This does not replace screenshot comparison. `docs/operations/visual-evidence/*.png` remains the expected place for future captured evidence.
```

- [ ] **Step 2: Update gap checklist**

In `docs/release/v1-gap-checklist.md`:

- keep `Visual screenshot comparison against baseline PNGs` as `Partial`;
- keep live smoke and Telegram evidence partial;
- change `Chinese-first UI copy and dense baseline hierarchy` from `Partial` to `Closed` with evidence pointing to the modified frontend files and visual verification doc;
- change `Frozen app shell and primary navigation` to `Closed for implementation; screenshot evidence remains separate` or equivalent wording.

- [ ] **Step 3: Run full verification**

Run:

```bash
cd web && npm test -- --run AppShell DashboardPage NodeOnboardingPage TargetDetailPage
cd web && npm run build
go test ./...
./scripts/verify.sh
```

Expected:

- focused frontend tests pass;
- web build passes;
- Go tests pass;
- full repository verification passes.

- [ ] **Step 4: Commit**

```bash
git add docs/operations/v1-visual-verification.md docs/release/v1-gap-checklist.md
git commit -m "Record V1 visual language alignment evidence" -m "After the implementation alignment pass, the release checklist should distinguish closed implementation-level shell/copy alignment from still-pending screenshot comparison evidence.

Constraint: Do not claim live visual screenshot proof without committed screenshots.
Confidence: high
Scope-risk: narrow
Tested: cd web && npm test -- --run AppShell DashboardPage NodeOnboardingPage TargetDetailPage; cd web && npm run build; go test ./...; ./scripts/verify.sh.
Not-tested: Live screenshot comparison remains pending."
```

---

## Final Review

After all tasks:

```bash
git status --short --branch
git log --oneline -8
```

Expected: clean `main` branch with the new spec, plan, implementation, docs, and verification commits.
