# G2 剩余执行路线

本页安排持久删除与恢复的剩余工作；行为以[删除合同](../spec/contracts/records/deletion-recovery.md)、[集成与恢复合同](../spec/contracts/records/integration-recovery.md)、[授权合同](../spec/contracts/records/authorization.md)及其引用的现行规范为准。原编号用于追溯，不表示各项均须从头实现。

当前候选是 `feat/g2-durable-deletion-integrated` 的完整工作区，**G2 NOT_READY，永久删除 HTTP CLOSED**。受限历史 V1 九 adapter continuation/portability 与 N ABI V5 有界诊断及失败 readback 已阶段接受并交付，详见 [PR #635](https://github.com/xiangnan0811/houfeng/pull/635) 和[固定修订](https://github.com/xiangnan0811/houfeng/tree/ad460e207bf6d2f7b58e6e08a7c2389b8454ee70)。这些切片不代替整体接受。

源码入口为 `scripts/g2/source/`、`scripts/g2/phase0/` 和 `scripts/g2/http_smoke/`；`phase0` 是固定输入。历史运行资产及私有归档是证据，不是 clean checkout 的运行依赖。新 VM 或 source 运行需要新增明确授权；两次已有 exact 授权均已消耗。

## 阶段合并与后继迁移

本阶段候选尚待合并；最终合并 C81 以实际候选为准。合并前普通 Compose 所需的 `0081` schema/ACL 修订须同步 compiler 与独立 oracle 并验证，`0001`–`0080` 原字节不变。阶段合并一旦发生，最终合并修订中的 `0081`/C81 即冻结为不可回写的前驱。此后需要的 schema 或 ACL 变化须追加迁移：先检查届时 `db/migrations/` 的实际最大编号，使用其后的下一个未占用序号；`0082` 仅是没有并发新增时的例子。

后继 compiler 改动前，须从精确的最终合并 C81 修订及 pristine C81 PostgreSQL 独立取得 canonical migration/privilege bodies 和 catalog 事实，记录修订、数据库及角色绑定、body 长度和 hash；以这些独立事实注册 C81 predecessor profile、精确后继 fragment/权限差量与 hardening、完整支持的链及 suffix 和独立 oracle。scoped writer 保持单个 `SERIALIZABLE` 事务整体提交或回滚，runtime 只读准入。后继验收覆盖 fresh/successor、C81 升级、全部声明的链、自定义角色、历史字节、repeat、升级前拒绝/后准入、漂移拒绝及失败回滚；细则见 [APP 当前迁移合同](../spec/contracts/platform/app-acl-current.md)。

后继策略本身不要求提前新增 P81 或后继 golden；当前为 82 sources、30 fragments、18 profiles、196607 chains。合并及这些后继前置均不证明 G2、source 或 K 已闭合，也不开放永久删除 HTTP。

## Native 归因与完整矩阵

先使用已保存失败取证归因 UID SETTLE：selection146/build150 为 **0 PASS / 1 FAIL / 50 NOT_RUN**，已观察到 `RELEASE_RETURN`，等待 `OBJECT_REMOVAL`。取证 `COMPLETE` 只说明失败证据完整，不是场景 PASS；delivery/timeliness 为 `UNKNOWN`，holder 为 `UNRESOLVED`。有界诊断组件及 readback 的阶段接受不能证明 stall 已解决。

取得新运行授权并解决归因后，实际接受 registration/WATCH 顺序、held launcher→target、两个 owner probe 的单次只读 bind 与 retired grants、后续 realm registration，以及 pinned-runc OCI 和 broker-kill。按实际合同完成全 51 场景，最终须 **51 PASS / 0 FAIL / 0 NOT_RUN**；只复用输入与候选未变且覆盖相同的有效证据。

## M1：签发、客户端与 Comparison

对应 **G2-01/05/06、D1/D2**。已有授权、C81、客户端和 comparison 局部实现；下一步通过真实 source-bound issuer 与受限角色闭合三票，而非以 subset、prepare-gate 旁路或声明代替九 adapter 注册和 receipt。真实权限及二次确认须绑定 project、record、lifecycle/version、auth epoch 和 fence；错误 issuer/worker、过期 operation 拒绝，root-independent gate 持久，租约到期只允许接管。验证 restore/edit 两锁序、fresh 与 predecessor/successor admission、漂移拒绝、失败回滚、exact replay、重启读回和冻结资产保护。

客户端以真实 event/target/receipt 和效果原子提交，root 消失后仍可送达。两 context/两 tab 验证在线关闭、丢 ack 重放、IDB abort 不 ack、迟到 save/unmount/切换拒绝、logout 清当前用户而 401 不删、其他用户/记录保留，以及启动 TTL 和重连 reconcile；离线副本为 unknown/null，不承诺擦除远端设备。无权 record 不可枚举。

Comparison 验证含 metadata-only 输入的真实 A/B、历史 canonical 校验与补索引，损坏或未认证时阻塞；A purge 后 B 的 payload、refs、lineage 和正文 hash 保持，其他 owner 的 distinct 结果精确计数且不泄露身份。浏览器成功或丢响应均不得重复创建 B，A 失效后的旧 save 不得发布或自动改写 B。

客户端清理及 Comparison 交互见 [Records Web 合同](../spec/contracts/records/web-surfaces.md)与[证据快照合同](../spec/contracts/evidence-snapshot.md)；比较库存与保留副本预览见[删除合同对应条款](../spec/contracts/records/deletion-recovery.md#comparison-只读库存与保留副本预览)。

## 全 writer 与九 adapter 的物理闭合

对应 **G2-02/03/04、D3–D6、P1–P5**；依赖 M1/source 准入。已有九 adapter continuation、local I/O 和 S3 exact-version GC 等切片，仍须核对完整候选的真实接线和接受，不把历史切片当作整票完成，也不从头重迁既有 adapter。

物理 I/O、能力关闭和 GC 按[交付原语](../spec/contracts/platform/delivery-primitives.md)；外发与 artifact 的具体责任按[协作通知](../spec/contracts/records/collaboration-notifications.md)和[可移植性](../spec/contracts/records/portability.md)。

- 覆盖 revision、draft、upload/promotion、processor、copy、evidence、search/activity、collaboration/outbox、import/export/PDF 的真实 writer。gate 前提交纳入清理，gate 后不发布或复活；仅允许原 obligation 登记迟到事实。验证统一 rank、反序 batch、expiry、source/target 与 import/GC 两顺序、重读变化后的 rollback/reselect，并保留其他记录和 origin tombstone。
- 每个 destructive 事务核 current owner/generation/expiry/fence，effects、absence、receipt 原子提交或回滚；错误角色、失权旧 owner 零越权效果，接管重放稳定且 quota 只结算一次。真实 claim/guard 保护共享对象、其他 record、checkpoint 与 tombstone；root-dependent 责任完成或真实移交后 core 最后删除，缺 adapter 或未结义务 fail closed。验证 edit/upload/publication 与 purge、trash restore 与 purge 的两种锁序及中断接管。
- 每次 I/O 有 durable obligation；local 使用 namespace/incarnation、持久 nonce、跨进程 key guard、keywide 核对和封口。root 已无后仍能重启接管、精确 cleanup 和一次 quota；阻塞 Put 保持 pending，迟到 version 只绑定原义务。取消、超时、lease/URL 到期或单次 absence 不证明远端关闭。
- D3/D4：按实际 provider 合同验证慢 body PUT 跨 gate/expiry、断连及晚完成；provider 接受后 kill/丢响应和 root-absent 恢复须找回物化身份。旧能力无法证明时拒绝受影响记录准入并保留补证或迁移前置；未知 provider closure 保持 pending，MinIO 样本不能代证。
- D5：验证 prepared/unbound shared 与 bound noncurrent 交错时 refs/pins/bytes 保留；旧版本删除须精确授权，普通 publication 只处理 current。跨进程封口和 root-absent 接管另证，不借 GC 扩权。
- D6：闭合 processing/unknown/delivered 去重、root-independent finalize 与旧 export/import/PDF artifact 身份转换；验证 send 前 purge 后迟到 finalize、PUT 成功丢响应后重启。已发送或下载副本不可撤回，未知不重发或假报完成。

## 生命周期、自动策略与容量

对应 **D7/Q1**；下列已定产品条件尚未全部进入 spec，实施时须同步合同及验证。归档记录先恢复 active 才能移入 trash；trash 可恢复 active；active 或 trashed 经二次确认可立即 purge，archived 不可直接 purge。验证首次请求、同 key 同 body 重放、异 body 拒绝、新 key 同状态冲突、丢响应及 restore 后旧 purge；旧请求重放不撤销后续状态、不刷新 `trashed_at`，保留既有 archive 语义。

完成生命周期 UI、可信 policy issuer 和自动过期/容量淘汰。issuer 绑定规则与候选，selection→gate 时重核权限、最老顺序和引用；默认 30 天/1 GB，可配置，每次重新计算并淘汰最老整记录。独立 oracle 复算全部保留正式 revision 正文、附件及预览、canonical evidence；trash 内共享对象去重，逻辑占用与物理释放分开报告。覆盖并发、零释放、共享和权限变化。

## K：正向物理恢复

现无完整 source recovery coordinator/CLI。下一步设计并实际证明可信 stopped-writer/restore-unit producer、同 generation APP/W2、旧凭据与 namespace 撤销、W2 inventory/prepare、真实 filesystem identity/handoff、root-independent 义务、seal→activation producer/证据以及失败闭环；遵循[集成恢复](../spec/contracts/records/integration-recovery.md)和 [Compose authority](../spec/contracts/platform/compose-authority.md)。未来 K 的具体协议、CLI envelope 和正向 driver 须先有决策完整的原生 Plan 并获批准。通用 local/S3 recovery 不能关闭 K，K 也不是全部备份恢复能力。

## J：联合接受、隔离 smoke 与 OLD

相关 source、M1、全 writer、产品和 K 前置闭合后，冻结完整源码、产品、工具链、配置与输入，在同一候选上联合接受；冻结后不并发写产品。验证 same APP/RID 多 generation、C81 fresh/dual carriers/systemd、三 normal/recovery、source handoff/V1 reconcile、birth/namespace/跨 RID、root-absent/late I/O/quota-once，以及 D1–D7；未知真实 provider 义务须独立关闭。包含 Begin-only/ready disposition、C80 A retirement、W1 common head、C80 B Preview/Execute、join/refence、C81 convergence 和 C81 A refusal/B mutation。缺失、skip、失败或未知不能记为通过。

隔离 smoke 须构建并运行 center、processor、record-platform-admin 和 deletion-smoke 四个 binary，使用真实 UID 与受限角色；实际观察 HTTP 上传→processor→下载/import/export、中断重启、stream 拒绝与排空、九 adapter core-last、共享内容保留、独占及临时数据清理与 fsync、quota once、终态重放，以及两个永久删除 endpoint 的真实 503。该 smoke 不开放生产 HTTP。

OLD 的 **51 项未证明等价**不是 51 个已确认缺陷。J 前逐项对照当前 caller、测试和 spec，证明覆盖，或补回缺失行为/测试，或经合同修订处置；不能以名称或行匹配代证。[固定修订中的 OLD 文件范围](https://github.com/xiangnan0811/houfeng/blob/ad460e207bf6d2f7b58e6e08a7c2389b8454ee70/docs/design/g2-plan.md#old-工作区未解差异)保留逐文件可发现性；原内容及索引另有私有归档，不是 clean checkout 运行依赖。
