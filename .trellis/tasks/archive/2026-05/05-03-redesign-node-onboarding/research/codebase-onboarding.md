# NodeOnboardingPage 重设计前期研究

**日期**: 2026-05-03  
**范围**: 前端现状、后端契约、设计权威、共享原子能力、Token 安全约束  
**背景**: v2-houfeng 视觉语言已冻结；前置任务 05-03-redesign-node-pages 完成节点列表+详情页改造；本任务是 gap #17：接入工作台改造

---

## 1. NodeOnboardingPage 现状分析

### 1.1 完整文件清单

```
web/src/pages/NodeOnboardingPage.tsx         445 行
web/src/lib/onboardingTokenCache.ts          44 行  (token 本地会话缓存)
web/src/lib/onboardingTokenCache.test.ts     50 行  (测试)
web/src/pages/NodeOnboardingPage.test.tsx    测试 (TBD)
```

### 1.2 当前 Section 顺序与结构

页面使用 `<div className="page-stack">` + 5 个 `<DetailSection>` 平铺结构：

```jsx
<section className="hero-panel">
  /* 节点身份区：display_name + region/city/provider + 4 状态 badge */
  <h2 className="hero-panel__title">{onboarding.display_name}</h2>
  <div className="badge-row">
    <StatusBadge label={onboarding.lifecycle_status} />
    <StatusBadge label={onboarding.monitoring_status} />
    <StatusBadge label={onboarding.binding_status} />
    <StatusBadge label={onboarding.phase} />
  </div>
</section>

<div className="summary-grid">
  /* 3 KPI 卡：当前阶段 / 首批样本 / 已接收观测 */
  <article className="summary-card">
    <p className="summary-card__label">当前阶段</p>
    <p className="summary-card__value">{onboarding.phase}</p>
  </article>
</div>

/* Section 1: 绑定冲突 (条件性渲染) */
{showBindingConflict ? (
  <DetailSection eyebrow="绑定冲突" title="绑定冲突处置" aside="高优先级">
    <article className="metric-card">
      <h3>高优先级：绑定冲突待处理</h3>
      <dl>
        <div>
          <dt>当前已绑定指纹</dt>
          <dd>{currentFingerprintSummary(onboarding)}</dd>
        </div>
        <div>
          <dt>待确认指纹</dt>
          <dd>{maskFingerprint(pendingBinding?.fingerprint)}</dd>
        </div>
      </dl>
      <div className="badge-row">
        <button onClick={() => handleBindingAction('confirm', confirmNodeRebind)}>
          确认重新绑定
        </button>
        <button onClick={() => handleBindingAction('reject', rejectPendingNodeBinding)}>
          拒绝该指纹
        </button>
        <button onClick={() => handleBindingAction('reset', resetNodeBinding)}>
          重置绑定关系
        </button>
      </div>
    </article>
  </DetailSection>
) : null}

/* Section 2: 接入 Token */
<DetailSection 
  eyebrow="接入 Token" 
  title="接入凭证" 
  aside={tokenIssue ? `最近生成：${formatDateTime(tokenIssue.issued_at)}` : ...}
>
  {tokenIssue ? (
    <article className="metric-card">
      <h3>当前会话 Token</h3>
      <p>{tokenIssue.token}</p>
      <p>请在本次会话内完成安装或妥善保存，离开后系统不会重新展示明文。</p>
    </article>
  ) : (
    <div className="empty-state">
      <h3>当前会话里没有可显示的 Token 明文。</h3>
      <p>请重新生成接入 Token，再继续安装或核对配置。</p>
    </div>
  )}
  <div className="badge-row">
    <button onClick={handleIssueEnrollmentToken}>
      重新生成接入 Token
    </button>
  </div>
</DetailSection>

/* Section 3: 安装步骤 */
<DetailSection eyebrow="安装步骤" title="接入步骤">
  <article className="metric-card">
    <h3>建议顺序</h3>
    <ol>
      <li>在服务器上安装 agent</li>
      <li>写入该节点专属 token</li>
      <li>启动 systemd 服务</li>
      <li>等待首次同步与绑定完成</li>
    </ol>
  </article>
</DetailSection>

/* Section 4: 当前状态 */
<DetailSection eyebrow="当前状态" title="状态反馈">
  <div className="empty-state">
    <h3>{phase.title}</h3>
    <p>{phase.description}</p>
    <p>首批样本：{onboarding.has_host_sample ? '已到达' : '未到达'}</p>
    <p>已接收观测：{onboarding.has_accepted_observation ? '已接收' : '未接收'}</p>
    {onboarding.phase === '接入完成' ? (
      <Link className="text-link" to={`/nodes/${onboarding.node_id}`}>
        查看节点详情
      </Link>
    ) : null}
  </div>
</DetailSection>
```

