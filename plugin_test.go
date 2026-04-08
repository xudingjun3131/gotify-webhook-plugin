package main

import (
	"encoding/json"
	"testing"

	"github.com/gotify/plugin-api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Interface Compatibility Tests ---

func TestAPICompatibility(t *testing.T) {
	assert.Implements(t, (*plugin.Plugin)(nil), new(WebhookPlugin))
	assert.Implements(t, (*plugin.Webhooker)(nil), new(WebhookPlugin))
	assert.Implements(t, (*plugin.Messenger)(nil), new(WebhookPlugin))
	assert.Implements(t, (*plugin.Configurer)(nil), new(WebhookPlugin))
	assert.Implements(t, (*plugin.Displayer)(nil), new(WebhookPlugin))
	assert.Implements(t, (*plugin.Storager)(nil), new(WebhookPlugin))
}

func TestPluginInfo(t *testing.T) {
	info := GetGotifyPluginInfo()
	assert.NotEmpty(t, info.ModulePath)
	assert.NotEmpty(t, info.Name)
	assert.Equal(t, "1.0.0", info.Version)
	assert.Contains(t, info.Description, "Telegram")
	assert.Contains(t, info.Description, "Slack")
}

// --- Config Tests ---

func TestDefaultConfig(t *testing.T) {
	p := &WebhookPlugin{}
	cfg := p.DefaultConfig()
	assert.NotNil(t, cfg)

	config, ok := cfg.(*Config)
	assert.True(t, ok)
	assert.True(t, len(config.Targets) > 0, "default config should have example targets")

	platforms := map[string]bool{}
	for _, target := range config.Targets {
		assert.False(t, target.Enabled, "default targets should be disabled")
		platforms[target.Platform] = true
	}

	for _, pf := range []string{"wecom", "dingtalk", "feishu", "telegram", "email", "sns", "aliyun-sms", "tencent-sms", "discord", "slack", "custom"} {
		assert.True(t, platforms[pf], "default config should contain platform %s", pf)
	}
}

func TestValidateConfig_Valid(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Name: "group1", Platform: "wecom", WebhookURL: "https://example.com/1", Enabled: true},
			{Name: "group2", Platform: "telegram", WebhookURL: "https://example.com/2", Enabled: true},
			{Name: "ops", Platform: "tencent-sms", SMSAppID: "1400006666", Enabled: true},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.NoError(t, err)
	assert.Equal(t, config, p.config)
}

func TestValidateConfig_InvalidPlatform(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Name: "test", Platform: "invalid", WebhookURL: "https://example.com"},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid platform")
}

func TestValidateConfig_InvalidName(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Name: "has space", Platform: "wecom", WebhookURL: "https://example.com"},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestValidateConfig_InvalidNameChineseChar(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Name: "我的群", Platform: "wecom", WebhookURL: "https://example.com"},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestValidateConfig_ValidNameCharacters(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Name: "my-group_1.test", Platform: "wecom", WebhookURL: "https://example.com"},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.NoError(t, err)
}

func TestValidateConfig_DuplicateNameSamePlatform(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Name: "group1", Platform: "wecom", WebhookURL: "https://example.com/1"},
			{Name: "group1", Platform: "wecom", WebhookURL: "https://example.com/2"},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestValidateConfig_SameNameDiffPlatform(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Name: "ops", Platform: "wecom", WebhookURL: "https://example.com/1"},
			{Name: "ops", Platform: "dingtalk", WebhookURL: "https://example.com/2"},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.NoError(t, err)
}

func TestValidateConfig_MissingName(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Platform: "wecom", WebhookURL: "https://example.com"},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestValidateConfig_MissingURL(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Name: "test", Platform: "wecom"},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook_url is required")
}

func TestValidateConfig_TencentSMSAllowsSMSAppIDWithoutWebhookURL(t *testing.T) {
	p := &WebhookPlugin{}
	config := &Config{
		Targets: []TargetConfig{
			{Name: "sms", Platform: "tencent-sms", SMSAppID: "1400006666"},
		},
	}
	err := p.ValidateAndSetConfig(config)
	assert.NoError(t, err)
}

