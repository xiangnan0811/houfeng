# Stage 2 Phase 6: 多通知通道（飞书 Webhook）

## Goal

加飞书群机器人 Webhook 作为第二个通知通道。Settings 中配置，incident 推送时间到飞书群里。

## Background

- 已有 `Notifier` interface（`Send(ctx, summary) error`）+ `SettingsAwareNotifier`（运行时读 settings）
- Telegram 已实现（HTTP POST to Telegram Bot API）
- 飞书群机器人更简单：只需 POST JSON 到 webhook URL

## Requirements

### 1. 新建 `internal/center/notify/feishu.go`

```go
type FeishuNotifier struct {
    webhookURL string
    client     *http.Client
}

func NewFeishuNotifier(webhookURL string) *FeishuNotifier {
    return &FeishuNotifier{webhookURL: webhookURL, client: &http.Client{Timeout: 10 * time.Second}}
}

func (n *FeishuNotifier) Send(ctx context.Context, summary string) error {
    body := map[string]interface{}{
        "msg_type": "text",
        "content": map[string]string{
            "text": summary,
        },
    }
    // POST JSON to webhookURL
    // Return error on non-2xx
}
```

### 2. Settings 扩展

`internal/center/settings/types.go` `TelegramSettings` 旁边加：

```go
type FeishuSettings struct {
    Enabled     bool   `json:"enabled"`
    WebhookURL  string `json:"webhook_url"`
}

type NotificationSettings struct {
    Telegram TelegramSettings `json:"telegram"`
    Feishu   FeishuSettings   `json:"feishu"`
}
```

将 `TelegramSettings` 移到 `NotificationSettings.Telegram` 下（如果当前是顶层字段需 refactor——**尽量不改已有结构，加 NotificationSettings wrapper**）。

更简单：直接在已有结构旁边加 `FeishuEnabled / FeishuWebhookURL` 字段，不动 Telegram 已有结构。

**最终方案**：CenterSettings 加两个顶层字段 `feishu_enabled bool` + `feishu_webhook_url string`。跟现有 Telegram 字段并列，不做 wrapper refactor。

### 3. SettingsAwareNotifier 扩展

`sendWithTelegramSettings` → 改为 `sendToAllChannels`：

- 遍历已启用的通道：Telegram（如 token+chatID 具备）→ 发送；Feishu（如 enabled+webhookURL 具备）→ 发送
- 任一通道失败不 block 其他通道
- 没有可用通道时 silent no-op

### 4. 前端 SettingsPage

Telegram section 旁边加 Feishu section（DetailSection ribbon="accent-2"）：
- Toggle "启用飞书通知"
- Input "Webhook URL"
- 状态卡："已配置 · 最后一次发送: X"（复用 Telegram 同款模式）

### 5. 测试

- `feishu_test.go`：正常发送 / 错误状态码 / 网络超时
- settings 单测默认 Feishu 字段

## Acceptance Criteria

- [ ] FeishuNotifier 实现 Send 接口
- [ ] Settings 含 feishu Webhook 配置
- [ ] incident 发生时同时推送 Telegram + Feishu
- [ ] 单通道失败不 block 其他
- [ ] lint / test / build 全绿（基线 392）
- [ ] Go test 全绿

## Out of Scope

- 邮件 / 企微 / 钉钉（后续加，同款模式）
- 消息模板定制

## Technical Approach

单 PR。~4 文件（feishu.go + types.go + service.go + SettingsPage.tsx）。