### 1.3 Token 区块当前实现

**当前行为**：
- 仅在 `tokenIssue` 存在时显示明文（通过 localStorage `onboardingTokenCache` 缓存会话内 token）
- 明文显示为 `<p>{tokenIssue.token}</p>` 纯文本
- **缺失项**：
  - ❌ 没有"复制到剪贴板"按钮
  - ❌ 没有倒计时 / TTL 显示（token 无明确过期时间提示）
  - ❌ token 区块在"接入完成"阶段仍占据大半屏（应该 dim 或隐藏）
  - ✅ 离开后不会重新展示（缓存逻辑 line 161-167 检查 `enrollment_token_issued_at` 与缓存 `issued_at` 匹配）

### 1.4 四个 Phase 的当前可视化

通过 `describePhase(state)` 函数（line 61-89）返回标题 + 描述文案，在两个地方消费：
1. **Summary grid** 的单个 KPI 卡（line 321）：显示 `onboarding.phase` 枚举值
2. **"当前状态" DetailSection**（line 429-434）：显示 phase 对应的长文案说明

**缺失的可视化**：
- ❌ 没有进度指示 stepper / progress bar（4 phase 应有分步骤可视化）
- ❌ 没有色彩或图标区分（4 个 phase 都用同一种 empty-state 风格）
- 当前仅用文字 + metadata 字段说明阶段，缺乏快速识别性

### 1.5 安装步骤当前形态

位于 line 415-425，是**静态 markdown 风格列表**：
```jsx
<ol>
  <li>在服务器上安装 agent</li>
  <li>写入该节点专属 token</li>
  <li>启动 systemd 服务</li>
  <li>等待首次同步与绑定完成</li>
</ol>
```

**关键缺失**：
- ❌ **不包含任何模板变量替换**：没有把 `enrollment_token` / `server_url` 具体值填入
- ❌ 第 2 步 "写入该节点专属 token" 没有指导用户如何获取当前节点的 token（应该链接到上方 Token 区块或直接显示示例）
- ❌ 第 4 步没有说明预期等待时间

### 1.6 绑定冲突子流程

**现状**：
- 当 `binding_status === '指纹变更待确认'` 时，页面顶部显示冲突卡片（line 333-386）
- 卡片展示：当前指纹摘要 + 待确认指纹 + 首次出现 / 最近出现时间 + 尝试次数
- 三个动作按钮（确认 / 拒绝 / 重置），都通过 `handleBindingAction` 触发 API 调用
- 调用成功后刷新 onboarding 状态，失败显示错误信息

**现状评价**：
- ✅ 状态机与按钮逻辑完整
- ✅ 错误反馈清晰
- ❌ **没有确认弹窗或 ActionConfirmationCard**（rules-and-interaction.md 应要求 confirmation card）
- ❌ 没有"这个操作的影响"说明

---

## 2. 后端 Onboarding 数据契约

### 2.1 NodeOnboardingState 完整字段

源文件：`internal/center/nodes/types.go:106-114`

```go
type OnboardingState struct {
  Record  // 继承所有 NodeRecord 字段（node_id, display_name, region, city, provider, 
          // lifecycle_status, monitoring_status, binding_status, labels, note, 
          // current_health_status, last_heartbeat_at, last_sync_at, 
          // current_active_incident_count, current_primary_issue_summary, created_at, updated_at)

  Phase                            string                  `json:"phase"`
  HasHostSample                    bool                    `json:"has_host_sample"`
  HasAcceptedObservation           bool                    `json:"has_accepted_observation"`
  EnrollmentTokenIssuedAt          *time.Time              `json:"enrollment_token_issued_at,omitempty"`
  CurrentBindingFingerprintSummary string                  `json:"current_binding_fingerprint_summary,omitempty"`
  PendingBinding                   *PendingBindingMetadata `json:"pending_binding,omitempty"`
}

type PendingBindingMetadata struct {
  Fingerprint  string     `json:"fingerprint"`
  FirstSeenAt  *time.Time `json:"first_seen_at,omitempty"`
  LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
  AttemptCount int        `json:"attempt_count"`
}

type EnrollmentTokenIssue struct {
  Token    string    `json:"token"`
  IssuedAt time.Time `json:"issued_at"`
}
```

