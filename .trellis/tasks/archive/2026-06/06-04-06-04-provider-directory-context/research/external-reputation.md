# 外部口碑入口调研

## Decision

本次不在前端抓取或展示外部实时评分，只生成外部研究入口：

- LowEndTalk：社区讨论入口，适合查 provider 的历史帖子和用户反馈。
- Trustpilot：三方用户评价入口，适合补充“外部评价”视角。
- HostAdvice：主机评价入口，适合补充 hosting review 视角。
- VPSBenchmarks：性能基准入口，适合补充 benchmark 视角，不等同于口碑。

这些入口和 provider 的 `rating` 分离展示。`rating` 命名为“我的评分”，表示用户在候风内记录的主观复盘；外部入口只作为下一步研究跳转，不宣称系统知道外部账号、账单、口碑或性能真相。

## Why Not Fetch Ratings

- 没有后端 API 和缓存层时，前端抓取外部站点会遇到 CORS、反爬、页面结构漂移和可用性问题。
- 外部分数会随时间变化，直接展示会制造“候风正在背书评分”的误解。
- LET 是论坛讨论，不是数值评分源；VPSBenchmarks 是 benchmark，不是用户主观口碑。
- 当前任务边界是不新增后端 contract，因此更适合先做稳定的 research link。
