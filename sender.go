package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Sender handles forwarding raw payloads to webhook endpoints.
type Sender struct {
	client *http.Client
}

// NewSender creates a new Sender with a configured HTTP client.
func NewSender() *Sender {
	return &Sender{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ForwardRaw sends raw body directly to the target webhook URL without reformatting.
// It acts as a transparent proxy: the payload is forwarded exactly as received.
// Only platform-specific signing is applied if a secret is configured:
//   - 钉钉: timestamp + sign 追加到 URL query 参数
//   - 飞书: timestamp + sign 注入到 JSON body
//   - 自定义: X-Signature header
//   - 企业微信: 无需签名
func (s *Sender) ForwardRaw(target TargetConfig, rawBody []byte, contentType string) error {
	webhookURL := target.WebhookURL
	headers := map[string]string{}

	if contentType != "" {
		headers["Content-Type"] = contentType
	} else {
		headers["Content-Type"] = "application/json"
	}

	// 仅做签名处理，不修改消息体格式
	switch target.Platform {
	case "dingtalk":
		if target.Secret != "" {
			ts, sign, err := SignDingTalk(target.Secret)
			if err != nil {
				return fmt.Errorf("signing error for target %s: %w", target.Name, err)
			}
			sep := "&"
			if !strings.Contains(webhookURL, "?") {
				sep = "?"
			}
			webhookURL = fmt.Sprintf("%s%stimestamp=%s&sign=%s", webhookURL, sep, ts, url.QueryEscape(sign))
		}

	case "feishu":
		if target.Secret != "" {
			ts, sign, err := SignFeishu(target.Secret)
			if err != nil {
				return fmt.Errorf("signing error for target %s: %w", target.Name, err)
			}
			// 注入 timestamp 和 sign 到 JSON body
			var payloadMap map[string]interface{}
			if err := json.Unmarshal(rawBody, &payloadMap); err == nil {
				payloadMap["timestamp"] = ts
				payloadMap["sign"] = sign
				if signed, err := json.Marshal(payloadMap); err == nil {
					rawBody = signed
				}
			}
		}

	case "custom":
		if target.Secret != "" {
			sig, err := SignCustom(target.Secret, rawBody)
			if err != nil {
				return fmt.Errorf("signing error for target %s: %w", target.Name, err)
			}
			headers["X-Signature"] = sig
		}
		// 应用自定义请求头
		for k, v := range target.Headers {
			headers[k] = v
		}

	// case "wecom": 企业微信不需要签名处理
	}

	// 决定 HTTP 方法
	method := "POST"
	if target.Method != "" {
		method = strings.ToUpper(target.Method)
	}

	req, err := http.NewRequest(method, webhookURL, bytes.NewReader(rawBody))
	if err != nil {
		return fmt.Errorf("request creation error for target %s: %w", target.Name, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("request error for target %s: %w", target.Name, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("target %s returned status %d: %s", target.Name, resp.StatusCode, string(body))
	}

	log.Printf("[webhook-plugin] Forwarded to %s (%s), response: %s", target.Name, target.Platform, string(body))
	return nil
}
