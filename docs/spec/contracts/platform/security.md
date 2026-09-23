# 凭据、数据库与同源安全合同

## Scenario: center credential and database hardening config

1. **Scope / Trigger**
   - Trigger: 修改 `internal/center/config`、`internal/center/auth/password.go`、`internal/center/auth/service.go`、`cmd/houfeng-center/bootstrap.go`、部署 env docs，或中心启动安全配置。
   - 目标：密码 hash 成本、session ID HMAC secret 和生产 PostgreSQL TLS 要成为可测试的启动合同，避免安全调优只停留在文档建议。

2. **Signatures**
   - Config env:
     - `HOUFENG_PASSWORD_BCRYPT_COST`
     - `HOUFENG_SESSION_HMAC_KEY`
     - `HOUFENG_SESSION_HMAC_KEY_FILE`
     - `HOUFENG_DATABASE_REQUIRE_TLS`
     - `HOUFENG_DATABASE_URL`
   - Go config: `config.CenterConfig{PasswordBcryptCost int, SessionHMACKey []byte, DatabaseURL string}`。
   - Auth options: `auth.Options{PasswordBcryptCost int}`。
   - Seed options: `auth.SeedInitialUserOptions{PasswordBcryptCost int}`。
   - Store constructors:
     - `store.NewPostgresSessionRepository(pool *pgxpool.Pool, hmacKey []byte) (*PostgresSessionRepository, error)`
     - `store.NewPostgresMonitoringInstanceRepositoryWithTokenHMACKey(pool *pgxpool.Pool, hmacKey []byte) *PostgresMonitoringInstanceRepository`
     - `store.NewPostgresSyncRepositoryWithTokenHMACKey(pool *pgxpool.Pool, hmacKey []byte) *PostgresSyncRepository`

3. **Contracts**
   - `HOUFENG_PASSWORD_BCRYPT_COST` missing uses `auth.DefaultPasswordBcryptCost`（当前等于 Go bcrypt `DefaultCost`）。
   - Bcrypt cost must be within Go bcrypt `MinCost..MaxCost`; invalid, empty, non-integer, too-low, or too-high values fail config load.
   - `auth.HashPasswordWithCost` validates normal password policy first, then validates cost, then calls bcrypt with the configured cost.
   - `auth.Service.ChangePassword` and first-user seeding must use `cfg.PasswordBcryptCost`; package-level `HashPassword` remains the compatibility/default helper.
   - `HOUFENG_SESSION_HMAC_KEY` is required, must be at least 32 bytes, and is copied into `config.CenterConfig.SessionHMACKey`.
   - `HOUFENG_SESSION_HMAC_KEY_FILE` takes precedence over `HOUFENG_SESSION_HMAC_KEY` for secret-mount deployments.
   - `cmd/houfeng-center/bootstrap.go` must pass `cfg.SessionHMACKey` into `store.NewPostgresSessionRepository`; the session repository must not have a static production default HMAC key.
   - `cmd/houfeng-center/bootstrap.go` must pass the same `cfg.SessionHMACKey` into agent enrollment/sync token repositories through `NewPostgresMonitoringInstanceRepositoryWithTokenHMACKey` and `NewPostgresSyncRepositoryWithTokenHMACKey`; production agent token hashing must not use repository default test key material.
   - New agent enrollment and sync token hashes must use versioned purpose-separated HMAC-SHA256 values. Legacy plain SHA-256 token hashes may only remain as a verification-and-migration compatibility path.
   - Rotating the session HMAC secret invalidates existing browser sessions because database lookup hashes no longer match existing rows. It also invalidates agent enrollment/sync token hashes that have migrated to the HMAC format; rollback/rotation requires planned re-enrollment or token reissue.
   - `HOUFENG_DATABASE_REQUIRE_TLS=true` means `HOUFENG_DATABASE_URL` must include `sslmode=require`、`sslmode=verify-ca`、or `sslmode=verify-full`.
   - Missing `sslmode` or weak modes (`disable`、`allow`、`prefer`) fail startup only when the require-TLS flag is true, so local Compose / localhost development can keep `sslmode=disable`.

