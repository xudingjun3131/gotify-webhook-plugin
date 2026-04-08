package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	aliopenapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	alisms "github.com/alibabacloud-go/dysmsapi-20170525/v4/client"
	aliutil "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	profile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	sms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

// Sender handles forwarding raw payloads to webhook endpoints.
type Sender struct {
	client *http.Client
}

// NewSender creates a new Sender with a configured HTTP client.
func NewSender() *Sender {
	return &Sender{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// ForwardRaw sends raw body directly to the target endpoint without reformatting.
func (s *Sender) ForwardRaw(target TargetConfig, rawBody []byte, contentType string) error {
	switch target.Platform {
	case "email":
		return s.sendEmail(target, rawBody, contentType)
	case "sns":
		return s.sendSNS(target, rawBody)
	case "aliyun-sms":
		return s.sendAliyunSMS(target, rawBody)
	case "tencent-sms":
		return s.sendTencentSMS(target, rawBody)
	}

	webhookURL := target.WebhookURL
	headers := map[string]string{}
	if contentType != "" {
		headers["Content-Type"] = contentType
	} else {
		headers["Content-Type"] = "application/json"
	}

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
			var payloadMap map[string]interface{}
			if err := json.Unmarshal(rawBody, &payloadMap); err == nil {
				payloadMap["timestamp"] = ts
				payloadMap["sign"] = sign
				if signed, err := json.Marshal(payloadMap); err == nil {
					rawBody = signed
				}
			}
		}
	case "telegram":
		if target.Secret != "" {
			headers["X-Telegram-Bot-Api-Secret-Token"] = target.Secret
		}
	case "discord", "slack":
		if target.Secret != "" {
			headers["X-Webhook-Token"] = target.Secret
		}
	case "custom":
		if target.Secret != "" {
			sig, err := SignCustom(target.Secret, rawBody)
			if err != nil {
				return fmt.Errorf("signing error for target %s: %w", target.Name, err)
			}
			headers["X-Signature"] = sig
		}
		for k, v := range target.Headers {
			headers[k] = v
		}
	}

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

func (s *Sender) sendEmail(target TargetConfig, rawBody []byte, contentType string) error {
	if target.WebhookURL == "" {
		return fmt.Errorf("email target %s: webhook_url is required, format smtp://host:port", target.Name)
	}
	u, err := url.Parse(target.WebhookURL)
	if err != nil {
		return fmt.Errorf("email target %s: invalid smtp url: %w", target.Name, err)
	}
	if u.Scheme != "smtp" && u.Scheme != "smtps" {
		return fmt.Errorf("email target %s: webhook_url must use smtp or smtps scheme", target.Name)
	}
	if len(target.EmailTo) == 0 {
		return fmt.Errorf("email target %s: email_to is required", target.Name)
	}

	host := u.Hostname()
	addr := u.Host
	username := ""
	password := ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	from := target.EmailFrom
	if from == "" {
		from = username
	}
	if from == "" {
		from = "gotify@localhost"
	}

	subject, body := deriveEmailContent(rawBody, contentType, target)
	mimeType := "text/plain; charset=UTF-8"
	if IsHTML(body) || strings.Contains(strings.ToLower(contentType), "text/html") {
		mimeType = "text/html; charset=UTF-8"
	}

	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(target.EmailTo, ",")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", encodeMailSubject(subject)))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: %s\r\n", mimeType))
	msg.WriteString("\r\n")
	msg.WriteString(body)

	if u.Scheme == "smtps" {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
		if err != nil {
			return fmt.Errorf("email target %s: smtps dial failed: %w", target.Name, err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return fmt.Errorf("email target %s: create smtp client failed: %w", target.Name, err)
		}
		defer client.Quit()
		if username != "" {
			auth := smtp.PlainAuth("", username, password, host)
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("email target %s: smtp auth failed: %w", target.Name, err)
			}
		}
		if err := client.Mail(from); err != nil {
			return fmt.Errorf("email target %s: MAIL FROM failed: %w", target.Name, err)
		}
		for _, rcpt := range target.EmailTo {
			if err := client.Rcpt(rcpt); err != nil {
				return fmt.Errorf("email target %s: RCPT TO failed for %s: %w", target.Name, rcpt, err)
			}
		}
		wc, err := client.Data()
		if err != nil {
			return fmt.Errorf("email target %s: DATA failed: %w", target.Name, err)
		}
		if _, err := wc.Write(msg.Bytes()); err != nil {
			return fmt.Errorf("email target %s: write body failed: %w", target.Name, err)
		}
		if err := wc.Close(); err != nil {
			return fmt.Errorf("email target %s: close body failed: %w", target.Name, err)
		}
		return nil
	}

	var auth smtp.Auth
	if username != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}
	if err := smtp.SendMail(addr, auth, from, target.EmailTo, msg.Bytes()); err != nil {
		return fmt.Errorf("email target %s: send failed: %w", target.Name, err)
	}
	return nil
}

