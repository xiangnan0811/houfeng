# Final verification

## Local gates

- `git diff --check`: passed.
- `python3 ./.trellis/scripts/task.py validate 07-01-asset-decision-modal-second-pass`: passed.
- `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`: 1 file, 22 tests passed.
- `cd web && npm run lint`: passed.
- `cd web && npm run test -- --run`: 71 files, 548 tests passed.
- `cd web && npm run build`: passed.

## Browser audit

Preview:

- Vite: `http://127.0.0.1:5188`
- Mock API: `http://127.0.0.1:8080`
- Chromium CDP: `http://127.0.0.1:9223`

Viewports:

- Desktop `1440x1000`
- Mobile `390x900`

Default modal metrics after implementation:

| Surface | Desktop text | Mobile text | Buttons | Badges | Default nav | Forbidden default content | Overflow |
| --- | ---: | ---: | ---: | ---: | --- | --- | --- |
| auto cancel | 128 | 128 | 5 | 3 | `成员明细`, `保存记录` | none | no |
| auto renewal | 135 | 135 | 5 | 3 | `成员明细`, `保存记录` | none | no |
| auto cost | 135 | 135 | 5 | 3 | `成员明细`, `保存记录` | none | no |
| auto region | 130 | 130 | 5 | 2 | `成员明细`, `保存记录` | none | no |
| auto provider | 129 | 129 | 5 | 2 | `成员明细`, `保存记录` | none | no |
| auto evidence | 135 | 135 | 5 | 3 | `成员明细`, `保存记录` | none | no |
| manual group | 136 | 136 | 5 | 3 | `成员维护`, `保存记录` | none | no |
| saved record | 144 | 144 | 4 | 6 | `执行跟进`, `成员跟进`, `来源复核` | none | no |
| scenario template | 179 | 179 | 4 | 3 | `概览`, `创建组合`, `成员蓝图` | none | no |

Forbidden default content checked:

- automatic groups: no member preview, member table, raw-data nav, member names, or per-member `处理` buttons by default;
- manual groups: no member preview, member names, add/raw/template actions, or intent rows by default;
- saved records: no saved member summary, raw member nav, snapshot member list, or member names by default;
- template detail: no raw member table or fixture member names by default.

Main cost view:

- desktop: text 808, buttons 15, badges 16, no document/body overflow;
- mobile: text 736, buttons 15, badges 16, no document/body overflow.

Note: `scripts/visual_evidence.py browser-sanity` was not used because local Python Playwright is unavailable in this environment. CDP checks used the existing browser skill dependency (`ws`) and did not add Playwright or e2e dependencies.
