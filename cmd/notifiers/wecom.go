package notifiers

import (
	"fmt"
	"strings"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/go-resty/resty/v2"
)

type WeComNotifier struct {
	Key string
}

func (w *WeComNotifier) BuildAppPubishedMessage(req shared.PublishRequest, result map[string]string) string {

	builder := new(strings.Builder)

	builder.WriteString(fmt.Sprintf(`# apkgo应用发布结束 \n<font color=\"info\">%s(%s) %s</font>\n`, req.AppName, req.VersionName, req.PackageName))

	for k, v := range result {
		if v == "" {
			builder.WriteString(fmt.Sprintf(`%s: <font color=\"info\">成功</font> \n`, k))
		} else {
			builder.WriteString(fmt.Sprintf(`%s: <font color=\"warning\">%s</font> \n`, k, v))
		}
	}

	builder.WriteString(fmt.Sprintf(`<font color=\"comment\">%s</font>`, time.Now().Format(time.RFC3339)))

	jsonStr := fmt.Sprintf(`{
		"msgtype": "markdown",
		"markdown": {
			"content": "%s"
		}
	}`, builder.String())
	return jsonStr
}

func (w *WeComNotifier) Notify(jsonStr string) error {

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", w.Key)
	resp, err := resty.New().R().
		SetBody(jsonStr).
		SetHeader("Content-Type", "application/json").
		Post(url)

	if err != nil {
		return err
	}

	if resp.StatusCode() >= 400 {
		return fmt.Errorf("请求失败 %s, %s", resp.Status(), string(resp.Body()))
	}

	return nil
}
