# VPS asset UX remediation

## Goal

把 VPS 资产录入、订阅管理、有效期延长和监控实例接入流程从“字段可提交”提升为高频资产运维可用的顺手体验。操作者应能快速录入 50+ VPS，少填重复字段，明确订阅/续费语义，从 VPS 详情直接创建并接入监控实例，并在弹窗成功后得到清晰收束。

## Requirements

- VPS 创建/编辑以访问入口为核心：IPv4 默认作为 SSH Host，只有用户显式展开时才填写不同 SSH Host；IPv6 默认关闭，开启后才输入。
- 国家/地区不再纯手输：提供常用国家/地区选择和自定义 fallback，保存仍兼容现有 `country` 字符串。
- 订阅创建/编辑使用内置常用币种和支付方式，并允许自定义字符串；币种保持 3 位代码，暂不做汇率换算。
- 订阅计费输入改为计费周期单位和长度；后端 contract 提供 `billing_period_unit`、`billing_period_length` 和兼容月化成本计算。
- 续费方式改为明确单选 `auto/manual/auto_cancelled/lottery/bonus/other`，并兼容旧 `auto_renew` / `auto_renew_cancelled` 语义。
- VPS 详情新增有效期延长动作，记录延长至日期、原因、费用、币种和来源类型；保存后自动更新当前 active 订阅续费日并写审计历史。
- VPS 详情新增“创建并接入监控实例”入口，从当前 VPS 自动预填监控实例字段，不再要求用户确认继承字段。
- 监控实例创建成功后进入监控详情并打开 agent 接入；从 VPS 发起时，接入完成的收束动作返回原 VPS 详情。
- agent 接入弹窗加宽，说明与手工回退默认折叠；生成命令后自动复制到剪贴板，并提供明确完成按钮。
- 共享 Modal 尺寸与表单收束行为治理：复杂表单不再瘦长，成功关闭/跳转，取消丢弃草稿，失败留在当前弹窗。

## Acceptance Criteria

- [ ] VPS 创建和编辑测试覆盖 IPv4 自动填充 SSH Host、不同 SSH Host 展开、IPv6 默认关闭/开启、自定义国家。
- [ ] 订阅 API 与前端测试覆盖新计费周期字段、续费方式推导、常用币种/支付方式和自定义值。
- [ ] 有效期延长 API 测试覆盖自动更新当前 active 订阅 `renew_at`、写入审计记录、无 active 订阅时报错。
- [ ] VPS 详情可从当前 VPS 创建并接入监控实例，创建 payload 继承 VPS 上下文，成功后进入 onboarding。
- [ ] agent 接入命令生成后尝试自动复制；成功/失败均有可见反馈；完成按钮按来源跳回 VPS 或留在监控实例。
- [ ] 相关弹窗在桌面和移动宽度下不出现明显无意义换行、内容溢出或成功后悬停不关闭。
- [ ] `go test ./...`、web lint/test/build、`git diff --check` 通过，或明确记录不可用原因。

## Notes

- Out of scope: 实时汇率换算、国家主数据管理后台、支付方式管理后台、全站无边界重设计。