4. **Validation & Error Matrix**
   | Condition | Expected behavior |
   | --- | --- |
   | missing bcrypt cost | use default |
   | bcrypt cost below min / above max / non-integer | `LoadCenterConfig` error |
   | change password with configured cost | stored bcrypt hash reports that cost via `bcrypt.Cost` |
   | seed initial user with configured cost | stored bcrypt hash reports that cost via `bcrypt.Cost` |
   | missing session HMAC key | `LoadCenterConfig` error |
   | session HMAC key shorter than 32 bytes | `LoadCenterConfig` error |
   | `HOUFENG_SESSION_HMAC_KEY_FILE` set | file content wins over env key |
   | bootstrap session repository wiring | receives exactly `cfg.SessionHMACKey` |
   | bootstrap agent token repository wiring | monitoring instance and sync repositories receive exactly `cfg.SessionHMACKey` |
   | legacy SHA-256 agent token hash validates | request succeeds and rewrites stored hash to versioned HMAC |
   | `HOUFENG_DATABASE_REQUIRE_TLS=true` and `sslmode=disable` / missing | `LoadCenterConfig` error |
   | `HOUFENG_DATABASE_REQUIRE_TLS=true` and `sslmode=verify-full` | config load succeeds |
   | invalid boolean require-TLS value | `LoadCenterConfig` error |

5. **Good / Base / Bad Cases**
   - Good: production external PostgreSQL uses `HOUFENG_DATABASE_REQUIRE_TLS=true` and `sslmode=verify-full`.
   - Good: production sets a stable random `HOUFENG_SESSION_HMAC_KEY_FILE` through a secret mount; rolling restart keeps browser sessions and migrated agent token hashes valid.
   - Base: local Docker Compose uses co-located `db` service with generated `sslmode=disable` and leaves `HOUFENG_DATABASE_REQUIRE_TLS` unset/false.
   - Bad: raising bcrypt cost globally by changing a package constant without config tests or benchmark guidance.
   - Bad: `PostgresSessionRepository` silently falls back to `[]byte("houfeng-session-hmac-v1")`, so every deployment shares the same public HMAC key.
   - Bad: production center uses `store.NewPostgresMonitoringInstanceRepository(pool)` or `store.NewPostgresSyncRepository(pool)`, causing agent token hashes to use test/default HMAC key material.
   - Bad: documenting production database TLS while code still accepts `sslmode=disable` with no startup guard.

6. **Tests Required**
   - `internal/center/config/config_test.go`: default/override/invalid bcrypt cost and require-TLS accepted/rejected `sslmode` cases.
   - `internal/center/config/config_test.go`: required session HMAC key, `_FILE` precedence, and short-key rejection.
   - `internal/center/auth/password_test.go`: `HashPasswordWithCost` embeds requested cost and rejects invalid cost.
   - `internal/center/auth/service_test.go`: password change stores a hash with configured cost.
   - `internal/center/auth/seed_test.go`: first-user seed stores a hash with configured cost.
   - `internal/center/store/sessions_test.go`: repository rejects missing HMAC key and stores/queries session IDs only by HMAC hash.
   - `internal/center/store/agent_token_hash_test.go` plus monitoring/sync repository tests: new agent token hashes are versioned HMAC values, legacy SHA-256 hashes still verify, and successful use migrates legacy rows.
   - `cmd/houfeng-center/bootstrap_test.go`: default seed dependency and bootstrap auth service wiring pass `cfg.PasswordBcryptCost`; session repository wiring passes `cfg.SessionHMACKey`; source/wiring checks cover agent token repositories receiving `cfg.SessionHMACKey`.

7. **Wrong vs Correct**

```go
// 错误：配置有 cost，但写 hash 时仍走 package-level default。
hash, err := auth.HashPassword(newPassword)
```

