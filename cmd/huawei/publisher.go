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

	// 需要3分钟后再尝试提交审核

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
			return fmt.Errorf("失败太多次了，请前往华为后台检查")
		}

		times++
	}

	return nil
}
