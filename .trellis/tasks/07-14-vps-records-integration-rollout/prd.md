# 集成切换、安全、性能、备份恢复与终验

## Goal

在子任务 1–10 的领域能力全部合入后，把记录平台接成可部署、可备份、可验证恢复、永久删除不复活且可安全切换的完整产品；使用真实 PostgreSQL 16、local/MinIO Blob、scanner 与内容处理器完成跨存储演练，并以性能、安全、视觉、可访问性、staging 和发布链路的硬门禁决定是否开放默认入口。

## Dependencies and ownership

- 直接依赖父任务中的子任务 1–10，任何依赖未合入受保护主线时不得启动本任务。
- 启动前必须生成并验证无内容 child-delivery manifest：逐子任务记录task slug、merged PR number、source/merge commit、required-check run ID/conclusion、migration/adapter/spec digest；Git host证明PR已合入受保护`main`，且 `git merge-base --is-ancestor <merge_commit> HEAD` 对1–10全部返回0。缺失、仅本地commit、未合并PR、旧run或非ancestor都阻止启动，不能凭任务目录状态代替交付证据。
- 本任务计划拥有 `db/migrations/0060_record_platform_cutover.sql`。如果任务 1–10 已合入后主线另行占用 0060，本任务只为 cutover 选择下一个空闲编号并同步本子任务/父表引用；已经合入的 0051–0059 永远不改名、不重排、不改写。该迁移只保存版本化 feature/cutover 状态、兼容门禁和必要约束，不删除 `experience_logs`、legacy 映射、tombstone、账本或历史数据。
- `0058_create_record_portability.sql` 与 `0059_migrate_experience_logs_to_records.sql` 由“导入导出与 legacy 迁移”子任务拥有；独立 ledger、witness、recovery-control migration namespace 由平台基础子任务拥有。
- 本任务不重新发明记录、附件、证据、协作、搜索、活动或比较合同，而是通过显式constructor manifest注册并验证各子任务提供的 backup、restore、delete-replay、authorization、export 和 projection adapter。本任务只拥有registry validation、严格sequence、receipt、rebuild调用顺序与orchestration；领域表/Blob/workspace的删除、恢复和verify逻辑仍由owner package adapter拥有，缺合同必须回到对应owner的受保护PR修复，禁止在task11写跨包SQL或通用cleanup fallback。
- 父任务仍然只拥有任务图和跨子任务验收；本任务完成也不自动授权删除 legacy 表或跳过父任务最终复核。

## Requirements

### 1. 受支持的备份与恢复运行时

