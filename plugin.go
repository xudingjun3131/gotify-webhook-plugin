package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	plugin "github.com/gotify/plugin-api"
)

// namePattern validates that target names are URL-safe (alphanumeric, hyphens, underscores, dots).
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

// GetGotifyPluginInfo returns gotify plugin info.
func GetGotifyPluginInfo() plugin.Info {
	return plugin.Info{
		ModulePath:  "github.com/gotify/gotify-webhook-plugin",
		Version:     "1.0.0",
		Author:      "Gotify Webhook Plugin",
		Website:     "https://github.com/gotify/gotify-webhook-plugin",
		Description: "透明代理 Webhook 插件：原样转发消息到企业微信、钉钉、飞书或自定义 Webhook",
		License:     "MIT",
		Name:        "Webhook Forwarder",
	}
}

// WebhookPlugin is the gotify plugin instance.
type WebhookPlugin struct {
	config         *Config
	sender         *Sender
	receiver       *Receiver
	msgHandler     plugin.MessageHandler
	basePath       string
	enabled        bool
	userCtx        plugin.UserContext
	storageHandler plugin.StorageHandler
}

// Enable enables the plugin.
func (p *WebhookPlugin) Enable() error {
	p.enabled = true
	log.Printf("[webhook-plugin] Plugin enabled for user %s (ID: %d)", p.userCtx.Name, p.userCtx.ID)
	return nil
}

// Disable disables the plugin.
func (p *WebhookPlugin) Disable() error {
	p.enabled = false
	log.Printf("[webhook-plugin] Plugin disabled for user %s (ID: %d)", p.userCtx.Name, p.userCtx.ID)
	return nil
}

// SetMessageHandler implements plugin.Messenger.
func (p *WebhookPlugin) SetMessageHandler(h plugin.MessageHandler) {
	p.msgHandler = h
}

// SetStorageHandler implements plugin.Storager.
func (p *WebhookPlugin) SetStorageHandler(h plugin.StorageHandler) {
	p.storageHandler = h
}

// DefaultConfig implements plugin.Configurer.
func (p *WebhookPlugin) DefaultConfig() interface{} {
	return &Config{
		// ===== 出站目标（透明代理转发） =====
		// 每个平台可配置多个目标，通过 name 区分
		// 出站 URL: POST /send/<platform>/<name>
		// 示例: POST /send/wecom/wecom-ops   → 转发到企微运维群
		//       POST /send/wecom/wecom-dev   → 转发到企微开发群
		//       POST /send/dingtalk/dt-ops   → 转发到钉钉运维群
		//       POST /send/wecom            → 广播到所有已启用的企微目标
		Targets: []TargetConfig{
			{
				Name:       "wecom-ops",
				Platform:   "wecom",
				WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=YOUR_KEY_1",
				Enabled:    false,
			},
			{
				Name:       "wecom-dev",
				Platform:   "wecom",
				WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=YOUR_KEY_2",
				Enabled:    false,
			},
			{
				Name:       "dt-ops",
				Platform:   "dingtalk",
				WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN",
				Secret:     "SEC_YOUR_DINGTALK_SECRET",
				Enabled:    false,
			},
			{
				Name:       "fs-dev",
				Platform:   "feishu",
				WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/YOUR_HOOK_ID",
				Secret:     "YOUR_FEISHU_SECRET",
				Enabled:    false,
			},
			{
				Name:       "my-custom",
				Platform:   "custom",
				WebhookURL: "https://example.com/webhook",
				Secret:     "YOUR_CUSTOM_SECRET",
				Enabled:    false,
				Method:     "POST",
				Headers:    map[string]string{"X-Custom-Header": "value"},
			},
		},
		// ===== 入站接收（外部 → Gotify） =====
		// 接收外部平台原生格式的消息，解析后存为 Gotify 消息
		// 入站 URL: POST /receive?platform=<platform>&token=<secret>
		// 示例:
		//   企业微信: POST /receive?platform=wecom&token=YOUR_WECOM_TOKEN
		//     Body: {"msgtype":"text","text":{"content":"来自企微的消息"}}
		//   钉钉:    POST /receive?platform=dingtalk
		//     Body: {"msgtype":"text","text":{"content":"来自钉钉的消息"}}
		//     (钉钉使用 URL 参数 timestamp+sign 验签，无需 token)
		//   飞书:    POST /receive?platform=feishu
		//     Body: {"msg_type":"text","content":{"text":"来自飞书的消息"},"timestamp":"...","sign":"..."}
		//     (飞书签名在 body 中，无需 token)
		//   自定义:  POST /receive?platform=custom&token=YOUR_CUSTOM_TOKEN
		//     Body: {"title":"标题","message":"内容","priority":5}
		Incoming: IncomingConfig{
			Enabled: true,
			Secret:  "",
			Platforms: map[string]PlatformReceiveConfig{
				"wecom":    {Enabled: true, Secret: "YOUR_WECOM_RECEIVE_TOKEN"},
				"dingtalk": {Enabled: true, Secret: "YOUR_DINGTALK_RECEIVE_SECRET"},
				"feishu":   {Enabled: true, Secret: "YOUR_FEISHU_RECEIVE_SECRET"},
				"custom":   {Enabled: true, Secret: "YOUR_CUSTOM_RECEIVE_TOKEN"},
			},
		},
		// ===== HTML → Markdown 自动转换 =====
		// 当入站消息（企微/钉钉/飞书/自定义）包含 HTML 内容时，
		// 自动检测并转为 Markdown，利用 Gotify 前端原生渲染
		HTML2MD: HTML2MDConfig{
			Enabled: true,
		},
	}
}

