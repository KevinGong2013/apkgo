package cmd

import (
	"fmt"
	"testing"
)

func TestDo(t *testing.T) {

	cfgFilePath = "/Users/gix/Documents/GitHub/apkgo/.apkgo.json"
	initConfig()

	stores = []string{"vivo"}
	releaseNots = "1. 提升稳定性\n2.优化性能"
	file = "/Users/gix/Documents/aster/build/app/outputs/flutter-apk/app-release.apk"

	initialPublishers()

	req := assemblePublishRequest()

	isDebugMode = false
	err := notify(req, publish(req))

	fmt.Println(err)
}
