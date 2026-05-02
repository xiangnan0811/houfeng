# Stage 1 P1: merge double fetch wrapper (gap #9)

## Goal

合并 `web/src/lib/fetcher.ts`（auth-only）和 `web/src/lib/api.ts` 内置 `request()`（业务）为单一 fetch wrapper + 单一 401 hook。删 `fetcher.ts` + `fetcher.test.ts`。

## What I already know

### 现状

- `lib/fetcher.ts` (33 行)：`fetcher<T>` + `AuthError` + `setUnauthorizedHandler`；`auth-client.ts` 使用
- `lib/api.ts` 内 `request()` (line 40-67)：`ApiError(401, ...)` + `setApiUnauthorizedHandler`；业务 page 使用
- 两套 401 hook 独立注册（`auth-context.tsx:26-31` 只注册 fetcher 的，**业务 hook 当前无人 set**——这本身就是 bug）
- callsite limited：
  - `auth-client.ts` (1 import + 4 fetcher call + 1 AuthError catch)
  - `auth-context.tsx` (1 import + 2 set call)
  - `fetcher.test.ts` (delete)

### Decision (ADR-lite)

**Decision**: 方案 A — **api.ts 吸收 fetcher.ts**。
- 删 `fetcher.ts` + `fetcher.test.ts`
- `auth-client.ts` 改用 api.ts 的 helpers (postJSONBody / requestJSON / requestEmpty 已具备)
- `auth-context.tsx` 改用 `setApiUnauthorizedHandler`（这次也修了 "业务 hook 当前无人 set" 的次生 bug——auth-context 现在统一注册）
- `AuthError` 类删除；auth-client 改 catch `ApiError && status === 401` 等价处理
- export `setApiUnauthorizedHandler` 重命名为 `setUnauthorizedHandler`（更通用，本任务唯一一个）

**Consequences**:
- 单一 401 hook 路径
- 单一 Error 类（ApiError）—— callsite 用 `instanceof ApiError && err.status === 401` 替代 `instanceof AuthError`
- 删一个文件 + 删一个测试 = 净 LOC -50+

## Requirements

1. `web/src/lib/api.ts` 改名 `setApiUnauthorizedHandler` → `setUnauthorizedHandler`
2. 删 `web/src/lib/fetcher.ts`
3. 删 `web/src/lib/fetcher.test.ts`
4. 改 `web/src/lib/auth-client.ts`：
   - 删 `import { fetcher, AuthError } from './fetcher'`
   - 改用 api.ts 的 helpers（postJSONBody / requestEmpty / requestJSON 等）
   - me() 的 `if (e instanceof AuthError)` 改为 `if (e instanceof ApiError && e.status === 401)`
5. 改 `web/src/lib/auth-context.tsx`：
   - import 改为 `import { setUnauthorizedHandler } from './api'`
6. 跑 `make verify-web` 全绿

注：api.ts 内已 export 的 helpers 目前是 `function`（非 export），需要 export 出 postJSONBody / requestEmpty / requestJSON 给 auth-client 用。

## Acceptance Criteria

- [ ] fetcher.ts + fetcher.test.ts 已删除
- [ ] api.ts export `setUnauthorizedHandler` (不带 Api 前缀)
- [ ] api.ts export postJSONBody / requestEmpty / requestJSON 等 auth-client 需要的 helpers
- [ ] auth-client.ts 不再 import fetcher
- [ ] auth-context.tsx 不再 import fetcher
- [ ] grep `AuthError` 在 web/src/ 0 命中
- [ ] grep `setApiUnauthorizedHandler` 0 命中
- [ ] grep `from './fetcher'` 0 命中
- [ ] make verify-web 全绿
- [ ] git diff 范围只在 web/src/lib/ + （可能）auth-client.test.ts 调整

## Out of Scope

- 改 auth-client.test.ts 业务测试（仅必要的 mock 重命名）
- 改业务 page 任何 API 调用方式
- 改 ApiError 形态 / status 含义

## Final Confirmation

**Goal**: api.ts 吸收 fetcher.ts；单一 401 hook + 单一 Error 类；删 2 个文件。
**Approach**: trellis-implement 一次完成。
