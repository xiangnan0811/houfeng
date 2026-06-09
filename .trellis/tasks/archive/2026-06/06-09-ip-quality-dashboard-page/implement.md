# Implementation Plan: IP质量驾驶舱页面与VPS摘要入口

## Preconditions

- 当前任务状态必须保持 `planning`，规划工件经用户或本轮明确授权后再 `task.py start`。
- 实现前加载 `trellis-before-dev`，读取 web 和 IP quality 相关 specs。
- 当前分支：`feature/ip-quality-dashboard-page`。

## Checklist

1. 准备展示 helper
   - 提取 service label、unlock status label、risk flag、risk tone、derived score、coverage 等纯函数。
   - 覆盖缺失值和 unknown/error 状态。

2. 改造 VPS 详情摘要卡
   - `VPSIPQualitySection` 接收 `vpsId`。
   - 展示摘要结论、关键风险、服务解锁概览、采集时间、provider/service 覆盖。
   - 增加 `Link` 到 `/vps/:vpsId/ip-quality`。
   - 保留 error/empty/stale/ambiguous 降级。

3. 新增完整驾驶舱页面
   - 新建 `web/src/pages/VPSIPQualityPage.tsx`。
   - 使用 `useParams` + `getVPSIPQuality` 拉取报告。
   - 渲染标题区、质量结论、2x2 指标、风险矩阵、provider 表、service grid、上下文/覆盖、历史、诊断。
   - 使用 `PageState` 处理 loading/error/empty。
   - 提供返回 VPS 详情链接。

4. 注册路由
   - `web/src/app/router.tsx` 增加 lazy import 和 `/vps/:vpsId/ip-quality` route。
   - loading label 使用中文。

5. 样式
   - 在 `web/src/styles/pages.css` 增加 `vps-ip-quality-summary` / `vps-ip-quality-dashboard` BEM 样式。
   - 使用 tokens、状态色和 `color-mix`。
   - 保持响应式：桌面 2 列，窄屏单列；2x2 指标在移动端折叠为单列。

6. 测试
   - 新增/更新 React tests。
   - 验证 fetch URL、主要文本、链接和降级状态。
   - 调整旧的详情页 IP 质量测试预期。

7. 验证
   - `npm test -- --run web/src/pages/VPSDetailPage.test.tsx web/src/pages/VPSIPQualityPage.test.tsx`
   - `npm run typecheck`
   - `npm run lint`
   - 如需视觉 sanity，启动 web dev server 后用浏览器检查 `/vps/:id` 和 `/vps/:id/ip-quality`。

## Risky Files / Rollback Points

- `web/src/pages/VPSDetailPage.tsx`：只改 `VPSIPQualitySection` props，不碰详情页主数据加载。
- `web/src/app/router.tsx`：新增 route 应放在 `vps/:vpsId` 附近；如果 route order 有匹配问题，确保更具体路径优先或 React Router branch ranking 生效。
- `web/src/styles/pages.css`：避免影响现有 `.vps-cost-card` 和 `.asset-*` 样式。
- `VPSDetailPage.test.tsx`：旧测试期望完整矩阵，需要改为摘要+链接。

## Review Gates

- 实现后先自查是否完整覆盖 mockup 的展示区块。
- 如果测试或浏览器截图显示布局密度不足，先修复再进入 Trellis check。
- 不新增后端行为；如果实现中发现当前 API 不能支撑某字段，必须用诚实降级，不临时扩后端。
