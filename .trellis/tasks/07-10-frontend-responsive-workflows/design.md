# 窄视口核心流程设计

## Overflow Policies

定义三种唯一策略：命令不裁切；tabs 单行横向滚动；宽数据表在局部可聚焦容器滚动。禁止用隐藏文字加 aria-label 掩盖可见信息缺失。

## Component Decisions

- `.tabs--pill` 使用 `overflow-x:auto`、`scrollbar-gutter`，tab 为 `flex:0 0 auto; white-space:nowrap`。
- Asset secondary nav 使用单行滚动或保证完整标题的两列布局，删除 920/640px 冲突覆盖。
- Provider entry links 移除统一 `max-width:48px`；长标签使用明确 modifier。
- table wrapper 具有 `tabIndex=0`、可访问 label 与可见滚动提示，heading/toolbar 不放进 wrapper。

## Rollback

Settings Tabs、Asset nav、Provider table 分提交和截图基线，可按 owner 独立回滚。