```go
// 正确：服务使用启动配置里的 cost。
hash, err := auth.HashPasswordWithCost(newPassword, s.passwordBcryptCost)
```

```go
// 错误：session repository 内部使用所有部署共享的静态 key。
sessionRepo := store.NewPostgresSessionRepository(pool)
```

```go
// 正确：启动配置显式传入部署 secret。
sessionRepo, err := store.NewPostgresSessionRepository(pool, cfg.SessionHMACKey)
```

```go
// 错误：生产 agent token hash 使用 repository 默认测试 key。
syncRepo := store.NewPostgresSyncRepository(pool)
```

```go
// 正确：生产 agent token hash 从启动 secret 派生用途隔离 HMAC key。
syncRepo := store.NewPostgresSyncRepositoryWithTokenHMACKey(pool, cfg.SessionHMACKey)
```

---

## Scenario: Center 与 Web 严格 CSP 同源合同

### 1. Scope / Trigger

- Trigger: 修改 `internal/center/http/SecurityHeaders`、CSP policy、`web/index.html`、Vite dev/preview headers、字体/图标等静态资源、主题 bootstrap，或生产 TSX/CSS 的资源与样式表达时，必须遵守本合同。
- 目标：Center、前端开发预览和 production build 共享一份精确的严格同源策略；不靠 `unsafe-inline`、nonce、data URI 或远程 origin 掩盖资源迁移缺口。

### 2. Signatures

- Policy source: `internal/center/http/csp-policy.txt`，单行精确文本。
- Go embed: `//go:embed csp-policy.txt` → `contentSecurityPolicySource` → `strings.TrimSpace(...)` → `SecurityHeaders(enableHSTS bool)` 的 `Content-Security-Policy` response header。
- Vite boundary: `web/vite.config.ts` 从仓库同一 policy 文件读取，并赋给 `server.headers` 与 `preview.headers`。
- Docker boundary: root `Dockerfile` 的 `web-build` stage 在 `npm run build` 前把同一文件复制到 `/src/internal/center/http/csp-policy.txt`，保持 Vite 的仓库相对路径成立。
- Browser resources: `/theme-bootstrap.js`、`/fonts/*.woff2`、`/select-caret-*.svg` 均来自 `web/public/` 的同源 URL。

### 3. Contracts

- 唯一批准策略是：`default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'`。
- Go runtime 与 Vite 不得各维护策略副本；运行时唯一来源是 `csp-policy.txt`。测试中的 expected literal 只用于发现 policy 漂移，不能成为第二个运行时来源。
- Docker web stage 不能只复制 `web/`：它必须复制原始 `csp-policy.txt` 到 Vite 解析的 `/src/internal/center/http/` 路径；禁止在 `web/` 下生成或维护第二份 policy。
- HTML 不得含 inline script 或远程 font；主题 bootstrap 必须在 React 入口前同步加载同源文件，并只接受 `houfeng|classic` 与 `dark|light|system` allowlist。
- CSS 不得引用 remote font 或 `data:` image。IBM Plex Sans 400/500/600/700、Mono 400/500/600、OFL 和三套主题 caret 必须作为受跟踪的 `web/public/` 资源存在。
- 所有生产 `.tsx` 禁止 JSX `style=`。静态视觉使用 BEM/令牌，SVG 动态几何使用 attributes，比例与列宽优先使用 `<progress>` 与 `<col width>`。Modal scroll lock / clipboard fallback 的窄范围 CSSOM 写入必须保留行为测试与真实 Chromium CSP 证据，不得扩展成业务样式通道。
- CSP 合格需要三层证据同时成立：source contract、Go exact-header/Vite shared-header tests、真实 production build 浏览器 violation gate；只通过其中一层不能宣称兼容。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| `csp-policy.txt` 与批准文本不一致 | Go exact-header、Vite header 或 Web source contract 至少一项失败 |
| Docker web stage 未复制 policy，或复制发生在 `npm run build` 之后 | source contract 失败；镜像构建在加载 Vite config 时以 `ENOENT /src/internal/center/http/csp-policy.txt` 失败 |
| HTML 出现 inline script / Google Fonts | `cspContract.test.ts` 失败，production browser 不得放宽策略通过 |
| CSS 出现 `data:` image / remote font | source contract 失败；禁止加入 `data:` 或远程 origin 到 policy |
| production TSX 出现 `style=` | source contract 报出文件与行号；改用 class/attribute/原生元素 |
| bootstrap persisted preset/mode 非 allowlist | 回退到 `theme-houfeng-dark`，不得生成任意 class |
| 字体、caret、bootstrap 或 OFL 缺失 | public-resource contract 失败；浏览器 resource/network gate 不得标绿 |
| 任一核心路由触发 `securitypolicyviolation`、console/runtime error 或非预期 4xx/5xx | browser gate 失败并保留 route + viewport + directive/URL 证据 |
| 登录页 `/api/auth/me` 返回预期 401 | 只作为未认证基线，不计入非预期网络错误；Document 仍必须带精确 CSP |

