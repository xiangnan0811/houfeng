# Asset Decisions 领域拆分

## Goal

在不改变请求、URL、DOM workflow 和 mutation 语义的前提下，删除 2,705 行总控组件与 wrapper loophole，让 Asset Decisions 按业务域拥有独立 controller 和测试。

## Confirmed Facts

- 路由 page 只有 5 行，真实实现被整体搬到 2,705 行 `AssetDecisionsPageContent.tsx`。
- Content 有 73 个 hook 调用点、12 个 effect、十余个 remote state，并向展示组件穿透大量 setter。
- 当前 800 行守护只检查 wrapper 文件，可通过重命名规避。

## Requirements

- 先冻结 initial load、partial error、URL/deep link 与所有 mutation/refresh contract。
- 唯一 route-state hook 读写 search params；七个领域 hook 暴露 `{state, commands}`，不暴露原始 setter。
- mutation command 内部拥有 saving/error/refresh，通过明确 invalidation event 跨域协调。
- 最终删除 `AssetDecisionsPageContent.tsx`，route page 不 import API，展示组件不 import controller/API。

## Dependency And Scope

- 依赖 `frontend-modal-stack-focus`；在 Gate A 完成后才启动。
- 不修改后端 contract，不顺手调整视觉或业务规则。

## Acceptance Criteria

- [ ] 现有 workflow、URL、method/path/body 和最小 refresh set 均有回归测试。
- [ ] route state、portfolio、groups、manual groups、templates、records、renewal queue 分域。
- [ ] 不存在 `*PageContent` 总控替身，route page 无 API/mutation/effect 堆积。
- [ ] AST guard 覆盖目录 glob 与依赖方向，无法通过换文件名绕过。
