# Gotify Webhook Forwarder Plugin（透明代理）

一个 Gotify 插件，实现两个功能
1：作为 **透明代理** 将 Webhook 消息转发到企业微信、钉钉、飞书或自定义 Webhook；
2：gotify兼容企业微信、钉钉、飞书或自定义 Webhook，gotify自己接受消息；

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

路由: `POST /receive?platform=<platform>&token=<secret>`

## 使用示例

### 转发企业微信消息（原生格式）

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/wecom' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自 Gotify 的消息"}}'
```

### 转发企业微信 Markdown

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/wecom' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"markdown","markdown":{"content":"### 告警\n> CPU 使用率超过 90%"}}'
```

### 转发钉钉消息（签名自动添加）

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/dingtalk' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自 Gotify 的消息"}}'
```

### 转发飞书消息（签名自动注入）

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/send/feishu' \
  -H 'Content-Type: application/json' \
  -d '{"msg_type":"text","content":{"text":"来自 Gotify 的消息"}}'
```

### 接收入站消息

```bash
curl -X POST 'http://gotify:8080/plugin/<token>/receive?platform=wecom&token=YOUR_SECRET' \
  -H 'Content-Type: application/json' \
  -d '{"msgtype":"text","text":{"content":"来自企业微信的消息"}}'
```

## 插件配置

配置通过 Gotify 管理界面编辑，示例：

```yaml
targets:
  - name: "我的企微群(必须英文)"
    platform: "wecom"
    webhook_url: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=YOUR_KEY"
    enabled: true
  - name: "我的钉钉群(必须英文)"
    platform: "dingtalk"
    webhook_url: "https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN"
    secret: "SECxxx"
    enabled: true
  - name: "我的飞书群(必须英文)"
    platform: "feishu"
    webhook_url: "https://open.feishu.cn/open-apis/bot/v2/hook/YOUR_HOOK_ID"
    secret: "your_secret"
    enabled: true
  - name: 自定义(必须英文)
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
```

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| POST | `/test` | 向所有已启用目标发送测试消息 |
| POST | `/send/:platform` | 透明转发到指定平台（wecom/dingtalk/feishu/custom） |
| POST | `/receive?platform=<p>` | 接收外部平台消息转存为 Gotify 消息 |

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
├── sender.go       # 透明代理转发（ForwardRaw）+ 签名
├── signer.go       # 钉钉/飞书/自定义 HMAC 签名
├── receiver.go     # 入站消息解析（外部 → Gotify）
├── plugin_test.go  # 单元测试
├── go.mod
├── Makefile
└── README.md
```
