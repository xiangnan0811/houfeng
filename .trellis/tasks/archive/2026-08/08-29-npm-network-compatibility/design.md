# NPM 网络兼容设计

## 1. Problem and boundary

Houfeng 需要在不接管 NPM 生命周期的前提下，为两种既有部署提供安全 upstream：

1. bridge-mode NPM：Houfeng Center 加入 NPM 已有 external network，由 Docker DNS 暴露为 `houfeng:16001`。
2. host-mode NPM：NPM 共享宿主机网络，无法加入 external bridge 或解析 `houfeng`；Center 通过宿主机 IPv4 loopback 暴露为 `127.0.0.1:16001`。

两种模式只改变 Center 的代理入口，不改变八个服务的镜像、依赖、secret、authority、数据卷、健康检查或内部 default network。

## 2. Release asset architecture

| Asset | Responsibility |
| --- | --- |
| `compose.yaml` | 完整公共服务图；Center 显式保留 `default` network；不声明 external proxy network，不发布端口 |
| `compose.proxy-network.yaml` | 给 Center 增加 `houfeng-proxy` network 与 alias `houfeng`；定义 required external network name |
| `compose.proxy-host.yaml` | 给 Center 增加一个 long-syntax TCP port mapping：`host_ip: 127.0.0.1`、`published: "16001"`、`target: 16001` |
| `compose.env.example` | 默认设置 `COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml`；说明 host 用户切换到 host mode file，并让 `HOUFENG_PROXY_NETWORK` 保持空白 |

模式文件保持薄且互斥。基础文件不依赖 `HOUFENG_PROXY_NETWORK`，因此 Compose 按文件逐个插值时，host 模式不会解析 shared-network 文件里的 required expression。

不使用 profiles：profiles 只控制 service，不能条件化 top-level network。不发布两份完整服务图：这会复制初始化、authority、secret 和 recovery wiring。

## 3. Operator data flow

### 3.1 Shared-network mode

1. 用户下载四个 release assets，并把 env template 保存为 `.env`。
2. 保留默认 `COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml`。
3. 在 NPM 目录运行只读 inspect，得到 NPM app 实际加入的用户自定义网络名。
4. 设置 `HOUFENG_PROXY_NETWORK=<existing-network>`。
5. `docker compose config` 合并基础文件和 network mode file；若网络变量为空，插值立即失败。
6. `docker compose up -d` 让 Center 同时加入 default 和 existing NPM network；其他 Houfeng 服务只留在 default network。
7. NPM Proxy Host 转发到 `http://houfeng:16001`。

### 3.2 Host-proxy mode

1. 用户下载相同四个 release assets。
2. 设置 `COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml`，保持 `HOUFENG_PROXY_NETWORK=`。
3. 在启动前运行 `docker version --format '{{.Server.Version}}'`，确认 Engine 至少为 28.0.0。
4. `docker compose config` 合并基础文件和 host mode file；最终 Center 仍加入 default network，并只发布 IPv4 loopback port。
5. NPM Proxy Host 转发到 `http://127.0.0.1:16001`。

两种模式的公共健康验证都通过 `HOUFENG_PUBLIC_BASE_URL` 访问 `/api/healthz`；host 模式可额外从宿主机执行带正确 Host header 的 loopback health probe。

## 4. Configuration and failure contract

| Condition | Expected behavior |
| --- | --- |
| shared-network mode + blank `HOUFENG_PROXY_NETWORK` | `docker compose config` 以 mode-file required message 失败 |
| shared-network mode + existing network absent | `docker compose up` 以 external network not found 失败；不创建替代网络 |
| host-proxy mode + blank `HOUFENG_PROXY_NETWORK` | `docker compose config` 成功；最终模型无 external proxy network |
| host-proxy mode + Docker Engine < 28 | 文档定义为 unsupported，操作员在启动前停止；不提供防火墙旁路 |
| 两个 mode files 同时配置 | unsupported operator edit；文档要求只选一个，静态测试冻结 env template 只选择一个 |
| `COMPOSE_FILE` 被删除或改坏 | 不属于受支持的 release env；升级指引要求保留/重新选择 mode，`docker compose config` 是启动前强制检查 |
| host port 16001 已占用 | container 创建失败并显示 bind error；MVP 不增加可变 host port |

固定 host port 是刻意的 YAGNI 边界：NPM upstream 和诊断保持唯一；端口冲突时由用户先解决冲突，而不是扩展新的生产配置面。

## 5. Security

- shared-network 模式不发布宿主机端口；只有 Center 加入 NPM external network。
- host-proxy 模式使用 long syntax 固定 `host_ip: 127.0.0.1`，测试拒绝缺失 host IP、`0.0.0.0`、`::` 或 short syntax。
- host-proxy 模式要求 Docker Engine 28.0.0+，避免把旧 Engine 的 same-L2 localhost reachability 风险包装成受支持配置。
- 两种模式都保持 HTTPS public origin、Secure session cookie、NPM WebSocket/Block Common Exploits/Force SSL、request body/rate/connection limits。
- `HOUFENG_TRUSTED_PROXIES` 仍只在确需 forwarded client IP 时配置为实测的精确代理来源 CIDR；不得为 host 模式猜测 `0.0.0.0/0`、`::/0` 或 `127.0.0.0/8`。

## 6. Upgrade, backup, and rollback

现有 shared-network 部署升级时下载两个新增 mode assets，在私有 `.env` 增加默认 `COMPOSE_FILE`，保留原有 `HOUFENG_PROXY_NETWORK`；渲染后的运行拓扑不变。

备份/迁移单元扩展为基础 Compose、当前使用的 mode file（建议保留两个 release mode files）、`.env`、`optional-secrets/` 和完整 `data/`。数据库、附件和 authority 恢复合同不变。

在两种模式间切换只允许修改 `COMPOSE_FILE` 以及 mode-specific network 值，然后先运行 `docker compose config` 再 `up -d`；Compose 可重建 Center，但不得重建或分裂数据/authority identity。

回滚到旧 release 时恢复该 release 匹配的 Compose/env assets。若新版本尚未启动业务服务，可把 `COMPOSE_FILE` 切回原 mode 并重新渲染；任何数据库迁移回滚仍遵循现有完整冷备恢复规则。

## 7. Test and release design

### Static contract tests

- 基础文件：完整八服务图、Center 明确保留 default network、无 `ports`、无 `HOUFENG_PROXY_NETWORK`、无 external proxy network。
- network mode file：只扩展 Center network；stable alias；required external network；无 ports。
- host mode file：只扩展 Center loopback port；无 external network、alias 或 `HOUFENG_PROXY_NETWORK`。
- env/docs：默认 `COMPOSE_FILE`、两种 upstream、Engine 28+、existing-network discovery、禁止 `host` network value/placeholder network。

### Compose render checks

- shared-network：以完整测试 env 渲染两文件模型，检查唯一 Houfeng image、Center 同时具有 default + external network 且无 published port。
- host-proxy：unset `HOUFENG_PROXY_NETWORK` 后渲染两文件模型，检查唯一 Houfeng image、Center default network 和 loopback mapping，检查模型无 external proxy network。

### Release workflow

workflow staging、upload、public asset cardinality、download 和 byte comparison 从两个资产扩展到四个；两种模型都必须在 upload 前渲染并引用匹配 release image。post-upload 验证仍允许 Release 包含无关 agent assets。

### Quality gates

运行 focused deployment package、`make verify-go`、Compose 双模式 render、workflow YAML parse、相关 shell syntax、`git diff --check` 和独立 Trellis review。PR required checks 通过后合并；若变更触发 release，继续验证四个公开资产和两种下载后模型。
