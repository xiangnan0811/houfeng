# Markdown 编辑、阅读、差异与材料设计

## 0. Development rebaseline

本任务没有 migration。历史 revision 的 Markdown dialect/version compatibility 保留，因为它是新功能自身的数据合同；旧 application database 或 `experience_logs` compatibility 不保留。feature flag 只用于开发期回滚。

Child 9 先交付较小的 `comment_markdown/v1` 闭合render model与共享
hostile/golden corpus。本文档方言将其作为子集并扩展，而不是复制依赖、分叉
sanitizer规则或替换历史评论renderer；任何共同语义必须由跨合同conformance
测试证明一致。

## 1. Markdown v1方言

服务端 `internal/center/records/markdown.go` 使用 Goldmark 1.8.4，启用GFM/footnote/代码扩展，禁用raw HTML；Bluemonday 1.0.27使用显式tag/attribute/scheme allowlist。Web使用react-markdown 10.1.0 + remark-gfm 4.0.1 + rehype-sanitize 6.0.0，不启用rehype-raw。双方消费 `testdata/markdown/houfeng-v1.json`，并复用 Child 9 `comment_markdown/v1` corpus 中的共同case。

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

## 6. 协作组件集成

子任务9拥有 `RecordCollaborationFields`、`RecordActionList`、`PromoteChecklistActionDialog`、`RecordComments` 与 `RecordFollowButton` 的API和受控组件。本任务的route controller统一加载权限/revision/draft/material/collaboration，但不复制协作状态机：owner/participants/follow-up进入正式revision save，action/comment/follow使用各自API、CAS和活动合同。材料sidebar/drawer展示实际行动项入口，阅读页展示评论/关注；checklist提升只把用户确认的文本发送给action command，不自动删除或勾选Markdown源文。
