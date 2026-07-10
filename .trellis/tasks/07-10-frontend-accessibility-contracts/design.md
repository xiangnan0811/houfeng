# 可访问性交互契约设计

## Field Contract

Input/Select 使用 `useId` 或传入 id 派生 hint/error ids；调用者已有 `aria-describedby` 与内部 ids 去重合并。error 存在时设置 `aria-invalid=true`，required 原样落到原生控件。

## Tabs API

```ts
export interface TabsProps<V extends string> {
  label: string
  idBase: string
  items: readonly TabItem<V>[]
  value: V
  onChange: (next: V) => void
}
```

selected tab 是唯一 tab stop；ArrowLeft/Right 循环，Home/End 跳首尾并调用 onChange。调用页面负责 panel 的 role/id/aria-labelledby。

## Semantic Migration

Sidebar 用户 chip 与主题项使用 button/menu semantics；VPS 资产名使用 Link；整行点击只作 pointer enhancement；AppShell 第一可聚焦项为 skip link。

## Guard And Rollback

TypeScript AST test 扫描 production TSX，避免正则误报。atoms 与页面迁移分提交，页面回归时可保留 atom 修复。