// --- Target Lookup Tests ---

func TestFindTarget(t *testing.T) {
	p := &WebhookPlugin{
		config: &Config{
			Targets: []TargetConfig{
				{Name: "group1", Platform: "wecom", Enabled: true},
				{Name: "group2", Platform: "wecom", Enabled: true},
				{Name: "ops", Platform: "dingtalk", Enabled: true},
			},
		},
	}

	target := p.findTarget("wecom", "group1")
	assert.NotNil(t, target)
	assert.Equal(t, "group1", target.Name)

	target = p.findTarget("wecom", "group2")
	assert.NotNil(t, target)
	assert.Equal(t, "group2", target.Name)

	target = p.findTarget("dingtalk", "ops")
	assert.NotNil(t, target)

	target = p.findTarget("wecom", "nonexist")
	assert.Nil(t, target)

	target = p.findTarget("feishu", "group1")
	assert.Nil(t, target)
}

func TestFindTargetsByPlatform(t *testing.T) {
	p := &WebhookPlugin{
		config: &Config{
			Targets: []TargetConfig{
				{Name: "g1", Platform: "wecom", Enabled: true},
				{Name: "g2", Platform: "wecom", Enabled: false},
				{Name: "g3", Platform: "wecom", Enabled: true},
				{Name: "ops", Platform: "dingtalk", Enabled: true},
			},
		},
	}

	targets := p.findTargetsByPlatform("wecom")
	assert.Len(t, targets, 2)
	assert.Equal(t, "g1", targets[0].Name)
	assert.Equal(t, "g3", targets[1].Name)

	targets = p.findTargetsByPlatform("feishu")
	assert.Len(t, targets, 0)
}

// --- Plugin Instance Tests ---

func TestNewGotifyPluginInstance(t *testing.T) {
	ctx := plugin.UserContext{ID: 1, Name: "testuser", Admin: false}
	instance := NewGotifyPluginInstance(ctx)
	assert.NotNil(t, instance)

	wp, ok := instance.(*WebhookPlugin)
	assert.True(t, ok)
	assert.Equal(t, uint(1), wp.userCtx.ID)
	assert.NotNil(t, wp.sender)
	assert.NotNil(t, wp.receiver)
}

func TestEnableDisable(t *testing.T) {
	wp := &WebhookPlugin{userCtx: plugin.UserContext{ID: 1, Name: "test"}}
	assert.False(t, wp.enabled)

	assert.NoError(t, wp.Enable())
	assert.True(t, wp.enabled)

	assert.NoError(t, wp.Disable())
	assert.False(t, wp.enabled)
}

// --- Signer Tests ---

func TestSignDingTalk(t *testing.T) {
	ts, sign, err := SignDingTalk("test-secret")
	require.NoError(t, err)
	assert.NotEmpty(t, ts)
	assert.NotEmpty(t, sign)
}

func TestSignDingTalk_EmptySecret(t *testing.T) {
	ts, sign, err := SignDingTalk("")
	require.NoError(t, err)
	assert.Empty(t, ts)
	assert.Empty(t, sign)
}

func TestSignFeishu(t *testing.T) {
	ts, sign, err := SignFeishu("test-secret")
	require.NoError(t, err)
	assert.NotEmpty(t, ts)
	assert.NotEmpty(t, sign)
}

func TestSignFeishu_EmptySecret(t *testing.T) {
	ts, sign, err := SignFeishu("")
	require.NoError(t, err)
	assert.Empty(t, ts)
	assert.Empty(t, sign)
}

func TestSignCustom(t *testing.T) {
	sig, err := SignCustom("test-secret", []byte(`{"hello":"world"}`))
	require.NoError(t, err)
	assert.NotEmpty(t, sig)
	assert.Contains(t, sig, "sha256=")
}

