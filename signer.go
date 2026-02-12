package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
)

// SignDingTalk generates the DingTalk webhook signature.
// DingTalk signs by: HMAC-SHA256(timestamp + "\n" + secret, key=secret) then Base64.
// Returns the timestamp and sign strings to be appended as URL query parameters.
func SignDingTalk(secret string) (timestamp string, sign string, err error) {
	if secret == "" {
		return "", "", nil
	}
	ts := time.Now().UnixMilli()
	timestamp = strconv.FormatInt(ts, 10)
	stringToSign := fmt.Sprintf("%s\n%s", timestamp, secret)

	mac := hmac.New(sha256.New, []byte(secret))
	_, err = mac.Write([]byte(stringToSign))
	if err != nil {
		return "", "", fmt.Errorf("failed to compute HMAC: %w", err)
	}
	sign = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return timestamp, sign, nil
}

// SignFeishu generates the Feishu webhook signature.
// Feishu signs by: HMAC-SHA256(key = timestamp + "\n" + secret, data = "").
// Returns the timestamp and sign strings to be included in the JSON payload.
func SignFeishu(secret string) (timestamp string, sign string, err error) {
	if secret == "" {
		return "", "", nil
	}
	ts := time.Now().Unix()
	timestamp = strconv.FormatInt(ts, 10)
	stringToSign := fmt.Sprintf("%s\n%s", timestamp, secret)

	mac := hmac.New(sha256.New, []byte(stringToSign))
	mac.Write([]byte(""))
	sign = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return timestamp, sign, nil
}

// SignCustom generates a generic HMAC-SHA256 signature for custom webhooks.
// Returns the signature as "sha256=<hex>", suitable for an X-Signature header.
func SignCustom(secret string, payload []byte) (string, error) {
	if secret == "" {
		return "", nil
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, err := mac.Write(payload)
	if err != nil {
		return "", fmt.Errorf("failed to compute HMAC: %w", err)
	}
	return fmt.Sprintf("sha256=%x", mac.Sum(nil)), nil
}
