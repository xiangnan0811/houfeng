# Current State Audit Notes

## Scope

本 research 记录当前规划任务的仓库内证据，供后续实现/检查阶段引用。它不是完整代码审计，而是围绕“旧计划还剩什么、下一阶段如何进入”做的 release/roadmap 证据整理。

## Evidence From The Root Plan

`houfeng_codex_下一步开发计划.md` 的 2026-05-10 状态段已经给出明确结论：

- Task 1-3、Task 5-8 已按仓库当前实现闭合。
- VPS-scoped service/domain 轻量扩展也已闭合。
- Task 4 的 dry-run/import 工具链完成，但真实 40+ VPS 数据执行仍是 user-data-dependent deferred。
- 没有真实数据文件和授权时，不能宣称真实数据执行完成。
- 若扩展到 Provider/DNS 同步、Web SSH、插件、服务发现、完整服务注册表、完整域名管理、RBAC、汇率或评分算法，应先建立新的产品计划和 Trellis task。

## Evidence From Release Docs

`docs/release/asset-ledger-roadmap-completion.md` 记录：

- Asset Ledger 已与 Fleet Observability 并列存在。
- Providers、VPS assets、subscriptions、VPS-to-Node links、renewal decisions、asset histories、timeline/experience logs、service/domain assets、asset pages、decision pages、Dashboard asset summary 均已有实现证据。
- Task 4 的实际真实数据执行未完成，且这是用户数据/授权边界，不是仓库内功能实现缺口。
- “No further immediate feature task remains from `houfeng_codex_下一步开发计划.md`.”

`docs/release/next-phase-plan.md` 当前仍保留 Stage 2 入口和 long-page 拆分历史记录，需要追加当前状态：

- Stage 2 第一条扩展计划已经完成到当前边界。
- 前端页面体验未被用户认可前，不应继续把机械拆分作为下一任务。
- 下一阶段应在真实数据验证或新产品/UX 规划之间做明确选择。

## Current Frontend Refactor Context

最近一批 PR 已经完成了多处页面拆分：

- `DashboardPage`
- `SettingsPage`
- `NodesPage`
- `NodeDetailPage`
- `TargetDetailPage`
- `TargetsPage`
- `EventsPage`

当前 `wc -l` 仍显示若干页面/测试文件较长，但这已经不应自动转化为下一项任务。用户明确表示页面当前不满意，未来肯定还需要大调整。因此下一步应先重新规划页面产品结构、信息架构与 UX，再决定哪些组件值得拆分。

## Planning Conclusion

当前项目剩余工作应分为四类：

1. **条件性剩余**：真实 VPS JSON dry-run/import，等用户提供/授权数据后再做。
2. **需要新产品计划**：DNS/Provider 同步、Web SSH、插件、服务发现、完整域名管理、RBAC、汇率、评分算法等。
3. **暂停的技术债**：前端长页面/大文件继续拆分，等页面 redesign 方向明确后再做。
4. **持续流程要求**：Trellis task、非 main 分支、PR、CI green 后合并、同步本地主分支；release/publish workflow 本阶段暂不处理。
