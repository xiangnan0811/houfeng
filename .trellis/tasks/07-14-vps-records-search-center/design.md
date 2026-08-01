# 搜索、记录中心与全局搜索设计

## 1. Search projection

`0055_create_record_search.sql`启用`pg_trgm`并创建`record_search_documents`、`record_search_subjects`、projection watermark/rebuild jobs。document保存revision ID、normalized plaintext、tsvector、current/archive flags、结构化筛选列和auth scope digest；不是业务权威。

records revision participant在同事务upsert current document。Markdown plaintext只调用task5的versioned parser/render model，不另写会把HTML、引用label或代码语义解析不同的stripper。owner/participant/follow-up从current revision projection读取，action筛选调用task9的结构化`EXISTS`合同；评论/行动项正文和通知摘要不进入全文document。history projection按需/异步构建但cursor固定as-of；rebuild从revision/relations/material summary重建并对canonical hash。

父执行顺序会先合入不依赖search表的0056 collaboration，再合入0055 search。此时0051–0054与0056已冻结；任务外冲突只可顺延仍未发布的search/0057–0060，不能动0056。保留号可用时，当前migrator按migration name逐项记录并可应用较小编号的missing migration；0055必须同时验证普通0051–0054路径与已有0056 ledger路径，且不得改写0056。

## 2. Query/cursor

```go
type Query struct {
	Text string
	Scope Scope // current|archive|history
	Filters Filters
	Sort Sort
	Limit int
	Cursor string
}
```

首次请求固定upper bound/as-of watermark与normalized query digest。task 6 同时提供共享的 `internal/center/recordcursor` confidential codec：cursor内部含version/purpose/digest/auth scope hash/watermark/last rank+updated_at+record_id，但外层使用跨实例共享0400 keyring的AES-256-GCM随机nonce加密认证，并按固定长度桶padding后base64url编码；仅HMAC签名的可解码JSON、可比较的明文水位或在日志中展开token均禁止。namespace/purpose阻止search与activity token混用，TTL固定一小时；轮换先全员分发新decrypt key、验证membership digest，再切current，旧key保留到最后签发时间+TTL+2分钟skew。客户端只把token当不可比较字符串。cursor续页不能携其他query；SQL先通过recordauth可见scope候选，再trigram/tsvector rank、limit+1。

## 3. Web

`RecordsPage`是唯一controller/composition；private `pages/records/query`解析URL与draft filters。静态`/records/drafts`在dynamic record route前。筛选drawer复用Modal；宽表只有named local scroll。Sidebar一级“记录”在最终gate前可feature隐藏。

`GlobalSearch`新增record endpoint请求，与其他资产结果并发但有Abort/latest-request guard；显示最多5条record current summary和“在记录中心查看全部”。

## 4. 安全/状态

snippet由服务端从授权document生成并转义；禁止前端切全文。权限撤销使result移除且不泄露count。首次空库与query无结果不同；projector lag显示watermark/刷新而不是空。
