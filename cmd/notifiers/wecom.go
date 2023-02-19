package notifiers

import (
	"fmt"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-resty/resty/v2"
)

type WeComNotifier struct {
	Key string
}

func (w *WeComNotifier) BuildAppPubishedMessage(req *shared.PublishRequest, result, customMsg string) string {
	jsonStr := fmt.Sprintf(`{
		"msgtype": "news",
		"news": {
			"articles": [
				{
					"title": "apkgo应用发布结束",
					"description": "%s(%s)  %s\n at %s\n%s\n%s",
				}]
		}
	}`, req.AppName, req.VersionName, req.PackageName, time.Now(), result, customMsg)
	return jsonStr
}

func (w *WeComNotifier) Notify(jsonStr string) error {

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", w.Key)
	resp, err := resty.New().R().SetBody(jsonStr).SetHeader("Content-Type", "application/json").Post(url)

	if err != nil {
		return err
	}

	if resp.StatusCode() >= 400 {
		return fmt.Errorf("请求失败 %s, %s", resp.Status(), string(resp.Body()))
	}
	return nil
}
