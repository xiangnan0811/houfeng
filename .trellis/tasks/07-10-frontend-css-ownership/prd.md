# 前端 CSS owner 化与减债

## Goal

把物理拆分的 legacy partial 转换为可治理 owner，使 CSS source、规则、声明、重复 selector 和 production 产物只降不升，并逐域删除不可达 cascade。

## Confirmed Facts

- 全局 CSS source 为 435,865 bytes、3,044 rules、11,892 declarations，production 主 CSS 415,864 bytes。
- `index.css` 串联 24 个 partial，多个 legacy 文件仍为 40-98 KB，约 178 个完整 selector 文本重复。
- 近期拆分主要移动规则，未减少产物；当前 contract test 只正则检查三个 first-match。

## Requirements

- 用 PostCSS AST 输出稳定 inventory 和 budget，初始值取 fresh branch 基线且不得任意抬高。
- owner 固定为 app-shell、dashboard、assets、vps、observability、settings/subscriptions、shared atoms/page。
- 不创建新的 misc bucket；无法归属规则进入删除候选。
- 按 owner 小批删除/合并，media/theme context 不做机械去重。

## Dependency And Scope

- 依赖 `frontend-asset-decisions-domains`，避免为待删除组件固化样式。
- route-owned CSS 只做 Asset Decisions 单一 pilot；无可测收益则保留 owner 全局导入。

## Acceptance Criteria

- [x] AST inventory 和 fail-closed budget 覆盖 source/rules/declarations/duplicates/colors/important/raw+gzip，并可由 Task 10 原样接入 CI。
- [x] 正则 first-match contract 被 AST/context contract 替换。
- [x] 每条 legacy block 有唯一 owner，无新增 misc bucket。
- [x] 最终所有核心指标低于初始基线，production-dist 的 desktop/tablet/mobile local browser gate 通过；Task 10 再把该证据固化为 Playwright CI。