func (s *Sender) sendSNS(target TargetConfig, rawBody []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	loadOptions := []func(*awsconfig.LoadOptions) error{}
	if target.Region != "" {
		loadOptions = append(loadOptions, awsconfig.WithRegion(target.Region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return fmt.Errorf("sns target %s: load aws config failed: %w", target.Name, err)
	}

	client := sns.NewFromConfig(cfg)
	subject, message := extractSubjectAndMessage(rawBody, target)
	input := &sns.PublishInput{Message: aws.String(message)}
	if target.TopicARN != "" {
		input.TopicArn = aws.String(target.TopicARN)
	} else if len(target.PhoneNumbers) > 0 {
		input.PhoneNumber = aws.String(target.PhoneNumbers[0])
	} else {
		return fmt.Errorf("sns target %s: topic_arn or phone_numbers is required", target.Name)
	}
	if subject != "" {
		input.Subject = aws.String(subject)
	}
	if _, err := client.Publish(ctx, input); err != nil {
		return fmt.Errorf("sns target %s: publish failed: %w", target.Name, err)
	}
	return nil
}

func (s *Sender) sendAliyunSMS(target TargetConfig, rawBody []byte) error {
	if len(target.PhoneNumbers) == 0 || target.TemplateCode == "" || target.SignName == "" {
		return fmt.Errorf("aliyun-sms target %s: phone_numbers/template_code/sign_name are required", target.Name)
	}
	accessKeyID, accessKeySecret := splitCredential(target.Secret)
	if accessKeyID == "" || accessKeySecret == "" {
		return fmt.Errorf("aliyun-sms target %s: secret must be ACCESS_KEY_ID:ACCESS_KEY_SECRET", target.Name)
	}

	endpoint := "dysmsapi.aliyuncs.com"
	if target.Region != "" {
		endpoint = fmt.Sprintf("dysmsapi.%s.aliyuncs.com", target.Region)
	}
	cfg := &aliopenapi.Config{
		AccessKeyId:     &accessKeyID,
		AccessKeySecret: &accessKeySecret,
		Endpoint:        &endpoint,
	}
	client, err := alisms.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("aliyun-sms target %s: create client failed: %w", target.Name, err)
	}

	_, message := extractSubjectAndMessage(rawBody, target)
	paramsJSON, _ := json.Marshal(map[string]string{"content": truncate(message, 200)})
	phoneNumbers := strings.Join(target.PhoneNumbers, ",")
	request := &alisms.SendSmsRequest{
		PhoneNumbers:  &phoneNumbers,
		SignName:      &target.SignName,
		TemplateCode:  &target.TemplateCode,
		TemplateParam: aws.String(string(paramsJSON)),
	}
	runtime := &aliutil.RuntimeOptions{}
	if _, err := client.SendSmsWithOptions(request, runtime); err != nil {
		return fmt.Errorf("aliyun-sms target %s: send failed: %w", target.Name, err)
	}
	return nil
}

func (s *Sender) sendTencentSMS(target TargetConfig, rawBody []byte) error {
	appID := target.SMSAppID
	if appID == "" {
		appID = target.WebhookURL
	}
	if len(target.PhoneNumbers) == 0 || target.TemplateCode == "" || target.SignName == "" || target.Region == "" || appID == "" {
		return fmt.Errorf("tencent-sms target %s: phone_numbers/template_code/sign_name/region/sms_app_id are required", target.Name)
	}
	secretID, secretKey := splitCredential(target.Secret)
	if secretID == "" || secretKey == "" {
		return fmt.Errorf("tencent-sms target %s: secret must be SECRET_ID:SECRET_KEY", target.Name)
	}

	cred := tccommon.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	client, err := sms.NewClient(cred, target.Region, cpf)
	if err != nil {
		return fmt.Errorf("tencent-sms target %s: create client failed: %w", target.Name, err)
	}

	_, message := extractSubjectAndMessage(rawBody, target)
	request := sms.NewSendSmsRequest()
	request.PhoneNumberSet = tccommon.StringPtrs(target.PhoneNumbers)
	request.SmsSdkAppId = tccommon.StringPtr(appID)
	request.SignName = tccommon.StringPtr(target.SignName)
	request.TemplateId = tccommon.StringPtr(target.TemplateCode)
	request.TemplateParamSet = tccommon.StringPtrs([]string{truncate(message, 200)})
	if _, err := client.SendSms(request); err != nil {
		return fmt.Errorf("tencent-sms target %s: send failed: %w", target.Name, err)
	}
	return nil
}

func deriveEmailContent(rawBody []byte, contentType string, target TargetConfig) (string, string) {
	subject, message := extractSubjectAndMessage(rawBody, target)
	if subject == "" {
		subject = "Gotify Notification"
	}
	if message == "" {
		message = string(rawBody)
	}
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data") {
		if parsedSubject, parsedBody := parseMultipartEmailPayload(rawBody, contentType); parsedBody != "" {
			if parsedSubject != "" {
				subject = parsedSubject
			}
			message = parsedBody
		}
	}
	return subject, message
}

