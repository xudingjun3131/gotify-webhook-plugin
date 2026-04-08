# Gotify Webhook Forwarder Plugin（多通道路由 / 透明代理）

一个 Gotify 插件，提供两类能力：

1. **出站通知**：把外部系统传入的原始消息转发到第三方通知平台
2. **入站通知**：接收第三方平台或自定义系统的 webhook，并转存为 Gotify 消息

当前支持的平台包括：

- 企业微信
- 钉钉
- 飞书
- Telegram Bot
- Email
- Amazon SNS
- 阿里云短信
- 腾讯云短信
- Discord
- Slack
- 自定义 Webhook

其中：

- **Webhook / Bot API 型平台**：企业微信、钉钉、飞书、Telegram、Discord、Slack、自定义
- **专用发送器型平台**：Email、Amazon SNS、阿里云短信、腾讯云短信

当入站消息被识别为 Markdown 且内容中包含 HTML 时，插件会自动执行 HTML → Markdown 转换，并利用 Gotify 的 Markdown 渲染能力展示。

## 核心设计

插件默认遵循 **不做格式转换、尽量原样转发** 的原则。

对于 Webhook / Bot API 型平台，插件只做这些事情：

1. 读取原始请求体
2. 按目标平台补签名或 header
3. 原样转发到目标平台

例如：

- [`dingtalk`](plugin.go:83) 自动追加签名参数到 URL
- [`feishu`](plugin.go:90) 自动把签名注入 JSON body
- [`telegram`](plugin.go:97) 可自动附加 `X-Telegram-Bot-Api-Secret-Token`
- [`discord`](plugin.go:142) / [`slack`](plugin.go:149) 可自动附加 `X-Webhook-Token`
- [`custom`](plugin.go:156) 可附加 `X-Signature`

对于 Email / SNS / 短信类平台，插件会调用专用发送器：

- SMTP / SMTPS 发邮件
- AWS SNS SDK 发主题或短信
- 阿里云短信 SDK 发短信
- 腾讯云短信 SDK 发短信

## 功能说明

### 出站转发（→ 外部平台）

| 平台 | `platform` 参数 | 发送模式 | 备注 |
|------|------------------|----------|------|
| 企业微信 | `wecom` | Webhook | 原样转发 |
| 钉钉 | `dingtalk` | Webhook | 自动加签 |
| 飞书 | `feishu` | Webhook | 自动签名注入 body |
| Telegram | `telegram` | Bot API / Webhook | 可自动附加 secret header |
| Discord | `discord` | Webhook | 可附加 `X-Webhook-Token` |
| Slack | `slack` | Webhook | 可附加 `X-Webhook-Token` |
| Email | `email` | SMTP / SMTPS | 支持 `text/plain` / `text/html` |
| Amazon SNS | `sns` | AWS SDK | 支持 Topic ARN 或短信号码 |
| 阿里云短信 | `aliyun-sms` | SDK | 通过模板发送 |
| 腾讯云短信 | `tencent-sms` | SDK | 通过模板发送 |
| 自定义 | `custom` | HTTP | 支持自定义 header 与签名 |

### 入站接收（外部平台 → Gotify）

统一路由：

```text
POST /receive?platform=<platform>&token=<secret>
```

支持的入站平台：

- `wecom`
- `dingtalk`
- `feishu`
- `telegram`
- `email`
- `sns`
- `aliyun-sms`
- `tencent-sms`
- `discord`
- `slack`
- `custom`

说明：

- 钉钉 / 飞书沿用其各自签名校验逻辑
- 其他平台支持通过 URL 参数 `token` 或约定 header 进行校验
- Telegram 入站支持 `X-Telegram-Bot-Api-Secret-Token`
- Email / SNS / Discord / Slack / 短信类入站支持 `X-Webhook-Token`

## 使用示例

### 出站：Webhook / Bot API 型平台

**企业微信：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/wecom/wecom-group1' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自 Gotify 的消息"}}'
```

**钉钉：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/dingtalk/ops-alert' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自 Gotify 的消息"}}'
```

