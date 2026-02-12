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
// Returns the parsed title, message, and priority.
func (r *Receiver) ParseAndVerify(c *gin.Context, platform, secret string) (title, message string, priority int, err error) {
	switch platform {
	case "wecom":
		return r.parseWeCom(c)
	case "dingtalk":
		return r.parseDingTalk(c, secret)
	case "feishu":
		return r.parseFeishu(c, secret)
	case "custom":
		return r.parseCustom(c, secret)
	default:
		return "", "", 0, fmt.Errorf("unsupported platform: %s, supported: wecom/dingtalk/feishu/custom", platform)
	}
}

// --- WeCom (企业微信) Incoming ---
// 企业微信 webhook 回调不带签名，消息格式为 JSON
func (r *Receiver) parseWeCom(c *gin.Context) (string, string, int, error) {
	var body struct {
		MsgType string `json:"msgtype"`
		Text    struct {
			Content        string   `json:"content"`
			MentionedList  []string `json:"mentioned_list,omitempty"`
		} `json:"text"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", 0, fmt.Errorf("invalid wecom message format: %w", err)
	}

	switch body.MsgType {
	case "markdown":
		return "企业微信消息", body.Markdown.Content, 5, nil
	case "text":
		return "企业微信消息", body.Text.Content, 5, nil
	default:
		if body.Text.Content != "" {
			return "企业微信消息", body.Text.Content, 5, nil
		}
		return "企业微信消息", fmt.Sprintf("[不支持的消息类型: %s]", body.MsgType), 3, nil
	}
}

// --- DingTalk (钉钉) Incoming ---
// 钉钉签名验证: timestamp + "\n" + secret → HMAC-SHA256 → Base64
// 签名通过 URL 参数 timestamp 和 sign 传入
func (r *Receiver) parseDingTalk(c *gin.Context, secret string) (string, string, int, error) {
	// 验证签名
	if secret != "" {
		timestamp := c.Query("timestamp")
		sign := c.Query("sign")
		if timestamp == "" || sign == "" {
			return "", "", 0, fmt.Errorf("dingtalk signature required: missing timestamp or sign parameter")
		}
		if err := verifyDingTalkSign(timestamp, secret, sign); err != nil {
			return "", "", 0, fmt.Errorf("dingtalk signature verification failed: %w", err)
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
		return "", "", 0, fmt.Errorf("invalid dingtalk message format: %w", err)
	}

	switch body.MsgType {
	case "markdown":
		title := body.Markdown.Title
		if title == "" {
			title = "钉钉消息"
		}
		return title, body.Markdown.Text, 5, nil
	case "actionCard":
		title := body.ActionCard.Title
		if title == "" {
			title = "钉钉消息"
		}
		return title, body.ActionCard.Text, 5, nil
	case "text":
		return "钉钉消息", body.Text.Content, 5, nil
	default:
		if body.Text.Content != "" {
			return "钉钉消息", body.Text.Content, 5, nil
		}
		return "钉钉消息", fmt.Sprintf("[不支持的消息类型: %s]", body.MsgType), 3, nil
	}
}

// --- Feishu (飞书) Incoming ---
// 飞书签名验证: timestamp + "\n" + secret → HMAC-SHA256 → Base64
// 签名通过 JSON body 中的 timestamp 和 sign 字段传入
func (r *Receiver) parseFeishu(c *gin.Context, secret string) (string, string, int, error) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to read feishu request body: %w", err)
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
		return "", "", 0, fmt.Errorf("invalid feishu message format: %w", err)
	}

	// 验证签名
	if secret != "" {
		if fullBody.Timestamp == "" || fullBody.Sign == "" {
			return "", "", 0, fmt.Errorf("feishu signature required: missing timestamp or sign in body")
		}
		if err := verifyFeishuSign(fullBody.Timestamp, secret, fullBody.Sign); err != nil {
			return "", "", 0, fmt.Errorf("feishu signature verification failed: %w", err)
		}
	}

	switch fullBody.MsgType {
	case "text":
		return "飞书消息", fullBody.Content.Text, 5, nil
	case "interactive":
		title := fullBody.Card.Header.Title.Content
		if title == "" {
			title = "飞书卡片消息"
		}
		return title, string(rawBody), 5, nil
	default:
		if fullBody.Content.Text != "" {
			return "飞书消息", fullBody.Content.Text, 5, nil
		}
		return "飞书消息", string(rawBody), 3, nil
	}
}

// --- Custom Incoming ---
// 自定义格式：简单 JSON，签名通过 X-Signature header 或 URL token 参数验证
func (r *Receiver) parseCustom(c *gin.Context, secret string) (string, string, int, error) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to read request body: %w", err)
	}

	// 验证 X-Signature header 签名
	if secret != "" {
		sig := c.GetHeader("X-Signature")
		if sig != "" {
			expectedSig, _ := SignCustom(secret, rawBody)
			if sig != expectedSig {
				return "", "", 0, fmt.Errorf("custom webhook signature mismatch")
			}
		}
	}

	var body struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Content  string `json:"content"`
		Text     string `json:"text"`
		Priority int    `json:"priority"`
	}

	if err := json.Unmarshal(rawBody, &body); err != nil {
		// 如果不是 JSON，当作纯文本
		return "Webhook Message", string(rawBody), 5, nil
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

	return title, message, priority, nil
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
