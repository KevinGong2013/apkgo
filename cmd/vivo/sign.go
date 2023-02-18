package vivo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (c *Client) assembleParams(method string, bizParams map[string]string) map[string]string {

	// 将公共参数和业务参数放入一个map中，并按照ASCII码排序
	params := map[string]string{
		"method":         method,
		"access_key":     c.accessKey,
		"timestamp":      fmt.Sprintf("%d", time.Now().UnixMilli()),
		"format":         "json",
		"v":              "1.0",
		"sign_method":    "HMAC-SHA256",
		"target_app_key": "developer",
	}

	for k, v := range bizParams {
		params[k] = v
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 按照排序后的顺序将参数拼接成一个字符串
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString("&")
		}
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(params[k])
	}
	paramStr := sb.String()

	// 将参数字符串转换为byte数组
	paramBytes := []byte(paramStr)

	// 使用HmacSHA256加密
	hasher := hmac.New(sha256.New, c.accessSecretBytes)
	hasher.Write(paramBytes)
	signature := hasher.Sum(nil)

	// 将加密后的byte数组转换为16进制字符串
	signStr := hex.EncodeToString(signature)

	params["sign"] = signStr

	return params
}
