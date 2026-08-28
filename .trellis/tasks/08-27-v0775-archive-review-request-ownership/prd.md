# 收紧 Legacy archive review 请求归属

## Goal

为 Legacy 归档资格读取增加独立、可失效的 request owner，使路由切换、关闭弹窗和再次打开都能阻止旧请求的 late success、failure、finally 污染当前确认弹窗。

## Requirements

1. 每次 archive review 请求捕获单调递增 request ID 与目标 VPS ID；只有同时匹配最新 request ID 和当前路由 VPS 的请求拥有 UI 修改权。
2. 新开 review 必须使此前请求失效，包括同一 VPS close 后 reopen 的情况。
3. 路由 effect 开始/清理与 modal close 必须使当前 review owner 失效，不能只清空可见 state。
4. success、catch、finally 分支分别在写入 review data、error、loading 前校验 owner；旧 finally 不得把新 dialog 的 loading 设为 false。
5. archive confirm 的既有服务端资格复核、mutation owner、错误文案和导航语义保持不变。
6. 测试使用 controlled deferred promise 精确控制先后顺序，不使用 sleep。
7. 同 VPS query reload 的 route payload 若关闭/reset modal，必须先递增 request ID；reload pending 期间新开的 review 不能在 payload reset 后提交 success、failure 或 finally。
8. late-success 必须拆成独立证明：A2 pending 时先完成 stale A1，证明旧 `finally` 不清除 A2 loading/confirm authority；另在 eligible A2 已完成并启用 confirm 后完成带 blocker 的 A1，证明旧 `then` 不改写 data/confirm。

## Out of scope

- operation write owner 与 detail/services/domains refresh；上一子任务负责。
- 将客户端 review 视为授权或替代 archive endpoint 的服务端复核。
- 新增 abort controller、全局 modal manager 或通用请求库。

## Acceptance Criteria

- [x] A review pending 后切 B 并打开 B review，A success 不能写入 B review、启用 B confirm 或覆盖 B 数据。
- [x] A rejection 不能在 B dialog 显示错误，也不能清掉 B 的 review。
- [x] A finally 不能结束 B 的 loading；只有 B 自己的 owner 可结束。
- [x] 同一 VPS 关闭后重开，第一次请求的 success/failure/finally 均不能修改第二次 dialog。
- [x] 当前 owner 的成功、失败与 loading 收尾仍按现有交互工作，archive confirm 流程保持通过。
- [x] focused `LegacyVPSDetail.test.tsx` 与 web 质量门通过。
- [x] same-VPS query reload pending → open review → payload reset → late review rejection/success/finally；页面无迟到 error/data/loading 泄漏。
- [x] same-VPS late-success 分别对 `finally` 与 `then` 做 mutation 证明：A1-before-A2 保持 loading/disabled confirm，A2-before-blocker-A1 保持 A2 data/enabled confirm。

## Constraints

- 本任务在 operation/refresh ownership 子任务之后顺序执行，并以其最终组件结构为起点。
- request ID 是 modal read owner，不复用 write token，也不改变 transport 行为。