func parseMultipartEmailPayload(rawBody []byte, contentType string) (string, string) {
	idx := strings.Index(contentType, "boundary=")
	if idx < 0 {
		return "", ""
	}
	boundary := strings.Trim(strings.TrimSpace(contentType[idx+9:]), `"`)
	reader := multipart.NewReader(bytes.NewReader(rawBody), boundary)
	fields := map[string]string{}
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		data, _ := io.ReadAll(part)
		fields[part.FormName()] = string(data)
	}
	body := fields["text"]
	if body == "" {
		body = fields["html"]
	}
	return fields["subject"], body
}

func extractSubjectAndMessage(rawBody []byte, target TargetConfig) (string, string) {
	var payload map[string]interface{}
	if err := json.Unmarshal(rawBody, &payload); err == nil {
		subject := asString(payload["subject"])
		if subject == "" {
			subject = asString(payload["title"])
		}
		message := asString(payload["message"])
		if message == "" {
			message = asString(payload["content"])
		}
		if message == "" {
			message = asString(payload["text"])
		}
		if target.EmailSubject != "" {
			subject = target.EmailSubject
		}
		if target.Subject != "" {
			subject = target.Subject
		}
		if subject != "" || message != "" {
			return subject, message
		}
	}
	if target.EmailSubject != "" {
		return target.EmailSubject, string(rawBody)
	}
	if target.Subject != "" {
		return target.Subject, string(rawBody)
	}
	return "", string(rawBody)
}

func asString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		return ""
	}
}

func splitCredential(secret string) (string, string) {
	parts := strings.SplitN(secret, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func encodeMailSubject(subject string) string {
	return fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(subject)))
}