// ValidateAndSetConfig implements plugin.Configurer.
func (p *WebhookPlugin) ValidateAndSetConfig(c interface{}) error {
	config, ok := c.(*Config)
	if !ok {
		return fmt.Errorf("invalid config type")
	}

	// 检查 name 唯一性（同平台下不能重复）
	nameMap := make(map[string]bool) // key: "platform/name"

	for i, target := range config.Targets {
		if target.Name == "" {
			return fmt.Errorf("target #%d: name is required", i+1)
		}
		if !namePattern.MatchString(target.Name) {
			return fmt.Errorf("target #%d: name '%s' is invalid, only alphanumeric, hyphen, underscore, dot allowed", i+1, target.Name)
		}
		if target.WebhookURL == "" {
			return fmt.Errorf("target %s: webhook_url is required", target.Name)
		}
		validPlatform := false
		for _, p := range ValidPlatforms {
			if target.Platform == p {
				validPlatform = true
				break
			}
		}
		if !validPlatform {
			return fmt.Errorf("target %s: invalid platform '%s', must be one of: %s",
				target.Name, target.Platform, strings.Join(ValidPlatforms, ", "))
		}

		key := target.Platform + "/" + target.Name
		if nameMap[key] {
			return fmt.Errorf("duplicate target name '%s' under platform '%s'", target.Name, target.Platform)
		}
		nameMap[key] = true
	}

	p.config = config
	return nil
}

// findTarget finds a target by platform and name.
func (p *WebhookPlugin) findTarget(platform, name string) *TargetConfig {
	for i := range p.config.Targets {
		if p.config.Targets[i].Platform == platform && p.config.Targets[i].Name == name {
			return &p.config.Targets[i]
		}
	}
	return nil
}

// findTargetsByPlatform finds all enabled targets for a given platform.
func (p *WebhookPlugin) findTargetsByPlatform(platform string) []TargetConfig {
	var targets []TargetConfig
	for _, t := range p.config.Targets {
		if t.Platform == platform && t.Enabled {
			targets = append(targets, t)
		}
	}
	return targets
}

