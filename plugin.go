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
		Description: "透明代理通知插件：支持企业微信、钉钉、飞书、Telegram、Discord、Slack，以及 Email / Amazon SNS / 阿里云短信 / 腾讯云短信 / 自定义通知",
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
		// ===== 出站目标（透明代理转发 / 专用发送器） =====
		// 每个平台可配置多个目标，通过 name 区分
		// 出站 URL: POST /send/<platform>/<name>
		Targets: []TargetConfig{
			{
				Name:       "wecom-ops",
				Platform:   "wecom",
				WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=YOUR_KEY_1",
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
				Name:       "tg-alerts",
				Platform:   "telegram",
				WebhookURL: "https://api.telegram.org/bot<token>/sendMessage",
				Secret:     "YOUR_TELEGRAM_WEBHOOK_SECRET",
				Enabled:    false,
			},
			{
				Name:         "smtp-alerts",
				Platform:     "email",
				WebhookURL:   "smtp://user:password@smtp.example.com:587",
				EmailFrom:    "gotify@example.com",
				EmailTo:      []string{"ops@example.com"},
				EmailSubject: "Gotify Alert",
				Enabled:      false,
			},
			{
				Name:       "aws-sns-topic",
				Platform:   "sns",
				TopicARN:   "arn:aws:sns:ap-southeast-1:123456789012:alerts",
				Region:     "ap-southeast-1",
				Subject:    "Gotify Alert",
				Enabled:    false,
			},
			{
				Name:         "aliyun-sms-alerts",
				Platform:     "aliyun-sms",
				Secret:       "ACCESS_KEY_ID:ACCESS_KEY_SECRET",
				PhoneNumbers: []string{"13800138000"},
				TemplateCode: "SMS_123456789",
				SignName:     "YourSign",
				Region:       "cn-hangzhou",
				Enabled:      false,
			},
			{
				Name:         "tencent-sms-alerts",
				Platform:     "tencent-sms",
				Secret:       "SECRET_ID:SECRET_KEY",
				SMSAppID:     "1400006666",
				PhoneNumbers: []string{"+8613800138000"},
				TemplateCode: "1234567",
				SignName:     "YourSign",
				Region:       "ap-guangzhou",
				Enabled:      false,
			},
			{
				Name:       "discord-alerts",
				Platform:   "discord",
				WebhookURL: "https://discord.com/api/webhooks/WEBHOOK_ID/WEBHOOK_TOKEN",
				Secret:     "YOUR_DISCORD_WEBHOOK_SECRET",
				Enabled:    false,
			},
			{
				Name:       "slack-alerts",
				Platform:   "slack",
				WebhookURL: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
				Secret:     "YOUR_SLACK_WEBHOOK_SECRET",
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
		Incoming: IncomingConfig{
			Enabled: true,
			Secret:  "",
			Platforms: map[string]PlatformReceiveConfig{
				"wecom":       {Enabled: true, Secret: "YOUR_WECOM_RECEIVE_TOKEN"},
				"dingtalk":    {Enabled: true, Secret: "YOUR_DINGTALK_RECEIVE_SECRET"},
				"feishu":      {Enabled: true, Secret: "YOUR_FEISHU_RECEIVE_SECRET"},
				"telegram":    {Enabled: true, Secret: "YOUR_TELEGRAM_WEBHOOK_SECRET"},
				"email":       {Enabled: true, Secret: "YOUR_EMAIL_RECEIVE_TOKEN"},
				"sns":         {Enabled: true, Secret: "YOUR_SNS_RECEIVE_TOKEN"},
				"aliyun-sms":  {Enabled: true, Secret: "YOUR_ALIYUN_SMS_RECEIVE_TOKEN"},
				"tencent-sms": {Enabled: true, Secret: "YOUR_TENCENT_SMS_RECEIVE_TOKEN"},
				"discord":     {Enabled: true, Secret: "YOUR_DISCORD_WEBHOOK_SECRET"},
				"slack":       {Enabled: true, Secret: "YOUR_SLACK_WEBHOOK_SECRET"},
				"custom":      {Enabled: true, Secret: "YOUR_CUSTOM_RECEIVE_TOKEN"},
			},
		},
		// ===== HTML → Markdown 自动转换 =====
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
		if target.WebhookURL == "" && target.Platform != "sns" && target.Platform != "aliyun-sms" {
			if target.Platform != "tencent-sms" || target.SMSAppID == "" {
				return fmt.Errorf("target %s: webhook_url is required", target.Name)
			}
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
	g.POST("/send/:platform/:name", func(c *gin.Context) {
		if !p.enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "plugin is disabled"})
			return
		}

		platform := c.Param("platform")
		name := c.Param("name")

		if !isValidPlatform(platform) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("unsupported platform '%s', supported: %s", platform, strings.Join(ValidPlatforms, "/")),
			})
			return
		}

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
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("platform parameter is required (%s)", strings.Join(ValidPlatforms, "/"))})
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
			if token != secret && c.GetHeader("X-Webhook-Token") != secret {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing token"})
				return
			}
		}

		title, message, priority, isMarkdown, err := p.receiver.ParseAndVerify(c, platform, secret)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var extras map[string]interface{}
		if isMarkdown && p.config.HTML2MD.Enabled && IsHTML(message) {
			md, convErr := ConvertHTMLToMarkdown(message)
			if convErr != nil {
				log.Printf("[webhook-plugin] HTML→Markdown auto-conversion failed for %s: %v", platform, convErr)
			} else {
				message = md
				log.Printf("[webhook-plugin] Auto-converted HTML→Markdown for %s message", platform)
			}
		}
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

	platformNames := map[string]string{
		"wecom":       "企业微信",
		"dingtalk":    "钉钉",
		"feishu":      "飞书",
		"telegram":    "Telegram",
		"email":       "Email",
		"sns":         "Amazon SNS",
		"aliyun-sms":  "阿里云短信",
		"tencent-sms": "腾讯云短信",
		"discord":     "Discord",
		"slack":       "Slack",
		"custom":      "自定义",
	}
	platformKinds := map[string]string{
		"wecom":       "Webhook",
		"dingtalk":    "Webhook + 自动签名",
		"feishu":      "Webhook + 自动签名",
		"telegram":    "Bot API / Webhook",
		"email":       "SMTP / SMTPS",
		"sns":         "AWS SDK",
		"aliyun-sms":  "阿里云短信 SDK",
		"tencent-sms": "腾讯云短信 SDK",
		"discord":     "Webhook",
		"slack":       "Webhook",
		"custom":      "自定义 HTTP",
	}

	var sb strings.Builder
	sb.WriteString("## Gotify 多通道路由插件\n\n")
	sb.WriteString("支持 **透明代理 Webhook** 与 **专用发送器** 两类模式：\n\n")
	sb.WriteString("- Webhook 型：企业微信、钉钉、飞书、Telegram、Discord、Slack、自定义\n")
	sb.WriteString("- 专用发送型：Email、Amazon SNS、阿里云短信、腾讯云短信\n\n")

	sb.WriteString("### 路由规则\n")
	sb.WriteString("| 路由 | 说明 |\n")
	sb.WriteString("|------|------|\n")
	sb.WriteString("| `POST /send/<platform>/<name>` | 发送到指定平台的指定目标 |\n")
	sb.WriteString("| `POST /send/<platform>` | 广播到该平台的所有已启用目标 |\n")
	sb.WriteString("| `POST /receive?platform=<platform>` | 接收入站通知并写入 Gotify |\n")
	sb.WriteString("| `POST /test` | 测试所有已启用目标 |\n")
	sb.WriteString("| `POST /test/<name>` | 测试指定目标 |\n\n")

	sb.WriteString("### 支持平台\n")
	sb.WriteString("| 平台 | platform 参数 | 类型 |\n")
	sb.WriteString("|------|---------------|------|\n")
	for _, pf := range ValidPlatforms {
		sb.WriteString(fmt.Sprintf("| %s | `%s` | %s |\n", platformNames[pf], pf, platformKinds[pf]))
	}
	sb.WriteString("\n")

	if baseURL != "" {
		sb.WriteString("### API 端点\n")
		sb.WriteString("| 功能 | 方法 | 地址 |\n")
		sb.WriteString("|------|------|------|\n")
		sb.WriteString(fmt.Sprintf("| 健康检查 | GET | `%s/health` |\n", baseURL))
		sb.WriteString(fmt.Sprintf("| 测试全部 | POST | `%s/test` |\n", baseURL))
		sb.WriteString(fmt.Sprintf("| 入站接收 | POST | `%s/receive?platform=<p>` |\n\n", baseURL))
	}

	sb.WriteString("### 出站目标\n")
	if p.config != nil {
		platforms := map[string][]TargetConfig{}
		for _, t := range p.config.Targets {
			platforms[t.Platform] = append(platforms[t.Platform], t)
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
		sb.WriteString("**Telegram Bot 发送：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/send/telegram/tg-alerts' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"chat_id\":\"123456789\",\"text\":\"来自 Gotify 的 Telegram 消息\"}'\n```\n\n")

		sb.WriteString("**Slack Webhook 发送：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/send/slack/slack-alerts' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"text\":\"来自 Gotify 的 Slack 消息\"}'\n```\n\n")

		sb.WriteString("**Discord Webhook 发送：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/send/discord/discord-alerts' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"content\":\"来自 Gotify 的 Discord 消息\"}'\n```\n\n")

		sb.WriteString("**Email 发送：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/send/email/smtp-alerts' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"title\":\"数据库告警\",\"message\":\"主库延迟超过阈值\"}'\n```\n\n")

		sb.WriteString("**Amazon SNS 发送：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/send/sns/aws-sns-topic' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"subject\":\"Gotify Alert\",\"message\":\"Amazon SNS 推送测试\"}'\n```\n\n")
	}

	sb.WriteString("### 入站接收（外部平台 → Gotify）\n")
	sb.WriteString("接收来自外部平台或第三方系统的通知，解析后保存为 Gotify 消息。\n\n")
	if p.config != nil {
		inStatus := "❌ 禁用"
		if p.config.Incoming.Enabled {
			inStatus = "✅ 启用"
		}
		sb.WriteString(fmt.Sprintf("- 全局状态: %s\n", inStatus))
		for _, pf := range ValidPlatforms {
			pcfg, ok := p.config.Incoming.Platforms[pf]
			if !ok {
				continue
			}
			ps := "❌"
			if pcfg.Enabled {
				ps = "✅"
			}
			hasSecret := "无密钥"
			if pcfg.Secret != "" {
				hasSecret = "已配置密钥"
			}
			sb.WriteString(fmt.Sprintf("- %s **%s** / `%s` (%s)\n", ps, platformNames[pf], pf, hasSecret))
		}
	}

	if baseURL != "" {
		sb.WriteString("\n### 入站接收示例\n\n")
		sb.WriteString("**Telegram Webhook → Gotify：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=telegram' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -H 'X-Telegram-Bot-Api-Secret-Token: YOUR_TELEGRAM_WEBHOOK_SECRET' \\\n")
		sb.WriteString("  -d '{\"message\":{\"text\":\"来自 Telegram 的消息\",\"chat\":{\"title\":\"报警群\"}}}'\n```\n\n")

		sb.WriteString("**Email Webhook → Gotify：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=email&token=YOUR_EMAIL_RECEIVE_TOKEN' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"from\":\"noreply@example.com\",\"subject\":\"巡检报告\",\"text\":\"巡检通过\"}'\n```\n\n")

		sb.WriteString("**Amazon SNS → Gotify：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=sns&token=YOUR_SNS_RECEIVE_TOKEN' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"Type\":\"Notification\",\"Subject\":\"Cloud Alarm\",\"Message\":\"CPU usage high\",\"TopicArn\":\"arn:aws:sns:...\"}'\n```\n\n")

		sb.WriteString("**Slack Webhook → Gotify：**\n")
		sb.WriteString(fmt.Sprintf("```bash\ncurl -X POST '%s/receive?platform=slack&token=YOUR_SLACK_WEBHOOK_SECRET' \\\n", baseURL))
		sb.WriteString("  -H 'Content-Type: application/json' \\\n")
		sb.WriteString("  -d '{\"text\":\"来自 Slack 的消息\",\"channel_name\":\"alerts\",\"username\":\"bot\"}'\n```\n\n")
	}

	sb.WriteString("### Name 命名规则\n")
	sb.WriteString("Target name 必须是 URL 安全的字符串，只允许：`a-z A-Z 0-9 _ - .`\n\n")

	sb.WriteString("### HTML → Markdown 自动转换\n")
	sb.WriteString("当入站消息被识别为 Markdown 且内容实际包含 HTML 标签时，插件会自动转为 Markdown，并写入 Gotify 的 Markdown display extras。\n\n")
	if p.config != nil {
		html2mdStatus := "❌ 禁用"
		if p.config.HTML2MD.Enabled {
			html2mdStatus = "✅ 启用"
		}
		sb.WriteString(fmt.Sprintf("- 状态: %s\n", html2mdStatus))
	}

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
	case "telegram":
		payload = map[string]interface{}{
			"chat_id": "123456789",
			"text":    "🔔 Gotify Webhook Plugin 测试消息 — Telegram 通道",
		}
	case "email":
		payload = map[string]interface{}{
			"title":   "Gotify Test",
			"message": "🔔 Gotify Webhook Plugin 测试消息 — Email 通道",
		}
	case "sns":
		payload = map[string]interface{}{
			"subject": "Gotify Test",
			"message": "🔔 Gotify Webhook Plugin 测试消息 — Amazon SNS 通道",
		}
	case "aliyun-sms", "tencent-sms":
		payload = map[string]interface{}{
			"message": "【Gotify】测试消息：短信通道可达",
		}
	case "discord":
		payload = map[string]interface{}{
			"content": "🔔 Gotify Webhook Plugin 测试消息 — Discord 通道",
		}
	case "slack":
		payload = map[string]interface{}{
			"text": "🔔 Gotify Webhook Plugin 测试消息 — Slack 通道",
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
	case "telegram":
		if text, ok := m["text"].(string); ok {
			return truncate(text, 100)
		}
	case "email":
		if subject, ok := m["subject"].(string); ok {
			if message, ok := m["message"].(string); ok {
				return truncate(subject+": "+message, 100)
			}
			if text, ok := m["text"].(string); ok {
				return truncate(subject+": "+text, 100)
			}
			return truncate(subject, 100)
		}
	case "sns":
		if message, ok := m["message"].(string); ok {
			return truncate(message, 100)
		}
		if message, ok := m["Message"].(string); ok {
			return truncate(message, 100)
		}
	case "aliyun-sms", "tencent-sms":
		if message, ok := m["message"].(string); ok {
			return truncate(message, 100)
		}
		if content, ok := m["content"].(string); ok {
			return truncate(content, 100)
		}
	case "discord":
		if content, ok := m["content"].(string); ok {
			return truncate(content, 100)
		}
	case "slack":
		if text, ok := m["text"].(string); ok {
			return truncate(text, 100)
		}
	case "custom":
		if message, ok := m["message"].(string); ok {
			return truncate(message, 100)
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