func TestSignCustom_EmptySecret(t *testing.T) {
	sig, err := SignCustom("", []byte(`{"hello":"world"}`))
	require.NoError(t, err)
	assert.Empty(t, sig)
}

// --- Native Test Payload Tests ---

func TestBuildNativeTestPayload_WeCom(t *testing.T) {
	payload := buildNativeTestPayload("wecom")
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &m))
	assert.Equal(t, "text", m["msgtype"])
	text := m["text"].(map[string]interface{})
	assert.Contains(t, text["content"], "企业微信")
}

func TestBuildNativeTestPayload_DingTalk(t *testing.T) {
	payload := buildNativeTestPayload("dingtalk")
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &m))
	assert.Equal(t, "text", m["msgtype"])
}

func TestBuildNativeTestPayload_Feishu(t *testing.T) {
	payload := buildNativeTestPayload("feishu")
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &m))
	assert.Equal(t, "text", m["msg_type"])
}

func TestBuildNativeTestPayload_Telegram(t *testing.T) {
	payload := buildNativeTestPayload("telegram")
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &m))
	assert.Equal(t, "123456789", m["chat_id"])
	assert.Contains(t, m["text"], "Telegram")
}

func TestBuildNativeTestPayload_Email(t *testing.T) {
	payload := buildNativeTestPayload("email")
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &m))
	assert.Equal(t, "Gotify Test", m["title"])
	assert.Contains(t, m["message"], "Email")
}

func TestBuildNativeTestPayload_SNS(t *testing.T) {
	payload := buildNativeTestPayload("sns")
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &m))
	assert.Equal(t, "Gotify Test", m["subject"])
	assert.Contains(t, m["message"], "Amazon SNS")
}

func TestBuildNativeTestPayload_SMS(t *testing.T) {
	for _, pf := range []string{"aliyun-sms", "tencent-sms"} {
		payload := buildNativeTestPayload(pf)
		var m map[string]interface{}
		require.NoError(t, json.Unmarshal(payload, &m))
		assert.Contains(t, m["message"], "短信通道")
	}
}

func TestBuildNativeTestPayload_Discord(t *testing.T) {
	payload := buildNativeTestPayload("discord")
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &m))
	assert.Contains(t, m["content"], "Discord")
}

func TestBuildNativeTestPayload_Slack(t *testing.T) {
	payload := buildNativeTestPayload("slack")
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &m))
	assert.Contains(t, m["text"], "Slack")
}

// --- Helper Tests ---

func TestExtractSummary_WeCom(t *testing.T) {
	body := []byte(`{"msgtype":"text","text":{"content":"Hello World"}}`)
	assert.Equal(t, "Hello World", extractSummary(body, "wecom"))
}

func TestExtractSummary_DingTalk(t *testing.T) {
	body := []byte(`{"msgtype":"markdown","markdown":{"title":"Alert","text":"CPU high"}}`)
	assert.Contains(t, extractSummary(body, "dingtalk"), "Alert")
}

func TestExtractSummary_Feishu(t *testing.T) {
	body := []byte(`{"msg_type":"text","content":{"text":"飞书消息"}}`)
	assert.Equal(t, "飞书消息", extractSummary(body, "feishu"))
}

func TestExtractSummary_Telegram(t *testing.T) {
	body := []byte(`{"text":"telegram hello"}`)
	assert.Equal(t, "telegram hello", extractSummary(body, "telegram"))
}

func TestExtractSummary_Email(t *testing.T) {
	body := []byte(`{"subject":"巡检报告","text":"巡检通过"}`)
	assert.Equal(t, "巡检报告: 巡检通过", extractSummary(body, "email"))
}

func TestExtractSummary_SNS(t *testing.T) {
	body := []byte(`{"Message":"CPU usage high"}`)
	assert.Equal(t, "CPU usage high", extractSummary(body, "sns"))
}

func TestExtractSummary_Discord(t *testing.T) {
	body := []byte(`{"content":"discord hello"}`)
	assert.Equal(t, "discord hello", extractSummary(body, "discord"))
}