### 5. Good/Base/Bad Cases

- Good: Center production、Vite dev/preview 都返回同一策略；七个字体文件、主题脚本和 caret 同源加载，核心路由在三档视口均零 violation。
- Base: 新增动态 SVG 图表，用 presentation attributes + class 表达几何和视觉，并在 source/browser gate 中通过。
- Bad: 为保留 `<script>...</script>` 或 React `style={{...}}` 把 `unsafe-inline` 加回 `script-src` / `style-src`。
- Bad: Go 与 Vite 各复制一份 policy；一次安全收紧只改其中一处，导致本地预览与 production 行为分叉。

### 6. Tests Required

- `internal/center/http/middleware_test.go`: `TestSecurityHeadersSetsBaselineHeaders` 必须断言完整、精确 CSP header。
- `web/vite.config.test.ts`: 断言 dev 与 preview headers 等于批准 policy。
- `web/src/security/cspContract.test.ts`: 断言唯一 policy 文件、无 remote/inline/data/JSX style、所有同源资源与 license 存在、font/caret wiring 完整、theme allowlist 与 `classic-light` 回退一致。
- `web/src/security/cspContract.test.ts`: 同时断言 Docker `web-build` stage 在 `npm run build` 前把原始 policy 复制到 `/src/internal/center/http/csp-policy.txt`。
- 改到的 chart/table/progress/component 必须有 focused unit test，断言不再生成 `style` prop 且运行时值仍进入对应 attribute/value。
- production build 后用真实 Chromium 覆盖 login 与核心路由、`1440x1000` / `1024x768` / `390x900`，捕获 `securitypolicyviolation`、console/runtime、network、Document header、字体、caret、主题切换与动态图表交互；持久化 CI browser gate 由前端质量 ratchet 任务维护。

### 7. Wrong vs Correct

```go
// 错误：在 Go 内复制策略并为现有 inline 资源放宽。
header.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
```

```go
// 正确：运行时只消费嵌入的同一 policy 文件。
//go:embed csp-policy.txt
var contentSecurityPolicySource string

header.Set("Content-Security-Policy", strings.TrimSpace(contentSecurityPolicySource))
```

```tsx
// 错误：React 会生成被严格 style-src 拒绝的内联样式。
<div style={{ width: `${ratio}%` }} />

// 正确：用原生语义元素携带动态比例，视觉由 CSS class 负责。
<progress className="score-bar" value={ratio} max={100} />
```

```dockerfile
# 错误：web stage 看不到 Vite 读取的仓库级 policy。
COPY web/ ./
RUN npm run build

# 正确：复制同一个源文件到 Vite 预期路径，再构建 web。
COPY internal/center/http/csp-policy.txt /src/internal/center/http/csp-policy.txt
COPY web/ ./
RUN npm run build
```

---
