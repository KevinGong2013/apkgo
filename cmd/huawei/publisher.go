package huawei

import (
	"fmt"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
)

func (c *Client) Name() string {
	return "华为AppGalleryConnect"
}

func (c *Client) Do(req shared.PublishRequest) error {

	appId, err := c.fetchAppId(req.PackageName)
	if err != nil {
		return err
	}

	// 上传apk
	if err = c.upload(appId, req.ApkFile); err != nil {
		return err
	}

	// 需要2分钟后再尝试提交审核
	// 软件包采用异步解析方式，请您在传包后等候2分钟再调用提交发布接口。
	time.Sleep(time.Minute * 2)

	// 提交发布
	// 1分钟执行执行一次
	times := 0
	t := time.NewTicker(time.Minute)
	defer t.Stop()

	for range t.C {
		r := c.submitApp(appId)
		if r.Code == 0 {
			t.Stop()
			return nil
		}

		if times >= 10 {
			return fmt.Errorf("apk上传成功但提交审核失败，请前往华为后台手动提交发布。https://developer.huawei.com/consumer/cn/console#/serviceCards/")
		}

		times++
	}

	return nil
}