- 提供官方、非交互优先且支持结构化 JSON 输出的 backup/restore CLI；危险动作需要显式 deployment、manifest、target workspace 和确认参数，自动化不得依赖提示符解析。
- backup 必须先取得 deployment-wide backup lease与短时write-admission barrier，在应用数据库事务中原子登记唯一 backup/snapshot ID、`backup_epochs` marker、连续 `deletion_replay_state` 水位和当前正式/历史引用 pin；随后由PostgreSQL 16 `REPEATABLE READ` exporter transaction调用`pg_export_snapshot()`，同一snapshot枚举object refs并传给`pg_dump --snapshot`。pin digest与snapshot refs不一致即停止；export完成后才释放短时写barrier，exporter保持到dump导入/完成。数据库与Blob不得来自两个未绑定的读时点。
- 每个 attempt 在写第一字节前登记 workspace；数据库 dump、WAL/sidecar、Blob/object copy、multipart、manifest、签名和发布 partial 均可枚举、可取消且最终有 publish receipt 或 purge receipt。
- backup staging DB/object prefix、multipart与sidecar默认必须从应用backup、PITR/WAL、volume snapshot、S3 version/Object Lock和其他复制策略中可证明地排除。若平台不能证明排除，则在写第一字节前把该partial域注册为新的受管recovery source，签发inventory/manifest并纳入deletion replay；其`recoverable_until`不得晚于本次计划artifact，平台最短保留超过该上限时preflight失败且不得写字节。
- 发布条件是签名 `RecoveryPointManifest`、数据库 artifact hash、应用 schema、对象列表/catalog digest、同一快照的 deletion replay baseline、`backup_created_at` 与 `recoverable_until` 全部一致；“复制命令退出 0”不构成备份成功。
- local Blob 与 MinIO/S3 都必须通过同一 backup/restore conformance。S3 profile 使用真实 version/object hash；Object Lock/noncurrent version 被视为恢复介质并纳入库存和最长披露窗口，不冒充活动 Blob 已删除。
- 所有已声明受管恢复源——至少包含全量/逻辑数据库备份、PITR/base+WAL、卷/文件系统快照 sidecar、Blob/S3 catalog——先规范化为来源专属 `RecoveryPointManifest + replay_baseline`；缺少原子绑定、签名、完整 catalog 或 trust chain 时只能隔离取证，不能启动服务。
- 恢复先在独立 recovery-control store 创建隔离 workspace，任何 DB/Blob/WAL 字节落盘前完成签名、信任链、库存、空间、网络、备份排除、剩余恢复窗口和 processor 配置 preflight。
- restore DB/Blob/WAL/tmp/export/processor workspace必须证明不进入任何backup/PITR/snapshot/version/Object Lock/外发/索引策略；不能证明时，写第一字节前把整个derived workspace注册为签名受管recovery source，拥有inventory、manifest和deletion replay，且derived expiry不晚于source manifest。平台最短保留无法满足source expiry时直接拒绝，不得以“隔离中”替代注册或物理清理。
- 普通 workspace 的有效期遵循 `min(last_progress_at + 24h, created_at + 7d, source.recoverable_until)`，一小时 lease 只允许显式续租且永不越过 source expiry；forensic 转换需要备份管理员能力、原因、审批与访问审计，也不能延长 expiry。
- 恢复严格按父设计六步执行：验证 manifest；恢复 DB/Blob 且保持 HTTP/worker 关闭；从 fresh primary + full witness 取得连续账本；按 entry type 幂等重放；重建投影/缓存并验证全部引用；再次追平 fresh head 后才通过启动 gate。
- 恢复成功必须原子把 workspace 所有权转给新 deployment，不能同时遗留 staging 副本；失败、取消、超时、过期或 gate 拒绝均由可重试 janitor 物理清理 DB dir、Blob/object、WAL/tmp、导出和 processor workspace 并生成逐位置 receipt。

### 2. 删除重放与防复活

- 注册记录根、完整修订、草稿/恢复点、评论历史、行动项、关注/通知、附件、证据、搜索、活动、比较保存、导出、导入临时材料、legacy `experience_logs`、processor workspace 和 restore workspace 的版本化 replay adapter。
- `delete_commit` 必须清理被恢复对象的在线/legacy/投影/缓存/导出/导入/processor 副本并根据恢复后的全局引用执行 payload/blob GC；合法被其他记录引用的内容继续存在且权限不得扩大。
- 黑盒删除集成必须在JSON/附件/证据/preview/download/export流的headers前、headers后、首chunk与末chunk注入reservation，并覆盖活动import/processor/backup/restore workspace；ledger append前所有content lease/stream必须停止并出receipt，已经或可能发送字节的preview稳定失效并披露外部副本，不能把仍写socket的实例判为drained。
- `attempt_not_committed` 只重建终态投影、release epoch 和 reservation 解除，不创建 tombstone、不清内容、不占用对象删除唯一身份。
- `contract_activation` 必须重建 activation inventory、minimum fence-contract version 和 deployment membership/readiness/queue gate；不能当成空操作推进水位。
- 未知 entry type、object kind 或 contract version，账本断序/断链、primary 回滚、witness 不新鲜/不一致、尾部截断或 replay receipt 缺失时恢复与 records protected domain 均失败关闭。
- backup 后永久删除再 restore 时，被删除记录、legacy 原行、独占 Blob/payload、搜索/活动/导出以及导入/processor/restore 临时副本复活数必须为 0。
- 旧 import plan、重放中的迟到 worker、旧 backend/worker、cache warmer、projector 和外部通知重试均必须受 identity epoch、reservation epoch、minimum fence-contract version 与 fresh ledger head 拒绝，不能在切换或回滚中复活内容。
- 启用永久删除前生成 witness 确认的 genesis/activation entry 与签名 activation manifest，盘点所有仍在保留窗口内的旧备份；未知旧介质或 activation 后缺 watermark/signature 的介质不属于受支持恢复。
- 终态保留使用fake clock分别越过24小时lease/guard/member窗口和30天receipt/job/telemetry窗口；除ledger/full witness、最小audit、origin tombstone、当前recovery inventory/trust与当前deployment allowlist外，正文和stable record/object ID命中必须为0。`attempt_not_committed`只保留无内容outcome且不是tombstone。

