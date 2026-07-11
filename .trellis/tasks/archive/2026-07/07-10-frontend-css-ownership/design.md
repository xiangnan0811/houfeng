# CSS owner 与预算设计

## Analyzer Contract

`scripts/analyze-web-css.mjs` 使用直接 devDependency `postcss`，稳定输出 source bytes、rules、declarations、selector + at-rule context、重复 selector、literal colors、`!important`、owner、production raw/gzip。

初始上限：source 435865、rules 3044、declarations 11892、重复 selector 178、production CSS 415864 bytes；实施分支 fresh build 重采后只允许有证据地降低或校正测量误差。

## Ownership And Cascade

`index.css` 明确 owner 导入顺序。重复 selector 只有在同一 cascade intent 下合并；media/theme override 保留 context。使用 class/source inventory 加浏览器基线判断不可达，不单凭文本搜索删除。

## Route CSS Pilot

仅在 Asset Decisions 试验 lazy route CSS。必须同时满足无 FOUC、视觉/交互 gate 通过且初始全局 CSS raw+gzip 下降，否则撤销 pilot，不扩散到其他 route。

## Rollback

budget baseline 独立提交，随后一个 owner 一个提交/PR；禁止跨 owner 大段删除。
