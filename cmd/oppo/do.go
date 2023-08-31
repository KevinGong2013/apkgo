package oppo

import (
	"errors"
	"strconv"
	"time"

	"github.com/KevinGong2013/apkgo/cmd/shared"
)

func (c *Client) Do(req shared.PublishRequest) error {

	param := publishRequestParameter{}

	param.PkgName = req.PackageName
	param.VersionCode = strconv.Itoa(int(req.VersionCode))

	// 1. 获取一批基础信息
	app, err := c.query(req.PackageName)
	if err != nil {
		return err
	}
	param.AppName = app.AppName

	secondCategoryId, _ := strconv.Atoi(app.SecondCategoryID)
	param.SecondCategoryId = secondCategoryId

	thirdCategoryId, _ := strconv.Atoi(app.ThirdCategoryID)
	param.ThirdCategoryId = thirdCategoryId

	param.Summary = "[寓小二]公寓系统定制专家" //app.Summary
	param.DetailDesc = app.DetailDesc
	param.UpdateDesc = req.UpdateDesc
	param.PrivacySourceUrl = app.PrivacySourceURL
	param.IconUrl = app.IconURL
	param.PicUrl = app.PicURL
	param.OnlineType = 1
	param.TestDesc = "submit by apkgo tool."
	param.CopyrightUrl = app.CopyrightURL
	param.BusinessUsername = app.BusinessUsername
	param.BusinessMobile = app.BusinessMobile
	param.BusinessEmail = app.BusinessEmail

	ageLevel, _ := strconv.Atoi(app.AgeLevel)
	param.AgeLevel = ageLevel

	param.AdaptiveType = app.AdaptiveType
	param.AdaptiveEquipment = app.AdaptiveEquipment

	param.CustomerContact = app.CustomerContact

	// 2. 上传apk包
	uploadResult, err := c.uploadAPK(req.ApkFile)
	if err != nil {
		return err
	}

	cpucode := 0

	if len(req.SecondApkFile) > 0 {
		cpucode = 32
	}

	param.ApkUrl = append(param.ApkUrl, apkInfo{
		Url:     uploadResult.URL,
		Md5:     uploadResult.MD5,
		CpuCode: cpucode,
	})

	if len(req.SecondApkFile) > 0 {
		secondResult, err := c.uploadAPK(req.SecondApkFile)
		if err != nil {
			return err
		}
		param.ApkUrl = append(param.ApkUrl, apkInfo{
			Url:     secondResult.URL,
			Md5:     secondResult.MD5,
			CpuCode: 64,
		})
	}

	// 3. 发布
	if err := c.publish(param); err != nil {
		return err
	}

	times := 0
	for {
		if times > 10 {
			return errors.New("发布失败")
		}
		time.Sleep(time.Second * 10)
		state, err := c.taskState(req.PackageName, strconv.Itoa(int(req.VersionCode)))
		if err != nil {
			return err
		}
		// 成功
		if state.TaskState == "2" {
			return nil
		}
		if state.TaskState == "3" {
			return errors.New(state.ErrMsg)
		}
		times += 1
	}
}
