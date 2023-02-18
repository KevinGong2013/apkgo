package vivo

import (
	"strconv"

	"github.com/KevinGong2013/apkgo/cmd/shared"
)

func (c *Client) Do(req shared.PublishRequest) error {

	updateReq := updateAppRequest{
		OnlineType:       "1",
		CompatibleDevice: "2",
		VersionCode:      strconv.Itoa(int(req.VersionCode)),
		UpdateDesc:       req.UpdateDesc,
		PackageName:      req.PackageName,
	}

	// 上传APK
	if len(req.SecondApkFile) > 0 {
		// 分包
		resp32, err := c.upload32(req.PackageName, req.ApkFile)
		if err != nil {
			return err
		}

		resp64, err := c.upload64(req.PackageName, req.SecondApkFile)
		if err != nil {
			return err
		}

		// 发布
		return c.updateSubPackageApp(resp32.SerialNumber, resp64.SerialNumber, updateReq)
	} else {

		resp, err := c.uploadFlat(req.PackageName, req.ApkFile)
		if err != nil {
			return err
		}

		return c.updateApp(resp.SerialNumber, resp.FileMd5, updateReq)
	}
}