func TestExtractSummary_Slack(t *testing.T) {
	body := []byte(`{"text":"slack hello"}`)
	assert.Equal(t, "slack hello", extractSummary(body, "slack"))
}

func TestExtractSummary_InvalidJSON(t *testing.T) {
	assert.Equal(t, "(非 JSON 内容)", extractSummary([]byte(`not json`), "wecom"))
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hel...", truncate("hello world", 3))
	assert.Equal(t, "你好...", truncate("你好世界测试", 2))
}

func TestIsValidPlatform(t *testing.T) {
	for _, pf := range []string{"wecom", "dingtalk", "feishu", "telegram", "email", "sns", "aliyun-sms", "tencent-sms", "discord", "slack", "custom"} {
		assert.True(t, isValidPlatform(pf), "platform should be valid: %s", pf)
	}
	assert.False(t, isValidPlatform("invalid"))
	assert.False(t, isValidPlatform(""))
}

func TestNamePattern(t *testing.T) {
	assert.True(t, namePattern.MatchString("group1"))
	assert.True(t, namePattern.MatchString("my-group"))
	assert.True(t, namePattern.MatchString("my_group"))
	assert.True(t, namePattern.MatchString("my.group"))
	assert.True(t, namePattern.MatchString("WeChat-Ops-01"))
	assert.False(t, namePattern.MatchString("has space"))
	assert.False(t, namePattern.MatchString("中文名"))
	assert.False(t, namePattern.MatchString("name/slash"))
	assert.False(t, namePattern.MatchString(""))
}

// --- Display Test ---

func TestGetDisplay(t *testing.T) {
	p := &WebhookPlugin{
		basePath: "/plugin/123/",
		config: &Config{
			Targets: []TargetConfig{
				{Name: "ops-alert", Platform: "wecom", WebhookURL: "https://qyapi.weixin.qq.com/1", Enabled: true},
				{Name: "tg-alerts", Platform: "telegram", WebhookURL: "https://api.telegram.org/botX/sendMessage", Secret: "TOKEN", Enabled: true},
				{Name: "slack-alerts", Platform: "slack", WebhookURL: "https://hooks.slack.com/services/x", Enabled: true},
				{Name: "smtp-alerts", Platform: "email", WebhookURL: "smtp://user:pass@smtp.example.com:587", Enabled: false},
			},
			Incoming: IncomingConfig{
				Enabled: true,
				Platforms: map[string]PlatformReceiveConfig{
					"wecom":    {Enabled: true},
					"telegram": {Enabled: true, Secret: "TG_SECRET"},
					"slack":    {Enabled: true, Secret: "SLACK_SECRET"},
				},
			},
			HTML2MD: HTML2MDConfig{
				Enabled: true,
			},
		},
	}

	display := p.GetDisplay(nil)
	assert.Contains(t, display, "Telegram")
	assert.Contains(t, display, "Slack")
	assert.Contains(t, display, "Email")
	assert.Contains(t, display, "Amazon SNS")
	assert.Contains(t, display, "SMTP / SMTPS")
	assert.Contains(t, display, "Webhook 型")
	assert.Contains(t, display, "专用发送型")
	assert.Contains(t, display, "tg-alerts")
	assert.Contains(t, display, "slack-alerts")
	assert.Contains(t, display, "🔑")
	assert.Contains(t, display, "Name 命名规则")
	assert.Contains(t, display, "HTML")
	assert.Contains(t, display, "✅ 启用")
}

// --- HTML to Markdown Converter Tests ---

func TestConvertHTMLToMarkdown_BasicTags(t *testing.T) {
	html := "<h1>标题</h1><p>这是一段<b>加粗</b>文本</p>"
	md, err := ConvertHTMLToMarkdown(html)
	require.NoError(t, err)
	assert.Contains(t, md, "# 标题")
	assert.Contains(t, md, "**加粗**")
}