**飞书：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/feishu/dev-notify' \
  -H 'Content-Type: application/json' \
  -d '{"msg_type":"text","content":{"text":"来自 Gotify 的消息"}}'
```

**Telegram Bot：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/telegram/tg-alerts' \
  -H 'Content-Type: application/json' \
  -d '{"chat_id":"123456789","text":"来自 Gotify 的 Telegram 消息"}'
```

**Discord：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/discord/discord-alerts' \
  -H 'Content-Type: application/json' \
  -d '{"content":"来自 Gotify 的 Discord 消息"}'
```

**Slack：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/slack/slack-alerts' \
  -H 'Content-Type: application/json' \
  -d '{"text":"来自 Gotify 的 Slack 消息"}'
```

### 出站：专用发送器型平台

**Email：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/email/smtp-alerts' \
  -H 'Content-Type: application/json' \
  -d '{"title":"数据库告警","message":"主库延迟超过阈值"}'
```

如果直接发送 HTML：

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/email/smtp-alerts' \
  -H 'Content-Type: text/html' \
  -d '<h1>数据库告警</h1><p>主库延迟超过 <b>10s</b></p>'
```

**Amazon SNS：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/sns/aws-sns-topic' \
  -H 'Content-Type: application/json' \
  -d '{"subject":"Gotify Alert","message":"Amazon SNS 推送测试"}'
```

**阿里云短信：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/aliyun-sms/aliyun-sms-alerts' \
  -H 'Content-Type: application/json' \
  -d '{"message":"【Gotify】服务告警：CPU 使用率超过阈值"}'
```

**腾讯云短信：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/tencent-sms/tencent-sms-alerts' \
  -H 'Content-Type: application/json' \
  -d '{"message":"【Gotify】服务告警：CPU 使用率超过阈值"}'
```

### 入站接收

**企业微信 → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=wecom&token=YOUR_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自企微的消息"}}'
```

**钉钉 → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=dingtalk' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自钉钉的消息"}}'
```

**飞书 → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=feishu' \
  -H 'Content-Type: application/json' \
  -d '{"msg_type":"text","content":{"text":"来自飞书的消息"}}'
```

**Telegram → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=telegram' \
  -H 'Content-Type: application/json' \
  -H 'X-Telegram-Bot-Api-Secret-Token: YOUR_TELEGRAM_WEBHOOK_SECRET' \
  -d '{"message":{"text":"来自 Telegram 的消息","chat":{"title":"报警群"}}}'
```

**Email → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=email&token=YOUR_EMAIL_RECEIVE_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"from":"noreply@example.com","subject":"巡检报告","text":"巡检通过"}'
```

**Amazon SNS → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=sns&token=YOUR_SNS_RECEIVE_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"Type":"Notification","Subject":"Cloud Alarm","Message":"CPU usage high","TopicArn":"arn:aws:sns:..."}'
```

**Discord → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=discord&token=YOUR_DISCORD_WEBHOOK_SECRET' \
  -H 'Content-Type: application/json' \
  -d '{"content":"来自 Discord 的消息","username":"alert-bot"}'
```

**Slack → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=slack&token=YOUR_SLACK_WEBHOOK_SECRET' \
  -H 'Content-Type: application/json' \
  -d '{"text":"来自 Slack 的消息","channel_name":"alerts","username":"bot"}'
```

**自定义 JSON → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=custom&token=YOUR_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"title":"告警标题","message":"告警详情内容","priority":5}'
```

**自定义 HTML → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=custom&token=YOUR_TOKEN' \
  -H 'Content-Type: text/html' \
  -d '<h1>告警</h1><p>CPU 使用率 <b>99%</b></p>'
