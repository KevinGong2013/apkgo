package notifiers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-resty/resty/v2"
)

type LarkNotifier struct {
	Key         string `json:"key,omitempty"`
	SecretToken string `json:"secret_token"`
}

// https://open.feishu.cn/document/ukTMukTMukTM/ucTM5YjL3ETO24yNxkjN

func (l *LarkNotifier) BuildAppPubishedMessage(req shared.PublishRequest, result map[string]string) string {
	builder := new(strings.Builder)

	builder.WriteString(fmt.Sprintf("**%s(%s)** %s\\n\\n", req.AppName, req.Version(), req.PackageName))

	var failed []string
	for k, v := range result {
		if len(v) == 0 {
			builder.WriteString(fmt.Sprintf("👌%s上传成功\\n", k))
		} else {
			builder.WriteString(fmt.Sprintf("❌%s err: %s\\n", k, v))
			failed = append(failed, k)
		}
	}
	if len(failed) == 0 {
		builder.WriteString("\\n👏👏👏 所有平台上传成功")
	} else if len(failed) == len(result) {
		builder.WriteString("\\n😢😢😢 所有平台上传失败")
	} else {
		builder.WriteString(fmt.Sprintf("%s 上传失败，请检查", strings.Join(failed, ",")))
	}

	partialJSON := fmt.Sprintf(`
	"msg_type": "interactive",
	"card": {
		"config": {
			"wide_screen_mode": true
		},
		"elements": [
			{
			"tag": "markdown",
			"content": "%s"
			},
			{
			"tag": "note",
			"elements": [
				{
				"tag": "plain_text",
				"content": "%s"
				}
			]
			}
		],
		"header": {
			"template": "green",
			"title": {
			"content": "apkgo应用发布结束",
			"tag": "plain_text"
			}
		}
	}
	`, builder.String(),
		time.Now().Format(time.RFC3339),
	)

	return partialJSON
}

func (l *LarkNotifier) Notify(partialJsonStr string) error {
	var jsonStr string
	if l.SecretToken != "" {
		timestamp := time.Now().Unix()
		signature, err := GenSign(l.SecretToken, timestamp)
		if err != nil {
			return err
		}

		jsonStr = fmt.Sprintf(`{
			"timestamp": %v,
			"sign": "%s",
			%s
		}`, timestamp, signature, partialJsonStr)

	} else {
		jsonStr = fmt.Sprintf(`{
			%s
		} `, partialJsonStr)
	}

	url := fmt.Sprintf("https://open.feishu.cn/open-apis/bot/v2/hook/%s", l.Key)
	resp, err := resty.New().R().SetBody(jsonStr).SetHeader("Content-Type", "application/json").Post(url)

	if err != nil {
		return err
	}
	if resp.StatusCode() >= 400 {
		return fmt.Errorf("请求失败 %s, %s", resp.Status(), string(resp.Body()))
	}

	return nil
}

func GenSign(secret string, timestamp int64) (string, error) {
	//Encode timestamp + key with SHA256, and then with Base64
	stringToSign := fmt.Sprintf("%v", timestamp) + "\n" + secret
	var data []byte
	h := hmac.New(sha256.New, []byte(stringToSign))
	_, err := h.Write(data)
	if err != nil {
		return "", err
	}
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return signature, nil
}