// RegisterWebhook implements plugin.Webhooker.
func (p *WebhookPlugin) RegisterWebhook(basePath string, g *gin.RouterGroup) {
	p.basePath = basePath

	// 健康检查
	g.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":           "ok",
			"enabled":          p.enabled,
			"outgoing_targets": len(p.config.Targets),
			"incoming_enabled": p.config.Incoming.Enabled,
		})
	})

	// ===== 出站转发（精确路由到指定目标） =====
	// POST /send/<platform>/<name> — 发送到指定平台的指定目标
	// 请求体格式与目标平台的 webhook 接口要求完全一致（透明代理）
	//
	// 示例:
	//   POST /send/wecom/wecom-group1     → 发送到名为 "wecom-group1" 的企微目标
	//   POST /send/dingtalk/ops-alert     → 发送到名为 "ops-alert" 的钉钉目标
	//   POST /send/feishu/dev-notify      → 发送到名为 "dev-notify" 的飞书目标
	g.POST("/send/:platform/:name", func(c *gin.Context) {
		if !p.enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "plugin is disabled"})
			return
		}

		platform := c.Param("platform")
		name := c.Param("name")

		// 验证平台
		if !isValidPlatform(platform) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("unsupported platform '%s', supported: %s", platform, strings.Join(ValidPlatforms, "/")),
			})
			return
		}

		// 查找目标
		target := p.findTarget(platform, name)
		if target == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": fmt.Sprintf("target '%s' not found for platform '%s'", name, platform),
			})
			return
		}
		if !target.Enabled {
			c.JSON(http.StatusForbidden, gin.H{
				"error": fmt.Sprintf("target '%s' is disabled", name),
			})
			return
		}

		// 读取原始请求体 — 原样转发
		rawBody, err := io.ReadAll(c.Request.Body)
		if err != nil || len(rawBody) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "empty request body"})
			return
		}

		contentType := c.GetHeader("Content-Type")

		if err := p.sender.ForwardRaw(*target, rawBody, contentType); err != nil {
			log.Printf("[webhook-plugin] Forward to %s/%s failed: %v", platform, name, err)
			c.JSON(http.StatusBadGateway, gin.H{
				"error":  err.Error(),
				"target": name,
			})
			return
		}

		log.Printf("[webhook-plugin] Forwarded to %s/%s", platform, name)

		// Gotify 中记录
		if p.msgHandler != nil {
			summary := extractSummary(rawBody, platform)
			_ = p.msgHandler.SendMessage(plugin.Message{
				Title:    fmt.Sprintf("[Webhook→%s/%s] 转发成功", platform, name),
				Message:  summary,
				Priority: 1,
			})
		}

		c.JSON(http.StatusOK, gin.H{"status": "sent", "target": name, "platform": platform})
	})

	// ===== 出站转发（广播到平台所有已启用目标） =====
	// POST /send/<platform> — 发送到该平台的所有已启用目标
	g.POST("/send/:platform", func(c *gin.Context) {
		if !p.enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "plugin is disabled"})
			return
		}

		platform := c.Param("platform")

		if !isValidPlatform(platform) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("unsupported platform '%s', supported: %s", platform, strings.Join(ValidPlatforms, "/")),
			})
			return
		}

		rawBody, err := io.ReadAll(c.Request.Body)
		if err != nil || len(rawBody) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "empty request body"})
			return
		}

		contentType := c.GetHeader("Content-Type")
		targets := p.findTargetsByPlatform(platform)

		if len(targets) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("no enabled targets for platform '%s'", platform)})
			return
		}

		var sent []string
		var errs []string
		for _, target := range targets {
			if err := p.sender.ForwardRaw(target, rawBody, contentType); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", target.Name, err))
				log.Printf("[webhook-plugin] Forward to %s/%s failed: %v", platform, target.Name, err)
			} else {
				sent = append(sent, target.Name)
			}
		}

		if len(errs) > 0 {
			c.JSON(http.StatusBadGateway, gin.H{
				"status": "partial",
				"sent":   sent,
				"errors": errs,
			})
			return
		}

		log.Printf("[webhook-plugin] Broadcast to %s: %v", platform, sent)

		if p.msgHandler != nil {
			summary := extractSummary(rawBody, platform)
			_ = p.msgHandler.SendMessage(plugin.Message{
				Title:    fmt.Sprintf("[Webhook→%s] 广播转发成功", platform),
				Message:  fmt.Sprintf("目标: %s\n%s", strings.Join(sent, ", "), summary),
				Priority: 1,
			})
		}

		c.JSON(http.StatusOK, gin.H{"status": "sent", "targets": sent})
	})

	// ===== 入站接收（外部平台 → Gotify） =====
	g.POST("/receive", func(c *gin.Context) {
		if !p.enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "plugin is disabled"})
			return
		}
		if !p.config.Incoming.Enabled {
			c.JSON(http.StatusForbidden, gin.H{"error": "incoming webhook is disabled"})
			return
		}
		if p.msgHandler == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "message handler not set"})
			return
		}

		platform := c.Query("platform")
		if platform == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "platform parameter is required (wecom/dingtalk/feishu/custom)"})
			return
		}

		platformCfg, exists := p.config.Incoming.Platforms[platform]
		if exists && !platformCfg.Enabled {
			c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("incoming for platform '%s' is disabled", platform)})
			return
		}

		secret := p.config.Incoming.Secret
		if exists && platformCfg.Secret != "" {
			secret = platformCfg.Secret
		}

		token := c.Query("token")
		if secret != "" && platform != "dingtalk" && platform != "feishu" {
			if token != secret {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing token"})
				return
			}
		}

		title, message, priority, isMarkdown, err := p.receiver.ParseAndVerify(c, platform, secret)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// 仅当消息类型为 markdown 且启用了 HTML→Markdown 时，检测并转换 HTML
		var extras map[string]interface{}
		if isMarkdown && p.config.HTML2MD.Enabled && IsHTML(message) {
			md, convErr := ConvertHTMLToMarkdown(message)
			if convErr != nil {
				log.Printf("[webhook-plugin] HTML→Markdown auto-conversion failed for %s: %v", platform, convErr)
				// 转换失败时保持原始内容
			} else {
				message = md
				log.Printf("[webhook-plugin] Auto-converted HTML→Markdown for %s message", platform)
			}
		}
		// markdown 类型的消息始终注入 extras 以触发 Gotify 前端 Markdown 渲染
		if isMarkdown {
			extras = map[string]interface{}{
				"client::display": map[string]interface{}{
					"contentType": "text/markdown",
				},
			}
		}

		if err := p.msgHandler.SendMessage(plugin.Message{
			Title:    title,
			Message:  message,
			Priority: priority,
			Extras:   extras,
		}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to create message: %v", err)})
			return
		}

		log.Printf("[webhook-plugin] Received %s message, created Gotify message: %s", platform, title)
		c.JSON(http.StatusOK, gin.H{"status": "message created", "platform": platform})
	})


	// ===== 出站测试 =====
	// POST /test          — 测试所有已启用目标
	// POST /test/<name>   — 测试指定名称的目标
	g.POST("/test", func(c *gin.Context) {
		if !p.enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "plugin is disabled"})
			return
		}

		var results []map[string]interface{}
		for _, target := range p.config.Targets {
			if !target.Enabled {
				continue
			}
			results = append(results, p.testTarget(target))
		}

		if len(results) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "no enabled targets to test"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"results": results})
	})

	g.POST("/test/:name", func(c *gin.Context) {
		if !p.enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "plugin is disabled"})
			return
		}

		name := c.Param("name")
		var target *TargetConfig
		for i := range p.config.Targets {
			if p.config.Targets[i].Name == name {
				target = &p.config.Targets[i]
				break
			}
		}
		if target == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("target '%s' not found", name)})
			return
		}
		c.JSON(http.StatusOK, p.testTarget(*target))
	})
}

