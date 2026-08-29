# 兼容既有 NPM 网络与 host 模式

## Goal

让 Houfeng 适配用户已经部署的 Nginx Proxy Manager（NPM），而不是要求用户为了安装 Houfeng 重建或改变现有 NPM 网络拓扑；同时保留默认不向公网发布 Center 端口的安全边界。

## Background

- 当前生产 `compose.yaml` 把 `HOUFENG_PROXY_NETWORK` 作为无条件必填值，让 `houfeng` 加入该 external network 并使用稳定别名 `houfeng`；默认不发布宿主机端口。
- 对 bridge-mode NPM，正确责任方向是 Houfeng 加入 NPM 已使用的现有 Docker 网络，NPM Compose 无需因 Houfeng 改造。
- `network_mode: host` 的 NPM 没有可供 Houfeng 加入并使用 Docker DNS 的用户自定义 bridge 网络；`HOUFENG_PROXY_NETWORK=host` 也不能表达所需行为，因为 Center 仍需通过私有 Compose 网络访问 PostgreSQL 和 ClamAV。
- Docker 官方记录：Engine 28.0.0 以前，发布到 localhost 的容器端口仍可能被同一 L2 网络内的其他主机访问。

## Requirements

### R1. Explicit proxy modes

- 生产部署由公共基础 `compose.yaml`、`compose.proxy-network.yaml` 和 `compose.proxy-host.yaml` 组成。
- `.env` 通过 `COMPOSE_FILE` 显式选择一个代理模式，普通 `docker compose config/pull/up` 命令保持不变。
- shared-network 模式是 env 模板的默认选择；不得同时加载两个模式文件。

### R2. Existing bridge-network NPM

- `compose.proxy-network.yaml` 独占 external network 声明、`HOUFENG_PROXY_NETWORK` 必填检查和稳定别名 `houfeng`。
- 用户填写 NPM 已加入的实际 Docker 网络名；Houfeng 主动加入该网络，不要求创建 Houfeng 专属代理网络或修改 NPM Compose。
- 该模式不得发布任何 Center 宿主机端口；NPM upstream 保持 `houfeng:16001`。

### R3. Existing host-network NPM

- `compose.proxy-host.yaml` 不声明、引用或要求 `HOUFENG_PROXY_NETWORK`。
- 该模式只把 Center TCP 16001 发布到宿主机 IPv4 loopback `127.0.0.1:16001`；不得绑定 `0.0.0.0`、`::`、公网或局域网地址。
- Center 继续加入 Houfeng 私有 default network，以访问 PostgreSQL 和 ClamAV；NPM upstream 使用 `127.0.0.1:16001`。
- host-proxy 模式最低支持 Docker Engine 28.0.0；不为旧版本设计或承诺宿主机防火墙补偿方案。

### R4. Shared production invariants

- 两种模式都继续要求外部 HTTPS、正确的 `HOUFENG_PUBLIC_BASE_URL`、NPM WebSocket 转发以及现有代理安全设置。
- 现有初始化、数据库、Records authority、processor、ClamAV、secret scope、可移植数据和恢复合同不得改变。
- 不自动发现、连接或修改用户的 NPM；文档提供只读发现现有 NPM 网络的方法。

### R5. Release and operator contract

- GitHub Release 发布公共基础文件、两个模式文件和匹配版本的 `compose.env.example`。
- 发布流程独立渲染并验证 shared-network 与 host-proxy 两种最终 Compose 模型，再上传并公开读回每个精确资产名，要求下载内容与 staged 文件逐字节一致。
- README、正式部署指南、env 模板和 Trellis 部署规范必须说明模式选择、upstream、Docker 版本边界、验证、升级、备份与恢复文件集合。

## Acceptance Criteria

- [ ] AC1: bridge-mode NPM 用户复用 NPM 已加入的现有网络；Houfeng 在该网络以 `houfeng:16001` 可达，NPM Compose 无需修改。
- [ ] AC2: shared-network 模式缺少 `HOUFENG_PROXY_NETWORK` 时 `docker compose config` 以明确诊断失败，且最终模型不含 published port。
- [ ] AC3: host-mode NPM 用户把 `COMPOSE_FILE` 切换到 host 模式后，可保持 `HOUFENG_PROXY_NETWORK` 为空并成功渲染；最终模型只含 `127.0.0.1:16001 -> 16001/tcp`。
- [ ] AC4: host 模式文档在启动前要求并展示 Docker Engine 28.0.0+ 检查，明确 NPM upstream 为 `127.0.0.1:16001`，且不提供旧 Engine 防火墙绕过方案。
- [ ] AC5: 两个模式最终模型中，只有 Center 获得代理可达性，Center 始终保留 default network；数据库、ClamAV、authority、processor 与 initializer 不加入外部代理网络或发布端口。
- [ ] AC6: 自动化测试冻结模式文件职责、env 默认选择、shared-network stable alias/no-host-port、host loopback-only/no-external-network requirement，以及四个 release asset 的验证和公开读回合同。
- [ ] AC7: README 和正式部署指南提供可复制的现有 NPM 网络发现、模式选择、下载、`docker compose config`、启动和公共 health 验证步骤，不建议填写 `host` 或创建占位网络。
- [ ] AC8: focused deployment tests、两种 Compose render、Go 质量门、workflow/YAML/shell 检查、diff hygiene、独立 Trellis check、PR CI 和发布资产验证全部通过。

## Out of Scope

- 自动发现或修改用户的 NPM Compose 项目。
- 代替 NPM 管理证书、域名、TLS、访问控制、限流或其他 Proxy Host 生命周期。
- 支持将 Houfeng Center 直接暴露到公网而不经过 HTTPS 反向代理。
- 为 Docker Engine 28.0.0 以前的 host-proxy 部署设计防火墙规则或兼容承诺。
- 改变 Houfeng 内部数据库、Records authority、附件处理或 Agent 部署拓扑。
