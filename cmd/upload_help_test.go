package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDo(t *testing.T) {

	cfgFilePath = "/Users/gix/Documents/GitHub/apkgo/.apkgo.json"
	// initConfig()

	stores = []string{"vivo", "cams", "apkgo_demo"}
	releaseNots = "1. 提升稳定性\n2.优化性能"
	file = "/Users/gix/Documents/aster/build/app/outputs/flutter-apk/app-release.apk"

	assert.NoError(t, initialPublishers())

	// req := assemblePublishRequest()

	// publish(req)
}
