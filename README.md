# Gotify Webhook Forwarder Plugin（透明代理）

一个 Gotify 插件，实现两大功能：
1. 作为 **透明代理** 将 Webhook 消息转发到企业微信、钉钉、飞书或自定义 Webhook
2. 接收企业微信、钉钉、飞书或自定义 Webhook 消息，转存为 Gotify 消息

每个平台支持 **text** 和 **markdown** 两种格式。当消息类型为 markdown 时，插件自动检测 HTML 并转为 Markdown，利用 Gotify 前端原生渲染。

## 核心设计

**插件不做任何格式转换**。请求体格式必须与目标平台的 webhook 接口要求完全一致，插件仅：

1. 读取原始请求体
2. 按需添加签名（钉钉加签、飞书签名）
3. 原样转发到目标平台

> 第三方系统只需将 webhook URL 从直接指向企微/钉钉/飞书，改为指向本插件的 `/send/<platform>` 端点即可。

## 功能
### 出站转发（→ 外部平台）

| 平台 | 路由 | 签名方式 |
|------|------|----------|
| 企业微信 | `/send/wecom/webhook1` | 无需签名 |
| 钉钉 | `/send/dingtalk/webhook1` | HMAC-SHA256 加签（自动追加到 URL） |
| 飞书 | `/send/feishu/webhook1` | HMAC-SHA256 签名（自动注入到 JSON body） |
| 自定义 | `/send/custom/webhook1` | X-Signature header（可选） |

### 入站接收（外部平台 → Gotify）

接收来自企微/钉钉/飞书/自定义格式的 Webhook 消息，解析后转存为 Gotify 消息。
每个平台支持 **text** 和 **markdown** 两种消息格式。
当 `msgtype/msg_type = markdown` 且 `html2md.enabled = true` 时，内容中的 HTML 自动转为 Markdown。

路由: `POST /receive?platform=<platform>&token=<secret>`

## 使用示例

### 出站转发

```bash
# 转发企业微信消息（原生格式）
curl -X POST 'http://gotify:8080/plugin/<token>/send/wecom/wecom-group1' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自 Gotify 的消息"}}'

# 转发企业微信 Markdown
curl -X POST 'http://gotify:8080/plugin/<token>/send/wecom/wecom-group1' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"markdown","markdown":{"content":"### 告警\n> CPU 使用率超过 90%"}}'

# 转发钉钉消息（签名自动添加）
curl -X POST 'http://gotify:8080/plugin/<token>/send/dingtalk/ops-alert' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自 Gotify 的消息"}}'

# 转发飞书消息（签名自动注入）
curl -X POST 'http://gotify:8080/plugin/<token>/send/feishu/dev-notify' \
  -H 'Content-Type: application/json' \
  -d '{"msg_type":"text","content":{"text":"来自 Gotify 的消息"}}'
```

### 入站接收

**企业微信 → Gotify（token 校验）：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=wecom&token=YOUR_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自企微的消息"}}'
```

**企业微信 Markdown → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=wecom&token=YOUR_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"markdown","markdown":{"content":"### 告警\n> CPU 超过 90%%"}}'
```

**钉钉 Text → Gotify（签名验证）：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=dingtalk' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自钉钉的消息"}}'
```

**钉钉 Markdown → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=dingtalk' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"markdown","markdown":{"title":"监控告警","text":"### CPU 告警\n使用率超过 **90%**"}}'
```

**飞书 Text → Gotify（签名在 body 中）：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=feishu' \
  -H 'Content-Type: application/json' \
  -d '{"msg_type":"text","content":{"text":"来自飞书的消息"}}'
```

**飞书 Markdown → Gotify：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=feishu' \
  -H 'Content-Type: application/json' \
  -d '{"msg_type":"markdown","content":{"text":"### 告警\n> CPU 超过 90%%"}}'
```

**自定义 Text → Gotify（token 校验）：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=custom&token=YOUR_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"title":"告警标题","message":"告警详情内容","priority":5}'
```

**自定义 Markdown → Gotify（含 HTML 自动转换）：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=custom&token=YOUR_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"markdown","title":"监控告警","message":"<h1>CPU 告警</h1><p>使用率 <b>99%</b></p>","priority":5}'
```

**自定义 — 纯 HTML Body：**

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=custom&token=YOUR_TOKEN' \
  -H 'Content-Type: text/html' \
  -d '<h1>告警</h1><p>CPU 使用率 <b>99%</b></p>'
```

### HTML 自动转换说明

**仅当 `msgtype` 为 `markdown` 时**，插件才检测消息中的 HTML 标签并自动转为 Markdown。
`text` 格式的消息不做任何转换。

> 插件会自动从 HTML 中提取 `<title>` 或 `<h1>` 作为消息标题，也可以通过 JSON 的 `title` 字段手动指定。

## 插件配置

配置通过 Gotify 管理界面编辑，示例：

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
  - name: "my-custom"
    platform: custom
    webhook_url: https://example.com/webhook
    enabled: false
    method: POST
incoming:
  enabled: true
  secret: "global_secret"
  platforms:
    custom:
      enabled: true
      secret: ""
    wecom:
      enabled: true
      secret: ""
    dingtalk:
      enabled: true
      secret: ""
    feishu:
      enabled: true
      secret: ""
html2md:
  enabled: true    # 启用入站消息 HTML 自动检测转换
```

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| POST | `/test` | 向所有已启用目标发送测试消息 |
| POST | `/send/:platform` | 透明转发到指定平台（wecom/dingtalk/feishu/custom） |
| POST | `/send/:platform/:name` | 透明转发到指定平台的指定目标 |
| POST | `/receive?platform=<p>` | 接收外部平台消息转存为 Gotify 消息（支持 HTML 自动转 MD） |

## 编译

```bash
make update-go-mod  # 拉取最新的代码
make build-linux-amd64 # docker编译
go build -buildmode=plugin -o build/gotify-webhook-plugin-linux-amd64.so . # 本地编译
```

```bash
make update-go-mod GOTIFY_VERSION=v2.9.0 # 获取指定版本
make build-linux-amd64 GOTIFY_VERSION=v2.9.0 # docker编译指定版本
go build -buildmode=plugin -o build/gotify-webhook-plugin-linux-amd64.so . # 本地编译
```

需要 Docker。编译产物为 `build/gotify-webhook-plugin-linux-amd64.so` 文件，放入 Gotify 的 `plugins/` 目录即可，注意gotify需要重启。

## 文件结构

```
gotify-webhook-plugin/
├── plugin.go       # 插件主体：路由、配置、显示
├── config.go       # 配置结构体
├── converter.go    # HTML → Markdown 转换与检测逻辑
├── sender.go       # 透明代理转发（ForwardRaw）+ 签名
├── signer.go       # 钉钉/飞书/自定义 HMAC 签名
├── receiver.go     # 入站消息解析（外部 → Gotify）
├── plugin_test.go  # 单元测试
├── go.mod
├── Makefile
└── README.md
```