### 2.2 POST /api/nodes/{nodeId}/enrollment-token 返回结构

源文件：`internal/center/http/handlers/node_onboarding.go:39-64`

```go
func NodeEnrollmentToken(repo nodes.OnboardingRepository) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
      writeError(w, http.StatusMethodNotAllowed, "method not allowed")
      return
    }
    nodeID, ok := nodeActionNodeID(r.URL.Path, "/enrollment-token")
    if !ok {
      writeError(w, http.StatusNotFound, "node not found")
      return
    }
    issue, err := repo.IssueNodeEnrollmentToken(r.Context(), nodeID)
    // ...
    writeJSON(w, http.StatusOK, issue)  // 返回 EnrollmentTokenIssue 结构
  })
}
```

**返回示例** (gap #13 已验证):
```json
{
  "token": "enrollment_8a9...xyz",
  "issued_at": "2026-05-03T10:49:09Z"
}
```

### 2.3 Token TTL 与一次性语义

**关键发现**：
- ❌ **后端没有 token 过期时间 (expires_at) 字段**
- ❌ **token 没有 TTL 配置**（既无数据库字段，也无配置参数）
- ✅ Token **是一次性明文显示的**，但机制是：
  - 前端通过 localStorage 缓存 token + issued_at
  - 每次进入 onboarding 页面时，检查 cache 中 issued_at 是否与服务端返回的 enrollment_token_issued_at 匹配
  - 若不匹配（说明生成了新 token），清除缓存
  - 若匹配，继续显示缓存的明文
  - **离开页面 = 离开本次会话，localStorage 中的 token 仍存但页面不再展示**（属"会话级一次性"，非"系统级一次性"）

**跟踪源**：
- `web/src/lib/onboardingTokenCache.ts` — 提供 `setOnboardingTokenCache` / `getOnboardingTokenCache` / `clearOnboardingTokenCache` 三方法
- `web/src/pages/NodeOnboardingPage.tsx:161-177` — 缓存检查与清除逻辑

### 2.4 binding_status 与 pending_binding 字段语义

见 `internal/center/nodes/types.go:19-21`：

| 状态 | 含义 |
|------|------|
| `BindingUnbound` | 未绑定（新创建节点初始状态） |
| `BindingBound` | 已绑定（agent 使用有效 token + matching fingerprint 同步成功） |
| `BindingPendingConfirmation` | 指纹变更待确认（已绑定后收到不同 fingerprint 的 enroll 请求） |

**pending_binding** 仅在 `BindingPendingConfirmation` 状态下填充，包含待确认指纹的元数据。

### 2.5 后端缺失字段：server_url

**⚠️ 关键发现**：
- ❌ `NodeOnboardingState` 中**没有 `server_url` 字段**
- ❌ `EnrollmentTokenIssue` 中**也没有 `server_url` 字段**
- ❌ Agent 端如何知道要连接到哪个 houfeng center？目前通过 **agent 启动时的环境变量 `HOUFENG_SERVER_URL`** 配置
- 后端仅提供 token，不提供对应的 server_url

**设计问题**：
- 当一个用户有多个 houfeng center 实例时，安装步骤的"server_url"无法自动填充
- 必须让用户手动填写（可能填错）
- **建议后端在 `EnrollmentTokenIssue` 返回中添加 `server_url` 字段**（从环保变量或配置获取）

---

## 3. 视觉设计权威

### 3.1 component-spec.md § 五 NodeOnboardingPage 段落

直接摘自 `docs/design/v2-houfeng/component-spec.md:219-222`：

```
### NodeOnboardingPage
- 安全感重：cardRole='warning' ribbon top 用 critical
- token 一次性显示：mono + 复制按钮 + 倒计时 + 「已保存，关闭」按钮
- 关闭后无法再获取：用 dim card + critical 文案再次警示
```

**解读**：
1. **cardRole='warning' + critical ribbon**：整个 page 需要 warning 卡风格（虚线边、红色顶条）来强调安全相关
2. **Token 区块**：
   - `mono` 字体（技术 ID 类）
   - 三个操作：复制到剪贴板 + 倒计时计数 + 保存并关闭按钮
3. **已保存后**：Token 区块变成 `dim` card（弱化）+ critical 文案警示"已关闭后不再显示"

### 3.2 design-language.md 相关条款

#### § 6 状态语言

摘自 `docs/design/v2-houfeng/design-language.md:207-230`：

```
## 6. 状态语言

### 6.1 健康状态六态

| 状态 | 颜色 | StatusGlyph 形状 |
|---|---|---|
| 正常 normal | 松青 | 实心圆 ● |
| 关注 notice | 杏黄 | 半填圆 ◐ |
| 告警 alert | 朱砂 | 空心圆 + 下半填 |
| 严重 critical | 绛红 | 实心方 + 对角斜线 |
| 维护 maintenance | 烟蓝 | 空心方框 |
| 离线 offline | 远岚灰 | 虚线圆 |

### 6.3 状态优先级

视觉权重：critical > alert > notice > normal > maintenance > offline
```

**应用到 4-phase stepper**：
- 未开始接入 → normal (松青)
- 已绑定，等待稳定观测 → notice (杏黄)
- 接入完成 → normal / critical (取决于当前健康度)
- 绑定冲突待处理 → critical (绛红)

#### § 7 三态规范 (Loading / Error / Empty)

摘自 `docs/design/v2-houfeng/design-language.md:232-276`：

```
## 7. Loading / Error / Empty 三态规范

### 7.1 Loading
- 数据加载用 `surface` 底色 + 一行 mono 文案 `"正在加载…"` + 时间戳
- **不做骨架屏**
- **不用 spinner**

### 7.2 Error
- 用 `card--warning` 风格（虚线 critical 边 + critical-soft 背景）
- 三件必备：① 一句中文描述 ② mono 错误摘要 ③ 重试按钮（如果可重试）
- **不弹 toast**；错误就地展示

### 7.3 Empty
- 复用 `.empty-state` 类，升级为：① 居中 SVG 装饰 ② 一行解释 ③ 一个 ghost button CTA
```

**应用**：
- Token 未生成状态 → `.empty-state` 风格（已实现）
- Token 生成失败 → `card--warning` 错误卡（需新增）

### 3.3 rules-and-interaction.md 接入相关条款

摘自 `docs/design/v1-baseline/rules-and-interaction.md:360-368`（v1 baseline，v2 继承结构）：

```
## 7.1 Node 创建后的接入流程

### 创建后工作流

建议进入独立的**接入准备页 / 节点接入卡**，用于：

- 展示 token
- 展示绑定状态
- 展示接入指引
- 展示最近一次接入尝试状态
```

**现状符合**：NodeOnboardingPage 确实是"独立接入准备页"。

---

## 4. 共享原子能力盘点

### 4.1 已有 atoms（确认已实装）

```
✅ Card          — web/src/components/atoms/Card.tsx (85 行)
   - 四个 role：default / state / accent / warning（新增 dim）
   - ribbonPlacement: 'left' | 'top'（新增）
   
✅ Badge         — web/src/components/atoms/Badge.tsx (92 行)
   - 三变体：state / info / count
   - 七 tone：normal | notice | alert | critical | maintenance | offline | neutral
   
✅ Button        — web/src/components/atoms/Button.tsx (114 行)
   - 大小：sm / md / lg
   - 变体：primary / secondary / ghost / danger
   - 状态：disabled / loading
   
✅ StatusBadge   — web/src/components/StatusBadge.tsx (35 行)
   - Badge 的瘦包装，做 tone 映射（cyan→neutral / green→normal 等）
   
✅ DetailSection — web/src/components/DetailSection.tsx (60 行)
   - 结构：header (eyebrow + title + aside) + body
   - 新增 ribbon? prop（顶 hairline state 色）
```

### 4.2 缺失的原子（关键发现）

```
❌ Stepper / ProgressBar
   - 没有 4-phase 进度条组件
   - 需要新建 Stepper 原子或用 CSS grid 组装
   
❌ CountdownTimer / Timer
   - 没有倒计时组件
   - Token 倒计时需要新实现
   
✅ 复制到剪贴板工具函数
   - grep "clipboard|copy" 返回零
   - 需要实现 useClipboard hook 或 copyToClipboard 函数
   - 建议参考：navigator.clipboard.writeText() API
```

### 4.3 Mono 族原子（已实装，未全面落地）

```
✅ MonoDigits    — web/src/components/atoms/Mono.tsx
   <MonoDigits>{42.7}</MonoDigits>  → <span className="mono" style="tabular-nums">42.7</span>
   
✅ Hostname      — 同文件
   <Hostname truncate>{id}</Hostname>  → mono + 省略号 + hover tooltip
   
✅ Timestamp     — 同文件
   <Timestamp value={iso} mode="absolute|relative|both" />
   - absolute: "2026/04/30 15:33:21"
   - relative: "12 分钟前"
   - both: 默认 relative，hover 显 absolute
   
📍 Sparkline     — web/src/components/atoms/Sparkline.tsx (40 行)
   - 纯 SVG <polyline>，64×16px 默认
   - tone?: Tone（默认用 --text-secondary）
   - 末点加 1px 圆点锚点
   
📍 DataTable     — web/src/components/atoms/DataTable.tsx (60+ 行)
   - <table role="table"> 语义化
   - 紧凑 36px / 标准 44px 行高
   - thead bg-sidebar + eyebrow 风格列名
   - tr hover 显 surface-elevated
```

### 4.4 Token 一次性显示所需的新组件

基于 component-spec.md § 五的设计，需要：

1. **CopyButton 原子** — 单个"复制"图标按钮
   - 点击后 → 复制到剪贴板 + 显示 toast "已复制"
   
2. **CountdownTimer 原子** — 倒计时显示
   - props: `startTime: Date`, `durationSeconds: number`
   - 显示剩余秒数或"已过期"
   
3. **TokenBlock 组合组件** — 整合上述元素
   - 展示 `<MonoDigits>{token}</MonoDigits>` mono 字体
   - 并排 CopyButton + CountdownTimer
   - 底部 "已保存，关闭" 按钮
   
4. **PhaseIndicator 组合** — 4-phase stepper
   - 显示当前 phase 的进度条
   - 四个阶段标签 + 连接线 + 当前指示点

---

## 5. Token 安全约束与 Server_url 来源

### 5.1 Token 明文一次性约束的实现路径

**当前实现** (前端缓存策略)：
```typescript
// web/src/lib/onboardingTokenCache.ts
type TokenIssue = { token: string; issued_at: string }

function setOnboardingTokenCache(nodeId: string, issue: TokenIssue): void {
  const key = `houfeng.onboarding.token.${nodeId}`
  localStorage.setItem(key, JSON.stringify(issue))
}

function getOnboardingTokenCache(nodeId: string): TokenIssue | null {
  const key = `houfeng.onboarding.token.${nodeId}`
  const stored = localStorage.getItem(key)
  return stored ? JSON.parse(stored) : null
}
```

**流程**：
1. 用户点"重新生成 Token" → POST /api/nodes/{id}/enrollment-token → 前端缓存
2. 页面显示缓存的明文 token
3. 用户离开页面 → localStorage 仍有（但页面不再显示）
4. 用户重新进入 → 检查缓存 issued_at 与服务端 enrollment_token_issued_at：
   - 若匹配 → 显示缓存明文（同一会话继续有效）
   - 若不匹配 → 清除缓存（新 token 已生成，旧明文失效）

**安全评价**：
- ✅ localStorage 是浏览器 origin 级隔离（HTTPS + httpOnly cookie 更安全，但 token 本身不是 session cookie）
- ⚠️  Token 明文存储在 localStorage，恶意 script 可读取（需要 CSP 防护）
- ✅ 离开 onboarding 页后，虽然 localStorage 仍存，但**页面不再展示** → 符合设计"关闭后无法再获取"的意图

### 5.2 Server_url 配置来源

**后端**：`internal/center/config/config.go`

```go
const defaultHTTPAddr = ":8080"

func envOrDefault(env string, def string) (string, error) {
  if val, ok := os.LookupEnv(env); ok {
    return val, nil
  }
  return def, nil
}

// 在 Config 初始化时
httpAddr, err := envOrDefault("HOUFENG_HTTP_ADDR", defaultHTTPAddr)
```

**现状**：
- ❌ 后端**仅配置内部 HTTP listen 地址**（`:8080` 或从 env）
- ❌ **没有公网 URL / 反代 URL 的概念**
- ❌ API 返回给前端的数据中**没有 server_url**
- Agent 需要通过环境变量 `HOUFENG_SERVER_URL` 独立配置

**设计问题**：
- 当用户通过 NodeOnboardingPage 查看安装步骤时，无法自动填充"要连接到的 server URL"
- 必须让用户看文档后手动输入（e.g. `https://houfeng.example.com`）
- 多个 center 实例场景下容易出错

**建议改进**：
1. 在 center 配置中添加 `HOUFENG_PUBLIC_URL` (e.g. `https://houfeng.example.com`)
2. 在 `EnrollmentTokenIssue` 或 `OnboardingState` 返回中添加 `server_url` 字段
3. 前端安装步骤模板中自动填充该 URL

### 5.3 Token 与 Server_url 在安装步骤中的应用

**设计需求** (component-spec.md 隐含)：

安装步骤应改为：
```
1. 在服务器上安装 houfeng-agent binary
2. 创建 systemd 环境文件 /etc/houfeng-agent/agent.env，写入：
   HOUFENG_SERVER_URL=<server_url_from_page>
   HOUFENG_ENROLLMENT_TOKEN=<token_from_page>
3. 启动服务：systemctl enable --now houfeng-agent
4. 检查状态：systemctl status houfeng-agent
5. 返回本页面，等待首次同步（约 2-5 分钟）
```

**目前欠缺**：
- ❌ 没有模板变量替换（纯静态列表）
- ❌ 没有 server_url（后端未提供）
- ❌ 没有 token 复制快捷方式（可链接到上方 Token 区块）
- ❌ 没有预期等待时间提示

---

## 6. 关键实现清单（为改造做准备）

| 项目 | 当前状态 | 改造需求 | 优先级 |
|------|--------|--------|--------|
| **Stepper / ProgressBar** | ❌ 无 | 新建 Stepper 原子（4 步） | P0 |
| **CountdownTimer** | ❌ 无 | 新建 CountdownTimer 原子（或 hook） | P0 |
| **CopyButton / useClipboard** | ❌ 无 | 实现 navigator.clipboard 包装 | P0 |
| **Token 一次性显示** | ⚠️ 部分 | 加"已保存关闭"按钮 + 倒计时 + 复制 | P0 |
| **Warning ribbon card** | ✅ 已有 | Card cardRole='warning' ribbon='top' | P1 |
| **Dim card** | ✅ 已有 | Card cardRole='dim' 用于关闭后提示 | P1 |
| **Server_url 后端提供** | ❌ 无 | 修改 EnrollmentTokenIssue 结构 | P0 |
| **安装步骤模板变量** | ❌ 纯静态 | 改为动态内容插值 + markdown 渲染 | P1 |
| **Phase 进度可视化** | ⚠️ 纯文字 | 用 Stepper + 色彩表示 4 阶段 | P1 |
| **绑定冲突 ActionConfirmationCard** | ❌ 纯按钮 | 用 ActionConfirmationCard 包装 | P2 |

---

## 7. 小结

### 当前主要缺口

1. **进度可视化**：4-phase 没有 stepper，仅通过文字说明
2. **Token 安全 UX**：
   - 缺复制按钮（navigator.clipboard）
   - 缺倒计时（距生成多长时间 / 距过期剩余时间——需后端先提供 TTL）
   - 缺"保存后关闭"逻辑（需要关闭 token 显示 + 同时显示 dim card 警示）
3. **安装指引**：
   - 纯静态列表，不包含当前节点的 token 与 server_url
   - 需要后端在 EnrollmentTokenIssue 返回中添加 server_url
4. **设计与代码差距**：
   - component-spec.md § 五说的"warning ribbon + token mono + 复制按钮 + 倒计时 + 已保存关闭 + dim card"几乎全部缺失
   - 需要对标 v2 规范做全面改造

### 实施路径建议

1. **Phase 1（核心）**：
   - 后端添加 EnrollmentTokenIssue.ServerUrl 字段
   - 前端新建 Stepper 原子与 CountdownTimer hook
   - 实现 useClipboard hook
   - 改造 token 区块为 mono + 复制 + 倒计时 + 保存按钮
   - 实现 dim card 提示逻辑

2. **Phase 2（改进）**：
   - 安装步骤改为动态模板（纯 markdown 或自定义 JSX）
   - Phase 指示器用 Stepper 替代纯文字
   - 绑定冲突卡片用 ActionConfirmationCard 重构

3. **Phase 3（可选）**：
   - 添加更详细的错误说明与排查指南
   - 支持多种安装方式（systemd / docker / manual）

---

**报告完成日期**: 2026-05-03  
**研究范围**: 前端 445 行 + 后端 types/handler + 设计文档 2 份 + 组件库存盘  
**关键输出**: 4 个 P0 缺口（stepper / countdown / clipboard / server_url）+ 改造优先级矩阵
