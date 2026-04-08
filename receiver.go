package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Receiver handles incoming webhook messages from external platforms.
type Receiver struct{}

// NewReceiver creates a new Receiver instance.
func NewReceiver() *Receiver {
	return &Receiver{}
}

// ParseAndVerify parses the incoming request and verifies the signature.
// Returns the parsed title, message, priority, and whether the message type is markdown.
func (r *Receiver) ParseAndVerify(c *gin.Context, platform, secret string) (title, message string, priority int, isMarkdown bool, err error) {
	switch platform {
	case "wecom":
		return r.parseWeCom(c)
	case "dingtalk":
		return r.parseDingTalk(c, secret)
	case "feishu":
		return r.parseFeishu(c, secret)
	case "telegram":
		return r.parseTelegram(c, secret)
	case "email":
		return r.parseEmail(c, secret)
	case "sns":
		return r.parseSNS(c, secret)
	case "aliyun-sms":
		return r.parseAliyunSMS(c, secret)
	case "tencent-sms":
		return r.parseTencentSMS(c, secret)
	case "discord":
		return r.parseDiscord(c, secret)
	case "slack":
		return r.parseSlack(c, secret)
	case "custom":
		return r.parseCustom(c, secret)
	default:
		return "", "", 0, false, fmt.Errorf("unsupported platform: %s", platform)
	}
}

// --- WeCom (企业微信) Incoming ---
func (r *Receiver) parseWeCom(c *gin.Context) (string, string, int, bool, error) {
	var body struct {
		MsgType string `json:"msgtype"`
		Text    struct {
			Content       string   `json:"content"`
			MentionedList []string `json:"mentioned_list,omitempty"`
		} `json:"text"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid wecom message format: %w", err)
	}

	switch body.MsgType {
	case "markdown":
		return "企业微信消息", body.Markdown.Content, 5, true, nil
	case "text":
		return "企业微信消息", body.Text.Content, 5, false, nil
	default:
		if body.Text.Content != "" {
			return "企业微信消息", body.Text.Content, 5, false, nil
		}
		return "企业微信消息", fmt.Sprintf("[不支持的消息类型: %s]", body.MsgType), 3, false, nil
	}
}

// --- DingTalk (钉钉) Incoming ---
func (r *Receiver) parseDingTalk(c *gin.Context, secret string) (string, string, int, bool, error) {
	if secret != "" {
		timestamp := c.Query("timestamp")
		sign := c.Query("sign")
		if timestamp == "" || sign == "" {
			return "", "", 0, false, fmt.Errorf("dingtalk signature required: missing timestamp or sign parameter")
		}
		if err := verifyDingTalkSign(timestamp, secret, sign); err != nil {
			return "", "", 0, false, fmt.Errorf("dingtalk signature verification failed: %w", err)
		}
	}

	var body struct {
		MsgType string `json:"msgtype"`
		Text    struct {
			Content string `json:"content"`
		} `json:"text"`
		Markdown struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"markdown"`
		ActionCard struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"actionCard"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid dingtalk message format: %w", err)
	}

	switch body.MsgType {
	case "markdown":
		title := body.Markdown.Title
		if title == "" {
			title = "钉钉消息"
		}
		return title, body.Markdown.Text, 5, true, nil
	case "actionCard":
		title := body.ActionCard.Title
		if title == "" {
			title = "钉钉消息"
		}
		return title, body.ActionCard.Text, 5, true, nil
	case "text":
		return "钉钉消息", body.Text.Content, 5, false, nil
	default:
		if body.Text.Content != "" {
			return "钉钉消息", body.Text.Content, 5, false, nil
		}
		return "钉钉消息", fmt.Sprintf("[不支持的消息类型: %s]", body.MsgType), 3, false, nil
	}
}

// --- Feishu (飞书) Incoming ---
func (r *Receiver) parseFeishu(c *gin.Context, secret string) (string, string, int, bool, error) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "", "", 0, false, fmt.Errorf("failed to read feishu request body: %w", err)
	}

	var fullBody struct {
		Timestamp string `json:"timestamp"`
		Sign      string `json:"sign"`
		MsgType   string `json:"msg_type"`
		Content   struct {
			Text string `json:"text"`
		} `json:"content"`
		Card struct {
			Header struct {
				Title struct {
					Content string `json:"content"`
				} `json:"title"`
			} `json:"header"`
		} `json:"card"`
	}

	if err := json.Unmarshal(rawBody, &fullBody); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid feishu message format: %w", err)
	}

	if secret != "" {
		if fullBody.Timestamp == "" || fullBody.Sign == "" {
			return "", "", 0, false, fmt.Errorf("feishu signature required: missing timestamp or sign in body")
		}
		if err := verifyFeishuSign(fullBody.Timestamp, secret, fullBody.Sign); err != nil {
			return "", "", 0, false, fmt.Errorf("feishu signature verification failed: %w", err)
		}
	}

	switch fullBody.MsgType {
	case "text":
		return "飞书消息", fullBody.Content.Text, 5, false, nil
	case "post", "markdown":
		return "飞书消息", fullBody.Content.Text, 5, true, nil
	case "interactive":
		title := fullBody.Card.Header.Title.Content
		if title == "" {
			title = "飞书卡片消息"
		}
		return title, string(rawBody), 5, false, nil
	default:
		if fullBody.Content.Text != "" {
			return "飞书消息", fullBody.Content.Text, 5, false, nil
		}
		return "飞书消息", string(rawBody), 3, false, nil
	}
}

