# Task 10 最终交付与 staging 验证证据

> 结论：Task 8–10 的 Gate C 在同一 release `v0.58.8` 上通过；Task 10 可归档。父任务继续保持 `planning`，下一阶段只做十个 children 的跨任务集成复核与归档，不再修改业务实现。

## Integration Identity

- Final release/tag: [`v0.58.8`](https://github.com/xiangnan0811/houfeng/releases/tag/v0.58.8)
- Release/main commit: `5dedf222283bb4e1e34b6c7b99e0abc7657eff29`
- Task 10 implementation PR: [#368](https://github.com/xiangnan0811/houfeng/pull/368), implementation HEAD `ad33adaba0d38a67bb241e12f06d1a1623cb99da`, merge `03c6acb6baea6fc78214063e55ac7de870cf8cf4`
- Staging feedback fixes included in the final release:
  - [#370](https://github.com/xiangnan0811/houfeng/pull/370) `fix(web): correlate staging resource errors`
  - [#372](https://github.com/xiangnan0811/houfeng/pull/372) `test(web): settle fonts before staging navigation`
  - [#373](https://github.com/xiangnan0811/houfeng/pull/373) `test(web): wait for staging routes to settle`
  - [#374](https://github.com/xiangnan0811/houfeng/pull/374) `fix(web): keep custom scenario templates reachable`
  - [#376](https://github.com/xiangnan0811/houfeng/pull/376) `fix(web): keep empty template groups renderable`
- Final Release Please PR: [#377](https://github.com/xiangnan0811/houfeng/pull/377), merge/tag commit `5dedf222283bb4e1e34b6c7b99e0abc7657eff29`

真实 staging 暴露的字体/请求 settle、custom template 可见性和 Go nil-slice `evidence_chips:null` 回归都在本任务内关闭。最后一个问题发生在创建组合已成功落库之后的渲染边界，属于前序 Asset Decisions 修改引入的 Task 10 staging 回归，没有拆成新需求或新 Trellis task。

## Source, Coverage And Browser Gates

- Runtime: Node `22.23.1`, npm `10.9.8`, `@playwright/test 1.61.1`, Chromium `149.0.7827.55`.
- Final full Vitest/V8: 119 files / 839 tests.
- Coverage: statements `79.52%` (`8315/10456`), branches `70.89%` (`6977/9841`), functions `78.95%` (`2667/3378`), lines `83.15%` (`7392/8889`). All 15 critical production paths remain present and branch coverage is at least 90%.
- Local/CI Chromium contracts: 58/58, including auth 2, core matrix 27, fixture-router 3, page states 12, security 2, accessibility/keyboard 8, responsive/geometry 4.
- Bundle/font actual vs limit: entry JS gzip `110740/110742`, entry CSS gzip `37135/37135`, max async JS gzip `32050/32052`, seven WOFF2 raw bytes `139072/139072`.
- CSS: 26 files, `311063` source bytes, 2107 rules, 8517 declarations, 151 repeated selectors, 247 literal-color declarations, 11 `!important`; production `293270` raw / `38119` gzip. Task 9 remains the only CSS AST/budget owner.
- `npm audit --include=dev`: 0 vulnerabilities.
- PR #368 CI run [`29161077816`](https://github.com/xiangnan0811/houfeng/actions/runs/29161077816) showed `go`, `web`, `web-browser`, `docker-image` and GitGuardian success before merge.
- Final release commit main CI run [`29179972331`](https://github.com/xiangnan0811/houfeng/actions/runs/29179972331) passed `go`, `web`, `web-browser` and `docker-image` on the exact staging-tested commit.

## Protected Delivery And Published Artifacts

- Main branch protection has strict required contexts `go`, `web`, `web-browser`, `docker-image`; admins are enforced, force pushes/deletions are disabled, and conversation resolution is required.
- `publish-images` run [`29179975996`](https://github.com/xiangnan0811/houfeng/actions/runs/29179975996) passed agent-assets, linux/amd64, linux/arm64 and multi-arch publish.
- GitHub Release assets include amd64/arm64 agents, `sha256sums.txt` and its minisign signature.
- Docker Hub tags `linnea7171/houfeng:v0.58.8`, `:0.58.8` and `:latest` resolve to the same OCI index digest `sha256:33bdc5893904bfbcd481fefe2596fb4a134beab5bda68538524e61d5d05193ae`.
- Platform manifests are linux/amd64 `sha256:06b5de55b80796f5116ddbc46045e920071456ca7f8e4ac2aa77082414543cfe` and linux/arm64 `sha256:1eca5374616d0e5e5a26ec1b8919f584b20d1ae836123764733c81cd22f95708`; the other two index entries are provenance attestations with `unknown/unknown` platform, not extra runtime architectures.

## Staging Environment Guardrails

- GitHub environment: `staging`, id `17999943032`.
- Deployment policy: `protected_branches=false`, `custom_branch_policies=true`; the sole policy is branch `main`.
- Workflow remains `workflow_dispatch(expected_version)` only, `permissions: contents: read`, fixed `frontend-staging-smoke` concurrency, `cancel-in-progress:false`.
- Negative feature-ref run [`29161439145`](https://github.com/xiangnan0811/houfeng/actions/runs/29161439145) failed the secret-free `ref-guard`; the environment job `staging-smoke` was skipped with an empty step list. This proves the modified feature ref did not reach environment secrets.
- URL is held as environment variable and credentials as environment secrets. Their values are absent from repository files and this evidence.

## Authenticated Staging Run

- Workflow run: [`29181528110`](https://github.com/xiangnan0811/houfeng/actions/runs/29181528110)
- Ref/commit: `main@5dedf222283bb4e1e34b6c7b99e0abc7657eff29`
- Expected/observed version: `v0.58.8` / `v0.58.8`
- Browser: Chromium `149.0.7827.55`
- Result: `ref-guard` passed; authenticated audit passed; sanitized artifact upload passed; overall conclusion `success`.
- Real-environment steps, all passed:
  1. release version;
  2. UI login;
  3. nine core routes;
  4. custom scenario-template cancel-only confirmation;
  5. reversible Settings save/readback/restore/readback;
  6. theme persistence across reload.
- Deployed-frontend injection steps, all passed and not represented as backend truth:
  1. Dashboard critical/abnormal/maintenance/onboarding/stable;
  2. Dashboard supporting 503;
  3. controlled slow response;
  4. long provider list at `1440x1000`, `1024x768`, `390x900`.
- Routes: `/`, `/vps`, `/asset-decisions`, `/monitoring`, `/targets`, `/events`, `/providers`, `/subscriptions`, `/settings`, plus `/settings?tab=monitoring` for reversible mutation.
- Diagnostics: console errors `0`, page errors `0`, request failures `0`, unexpected HTTP errors `0`, CSP violations `0`, unhandled rejections `0`.
- Network inventory: 172 entries; 170×200, one expected pre-login `GET /api/auth/me` 401, one expected injected `GET /api/vps` 503.
- Main document evidence allowlists only CSP, content-type, permissions-policy, referrer-policy, HSTS, X-Content-Type-Options and X-Frame-Options. The observed CSP exactly matches `internal/center/http/csp-policy.txt` and contains no `unsafe-inline`.

## Audit Artifact And Sanitization

- Artifact: `frontend-staging-audit-29181528110`
- Artifact id: `8256569614`
- Compressed size: `3001069` bytes
- GitHub artifact ZIP digest: `sha256:2f8ddf6225b8aca98f84b99d533eb4b576ce150eef7afda56e8c8b2ce5ed7404`
- Created: `2026-07-12T05:42:44Z`; expires: `2026-08-11T05:42:43Z`.
- Contents: `manifest.json`, `summary.md`, 12 real-environment screenshots and 9 injected screenshots. No trace, video, automatic screenshot, `error-context`, raw request/response body or auth state is included.
- Text scan found no `authorization`, `set-cookie`, `cookie`, `password`, `secret`, `bearer`, access/refresh token, request/response body or username keys/values. Network paths are origin-relative and sensitive query values are replaced with `<redacted>`.
- Screenshots were visually inspected for the Asset Decisions surface, custom-template cancel result, restored Settings state, Dashboard 503 and 390px provider list. Login fields and the user chip are masked; no credential or token is visible.
- Extracted-file hashes:
  - `manifest.json`: `sha256:fee36f3b9c08c75043bc6cf48c9e43b00f32510248c1da8fd87d0a819156ed97`
  - `summary.md`: `sha256:66a28015b0e52ddd3cd49b75f1d8b0a7bfcd5c2847821f03066656be5350308c`

Artifact expiry does not remove the decision record: run id, commit/tag, browser, routes/viewports, counters, policy, artifact digest, screenshots inventory and conclusion are persisted here and in the parent Gate C section.

## Residual Risks

- This is a dedicated non-production environment with one staging account and the current non-sensitive dataset; it is not a production authorization or data-integrity conclusion.
- Injection validates the deployed frontend's loading/stale/unavailable/responsive behavior on the real origin and CSP. It does not prove the backend would emit those exact failures or that production data is healthy.
- Chromium is the only automated browser. No real mobile device, iOS Safari, Firefox, Windows high-contrast, Lighthouse or eight-hour polling/memory run was performed.
- The custom template and empty manual groups are non-sensitive staging fixtures retained for repeatable future audits; they are not production records.
- GitHub emitted an operational warning that `actions/cache@v4` and `actions/upload-artifact@v4` still target the deprecated Node 20 action runtime and were forced to Node 24 by the runner. Project install/lint/test/build and Playwright remain pinned to Node `22.23.1`; the warning did not affect this run but should be revisited when upstream action majors are available.

## Archive Decision

All Task 10 product, test, CI, release, environment and authenticated staging acceptance criteria are met on one integration version. Task 10 is ready to archive. The parent remains `planning`: after this archive PR and post-merge main CI, it must be explicitly started for the final cross-child integration audit; that parent phase may update evidence/archive state only and must not reopen business implementation.