### 3. 真实跨进程集成环境

- 测试栈使用 PostgreSQL 16 的应用库、primary ledger、full witness、recovery-control 四个独立服务/卷/凭据，并证明应用备份不包含三个独立恢复域。
- MinIO profile 必须实际创建 versioned/Object Lock bucket，并验证 retention/legal-hold、条件写、multipart 中断、noncurrent version 和 hash/catalog；local profile 使用独立持久目录、fsync/atomic semantics。
- ClamAV、Poppler/Chromium 内容处理器及 workspace janitor 在真实进程中运行；required scanner 缺失、过期或失败时压缩包保持隔离或拒绝，不能由测试 fake 掩盖。
- 当前 center 实际装配的 5 个 worker 是 baseline。每加入一个 worker 都必须显式构造、命名、测试 lifecycle/readiness，并更新准确 worker count；不得用不可审计的 service locator 或后台 goroutine 偷跑。
- CI 至少包含 fresh install、从 0050 upgrade、迁移重复执行、真实 PostgreSQL/MinIO/processor、删除重放、恢复演练和视觉浏览器任务。0050 upgrade输入必须由不可变0.59.0 release source上的真实migration/app migrator在PostgreSQL 16生成schema-only dump并记录tag/commit/migration hashes/schema hash；CI每次重新生成并byte-compare，禁止手写或人工维护漂移fixture。新增内部 job 必须被远端当前 required contexts `go` / `web` / `web-browser` / `docker-image` 的终态聚合图实际覆盖，或由仓库 owner 显式更新并复核 branch protection，不能只创建一个非 required 的绿色 job。失败诊断不得上传记录内容、凭据、对象路径或预签 URL。

### 4. 安全与遥测

- 建立受管 telemetry inventory，覆盖 HTTP/worker/logger/trace/APM/error reporter、ingress/LB/CDN/object access、PostgreSQL/proxy/audit/slow-query、backup/restore、浏览器 collector 和本地/远端 processor；每个 sink 记录 owner、配置 hash、位置、归档/备份和不超过 30 天的最长内容域保留期。
- 未知 sink、无法读取实际配置、无法证明 allowlist 或无法核验不超过30天TTL时，永久删除 capability始终失败关闭，直到active deployment inventory与live config digest通过。把sink登记为recovery source只能补充副本库存，绝不能豁免telemetry的30天硬上限。
- 全部 sink 永久禁止请求/响应 body、raw URL/query/header、SQL bind、Markdown、搜索词、标题/摘要、评论/行动项文本、文件名、证据 payload/摘要、外部通知内容、导出片段/路径、可读 storage key、预签 URL、DOM/input、浏览器缓冲和 stable record/object ID。
- 允许的普通观测字段仅限 route/prefix template、短期 correlation/request ID、删除 operation ID、规范化 SQL fingerprint、状态、字节数、耗时、reason/error code 和无内容计数；最小永久删除审计遵循父设计的去内容合同。
- PostgreSQL/代理固定禁用 statement values/error parameter，ingress/object 日志不记录 raw path/query，browser collector 不采集 DOM/表单/网络 body；processor stdout/stderr 和第三方 SDK breadcrumb 使用同一 allowlist/redaction。
- 使用包含秘密、中文/英文 Markdown、文件名、证据、评论、搜索词和稳定 ID 的 corpus 穿过成功/失败/超时/重试路径，inventory 中每个在线 sink、归档、日志备份和 core dump 命中数必须为 0。
- 使用fake clock推进31天，实际sink及其归档/日志备份中的request/correlation/deletion operation ID残留必须为0；长期只允许无法反查对象的route/status/time-bucket聚合。example inventory只验证schema，不能作为staging/production gate证据。
- 覆盖 IDOR、Markdown XSS、SSRF、MIME/签名欺骗、zip-slip/symlink/hardlink/压缩炸弹、manifest swap/签名/expiry/watermark 篡改、旧 plan 重放、下载/导出越权与故障诊断泄露。

### 5. 性能、容量与可复现测量

