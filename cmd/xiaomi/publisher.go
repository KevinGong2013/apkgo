package xiaomi

import (
	"fmt"

	"github.com/KevinGong2013/apkgo/cmd/shared"
)

func (c *Client) Do(req shared.PublishRequest) error {

	// 1.  先查一下具体情况
	info, err := c.query(req.PackageName)
	if err != nil {
		return err
	}

	if info.VersionCode >= int(req.VersionCode) {
		return fmt.Errorf("小米应用商店在架版本(%d)大于当前版本号(%d)", info.VersionCode, req.VersionCode)
	}

	// 2. 可以去发布了
	return c.push(req.SynchroType, req.ApkFile, req.SecondApkFile, appInfoRequest{
		AppName:     req.AppName,
		PackageName: req.PackageName,
		UpdateDesc:  &req.UpdateDesc,
	})
}
