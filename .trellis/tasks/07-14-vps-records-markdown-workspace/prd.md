# Markdown 编辑、阅读、差异与材料

## Goal

实现 Markdown 权威源文、模板、引用块、安全阅读、材料选择、草稿状态、修订差异与冲突合并工作区。

## 2026-08-02 Development Rebaseline

本任务不创建 root migration。按默认顺序从已合入 Child 2/3/4/9 的 protected main 开始，复用 `0055` collaboration 合同；只要求当前开发版功能，不承担旧数据库、legacy experience、staging 或 release 兼容。

2026-08-17 责任边界重基线：Child 9 先交付最小版本化
`comment_markdown/v1` server/Web renderer 与 hostile/golden corpus。本任务必须
复用该合同，并在其上扩展完整文档方言、引用和编辑器；不得另建第二套评论
renderer或原地重解释历史评论。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §9、§12.6、§13、§19–§21、§24。
- 直接依赖：子任务 2、3、4、9 已合入 main；子任务1为传递依赖。子任务9先提供 owner/participant/action/comment/follow 的稳定 API、受控组件与安全评论 corpus，本任务负责把它们完整接入记录阅读/编辑工作区，不能留下孤立能力。
- Markdown源文和版本化结构化引用清单是唯一权威，不使用编辑器私有JSON。
- 支持 GFM表格/任务清单/删除线、脚注、围栏代码/高亮、标题目录；原始HTML、脚本、iframe、style、危险URL禁用。
- 系统证据与受管附件使用稳定引用语法和独立引用清单；正文不嵌入JSON、binary或data URL。
- 提供新建、阅读、编辑、历史revision、restore、三模式编辑/分栏/预览、工具栏/快捷键、模板与材料侧栏/390px抽屉。
- 自动草稿与正式保存分离；服务端同步失败保留本地未同步字段/Markdown，IndexedDB按user/draft隔离≤24h且不含材料bytes。
- 正式保存冲突展示字段和Markdown差异，用户人工合并；权限撤销/永久删除立即清内存/IndexedDB/object URL并显示无内容shell。
- 安全渲染在服务端Goldmark+Bluemonday和Web react-markdown/rehype-sanitize双层执行；文档 corpus 包含并扩展 Child 9 comment-safe corpus，评论和文档的共同语义必须保持等价。
- 阅读面明确区分系统证据、用户附件和作者判断；引用失效显示tombstone，不留空白/坏卡。
- 阅读/编辑工作区复用子任务9的负责人/参与者/跟进、行动项、评论和关注组件；桌面材料侧栏与390px抽屉包含实际行动项入口，阅读面保留评论时间线。Markdown task checklist只有经过显式预览/确认才提升为结构化行动项。
- Markdown目录导出和PDF可复用服务端render model，但真正export job由子任务10实现。
- 视觉实现遵守Artifact v1编辑器/证据选择器桌面和390px合同、焦点与状态矩阵。

## Acceptance Criteria

- [ ] Go/TS对同一golden corpus产生等价语义树/安全输出，hostile HTML/URL/属性/XSS命中为0。
- [ ] 编辑、分栏、预览、工具栏、快捷键、模板插入/类型切换不覆盖已有正文。
- [ ] evidence/attachment引用可视装饰与底层Markdown/清单往返无损；未知/失效引用安全显示。
- [ ] 自动草稿三态、离开警告、跨设备恢复、IndexedDB TTL/清理和正式保存独立可测。
- [ ] 并发冲突保留本地输入并显示字段/Markdown diff；无人工选择不覆盖server revision。
- [ ] revision详情/restore只读历史、复制为新revision且材料引用仍指当时版本。
- [ ] owner/participant/follow-up与正式revision同一保存；行动项、评论、关注保持独立活动合同。checklist提升需要显式确认且不会静默改正文或记录状态。
- [ ] revoke/deletion在活动/后台/多tab/pageshow/reconnect路径均先遮蔽清理，不闪现旧DOM。
- [ ] desktop/390编辑器和证据选择器通过Artifact布局、键盘/focus、Axe与无横向溢出测试。
- [ ] CodeMirror/Markdown依赖仅进入lazy record chunks，bundle/CSS budget不提高。
- [ ] Node22 `make verify-web`、Go render tests、E2E focused route tests通过。

## Out of Scope

- 不构建WYSIWYG私有文档模型、实时多人协同编辑或自动从Markdown提取业务事实。
- 不重新实现评论持久化、评论renderer或评论安全状态机；这些由已合入的Child 9合同提供。
- Markdown task checkbox不自动变行动项；子任务9拥有提升命令和组件，本任务只把已合入合同接入编辑器并验证往返/焦点/权限。

## Execution Gate

- 保持 planning；Child 2/3/4/9 合入、实际 Markdown/comment/material API 复核和用户执行授权后才 start。
