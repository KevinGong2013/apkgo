package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDo(t *testing.T) {

	cfgFile = "/Users/gix/Documents/GitHub/apkgo/.apkgo.json"

	err := InitialPublishers([]string{"vivo", "cams", "apkgo_demo"})
	assert.NoError(t, err)

	err = Do("1. 提升稳定性\n2.优化性能", "/Users/gix/Documents/aster/build/app/outputs/flutter-apk/app-release.apk")
	assert.NoError(t, err)
}