- 生成固定 seed/hash：10,000 条当前记录、200,000 个完整修订、1,000,000 条跨来源活动；每记录中位 3 个证据/2 个附件并含 5 MiB 最大证据 fixture，覆盖中英文、代码 token、已归档、受限、来源删除与部分覆盖。
- 参考环境固定为父设计规定的 Linux/CPU/内存/PostgreSQL/NVMe/RTT/local 与 MinIO profile；报告记录硬件、内核、容器限制、全部服务版本、配置 hash、commit、schema、seed hash。环境不符的结果只能列为附录。
- 每轮从干净 seed restore 后 `ANALYZE`，预热 5 分钟，固定到达率测量 15 分钟，独立重复 3 轮且每轮都过线；保存 p50/p95/p99、错误率、资源/连接/IO、SQL fingerprint 和代表性 `EXPLAIN (ANALYZE, BUFFERS)`。
- VPS 概览 p95 ≤750ms；搜索首25条与时间线首50条 p95≤1s；草稿 p95≤500ms；纯文字正式修订 p95≤1s；comparison candidate/summary p95≤1s、6×2,000 detail p95≤2s；最大允许证据 preview/capture p95≤10s 并有进行状态。
- 读写混合负载必须同时包含 overview/search/timeline/draft/revision、comparison summary 2rps/detail 0.2rps和evidence 0.2rps，覆盖均匀与80/20热点分布；overview/search/timeline每轮成功样本≥5,000，draft/revision/comparison summary≥1,500，comparison detail/evidence每profile≥150。非预期错误率为0，预期409/404/429单列而不从分母移除。
- 容量门同时覆盖 comparison单请求96MiB与aggregate 512MiB admission、Blob/证据/附件配额、processor/import/restore workspace、outbox/projector积压、孤立对象、backup partial和janitor收敛。设计必须给每类写明阈值、稳定reason code、最大drain time和可执行命令；不得以放宽Web/DB/cgroup预算掩盖回归。

### 6. 完整视觉、可访问性与理解测试

- 以 `vps-records-visual-contract/v1` 为唯一视觉布局合同，为六个表面建立桌面和 390px 语义/几何基线：VPS 概览、记录中心、单主体时间线、比较工作台、Markdown 编辑器、证据选择器。正式门禁沿用仓库 Playwright Chromium 的 DOM、状态、焦点、几何、overflow、Axe 与 diagnostics 合同，不引入 tracked pixel golden、screenshot manifest 或批量 raster 截图。
- 每个表面具备适用的初始加载、首次空态、查询无结果、局部失败、提交/后台处理、权限撤销/永久删除 fixture；VPS 另覆盖稳定/异常两态，证据源/processor/ledger 降级必须与业务空态区分。
- 稳定/无异常 VPS 概览以及正常记录/行动项不能渲染异常标题、动作、禁用按钮、空容器、隐藏占高或“无异常/无逾期”占位；异常事实存在时才插入事实、影响、证据新鲜度和必要入口，恢复后从 DOM 与布局移除。
- 所有可点击内容静止态可识别，状态使用文字+形状/图标+颜色；Axe critical/serious 为 0，核心流程纯键盘完成，焦点顺序符合视觉合同，modal/drawer 关闭后恢复触发器，动态异常不抢焦点。
- 390px 进行语义裁剪/重排而非桌面逐项堆叠；筛选和材料进入 drawer，比较一次聚焦一个 kind/指标，命名滚动区保留 sticky 行标题，所有触摸目标至少 44px，无未授权横向页面溢出。
- 使用至少20名未参与本项目需求规划、视觉/技术设计、产品或代码实现、代码审查、测试/fixture实现、Trellis/Codex规划复核的目标参与者执行30秒理解测试，至少10名从未使用候风，其余近6个月使用不超过3次；稳定/异常、桌面/390px反平衡，至少18/20在30秒内正确说出身份、风险、最近变化和下一入口。Playwright或staging浏览器cue检查只验证结构/可操作性，不能冒充、替代或计入理解测试。
- 本地/短期人工视觉评审证据、Axe、键盘/focus/44px、理解测试报告必须来自同一 commit 与固定 fixture；任何设计顺序或异常占位差异先回到设计评审，不能在样式实现中自行变更合同。短期截图只按现有脱敏/保留规则使用，不进入仓库正式基线。

### 7. Staging、切换、回退与发布

