# 资产组合决策工作台体验升级 · Implement

## Checklist

- [x] 读取 before-dev 规范：web index、component/state/styling/quality，必要时 code reuse guide。
- [x] 启动 Trellis task：`python3 ./.trellis/scripts/task.py start 06-06-asset-decisions-workbench-polish`。
- [x] 在 `AssetDecisionsPage.tsx` 增加局部派生模型：
  - portfolio lead / first action
  - context summary
  - source availability summary
  - compact group facts
- [x] 重构首屏 summary 区域，让它表达“当前判断轨道”和上下文，而不是纯计数。
- [x] 调整自动组卡片布局和内容层级，确保主问题、建议、证据质量、承载/监控/成本更易扫描。
- [x] 调整下一步导览卡片结构，明确来源、对象、动作和局部错误边界。
- [x] 强化场景/记录区域文案和层级，保持低于自动组、高于证据/单台队列。
- [x] 强化记录详情 execution board 的 lane summary 和成员卡片视觉，不改变 PATCH 行为。
- [x] 更新 `web/src/index.css` 局部样式和 responsive 规则。
- [x] 更新 `AssetDecisionsPage.test.tsx`，覆盖：
  - 新 command summary 文案和上下文展示。
  - 自动组仍是主 surface。
  - next work 点击不触发业务资产写入。
  - record execution board CTA/followup 写接口边界。
  - 单台队列 PATCH payload 不变。
- [x] 更新 `.trellis/spec/web/state-and-data.md` 和 `docs/design/v2-houfeng/component-spec.md`。

## Validation Commands

- `npm --prefix web test -- AssetDecisionsPage`
- `npm --prefix web test -- AssetDecisionsPage DashboardPage VPSPage ProvidersPage SubscriptionsPage MonitoringPage TargetsPage ObservabilityEvidenceLead`
- `npm --prefix web run lint`
- `npm --prefix web run build`
- `git diff --check`
- `python3 ./.trellis/scripts/task.py validate 06-06-asset-decisions-workbench-polish`
- Visual sanity:
  - Prefer `python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --routes "/asset-decisions?view=needs_decision&renew_within_days=30" "/asset-decisions?view=evidence&renew_within_days=30&scenario=evidence_cleanup"`.
  - If local Python Playwright is unavailable, use Vite + existing visual fixture/mock API fallback and Codex in-app browser; report tooling limitation clearly.

## Review Gates

- Do not start implementation before `task.py start` succeeds.
- Do not introduce direct `fetch()` in page/component.
- Do not call VPS/Subscription/MonitoringInstance/Target write APIs from new controls.
- Do not modify backend, migrations, global tokens, or unrelated page surfaces.
- Do not mark task complete without tests and visual sanity evidence.

## Validation Evidence

- `npm --prefix web test -- AssetDecisionsPage --run` passed: 1 file / 16 tests.
- `npm --prefix web test -- AssetDecisionsPage DashboardPage VPSPage ProvidersPage SubscriptionsPage MonitoringPage TargetsPage ObservabilityEvidenceLead --run` passed: 8 files / 111 tests.
- `npm --prefix web run lint` passed.
- `npm --prefix web run build` passed.
- `git diff --check` passed.
- `python3 ./.trellis/scripts/task.py validate 06-06-asset-decisions-workbench-polish` passed.
- `python3 scripts/visual_evidence.py browser-sanity ... --mock-api asset-workflows` was blocked by missing local Python Playwright, as documented by the script.
- Fallback visual sanity used Vite with the existing `asset-workflows` mock API fixture and the Codex in-app browser. Routes checked:
  - `/asset-decisions?view=needs_decision&renew_within_days=30`
  - `/asset-decisions?view=evidence&renew_within_days=30&scenario=evidence_cleanup`
  - Viewports: `1440x1000`, `390x900`
  - Evidence: command summary, decision groups, next work, scenario records, renewal evidence, single queue, and carrier evidence loaded with zero console errors and no page/main-content horizontal overflow. Record detail modal execution board opened in both viewports; board/lane/modal/page overflow was false, with the wide member table constrained to its own `.asset-table-scroll` region.
