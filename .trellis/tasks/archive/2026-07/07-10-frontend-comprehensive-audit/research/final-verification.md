# 前端全方位修复父任务最终集成验证

> 结论：十个 child task 已全部归档；Gate A/B/C 的实现均包含在同一发布版本 `v0.58.8`，真实认证 staging 已在该版本通过。父任务复核没有修改业务实现，满足归档条件。

## 1. 集成身份与证据边界

- 最终业务发布版本：[`v0.58.8`](https://github.com/xiangnan0811/houfeng/releases/tag/v0.58.8)。
- 发布、镜像与 staging 共同 commit：`5dedf222283bb4e1e34b6c7b99e0abc7657eff29`。
- 父任务启动前已验证的 `main`：`e7c89208d185c351acf252cc9d9bbaded9e7ad51`，即 Task 10 归档 PR [#378](https://github.com/xiangnan0811/houfeng/pull/378) 的 merge commit。
- `git diff --name-status 5dedf222..e7c89208` 仅包含 `.trellis/tasks/**` 与 `.trellis/workspace/**` 的任务证据、归档和 journal；`web/`、`internal/`、`agent/`、`cmd/`、`db/`、`Makefile`、依赖、Dockerfile 和 GitHub workflows 均无差异。因此父任务复核所跑的产品树与 authenticated staging 所测 `v0.58.8` 产品树一致。
- 父任务分支：`codex/frontend-comprehensive-audit`，从已通过 main CI 的 `e7c89208` fast-forward 后启动；本阶段只写本文件、验收勾选、Trellis 状态、Release Please 流程 gotcha 和 journal，不重开业务实现。

## 2. 十个 child 的闭环与包含关系

所有 child 的 `task.json` 均为 `status=completed`，且只存在于 `.trellis/tasks/archive/2026-07/`；active task 根目录只剩本 parent。下表的 implementation merge commit 均经 `git merge-base --is-ancestor <merge> e7c89208` 返回 0。

| # | Child / 问题映射 | Implementation PR | Merge commit | 父级结论 |
| ---: | --- | --- | --- | --- |
| 1 | `frontend-quality-gate-strict`；P1-07、P2-09、P3-02 | [#342](https://github.com/xiangnan0811/houfeng/pull/342) | `b791103b55cef38cbe225292e48eaedde87916b9` | 已归档、已包含 |
| 2 | `frontend-modal-stack-focus`；P1-01 | [#344](https://github.com/xiangnan0811/houfeng/pull/344) | `82141746a6cf1249f6b2cdbf20e4c141333a9ffe` | 已归档、已包含 |
| 3 | `frontend-dashboard-trust`；P1-02、P1-03、P1-05、P3-03 | [#346](https://github.com/xiangnan0811/houfeng/pull/346) | `afa0ef71354f5ee01e7d867ad4b3856b8d9c471a` | 已归档、已包含；旧 task metadata 缺 PR 字段，以 GitHub merge 事实补证 |
| 4 | `frontend-shell-recovery`；P1-04、P2-07、P2-08 | [#349](https://github.com/xiangnan0811/houfeng/pull/349) | `a79677bfec89f0abf393c09a82116d0b4cb60efd` | 已归档、已包含 |
| 5 | `frontend-csp-compat`；P1-06 | [#352](https://github.com/xiangnan0811/houfeng/pull/352) | `89c25720cae985c72dd78e024a0e7947186ba2a8` | 已归档、已包含；旧 task metadata 缺 PR 字段，以 GitHub merge 事实补证 |
| 6 | `frontend-accessibility-contracts`；P2-01、P2-02、P2-03 | [#357](https://github.com/xiangnan0811/houfeng/pull/357) | `df5669e039a82641df1e484c411f2236fd001d4b` | 已归档、已包含 |
| 7 | `frontend-responsive-workflows`；P2-04 | [#360](https://github.com/xiangnan0811/houfeng/pull/360) | `2f82f29b74af751e8faf3ee9b73ca2cffe461bc1` | 已归档、已包含；旧 task metadata 缺 PR 字段，以 GitHub merge 事实补证 |
| 8 | `frontend-asset-decisions-domains`；P2-05 | [#363](https://github.com/xiangnan0811/houfeng/pull/363) | `b262e1db95a81633ea9a1aa76c6c9669bfc239dc` | 已归档、已包含 |
| 9 | `frontend-css-ownership`；P2-06 | [#365](https://github.com/xiangnan0811/houfeng/pull/365) | `2a26a55e7921b39775e1d4dd1baec319ee609dc9` | 已归档、已包含 |
| 10 | `frontend-quality-ratchets`；P2-09、P2-10、P3-01、P3-03、P3-04 | [#368](https://github.com/xiangnan0811/houfeng/pull/368) | `03c6acb6baea6fc78214063e55ac7de870cf8cf4` | 已归档、已包含；staging feedback PR #370/#372/#373/#374/#376 也在 `v0.58.8` |

父级问题映射没有空洞，也没有重复建立第十一个 child。`evidence_chips:null` 回归发生在模板创建已经成功落库后的前端渲染边界，按用户要求作为 Task 10 staging 阻断由 [#376](https://github.com/xiangnan0811/houfeng/pull/376) 关闭，并由真实 JSON fixture 回归测试固化。

## 3. 父任务 fresh source 与 browser gate

### 3.1 本地 source gate

首次直接运行 `env -u NODE_ENV make verify` 时，Go 全套通过，Web preflight 按合同在安装前拒绝当前 PATH 中的 Node `v24.18.0`：`web requires Node 22.x; found v24.18.0`。这不是产品失败，而是 Task 1 质量门正确阻断环境污染。

随后仅把已安装的 `/home/murray/.nvm/versions/node/v22.23.1/bin` 放到当前命令 PATH 首位，原样运行：

```bash
env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify
```

结果：

- Go fmt/vet/tests 全部通过。
- `npm ci --include=dev` 安装 304 packages，审计输出 0 vulnerabilities。
- ESLint 通过。
- Vitest/V8：119 files / 839 tests 全部通过。
- Fresh coverage：statements `79.56%`、branches `70.93%`、functions `78.95%`、lines `83.15%`；全局与 15 个关键路径 ratchet 均通过。
- strict TypeScript project build 与 Vite production build 通过；257 modules、33 async chunks，无 large chunk warning。
- bundle/font：entry JS gzip `110740/110742`、entry CSS gzip `37135/37135`、max async JS gzip `32050/32052`、7 个 WOFF2 raw `139072/139072`，全部未越界。
- CSS：26 files、`311063` source bytes、2107 rules、8517 declarations、151 repeated selectors、247 literal colors、11 `!important`；production `293270` raw / `38119` gzip，AST budget 通过。

release `v0.58.8` 的归档记录为 statements `79.52%`、branches `70.89%`、functions `78.95%`、lines `83.15%`；父级 fresh run 没有低于该证据或当前预算。

### 3.2 本地正式 Chromium gate

运行：

```bash
PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run test:e2e
```

- Playwright `1.61.1`，Google Chrome for Testing `149.0.7827.55`。
- 58/58 tests 通过：auth 2、九路由三视口 core matrix 27、fixture-router 3、page states 12、security 2、accessibility/keyboard 8、responsive/geometry 4。
- 覆盖 `1440x1000`、`1024x768`、`390x900`；包括 Dashboard 五状态、VPS 503 不进入 onboarding、Modal 逐层 Escape/focus/body lock、skip link、User menu、Settings Tabs、axe serious/critical、严格 CSP、Asset/Provider 390px 命令与局部宽表滚动。
- suite 的 fail-closed diagnostics 对 console/page/request/HTTP/CSP/unhandled rejection 保持阻断；本次无失败 artifact。

### 3.3 受保护 main 复核

- Task 10 归档 PR #378 required checks 全绿后以普通 merge 合并，merge `e7c89208`；未使用 admin bypass。
- 该精确 commit 的 main CI [`29182307727`](https://github.com/xiangnan0811/houfeng/actions/runs/29182307727) 为 success：`go`、`web`、`web-browser`、`docker-image` 全部通过。
- main protection 实时复核：strict required contexts 为 `go`、`web`、`web-browser`、`docker-image`；enforce admins、conversation resolution 开启，force push/delete 关闭。

## 4. Gate A / B / C 同版判定

### Gate A：P1 关闭

- Task 1–5 最早在同版 `v0.58.0` 关闭；它们的 merge commits 全部是 `v0.58.8` 与当前 main 的祖先。
- 当前 839 tests 与 58 Chromium contracts再次同时覆盖污染环境、Dashboard subset/false-empty/五态、Shell 五态与 freshness、Modal stack、error recovery 和严格 CSP。
- 结论：Gate A 在最终集成产品树中保持通过。

### Gate B：交互与移动端关闭

- Task 6–7 最早在同版 `v0.58.2` 关闭；两个 implementation merge 均包含于 `v0.58.8`。
- 当前 browser gate 再次覆盖 Input/Select/Tabs/Menu/skip link、真实键盘路径、axe serious/critical、390px command、Tabs overflow 和具名局部宽表滚动。
- 结论：Gate B 在最终集成产品树中保持通过。

### Gate C：结构债、持续门与 staging

- Task 8–10 及 staging feedback fixes 共同进入 `v0.58.8`。
- 2,705 行 `AssetDecisionsPageContent.tsx` 不存在；七个 `{state, commands}` controller、唯一 route-state owner、API/import direction 和文件/effect 预算由 AST contract 阻断。
- CSS source/rules/declarations/repeated selectors 与 production bytes 低于 Task 9 baseline，并由 owner manifest、reachability、AST 与 budget gate 阻断回退。
- coverage、Playwright、axe、CSP、bundle/font、CSS AST、toolchain 与 staging source contract 已进入 CI。
- authenticated staging 在同一 `v0.58.8` 通过；mock/injection 与真实数据证明边界明确。
- 结论：Gate C 在最终集成产品树中通过。

## 5. 真实认证 staging 与审计 artifact

- GitHub environment：`staging`，id `17999943032`；`custom_branch_policies=true` 且唯一 deployment branch policy 为 `main`。
- feature-ref 负向 run [`29161439145`](https://github.com/xiangnan0811/houfeng/actions/runs/29161439145) 在 secret-free `ref-guard` 失败，`staging-smoke` 未启动。
- authenticated run：[`29181528110`](https://github.com/xiangnan0811/houfeng/actions/runs/29181528110)，`workflow_dispatch`，`main@5dedf222`，expected/observed 均为 `v0.58.8`，结论 success。
- 真实环境六步：版本、UI 登录、九核心路由、自定义模板 cancel-only、Settings 保存/readback/恢复/readback、主题 reload 全部通过。
- deployed-frontend injection 四步：Dashboard 五态、VPS 503、受控慢响应、三视口长 Provider 列表全部通过；这些步骤只证明已部署前端容错，不冒充真实后端/生产数据健康。
- diagnostics：console `0`、page error `0`、request failure `0`、unexpected HTTP `0`、CSP `0`、unhandled rejection `0`。网络 172 条：170×200、预期登录前 401 一条、预期注入 503 一条。
- artifact：`frontend-staging-audit-29181528110`，id `8256569614`，size `3001069` bytes，digest `sha256:2f8ddf6225b8aca98f84b99d533eb4b576ce150eef7afda56e8c8b2ce5ed7404`；实时复核为 `expired=false`，到期 `2026-08-11T05:42:43Z`。
- artifact 只含 manifest、summary、12 张真实环境截图与 9 张 injection 截图；Task 10 已完成文本/视觉脱敏核对，没有 trace、video、error-context、auth state、cookie、凭据、token、请求/响应 body。

## 6. Release 与镜像

- GitHub Release 最新正式版本仍为 `v0.58.8`，target `5dedf222`；四个 agent/checksum/minisign assets 均存在。
- `publish-images` run [`29179975996`](https://github.com/xiangnan0811/houfeng/actions/runs/29179975996) 已完成 amd64、arm64、agent assets 与 multi-arch manifest。
- 归档记录的 OCI index digest 为 `sha256:33bdc5893904bfbcd481fefe2596fb4a134beab5bda68538524e61d5d05193ae`；linux/amd64 为 `sha256:06b5de55b80796f5116ddbc46045e920071456ca7f8e4ac2aa77082414543cfe`，linux/arm64 为 `sha256:1eca5374616d0e5e5a26ec1b8919f584b20d1ae836123764733c81cd22f95708`。
- 父任务实时运行 `docker manifest inspect`，`v0.58.8`、`0.58.8`、`latest` 三个 tag 的完整 manifest 内容 SHA-256 均为 `47b01065e95a4eeca92680fb8d6b9351c21588dd90333e683d51d321f2d504d2`，平台/provenance descriptor 集合一致。

## 7. Release Please 实际结果

- Task 10 归档 merge 后，Release Please run [`29182307737`](https://github.com/xiangnan0811/houfeng/actions/runs/29182307737) 成功，但创建了 open PR [#379](https://github.com/xiangnan0811/houfeng/pull/379) `chore(main): release 0.58.9`。
- #379 只改 `.release-please-manifest.json` 与 `CHANGELOG.md`，内容仅为 `docs(task): record Task 10 staging verification`；其 required checks 已通过。
- 本次父任务没有把纯 Trellis 证据视为 release-worthy product change，因此 #379 保持 open、未合并、未产生 `v0.58.9` tag 或新镜像。后续真实 release-worthy commit 可让 Release Please 更新该 PR。

## 8. 残余风险

- staging 是专用非生产环境、单一非生产账号和当前非敏感数据集；不能外推生产权限或生产数据完整性。
- 自动浏览器仅 Chromium；未覆盖真实移动设备、iOS Safari、Firefox、Windows 高对比模式或 WebKit。
- 未执行 Lighthouse、真实弱网性能预算、持续八小时会话的轮询/内存稳定性或像素级 visual regression。
- injection lane 证明 deployed frontend 的 loading/stale/unavailable/响应式行为，不证明真实后端会产生同样故障，也不证明生产数据健康。
- 手写 TypeScript 与 Go contract 仍无 codegen；高风险边界已有 fixture/contract test，但后端正式拥有 schema 前不引入另一套无人维护 schema。
- GitHub runner 曾提示 `actions/cache@v4`、`actions/upload-artifact@v4` 的 Node 20 action runtime 被强制到 Node 24；项目 install/lint/test/build 与 Playwright job 仍由 `.node-version` 固定 Node `22.23.1`。待上游提供合适 major 后单独评估，不在本父任务顺手改 workflow。
- coverage 与 bundle/CSS limits 已是紧 ratchet；合理增长必须有证据地更新预算，不能静默抬高或删除 gate。

## 9. 归档判定

- 十个 child 均合并、关闭并归档。
- Gate A/B/C 在 `v0.58.8` 同一产品版本通过；当前 main 与该版本的产品树无差异。
- authenticated staging、响应头、console/network、截图与 artifact 均有可追溯证据。
- 父任务 fresh source/browser gate 与 main post-merge CI 全绿。
- 未发现需要在父任务中重开业务实现的新缺陷；所有未覆盖项均作为残余风险保留，没有被误标为通过。

因此父任务可进入 Trellis quality check、spec-update decision、提交与归档流程。
