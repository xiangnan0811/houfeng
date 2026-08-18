# Markdown 编辑、阅读、差异与材料设计

## 0. Development rebaseline

本任务没有 migration。历史 revision 的 Markdown dialect/version compatibility 保留，因为它是新功能自身的数据合同；旧 application database 或 `experience_logs` compatibility 不保留。feature flag 只用于开发期回滚。

Child 9 先交付较小的 `comment_markdown/v1` 闭合render model与共享
hostile/golden corpus。本文档方言将其作为子集并扩展，而不是复制依赖、分叉
sanitizer规则或替换历史评论renderer；任何共同语义必须由跨合同conformance
测试证明一致。

## 1. Markdown v1方言

服务端 `internal/center/recordmarkdown` 的分工：

- **Goldmark 1.8.4 是源级准入门（`ScanDocumentSource`）**，启用 GFM/footnote/代码扩展、禁用 raw HTML，用来在解析前拒绝 raw HTML、图片和非规范链接。它不是语义 parser。
- **语义解析由本包的块扫描器完成**：文档独有块（标题、任务列表、脚注、表格、引用块、分隔线、`houfeng-ref` 引用、围栏代码）由 `recordmarkdown` 自己解析，其余"共享区域"委派给 Child 9 的 `comment_markdown/v1` 块/行内核心（`ParseSharedMarkdownRegionV1`），从而复用而不分叉共同语义。共享区域按空行切块后逐块委派，文档预算（256KB / 4096 节点 / 深度 16）不再继承评论方言的 16KB 上限。
- **Bluemonday 1.0.27 只服务于 `RenderSafeHTML`**，它是预留给 Child 10 导出/打印路径的服务端 HTML 出口；当前读写路径不产出服务端 HTML，浏览器只消费 `render_model`。

该包独立于 `records`，避免 `records` ↔ `recordcollaboration` 导入环。Web使用react-markdown 10.1.0 + remark-gfm 4.0.1 + rehype-sanitize 6.0.0，不启用rehype-raw。双方消费 `testdata/markdown/houfeng-v1.json`，并复用 Child 9 `comment_markdown/v1` corpus 中的共同case。

方言当前不表达的结构（嵌套列表、列表项懒续行、缩进代码块、未闭合围栏）由 `ParseDocumentMarkdownV1` 明确拒绝：与其发布一个和 Markdown 读法不一致的 model，不如让 revision 响应带 `render_model_status: "unsupported"`，由前端回退到实时渲染并提示。扩展这些结构需要先改 `houfeng_markdown/v1` 合同（Go 校验 + TS 解码器 + 渲染器 + corpus 同步），属于独立决策。

引用语法：

```markdown
<!-- houfeng-ref:v1 evidence ev_7K2P -->
[系统证据：第三晚 TCP 观测](houfeng-evidence:ev_7K2P)
<!-- houfeng-ref:v1 attachment att_4D8M -->
[用户附件：mtr-third-night.png](houfeng-attachment:att_4D8M)
```

parser只接受清单中已授权ID；display label不作为身份权威。

## 2. 路由与前端边界

- `RecordNewPage.tsx` `/records/new`
- `RecordDetailPage.tsx` `/records/:recordId`
- `RecordEditPage.tsx` `/records/:recordId/edit`
- `RecordRevisionPage.tsx` `/records/:recordId/revisions/:revisionId`

静态new/compare在dynamic `:recordId`前显式注册。route page是controller；`pages/records/editor/`只接props，不调用API。CodeMirror与preview按route lazy import，正文行长受限；材料drawer复用Modal/focus stack，不假设存在通用Drawer原子。

## 3. Draft/security controller

`useRecordDraft`返回 `{state,commands}`，管理server ETag、idle autosave、unload warning、IndexedDB unsynced buffer和formal save。`recordSecurity.ts`统一ClientContentLease/SSE→poll fallback/BroadcastChannel；revoke先遮蔽再读本地buffer。

## 4. Diff/冲突

字段diff按revision schema有序输出；Markdown用`diff@9.0.0`生成line/word hunks。resolver保留local/server/base三方输入，用户逐字段选择或编辑合并结果；保存仍以最新base revision重试。

## 5. 安全阅读模型

服务端返回raw source + allowlisted rendered model，不返回可执行HTML；Web自行安全渲染并以结构化组件替换houfeng refs。导出服务只调用同一server renderer，不能另建宽松Markdown路径。

写入路径（`records.NormalizeCompleteRevisionInput`）只校验 UTF-8 与方言版本，不校验方言结构，因此"能保存"严格宽于"能产出 render model"：嵌套列表、未闭合围栏、raw HTML、超出 256KB 预算的正文都能落库，只在读取时以 `render_model_status: "unsupported"` 表达。这是有意的——写入不因渲染器的表达能力阻塞操作员，浏览器回退路径（react-markdown + rehype-sanitize，未启用 rehype-raw）本身就是消毒路径。

因此服务端 HTML 出口不是 `RenderSafeHTML`（入参是 model，`unsupported` 正文没有 model），而是 `SafeDocumentHTML(source, authorized) (html, status, error)`：能建模走 model + Bluemonday；不能建模则把 source 以转义 `<pre><code>` 交给同一个 Bluemonday policy。选择"转义纯文本"而不是"拒绝导出"，因为导出承担可移植性承诺，不能因为一个嵌套列表就丢内容；这条回退不是第二条 Markdown 路径——它根本不解释 Markdown，测试断言包裹之外不残留任何 `<`/`>`。Child 10 直接调用它并按返回的 status 决定是否在导出物里标注降级，不要在导出侧重新实现回退。

`DocumentRenderStatus`（`ready` / `unsupported`）由 `recordmarkdown` 拥有，HTTP 响应与导出出口共用同一套 token；Web 侧 `decodeRenderModelStatusV1` 在消费点收窄，未知值按"无状态"处理。

## 6. 协作组件集成

子任务9拥有 `RecordRevisionCollaborationControls`、`RecordActionPanel`、`RecordCommentThread`、`RecordWatchControl` 与 `RecordCommentMarkdown`。本任务创建 `PromoteChecklistActionDialog`，并在阅读/编辑工作区挂载上述受控组件。route controller统一加载权限/revision/draft/material/collaboration，但不复制协作状态机：owner/participants/follow-up进入正式revision save，action/comment/follow使用各自API、CAS和活动合同。材料sidebar/drawer展示实际行动项入口，阅读页展示评论/关注；checklist提升只把用户确认的文本发送给action command，不自动删除或勾选Markdown源文。
