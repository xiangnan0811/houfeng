# Stage 2 Phase 5: 自定义阈值

## Goal

把评估器中硬编码的阈值（CPU 80%/95%、内存 85%/95% 等）变为用户在 Settings 中可配的参数。Settings 已有 IncidentDefaults + 全局默认 + 覆盖规则 JSON 机制，只需补指标级阈值字段 + 接入评估器。

## Background

- 评估器 `internal/center/incidents/evaluator.go` 所有阈值硬编码：如 `fastThresholdSeverity(sample.CPUUsagePct, 80, 90, 95)`
- Settings 已有 `IncidentDefaults` struct（HeartbeatIntervalSeconds / StaleThresholdIntervals / SweepIntervalSeconds 等），**缺指标级阈值**
- 覆盖规则已有 JSON textarea（Phase 3 mono 任务加过），可扩展为 per-group threshold override
- SettingsPage 已有 `IncidentDefaultsEditor` 组件 + 表单

## Requirements

### 1. 扩展 Settings IncidentDefaults

`internal/center/settings/types.go` `IncidentDefaults` 加：

```go
type IncidentDefaults struct {
    // existing ...
    HeartbeatIntervalSeconds int `json:"heartbeat_interval_seconds"`
    StaleThresholdIntervals  int `json:"stale_threshold_intervals"`
    SweepIntervalSeconds     int `json:"sweep_interval_seconds"`
    
    // NEW: metric thresholds
    CPUWarningPct    int `json:"cpu_warning_pct"`    // default 80
    CPUCriticalPct   int `json:"cpu_critical_pct"`   // default 90  (actually 95 in current code, let's match)
    MemWarningPct    int `json:"mem_warning_pct"`    // default 85
    MemCriticalPct   int `json:"mem_critical_pct"`   // default 95
    DiskWarningPct   int `json:"disk_warning_pct"`   // default 85
    DiskAlertPct     int `json:"disk_alert_pct"`     // default 92 (the middle tier)
    DiskCriticalPct  int `json:"disk_critical_pct"`  // default 97
    InodeWarningPct  int `json:"inode_warning_pct"`  // default 80
    InodeAlertPct    int `json:"inode_alert_pct"`    // default 90
    InodeCriticalPct int `json:"inode_critical_pct"` // default 95
}
```

Default values = 当前硬编码值（向后兼容）。`validateIncidentDefaults` 加范围校验（1-100）。

### 2. 评估器接入 Settings

`internal/center/incidents/evaluator.go` 改为从 settings 读阈值：

- `Evaluator` struct 加 `settings *settings.CenterSettings` 字段
- 评估前通过 `e.settings.IncidentDefaults` 取阈值，不硬编码
- 如果 settings 为 nil（测试环境），fallback 到 default 常量（保持现有测试通过）

### 3. 前端 SettingsPage 阈值表单

`web/src/pages/SettingsPage.tsx` `IncidentDefaultsEditor` 扩：

当前有 heartbeatIntervalSeconds / staleThresholdIntervals / sweepIntervalSeconds 等 input。

每组指标加两行：warning 阈值 + critical 阈值（numeric input + "%" unit suffix）。用 `summary-grid summary-grid--numeric` 紧凑排列。

### 4. 测试

- settings 单测：验证 default 值 + validate
- evaluator 单测：验证从 settings 读阈值（mock settings）
- 前端：SettingsPage.test.tsx 既有断言更新（IncidentDefaultsEditor 新增字段）

## Acceptance Criteria

- [ ] Settings IncidentDefaults 含 CPU/Mem/Disk/Inode 阈值字段，带默认值
- [ ] 评估器从 settings 读阈值，nil fallback default 常量
- [ ] SettingsPage 阈值表单可编辑/保存
- [ ] lint / test / build 全绿（基线 392）
- [ ] Go test 全绿

## Out of Scope

- 覆盖规则联动阈值（per-group override 机制已存在，但首期不接 group→threshold 的映射逻辑——需要复杂规则引擎）
- 连续 N 次触发条件
- 时间段差异阈值

## Technical Approach

单 PR。改动内聚：Go model + evaluator + settings handler + 前端表单。

## Technical Notes

- Go: `internal/center/settings/types.go` + `internal/center/incidents/evaluator.go`
- Web: `web/src/pages/SettingsPage.tsx` + `web/src/lib/types.ts`（CenterSettings type）
