package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/KevinGong2013/apkgo/cmd/fir"
	"github.com/KevinGong2013/apkgo/cmd/huawei"
	"github.com/KevinGong2013/apkgo/cmd/pgyer"
	"github.com/KevinGong2013/apkgo/cmd/shared"
	"github.com/KevinGong2013/apkgo/cmd/vivo"
	"github.com/KevinGong2013/apkgo/cmd/xiaomi"

	"github.com/shogo82148/androidbinary/apk"
)

var publishers []shared.Publisher

func InitialPublishers(filter []string) error {
	cfgFileBytes, err := os.ReadFile(cfgFile)
	if err != nil {
		return err
	}

	config := new(Config)

	if err = json.Unmarshal(cfgFileBytes, config); err != nil {
		return err
	}

	for _, f := range filter {
		if v, ok := config.Publishers[f]; ok {
			switch f {
			case "xiaomi":
				xm, err := xiaomi.NewClient(v["username"], v["private_key"])
				if err != nil {
					return err
				}
				publishers = append(publishers, xm)
			case "vivo":
				vv, err := vivo.NewClient(v["access_key"], v["access_secret"])
				if err != nil {
					return err
				}
				publishers = append(publishers, vv)
			case "huawei":
				hw, err := huawei.NewClient(v["client_id"], v["client_secret"])
				if err != nil {
					return err
				}
				publishers = append(publishers, hw)
			case "pgyer":
				publishers = append(publishers, pgyer.NewClient(v["api_key"]))
			case "fir":
				publishers = append(publishers, fir.NewClient(v["api_token"]))
			}
		}
	}

	return nil
}

func Do(updateDesc string, apkFile ...string) error {
	if len(apkFile) == 0 {
		return errors.New("请指定apk文件路径")
	}
	if len(apkFile) > 2 {
		return errors.New("仅支持一个 fat apk，或者 32和64的安装包，不支持多个文件")
	}
	//
	pkg, _ := apk.OpenFile(apkFile[0])
	defer pkg.Close()

	secondApkFile := ""
	if len(apkFile) == 2 {
		secondApkFile = apkFile[1]
	}
	//
	req := shared.PublishRequest{
		AppName:     pkg.Manifest().App.Label.MustString(),
		PackageName: pkg.PackageName(),
		VersionCode: pkg.Manifest().VersionCode.MustInt32(),
		VersionName: pkg.Manifest().VersionName.MustString(),

		ApkFile:       apkFile[0],
		SecondApkFile: secondApkFile,
		UpdateDesc:    updateDesc,
		// 更新
		SynchroType: 1,
	}

	for i, p := range publishers {
		fmt.Printf("index %d \n", i)

		p.Do(req)
	}

	return nil
}
