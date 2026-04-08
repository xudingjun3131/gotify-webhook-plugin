package main

// Config is the plugin configuration that users can edit through the Gotify UI.
type Config struct {
	Targets  []TargetConfig `json:"targets" yaml:"targets"`   // 出站：推送目标列表
	Incoming IncomingConfig `json:"incoming" yaml:"incoming"` // 入站：接收 Webhook 配置
	HTML2MD  HTML2MDConfig  `json:"html2md" yaml:"html2md"`   // HTML→Markdown 转换
}

// HTML2MDConfig defines settings for automatic HTML to Markdown conversion.
type HTML2MDConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"` // 是否启用入站消息的 HTML 自动检测转换
}

// IncomingConfig defines settings for the incoming webhook receiver.
type IncomingConfig struct {
	Enabled   bool                             `json:"enabled" yaml:"enabled"`                           // 是否启用入站 Webhook 接收
	Secret    string                           `json:"secret" yaml:"secret"`                             // 全局接收密钥（URL参数 token 校验）
	Platforms map[string]PlatformReceiveConfig `json:"platforms,omitempty" yaml:"platforms,omitempty"` // 各平台独立密钥配置
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
	Name       string            `json:"name" yaml:"name"`                           // 目标名称，如 "我的企业微信群"
	Platform   string            `json:"platform" yaml:"platform"`                   // 平台类型
	WebhookURL string            `json:"webhook_url" yaml:"webhook_url"`             // Webhook / API 地址
	Secret     string            `json:"secret,omitempty" yaml:"secret,omitempty"`   // 通用签名密钥或云厂商凭据（如 AK:SK / SecretId:SecretKey）
	Enabled    bool              `json:"enabled" yaml:"enabled"`                     // 是否启用此目标
	Method     string            `json:"method,omitempty" yaml:"method,omitempty"`   // HTTP 方法（默认 POST）
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"` // 自定义请求头

	// Email 专用配置
	EmailTo      []string `json:"email_to,omitempty" yaml:"email_to,omitempty"`           // 收件人列表
	EmailSubject string   `json:"email_subject,omitempty" yaml:"email_subject,omitempty"` // 固定邮件主题（为空时自动生成）
	EmailFrom    string   `json:"email_from,omitempty" yaml:"email_from,omitempty"`       // 发件人地址（可选，优先于请求头/默认值）

	// SMS / 云通知专用配置
	PhoneNumbers []string `json:"phone_numbers,omitempty" yaml:"phone_numbers,omitempty"` // 短信接收手机号 / SNS SMS 号码
	TemplateCode string   `json:"template_code,omitempty" yaml:"template_code,omitempty"` // 模板编码 / TemplateId
	SignName     string   `json:"sign_name,omitempty" yaml:"sign_name,omitempty"`         // 短信签名
	Region       string   `json:"region,omitempty" yaml:"region,omitempty"`               // 云服务地域
	TopicARN     string   `json:"topic_arn,omitempty" yaml:"topic_arn,omitempty"`         // Amazon SNS Topic ARN
	Subject      string   `json:"subject,omitempty" yaml:"subject,omitempty"`             // Amazon SNS Subject
	SMSAppID     string   `json:"sms_app_id,omitempty" yaml:"sms_app_id,omitempty"`       // 腾讯云短信 SmsSdkAppId
}

// ValidPlatforms lists the supported platform identifiers.
var ValidPlatforms = []string{
	"wecom",
	"dingtalk",
	"feishu",
	"telegram",
	"email",
	"sns",
	"aliyun-sms",
	"tencent-sms",
	"discord",
	"slack",
	"custom",
}