// testTarget sends a native test payload to a single target and returns the result.
func (p *WebhookPlugin) testTarget(target TargetConfig) map[string]interface{} {
	testPayload := buildNativeTestPayload(target.Platform)
	err := p.sender.ForwardRaw(target, testPayload, "application/json")
	result := map[string]interface{}{
		"target":   target.Name,
		"platform": target.Platform,
	}
	if err != nil {
		result["status"] = "error"
		result["error"] = err.Error()
		log.Printf("[webhook-plugin] Test to %s failed: %v", target.Name, err)
	} else {
		result["status"] = "ok"
		log.Printf("[webhook-plugin] Test to %s succeeded", target.Name)
	}
	return result
}

// GetDisplay implements plugin.Displayer.
func (p *WebhookPlugin) GetDisplay(location *url.URL) string {
	baseURL := ""
	if location != nil {
		base := strings.TrimRight(p.basePath, "/")
		baseURL = fmt.Sprintf("%s://%s%s", location.Scheme, location.Host, base)
	}

	var sb strings.Builder
	sb.WriteString("## Gotify Webhook 透明代理插件\n\n")

	sb.WriteString("### 💡 核心设计\n")
	sb.WriteString("本插件是一个 **透明代理**，接收与目标平台 webhook API **完全一致的原生 JSON 格式**，")
	sb.WriteString("仅在需要时自动添加签名，然后 **原样转发** 到目标平台。\n\n")
	sb.WriteString("> 每个平台支持配置多个目标，通过 URL 中的 **name** 参数精确路由。\n\n")

	sb.WriteString("### 路由规则\n")
	sb.WriteString("| 路由 | 说明 |\n")
	sb.WriteString("|------|------|\n")
	sb.WriteString("| `POST /send/<platform>/<name>` | 发送到指定平台的指定目标 |\n")
	sb.WriteString("| `POST /send/<platform>` | 广播到该平台的所有已启用目标 |\n")
	sb.WriteString("| `POST /test` | 测试所有已启用目标 |\n")
	sb.WriteString("| `POST /test/<name>` | 测试指定名称的目标 |\n\n")

	sb.WriteString("### 支持平台\n")
	sb.WriteString("| 平台 | platform 参数 | 签名方式 |\n")
	sb.WriteString("|------|---------------|----------|\n")
	sb.WriteString("| 企业微信 | `wecom` | 无需签名 |\n")
	sb.WriteString("| 钉钉 | `dingtalk` | HMAC-SHA256 加签（自动添加） |\n")
	sb.WriteString("| 飞书 | `feishu` | HMAC-SHA256 签名（自动注入 body） |\n")
	sb.WriteString("| 自定义 | `custom` | X-Signature header（可选） |\n\n")

	if baseURL != "" {
		sb.WriteString("### API 端点\n")
		sb.WriteString(fmt.Sprintf("| 功能 | 方法 | 地址 |\n"))
		sb.WriteString(fmt.Sprintf("|------|------|------|\n"))
		sb.WriteString(fmt.Sprintf("| 健康检查 | GET | `%s/health` |\n", baseURL))
		sb.WriteString(fmt.Sprintf("| 测试全部 | POST | `%s/test` |\n", baseURL))
		sb.WriteString(fmt.Sprintf("| 入站接收 | POST | `%s/receive?platform=<p>` |\n\n", baseURL))
	}

	// 显示出站目标，按平台分组
	sb.WriteString("### 出站目标\n")
	if p.config != nil {
		platforms := map[string][]TargetConfig{}
		for _, t := range p.config.Targets {
			platforms[t.Platform] = append(platforms[t.Platform], t)
		}

		platformNames := map[string]string{
			"wecom":    "企业微信",
			"dingtalk": "钉钉",
			"feishu":   "飞书",
			"custom":   "自定义",
		}

		for _, pf := range ValidPlatforms {
			targets, ok := platforms[pf]
			if !ok {
				continue
			}
			sb.WriteString(fmt.Sprintf("\n**%s (%s)**\n", platformNames[pf], pf))
			for _, t := range targets {
				status := "❌"
				if t.Enabled {
					status = "✅"
				}
				secretInfo := ""
				if t.Secret != "" {
					secretInfo = " 🔑"
				}
				if baseURL != "" {
					sb.WriteString(fmt.Sprintf("- %s `%s` → `%s/send/%s/%s`%s\n", status, t.Name, baseURL, pf, t.Name, secretInfo))
				} else {
					sb.WriteString(fmt.Sprintf("- %s `%s`%s\n", status, t.Name, secretInfo))
				}
			}
		}
	}

	if baseURL != "" {
		sb.WriteString("\n### 调用示例\n\n")

		sb.WriteString("**发送到指定企微目标：**\n")
		exampleName := "wecom-group1"
		if p.config != nil {
			for _, t := range p.config.Targets {
				if t.Platform == "wecom" {
					exampleName = t.Name
					break
				}
			}
		}
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/send/wecom/%s' \\\n", baseURL, exampleName))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msgtype\":\"text\",\"text\":{\"content\":\"来自 Gotify 的消息\"}}'\n```\n\n")

		sb.WriteString("**广播到所有钉钉目标：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/send/dingtalk' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msgtype\":\"text\",\"text\":{\"content\":\"广播消息\"}}'\n```\n\n")

		sb.WriteString("**飞书（签名自动注入）：**\n")
		exampleFeishu := "feishu-group1"
		if p.config != nil {
			for _, t := range p.config.Targets {
				if t.Platform == "feishu" {
					exampleFeishu = t.Name
					break
				}
			}
		}
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/send/feishu/%s' \\\n", baseURL, exampleFeishu))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msg_type\":\"text\",\"content\":{\"text\":\"来自 Gotify 的消息\"}}'\n```\n\n")
	}

	sb.WriteString("### 入站接收（外部平台 → Gotify）\n")
	sb.WriteString("接收来自外部平台的 Webhook 消息，解析后存为 Gotify 消息。\n\n")
	if p.config != nil {
		inStatus := "❌ 禁用"
		if p.config.Incoming.Enabled {
			inStatus = "✅ 启用"
		}
		sb.WriteString(fmt.Sprintf("- 全局状态: %s\n", inStatus))
		for name, pcfg := range p.config.Incoming.Platforms {
			ps := "❌"
			if pcfg.Enabled {
				ps = "✅"
			}
			hasSecret := "无密钥"
			if pcfg.Secret != "" {
				hasSecret = "已配置密钥"
			}
			sb.WriteString(fmt.Sprintf("- %s **%s** (%s)\n", ps, name, hasSecret))
		}
	}

	if baseURL != "" {
		sb.WriteString("\n### 入站接收示例\n\n")

		sb.WriteString("**企业微信 Text → Gotify（token 校验）：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=wecom&token=YOUR_TOKEN' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msgtype\":\"text\",\"text\":{\"content\":\"来自企微的消息\"}}'\n```\n\n")

		sb.WriteString("**企业微信 Markdown → Gotify：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=wecom&token=YOUR_TOKEN' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msgtype\":\"markdown\",\"markdown\":{\"content\":\"### 告警\\n> CPU 超过 90%%\"}}'\n```\n\n")

		sb.WriteString("**钉钉 Text → Gotify：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=dingtalk' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msgtype\":\"text\",\"text\":{\"content\":\"来自钉钉的消息\"}}'\n```\n\n")

		sb.WriteString("**钉钉 Markdown → Gotify：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=dingtalk' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msgtype\":\"markdown\",\"markdown\":{\"title\":\"监控告警\",\"text\":\"### CPU 告警\\n使用率超过 **90%%**\"}}'\n```\n\n")

		sb.WriteString("**飞书 Text → Gotify：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=feishu' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msg_type\":\"text\",\"content\":{\"text\":\"来自飞书的消息\"}}'\n```\n\n")

		sb.WriteString("**飞书 Markdown → Gotify：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=feishu' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msg_type\":\"markdown\",\"content\":{\"text\":\"### 告警\\n> CPU 超过 90%%\"}}'\n```\n\n")

		sb.WriteString("**自定义 Text → Gotify（token 校验）：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=custom&token=YOUR_TOKEN' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"title\":\"告警标题\",\"message\":\"告警详情内容\",\"priority\":5}'\n```\n\n")

		sb.WriteString("**自定义 Markdown → Gotify（含 HTML 自动转换）：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=custom&token=YOUR_TOKEN' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"msgtype\":\"markdown\",\"title\":\"监控告警\",\"message\":\"<h1>CPU 告警</h1><p>使用率 <b>99%%</b></p>\",\"priority\":5}'\n```\n\n")

		sb.WriteString("**自定义 — 纯 HTML Body：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=custom&token=YOUR_TOKEN' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: text/html' \\\n")
		sb.WriteString("  -d '<h1>告警</h1><p>CPU 使用率 <b>99%%</b></p>'\n```\n\n")
	}

	sb.WriteString("### Name 命名规则\n")
	sb.WriteString("Target name 必须是 URL 安全的字符串，只允许：`a-z A-Z 0-9 _ - .`\n\n")

	// HTML → Markdown 功能说明
	sb.WriteString("### HTML → Markdown 自动转换\n")
	sb.WriteString("**仅当 `msgtype` 为 `markdown` 时**，插件才检测消息中的 HTML 标签并自动转为 Markdown。\n")
	sb.WriteString("`text` 格式的消息不做任何转换。\n\n")
	if p.config != nil {
		html2mdStatus := "❌ 禁用"
		if p.config.HTML2MD.Enabled {
			html2mdStatus = "✅ 启用"
		}
		sb.WriteString(fmt.Sprintf("- 状态: %s\n", html2mdStatus))
	}

	sb.WriteString("\n**工作原理：**\n")
	sb.WriteString("1. 通过 `/receive` 入站通道（企微/钉钉/飞书/自定义）接收消息\n")
	sb.WriteString("2. 当 `msgtype/msg_type = markdown` 时，检测内容中的 HTML 标签\n")
	sb.WriteString("3. 将 HTML 转换为 Markdown，写入 `content` 字段\n")
	sb.WriteString("4. 自动注入 `extras.client::display.contentType = text/markdown`\n")
	sb.WriteString("5. Gotify 前端使用内置 Markdown 渲染器展示\n\n")


	return sb.String()
}