func (r *Receiver) parseTelegram(c *gin.Context, secret string) (string, string, int, bool, error) {
	if err := verifyHeaderSecret(c, secret, "X-Telegram-Bot-Api-Secret-Token", "X-Webhook-Token"); err != nil {
		return "", "", 0, false, err
	}

	var body struct {
		Message *struct {
			Text string `json:"text"`
			Chat struct {
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"chat"`
			From struct {
				Username  string `json:"username"`
				FirstName string `json:"first_name"`
			} `json:"from"`
		} `json:"message"`
		ChannelPost *struct {
			Text string `json:"text"`
			Chat struct {
				Title string `json:"title"`
			} `json:"chat"`
		} `json:"channel_post"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid telegram update format: %w", err)
	}

	if body.Message != nil {
		title := "Telegram 消息"
		if body.Message.Chat.Title != "" {
			title = body.Message.Chat.Title
		} else if body.Message.From.Username != "" {
			title = "Telegram @" + body.Message.From.Username
		} else if body.Message.From.FirstName != "" {
			title = "Telegram " + body.Message.From.FirstName
		}
		return title, body.Message.Text, 5, false, nil
	}
	if body.ChannelPost != nil {
		title := "Telegram 频道消息"
		if body.ChannelPost.Chat.Title != "" {
			title = body.ChannelPost.Chat.Title
		}
		return title, body.ChannelPost.Text, 5, false, nil
	}
	return "Telegram 消息", "[未识别的 Telegram Update]", 3, false, nil
}

func (r *Receiver) parseEmail(c *gin.Context, secret string) (string, string, int, bool, error) {
	if err := verifyHeaderSecret(c, secret, "X-Webhook-Token", "X-Email-Token"); err != nil {
		return "", "", 0, false, err
	}
	var body struct {
		Subject string `json:"subject"`
		Text    string `json:"text"`
		HTML    string `json:"html"`
		From    string `json:"from"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid email payload format: %w", err)
	}
	message := body.Text
	isMarkdown := false
	if strings.TrimSpace(message) == "" && strings.TrimSpace(body.HTML) != "" {
		message = body.HTML
		isMarkdown = true
	}
	if body.From != "" {
		message = fmt.Sprintf("From: %s\n\n%s", body.From, message)
	}
	if body.Subject == "" {
		body.Subject = "Email 消息"
	}
	return body.Subject, message, 5, isMarkdown, nil
}

func (r *Receiver) parseSNS(c *gin.Context, secret string) (string, string, int, bool, error) {
	if err := verifyHeaderSecret(c, secret, "X-Webhook-Token", "X-SNS-Token"); err != nil {
		return "", "", 0, false, err
	}
	var body struct {
		Subject string `json:"Subject"`
		Message string `json:"Message"`
		Type    string `json:"Type"`
		Topic   string `json:"TopicArn"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid sns payload format: %w", err)
	}
	if body.Subject == "" {
		body.Subject = "Amazon SNS 消息"
	}
	if body.Topic != "" {
		body.Message = fmt.Sprintf("Topic: %s\nType: %s\n\n%s", body.Topic, body.Type, body.Message)
	}
	return body.Subject, body.Message, 5, false, nil
}

func (r *Receiver) parseAliyunSMS(c *gin.Context, secret string) (string, string, int, bool, error) {
	if err := verifyHeaderSecret(c, secret, "X-Webhook-Token", "X-Aliyun-SMS-Token"); err != nil {
		return "", "", 0, false, err
	}
	var body struct {
		PhoneNumber string `json:"phone_number"`
		Content     string `json:"content"`
		Template    string `json:"template_code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid aliyun sms payload format: %w", err)
	}
	msg := body.Content
	if body.Template != "" {
		msg = fmt.Sprintf("Template: %s\nPhone: %s\n\n%s", body.Template, body.PhoneNumber, body.Content)
	}
	return "阿里云短信消息", msg, 5, false, nil
}

func (r *Receiver) parseTencentSMS(c *gin.Context, secret string) (string, string, int, bool, error) {
	if err := verifyHeaderSecret(c, secret, "X-Webhook-Token", "X-Tencent-SMS-Token"); err != nil {
		return "", "", 0, false, err
	}
	var body struct {
		PhoneNumber string `json:"phone_number"`
		Content     string `json:"content"`
		Template    string `json:"template_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid tencent sms payload format: %w", err)
	}
	msg := body.Content
	if body.Template != "" {
		msg = fmt.Sprintf("Template: %s\nPhone: %s\n\n%s", body.Template, body.PhoneNumber, body.Content)
	}
	return "腾讯云短信消息", msg, 5, false, nil
}

func (r *Receiver) parseDiscord(c *gin.Context, secret string) (string, string, int, bool, error) {
	if err := verifyHeaderSecret(c, secret, "X-Webhook-Token", "X-Discord-Token"); err != nil {
		return "", "", 0, false, err
	}
	var body struct {
		Content   string `json:"content"`
		Username  string `json:"username"`
		WebhookID string `json:"webhook_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid discord payload format: %w", err)
	}
	title := "Discord 消息"
	if body.Username != "" {
		title = "Discord / " + body.Username
	}
	return title, body.Content, 5, false, nil
}

func (r *Receiver) parseSlack(c *gin.Context, secret string) (string, string, int, bool, error) {
	if err := verifyHeaderSecret(c, secret, "X-Webhook-Token", "X-Slack-Token"); err != nil {
		return "", "", 0, false, err
	}
	var body struct {
		Text        string `json:"text"`
		Username    string `json:"username"`
		ChannelName string `json:"channel_name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, false, fmt.Errorf("invalid slack payload format: %w", err)
	}
	title := "Slack 消息"
	if body.ChannelName != "" {
		title = "Slack / #" + body.ChannelName
	} else if body.Username != "" {
		title = "Slack / " + body.Username
	}
	return title, body.Text, 5, false, nil
}

// --- Custom Incoming ---
func (r *Receiver) parseCustom(c *gin.Context, secret string) (string, string, int, bool, error) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "", "", 0, false, fmt.Errorf("failed to read request body: %w", err)
	}

	contentType := c.GetHeader("Content-Type")
	if strings.HasPrefix(contentType, "text/html") {
		title := "HTML 消息"
		content := string(rawBody)
		if strings.TrimSpace(content) == "" {
			return "", "", 0, false, fmt.Errorf("empty request body")
		}
		extracted := ExtractHTMLTitle(content)
		if extracted != "" {
			title = extracted
		}
		return title, content, 5, true, nil
	}

	if secret != "" {
		sig := c.GetHeader("X-Signature")
		if sig != "" {
			expectedSig, _ := SignCustom(secret, rawBody)
			if sig != expectedSig {
				return "", "", 0, false, fmt.Errorf("custom webhook signature mismatch")
			}
		}
	}

	var body struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Content  string `json:"content"`
		Text     string `json:"text"`
		MsgType  string `json:"msgtype"`
		Priority int    `json:"priority"`
	}

	if err := json.Unmarshal(rawBody, &body); err != nil {
		return "Webhook Message", string(rawBody), 5, false, nil
	}

	message := body.Message
	if message == "" {
		message = body.Content
	}
	if message == "" {
		message = body.Text
	}
	if message == "" {
		message = string(rawBody)
	}

	title := body.Title
	if title == "" {
		title = "Webhook Message"
	}

	priority := body.Priority
	if priority == 0 {
		priority = 5
	}

	isMarkdown := body.MsgType == "markdown"

	return title, message, priority, isMarkdown, nil
}

func verifyHeaderSecret(c *gin.Context, secret string, headerKeys ...string) error {
	if secret == "" {
		return nil
	}
	for _, key := range headerKeys {
		if c.GetHeader(key) == secret {
			return nil
		}
	}
	if c.Query("token") == secret {
		return nil
	}
	return fmt.Errorf("invalid or missing token")
}

// --- Signature Verification Helpers ---
func verifyDingTalkSign(timestamp, secret, expectedSign string) error {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	now := time.Now().UnixMilli()
	if math.Abs(float64(now-ts)) > 3600000 {
		return fmt.Errorf("timestamp expired")
	}

	stringToSign := fmt.Sprintf("%s\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if sign != expectedSign {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func verifyFeishuSign(timestamp, secret, expectedSign string) error {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	now := time.Now().Unix()
	if math.Abs(float64(now-ts)) > 300 {
		return fmt.Errorf("timestamp expired")
	}

	stringToSign := fmt.Sprintf("%s\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(stringToSign))
	mac.Write([]byte(""))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if sign != expectedSign {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