```

## HTML → Markdown 自动转换

仅当消息被识别为 Markdown 且 [`html2md.enabled`](config.go) 为 `true` 时，插件才会检测 HTML 并自动转换为 Markdown。

适用场景：

- 企业微信 markdown
- 钉钉 markdown
- 飞书 markdown
- 自定义 markdown
- Email / Telegram / Slack / Discord 等入站场景中被识别为 markdown 的 HTML 内容

说明：

- 纯文本不做转换
- 插件会尽量从 HTML 的 `<title>` 或 `<h1>` 中提取标题
- Gotify 前端会通过 extras 使用 Markdown 渲染

## 插件配置

配置通过 Gotify 管理界面编辑。下面给出一个覆盖全部主要平台的示例：

```yaml
targets:
  - name: "my-wecom-group"
    platform: "wecom"
    webhook_url: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=YOUR_KEY"
    enabled: true

  - name: "my-dingtalk-group"
    platform: "dingtalk"
    webhook_url: "https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN"
    secret: "SECxxx"
    enabled: true

  - name: "my-feishu-group"
    platform: "feishu"
    webhook_url: "https://open.feishu.cn/open-apis/bot/v2/hook/YOUR_HOOK_ID"
    secret: "your_secret"
    enabled: true

  - name: "tg-alerts"
    platform: "telegram"
    webhook_url: "https://api.telegram.org/bot<token>/sendMessage"
    secret: "YOUR_TELEGRAM_WEBHOOK_SECRET"
    enabled: true

  - name: "discord-alerts"
    platform: "discord"
    webhook_url: "https://discord.com/api/webhooks/WEBHOOK_ID/WEBHOOK_TOKEN"
    secret: "YOUR_DISCORD_WEBHOOK_SECRET"
    enabled: false

  - name: "slack-alerts"
    platform: "slack"
    webhook_url: "https://hooks.slack.com/services/T000/B000/XXXX"
    secret: "YOUR_SLACK_WEBHOOK_SECRET"
    enabled: false

  - name: "smtp-alerts"
    platform: "email"
    webhook_url: "smtp://user:password@smtp.example.com:587"
    email_from: "gotify@example.com"
    email_to:
      - "ops@example.com"
      - "dev@example.com"
    email_subject: "Gotify Alert"
    enabled: false

  - name: "aws-sns-topic"
    platform: "sns"
    topic_arn: "arn:aws:sns:ap-southeast-1:123456789012:alerts"
    region: "ap-southeast-1"
    subject: "Gotify Alert"
    enabled: false

  - name: "aliyun-sms-alerts"
    platform: "aliyun-sms"
    secret: "ACCESS_KEY_ID:ACCESS_KEY_SECRET"
    phone_numbers:
      - "13800138000"
    template_code: "SMS_123456789"
    sign_name: "YourSign"
    region: "cn-hangzhou"
    enabled: false

  - name: "tencent-sms-alerts"
    platform: "tencent-sms"
    secret: "SECRET_ID:SECRET_KEY"
    sms_app_id: "1400006666"
    phone_numbers:
      - "+8613800138000"
    template_code: "1234567"
    sign_name: "YourSign"
    region: "ap-guangzhou"
    enabled: false

  - name: "my-custom"
    platform: "custom"
    webhook_url: "https://example.com/webhook"
    secret: "YOUR_CUSTOM_SECRET"
    enabled: false
    method: "POST"
    headers:
      X-Custom-Header: "value"

incoming:
  enabled: true
  secret: "global_secret"
  platforms:
    wecom:
      enabled: true
      secret: ""
    dingtalk:
      enabled: true
      secret: ""
    feishu:
      enabled: true
      secret: ""
    telegram:
      enabled: true
      secret: "YOUR_TELEGRAM_WEBHOOK_SECRET"
    email:
      enabled: true
      secret: "YOUR_EMAIL_RECEIVE_TOKEN"
    sns:
      enabled: true
      secret: "YOUR_SNS_RECEIVE_TOKEN"
    aliyun-sms:
      enabled: true
      secret: "YOUR_ALIYUN_SMS_RECEIVE_TOKEN"
    tencent-sms:
      enabled: true
      secret: "YOUR_TENCENT_SMS_RECEIVE_TOKEN"
    discord:
      enabled: true
      secret: "YOUR_DISCORD_WEBHOOK_SECRET"
    slack:
      enabled: true
      secret: "YOUR_SLACK_WEBHOOK_SECRET"
    custom:
      enabled: true
      secret: ""

