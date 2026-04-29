# Houfeng V1 Visual Language Alignment Design

## Context

Phase 6 delivery verification made the remaining V1 visual risks explicit instead of treating them as finished without evidence. The current implementation is functionally broad, but the V1 gap checklist still marks two frontend areas as partial:

- the global shell and page hierarchy are simpler than the Unified / Baseline shell;
- parts of the UI still use English as ordinary interface copy instead of reserving it for technical identifiers.

This design closes the implementation-controlled portion of those gaps. It does not attempt pixel-perfect visual cloning, does not create new product behavior, and does not replace the frozen Stitch baseline. Live screenshot comparison remains a separate evidence step.

## Frozen Constraints

- Product name remains `候风 / Houfeng Fleet Control Plane`.
- Visual authority remains the Unified / Baseline Stitch screen set.
- Chinese is the primary interface language.
- English is allowed for technical identifiers: HTTP, TLS, TCP, ProbeItem, Token, systemd, API paths, IDs, and protocol values.
- The UI remains dark-first, restrained, dense, and engineering-tool oriented.

## Selected Approach

Use a narrow implementation-alignment pass:

1. Strengthen the shared app shell and top header so every page inherits the same operational frame.
2. Reweight the dashboard toward current risk first, then abnormal object summaries, then event history.
3. Replace ordinary English interface labels on onboarding and ProbeItem forms with Chinese-first labels while preserving technical terms.
4. Update the release checklist and visual-verification record to distinguish implementation alignment from still-pending screenshot evidence.

Rejected alternatives:

- Full visual rewrite of all pages. This is too broad and risks reopening the frozen V1 visual design.
- Pixel-perfect clone from Stitch HTML. The current app has live data and interaction states that should remain implemented components, not static exported markup.
- Add a screenshot-diff dependency now. The repository currently has no browser E2E stack, and adding dependencies is outside this narrow V1 alignment pass.

## App Shell Alignment

The app shell should keep its existing routing and data model, but present a more explicit control-plane frame:

- fixed left navigation with product identity;
- compact system context block in the sidebar;
- top header with Chinese product title, English product identifier as technical subtitle, and operational status chips;
- consistent content slot and dense spacing for all pages.

This is an implementation refinement of the existing shell, not a new navigation model.

## Dashboard Alignment

The dashboard already has the required data. The layout should make the current risk layer more explicit:

- top page panel becomes a current-risk command summary;
- abnormal object total, severe object total, maintenance object total, new incidents, and recoveries remain visible at the top;
- abnormal node and target sections remain below the risk summary;
- event stream remains the historical layer.

No new backend endpoint or dashboard object model is needed.

## Chinese-First Copy Alignment

User-facing interface labels should be Chinese-first:

- onboarding section eyebrows and action buttons become Chinese;
- “accepted observation” becomes “已接收观测”;
- binding conflict actions become “确认重新绑定”, “拒绝该指纹”, and “重置绑定关系”;
- ProbeItem form labels use Chinese descriptors: “HTTP 协议”, “HTTP 路径”, “HTTP 方法”.

Keep technical words where they are the object name or protocol: HTTP, TLS, TCP, ProbeItem, Token, systemd.

## Verification

Required checks:

- focused frontend tests for `AppShell`, `DashboardPage`, `NodeOnboardingPage`, and `TargetDetailPage`;
- `cd web && npm test -- --run AppShell DashboardPage NodeOnboardingPage TargetDetailPage`;
- `cd web && npm run build`;
- `go test ./...`;
- `./scripts/verify.sh`.

Documentation updates must not claim screenshot comparison is complete. They may mark Chinese-first copy and implementation-level hierarchy alignment as closed, while keeping live screenshot evidence partial.

## Self-Review

- Placeholder scan: no placeholder requirements remain.
- Internal consistency: the design preserves the frozen V1 baseline and narrows work to implementation alignment.
- Scope check: this is one focused frontend/docs implementation slice.
- Ambiguity check: screenshot evidence remains explicitly out of scope for completion claims unless actual screenshots are captured.