- 所有能力先以服务端 capability/feature gate 关闭合入；前端 flag 只控制发现性，不能替代服务端授权、fence、版本或 readiness gate。
- staging 先以 shadow read 对照新/旧数据，legacy 写仍为唯一写入口；`0059` dry-run/repeated apply 对账差异为 0 后，按“新写启用 → legacy 只读兼容 → 新读/路由默认”分阶段切换，绝不双写。
- `0060` 记录每个 deployment/project 的 cutover phase、合同版本、actor、时间与原因，并使用 CAS/幂等更新；任何阶段只允许在前置门禁 receipt 完整时前进，回退不能降低 minimum fence-contract version 或绕过 deletion replay。
- 默认入口开放前，staging 必须验证 0.59.0 以来的 VPS、监控、IP 质量、订阅预算和命令审计路径，新记录能固化来源快照且源归档/删除后仍按权限读取；未来路由/性能 kind 只需遵循已验证 registry conformance，不在本任务虚构数据。
- 回退 UI/路由只能回到 fence-aware compatibility API；账本 activation 后，低于 minimum fence-contract version 的 backend/worker 不得接收流量或队列，不能回滚到能读取/复活已删内容的 binary。
- `experience_logs`、legacy mapping 与 tombstone 默认保留；旧备份窗口到期、replay adapter 兼容矩阵通过且用户另行批准前，不执行表删除或内容清理。
- 交付顺序固定为：非main分支完成本地/重型门 → PR required checks → 合并 → main CI → Release Please/release与image publish → 拉取发布digest并分别smoke center/content-processor/backup/restore/rollout五个binary → 以全部records flags=`off`部署该精确digest → 从运行实例验证commit/version/digest/capability off → rollout preflight → shadow/cutover/rollback/soak → 完成。当前认证staging workflow只接受`refs/heads/main`和已部署的精确release version；receipt绑定commit、version、image digest与config。任一staging修复都必须回到非main分支走新的受保护PR/main CI/release/image/deploy全链，旧receipt全部失效，禁止现场热补或用未发布branch image继续切换。
- staging audit 对资源 path 使用 route template（例如 `/vps/:vpsId`、`/records/:recordId`），不得把 path segment、query value、标题、摘要、内部 ID 或主内容截图写入 artifact。records 真实路由不保留截图；现有显式截图也必须遵循仓库 allowlist/mask，并在 upload 前与 Playwright failure report、staging-audit、integration/recovery artifact 一起通过 content/stable-ID leak scan。
- staging credential、cookie、Authorization、真实资产内容、内部 ID 和截图敏感字段不得进入仓库、日志、CI artifact 或最终报告。

## Constraints and explicit exclusions

- 完整交付，不以 MVP、后续补安全、仅 local 成功、mock 通过或手工一次性脚本缩减范围。
- 不把 deletion ledger、full witness 或 recovery trust store 放回应用 PostgreSQL/同一卷/同一恢复凭据域。
- 不宣称候风能召回下载、用户外部归档、已投递外部消息、浏览器手工保存或未联网设备缓冲；完成文案必须披露受管备份窗口与外部副本边界。
- 不建设逐记录 KMS/Vault 密钥擦除、匿名分享、通用聊天/项目管理、跨证据总分、自动业务执行或命令输出长期归档。
- 正式 Web 门禁必须使用仓库 `.node-version` 的 Node 22.x；当前 shell 的 Node 24.18.0 结果不能作为验收证据。
- 不执行 down migration，不修改已发布 migration，不直接修改本地或远端 main/master。

## Acceptance Criteria

