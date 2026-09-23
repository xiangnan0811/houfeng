# 共享测试数据

本目录存放跨模块、跨语言共享的测试输入与预期结果，属于测试源码的一部分，必须由 Git 跟踪，以便干净 checkout 和 CI 能复现相同的验证。

Go 工具在常规包扫描时跳过名为 `testdata` 的目录；这不表示该目录应加入 `.gitignore`。

## 内容与归属

- 按领域组织共享样本，并保留单一来源，避免前后端各自维护副本。
- `markdown/houfeng-v1.json` 是 `houfeng_markdown/v1` 的共享契约样本，包含 Markdown 输入、预期渲染模型及应拒绝的输入和模型。Go 的 `internal/center/recordmarkdown` 测试与 Web 的解码、编辑器及渲染一致性测试共同读取它。
- 仅供单个 Go 包使用的样本放在该包的 `testdata/`；仅供前端使用的样本放在对应测试附近。
- 测试生成的报告、截图、缓存和临时数据库应放入被忽略的输出目录或测试临时目录，不写入本目录。真实凭据、生产数据及含敏感信息的导出不得放入本目录。

## 修改与验证

修改共享样本时，应说明对应的契约变化或新增回归场景，并同时验证所有消费者。预期结果必须经过审查，不能仅为消除测试失败而更新。

修改 Markdown 样本后，至少运行以下针对性检查：

```bash
# 仓库根目录：验证 Go 解析与模型合同
go test ./internal/center/recordmarkdown

# web/ 目录：验证 Web 解码与两条渲染路径的一致性
cd web
npm exec -- vitest run src/lib/documentMarkdown.test.ts src/pages/records/editor/markdownGolden.test.ts src/pages/records/editor/markdownEquivalence.test.tsx
```

若变更影响契约或实现，还须同步相关 `spec/`，并按修改范围运行项目要求的完整验证；以上针对性检查不能替代交付门禁。
