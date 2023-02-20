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

// https://open.dingtalk.com/document/group/custom-robot-access

type DingTalkNotifier struct {
	AccessToken string `json:"access_token"`
	SecretToken string `json:"secret_token"`
}

func (d *DingTalkNotifier) BuildAppPubishedMessage(req shared.PublishRequest, result map[string]string) string {

	builder := new(strings.Builder)

	builder.WriteString("# apkgo应用发布结束\n")
	builder.WriteString(fmt.Sprintf("**%s(%s)** %s\n\n", req.AppName, req.Version(), req.PackageName))

	var failed []string
	for k, v := range result {
		if len(v) == 0 {
			builder.WriteString(fmt.Sprintf("%s上传成功\n\n", k))
		} else {
			builder.WriteString(fmt.Sprintf("❌%s 上传失败 %s\n\n", k, v))
			failed = append(failed, k)
		}
	}
	if len(failed) == 0 {
		builder.WriteString("\n所有平台上传成功✅")
	} else if len(failed) == len(result) {
		builder.WriteString("\n所有平台上传失败❌")
	} else {
		builder.WriteString(fmt.Sprintf("\n\n%s 上传失败，请检查", strings.Join(failed, ",")))
	}

	builder.WriteString(fmt.Sprintf("\n\n%s", time.Now().Format(time.RFC3339)))

	jsonStr := fmt.Sprintf(`{
		"msgtype": 'markdown',
		"markdown": {
		  "title": "apkgo应用发布结束",
		  "text": "%s"
		}
	  }`, builder.String())

	return jsonStr
}

func (d *DingTalkNotifier) Notify(jsonStr string) error {

	url := fmt.Sprintf("https://oapi.dingtalk.com/robot/send?access_token=%s", d.AccessToken)

	if d.SecretToken != "" {
		timestamp := time.Now().UnixMilli()
		signature, err := sign(d.SecretToken, timestamp)
		if err != nil {
			return err
		}
		url = fmt.Sprintf("%s&timestamp=%d&sign=%s", url, timestamp, signature)
	}

	resp, err := resty.New().R().SetBody(jsonStr).SetHeader("Content-Type", "application/json").Post(url)

	if err != nil {
		return err
	}
	if resp.StatusCode() >= 400 {
		return fmt.Errorf("请求失败 %s, %s", resp.Status(), string(resp.Body()))
	}

	return nil

}

func sign(secret string, t int64) (string, error) {
	strToHash := fmt.Sprintf("%d\n%s", t, secret)
	hmac256 := hmac.New(sha256.New, []byte(secret))
	hmac256.Write([]byte(strToHash))
	data := hmac256.Sum(nil)
	return base64.StdEncoding.EncodeToString(data), nil
}
