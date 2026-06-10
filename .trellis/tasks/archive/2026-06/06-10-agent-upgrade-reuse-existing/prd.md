# 复用已接入监控实例升级 agent

## Goal

当 VPS 已有 active 监控实例关联时，所有普通 agent 接入入口都复用现有监控实例进入升级/重新接入流程，避免误创建第二个监控实例并导致新老共存。

## Requirements

- VPS 详情页只有在没有 active 监控实例关联时才显示并执行“创建并接入 agent”。
- VPS 已有 1 个 active 监控实例时，VPS 详情页主入口和 `workbench=monitoring` 深链必须进入该监控实例的 onboarding/install-command 流程，不创建新监控实例。
- VPS 已有多个 active 监控实例时，不自动清理历史重复数据；页面必须提示人工核对，并保留每个实例的升级/重新接入与解除关联操作。
- 后端必须保护 `POST /api/vps/{vps_id}/monitoring-instances`，在已有 active link 时返回 409，不留下孤立 `monitoring_instances`。
- 后端必须保护普通 `POST /api/vps/{vps_id}/link-monitoring-instance`，在已有 active link 时返回 409，避免另一个入口继续制造重复 active link。
- 监控实例详情页保留既有 install-command/onboarding 合同；文案按绑定状态区分“接入 agent”和“升级/重新接入 agent”。

## Acceptance Criteria

- [ ] 无 active link 的 VPS 仍可从 VPS 详情创建监控实例，成功后跳转 `/monitoring/{id}?onboarding=1&return_vps={vps_id}`。
- [ ] 已有 1 个 active link 的 VPS 点击接入主入口或打开 `?workbench=monitoring` 时，只跳转现有监控实例 onboarding，不调用 create API。
- [ ] 已有多个 active links 的 VPS 不显示创建按钮，显示重复关联提示，并允许用户逐个进入升级/重新接入或解除关联。
- [ ] 后端 create/link 写路径在已有 active link 时返回 409，且 create 路径不会插入新监控实例。
- [ ] 历史重复 active links 不被迁移或自动解除。
- [ ] 相关 Go 与 Web 测试覆盖上述行为并通过。

## Notes

- 用户确认默认语义为“复用已有实例”；历史重复数据“不自动清理”。
- 本次不引入 agent 版本兼容矩阵或自动升级检测，只修正接入/升级入口语义与重复创建防线。
