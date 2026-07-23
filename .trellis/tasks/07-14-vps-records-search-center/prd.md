# 搜索、记录中心与全局搜索

## Goal

实现服务端全文与结构化检索、记录中心、URL 筛选、稳定游标和权限安全的顶部搜索入口。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §7、§16、§19、§25。
- 直接依赖：子任务1、2、5、9已合入main。子任务5提供权威Markdown解析/纯文本合同，子任务9提供owner/participant/follow-up/action筛选事实；完整记录中心不得以hook关闭或后续补齐方式缩减这些筛选。
- 本任务计划拥有`0055_create_record_search.sql`。启动时已合入的0051–0054与0056永久冻结；如果任务外受保护主线占用尚未发布的0055，只把search改为下一个可用编号并顺延仍未发布的0057–0060，绝不改名/重排/改写0051–0054或已发布0056。保留号仍可用时必须由本任务用真实0055验证ledger已有0056后的late apply。
- 默认只索引active记录最新正式revision；archive/history分别显式加入，draft与永久删除内容永不索引。
- 服务端索引title/Markdown纯文本/tag/type/status/primary+related identity/evidence summary/attachment safe name，权限过滤先于rank/snippet/page。
- 中文/英文/code token使用规范化纯文本、pg_trgm GIN与tsvector；不引入外部搜索服务。
- 筛选覆盖type、primary/related subject、status、owner/participant、record follow-up、action status/assignee/due、occurred/updated、revision range；同字段OR、跨字段AND。
- query/sort/cursor由服务端执行并规范化到URL；cursor绑定query digest、auth scope、fixed bounds/as-of watermark与稳定tie-breaker。
- 所有记录平台 cursor 使用共享的 versioned confidential envelope：AES-256-GCM 随机 nonce、purpose namespace、固定长度桶 padding、跨实例 0400 keyring 与一小时 TTL。仅签名但可解码的 base64 JSON 不符合“opaque”；客户端不得读取或比较内部 watermark、scope hash 或 stable ID，key 轮换须先分发 verify/decrypt key，再切 current，并保留旧 key 至最后签发时间 + TTL + 2 分钟时钟偏差。
- 项目级记录中心提供搜索、显式filters/chips、高密度结果、稳定分页；实际待跟进/受阻/临期有数据才插入，无状态不占位。
- 顶部GlobalSearch改为server record摘要+全部结果入口，不能继续为记录拉全量列表/正文。
- 提供draft恢复入口，但draft只对作者显示且不进普通搜索/统计。
- 搜索投影可重建、可校验、可观测；projection lag显示水位，不能把旧结果冒充最新。

## Acceptance Criteria

- [ ] 10k records/200k revisions条件下首25条p95≤1s，query count有界，EXPLAIN使用权限/GIN/稳定游标索引。
- [ ] active/archive/history/draft/deleted范围矩阵无泄露；未授权标题/snippet/count/subject identity命中0。
- [ ] 中文、英文、code token、literal `%/_`、空白/大小写/Unicode normalization结果稳定。
- [ ] 全部筛选与URL codec往返无歧义，follow-up和action due不混淆，EXISTS不产生重复记录。
- [ ] cursor混入新query/权限变化/损坏/过期返回明确400/409且不跳页/重复。
- [ ] cursor plaintext/长度侧信道、purpose混用、nonce重用、两实例滚动轮换、旧 key TTL、未知/撤销 key 与篡改测试通过；客户端和普通日志无法取得 watermark、scope hash 或 stable record ID。
- [ ] record中心初始空、查询无结果、loading/error/append/revoke状态符合Artifact v1。
- [ ] GlobalSearch只取少量授权摘要，不下载完整Markdown/attachments/history。
- [ ] projector重建后canonical hash/结果与事务投影一致，lag/失败可恢复。
- [ ] `0055` 在0051–0054之后正常应用，也能在主线已经记录独立且不依赖search的0056时作为missing migration补应用；两条路径重复执行均保持schema/data不变。
- [ ] Go/PostgreSQL、Web、E2E、Node22 bundle/CSS与完整质量门通过。

## Out of Scope

- 不建设外部搜索集群、语义向量搜索或草稿全文检索。
- 不索引评论正文、行动项正文或通知摘要；结构化协作筛选只使用task9 current projection/`EXISTS`合同。

## Execution Gate

- 保持planning；依赖合入和用户执行授权后才start。