func TestConvertHTMLToMarkdown_List(t *testing.T) {
	html := "<ul><li>项目一</li><li>项目二</li></ul>"
	md, err := ConvertHTMLToMarkdown(html)
	require.NoError(t, err)
	assert.Contains(t, md, "项目一")
	assert.Contains(t, md, "项目二")
}

func TestConvertHTMLToMarkdown_Link(t *testing.T) {
	html := `<a href="https://example.com">链接</a>`
	md, err := ConvertHTMLToMarkdown(html)
	require.NoError(t, err)
	assert.Contains(t, md, "[链接](https://example.com)")
}

func TestConvertHTMLToMarkdown_Empty(t *testing.T) {
	md, err := ConvertHTMLToMarkdown("")
	require.NoError(t, err)
	assert.Equal(t, "", md)

	md, err = ConvertHTMLToMarkdown("   ")
	require.NoError(t, err)
	assert.Equal(t, "", md)
}

func TestConvertHTMLToMarkdown_PlainText(t *testing.T) {
	md, err := ConvertHTMLToMarkdown("这是纯文本")
	require.NoError(t, err)
	assert.Equal(t, "这是纯文本", md)
}

func TestConvertHTMLToMarkdown_Table(t *testing.T) {
	html := "<table><tr><th>名称</th><th>值</th></tr><tr><td>CPU</td><td>99%</td></tr></table>"
	md, err := ConvertHTMLToMarkdown(html)
	require.NoError(t, err)
	assert.Contains(t, md, "CPU")
	assert.Contains(t, md, "99%")
}

func TestExtractHTMLTitle_FromTitle(t *testing.T) {
	html := "<html><head><title>我的标题</title></head><body><p>内容</p></body></html>"
	assert.Equal(t, "我的标题", ExtractHTMLTitle(html))
}

func TestExtractHTMLTitle_FromH1(t *testing.T) {
	html := "<h1>告警标题</h1><p>内容</p>"
	assert.Equal(t, "告警标题", ExtractHTMLTitle(html))
}

func TestExtractHTMLTitle_TitleOverH1(t *testing.T) {
	html := "<html><head><title>Title标题</title></head><body><h1>H1标题</h1></body></html>"
	assert.Equal(t, "Title标题", ExtractHTMLTitle(html))
}

func TestExtractHTMLTitle_NoTitle(t *testing.T) {
	html := "<p>没有标题的内容</p>"
	assert.Equal(t, "", ExtractHTMLTitle(html))
}

func TestExtractHTMLTitle_Empty(t *testing.T) {
	assert.Equal(t, "", ExtractHTMLTitle(""))
}

// --- DefaultConfig HTML2MD Test ---

func TestDefaultConfig_HTML2MD(t *testing.T) {
	p := &WebhookPlugin{}
	cfg := p.DefaultConfig()
	config, ok := cfg.(*Config)
	assert.True(t, ok)
	assert.True(t, config.HTML2MD.Enabled)
}

// --- IsHTML Detection Tests ---

func TestIsHTML_True(t *testing.T) {
	assert.True(t, IsHTML("<h1>标题</h1>"))
	assert.True(t, IsHTML("<p>段落</p>"))
	assert.True(t, IsHTML("<div class='test'>内容</div>"))
	assert.True(t, IsHTML("前面有文字 <b>加粗</b> 后面也有"))
	assert.True(t, IsHTML("<table><tr><td>表格</td></tr></table>"))
	assert.True(t, IsHTML("<br/>"))
	assert.True(t, IsHTML("<img src='test.png'>"))
	assert.True(t, IsHTML("<a href='url'>链接</a>"))
}

func TestIsHTML_False(t *testing.T) {
	assert.False(t, IsHTML("这是纯文本"))
	assert.False(t, IsHTML("hello world"))
	assert.False(t, IsHTML("### Markdown 标题"))
	assert.False(t, IsHTML("**加粗** 和 *斜体*"))
	assert.False(t, IsHTML(""))
	assert.False(t, IsHTML("a < b > c"))
	assert.False(t, IsHTML("温度 < 30度"))
}
