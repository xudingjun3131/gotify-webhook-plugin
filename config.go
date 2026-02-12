package main

// Config is the plugin configuration that users can edit through the Gotify UI.
type Config struct {
	Targets  []TargetConfig `json:"targets" yaml:"targets"`   // 出站：推送目标列表
	Incoming IncomingConfig `json:"incoming" yaml:"incoming"` // 入站：接收 Webhook 配置
}

// IncomingConfig defines settings for the incoming webhook receiver.
type IncomingConfig struct {
	Enabled   bool                               `json:"enabled" yaml:"enabled"`                       // 是否启用入站 Webhook 接收
	Secret    string                             `json:"secret" yaml:"secret"`                         // 全局接收密钥（URL参数 token 校验）
	Platforms map[string]PlatformReceiveConfig   `json:"platforms,omitempty" yaml:"platforms,omitempty"` // 各平台独立密钥配置
}

// PlatformReceiveConfig defines per-platform incoming webhook settings.
type PlatformReceiveConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"` // 是否启用该平台的接收
	Secret  string `json:"secret" yaml:"secret"`   // 该平台的独立密钥（为空则使用全局密钥）
}

// TargetConfig defines a single webhook push target (transparent proxy mode).
// 消息体格式由调用方自行构造，与目标平台的 webhook 接口要求完全一致。
// 插件仅做签名和转发，不做格式转换。
type TargetConfig struct {
	Name       string            `json:"name" yaml:"name"`                                 // 目标名称，如 "我的企业微信群"
	Platform   string            `json:"platform" yaml:"platform"`                         // 平台类型: wecom / dingtalk / feishu / custom
	WebhookURL string            `json:"webhook_url" yaml:"webhook_url"`                   // Webhook 地址
	Secret     string            `json:"secret,omitempty" yaml:"secret,omitempty"`          // 签名密钥（可选，钉钉/飞书/自定义可用）
	Enabled    bool              `json:"enabled" yaml:"enabled"`                           // 是否启用此目标
	Method     string            `json:"method,omitempty" yaml:"method,omitempty"`          // HTTP 方法（仅 custom 使用，默认 POST）
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`        // 自定义请求头（仅 custom 使用）
}

// ValidPlatforms lists the supported platform identifiers.
var ValidPlatforms = []string{"wecom", "dingtalk", "feishu", "custom"}
