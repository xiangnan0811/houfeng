# 修复 IP 质量详情展示与采集覆盖 - 实施计划

## Preconditions

- 当前任务保持 `planning`，用户审查通过后才执行：

```bash
python3 ./.trellis/scripts/task.py start .trellis/tasks/06-10-fix-ip-quality-detail-coverage
```

- 执行前加载：

```bash
python3 ./.trellis/scripts/get_context.py --mode phase --step 2.1 --platform codex
cat .trellis/spec/backend/ip-quality-contract.md
cat .trellis/spec/web/component-conventions.md
cat .trellis/spec/web/styling-guidelines.md
cat .trellis/spec/web/quality-guidelines.md
cat .trellis/spec/backend/quality-guidelines.md
```

## Implementation Steps

1. 前端文案删除
   - 修改 `web/src/components/ip-quality/IPQualityDashboard.tsx`。
   - 删除 hero description、风险矩阵 meta、provider 表格 meta。
   - 更新页面测试，断言这些说明文字不存在。

2. 服务解锁卡片文案修复
   - 修改 `web/src/components/ip-quality/ipQualityPresentation.ts`。
   - 扩展内部诊断过滤，覆盖完整英文句子和 `unsupported_default_probe` 等 code。
   - 按 `status/probe_status/error_code` 输出中文中性说明。
   - 增加 helper 单测或页面 fixture 测试，覆盖 Disney+ skipped/unknown 场景。

3. 服务统计横排布局
   - 在 `IPQualityDashboard.tsx` 给统计容器使用本页专用 class。
   - 修改 `web/src/index.css` 中 IP 质量样式，保证桌面 flex row + 右对齐，移动端不溢出。
   - 浏览器视觉检查桌面和移动。

4. Provider 主表降噪
   - 在 `ipQualityPresentation.ts` 增加 helper：
     - `visibleProviderResults(report.provider_results)`：主表排除 optional `not_configured/skipped` 空行，保留 default failure。
     - `providerSourceGaps(report.provider_results)`：给 coverage/diagnostics 展示未配置/未检测来源。
   - 修改 `IPQualityDashboard.tsx` 使用 visible rows。
   - 在采集完整性或诊断区展示 source gaps，避免用户误以为数据被删除。
   - 调整 `providerEvidenceSignals`，不要对 failure/not_configured 行显示“无用户证据”。

5. 风险列紧凑展示
   - 改 JSX 为 inline risk cell。
   - CSS 新增 `vps-ip-quality-dashboard__risk-cell` 和 `__risk-score`。
   - 移除或收窄 provider table 对所有 `small` 的 block 样式影响，避免风险值换行。

6. Agent provider parser 回归与修复
   - 增加固定 fixture 测试，使用 `209.33.173.4` 的 provider 响应样例。
   - 按失败测试修复：
     - `parseIPAPIISProvider`
     - `parseProxycheckProvider`
     - `parseIP2LocationProvider`
     - `parseIPWhoIsProvider`
     - `parseIPQueryProvider`
   - 重点补齐 string bool、risk score、VPN/type 推导、region fallback、ASN/AS extra preservation。
   - 不做 live network test，不调用远程 shell。

7. Center/API 链路核对
   - 检查 sync ingest/store/API 是否已保存并回读 parser 新填字段。
   - 如果字段已完整透传，只补测试或不改 center。
   - 如果有丢字段，修 DTO/repository/API 测试。

8. 验证
   - 后端：

```bash
go test ./agent/ipquality
go test ./internal/contracts/agentapi ./internal/center/http/handlers ./internal/center/store
```

   - 前端：

```bash
cd web && npm run lint
cd web && npm run test -- --run
cd web && npm run build
```

   - 通用：

```bash
git diff --check
```

9. 审查与收尾
   - 使用 `trellis-check` 做跨层审查。
   - 如发现 IP 质量合同需要补充 parser/展示新约定，使用 `trellis-update-spec`。
   - 通过后进入 Phase 3.4 提交，再走 `finish-work`。

## Risk Points

- 不要把 optional `not_configured` 行直接删除到不可见；必须在 coverage/diagnostics 里保留采集缺口。
- 不要把 provider 分歧合并掉；`ipquery.io` 对 `209.33.173.4` 的 US/Windstream 分歧是应展示的证据。
- 不要把 unknown/skipped/failure 服务统计成 blocked。
- 不要通过 CSS 硬编码固定宽度导致移动端横向溢出。
- 不要为了补字段引入需要 API key 或不稳定网页挑战的默认来源。
