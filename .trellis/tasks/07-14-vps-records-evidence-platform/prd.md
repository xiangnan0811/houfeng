# 证据注册表与首批适配器

## Goal

实现证据 kind 注册表与 IP 质量、监控、事件、订阅预算、命令审计首批不可变快照适配器。

## 2026-08-02 Development Rebaseline

本任务拥有 `0054_create_record_evidence.sql`，只支持 fresh/current development database 与 exact repeat。`0054` 的 managed objects/privileges/admission tests 必须与 migration 同一交付；不提供旧库 upgrade、legacy evidence backfill 或 release cutover。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §8、§12、§15、§20、§25。
- 直接依赖：子任务 1、2 已合入 main；不依赖附件 Blob，结构化 payload 使用独立 content-addressed PostgreSQL payload 表。
- 建立版本化 evidence kind registry；每个 kind 必须实现 validate/preview/capture/redact/summarize/compare/export/renderer contract 和 conformance suite。
- 最小 envelope 固化 subject/source identity snapshot、requested/actual/observed/captured/referenced 四类时间、source revision/watermark、producer/calculation version、单位/质量/敏感等级、canonical hash和体积。
- preview/capture intent 有15分钟有效期；正式保存重读源事实并逐字段校验，覆盖/桶数/质量/schema/敏感结果/体积漂移返回 409。
- 首批 kind 必须严格对齐父设计 §12.2：`ip_quality.report/v1`、`monitoring.host/v1`、`monitoring.probe/v2`、`monitoring.event/v2`、`subscription.cost/v1`、`command.audit/v1`；资产历史（续费/价格/IP/规格）作为权威 source/activity adapter 覆盖，不在本任务擅自新增 `asset.history/*` registry kind。未来 route/performance 必须只通过 registry 接入。
- 监控证据使用绝对窗口与实际覆盖，保存样本数/缺口/维护/补传/最值/分位/有界峰值，不直接复制会补零且缺覆盖的 sparkline/runtime响应。
- IP stale policy、成本汇率/日期/基准货币、事件修正/回填、命令 output 24h语义均在快照中显式固化；stdout/stderr 永久禁止。
- 普通/敏感拓扑/永久禁止三级分类由服务端 schema决定；客户端改名不能绕过。
- evidence随 record revision长期保留；逻辑快照权限不因payload dedupe合并，来源删除只断live link。
- 提供 capture/read API、revision participant、deletion/export接口及Web allowlisted renderer registry；不提供任意JSON renderer。

## Acceptance Criteria

- [ ] `0054` fresh/repeat migration 与 current APP ACL/convergence/runtime admission 通过。
- [ ] 每个kind通过最小envelope、禁止字段、确定性canonicalization、大小、未知版本、复制/删除/权限、导入导出和比较compatibility tests。
- [ ] Preview DTO与最终snapshot逐字段一致；补传/retention/权限/计算版本漂移不会静默保存。
- [ ] 30天监控窗口、无完整覆盖、截断、缺口、维护与补传在快照和UI中可辨，趋势不补零/外推。
- [ ] IP、成本、事件、命令快照在源历史归档/删除后仍按记录授权可读并显示source unavailable。
- [ ] secret/command-output/raw-JSON corpus经所有adapter后永久禁止字段命中为0；敏感拓扑默认关闭并有预览。
- [ ] 同payload跨记录复制产生新logical snapshot与独立auth/audit；删除来源记录不删除已显式复制的其他快照。
- [ ] capture与record revision事务要么全部成功，要么只有可回收孤立payload；没有半份revision引用。
- [ ] 本实例权威库中出现unknown kind/schema时registry/readiness与普通读取失败关闭，不创建/复制/比较/导出且不调用通用JSON渲染；外部never-supported schema的安全envelope metadata只属于task10 integrity-valid quarantine/dry-run，不进入本任务普通证据UI。
- [ ] focused Go/Web、真实PostgreSQL、determinism/fuzz、`make verify-go`/Node22 `make verify-web`通过。

## Out of Scope

- 不长期保存命令stdout/stderr或任意用户JSON。
- 不在本任务交付横向比较页面；只实现kind compare contract，子任务8编排。
- 不实现未来路由/性能kind，但 conformance必须保证可扩展。

## Execution Gate

- 保持 planning；依赖、规划材料和用户执行授权完成后才 start。当前 Trellis 使用 native/auto 子智能体 workflow；production evidence capture/save 在 Child 10 提供 AdmissionGate 前必须 fail closed。