- [ ] 子任务1–10均有merged PR/check-run/digest receipt，Git host状态与`git merge-base --is-ancestor`证明每个merge commit均在当前主线祖先中；本任务从该验证后的最新主线建立非main分支。
- [ ] `0060_record_platform_cutover.sql` fresh、0050 upgrade、repeated apply 与并发 CAS 测试通过，且不删除 legacy/ledger/tombstone。
- [ ] backup/restore CLI 的 plan/run/status/verify/cancel/destroy 契约、JSON schema、幂等、exit code 和危险参数测试通过。
- [ ] local 与 MinIO/Object Lock 两个 profile 均生成可验证 signed manifest，正式/历史 DB 引用的 Blob hash 通过率 100%。
- [ ] backup staging与restore workspace在“可证明排除”和“无法排除而预注册derived source”两条路径均通过；derived expiry不晚于source/planned artifact，保留策略超上限时写入字节数为0。
- [ ] PostgreSQL 16 四数据库隔离得到自动测试证明，应用 backup 中独立 ledger/witness/recovery-control 内容命中为 0。
- [ ] 0050 upgrade fixture由不可变0.59.0 release migration/app migrator在PostgreSQL16 fresh生成，provenance与schema hash校验通过；CI regenerate diff为0，手工编辑检测稳定失败。
- [ ] restore 六步 gate、双 head 追平、deployment ownership transfer 与启动关闭合同在真实进程环境通过。
- [ ] backup/restore 每个故障 cutpoint 强杀后，未发布 partial 和未转 forensic 的失败 workspace 最终残留为 0，且每个 attempt 有 publish/transfer 或逐位置 purge receipt。
- [ ] backup 后删除再恢复，被删记录及其 legacy/独占 Blob/投影/导出/临时副本复活数为 0。
- [ ] backup后分别永久删除VPS、MonitoringInstance和Target再恢复：来源保持不存在、live link与名称自动重连数为0，存续记录/证据仍按final authorization floor可读并显示tombstone。
- [ ] ledger gap、断链、尾部截断、primary 回滚、witness 不新鲜/不一致、manifest swap、watermark/expiry/signature 篡改、unknown adapter 的错误启动放行数为 0。
- [ ] backup/trust key两阶段轮换、旧manifest恢复、compromised key拒绝、主trust store+checkpoint同时丢失后从full witness重建，以及full/PITR/WAL/snapshot/local/S3 noncurrent/Object-Lock介质到期销毁/无内容receipt全部通过。
- [ ] reservation在headers前后/首末chunk及活动export/import/processor/restore workspace的黑盒并发测试无未披露外发或误判drain；fake clock越过24h/30d后终态allowlist之外正文/stable ID命中为0。
- [ ] `attempt_not_committed` 与 `contract_activation` 按各自合同重放，连续水位不跳号；旧 binary/worker 流量和任务接纳数为 0。
- [ ] telemetry inventory 完整，秘密/内容/stable-ID corpus 在每个 sink、归档、日志备份和 core dump 命中数为 0，内容域 TTL 不超过 30 天。
- [ ] active deployment从operator-supplied inventory枚举全部实际sink并绑定live config digest；31天fake-clock后request/correlation/operation ID残留为0，只有不可逆聚合存续，example文件未被当成运行证据。
- [ ] PostgreSQL、MinIO、scanner、processor、浏览器、import/export 和恢复安全测试覆盖所有明确威胁，未出现已知越权、XSS、SSRF、主动内容或诊断 artifact 泄露。
- [ ] 固定 seed/hash 可由单一命令重建，三轮 local 与 MinIO benchmark 均满足全部 p95、样本量和非预期错误率门槛。
- [ ] capacity budget表全部通过：comparison单/aggregate/admission/drain、队列、workspace、quota、partial/orphan均在对应阈值和最大drain time内；报告来自指定4vCPU/4GiB self-hosted runner与授权benchmark operator，环境或operator attestation缺失时门禁保持失败。
- [ ] 六表面桌面/390px 语义/几何基线和全部适用状态 fixture 固定在同一 commit；无 tracked pixel golden 或批量截图，稳定态 DOM/布局异常占位命中为 0。
- [ ] Axe critical/serious 为 0，核心流程纯键盘通过，focus restore、44px、reduced motion 与页面横向溢出门通过。
- [ ] 30 秒理解测试至少 18/20 成功，协议、匿名 CSV/摘要和排除理由可复核且不含敏感数据。
- [ ] 理解参与者与项目规划/设计/实现/审查/测试实现无交集；自动化browser cue/staging smoke结果未计入20人样本或18/20成功数。
- [ ] `make verify-go`、Node 22 的 `make verify-web`、完整 Playwright、真实集成/恢复/性能/安全任务与 `git diff --check` fresh 通过。
- [ ] staging 完成 shadow read、legacy 对账、分阶段写/读切换、正常/异常/降级/撤权/源删除/移动端回归和回退演练。
- [ ] UI/路由回退没有降低 fence 合同或复活内容；`experience_logs` 与 tombstone 仍保留。
- [ ] required CI、合并后 main CI、Release Please、release、镜像发布和已发布镜像版本/架构/health 验证均有可追溯证据。
- [ ] 发布镜像在隔离环境分别完成center、content processor、backup、restore、rollout五binary smoke；随后以全部records flags=off部署同一精确digest并核对运行commit/version/digest，之后才产生任何rollout preflight或staging phase receipt。

## Open Questions

无。产品语义、完整范围、删除与备份边界、迁移所有权、视觉合同和发布门禁均已在父任务中确认；实施时若 0060 被任务外主线变化占用，只顺延尚未合入的 cutover migration并同步本任务/父表，绝不修改任务 1–10 已合入的 0051–0059。
