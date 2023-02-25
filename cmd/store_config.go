package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type StoreConfig struct {
	Publishers map[string]map[string]string `json:"stores"`
	Notifiers  Notifiers                    `json:"notifiers,omitempty"`
}

func parseStoreSecretFile() (*StoreConfig, error) {

	storeCfgFile := filepath.Join(apkgoHome, StoreConfigFileName)

	bytes, err := os.ReadFile(storeCfgFile)
	if err != nil {
		return nil, err
	}

	var conf StoreConfig

	// 解析config文件
	if err = json.Unmarshal(bytes, &conf); err != nil {
		// fmt.Println(text.FgRed.Sprintf("Config文件解析失败 %s", err.Error()))
		return nil, err
	}

	// 判断配置是否正确
	if len(conf.Publishers) == 0 {
		// fmt.Println(text.FgYellow.Sprint("没有可用store"))
		return nil, errors.New("无可用store")
	}

	return &conf, nil
}
