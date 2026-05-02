# Stage 1 P1: docs sync (merge smoke caveats + Telegram mark)

> 纯 docs，无代码改动。把 2026-05-02 smoke 4 caveats 落进权威文档。

## Goal

4 处 docs 修订：
1. `docs/release/v1-gap-checklist.md` —— 末尾 12 条新 gap 段追加 4 行（#13-#16，来自 smoke caveats）
2. `docs/operations/v1-smoke-run.md` Step 2 —— 修 token 响应键名（`plaintext_token` → `token`）
3. `docs/deploy/local-and-systemd.md` —— 加 `HOUFENG_WEB_DIST_DIR` 必配警告
4. `docs/release/next-phase-plan.md` —— Telegram 真实环境验证 mark "user-env-required，本 session 未触发"

## Requirements

### 1. v1-gap-checklist.md 末尾段加 4 行

在 "### Web (5 条)" 表格之后或 "### Backend" 表格扩展，加新表 "### Operations / Smoke (4 条 - 新增 2026-05-03)"：

```markdown
### Operations / Smoke (4 条，新增 2026-05-03，来自 2026-05-02 live smoke)

| # | 现象 | 证据 |
|---|---|---|
| 13 | `POST /api/nodes/{id}/enrollment-token` 实际响应键名是 `token`，docs Step 2 写 `plaintext_token` | 2026-05-02 smoke evidence: `research/v1-smoke-evidence-2026-05-02.md` (already archived); `docs/operations/v1-smoke-run.md` Step 2 当前文档 |
| 14 | `agent/hostsample` 需要 Linux `/proc/loadavg`，macOS local dev 静默 fail | smoke Step 3 PARTIAL: agent enrolls 但 host sample 段失败；systemd 部署目标不受影响 |
| 15 | Center `/` 返回 404 当 `HOUFENG_WEB_DIST_DIR` 未配置——生产部署必须配 | smoke Step 9 INCONCLUSIVE: 改用 vite :5173 验 SPA |
| 16 | `GET /api/events` 返回 bare JSON array，非 `{items:[...]}` envelope；后续如引入 envelope，所有 caller + smoke 同时破 | smoke Step 8 实测；`internal/center/http/handlers/events.go` |
```

### 2. v1-smoke-run.md Step 2 修 token 键名

找到 Step 2（line ~80-91）"Record the returned plaintext token once. Store it for the local agent" 段，改 `plaintext_token` 引用为 `token`（看实际原文确定具体改法）。注释加注："（响应键名实际为 `token`）"。

### 3. local-and-systemd.md 加 HOUFENG_WEB_DIST_DIR 警告

在 env vars 段或 Systemd unit 段，加一句强制警告：
"⚠️ `HOUFENG_WEB_DIST_DIR` 必须设置——否则 center `/` 返回 404，SPA 不可访问。生产部署应指向 `web/dist`。"

### 4. next-phase-plan.md Telegram mark deferred

在 Stage 1 P0 列表（"真实环境冒烟" 行附近）或 P1 列表（Telegram 行）加注：
"Telegram 真实环境验证：标 user-env-required；2026-05-02 smoke 因无 Telegram env vars 未触发；归 ops follow-up，**本表中视为已 acknowledged**，不阻塞 Stage 1 收口判定。"

## Acceptance Criteria

- [ ] v1-gap-checklist.md 末尾增 4 行 (#13-#16)
- [ ] v1-smoke-run.md Step 2 token 键名修正
- [ ] local-and-systemd.md 含 HOUFENG_WEB_DIST_DIR 强制警告
- [ ] next-phase-plan.md Telegram 行 mark deferred
- [ ] git diff 范围只在这 4 个 docs（+ 任务脚手架）
- [ ] 不动业务代码 / .trellis/spec/ / 其他 docs / 已 archive 文档

## Out of Scope

- 修复 caveat 对应的代码问题（如 agent macOS 兼容、events envelope 引入）
- 改 v1-smoke-run.md 的 9 步主要操作描述（仅修 Step 2 一行）

## Final Confirmation

**Goal**: 4 处 docs 同步 smoke 真实 finding。
**Approach**: trellis-implement 一次完成。