html2md:
  enabled: true
```

## 主要配置字段说明

### 通用字段

| 字段 | 说明 |
|------|------|
| `name` | 目标名称，必须 URL 安全 |
| `platform` | 平台标识 |
| `webhook_url` | Webhook / API 地址 |
| `secret` | 平台签名密钥、Webhook Secret，或云厂商凭据 |
| `enabled` | 是否启用 |
| `method` | 自定义 HTTP 方法，默认 `POST` |
| `headers` | 自定义请求头，仅自定义平台主要使用 |

### Email 专用字段

| 字段 | 说明 |
|------|------|
| `email_to` | 收件人列表 |
| `email_subject` | 固定邮件标题 |
| `email_from` | 发件人地址 |

说明：

- [`email`](config.go) 使用 [`smtp://`](sender.go) 或 [`smtps://`](sender.go) 作为 [`webhook_url`](config.go)
- 用户名密码从 URL userinfo 中读取
- 邮件正文会根据内容自动判断为纯文本或 HTML

### Amazon SNS 专用字段

| 字段 | 说明 |
|------|------|
| `topic_arn` | SNS Topic ARN |
| `region` | AWS 区域 |
| `subject` | SNS Subject |
| `phone_numbers` | 可选，若不发 Topic，可发到手机号 |

### 阿里云短信专用字段

| 字段 | 说明 |
|------|------|
| `secret` | `ACCESS_KEY_ID:ACCESS_KEY_SECRET` |
| `phone_numbers` | 短信接收号码 |
| `template_code` | 短信模板编码 |
| `sign_name` | 短信签名 |
| `region` | 地域，用于 endpoint |

说明：

- 当前模板参数中会注入 `content`
- 建议在短信模板中预留一个内容变量用于承接 Gotify 消息摘要

### 腾讯云短信专用字段

| 字段 | 说明 |
|------|------|
| `secret` | `SECRET_ID:SECRET_KEY` |
| `sms_app_id` | 腾讯云短信 `SmsSdkAppId` |
| `phone_numbers` | 短信接收号码 |
| `template_code` | 模板 ID |
| `sign_name` | 短信签名 |
| `region` | 地域 |

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 健康检查 |
| `POST` | `/test` | 向所有已启用目标发送测试消息 |
| `POST` | `/test/:name` | 测试指定目标 |
| `POST` | `/send/:platform` | 广播到指定平台全部已启用目标 |
| `POST` | `/send/:platform/:name` | 发送到指定平台的指定目标 |
| `POST` | `/receive?platform=<p>` | 接收外部平台消息并转存为 Gotify 消息 |

## Name 命名规则

目标名必须是 URL 安全字符串，仅允许：

- `a-z`
- `A-Z`
- `0-9`
- `_`
- `-`
- `.`

## 编译

```bash
make update-go-mod
make build-linux-amd64
go build -buildmode=plugin -o build/gotify-webhook-plugin-linux-amd64.so .
```

指定 Gotify 版本：

```bash
make update-go-mod GOTIFY_VERSION=v2.9.0
make build-linux-amd64 GOTIFY_VERSION=v2.9.0
go build -buildmode=plugin -o build/gotify-webhook-plugin-linux-amd64.so .
```

需要 Docker。编译产物为 `build/gotify-webhook-plugin-linux-amd64.so`，放入 Gotify 的 `plugins/` 目录后重启 Gotify 即可。

## 文件结构

```text
gotify-webhook-plugin/
├── plugin.go       # 插件主体：路由、配置、展示
├── config.go       # 配置结构体
├── converter.go    # HTML → Markdown 转换与检测逻辑
├── sender.go       # 出站转发 / Email / SNS / 短信发送器
├── signer.go       # 钉钉 / 飞书 / 自定义签名
├── receiver.go     # 入站消息解析（外部 → Gotify）
├── plugin_test.go  # 单元测试
├── go.mod
├── Makefile
└── README.md