// isValidPlatform checks if the platform is supported.
func isValidPlatform(platform string) bool {
	for _, p := range ValidPlatforms {
		if platform == p {
			return true
		}
	}
	return false
}

// buildNativeTestPayload constructs a test payload in the native format for each platform.
func buildNativeTestPayload(platform string) []byte {
	var payload interface{}
	switch platform {
	case "wecom":
		payload = map[string]interface{}{
			"msgtype": "text",
			"text": map[string]string{
				"content": "🔔 Gotify Webhook Plugin 测试消息 — 企业微信通道",
			},
		}
	case "dingtalk":
		payload = map[string]interface{}{
			"msgtype": "text",
			"text": map[string]string{
				"content": "🔔 Gotify Webhook Plugin 测试消息 — 钉钉通道",
			},
		}
	case "feishu":
		payload = map[string]interface{}{
			"msg_type": "text",
			"content": map[string]string{
				"text": "🔔 Gotify Webhook Plugin 测试消息 — 飞书通道",
			},
		}
	case "custom":
		payload = map[string]interface{}{
			"title":   "Gotify Test",
			"message": "🔔 Gotify Webhook Plugin 测试消息 — 自定义通道",
		}
	default:
		payload = map[string]string{"message": "test"}
	}
	data, _ := json.Marshal(payload)
	return data
}

