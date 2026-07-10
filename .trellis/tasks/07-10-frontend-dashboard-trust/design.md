# Dashboard 事实与命令面设计

## Data Model

```ts
export type RemoteState<T> =
  | { status: 'loading' }
  | { status: 'success'; value: T; loadedAt: string }
  | { status: 'error'; error: string }

export type DashboardMode =
  | 'onboarding'
  | 'critical'
  | 'abnormal'
  | 'maintenance'
  | 'stable'
```

纯 `buildDashboardModel` 接收各资源 remote state，输出 mode、唯一 primaryAction、最多三个 judgement items、evidence lanes 与 deep links，不 import React。

## State Rules

- 主 dashboard 请求失败仍为整页 error；VPS/订阅失败为局部 degradation。
- onboarding 只在 VPS success 且数组为空时成立。
- abnormal 是严重实例的超集，severe 不参与总数相加。
- fallback 必须带来源和 `snapshot_generated_at`，不得冒充同精度实时数据。

## Presentation Boundary

`DashboardPage` 只加载 remote state、调用 model 并渲染；不保留第二套 command surface。CSS 仅修改 Dashboard owner，移动端不使用四张纵向大卡阻塞主行动。

## Rollback

model 修正和 presentation 重排分 commit；必要时可只回滚视图重排而保留事实修正。
