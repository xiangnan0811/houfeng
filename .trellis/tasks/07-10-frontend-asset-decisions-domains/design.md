# Asset Decisions 领域拆分设计

## Route State Contract

```ts
type AssetDecisionRouteState = {
  filter: AssetDecisionGroupListFilter
  workbench: WorkbenchView
  secondary: SecondaryWorkbench | null
  open: { type: OpenStateKey; id: string } | null
  commands: {
    setWorkbench(value: WorkbenchView): void
    openEntity(type: OpenStateKey, id: string): void
    closeEntity(): void
    clearFilter(key: ContextFilterKey): void
  }
}
```

只有 `useAssetDecisionRouteState` 调用 `useSearchParams`。领域 controllers 按 portfolio、groups、manual groups、templates、records、renewal queue 拆分，统一返回 `{state, commands}`。

## Data And Mutation Flow

read hook 拥有其 remote state/effect；mutation command 拥有 saving/error 和成功后的最小 refresh。跨域变更发出 typed invalidation event，不直接写对方 setter。

## Page Boundary

`AssetDecisionsPage.tsx` 只组合 route state、领域 controllers、workbenches 和五个 Modal，不出现 API response merge、form mutation 细节或并列 effect。

## Migration And Rollback

按 route state → portfolio/groups → manual/templates → records/renewal → composition 的顺序迁移。每域一个 commit；行为差异时回滚当前域，不在拆分中修 UI。