// extractSummary tries to extract a brief summary from the raw body for Gotify message logging.
func extractSummary(rawBody []byte, platform string) string {
	var m map[string]interface{}
	if err := json.Unmarshal(rawBody, &m); err != nil {
		return "(非 JSON 内容)"
	}

	switch platform {
	case "wecom":
		if msgtype, ok := m["msgtype"].(string); ok {
			switch msgtype {
			case "text":
				if text, ok := m["text"].(map[string]interface{}); ok {
					if content, ok := text["content"].(string); ok {
						return truncate(content, 100)
					}
				}
			case "markdown":
				if md, ok := m["markdown"].(map[string]interface{}); ok {
					if content, ok := md["content"].(string); ok {
						return truncate(content, 100)
					}
				}
			}
			return fmt.Sprintf("消息类型: %s", msgtype)
		}
	case "dingtalk":
		if msgtype, ok := m["msgtype"].(string); ok {
			switch msgtype {
			case "text":
				if text, ok := m["text"].(map[string]interface{}); ok {
					if content, ok := text["content"].(string); ok {
						return truncate(content, 100)
					}
				}
			case "markdown":
				if md, ok := m["markdown"].(map[string]interface{}); ok {
					if title, ok := md["title"].(string); ok {
						return fmt.Sprintf("[%s] ...", title)
					}
				}
			}
			return fmt.Sprintf("消息类型: %s", msgtype)
		}
	case "feishu":
		if msgType, ok := m["msg_type"].(string); ok {
			if msgType == "text" {
				if content, ok := m["content"].(map[string]interface{}); ok {
					if text, ok := content["text"].(string); ok {
						return truncate(text, 100)
					}
				}
			}
			return fmt.Sprintf("消息类型: %s", msgType)
		}
	}

	return "(已转发)"
}

// truncate truncates a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// NewGotifyPluginInstance creates a plugin instance for a user context.
func NewGotifyPluginInstance(ctx plugin.UserContext) plugin.Plugin {
	return &WebhookPlugin{
		userCtx:  ctx,
		sender:   NewSender(),
		receiver: NewReceiver(),
		config:   &Config{},
	}
}

func main() {
	panic("this should be built as go plugin")
}
