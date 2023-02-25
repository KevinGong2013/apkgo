package cmd

import (
	"fmt"
	"testing"
)

func TestDo(t *testing.T) {

	if err := write(&Config{}); err != nil {
		fmt.Println(err)
	}

	// cfgFilePath = "/Users/gix/Documents/GitHub/apkgo/.apkgo.json"
	// initConfig()

	// stores = []string{"huawei"}
	// releaseNots = "1. 提升稳定性\n2.优化性能"
	// file = "/Users/gix/Documents/GitHub/apkgo/app-release.apk"

	// initialPublishers(false)

	// req := assemblePublishRequest()

	// isDebugMode = false
	// err := notify(nil, req, publish(req))

	// fmt.Println(err)
}
