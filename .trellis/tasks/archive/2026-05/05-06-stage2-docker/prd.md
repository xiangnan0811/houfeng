# Stage 2 Phase 7: Docker 容器信息采集与展示

## Goal

Agent 在采集主机指标时同步采集本机 Docker 容器信息（`docker ps` + 基础 stats），中心端在节点详情页展示容器列表和状态。不涉及容器编排——仅"看容器"。

## Background

- Agent 已有 `hostsample/` package 采集主机指标（CPU/Mem/Disk 等，25 字段）
- Agent 执行命令已有白名单（Phase 2），但 Docker 信息应走**结构化采集**（同 host sample），不走命令执行
- Docker 信息适合作为 host sample 的附加字段或以独立 container snapshot 模型

## Requirements

### 1. Agent 端：`agent/containersample/` package

新建 package，通过 Docker Unix socket（`/var/run/docker.sock`）或 CLI（`docker` 命令）采集：

```go
type ContainerInfo struct {
    ID      string   `json:"id"`
    Name    string   `json:"name"`
    Image   string   `json:"image"`
    Status  string   `json:"status"`   // "running" / "exited" / ...
    CPUPct  *float64 `json:"cpu_pct,omitempty"`   // docker stats --no-stream
    MemPct  *float64 `json:"mem_pct,omitempty"`
}
```

采集方式：`docker ps --format '{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}' --all --no-trunc` + `docker stats --no-stream --format ...`。

**关键简化**：如果 Docker 不可用（daemon 未运行 / socket 不可达 / docker 命令缺失），静默跳过容器采集（不影响 host sample 主流程）。

### 2. HostSample 扩展

`internal/contracts/agentapi/types.go` `HostSamplePayload` 加：

```go
Containers []ContainerInfo `json:"containers,omitempty"`
```

Agent runtime 在采集 host sample 后，如果 `containersample` 可用，附加 containers 数据。

### 3. 存储

host_observations 表的情况——当前存 JSONB 还是扁平字段？如果已经是 JSONB 存 host sample 的 25 字段，加 containers 字段无需 migration。如果 host_observations 是扁平列，加 `containers JSONB` 列。

**简化方案**：host_observations 表加 `containers JSONB` nullable 列（migration 0015）。已有 host sample 行 containers=NULL。

### 4. 前端：节点详情页容器区

NodeDetailPage watchtower ④次要折叠区加一个 `<details className="watchtower-secondary">` 「容器列表」：

- 展开后：`<DataTable density="compact">` 列：容器名 / Image / 状态（StatusGlyph running=normal, exited=offline）/ CPU% / Mem%
- 从 `runtimeFacts.latest_host_sample.containers[]` 读数据

### 5. 测试

- `agent/containersample/` 测试：mock docker CLI 输出
- `agent/runtime/` 测试：验证 containers 附加到 sync request
- 前端：NodeDetailPage.test.tsx 新增 ≥1 用例（容器列表渲染）

## Acceptance Criteria

- [ ] Agent containersample 采集 Docker 容器信息（ps + stats）
- [ ] HostSamplePayload + host_observations 表支持 containers
- [ ] 节点详情页容器列表 DataTable
- [ ] Docker 不可用时静默跳过，不报错不 block
- [ ] lint / test / build 全绿（基线 392）
- [ ] Go test 全绿

## Out of Scope

- 容器编排（启动/停止/重启）
- 容器日志查看
- Docker Compose / Swarm / K8s
- Container-specific incidents

## Technical Approach

单 PR。~5 文件（agent/containersample + agentapi types + migration + store + NodeDetailPage）。

## Technical Notes

- Agent: `agent/containersample/sample.go` + `sample_test.go`
- Agent contracts: `internal/contracts/agentapi/types.go`
- Migration: `db/migrations/0015_add_containers.sql`
- Store: `internal/center/store/runtime_facts.go` + `store/host_observations.go`
- Frontend: `web/src/pages/NodeDetailPage.tsx`
