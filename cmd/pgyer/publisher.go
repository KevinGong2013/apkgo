package pgyer

import (
	"errors"
	"fmt"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
)

func (c *Client) Name() string {
	return "蒲公英应用分发平台"
}

func (c *Client) Do(req shared.PublishRequest) error {

	resp, err := c.getCosToken(req.UpdateDesc)
	if err != nil {
		return err
	}

	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	if resp.Code != 0 {
		return fmt.Errorf("get cos token failed. %s", resp.Message)
	}

	resp.Data.Params["key"] = resp.Data.Key

	r, err := c.restyClient.R().
		SetFormData(resp.Data.Params).
		SetFile("file", req.ApkFile).
		SetResult(&response).
		Post(resp.Data.Endpoint)
	if err != nil {
		return err
	}
	if r.StatusCode() != 204 {
		return fmt.Errorf("upload file failed. %s", response.Message)
	}

	t := time.NewTicker(time.Second * 30)
	defer t.Stop()

	retryTimes := 0
	for range t.C {
		if retryTimes > 100 {
			return errors.New("更新app超时")
		}
		retryTimes++
		// 在这一步中上传 App 成功后，App 会自动进入服务器后台队列继续后续的发布流程。所以，在这一步中 App 上传完成后，并不代表 App 已经完成发布。一般来说，一般1分钟以内就能完成发布。要检查是否发布完成，请调用下一步中的 API。
		bf, err := c.buildInfo(resp.Data.Key)

		if err != nil {
			return err
		}

		if bf.Code == 1216 {
			return fmt.Errorf("update app failed. %s", bf.Message)
		}

		if len(bf.Data.Updated) > 0 {
			return nil
		}
	}

	return nil
}
