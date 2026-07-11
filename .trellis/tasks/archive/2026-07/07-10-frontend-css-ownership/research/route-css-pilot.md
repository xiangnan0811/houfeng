# Asset Decisions route CSS pilot 结论

## 结论

本 Task 不保留 Asset Decisions route-owned CSS pilot。当前 Assets owner 不是单一路由边界；为了只让 `/asset-decisions` 懒加载 CSS，必须先再次拆分 VPS Detail、Subscriptions、Providers、Archive 与 Asset Decisions 共用的样式。该结构改动超出本 Task 的单一 pilot 边界，也无法在不扩大 source file/owner surface 的前提下证明安全收益。

## 证据

- 最终 analyzer 将 Assets owner 识别为 3 个文件、532 条规则：`legacy-assets.css`、`legacy-provider.css`、`legacy-archive.css`。
- `asset-operation-form*` 同时由 `pages/asset-decisions/`、7 个 `pages/vps-detail/` 表单和 `SubscriptionsPage.tsx` 使用。
- `VPSCancellationWorkbench` 的 `asset-cancel-workbench*` 由懒加载的 `VPSDetailPage.tsx` 直接渲染，并非 Asset Decisions route-private surface。
- `legacy-provider.css` 与 `legacy-archive.css` 分别服务独立懒加载的 Providers、Archive/Archive Detail 路由。
- 最终 production build 仍只有入口 CSS 与既有 Login route CSS 两个网络 CSS 文件。把整个 Assets owner 移出入口会让上述路由缺样式；把同一 owner 同时导入多个 route 又不再是“Asset Decisions 单一路由 pilot”。

## 决策

- 保留七 owner 的全局 manifest，不声称 FOUC 或 route CSS 收益已经通过。
- 本轮收益来自不可达 selector 删除、catch-all 消除与同 context 去重；入口 CSS 已从 fresh baseline 的 399,514 raw / 50,095 gzip 降至 293,270 raw / 38,119 gzip。
- 只有当 Asset Decisions CSS 先成为真实 route-private owner，且能在浏览器中同时证明无 FOUC、完整 workflow 通过与入口 raw/gzip 继续下降时，才另立任务重试。
